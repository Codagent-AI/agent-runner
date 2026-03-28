# audit-log-lifecycle Specification

## Purpose
TBD - created by archiving change audit-log. Update Purpose after archive.
## Requirements
### Requirement: Run-level events

Baton SHALL emit a `run_start` event after workflow and param validation succeeds, before the first step executes. Baton SHALL emit a `run_end` event after the last step completes or after an error event.

#### Scenario: Successful run
- **WHEN** a workflow passes validation and all steps complete
- **THEN** baton emits `run_start` before the first step and `run_end` after the last step

#### Scenario: Validation failure
- **WHEN** workflow validation fails (schema error, missing params, engine validation)
- **THEN** no audit log file is created

#### Scenario: Run fails mid-execution
- **WHEN** a step fails and halts the workflow
- **THEN** baton emits `run_end` with outcome `failed` after the failed step's `step_end`

### Requirement: Run start context

The `run_start` event SHALL include the workflow file path, workflow name, workflow hash, and all params.

#### Scenario: Run start captures workflow metadata
- **WHEN** a run begins for workflow `workflows/deploy.yaml` with params `{env: "staging"}`
- **THEN** the `run_start` entry includes the file path, name, hash, and params

### Requirement: Resumed run indicator

When a run is resumed via `baton resume`, the `run_start` event SHALL indicate it is a resume, include the step it is resuming from, and create a new log file.

#### Scenario: Resumed run
- **WHEN** a run is resumed from step `design`
- **THEN** a new audit log file is created and the `run_start` entry includes a resume indicator and the resuming step ID

### Requirement: Step-level events

Baton SHALL emit a `step_start` event before dispatching a step and a `step_end` event after the step completes, for every step type (shell, agent, loop, sub-workflow, group).

#### Scenario: Step executes normally
- **WHEN** a step `build` starts and completes with outcome `success`
- **THEN** baton emits `step_start` before execution and `step_end` after, with no events from other steps in between (except child events for loops/groups/sub-workflows)

#### Scenario: Step fails
- **WHEN** a step fails with a runtime error
- **THEN** baton emits `step_end` with outcome `failed` and the error details

### Requirement: Iteration-level events

Baton SHALL emit `iteration_start` before executing a loop iteration's child steps and `iteration_end` after all child steps in that iteration complete.

#### Scenario: Loop with 3 iterations
- **WHEN** a counted loop executes 3 iterations
- **THEN** baton emits 3 `iteration_start` / `iteration_end` pairs, nested between the loop step's `step_start` and `step_end`

#### Scenario: Iteration fails
- **WHEN** a child step in iteration 2 fails
- **THEN** baton emits `iteration_end` for iteration 2 with outcome `failed`

### Requirement: Sub-workflow-level events

Baton SHALL emit `sub_workflow_start` before executing a sub-workflow's steps and `sub_workflow_end` after all sub-workflow steps complete, nested between the sub-workflow step's `step_start` and `step_end`.

#### Scenario: Sub-workflow executes
- **WHEN** a sub-workflow step invokes `verify-task.yaml`
- **THEN** baton emits `sub_workflow_start`, then child step events, then `sub_workflow_end`, all nested within the step's `step_start` / `step_end`

### Requirement: Crash handling

When an uncaught exception occurs, baton SHALL emit an `error` event followed by `run_end` before the process exits. The audit log file SHALL be flushed to disk before exit.

#### Scenario: Crash mid-step
- **WHEN** an uncaught exception occurs during step execution
- **THEN** baton emits `error`, then `run_end` with outcome `failed`, and flushes the log file before exiting

### Requirement: Audit log persists regardless of outcome

The audit log file SHALL remain on disk after the run completes, regardless of whether the run succeeded or failed.

#### Scenario: Successful run preserves log
- **WHEN** a workflow completes successfully and baton deletes the state file
- **THEN** the audit log file is not deleted

