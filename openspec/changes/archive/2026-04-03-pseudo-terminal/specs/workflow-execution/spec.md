## MODIFIED Requirements

### Requirement: Agent step execution dispatch

The runner's agent step executor SHALL delegate CLI invocation to the resolved CLI adapter. Interactive steps SHALL execute via the PTY layer. Headless steps SHALL execute via direct process execution. Both paths use the adapter for arg construction.

#### Scenario: Interactive step executes via PTY
- **WHEN** the runner executes an interactive agent step
- **THEN** the executor delegates arg construction to the CLI adapter and launches the process inside a PTY

#### Scenario: Headless step executes via direct exec
- **WHEN** the runner executes a headless agent step
- **THEN** the executor delegates arg construction to the CLI adapter and launches the process via direct exec

## REMOVED Requirements

### Requirement: Signal file mechanism
**Reason**: Replaced by PTY-based continue interception for interactive steps
**Migration**: None — internal implementation detail, no user-facing migration needed

### Requirement: Workflow-level agent field
**Reason**: The `agent` field on `Workflow` is removed. CLI selection is per-step with a hard-coded default of `claude`. A project-level default will be added in a future change.
**Migration**: Remove `agent:` from workflow YAML files. Steps that need a non-default CLI should set `cli:` explicitly.
