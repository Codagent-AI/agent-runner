## ADDED Requirements

### Requirement: CI waiting runs outside agent sessions

The `core:finalize-pr` workflow SHALL wait for pull-request CI and review state in non-agent workflow steps. This applies both to the per-cycle wait inside `ci-fix-loop` and to the final verification after the loop. No agent session SHALL be started or resumed to poll CI, fetch check results, or gather review comments. When CI passes on the first wait, no agent turn SHALL occur between the end of `push-pr` and the end of `core:finalize-pr`.

#### Scenario: Passing run has no agent turns after push
- **WHEN** `core:finalize-pr` runs, `push-pr` completes, and the first CI wait reports `CI_PASSED`
- **THEN** the workflow runs the CI wait, the loop gates, the final CI wait, and the final gates without starting or resuming any agent session, and finishes successfully

#### Scenario: Incomplete bot review needs no agent turn
- **WHEN** the first CI wait and the final CI wait both report `CI_REVIEW_INCOMPLETE`
- **THEN** no agent session is started or resumed after `push-pr`, and the workflow finishes with the existing incomplete-review warning

#### Scenario: Final verification is not an agent step
- **WHEN** `ci-fix-loop` ends, whether by a passing gate or an exhausted budget
- **THEN** the final CI status is produced by a non-agent step and captured as `final_ci_report`, and no agent is resumed to produce it

### Requirement: CI report and terminal marker contract

Each CI wait SHALL capture a human-readable report: `ci_report` for the in-loop wait and `final_ci_report` for the final wait. The final non-empty line of every report the wait produces SHALL be exactly one of `CI_PASSED`, `CI_FAILED`, `CI_COMMENTS`, `CI_PENDING`, or `CI_REVIEW_INCOMPLETE`, with nothing after it. When more than one condition applies, the marker SHALL follow this precedence: `CI_FAILED`, `CI_COMMENTS`, `CI_PENDING`, `CI_REVIEW_INCOMPLETE`, `CI_PASSED`.

Above the marker, the report SHALL include the PR URL and the determined status, plus each of these sections when it is non-empty:
- failed checks, with links and the last 100 lines of failed GitHub Actions job logs where available;
- merge conflicts, with conflicting paths where available;
- blocking reviews;
- actionable unresolved review comments;
- deferred threads;
- informational bot comments;
- still-running checks;
- unfinished review bots.

The existing gate scripts `ci-status-gate.sh`, `ci-fix-needed-gate.sh`, and `ci-review-incomplete-gate.sh` SHALL interpret these reports with their current behavior.

#### Scenario: Failed check report
- **WHEN** a required check fails on the PR head
- **THEN** the report lists the failed check with its link and failed-job log excerpt, and its final non-empty line is `CI_FAILED`

#### Scenario: Actionable feedback outranks pending automation
- **WHEN** an actionable unresolved review thread exists while a check is still running
- **THEN** the report lists both the thread and the still-running check, and its final non-empty line is `CI_COMMENTS`

#### Scenario: Gates read the script report unchanged
- **WHEN** the in-loop CI wait captures a report ending in `CI_PASSED`
- **THEN** `ci-status-gate` succeeds and the loop breaks, exactly as it does for an agent-produced report with the same marker

### Requirement: Check polling and merge state

The CI wait SHALL poll the PR's checks for the current head commit until every check reaches a terminal state or the overall deadline passes. The overall deadline SHALL default to 15 minutes, measured from the start of the wait, and SHALL bound all polling, including review-bot waiting. Classification SHALL follow these rules:
- A failed check SHALL produce `CI_FAILED`.
- A `CONFLICTING` merge state SHALL produce `CI_FAILED`, even when every check is green.
- While mergeability is `UNKNOWN`, the wait SHALL keep polling and SHALL NOT report `CI_PASSED`.
- If checks are still running at the deadline and no failure or actionable feedback exists, the result SHALL be `CI_PENDING`.
- If no checks were observed by the deadline, CI SHALL be treated as non-blocking, and the final status SHALL depend on review, comment, and mergeability evidence.

#### Scenario: Checks finish green
- **WHEN** all checks on the PR head reach a passing state before the deadline, mergeability is `MERGEABLE`, and no review condition applies
- **THEN** the wait stops polling checks without waiting for the deadline and reports `CI_PASSED`

#### Scenario: Merge conflict fails despite green checks
- **WHEN** all checks pass but the PR's mergeability is `CONFLICTING`
- **THEN** the report lists the conflict and ends with `CI_FAILED`

#### Scenario: Checks still running at deadline
- **WHEN** a check is still running when the overall deadline passes and no failed check, blocking review, or actionable comment exists
- **THEN** the report lists the running check and ends with `CI_PENDING`

#### Scenario: No checks configured
- **WHEN** no checks are ever observed before the deadline, mergeability is `MERGEABLE`, and no comment or review-bot condition applies
- **THEN** the report ends with `CI_PASSED`

#### Scenario: Unknown mergeability at deadline
- **WHEN** all checks pass but mergeability is still `UNKNOWN` at the deadline
- **THEN** the report ends with `CI_PENDING`

### Requirement: Review feedback classification

The CI wait SHALL classify review feedback deterministically:
- **Blocking review:** a reviewer whose latest review state is `CHANGES_REQUESTED`. It SHALL produce `CI_FAILED`.
- **Actionable:** an unresolved review thread that is not deferred, or a top-level comment from a human other than the PR author that is newer than the latest observable push and has not been followed by a top-level author reply. It SHALL produce `CI_COMMENTS` unless `CI_FAILED` applies. If no push timestamp or comment timestamp is available, the comment SHALL remain actionable unless a later author reply can be established.
- **Deferred thread:** an unresolved thread where the latest significant comment is from the PR author or from a bot other than the one that raised the finding, after trailing acknowledgments from the reviewing bot are ignored. A deferred thread SHALL be listed and SHALL NOT be actionable. A later human reply SHALL make the thread actionable again.
- **Informational:** top-level bot summaries, rate-limit notices, "draft not reviewed" notices, and resolved threads. These SHALL NOT be actionable.

#### Scenario: Changes requested blocks
- **WHEN** a reviewer's latest review on the PR is `CHANGES_REQUESTED` and CI is green
- **THEN** the report lists the blocking review and ends with `CI_FAILED`

#### Scenario: Only deferred threads remain
- **WHEN** CI is green, no review bot is unfinished, and the only unresolved threads are deferred
- **THEN** the report lists the deferred threads under their own heading and ends with `CI_PASSED`

#### Scenario: Human reply re-opens deferred thread
- **WHEN** a thread was deferred by the PR author and a human reviewer later replies in it without resolving it
- **THEN** the thread is listed as actionable and the report ends with `CI_COMMENTS`

#### Scenario: Bot summary is informational
- **WHEN** CI is green and the only bot output is a top-level review summary with no unresolved threads
- **THEN** the summary is listed as informational and the report ends with `CI_PASSED`

#### Scenario: New push addresses earlier top-level feedback
- **WHEN** a human top-level comment predates the current head's observable push and there is no newer feedback
- **THEN** that comment does not keep the new head in `CI_COMMENTS`

#### Scenario: Author reply addresses top-level feedback
- **WHEN** the PR author posts a top-level reply after a human top-level comment and there is no newer feedback
- **THEN** that earlier comment does not keep the PR in `CI_COMMENTS`

### Requirement: Expected review bots and freshness

`core:finalize-pr` SHALL accept an optional `review_bots` param: comma-separated bot logins, empty by default. Surrounding whitespace, letter case, and a trailing `[bot]` suffix SHALL be ignored when matching. Callers that do not pass it SHALL keep working.

A bot SHALL be expected when it is listed in `review_bots`, or when it has submitted a pull-request review on the PR for any head. A bot that has only posted top-level comments or check results SHALL NOT become expected that way.

Checks and commit statuses posted by an expected bot SHALL count as review-bot evidence, not as CI checks. A pending review-bot check SHALL therefore never produce `CI_PENDING`.

Freshness and bot state:
- The observable push point of the current head SHALL be the earliest check-suite creation time on the head commit, or the time of a force-push event that set the head, whichever is later. The commit's own commit time SHALL NOT be used, because an old commit can be pushed long after it was made. If no push timestamp is observable, the wait's first observation of the head SHALL bound the start grace but SHALL NOT invalidate existing current-head bot evidence.
- The freshness point SHALL be the later of the observable push point, when available, and the PR's most recent ready-for-review transition.
- Bot evidence SHALL be fresh only when it is dated after the freshness point. This applies to evidence tied to the current head commit as well. A check or status uses its completion or creation time, a review its submission time, and a top-level comment its last update time. A check, status, or review on the current head completed before a later ready-for-review transition SHALL be stale.
- Stale evidence SHALL NOT count as the bot having started or finished, and SHALL NOT satisfy a pass.
- An expected bot SHALL be finished only when its latest fresh head-tied progress evidence is positive completion: a check run on the current head with conclusion `SUCCESS`, a commit status on the current head with state `SUCCESS`, or a review submitted on the current head commit. A later pending or non-success check or status SHALL supersede an earlier success. A top-level comment SHALL NOT supersede head-tied progress evidence.
- Terminal check or status outcomes other than success SHALL NOT count as finished. This includes skipped, neutral, failure, cancelled, timed-out, action-required, stale, and error outcomes. A later fresh success from the same bot SHALL override them.
- The text of bot comments SHALL NOT be interpreted to decide whether a bot has started or finished.
- If the PR head changes during the wait, the wait SHALL recompute the push point, the freshness point, and the start grace for the new head, and SHALL discard bot state from the previous head.

Start grace and outcomes:
- The start grace SHALL end 3 minutes after the freshness point, or 3 minutes after the wait started, whichever is later. It SHALL stay within the overall deadline.
- An expected bot with no fresh evidence by the end of the start grace SHALL be treated as not started. The wait SHALL NOT keep polling for it past that point.
- An expected bot with fresh evidence that is not finished SHALL be waited for until it finishes or the overall deadline passes.
- If any expected bot is not finished when the wait ends, and CI is otherwise green with no actionable feedback, the result SHALL be `CI_REVIEW_INCOMPLETE`. This covers bots that never start, are rate-limited, are skipped, fail, or run out of time.
- When no bot is expected, the wait SHALL still allow the start grace before reporting `CI_PASSED`. A bot that posts a pending check or status on the current head during that time SHALL be expected for the rest of the wait.

#### Scenario: Stale bot pass does not count
- **WHEN** an expected bot's only terminal status is on an earlier head, CI on the current head is green, and the bot shows no fresh evidence before the start grace ends
- **THEN** the report lists the bot as unfinished and ends with `CI_REVIEW_INCOMPLETE`

#### Scenario: Configured bot never starts
- **WHEN** `review_bots` lists `coderabbitai`, CI is green, and no fresh evidence from `coderabbitai` appears within 3 minutes of the freshness point
- **THEN** the report ends with `CI_REVIEW_INCOMPLETE` without waiting for the overall deadline

#### Scenario: Previously reviewing bot becomes expected
- **WHEN** `review_bots` is empty, a bot submitted a review on an earlier head of the PR, and CI on the new head is green with no fresh evidence from that bot during the start grace
- **THEN** the bot is treated as expected and the report ends with `CI_REVIEW_INCOMPLETE`

#### Scenario: Comment-only bot is not expected
- **WHEN** `review_bots` is empty, a coverage bot has only posted top-level comments on the PR, and CI is green with no other bot activity
- **THEN** the coverage bot's comments are listed as informational and the report ends with `CI_PASSED`

#### Scenario: Bot finishes review within the wait
- **WHEN** an expected bot posts a pending status on the current head and later sets it to a terminal state before the deadline, with no actionable feedback
- **THEN** the report ends with `CI_PASSED`

#### Scenario: Current-head success without a push timestamp
- **WHEN** no check suite or matching force-push event establishes the current head's push time, but an expected bot has a successful status on that head after the most recent ready transition
- **THEN** the status counts as completed review even if it predates the wait process

#### Scenario: Bot starts another review after success
- **WHEN** an expected bot has a successful status on the current head and a newer pending status on that head
- **THEN** the earlier success does not complete the newer review, and the wait reports `CI_REVIEW_INCOMPLETE` if it remains pending at the deadline

#### Scenario: Pending review-bot status is not pending CI
- **WHEN** every CI check passes and an expected bot's status on the current head is still pending at the deadline
- **THEN** the report lists the bot as unfinished and ends with `CI_REVIEW_INCOMPLETE`, not `CI_PENDING`

#### Scenario: Repository without review bots
- **WHEN** `review_bots` is empty, no bot has reviewed the PR, CI is green, and no bot posts a pending check or status on the current head during the start grace
- **THEN** the report ends with `CI_PASSED` once the start grace ends, without waiting for the overall deadline

#### Scenario: First-time bot detected during grace
- **WHEN** `review_bots` is empty, no bot has reviewed the PR, and a bot posts a pending status on the current head during the start grace
- **THEN** the wait keeps polling for that bot, and reports `CI_REVIEW_INCOMPLETE` if the status is still pending at the deadline

#### Scenario: Review completed while draft is stale after ready
- **WHEN** an expected bot set a success status on the current head while the PR was a draft, the PR was then marked ready for review, and the bot shows no newer evidence during the start grace
- **THEN** the pre-ready status does not count as finished, and the report ends with `CI_REVIEW_INCOMPLETE`

#### Scenario: Old commit pushed just before the wait
- **WHEN** the head commit was made hours ago but its first check suite was created one minute before the wait started, and an expected bot posts a pending status two minutes later
- **THEN** the start grace runs from the check-suite time, the bot is observed as started, and the wait keeps polling for it

#### Scenario: Skipped or failed bot check is not a completed review
- **WHEN** CI is green and an expected bot's only fresh check on the current head concludes as skipped, neutral, failure, or cancelled, with no fresh review on the current head
- **THEN** the report lists the bot as unfinished with its conclusion, and ends with `CI_REVIEW_INCOMPLETE`

#### Scenario: Head changes during the wait
- **WHEN** an expected bot finished on the first head observed, and a new commit is pushed while the wait is still polling
- **THEN** the bot's earlier completion is discarded, and the result depends only on evidence for the new head after its push

#### Scenario: Bot actionable feedback during review
- **WHEN** an expected bot is still reviewing at the deadline and has already left an actionable unresolved thread
- **THEN** the report lists the unfinished bot and the thread, and ends with `CI_COMMENTS`

### Requirement: Complete evidence before passing

The CI wait SHALL read every page of the PR's check contexts on the current head, reviews, review threads, comments within review threads, and top-level comments before it reports `CI_PASSED` or `CI_REVIEW_INCOMPLETE`. If any of these collections is still incompletely read when the wait ends, that SHALL count as a pending condition. The result SHALL then be `CI_PENDING`, unless evidence already read supports `CI_FAILED` or `CI_COMMENTS`, which take precedence.

#### Scenario: Failure on a later page
- **WHEN** the PR's head has more check contexts than fit in one page, and the only failed check is on the second page
- **THEN** the report lists that failed check and ends with `CI_FAILED`

#### Scenario: Actionable thread on a later page
- **WHEN** CI is green and the only actionable unresolved review thread is on the second page of review threads
- **THEN** the report lists that thread and ends with `CI_COMMENTS`

#### Scenario: Pages still unread at deadline
- **WHEN** CI checks read so far are green, and fetching a later page of review threads keeps failing until the deadline
- **THEN** the report ends with `CI_PENDING`

### Requirement: Bounded external calls and error handling

Every call the CI wait makes to GitHub SHALL be bounded by a timeout that does not exceed the time left before the overall deadline. The wait SHALL therefore finish within the overall deadline plus a small fixed margin, even when a call hangs. A timed-out or transiently failing call SHALL be retried while time remains. When the deadline passes:
- If the check state was never read successfully, the result SHALL be `CI_PENDING`.
- If the checks were read but comment or review data could not be read, the result SHALL be `CI_PENDING`. It SHALL NOT be `CI_PASSED` or `CI_REVIEW_INCOMPLETE`.

If there is no pull request for the current branch, if GitHub authentication fails, or if a required tool is unavailable, the wait SHALL exit with a non-zero status and a diagnostic, and SHALL NOT produce a terminal marker. Such a failure SHALL NOT abort `core:finalize-pr` by itself; it SHALL flow through the existing unknown-marker gate behavior.

#### Scenario: Hung GitHub call is bounded
- **WHEN** a GitHub call made by the CI wait blocks indefinitely
- **THEN** the call is abandoned by its timeout, and the wait finishes within the overall deadline plus the fixed margin

#### Scenario: Comments unreadable at deadline
- **WHEN** all checks pass but every attempt to read review comments fails until the deadline
- **THEN** the report ends with `CI_PENDING`

#### Scenario: No pull request
- **WHEN** the CI wait runs on a branch that has no open pull request
- **THEN** the step fails with a diagnostic naming the missing pull request, `ci-status-gate` treats the missing marker as not passed, and no fix cycle runs for that iteration

#### Scenario: Authentication failure in final verification
- **WHEN** the final CI wait fails because GitHub authentication fails
- **THEN** `final-ci-status-gate` fails and `core:finalize-pr` finishes as failed

### Requirement: Lead session resumed only for fix cycles

`core:finalize-pr` SHALL resume the `lead-agent` session only in the `fix-pr` step, and only when `ci-fix-needed-gate` reports that a fix is needed (`CI_FAILED` or `CI_COMMENTS`). The `fix-pr` prompt SHALL include the full captured `ci_report` from that cycle and SHALL direct the agent to start from that report. The lead session SHALL be resumed at most once per fix cycle. The existing scope boundary SHALL remain in the `fix-pr` prompt: fixes that would change approved requirements, design, or scope are not made silently.

#### Scenario: Failing cycle resumes lead once with report
- **WHEN** the in-loop CI wait reports `CI_FAILED` with a failed check and log excerpt
- **THEN** `fix-pr` resumes the `lead-agent` session exactly once for that cycle, and its prompt contains that report, including the failed check and log excerpt

#### Scenario: Pending cycle does not resume lead
- **WHEN** the in-loop CI wait reports `CI_PENDING`
- **THEN** `fix-pr` is skipped for that cycle and no agent session is resumed

### Requirement: Fix-cycle budget and fall-through preserved

`ci-fix-loop` SHALL keep its current budget semantics:
- It runs at most `ci_fix_cycles` iterations, default `3`.
- It breaks early when `ci-status-gate` passes.
- When the budget is exhausted, it falls through to final verification instead of failing the workflow.

The final gates SHALL keep their current behavior:
- `final-ci-status-gate` fails the workflow unless the final report ends with `CI_PASSED` or `CI_REVIEW_INCOMPLETE`.
- `final-review-incomplete-gate` finishes the workflow with a warning when the final report ends with `CI_REVIEW_INCOMPLETE`.

#### Scenario: Exhausted budget still verifies final state
- **WHEN** every one of the `ci_fix_cycles` iterations reports `CI_FAILED` and runs `fix-pr`
- **THEN** the loop ends without failing the workflow, the final CI wait runs, and the workflow result follows the final report's marker

#### Scenario: Fix makes CI green
- **WHEN** the first cycle reports `CI_COMMENTS`, `fix-pr` pushes a fix, and the second cycle's wait reports `CI_PASSED`
- **THEN** the loop breaks after the second wait, the final CI wait reports `CI_PASSED`, and the workflow succeeds
