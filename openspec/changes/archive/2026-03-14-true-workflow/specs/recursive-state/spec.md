## ADDED Requirements

### Requirement: Recursive position tracking

The state file (`baton-state.json`) SHALL track the current execution position through nested workflows and loops recursively. When execution is inside a sub-workflow or loop, the state file SHALL capture the full nesting path — from the top-level workflow down to the currently executing step.

#### Scenario: Flat workflow state
- **WHEN** a workflow with no loops or sub-workflows completes step 2 of 4
- **THEN** the state file records `currentStep` as the step 2 ID (unchanged from current behavior)

#### Scenario: Nested sub-workflow state
- **WHEN** execution is inside step `run-gauntlet` of `implement-task.yaml`, which is itself inside the `implement-tasks` loop of `implement-change.yaml`, on the `gauntlet` step
- **THEN** the state file captures the full path: implement-change → implement-tasks (iteration index) → implement-task → run-gauntlet → gauntlet

#### Scenario: Loop iteration tracking
- **WHEN** execution is on iteration 2 of a for-each loop
- **THEN** the state file records the current iteration index and the loop variable's current value

### Requirement: Captured variables in state

Captured variables SHALL be persisted in the state file alongside session IDs. This allows resume to restore captured values without re-executing the capture step.

#### Scenario: Captured variable persisted
- **WHEN** a shell step captures stdout into `gauntlet_output` and baton writes the state file
- **THEN** the state file includes `gauntlet_output` and its value

#### Scenario: Resume restores captured variables
- **WHEN** baton resumes from a state file that contains captured variables
- **THEN** the captured variables are available for interpolation in subsequent steps

### Requirement: Resume from nested position

`baton resume` SHALL restore execution to the exact nested position recorded in the state file — including loop iteration and sub-workflow depth. Execution continues from the step after the last completed step at the deepest nesting level.

#### Scenario: Resume into a loop
- **WHEN** the state file records position inside a for-each loop at iteration 3 of 5
- **THEN** baton resumes at iteration 3, skipping iterations 1 and 2

#### Scenario: Resume into a sub-workflow
- **WHEN** the state file records position inside a sub-workflow at step 2 of 3
- **THEN** baton resumes inside the sub-workflow at step 2, within the parent's context

#### Scenario: Resume with stale nested state
- **WHEN** the sub-workflow file has changed since the state was written and the recorded step ID no longer exists
- **THEN** baton fails with a descriptive error identifying the missing step and which workflow file changed
