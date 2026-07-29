## MODIFIED Requirements

### Requirement: Sub-workflow step-specific data

Sub-workflow step `step_start` entries SHALL include the exact resolved versioned workflow path and interpolated params passed.

#### Scenario: Sub-workflow start
- **WHEN** a sub-workflow step starts with resolved path `workflows/verify-v1.0.yaml` and params `{task: "tasks/1.md"}`
- **THEN** the `step_start` entry includes the exact versioned path and params
