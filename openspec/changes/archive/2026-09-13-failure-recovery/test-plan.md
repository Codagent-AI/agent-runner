## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional integration, end-to-end, agent-acceptance, and exceptional human-only obligations.

Unit tests (not inventoried here) cover `Repair` validation and scope rules, `ResolveResumeStep` with each frame phase, `repairBlocked` marker detection, `ClassifyFailure` string rendering, prefix round-trips with the `attempt:N` token, and row-label suffixes. Use TDD for each; extend the nearest existing test file rather than adding parallel suites.

Existing harnesses to reuse: `cmd/agent-runner/smoke_headless_integration_test.go` (builds the binary, fakes agent CLIs on `PATH`, reads the run directory), the real-Git script tests in `workflows/` (`verify_task_commit_test.go` pattern, `ReadAsset` for scripts), `internal/runview/historical_integration_test.go` for audit-to-view projection, and the exec-level fake `ProcessRunner`. Real model calls are authorized only for AT-002, with the `implementor` profile against a scratch local repository with no remote. No real GitHub remote, personal token, or external publication is authorized.

Acceptance is exploratory: the tester reads the diff to decide where to look, predicts each result before running it, and reports what it observed. The `AT-*` entries below are the required starting seams, not an exhaustive inventory.

## Integration Tests

### INT-001: Repair cycle across the three sequencers
- Covers: `step-repair` (cycle, blocked declaration, exhaustion, flow-control precedence), `audit-log-entries` repair events, `recursive-state` frame writes.
- Boundary: `exec.ExecuteCheckStep`, the rewind handling in the runner top-level loop, `executeIterationBody`, `ExecuteSubWorkflowStep`, and `executeGroupStep`, the fake `ProcessRunner` for checks, a fake `claude` executable on `PATH` for agent steps (as in the headless smoke test), and real `state.json` writes through `FlushState`.
- Setup: table of fixture workflows placing a check with `repair` (a) at top level, (b) inside a counted loop body, (c) inside a sub-workflow, (d) inside a group, each in inline and rerun form; an inline variant on `session: resume` with a shell step, a sub-workflow that itself runs an agent step with a distinguishable response and session, and a skipped step between the outer agent and the check; a group variant with an agent before the group, an agent plus a failing check inside it, and a failing check after it; a variant whose scope has no prior agent; a rerun variant whose range has three steps with `capture` on the target, an intermediate step, and the check. The fake process runner is scripted per invocation (fail, fail, pass); the fake CLI records its stdin prompt and prints a canned response, optionally ending in `REPAIR_BLOCKED`, with a variant that prints the marker only in raw tool noise the adapter filters out.
- Action: run each fixture to a terminal outcome.
- Assertions: the check emits exactly one `step_start` and one `step_end`; audit sequence is `repair_attempt_start`, agent events under `[check, attempt:N, repair]` or replayed steps and the replayed check under `[check, attempt:N, ...]`, `repair_attempt_end`, then the terminal `step_end` with `repair_attempts`, `repair_blocked`, and `guarded_prefix`; the marker is honored only from the adapter-filtered final response; range-owned captures are cleared at rewind and captures outside the range survive; a `warn_on_failure` check adds exactly one warning origin, at the terminal outcome, and none when repaired; direct executor use with nil `FlushState` does not panic; `--until` naming the check stops after its terminal outcome, not after an internal run; the guarded identity and evidence are exactly the outer agent's execution (prefix and attempt), never the sub-workflow's agent; the group cases pick the group's agent inside and the pre-group agent after; the no-prior-agent case records no guarded execution; the inline repair on `session: resume` invokes the fake CLI with the outer agent's exact session ID; the state file shows the frame with the right phase after each flush; the step after the check sees `previous_success` only from the terminal result; a `break_if` on the check is evaluated once; an abort or blocking failure of a replayed step is absorbed as a failed attempt; the guarded response ending in `REPAIR_BLOCKED` stops the cycle before any repair; the inline prompt contains the evidence block and the untrusted notice; `capture` holds the final run's stdout.
- Execution: `go test ./internal/exec ./internal/runner -run Repair`.

### INT-002: Resume from every repair frame phase
- Covers: `recursive-state` resume requirements, `step-repair` persistence and resume.
- Boundary: `runner.ResumeWorkflow`, `restoreResumeContext`, `ResolveResumeStep`, real state files written by INT-001-style runs.
- Setup: produce state files by interrupting a run (fake runner returns an abort) at: inline repair in progress, replayed intermediate step in progress, check failed after exhaustion (rerun form), check failed after exhaustion (inline form), check failed as blocked, between the guarded agent step and the check, immediately after an exhausted `warn_on_failure` check inside a loop, and a nested loop → sub-workflow → check replay.
- Action: resume each with a fake runner scripted to succeed.
- Assertions: re-entry step is the inline agent for the same attempt, the replayed step, the rerun target, the check, the rerun target, the check (with the guarded response rebuilt from audit and present in the failure record), the step after the warning check, and the nested check respectively; attempt budget is fresh only after a terminal failure; a removed rerun target produces the descriptive error; loop iteration index is unchanged by the frame; the serialized frame sits at the owning scope's nesting level.
- Execution: `go test ./internal/runner -run ResumeRepair`.

### INT-003: Archive transition and verification against real Git
- Covers: the archive split in the design (transition idempotence, verification invariants, already-committed no-op, fail-safe on ambiguity), issue 65 acceptance criteria for archive.
- Boundary: `archive-transition.sh`, `verify-archive-commit.sh`, a real `git` repository, a fake `openspec` executable on `PATH` that performs the directory move, and a `commit-msg` hook.
- Setup: temp repo with an active change under `openspec/changes/<name>`, one canonical spec, an unrelated staged file outside the allowed paths, a pre-existing staged edit and a pre-existing unstaged edit inside `openspec/specs`, and a hook that rejects subjects longer than 60 characters or without a `TICKET-123:` prefix.
- Action and assertions:
  - first transition: JSON names exactly one archive dir and the starting `HEAD`; rerunning transition with the active dir gone returns the same JSON; two matching archive dirs or none exit non-zero without committing;
  - verify before any commit: exits non-zero, stderr names the missing commit;
  - after a compliant commit of exactly the owned delta: verify passes, the unrelated staged file and the pre-existing staged spec edit remain staged, the pre-existing unstaged spec edit remains in the worktree, `start_head..HEAD` touches nothing else;
  - after a commit that also includes the pre-existing staged spec edit: verify fails naming the path;
  - after history is rewritten so `start_head` is not an ancestor of `HEAD`: verify fails;
  - after a commit that includes the unrelated file: verify fails naming the path;
  - rerunning verify after success: exits 0 with no new commit;
  - a commit attempt with a non-compliant subject is rejected by the hook and verify still fails, so the repair path is reached.
- Execution: `go test ./workflows -run Archive`.

### INT-004: Replay-safe change-plan commit
- Covers: `commit-change-plan.sh` replay safety in the design.
- Boundary: the script against a real Git repository.
- Setup: temp repo with an uncommitted change directory.
- Action: run once, then again with nothing left to commit, then with an unrelated dirty file.
- Assertions: first run commits only the change directory; second run exits 0 with an "already committed" message and no new commit; unrelated file is neither committed nor blocks the no-op.
- Execution: `go test ./workflows -run CommitChangePlan`.

### INT-005: Audit to run view and run list projection
- Covers: `view-run`, `list-runs`, `live-run-view` deltas, `run-failure-evidence` reconstruction.
- Boundary: audit log parsing (`parsePrefix`, `Tree.ApplyEvent`), selected-detail rendering, list-row rendering, `state.json` `failureReason`.
- Setup: recorded audit fixtures for five runs: repaired inline, exhausted inline, blocked rerun, in-progress rerun (live), and a plain failed check that passed after resume; plus one pre-change audit log with no repair events.
- Action: build the tree and render the sidebar, selected detail for the check, an attempt row, and a repair agent row, and the list row.
- Assertions: suffixes `(repaired 1/1)`, `(2/2)`, `(blocked)`, `(repairing 1/1)`; attempt children with the `⟳` glyph and `rerun 1` container; `Failure evidence` section shows the classified reason and the guarded response; list row shows the truncated reason; the legend lists the repair-attempt glyph; the resumed run shows the earlier failed execution through the previous-execution context with its `Failure evidence`; the pre-change log renders exactly as before.
- Execution: `go test ./internal/runview ./internal/listview -run Repair`.

## End-to-End Tests

### E2E-001: Blocked draft-PR shape, then resume rewinds to the action
- Covers: `step-repair` blocked declaration and resume re-entry, `run-failure-evidence` console and state output, `recursive-state` rewind on resume.
- Surface: the built `agent-runner` binary run headless with `--resume`.
- Setup: isolated `HOME` and project as in the headless smoke test; a fixture workflow with an autonomous agent step `act`, then a shell step and a sub-workflow step that runs its own agent step (the fake CLI returns a distinguishable response for it), then a shell check `verify` with `repair: {rerun: act}` (the intervening agent proves the nearest-agent-in-scope rule); the fake `claude` CLI prints a diagnosis ending in `REPAIR_BLOCKED` on its first invocation and, when a marker file exists, creates the file the check looks for.
- Journey: run the workflow; observe failure; create the marker file; resume.
- Assertions: first run exits non-zero, prints `verify failed: … blocked: …` above the `to resume:` line with the blocked text taken from `act`'s response and not the sub-workflow agent's, `state.json` carries the same `failureReason`, audit shows no `repair_attempt_start`; resume runs `act` again with the evidence preface in its prompt (visible in the fake CLI's recorded stdin), then `verify` passes and the run completes with the state marked completed.
- Execution: `go test ./cmd/agent-runner -run RepairBlockedE2E` alongside the existing headless smoke test.

### E2E-002: Inline repair recovers a check
- Covers: `step-repair` inline cycle end to end, `step-flow-control` recovered success.
- Surface: the built binary, headless.
- Setup: fixture workflow with a check that requires a file, `repair: {agent: implementor, prompt: create it}` and `max: 1`; the fake CLI creates the file.
- Journey: run once.
- Assertions: exit 0; audit has one `repair_attempt_start`, agent events under `[check, attempt:1, repair]`, one `repair_attempt_end` with exit 0, and `step_end` outcome `success`; the run is not marked as having warnings.
- Execution: same package and command as E2E-001.

## Agent Acceptance Tests

### AT-001: Inspect repaired and blocked runs in the TUI
- Classification: Required.
- Covers: `view-run`, `list-runs`, `live-run-view` deltas as a user sees them.
- Actor and surface: a developer using `./dev.sh` in the run list and run view under a PTY (pyte reconstruction, per CLAUDE.md).
- Setup: the run directories produced by E2E-001 and E2E-002 (or equivalent fixture workflows run through the fake CLI).
- Steps: open the run list, confirm the failed run row shows the reason; open the blocked run, select the check, read the right pane; open the repaired run, select the check, expand and select `attempt 1` and `repair 1`; open `?`; run a fixture live and watch the check row while the repair agent runs.
- Expected: matches the mockups approved in the spec step: suffixes on the check row, attempt children, `Failure evidence` first in the pane, legend entry, live `(repairing 1/1)` with the cursor following into the attempt.
- Evidence: reconstructed screen captures of each state, saved under the run's output directory.
- Effects and cleanup: none beyond temporary run directories.
- Permitted substitutes: None.

### AT-002: Real agent repairs an archive commit under a rejecting hook
- Classification: Required.
- Covers: the motivating issue-65 archive case end to end with a real repair agent: archive transition, agent-driven commit under repository conventions, deterministic verification.
- Actor and surface: a developer running `./dev.sh openspec:simple-change` (or the archive sub-workflow directly with `change_name`) against a scratch local repository.
- Setup: a throwaway Git repository initialized with an OpenSpec layout and a completed active change, a `commit-msg` hook enforcing `TICKET-123: <Capitalized subject>` under 60 characters, the `implementor` profile with real model access, no remote.
- Steps: run the archive sub-workflow; observe the first commit attempt rejected by the hook; let the repair agent commit; let verification pass.
- Expected: exactly one archive directory, active directory removed, canonical specs committed, commit subject accepted by the hook, unrelated staged files untouched, run completes; the run view shows `verify-archive-commit (repaired 1/1)` with the agent's response under `repair 1`.
- Evidence: the run's audit log, `git log --stat` of the scratch repo, and a run-view capture.
- Effects and cleanup: real model calls with the implementor profile (modest cost); delete the scratch repository afterward.
- Permitted substitutes: None. If the model is unavailable, acceptance is incomplete.

### AT-003: Task delivered outside the repository is reported, not faked
- Classification: Required.
- Covers: `core:implement-task` `verify-task-commit` repair, both branches, with a real implementor session.
- Actor and surface: a developer running `core:implement-task` through `./dev.sh` against a scratch repository, twice.
- Setup: scratch repo with a trivial task file; run 1 uses a task whose deliverable is a file in this repo; run 2 uses a task that states its deliverable lives in a sibling directory outside the repo. An unrelated file is left staged in both runs.
- Steps: run 1: let the implementor implement but instruct it (via the task) not to commit; observe the check fail and the repair commit. Run 2: let the implementor deliver to the sibling directory; observe the check fail and the repair declare blocked.
- Expected: run 1 commits only the task's files, the unrelated staged file stays staged, the run continues. Run 2 makes no commit, the run fails with a reason naming the sibling location, and the run view shows the implementor's explanation under `Failure evidence`.
- Evidence: audit logs, `git log --stat` and `git status` of the scratch repo, run-view capture for run 2.
- Effects and cleanup: real model calls with the implementor profile; delete the scratch repo.
- Permitted substitutes: None.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | AT | HT |
| --- | --- | --- | --- | --- |
| step-repair: repair cycle, inline and rerun, in all four sequencers | INT-001 | E2E-002 | — | — |
| step-repair: replay clears range-owned captures | INT-001 | — | — | — |
| step-repair: blocked declaration stops repair | INT-001 | E2E-001 | — | — |
| step-repair: exhaustion and flow-control precedence | INT-001 | — | — | — |
| step-repair / recursive-state: persistence and resume re-entry, frame lifecycle | INT-002 | E2E-001 | — | — |
| run-failure-evidence: guarded identity persisted, rebuilt from audit | INT-002 | — | — | — |
| run-failure-evidence: nearest agent in scope across intervening steps, sub-workflow and group boundaries | INT-001 | E2E-001 | — | — |
| implement-task verify-task-commit repair: local commit branch and blocked branch | INT-001 | — | AT-002 | — |
| run-failure-evidence: earlier record survives resume | INT-005 | — | — | — |
| run-failure-evidence: reason on console, state, view | INT-005 | E2E-001 | AT-001 | — |
| view-run / list-runs / live-run-view: attempts, suffixes, evidence, legend | INT-005 | — | AT-001 | — |
| audit-log-entries: repair events and linkage | INT-001 | E2E-002 | — | — |
| Archive split: idempotent transition and verification invariants | INT-003 | — | AT-002 | — |
| Archive commit repaired under repository hook (issue 65) | INT-003 | — | AT-002 | — |
| commit-change-plan replay safety | INT-004 | — | — | — |
