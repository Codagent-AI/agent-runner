## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only obligations.

Most of the risk is in wiring: whether the loop's steps, sessions, captures, and `skip_if`/`break_if` gates in the embedded `core/implement-change-v2.0.yaml` actually produce the specified outcomes. The bundled scripts also have to agree with `check-evidence.sh` and with git. The integration tests therefore load the real embedded workflow and run the real bundled scripts against a temporary git repository. They fake only the agents. This follows the existing harnesses in `internal/exec/verify_change_workflow_test.go` and `internal/exec/implement_task_workflow_test.go`.

Three end-to-end tests drive the built binary with a fake agent CLI, as `cmd/agent-runner/repair_blocked_e2e_test.go` does. They prove what only a real process boundary shows:
- the CLI adapter's named-session resume across loop iterations;
- capturing the final response from real CLI output;
- budget state surviving a process restart;
- the labeled failure reason on the console and in `state.json`.

Prompt wording is not tested. Tests assert routing and data flow (which agent and session ran, and what data reached it), never instruction text.

## Integration Tests

### INT-001: Verdict paths through the embedded workflow
- Covers: Deterministic completion check; Blocked record stops before judgment; Independent completion judge (fresh session); Verdict outcomes (`continue`, `fix-record`, `complete`, `blocked`); builtin-workflows "Completion loop sits between implement and audit".
- Boundary: the loader's embedded `builtin:core/implement-change-v2.0.yaml`, then the loop executor, real bundled scripts (`check-implementation-complete.sh` calling `check-evidence.sh`, `check-implementation-progress.sh`, `check-completion-verdict.sh`, `check-not-blocked.sh`), and real git in a temporary repository. The agent processes are stubbed.
- Setup: a temporary git repository with a committed planned checklist and its planned copy, `implementation_start_head` recorded, and an evidence directory. A stub runner plays the agents by role. The implementor commits, edits rows, and writes or omits records. The judge writes a scripted `completion-verdict.json` and, for `fix-record`, edits the checklist and commits with the `[judge-completion]` prefix. The audit returns `No findings.` Execution starts at `implement`, or the loop runs as a sub-scope, whichever the harness supports.
- Action: one table-driven case per path:
  - (a) everything satisfied after `implement`;
  - (b) the record is missing, the judge returns `continue`, and the pass completes the change;
  - (c) citations lack backticks, the judge returns `fix-record` and fixes them;
  - (d) only `implementation-complete.md` is missing, and `fix-record` writes it;
  - (e) the only gap is a `not verified` row with a reason, and the judge returns `complete`;
  - (f) the judge returns `blocked` and writes the blocked record;
  - (g) the implementor writes `implementation-blocked.md` during `implement`;
  - (h) the only commit is the judge's `[judge-completion]` commit.
- Assertions:
  - The ordered step and agent events per case. For example, (a) runs no judge, (g) runs no completion check, and (b) resumes the implementor once.
  - The judge invocation uses the `tester` profile with a new session each time, never the `acceptance-tester` or `implementor-agent` session.
  - The `continue` invocation resumes the `implementor-agent` session, and its prompt input carries the judge's remaining-work list.
  - The workflow reaches `audit` in (a) through (e). It stops failed before `audit` in (f) and (g), with a failure reason naming the blocked record.
  - In (h), the completion check fails, naming the missing implementation commits.
  - A `fix-record` case uses no `continue` pass, which the ledger shows.
- Execution: `internal/exec` (for example a new `implement_change_v2_completion_loop_test.go`), under `make test`.

### INT-002: Loop bounds, stall, and verdict validation through the embedded workflow
- Covers: Loop bounds; Stall detection; Verdict validation.
- Boundary: the same as INT-001.
- Setup: the same harness, with the judge and implementor scripted across several iterations.
- Action:
  - (a) Three `continue` passes that each add a commit without finishing, then a fourth `continue` verdict.
  - (b) A third `continue` pass that completes the change.
  - (c) A `continue` pass with no commit and no row change while the check still fails.
  - (d) A `continue` pass that only writes `implementation-complete.md`, which makes the check pass.
  - (e) Three consecutive `fix-record` verdicts that do not satisfy the check.
  - (f) Invalid verdicts: a missing file, an unknown word, `continue` with an empty list, `complete` while the completion record is missing, and `blocked` with no blocked record.
  - (g) The longest accepted sequence, `fix-record ×2, continue` repeated three times, then `fix-record ×2`, followed by a terminal verdict.
- Assertions:
  - The run outcome and the first line of the classified failure reason: `implementation incomplete: …` with the list in the failure evidence; `implementation stalled: …`; `implementation record could not be fixed …`; and `completion judge returned no valid verdict: …`.
  - No fourth `continue` pass runs in (a).
  - (b) and (d) reach `audit`.
  - (c) runs no further judge.
  - (g) ends by a script or a break, never by loop exhaustion.
- Execution: `internal/exec`, under `make test`.

### INT-003: Completion scripts against git and the evidence check
- Covers: Deterministic completion check (each condition and the not-verified classification); Stall detection (snapshot comparison); Verdict validation and the resume-safe ledger.
- Boundary: the real bundled scripts run as subprocesses with JSON stdin, the sibling `check-evidence.sh` resolved through `$script_dir` from a materialized bundle, and real `git` and `jq`.
- Setup: temporary git repositories and checklist fixtures. A shared fixture set is run through both `check-evidence.sh` and `check-implementation-complete.sh`, so the two table parsers cannot drift. Fixtures include escaped `\|` cells, a `failed` row, a `not verified` row with and without a reason, and a planned-cell edit.
- Action: run each script per fixture. Rerun `check-implementation-progress.sh` and `check-completion-verdict.sh` with no new judgment in between, which is what a resume does.
- Assertions:
  - The exit codes, the first stderr lines, the contents of `completion-problems.txt` and `completion-check.json` (row IDs listed, and the classification), and the capture JSON shape `{"verdict","remaining"}`.
  - `evidence-problems.txt` is removed when the check passes.
  - A rerun does not add a ledger entry or count a verdict twice.
  - `[judge-completion]` commits are excluded from the commit count.
  - Both parsers agree on every shared fixture.
- Execution: `workflows/` (for example a new `completion_loop_scripts_test.go`), under `make test`.

### INT-004: `before repair` label survives audit reconstruction
- Covers: run-failure-evidence "Classified failure reason" (labeled cases, unlabeled cases, the same reason on every surface).
- Boundary: the repair executor writes a real audit log and `state.json`, then `internal/runview` rebuilds the failure from that log (`applyRepairStepEnd` and `classifiedFailureReason`), and `FailureReasonForSession` serves the debug path.
- Setup: small test workflows with a check that has (a) an inline repair agent declaring `REPAIR_BLOCKED`, (b) a `rerun` target that declares `REPAIR_BLOCKED` on its second attempt, (c) a last inline repair agent that is judged failed with failure kind `step` (a disallowed `AskUserQuestion` in the fake output), (d) a guarded action that blocks before any repair, (e) exhaustion after a final check run, (f) a check with empty stderr and exit 2 that is then blocked, and (g) a last inline repair agent that crashes (non-zero exit, `Selected model is at capacity`). A recorded audit-log fixture from before the change, with no `repair_output_before_repair` field, is also included.
- Action: execute each workflow to failure, then load its session directory through the run-view model and `FailureReasonForSession`.
- Assertions:
  - The reason persisted in state, in `run_end.failure_reason`, from the run view, and from `FailureReasonForSession` is the same exact string in each case.
  - (a), (b), (c), and (f) carry `before repair` in the specified position.
  - (d), (e), and the legacy fixture are unlabeled.
  - (g) renders `<check> repair failed (infrastructure): Selected model is at capacity after 1 repair attempts`, with no `before repair` label.
  - The flag survives an interrupt between the repair attempt and terminal failure, followed by resume.
- Execution: `internal/runview` historical integration tests together with `internal/exec` repair tests, under `make test`.

### INT-005: Resume boundaries for runs recorded on the earlier layout
- Covers: builtin-workflows "Run recorded in a removed step" and "Run recorded after implement under the earlier layout".
- Boundary: runner resume (`ResolveResumeStep` and the sub-workflow resume path) loading the updated embedded `core/implement-change-v2.0.yaml` from recorded nested state.
- Setup: state fixtures shaped like the earlier layout: (a) a failed `check-evidence` rerun frame whose target is `complete-evidence`; (b) `implement` recorded as completed, with no `implementor_response` capture and no completion record. Agents are stubbed and record any invocation.
- Action: resume each fixture.
- Assertions: (a) fails with an error naming the missing step `complete-evidence`, and no step runs. (b) runs the completion check, then fails at the judge step with a `step`-kind undefined-variable error naming `implementor_response`, and the stub records no judge invocation.
- Execution: `internal/runner` or `internal/exec` resume tests, under `make test`.

## End-to-End Tests

### E2E-001: An early-stopping implementor is resumed and finishes
- Covers: Completion record; Deterministic completion check; Independent completion judge; Verdict outcomes (`continue`); builtin-workflows layout. Journey: the run `change-2026-10-27T02-40-12-943756Z` failure mode.
- Surface: the built `agent-runner` binary, `run <core implement-change ref> --headless --until audit`, against the embedded workflow.
- Setup: an isolated `HOME` and project in a temporary git repository with a minimal valid change definition. Use a change kind whose definition checks need no unavailable external CLI; otherwise install or stub that CLI in the test's PATH. Run with `skip_validator=true`. A fake `claude` CLI on PATH plays every role profile, chosen by prompt markers. The checklist step writes a two-row checklist. `implement` commits one row's work and ends without a completion record, which is the early stop. The judge writes a `continue` verdict that lists the second row. The resumed implementor commits the rest, records evidence, and writes `implementation-complete.md`. The audit returns `No findings.`
- Journey: start the run and let it stop after `audit`.
- Assertions:
  - The process exits 0 with the `--until` stop message.
  - `audit.log` shows one judgment and one `continue` pass.
  - The fake CLI recorded the `continue` invocation as a resume of the same session ID as `implement`.
  - The judge invocation started a new session.
  - No `complete-evidence` step appears.
- Execution: `cmd/agent-runner` E2E tests (alongside `repair_blocked_e2e_test.go`), under `make test`.

### E2E-002: Budget and stall survive an interrupted run
- Covers: Loop bounds ("Budget survives resume"); Stall detection.
- Surface: the built binary, a headless run followed by `--resume <run-id>`.
- Setup: the same as E2E-001. The fake CLI makes the judge return `continue` every time. Two cases:
  - Case A (progress before the crash): implementor pass 1 commits. Pass 2 commits, then exits as a capacity crash (an infrastructure failure). The resumed invocation of pass 2 adds nothing. Pass 3 commits without finishing.
  - Case B (stalled across the crash): pass 1 commits. Pass 2 crashes before any commit or checklist change, and its resumed invocation also leaves HEAD and the checklist unchanged while the completion check still fails.
- Journey: in each case, run until the crash fails the run, resume it, and let it finish.
- Assertions:
  - In both cases the first run fails with a `failed (infrastructure)` reason during the second pass.
  - The `continue-snapshot.json` written before pass 2 survives the resume unchanged, and the ledger counts pass 2 exactly once.
  - Case A: pass 2 is not stalled, because its pre-crash commit counts toward the whole pass. After pass 3, the final judgment's `continue` fails the run with a reason containing `implementation incomplete`, and `state.json` and the console resume hint show the same reason. No fourth pass runs.
  - Case B: the run fails with a reason containing `implementation stalled` immediately after the resumed pass 2, and no further judge or pass runs.
  - The two cases end with different outcomes.
- Execution: `cmd/agent-runner` E2E tests, under `make test`.

### E2E-003: Labeled failure reason on the console and in state
- Covers: run-failure-evidence "Classified failure reason" (inline repair agent blocks; resume hint carries the reason).
- Surface: the built binary, running a project workflow headless.
- Setup: a project workflow with a check whose stderr is fixed text, and an inline repair whose fake agent ends with `REPAIR_BLOCKED`. This is a variant of `TestRepairBlockedE2E`.
- Journey: run to failure.
- Assertions:
  - The console line above `to resume:`, `state.json` `failureReason`, and `agent-runner debug --state <run-id>` all show `<check> failed: before repair: <stderr line>; blocked: <first line> after 1 repair attempts`.
  - The existing guarded-action-blocked E2E keeps its unlabeled reason.
- Execution: `cmd/agent-runner` E2E tests, under `make test`.

## Acceptance Testing Envelope

- **Environments and sandboxes:** the local macOS checkout run through `./dev.sh`, with scratch git repositories under the run's scratch directory and an isolated `HOME` for every run the pass starts. The terminal UI may be driven through a Python `pty.fork` and `pyte` harness, as described in `CLAUDE.md`, to inspect the run view and run list for the labeled failure reason and for the loop's steps. Starting runs from the TUI browser does not work under a synthetic PTY, so start runs from the command line.
- **Credentials and secrets:** the agent CLI logins already configured on this machine for the `implementor`, `tester`, `lead`, and `crosscheck` profiles. No GitHub token is needed, and none may be used for writes.
- **Authorized effects:**
  - Any number of runs with fake agent CLIs.
  - At most two real-agent runs of `core:implement-change` on a tiny scratch change (one or two checklist rows), each stopped with `--until audit` so that `verify-change` never opens a pull request. Expected cost is a few dollars of model usage per run.
  - Delete scratch repositories and isolated `HOME` directories afterwards.
- **Off limits:**
  - Pushing, opening or editing pull requests or issues, and any `gh` write.
  - Resuming or modifying any existing run under the real `~/.agent-runner/projects/`. The October 27 run `change-2026-10-27T02-40-12-943756Z` may be read as reference only.
  - Editing user settings, including `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE`.
  - Running Agent Validator against the real project.
- **Permitted substitutes:** forcing an early stop with a real model is unreliable. The pass may substitute a fake or scripted implementor for the early-stop, `fix-record`, `blocked`, budget, and stall paths, and keep real agents for at most the happy path and one `continue` path.
- **Known risk areas:**
  - The judge's reliability on the smaller factory `tester` default (`codex:gpt-6-luna:medium`). This is accepted, and the deterministic gates bound it.
  - Drift between the completion check's table parsing and `check-evidence.sh`.
  - Interpolating `implementor_response` and the remaining-work list into prompts (size, and treatment as untrusted).
  - Named-session resume of `implementor-agent` across loop iterations.
  - Accepted limitation: runs recorded inside the removed steps cannot resume, and resuming one fails with an error naming the missing step, for example `complete-evidence`. Old-layout runs recorded after `implement` completed fail at the judge with an undefined `implementor_response`. The pass may confirm this on a scratch run recorded against the old layout, never on a real one.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| Completion record | INT-001 | E2E-001 | — |
| Deterministic completion check | INT-001, INT-003 | E2E-001 | — |
| Blocked record stops before judgment | INT-001 | — | — |
| Independent completion judge | INT-001 | E2E-001 | — |
| Verdict validation | INT-002, INT-003 | — | — |
| Verdict outcomes | INT-001 | E2E-001 | — |
| Loop bounds | INT-002 | E2E-002 | — |
| Stall detection | INT-002, INT-003 | E2E-002 | — |
| Change v3 workflows implement through the audited workflow (loop layout, separate judge session, earlier-layout resume) | INT-001, INT-005 | E2E-001 | — |
| Classified failure reason (`before repair`) | INT-004 | E2E-003 | — |
| Early-stopping implementor journey (run `change-2026-10-27T02-40-12-943756Z`) | — | E2E-001 | — |
