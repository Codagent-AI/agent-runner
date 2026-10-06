## Context

`core:finalize-pr` (`workflows/core/finalize-pr-v1.0.yaml`) currently waits for CI in agent steps:
- `ci-fix-loop` contains the `wait-ci` step (`session: lead-agent`).
- The final `verify-final` step uses `session: resume`.

Both ask the lead agent to run `codagent:wait-ci`. That skill drives two scripts in a loop of bounded Bash calls:
- `check-ci.sh` polls `gh pr checks`, reviews, and mergeability.
- `get-pr-comments.sh` runs a GraphQL query and classifies review threads and comments.

Each Bash call is a billed model turn.

Relevant runtime facts:
- **Script steps** (`internal/exec/script.go`) run a bundled script via its shebang with `exec.Command(path).Run()`. They have no context and no timeout, in both `cmd/agent-runner/main.go` and `internal/liverun/process_runner.go`. stdin carries the JSON-encoded `script_inputs`.
- **Capture on failure.** stdout is captured into `capture` whether or not the script fails. The capture happens before the exit-code check.
- **Bundled assets.** A builtin namespace is materialized whole under `<session>/bundled/<namespace>/`, with `.sh` files at mode `0o700` and other files at `0o600`. Scripts receive `AGENT_RUNNER_BUNDLE_DIR`, and existing scripts locate helpers through `$(dirname "$0")`.
- **Existing tool dependencies.** `python3` is already an unconditional runtime dependency of core workflows: `validate-planning-artifacts.sh` calls it with no fallback. The other gate scripts use it when `jq` is missing.
- **Gates.** The three gate scripts, `ci-status-gate.sh`, `ci-fix-needed-gate.sh`, and `ci-review-incomplete-gate.sh`, read the last non-empty line of `report`. `workflows/ci_status_gate_test.go` covers them.

The behavioral contract is `specs/finalize-pr-ci-wait/spec.md`.

## Goals / Non-Goals

**Goals:**
- One deterministic CI-wait script, used by both the in-loop wait and the final wait, that emits the existing marker contract.
- Hard wall-clock bounds even when `gh` hangs.
- Review-bot expectation and freshness rules that are vendor-neutral and testable offline.
- `fix-pr` resumes the lead with the report embedded in its prompt.

**Non-Goals:**
- Runner-level step timeouts, or any Go runner change.
- Changes to the gate scripts, the marker set, or `ci_fix_cycles`.
- Changes to `codagent:wait-ci` or `codagent:fix-pr` in the agent-skills repository.
- An LLM classifier step. None is needed, as explained under Decisions.

## Approach

### Files

| File | Role |
|---|---|
| `workflows/core/ci-wait.sh` | POSIX `sh` entry point. It checks that `python3` and `gh` are on `PATH`; if either is missing, it exits 2 with a diagnostic on stderr. It then runs `exec python3 "$script_dir/ci_wait.py"`, with stdin passed through. |
| `workflows/core/ci_wait.py` | The collector: a single-file Python 3 script with no third-party imports. |
| `workflows/core/finalize-pr-v1.0.yaml` | Replaces the `wait-ci` and `verify-final` agent steps with script steps, adds the `review_bots` param, and embeds the report in the `fix-pr` prompt. |
| `workflows/ci_wait_test.go` | Go tests that run the collector against a fake `gh`. |
| `workflows/finalize_pr_test.go` (or an extension of `pull_request_test.go`) | Workflow-shape assertions. |
| `docs/built-in-workflows.md` | A short note on script-driven CI waiting and `review_bots`. |

### Workflow shape

```yaml
params:
  - name: ci_fix_cycles      # unchanged
  - name: review_bots
    required: false
    default: ""

# inside ci-fix-loop
- id: wait-ci
  script: ci-wait.sh
  capture: ci_report
  continue_on_failure: true
  script_inputs:
    review_bots: "{{review_bots}}"
# ci-status-gate, ci-fix-needed-gate: unchanged
- id: fix-pr
  session: lead-agent
  mode: autonomous
  skip_if: previous_success
  prompt: |
    The workflow's CI wait for this cycle produced the report below. Start from it; do not poll CI
    again before fixing. Invoke codagent:fix-pr ... (existing instructions and scope boundary unchanged)

    <ci-report>
    {{ci_report}}
    </ci-report>

# after the loop
- id: verify-final
  script: ci-wait.sh
  capture: final_ci_report
  continue_on_failure: true
  script_inputs:
    review_bots: "{{review_bots}}"
# final-ci-status-gate, final-review-incomplete-gate: unchanged
```

`verify-final` gets `continue_on_failure: true` so that a fatal collector error, which produces no marker, reaches `final-ci-status-gate`. The gate then fails the workflow with its standard message. This matches the spec's authentication-failure scenario. Parent workflows (`openspec:change`, `spec-driven:change`, `spec-driven:implement-change`, `openspec:implement-change`) pass nothing new and so get the default.

### Collector inputs (stdin JSON)

All inputs are strings, because script inputs are interpolated strings. Unknown keys are ignored.

| Key | Default | Purpose |
|---|---|---|
| `review_bots` | `""` | Comma-separated logins. Entries are trimmed and normalized. |
| `deadline_seconds` | `900` | Overall deadline. |
| `poll_interval_seconds` | `15` | Sleep between polls. |
| `bot_start_grace_seconds` | `180` | Start grace, measured from the freshness point. |
| `call_timeout_seconds` | `30` | Per-call cap. |

The workflow passes only `review_bots`. Tests pass small timing values so they run in seconds.

### Output channels

- **stdout** carries only the final report, written once at exit. It is captured, so progress lines on stdout would pollute `ci_report` and could displace the marker.
- **stderr** carries one progress line per poll, for example `ci-wait: poll 4, 3 checks pending, awaiting coderabbitai, 11m left`. It is visible in the live view and in the audit.

### Per-poll snapshot

Each poll makes one primary GraphQL call through `gh api graphql`, followed by page-continuation calls as needed (see *Pagination* below). It uses pull-request lookup by number, and the number is resolved once at startup with `gh pr view --json number,url`. The call fetches:

- **PR fields:**
  - `url`, `isDraft`, `mergeable`, `author { login __typename }`;
  - `headRefOid`, and the head commit's `checkSuites(first: 100) { nodes { createdAt } pageInfo }`;
  - `timelineItems(itemTypes: [READY_FOR_REVIEW_EVENT, HEAD_REF_FORCE_PUSHED_EVENT], last: 20)`, with `createdAt` and, for force pushes, `afterCommit { oid }`.
- **Head checks:** `statusCheckRollup.contexts(first: 100)` on the head commit:
  - `CheckRun { name status conclusion startedAt completedAt detailsUrl checkSuite { app { slug } } }`;
  - `StatusContext { context state targetUrl creator { login } createdAt }`.
- **Reviews:** `reviews(last: 100) { author { login __typename } state submittedAt commit { oid } body }`.
- **Review threads:** `reviewThreads(first: 100)`, with the same fields `get-pr-comments.sh` uses, plus `comments(first: 100) { pageInfo }` per thread. Classification logic is ported line-for-line.
- **Top-level comments:** `comments(last: 100) { author { login __typename } body updatedAt }`.

Every connection selects `pageInfo { hasNextPage endCursor }` (`hasPreviousPage startCursor` for `last:` connections).

**Pagination.** After the primary call, each connection that has more pages is followed with narrow continuation queries. This covers check contexts, check suites, reviews, review threads, comments inside a thread, and top-level comments. Each continuation query fetches only that connection by cursor, through the same bounded `run` helper. A snapshot is **complete** only when every connection has been read to its end. Continuation failures are transient. The snapshot keeps the pages it has read, marks itself incomplete, and the next poll retries from the start.

If the snapshot fails after `mergeable` is known, the previous snapshot is kept, marked stale. The next poll retries.

### Identity normalization

A bot identity is `lower(login)` with a trailing `[bot]` removed. For check runs, the identity is `checkSuite.app.slug`. For status contexts, it is `creator.login`. So `coderabbitai`, `coderabbitai[bot]`, and app slug `coderabbitai` all match.

### Classification per snapshot

1. **Expected bots** are the `review_bots` entries plus every identity whose author `__typename` is `Bot` and that has submitted a pull-request review (any state, any commit). Top-level-only bot commenters are *not* expected. Their comments stay informational. Bots that appear only in check runs are not expected either, which keeps `github-actions` and similar apps out of the set.
2. **Push point and freshness point.**
   - The push point is the later of two times: the earliest check-suite `createdAt` on the head commit, and the latest `HeadRefForcePushedEvent.createdAt` whose `afterCommit.oid` equals the head. GitHub creates check suites when a commit is pushed, so the earliest one approximates push time even for an old commit.
   - If neither exists, for example in a repository with no Actions or apps, the first observation of the head bounds the start grace. It does not reject already-completed evidence explicitly tied to the current head.
   - The freshness point is the later of the observable push point, when available, and the latest `ReadyForReviewEvent.createdAt`.
   - The commit's `committedDate` is never used.
3. **Check partition.** Check contexts on the head whose identity is an expected bot are *bot evidence*. All others are *CI checks*. CI checks are bucketed as pass, fail, or pending using `check-ci.sh`'s state sets:
   - **fail:** `FAILURE`, `CANCELLED`, `TIMED_OUT`, `ACTION_REQUIRED`, `STARTUP_FAILURE`, `ERROR`;
   - **pass:** `SUCCESS`, `SKIPPED`, `NEUTRAL`;
   - **pending:** everything else, including `EXPECTED` and statuses without a conclusion.
4. **Bot state**, for each expected bot:
   Evidence is *fresh* only when its timestamp is after the freshness point, for head-tied evidence too. The timestamps used are:
   - check run: `completedAt`, or `startedAt` while running;
   - status: `createdAt`;
   - review: `submittedAt`;
   - top-level comment: `updatedAt`.

   The states are:
   - **finished:** the latest fresh head-tied progress evidence is positive completion: a head check run with conclusion `SUCCESS`, a head status with state `SUCCESS`, or a review whose `commit.oid` equals the head. A later pending or non-success status supersedes an earlier success. Top-level comments can show that a bot started but do not supersede head-tied progress.
   - **in progress:** there is fresh head-tied evidence, such as a pending check or status, or a non-success terminal outcome (`SKIPPED`, `NEUTRAL`, `FAILURE`, `CANCELLED`, `TIMED_OUT`, `ACTION_REQUIRED`, `STALE`, `STARTUP_FAILURE`, `ERROR`), and the bot is not finished. The report shows the latest outcome, for example `coderabbitai: skipped`.
   - **started:** there is any fresh evidence, including a fresh top-level comment.
   - **not started:** otherwise.

   A later fresh `SUCCESS` from the same identity overrides earlier pending or non-success outcomes. That is the "current-head terminal status overrides earlier notice" rule. Comment text is never interpreted.
5. **Blocking reviews** are the latest non-`PENDING` review per reviewer with state `CHANGES_REQUESTED`.
6. **Threads and comments** use the `get-pr-comments.sh` thread deferral rules. Human top-level comments are actionable only if they postdate the observable push and any later author top-level reply; when timestamps are unavailable they remain actionable. Bot top-level comments are informational.
7. **Merge state:** `CONFLICTING` means failed. `UNKNOWN` means not settled.

### Loop and termination

```text
start → resolve PR (fatal on no-PR/auth) → loop:
  snapshot (bounded calls, all pages; on transient error keep last good, retry next poll)
  if head OID changed since last poll → drop bot state, recompute push point,
                                        freshness point, and grace for the new head
  if CI failed or CONFLICTING or blocking review         → finish
  settled_ci   = no pending CI checks and mergeable ∈ {MERGEABLE}
  grace_end    = max(freshness point, wait start) + grace
  settled_bots = every expected bot is finished, or is not started with now ≥ grace_end
  no_expected  = expected set empty and now ≥ grace_end
                 and no fresh unexpected bot evidence
  if settled_ci and snapshot complete and (settled_bots or no_expected) → finish
  if now ≥ deadline                                     → finish
  sleep min(poll_interval, remaining)
```

In the no-expected case, a bot that shows fresh head-tied pending evidence during the grace (for example, a first-time `CodeRabbit` pending status from a bot that has never submitted a review) joins the expected set for the rest of this wait. It is not persisted, because next time it is expected through its review.

The loop finishes early on a CI failure without waiting for bots, which matches `check-ci.sh`. Otherwise it waits for the review bots to settle even when actionable comments already exist, so the lead gets the bot's full findings in one cycle. This matches today's instruction to wait a bounded time for an active reviewer.

### Status selection at finish

Precedence is `CI_FAILED` > `CI_COMMENTS` > `CI_PENDING` > `CI_REVIEW_INCOMPLETE` > `CI_PASSED`.

| Marker | Condition |
|---|---|
| `CI_FAILED` | A failed CI check, `CONFLICTING`, or a blocking review. |
| `CI_COMMENTS` | Actionable threads or blocking human comments, taken from the last successful comment read. |
| `CI_PENDING` | A CI check is still pending, mergeability is `UNKNOWN`, the checks were never read successfully, the latest comment/review read failed and no successful read happened after the last head change, or the final snapshot is incomplete (a connection still has unread pages). |
| `CI_REVIEW_INCOMPLETE` | An expected bot is not finished. |
| `CI_PASSED` | Otherwise. |

### Report format (stdout)

```markdown
## CI Status: <passed|failed|comments|pending|review incomplete>

**PR:** <url>
**Head:** <short sha>
**Elapsed:** ~<N> minutes

### Failed Checks        (name, link, last 100 lines of `gh run view <id> --log-failed`)
### Merge Conflicts      (paths from `git merge-tree` when both commits are local)
### Blocking Reviews
### PR Comments          (actionable threads and blocking human comments)
### Deferred Threads
### Unfinished Review Bots  (identity + not started | in progress)
### Informational Bot Comments  (bodies truncated to 500 chars)
### Still Running
### Passing Checks       (names only)

CI_FAILED
```

Only non-empty sections are printed. Log fetching runs only when the status is `CI_FAILED`. Each fetch is bounded by the per-call timeout, and all fetches together are capped by a 60-second post-deadline margin. A fetch that exceeds the margin is noted as `(log unavailable: timed out)`.

### Bounded calls

Every external command goes through a single helper, `run(args, timeout)`:
- It calls `subprocess.run(..., timeout=min(call_timeout, max(1, remaining)), start_new_session=True)`.
- On `TimeoutExpired`, it kills the process group with `os.killpg`, so `gh` children cannot outlive the call, and returns a transient error.

`git merge-tree` goes through the same helper. The whole process is therefore bounded by deadline + call timeout + 60 s of log margin, which is the spec's "fixed margin".

### Error taxonomy

| Situation | Handling |
|---|---|
| `python3` or `gh` missing | `ci-wait.sh` exits 2, prints a diagnostic to stderr, and writes nothing to stdout. |
| Startup `gh pr view` stderr contains `no pull requests found` | Fatal: exit 1 with `ci-wait: no open pull request for branch <b>`. |
| Any `gh` call has an authentication failure (`gh auth login`, `HTTP 401`, `Bad credentials`) | Fatal: exit 1 with the `gh` message. |
| Other non-zero exit, timeout, or unparseable JSON | Transient: logged to stderr and retried on the next poll. |

On a fatal error, stdout is empty, so the captured report has no marker, and the existing gates treat it as unknown. In the loop, the status gate fails, the fix gate reports that no fix is needed, `fix-pr` is skipped, and the loop spends one iteration. At the final wait, `final-ci-status-gate` fails the workflow.

## Decisions

1. **Python 3 collector behind an `sh` entry point, rather than porting the bash+jq scripts.**
   - Python gives portable per-call timeouts with process-group kill, native JSON, and date parsing. These are the hard parts of the spec, and they are fragile in POSIX `sh` (GNU `timeout` is not on macOS).
   - `python3` is already required by core workflows.
   - The entry point stays `.sh` so it is materialized executable and matches existing script-step conventions.
   - *Alternatives:* bash+jq with a hand-rolled background-kill timeout (more code, harder to test); a hidden `agent-runner` Go subcommand (a script step cannot reliably find the running binary, since `./dev.sh` runs from source and nothing is on `PATH`, and it would add a CLI surface).
2. **One GraphQL snapshot per poll** replaces the separate `gh pr checks`, reviews REST, and comments GraphQL calls.
   - This means fewer calls to bound and a consistent point-in-time view.
   - It also exposes check-run app identity and timestamps, which `gh pr checks` does not.
3. **Expected bots come from submitted reviews, not from any bot activity.** This refines the spec's "any evidence" wording.
   - Coverage, deploy, and `github-actions` bots comment or post checks on most PRs. Counting them would turn every run into a false `CI_REVIEW_INCOMPLETE`.
   - Review bots such as CodeRabbit submit PR reviews.
   - The spec is updated to match.
4. **Bot progress is read only from head-tied checks, statuses, and reviews. Comment text is never parsed.**
   - This is vendor-neutral and deterministic.
   - A bot that is rate-limited or skipped without a terminal status or review stays unfinished and ends as `CI_REVIEW_INCOMPLETE` at the deadline. That is today's intended outcome, reached later on the wall clock but at zero token cost.
   - This removes any need for an LLM classifier step, so none is added, which keeps the passing path agent-free.
5. **Checks from expected review bots are excluded from CI checks.** A pending CodeRabbit status therefore yields `CI_REVIEW_INCOMPLETE`, not `CI_PENDING`. This preserves today's rule to "use `CI_PENDING` only for real CI checks", and the spec is updated to state it.
6. **Push time comes from observable PR activity.** GitHub exposes no push time for ordinary pushes.
   - The push point is the head's earliest check-suite creation or force-push event. When neither is available, the first observation of the head bounds the grace but does not invalidate current-head evidence. `committedDate` is not used, because an old commit can be pushed long after it was made.
   - Head-tied evidence must also post-date the freshness point, so a review completed while the PR was a draft is not reused after the ready transition.
   - The grace runs from the later of the freshness point and the wait start, and is recomputed when the head changes.
7. **Timing defaults:** 900 s deadline, 15 s poll interval, 180 s start grace, 30 s per call. The deadline and grace match the skill and the current prompt's "a few minutes". Timings can be overridden through stdin keys. When stdin omits a key, a test-only environment override applies (for example `AGENT_RUNNER_CI_WAIT_TIMINGS`), which workflow-level tests use to drive the embedded YAML. The workflow does not expose timings as params.
8. **The report is embedded inline in the `fix-pr` prompt, inside `<ci-report>` tags.** This is the simplest way to satisfy "resumed once with the report". The scope-boundary text is kept verbatim.

## Risks / Trade-offs

- **Rule drift from `codagent:wait-ci`.** Thread and comment classification is duplicated in `ci_wait.py`. *Mitigation:* the Go tests carry the classification fixtures from `get-pr-comments_test.sh`. A comment in `ci_wait.py` names the upstream source.
- **No check suites on the head.** In a repository with no Actions and no apps, there is no observable push timestamp. Current-head bot evidence can therefore be accepted even if it predates an unobservable push. A later ready-for-review transition still makes earlier evidence stale.
- **First-ever bot review with no pending status.** A review bot that has never reviewed this PR and posts no head status is not detected. The run can pass before that bot's first review arrives. On the next push it is expected. Users can set `review_bots` to close the gap.
- **Pagination cost.** Large PRs need extra continuation calls per poll. Each call is bounded, and an incomplete read can only produce `CI_PENDING`, never a pass.
- **Longer wall clock on unfinished bots.** A rate-limited bot now waits up to the deadline instead of an agent giving up earlier. There is no token cost.
- **Prompt size.** `fix-pr` prompts grow by the report (logs are capped at 100 lines per failed check). This is still far cheaper than the polling turns it replaces.

## Testing Strategy

The collector is tested in `workflows/ci_wait_test.go`, following the existing `ci_status_gate_test.go` pattern:
- The test materializes `core/ci-wait.sh` and `core/ci_wait.py` into a temp dir with `ReadAsset`.
- It puts a fake `gh` shell script first on `PATH`. The fake dispatches on its arguments, serves a sequence of fixture JSON snapshots (advancing a counter file per `graphql` call), and can be told to sleep forever or fail with a given stderr.
- It pipes small timings on stdin: deadline 3–5 s, interval 0.2 s, grace 1 s, call timeout 1 s.

Table cases assert the last non-empty stdout line, the section headings present, the exit code, and the elapsed-time bound:
- green → `CI_PASSED`;
- failed check with log → `CI_FAILED` plus log excerpt;
- conflicting → `CI_FAILED`;
- `CHANGES_REQUESTED` → `CI_FAILED`;
- actionable thread while a check is pending at the deadline → `CI_COMMENTS`;
- only deferred threads → `CI_PASSED`;
- human reply re-opens a deferred thread → `CI_COMMENTS`;
- bot summary only → `CI_PASSED`;
- pending check at the deadline → `CI_PENDING`;
- no checks → `CI_PASSED`;
- `UNKNOWN` mergeability → `CI_PENDING`;
- stale bot status → `CI_REVIEW_INCOMPLETE`;
- bot success on the current head completed before the ready-for-review event → `CI_REVIEW_INCOMPLETE`;
- old `committedDate` with a check suite created one minute before the wait, and the bot pending two minutes later → the bot is waited for;
- head OID changes mid-wait → earlier bot completion discarded;
- bot check concluding `SKIPPED`, `NEUTRAL`, `FAILURE`, or `CANCELLED` with no fresh head review → `CI_REVIEW_INCOMPLETE`, and the report shows the conclusion;
- failed check only on the second page of check contexts → `CI_FAILED`;
- actionable thread only on the second page of threads → `CI_COMMENTS`;
- thread whose latest reply is past the first comment page → deferral decided from the true latest reply;
- continuation page failing until the deadline → `CI_PENDING`;
- configured bot never starts → `CI_REVIEW_INCOMPLETE` after the grace and before the deadline;
- a bot that reviewed an earlier head, with no fresh evidence → `CI_REVIEW_INCOMPLETE`;
- a bot pending then success → `CI_PASSED`;
- no expected bots → `CI_PASSED` right after the grace;
- a pending review-bot status is not `CI_PENDING`;
- a hung `gh` → the wait finishes within deadline + margin;
- the comment read always failing → `CI_PENDING`;
- no PR → exit non-zero with empty stdout;
- authentication failure → exit non-zero;
- `gh` missing → exit 2.

Workflow-shape tests load the embedded `core:finalize-pr` and assert:
- `wait-ci` and `verify-final` are script steps with the right captures;
- there are no agent steps other than `push-pr` and `fix-pr`;
- the `fix-pr` prompt contains `{{ci_report}}`;
- the `review_bots` param is optional.

Existing gate tests stay unchanged. `make test` and `make lint` must pass.

## Migration Plan

This is a builtin workflow edit in place (`finalize-pr-v1.0.yaml`), because callers reference that exact version, and the change keeps the params and markers compatible. Runs resumed mid-`ci-fix-loop` from an older binary resume at step granularity. A resumed `wait-ci` simply runs as a script. Rollback is a revert of the YAML and the new assets.

## Open Questions

None.
