# Capability: recursive-state

## Purpose

Defines how the state file tracks execution position recursively through nested workflows and loops, and how resume restores that position.

## MODIFIED Requirements

### Requirement: Recursive position tracking

The state file (`state.json`) SHALL track the current execution position through nested workflows and loops recursively. When execution is inside a sub-workflow or loop, the state file SHALL capture the full nesting path from the top-level workflow down to the currently executing step. When a check with `repair` has an open repair cycle, the state file SHALL additionally record a repair frame at that scope: the owning check ID, the repair form and its target, the current phase (`checking`, `repairing`, `replaying`, or `failed`), the number of completed attempts, and the guarded execution identity (audit prefix and attempt) when one exists. The frame SHALL be cleared when the check succeeds and when a failed check is advanced past by `continue_on_failure` or `warn_on_failure`. When a failed check stops the run, the frame SHALL be retained with phase `failed` so resume can rewind.

#### Scenario: Flat workflow state
- **WHEN** a workflow with no loops or sub-workflows completes step 2 of 4
- **THEN** the state file records `currentStep` as the step 2 ID (unchanged from current behavior)

#### Scenario: Nested sub-workflow state
- **WHEN** execution is inside step `run-validator` of `implement-task.yaml`, which is itself inside the `implement-tasks` loop of `implement-change.yaml`, on the `validator` step
- **THEN** the state file captures the full path: implement-change → implement-tasks (iteration index) → implement-task → run-validator → validator

#### Scenario: Loop iteration tracking
- **WHEN** execution is on iteration 2 of a for-each loop
- **THEN** the state file records the current iteration index and the loop variable's current value

#### Scenario: Repair frame during replay
- **WHEN** execution is replaying `open-draft-pr` as attempt 1 of the check `verify-draft-pr`
- **THEN** the state file records the current step as `open-draft-pr` and a repair frame naming `verify-draft-pr`, form `rerun`, phase `replaying`, and 0 completed attempts

#### Scenario: Frame retained on a run-stopping failure
- **WHEN** a check with `repair: {rerun: open-draft-pr}` exhausts its budget and stops the run
- **THEN** the state file records the frame with phase `failed`, form `rerun`, target `open-draft-pr`, and the guarded execution identity

#### Scenario: Frame cleared when flow control advances
- **WHEN** a check with `repair` and `warn_on_failure: true` exhausts its budget
- **THEN** the state file records the check as completed with no repair frame

#### Scenario: Repair frame inside a loop iteration
- **WHEN** a check inside iteration 2 of a loop is in phase `repairing`
- **THEN** the repair frame is recorded at that iteration's nesting level and does not affect the parent loop's iteration index

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
