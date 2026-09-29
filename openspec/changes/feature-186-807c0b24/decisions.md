# Decisions

## propose

- **Decision:** Go with caveats. Caveats: CI and comment rules are duplicated from `codagent:wait-ci`, and review-bot progress detection loses LLM judgment.
  - Alternatives: no-go (keep the agent wait); shorten the agent's poll cadence instead.
  - Decision-bearing: yes. It matches the issue's stated goal and acceptance criteria, so it is not a stop.
- **Decision:** Port the polling and comment logic into bundled scripts under `workflows/core/` instead of calling the agent-skills scripts by installed path.
  - Alternatives: call `skills/wait-ci/scripts/*.sh` from the plugin install; change `codagent:wait-ci` in the agent-skills repo.
  - Decision-bearing: yes. It stays inside the target repo. Depending on the plugin path would make an embedded workflow depend on an external install location.
- **Decision:** The collector exits 0 whenever it determines a status, including `CI_FAILED`. It exits non-zero only on fatal errors, and the step keeps `continue_on_failure: true`.
  - Alternatives: map markers to exit codes.
  - Decision-bearing: no. This keeps the gate scripts as the sole control-flow authority, so they stay unchanged.
- **Decision:** Classify deterministically first. A cheap classifier step is allowed only if design proves it necessary, and it must never run on the clean-pass path or in the lead session.
  - Alternatives: always run a Sonnet-low classifier; never allow one.
  - Decision-bearing: yes. It follows the issue's conditional wording and preserves "no agent turn in a passing run".
- **Decision:** Treat ambiguous or unfinished review-bot evidence at the deadline as `CI_REVIEW_INCOMPLETE`, which is today's warning path.
  - Alternatives: treat it as `CI_PENDING`, which would consume a fix cycle.
  - Decision-bearing: no. It matches the current prompt's rules.
- **Decision:** Keep the overall wait deadline at about 15 minutes by default, matching `codagent:wait-ci`.
  - Alternatives: add a new workflow param.
  - Decision-bearing: no. Design may expose a param later.
- **Decision:** Leave `codagent:wait-ci` and `codagent:fix-pr` in the agent-skills repo unchanged. `fix-pr` receives the report in its prompt, and its own brief context gathering stays.
  - Alternatives: edit the skills to accept a precomputed report.
  - Decision-bearing: yes. Editing them would be a change outside the target repository, so it is out of scope.

## proposal-review

- **F1 (structural): no source for which review bots are expected. Applied.**
  - Decision: expected bots are the optional `review_bots` param plus any bot that has already left evidence on the PR. Freshness uses timestamps measured from the later of the latest push and the ready-for-review event.
    - An expected bot that never starts within the grace period gives `CI_REVIEW_INCOMPLETE`.
    - With no expected bot, the collector waits one short start grace, then allows `CI_PASSED`.
    - A newly installed bot that appears after the grace is expected on later runs.
  - Alternatives: a repo-level config file (a new persisted format, rejected as heavier); always waiting the full bot window (wasted wall-clock time on repos without bots); vendor-specific hard-coding (brittle).
  - Decision-bearing: yes. It is not direction-level: the new param is optional and additive, stays in this repo, and matches today's prompt behavior of waiting a bounded time for a bot to start.
- **F2 (significant): stalled `gh` calls are not bounded, because script steps have no runner timeout. Applied.**
  - Decision: every `gh` call gets a portable per-call timeout, capped by the time left before the overall deadline.
    - Timeouts and transient errors are retried.
    - Auth failure or a missing PR exits non-zero.
    - Unread check state at the deadline gives `CI_PENDING`.
    - Unread comment or review data gives `CI_PENDING` and is never treated as a pass.
    - A blocked-`gh` stub test is required.
  - Alternatives: add a runner-level step timeout (broader runner change, out of scope); rely on GNU `timeout` (absent on macOS).
  - Decision-bearing: yes. It is not direction-level.

## spec

- **Decision:** Use a single new capability, `finalize-pr-ci-wait`, with no modified capabilities.
  - Alternatives: modify `builtin-workflows`. Rejected because none of its current requirements cover CI waiting.
  - Decision-bearing: no.
- **Decision:** Carry the comment classification rules over unchanged from `codagent:wait-ci`: blocking `CHANGES_REQUESTED`, actionable, deferred, and informational.
  - Alternatives: simplify the rules.
  - Decision-bearing: no. This preserves today's behavior.
- **Decision:** Fatal collector errors (no PR, auth failure, missing tool) produce no marker and flow through the existing unknown-marker gate paths: the loop re-polls without a fix, and the final gate fails.
  - Alternatives: abort `core:finalize-pr` immediately.
  - Decision-bearing: no. It keeps the gates as the only control-flow authority.
- **Decision:** With no expected bot, report `CI_PASSED` right after one start grace instead of waiting for the overall deadline.
  - Alternatives: wait for the full deadline.
  - Decision-bearing: no.
- **Decision:** Fix the overall deadline default at 15 minutes and leave the exact start-grace duration to design.
  - Alternatives: specify both durations now.
  - Decision-bearing: no.

## design

- **Decision:** Build the collector as Python 3 (`ci_wait.py`) behind a POSIX `sh` entry point (`ci-wait.sh`).
  - Alternatives: port the bash+jq scripts with a hand-rolled timeout; a hidden `agent-runner` Go subcommand.
  - Decision-bearing: yes. Python gives portable per-call timeouts and native JSON, and `python3` is already required by core workflows. A Go subcommand cannot be located reliably from a script step and would add a CLI surface.
- **Decision:** Take one GraphQL snapshot per poll, covering head checks with app identity, reviews, threads, comments, mergeability, and the ready event.
  - Alternatives: keep separate `gh pr checks`, REST reviews, and GraphQL comments calls.
  - Decision-bearing: no.
- **Decision:** A bot is expected only if it is in `review_bots` or has submitted a PR review. Bots that only comment or post checks do not count. The spec and proposal were updated to match.
  - Alternatives: count any bot activity, as the earlier spec wording did.
  - Decision-bearing: yes. Counting all bot activity would turn coverage, deploy, and `github-actions` bots into false `CI_REVIEW_INCOMPLETE` results.
- **Decision:** Read bot progress only from head-tied checks, statuses, and reviews, never from comment text. As a result, no LLM classifier step is added.
  - Alternatives: keyword heuristics on comments; a cheap classifier step.
  - Decision-bearing: yes. Rate-limited or skipped bots stay unfinished, giving `CI_REVIEW_INCOMPLETE` at the deadline, which is today's intended outcome at zero token cost.
- **Decision:** Checks from expected review bots are excluded from CI checks, so a pending bot status gives `CI_REVIEW_INCOMPLETE`, never `CI_PENDING`. Added to the spec.
  - Alternatives: treat all checks as CI.
  - Decision-bearing: no. This preserves the current prompt's rule.
- **Decision:** The freshness point uses the head commit's `committedDate`, because GitHub exposes no push time.
  - Alternatives: the collector's own start time, which is wrong for re-poll cycles.
  - Decision-bearing: no.
- **Decision:** Timing defaults are a 900 s deadline, 15 s poll interval, 180 s start grace, and 30 s per call. Timings can be overridden only through stdin inputs, for tests. The deferred start-grace scenario was resolved to 3 minutes.
  - Alternatives: expose timings as workflow params.
  - Decision-bearing: no.
- **Decision:** `verify-final` gets `continue_on_failure: true`, so a fatal collector error reaches `final-ci-status-gate`, which then fails the workflow.
  - Alternatives: let the step failure abort the workflow directly.
  - Decision-bearing: no. The workflow still fails either way.
- **Decision:** Only the final report is written to stdout, because stdout is captured. Progress lines go to stderr.
  - Alternatives: none viable, since stdout progress would pollute the capture.
  - Decision-bearing: no.
- **Decision:** Edit `finalize-pr-v1.0.yaml` in place rather than adding a new v1.1.
  - Alternatives: add a new version and repoint the parent workflows.
  - Decision-bearing: no. Params and markers stay compatible.

## test-plan

- **Decision:** No automated E2E test against live GitHub. INT-001 to INT-003 run the embedded workflow through the real runner, with real scripts, a fake `gh`, and stubbed agents.
  - Alternatives: a live-PR E2E in CI.
  - Decision-bearing: no. Live GitHub would be slow and flaky, and would create external effects.
- **Decision:** INT-004 validates the GraphQL query against GitHub's schema, using a vendored SDL or, as a fallback, a recorded real-response fixture.
  - Alternatives: rely only on the fake `gh`.
  - Decision-bearing: no. A fake cannot catch a wrong field name.
- **Decision:** Timing overrides for workflow-level tests use a test-only mechanism, such as an env var default, and never workflow YAML changes.
  - Alternatives: expose timing params.
  - Decision-bearing: no.
- **Decision:** The acceptance envelope allows read-only `gh` calls on repository PRs, and a full `finalize-pr` run only on the change's own PR. It forbids new PRs, comment or review mutations, and merges.
  - Alternatives: allow scratch PRs.
  - Decision-bearing: no.
- **Decision:** Human-only testing is none.
  - Alternatives: none.
  - Decision-bearing: no.

## approach-review

- **F1 (high): head-tied bot evidence from before the ready transition counted as fresh, and `committedDate` misses old commits that were just pushed. Applied.**
  - Decision: the observable push point is the head's earliest check-suite `createdAt` or its force-push event. `committedDate` is dropped. When neither timestamp exists, the first observation bounds the grace but does not invalidate evidence tied to the current head.
    - Head-tied evidence must post-date the freshness point, so a review completed while the PR was a draft is stale after the ready transition.
    - The grace ends at max(freshness point, wait start) + 3 minutes.
    - Bot state resets when the head changes during the wait.
    - Spec, design, and proposal updated, with new scenarios and tests.
  - Alternatives: keep `committedDate`; exempt head-tied evidence from freshness.
  - Decision-bearing: yes. It is not direction-level: it tightens the proposal's own freshness promise.

## implementation-review

- **Missing push timestamp:** Accept existing current-head bot evidence after any ready transition when neither a check suite nor a matching force-push event establishes push time. The observation time still bounds the start grace. This avoids declaring completed reviews unfinished merely because the collector started later.
- **Latest bot progress:** A newer pending or non-success head-tied result supersedes an earlier success. Top-level comments show that a bot started but do not override check, status, or review progress.
- **Addressed top-level feedback:** Human top-level comments predating an observable push or a later author top-level reply are no longer actionable. Comments with missing timestamps remain actionable. This is the available deterministic approximation because GitHub issue comments do not form reply threads.
- **F2 (high): any terminal bot check counted as finished, including skipped or failed ones. Applied.**
  - Decision: only fresh `SUCCESS` checks or statuses, or a review on the head commit, count as finished. `SKIPPED`, `NEUTRAL`, and failure-class outcomes leave the bot unfinished, giving `CI_REVIEW_INCOMPLETE`, and the report shows the conclusion. Scenario and tests added.
  - Alternatives: treat `NEUTRAL` or `SKIPPED` as finished.
  - Decision-bearing: yes. It is not direction-level.
- **F3 (high): 100-item truncation could hide a failure or feedback and still pass. Applied.**
  - Decision: every connection is paginated through bounded continuation queries. An incomplete snapshot at the deadline counts as pending, so `CI_PENDING` results unless failure or comment evidence already read takes precedence.
    - New spec requirement: "Complete evidence before passing".
    - Pagination cases added to the design tests and to INT-004.
  - Alternatives: accept truncation, as the current scripts partly do.
  - Decision-bearing: yes. It is not direction-level.
- **F4 (medium): the acceptance envelope allowed a live `finalize-pr` run whose `fix-pr` step would mutate review threads, contradicting its off-limits rules. Applied.**
  - Decision: live acceptance is limited to direct collector runs. The live `finalize-pr` run is no longer authorized, and the fix path is covered by INT-002.
  - Alternatives: allow thread mutations on the change's own PR.
  - Decision-bearing: no.

## tasks

- **Decision:** `tasks.md` has a single implementation task that links every definition artifact, following the format of recent factory changes.
  - Alternatives: split into collector, workflow wiring, and tests tasks.
  - Decision-bearing: no. The step instructions require exactly one task.
