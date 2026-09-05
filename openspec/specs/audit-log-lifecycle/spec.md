# audit-log-lifecycle Specification

## Purpose
Define the audit event lifecycle emitted during Agent Runner workflow execution.
## Requirements
### Requirement: Run-level events

Agent Runner SHALL emit a `run_start` event after workflow and param validation succeeds, before the first step executes. Agent Runner SHALL emit a `run_end` event after the last step completes or after an error event.

#### Scenario: Successful run
- **WHEN** a workflow passes validation and all steps complete
- **THEN** Agent Runner emits `run_start` before the first step and `run_end` after the last step

#### Scenario: Validation failure
- **WHEN** workflow validation fails (schema error, missing params, engine validation)
- **THEN** no audit log file is created

#### Scenario: Run fails mid-execution
- **WHEN** a step fails and halts the workflow
- **THEN** Agent Runner emits `run_end` with outcome `failed` after the failed step's `step_end`

### Requirement: Run start context

The `run_start` event SHALL include the exact resolved versioned workflow file path, version-free workflow name, workflow hash, and all params.

#### Scenario: Run start captures workflow metadata
- **WHEN** a run begins for workflow `workflows/deploy-v1.0.yaml` with params `{env: "staging"}`
- **THEN** the `run_start` entry includes the exact versioned file path, version-free name, hash, and params

### Requirement: Resumed run indicator

When a run is resumed via `agent-runner -resume`, the `run_start` event SHALL indicate it is a resume and include the step it is resuming from.

#### Scenario: Resumed run
- **WHEN** a run is resumed from step `design`
- **THEN** the `run_start` entry includes a resume indicator and the resuming step ID

### Requirement: Step-level events

Agent Runner SHALL emit a `step_start` event before dispatching a step and a `step_end` event after the step completes, for every step type (shell, agent, loop, sub-workflow, group).

#### Scenario: Step executes normally
- **WHEN** a step `build` starts and completes with outcome `success`
- **THEN** Agent Runner emits `step_start` before execution and `step_end` after, with no events from other steps in between (except child events for loops/groups/sub-workflows)

#### Scenario: Step fails
- **WHEN** a step fails with a runtime error
- **THEN** Agent Runner emits `step_end` with outcome `failed` and the error details

### Requirement: Iteration-level events

Agent Runner SHALL emit `iteration_start` before executing a loop iteration's child steps and `iteration_end` after all child steps in that iteration complete.

#### Scenario: Loop with 3 iterations
- **WHEN** a counted loop executes 3 iterations
- **THEN** Agent Runner emits 3 `iteration_start` / `iteration_end` pairs, nested between the loop step's `step_start` and `step_end`

#### Scenario: Iteration fails
- **WHEN** a child step in iteration 2 fails
- **THEN** Agent Runner emits `iteration_end` for iteration 2 with outcome `failed`

### Requirement: Sub-workflow-level events

Agent Runner SHALL emit `sub_workflow_start` before executing an exact versioned sub-workflow's steps and `sub_workflow_end` after all sub-workflow steps complete, nested between the sub-workflow step's `step_start` and `step_end`.

#### Scenario: Sub-workflow executes
- **WHEN** a sub-workflow step invokes `verify-task-v1.0.yaml`
- **THEN** Agent Runner emits `sub_workflow_start`, then child step events, then `sub_workflow_end`, all nested within the step's `step_start` / `step_end`

### Requirement: Crash handling

When an uncaught exception occurs, Agent Runner SHALL emit an `error` event followed by `run_end` before the process exits. The audit log file SHALL be closed before exit.

#### Scenario: Crash mid-step
- **WHEN** an uncaught exception occurs during step execution
- **THEN** Agent Runner emits `error`, then `run_end` with outcome `failed`, and closes the log file before exiting

### Requirement: Audit log persists regardless of outcome

The audit log file SHALL remain on disk after the run completes, regardless of whether the run succeeded or failed.

#### Scenario: Successful run preserves log
- **WHEN** a workflow completes successfully
- **THEN** the audit log file is not deleted

### Requirement: Execution sessions have durable identities

Every run invocation that starts or resumes workflow execution SHALL have a durable execution-session identity. Run-level and step-level lifecycle events SHALL identify the execution session that emitted them so evidence from separate resume sessions can be distinguished without changing the stable source-run identity.

#### Scenario: New run starts
- **WHEN** Agent Runner starts a new source run
- **THEN** its lifecycle events identify both the run and its first execution session

#### Scenario: Run resumes
- **WHEN** Agent Runner resumes an existing source run
- **THEN** newly emitted lifecycle events retain the run identity and use a new execution-session identity

### Requirement: Step Git checkpoints are recorded durably

For each executable source step attempt, Agent Runner SHALL record a local repository checkpoint at step start and step end. When Git evidence is available, the checkpoint SHALL include the current revision plus index, working-tree, and untracked-file state sufficient to derive aggregate file/addition/deletion deltas and identify commits appearing across the boundary. When unavailable, unsupported, or interrupted before the closing checkpoint, the event SHALL state that limitation rather than inventing a revision or zero change.

#### Scenario: Step starts and completes in Git repository
- **WHEN** a step executes normally in a supported Git worktree
- **THEN** its lifecycle evidence contains starting and ending Git revisions and the local checkpoint data needed for conservative change attribution

#### Scenario: Step does not change HEAD
- **WHEN** a step completes without creating a commit
- **THEN** both checkpoints retain enough repository state to distinguish no change from uncommitted work

#### Scenario: Step is interrupted before end checkpoint
- **WHEN** the process is terminated before Agent Runner can capture the step's ending repository checkpoint
- **THEN** the lifecycle evidence marks the ending checkpoint unavailable

#### Scenario: Run is outside a Git repository
- **WHEN** a step executes where supported Git evidence is unavailable
- **THEN** the checkpoint records Git evidence as unavailable and the step continues normally

### Requirement: Source and audit lifecycle events are linked

Agent Runner SHALL durably record audit launch requested, audit launched, audit launch failed, audit completed, and reporting warning events as applicable. A successful launch SHALL link the source run and execution session to the audit run, and the audit run SHALL contain the reciprocal source identifiers.

Audit launch SHALL occur only after the source terminal outcome, metrics, step checkpoints, and other required durable evidence have been flushed.

#### Scenario: Audit launch succeeds
- **WHEN** an eligible execution session is finalized and its linked audit starts
- **THEN** durable lifecycle evidence links the source run and execution session to the audit run in both directions

#### Scenario: Audit launch fails
- **WHEN** Agent Runner cannot start the audit
- **THEN** the source audit log records the failed launch and reason without changing the source outcome

#### Scenario: Audit completes with reporting warning
- **WHEN** model auditing completes but Google Sheets reporting fails
- **THEN** the audit lifecycle records its completion and reporting warning separately

#### Scenario: Source evidence is not yet durable
- **WHEN** the source reaches a terminal outcome but required evidence has not finished flushing
- **THEN** Agent Runner does not launch the audit until finalization succeeds or records a non-blocking launch warning if it cannot be finalized

