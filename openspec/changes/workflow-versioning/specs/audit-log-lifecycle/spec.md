## MODIFIED Requirements

### Requirement: Run start context

The `run_start` event SHALL include the exact resolved versioned workflow file path, version-free workflow name, workflow hash, and all params.

#### Scenario: Run start captures workflow metadata
- **WHEN** a run begins for workflow `workflows/deploy-v1.0.yaml` with params `{env: "staging"}`
- **THEN** the `run_start` entry includes the exact versioned file path, version-free name, hash, and params

### Requirement: Sub-workflow-level events

Agent Runner SHALL emit `sub_workflow_start` before executing an exact versioned sub-workflow's steps and `sub_workflow_end` after all sub-workflow steps complete, nested between the sub-workflow step's `step_start` and `step_end`.

#### Scenario: Sub-workflow executes
- **WHEN** a sub-workflow step invokes `verify-task-v1.0.yaml`
- **THEN** Agent Runner emits `sub_workflow_start`, then child step events, then `sub_workflow_end`, all nested within the step's `step_start` / `step_end`
