## MODIFIED Requirements

### Requirement: Core namespace for general-purpose builtins

The builtin set SHALL include a `core` namespace containing general-purpose workflows that are not tied to any particular planning methodology. The `core` namespace SHALL at minimum contain `finalize-pr`, `implement-task`, `run-validator`, and `debug`.

#### Scenario: Core workflows invoked by namespace
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **THEN** the finalize-pr workflow loads from the embedded `core` namespace

#### Scenario: Core workflows not invoked by bare name
- **WHEN** the user runs `agent-runner run finalize-pr` with no `.agent-runner/workflows/finalize-pr.yaml` file
- **THEN** the command fails with a workflow-not-found error; the builtin `core:finalize-pr` is not resolved

#### Scenario: Debug workflow available under core
- **WHEN** the user runs `agent-runner run core:debug`
- **THEN** the debug workflow loads from the embedded `core` namespace and executes
