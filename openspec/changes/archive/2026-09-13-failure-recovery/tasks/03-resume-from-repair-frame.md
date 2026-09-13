# Task: Resume from every repair frame phase

## Goal

Make an interrupted or failed repair cycle resumable. `--resume` restores the persisted repair frame before ordinary step resolution and re-enters at the right place: the inline repair agent's session for the same attempt, the replayed step, the check, or the `rerun` target. A run that failed at an exhausted or blocked check re-enters the cycle with a fresh budget so a human fix (granting a token scope, switching a model, fixing a defect) is picked up without hand-editing `state.json`. Prove the motivating blocked-draft-PR journey end to end.

## Background

Read `openspec/changes/failure-recovery/proposal.md` for the motivating runs (issue 65). The repair executor already exists: `exec.ExecuteCheckStep` runs the inline and rerun cycles, all four sequencers (`internal/runner/runner.go` `executeSteps`, `internal/exec/loop.go` `executeIterationBody`, `internal/exec/subworkflow.go` `ExecuteSubWorkflowStep`, `internal/exec/dispatch.go` `executeGroupStep`) handle `ctx.PendingRewind`, and every state writer copies `ctx.RepairFrame` into `NestedStepState.Repair` and the guarded identity into `NestedStepState.LastAgent`. `model.RepairFrame` has JSON fields `checkId`, `form` (`inline` | `rerun`), `target`, `phase` (`checking` | `repairing` | `replaying` | `failed`), `attempts`, `budget`, `guarded` (`{prefix, attempt}`), `rangeCaptures`. Verify these before building on them.

Today, resume is resolved by `model.ResolveResumeStep(steps, recordedStepID, completed)` in `internal/model/state.go`, which knows only a recorded step ID and a completed bit at each nesting level, and `restoreResumeContext` in `internal/runner/resume.go` rebuilds the root context from `RunState`; the loop and sub-workflow executors have their own resume paths that consume the nested `NestedStepState` chain.

Design from `openspec/changes/failure-recovery/design.md`:

**Frame restoration.** On resume, `restoreResumeContext` and the loop / sub-workflow resume paths restore the frame onto the context of the scope that owns it (the frame sits at the nesting level of the check's own scope, never promoted). Groups have no nested state entry: `executeGroupStep` in `internal/exec/dispatch.go` keeps no child position, and a resumed run re-executes an interrupted group from its first child today. A frame for a check inside a group therefore lives on the enclosing scope's entry (whose recorded step is the group) with `CheckID` naming the check. On resume, restore it onto that scope's context, resolve the recorded step as the group as today, and let the group run forward from its first child; when execution reaches the check, `ExecuteCheckStep` finds the open frame for its ID and continues the cycle with the recorded attempt count (or a fresh budget when the phase is `failed`). The frame must be matched by `CheckID`, not by position, so a frame for a group-nested check is never applied to a different step. Do not add group child-position persistence. The guarded identity in `LastAgent` is restored too; when the check needs the guarded response, `exec.LoadAgentExecution(sessionDir, ref)` rebuilds it from the audit log by exact prefix and attempt.

**`ResolveResumeStep` gains a third input, the frame:**

- frame open with `Phase == replaying` → resume at the recorded current step (a replayed step), frame kept, so the sequencer continues the replay and the replayed check re-enters the open cycle.
- frame open with `Phase == repairing` → resume at the check; `ExecuteCheckStep` sees the phase and resumes the inline agent session for the same attempt (the attempt context's session map records the session ID, flushed on discovery by `ExecuteAgentStep`) before rerunning the check. Completed attempts are not repeated.
- frame open with `Phase == checking` → resume at the check and continue the cycle with the recorded attempt count.
- frame `Phase == failed` with `Form == rerun` → resume at `frame.Target` with `Attempts = 0` (fresh budget), frame kept open in phase `replaying` so the target gets the evidence preface and the replayed check continues the cycle.
- frame `Phase == failed` with `Form == inline` → resume at the check with `Attempts = 0`.
- no frame → today's behavior.

A stale target (the recorded rerun target no longer exists in the workflow file) reuses the existing "step no longer exists" error path with the target named. Loop iteration index is never changed by a frame.

**Fresh budget only after a terminal failure.** A frame in `repairing`, `replaying`, or `checking` keeps its `Attempts`; only `failed` resets it.

**Earlier failure records survive.** Audit is append-only; the earlier failed `step_end` and its guarded execution's events remain. Resume must not delete or rewrite them, and the final state write after a successful resumed run keeps `failureReason` cleared (a completed run has no failure reason) while audit keeps the history.

**Where to look.** `internal/runner/resume.go` (`PrepareResume`, `restoreResumeContext`, `ResumeWorkflow`), `internal/model/state.go` (`ResolveResumeStep`), the resume handling inside `internal/exec/loop.go` and `internal/exec/subworkflow.go`, and `internal/runner/resume_test.go` / `cmd/agent-runner/resume_test.go` for the existing test patterns.

**Testing conventions.** TDD per `CLAUDE.md`. Produce state files by interrupting real runs (a fake process runner or fake CLI that returns an abort) rather than hand-writing JSON where practical, so the frames under test are the ones the executor actually writes. Extend `internal/model/state_test.go` and `internal/runner/resume_test.go`. `make fmt`, `make lint`, `make test` before finishing; do not run `make build`.

## Spec

From `specs/step-repair/spec.md`:

### Requirement: Persistence and resume

Agent Runner SHALL persist an open repair frame identifying the owning check, its scope path and loop iteration, the repair form and target, the current phase (checking, repairing, replaying, or failed), the completed attempt count, and the guarded execution identity. The frame SHALL be cleared on success and when flow control advances past a failed check, and retained with phase `failed` when the failure stops the run. Resume inside an open frame SHALL restore it and continue the cycle from the recorded phase without repeating completed attempts. Resume of a run that failed at an exhausted or blocked check SHALL re-enter the cycle with a fresh budget: at the `rerun` target for the rerun form, and at the check for the inline form. Attempt counts SHALL be tracked per loop iteration.

#### Scenario: Interrupted during inline repair
- **WHEN** the run is interrupted while an inline repair agent for attempt 1 of 2 is executing
- **THEN** resume resumes that repair agent's session for attempt 1 and then reruns the check

#### Scenario: Interrupted during replay
- **WHEN** the run is interrupted while an intermediate step of a rerun replay is executing
- **THEN** resume continues the replay from that step and then reruns the check

#### Scenario: Resume after exhaustion with rerun form
- **WHEN** a run failed because `verify-draft-pr` was blocked and the user resumes after granting the missing scope
- **THEN** execution re-enters at `open-draft-pr` with the evidence preface, then reruns `verify-draft-pr`, with a full repair budget available

#### Scenario: Resume after exhaustion with inline form
- **WHEN** a run failed because an inline-repaired check exhausted its budget and the user resumes
- **THEN** the check runs first, and repair runs only if it fails again, with a full budget

From `specs/recursive-state/spec.md`:

### Requirement: Resume from nested position

`agent-runner -resume` SHALL restore execution to the exact nested position recorded in the state file, including loop iteration and sub-workflow depth. Execution continues from the step after the last completed step at the deepest nesting level. When the recorded position carries an open repair frame, resume SHALL restore the frame before resolving the step to execute and continue the repair cycle from the recorded phase. When the recorded step is a check whose repair cycle ended in failure, resume SHALL start at the check's `rerun` target for the rerun form, or at the check for the inline form, with a fresh attempt budget.

#### Scenario: Resume into a loop
- **WHEN** the state file records position inside a for-each loop at iteration 3 of 5
- **THEN** Agent Runner resumes at iteration 3, skipping iterations 1 and 2

#### Scenario: Resume into a sub-workflow
- **WHEN** the state file records position inside a sub-workflow with step 2 of 3 as the last completed step
- **THEN** Agent Runner resumes inside the sub-workflow at step 3, within the parent's context

#### Scenario: Resume with stale nested state
- **WHEN** the sub-workflow file has changed since the state was written and the recorded step ID no longer exists
- **THEN** Agent Runner fails with a descriptive error identifying the missing step and which workflow file changed

#### Scenario: Resume restores an open repair frame
- **WHEN** the state file records phase `repairing` with 0 completed attempts for check `check-plan`
- **THEN** Agent Runner resumes the repair agent for attempt 1 and then reruns `check-plan`

#### Scenario: Resume of a failed rerun check rewinds
- **WHEN** the state file records `verify-draft-pr` as failed with form `rerun` targeting `open-draft-pr`
- **THEN** Agent Runner resumes at `open-draft-pr`, not at `verify-draft-pr`

#### Scenario: Stale rerun target on resume
- **WHEN** the workflow file has changed and the recorded rerun target no longer exists
- **THEN** Agent Runner fails with a descriptive error identifying the missing target step

From `specs/run-failure-evidence/spec.md`:

#### Scenario: Interrupted between action and check
- **WHEN** the run is interrupted after `open-draft-pr` completes and before `verify-draft-pr` runs, then resumed, and the check fails
- **THEN** the failure record contains `open-draft-pr`'s final response rebuilt from audit by its persisted identity

#### Scenario: Evidence survives resume
- **WHEN** a run fails at a check, is resumed, and later completes
- **THEN** the earlier failure record remains inspectable for that attempt

## Test Plan

- `INT-002` (Resume from every repair frame phase): assigned to this task in full. Produce state files by interrupting a run (fake runner returns an abort) at: inline repair in progress, replayed intermediate step in progress, check failed after exhaustion (rerun form), check failed after exhaustion (inline form), check failed as blocked, between the guarded agent step and the check, immediately after an exhausted `warn_on_failure` check inside a loop, and a nested loop → sub-workflow → check replay. Resume each with a fake runner scripted to succeed. Assert the re-entry step is, respectively: the inline agent for the same attempt, the replayed step, the rerun target, the check, the rerun target, the check (with the guarded response rebuilt from audit and present in the failure record), the step after the warning check, and the nested check; the attempt budget is fresh only after a terminal failure; a removed rerun target produces the descriptive error; the loop iteration index is unchanged by the frame; the serialized frame sits at the owning scope's nesting level. Runs with `go test ./internal/runner -run ResumeRepair`.
- `E2E-001` (Blocked draft-PR shape, then resume rewinds to the action): assigned to this task. Add a headless test beside `cmd/agent-runner/smoke_headless_integration_test.go` using its isolated `HOME` and project and fake `claude` CLI on `PATH`. Fixture workflow: an autonomous agent step `act`, then a shell step and a sub-workflow step that runs its own agent step (the fake CLI returns a distinguishable response for it), then a shell check `verify` with `repair: {rerun: act}`. The fake CLI prints a diagnosis ending in `REPAIR_BLOCKED` on its first invocation and, when a marker file exists, creates the file the check looks for. Journey: run; observe failure; create the marker file; `--resume`. Assert: first run exits non-zero, prints `verify failed: … blocked: …` above the `to resume:` line with the blocked text taken from `act`'s response and not the sub-workflow agent's, `state.json` carries the same `failureReason`, audit shows no `repair_attempt_start`; resume runs `act` again with the evidence preface in its prompt (visible in the fake CLI's recorded stdin), then `verify` passes and the run completes with state marked completed. Runs with `go test ./cmd/agent-runner -run RepairBlockedE2E`.

## Done When

- Every scenario copied above passes in automated tests, INT-002 passes with `go test ./internal/runner -run ResumeRepair`, and E2E-001 passes with `go test ./cmd/agent-runner -run RepairBlockedE2E`.
- `ResolveResumeStep` takes the frame and is table-tested for every phase and form combination plus the no-frame case, with existing `ResolveResumeStep` tests unchanged.
- A run interrupted during an inline repair of a check inside a group resumes by re-executing the group from its first child (existing behavior), then continues the open cycle at the check without repeating the completed attempt; the frame is not applied to any other step.
- Resume restores the frame and `LastAgent` onto the owning scope's context at top level, inside a loop iteration, and inside a sub-workflow, and the audit-rebuilt guarded response is present in the failure record when the check fails again.
- A stale rerun target fails with a descriptive error naming the target step; existing stale-step errors are unchanged.
- The earlier failed `step_end` and its evidence remain in the audit log after a successful resumed run.
- `make fmt`, `make lint`, and `make test` pass.
