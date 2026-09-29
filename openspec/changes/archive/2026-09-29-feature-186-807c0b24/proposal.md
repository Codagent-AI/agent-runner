## Why

`core:finalize-pr` (`workflows/core/finalize-pr-v1.0.yaml`) waits for CI inside the lead agent's session. The `wait-ci` step in `ci-fix-loop` and the `verify-final` step both ask the lead agent (Opus, high effort in factory fix and feature runs) to invoke `codagent:wait-ci`. That skill is deliberately a loop of bounded Bash calls: `check-ci.sh` polls for about 90 seconds, returns, and the agent calls it again, up to about 15 minutes, then fetches comments and may poll again while a review bot finishes. Every one of those turns is a billed model turn in which nothing is decided. The Sep 21–28 usage analysis put CI waiting at about $30–$40 a week, roughly 12% of instrumented factory Claude spend.

Almost none of that work needs judgment. Polling `gh pr checks`, pulling failed-job logs, reading unresolved review threads, and choosing among five status markers by a fixed precedence are deterministic. The skill already pushes this work into scripts (`check-ci.sh`, `get-pr-comments.sh`) and uses the agent mainly as a loop driver. Agent Runner already has the primitive the loop needs: bundled script steps with captured output, gated by `ci-status-gate.sh` and `ci-fix-needed-gate.sh`. Moving the wait into a script step removes an expensive idle session from every factory run that opens a PR. It also makes finalization more predictable, because a script does not reinterpret the polling rules on each call.

## What Changes

- Replace the agent-driven `wait-ci` step in `ci-fix-loop` with a bundled script step that owns the CI wait:
  - polls the PR's checks until they reach a terminal state or a bounded timeout;
  - waits a bounded time for expected review bots to start and finish after the latest push. A bot is expected if it is listed in a new optional `review_bots` param or has already submitted a review on the PR. Status or comments older than the latest push or the draft-to-ready transition count as stale;
  - collects failed-check logs, blocking reviews, merge conflicts, and unresolved review threads and comments, keeping deferred threads and informational bot comments separate;
  - writes a compact human-readable report to `ci_report` whose final non-empty line is one of the existing markers (`CI_PASSED`, `CI_FAILED`, `CI_COMMENTS`, `CI_PENDING`, `CI_REVIEW_INCOMPLETE`), chosen by the precedence the current prompt defines.
- Resume the lead session (`session: lead-agent`) only in `fix-pr`, with the captured CI report in the prompt, so the lead agent starts from the failure details instead of polling for them.
- Replace the agent-driven `verify-final` step with the same script step, capturing `final_ci_report`. `final-ci-status-gate` and `final-review-incomplete-gate` stay as they are.
- Keep the marker contract and the three gate scripts unchanged. The `ci_fix_cycles` budget, the loop's fall-through on an exhausted budget, and the final verification behave as they do today.

- Add an optional `review_bots` param to `core:finalize-pr`: comma-separated bot logins, empty by default. This is additive, and existing callers keep working without it.

No public CLI, workflow-schema, or persisted-state format changes. The change is contained in the embedded `core:finalize-pr` workflow and its bundled scripts.

## Capabilities

### New Capabilities
- `finalize-pr-ci-wait`: How `core:finalize-pr` waits for and classifies PR CI and review state without an agent session. This covers polling bounds, review-bot staleness and wait rules, report contents, marker selection and precedence, and when the lead session is resumed.

### Modified Capabilities
- None. The existing `builtin-workflows` requirements (core namespace contents, versioned sub-workflow references) are unchanged. The script mechanics in `workflow-bundled-scripts` are used as specified.

## Technical Approach

- **Bundled script, not an agent skill.** A new bundled script in `workflows/core/` (a small `sh` entry point plus a Python 3 collector, per design) runs as a `script:` step with `capture: ci_report`. It needs `gh`, which `record-pull-request` already uses, and `python3`, which core workflow scripts already require. The polling and comment-classification rules are ported from `codagent:wait-ci`'s `check-ci.sh` and `get-pr-comments.sh` rather than referenced by path. An embedded workflow cannot depend on where an agent plugin happens to be installed.
- **One process owns the whole wait.** The script runs to completion inside one step, looping internally with sleeps and an overall deadline (default about 15 minutes, matching the skill). The existing stdout tee shows progress in the live view.
- **Every external call is bounded.** The runner runs script steps with `exec.Command(...).Run()` and no context or step timeout, in both `cmd/agent-runner/main.go` and `internal/liverun/process_runner.go`. A deadline check between sleeps therefore cannot bound a stalled `gh` call. The collector wraps each `gh` or API call in a per-call timeout that is capped by the time left before the overall deadline. The timeout must be portable and must not assume GNU `timeout`, which macOS lacks by default.
  - A timed-out or transiently failing call is retried while time remains.
  - Authentication failure or a missing PR is fatal: non-zero exit with a diagnostic.
  - If the deadline passes and check state was never read successfully, the result is `CI_PENDING`.
  - If checks are green but comment or review data could not be read, the result is also `CI_PENDING`, never `CI_PASSED` or `CI_REVIEW_INCOMPLETE`. Unread comments must not be treated as clear.
  - Tests include a `gh` stub that blocks past the deadline.
- **Exit code and capture.** The script exits 0 whenever it produces a status, including `CI_FAILED`, so the gates, not the collector, decide control flow. On a fatal error (no PR, missing `gh`, auth failure) it exits non-zero with a diagnostic. The step keeps `continue_on_failure: true`, and the unknown-marker path behaves as it does today when the agent produces no marker.
- **Deterministic classification first.** `get-pr-comments.sh` already decides actionable, deferred, and informational comments without an LLM. Review-bot progress is read only from current-head check runs, commit statuses, and reviews. Bot comment text is never interpreted, so no LLM classifier step is needed.
- **Which review bots are expected.** A script cannot tell a delayed review from a repository that has no review bot, so the collector uses an explicit expected set:
  - Expected bots are the logins in `review_bots` plus any bot that has already submitted a pull-request review on this PR, on any head. Bots that only comment or post checks do not qualify (see design).
  - Freshness uses timestamps: bot evidence counts only if it is dated after the later of an observable head push point and the PR's most recent ready-for-review event. GitHub exposes no push time, so the push point is taken from the head's earliest check suite or force-push event. If neither exists, first observation bounds the start grace without invalidating existing current-head evidence. A bot counts as finished only when its latest head-tied progress is a successful check, status, or head review. Every collection is read across all pages, and an incomplete read can never pass.
  - An expected bot with no fresh evidence gets a bounded start grace period (a few minutes, inside the overall deadline). If it never starts, or starts and does not finish, the result is `CI_REVIEW_INCOMPLETE` when CI is otherwise green and no actionable feedback exists.
  - When no bot is expected, the collector still waits one short start grace after a fresh push or ready transition, so a newly installed bot's first appearance is caught. The wait costs wall-clock time only, never tokens. If no bot appears, `CI_PASSED` is allowed once checks and comments are clear. A bot that first appears after that grace is not expected for this run, and the next finalize-pr run expects it.
- **Lead resume only for fixes.** `fix-pr` keeps `session: lead-agent` and `skip_if: previous_success`. Its prompt embeds `{{ci_report}}` and tells the agent to start from that report. In a passing run, no agent turn happens after `push-pr` completes. In a failing run, the lead session is resumed once per fix cycle.
- **Tests.** Go tests next to the existing `ci_status_gate_test.go` run the collector against a stubbed `gh` on `PATH` for passing, failing, comments, pending timeout, stale bot, incomplete bot, expected bot that never starts, no expected bot, blocked `gh` call, and conflict cases. They assert the marker and report shape. A workflow-shape test asserts that `wait-ci` and `verify-final` are script steps and that `fix-pr` receives the report.

## Out of Scope

- Changing `codagent:wait-ci` or `codagent:fix-pr` in the agent-skills repository. Interactive users of those skills keep today's behavior.
- Changing the marker set, the gate scripts, or `ci_fix_cycles` semantics.
- Adding runner-level features such as step timeouts, external event waiting, or webhook-driven resume.
- Other workflows that wait on CI outside `core:finalize-pr`.
- Changing how `push-pr` or `fix-pr` push commits.

## Impact

- `workflows/core/finalize-pr-v1.0.yaml`: `wait-ci` and `verify-final` become script steps, and the `fix-pr` prompt carries the report. Every embedding workflow picks this up, including `openspec:change`, `spec-driven:change`, and `implement-change`.
- New bundled script or scripts in `workflows/core/`, with Go tests in `workflows/`.
- `docs/built-in-workflows.md` gets a short note that CI waiting is script-driven.
- Runtime dependency: `gh` (authenticated) and `python3` must be on `PATH` where finalize-pr runs. This is already true in practice for PR workflows. Without them, the collector fails visibly instead of an agent working around it.
- Maintenance: the CI polling and comment rules now exist both in agent-runner and in `codagent:wait-ci`. Future rule changes need to be mirrored, or the skill could later call the bundled script.
- Cost: removes agent polling turns from every factory finalization. The expected saving is most of the measured $30–$40 a week.
