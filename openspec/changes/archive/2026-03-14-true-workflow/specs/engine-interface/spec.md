## MODIFIED Requirements

### Requirement: State file persistence

Baton SHALL persist workflow state to a JSON file after each step. The engine's `getStateDir(params)` (if implemented) determines the directory; otherwise baton defaults to the project root. The state file SHALL contain at the top level: `workflowFile`, `workflowName`, `params`, and `workflowHash`. The `currentStep` field SHALL be a recursive nested object tracking the full nesting path through sub-workflows and loop iterations; each node in this nesting chain SHALL contain its own scope-local `sessionIds` and `capturedVariables`.

#### Scenario: State file written after each step
- **WHEN** a step completes (success or abort)
- **THEN** baton writes the state file to the engine's state dir (or project root)

#### Scenario: Engine provides custom state dir
- **WHEN** the engine implements `getStateDir` and returns a path
- **THEN** baton writes the state file to that directory

#### Scenario: No engine configured
- **WHEN** a workflow has no engine block
- **THEN** baton writes the state file to the project root

#### Scenario: Workflow completes successfully
- **WHEN** all steps complete successfully
- **THEN** baton deletes the state file

#### Scenario: State file captures nested position
- **WHEN** execution is inside a sub-workflow within a loop
- **THEN** the state file's `currentStep` captures the full nesting path, not just the leaf step ID

#### Scenario: State file includes captured variables
- **WHEN** a shell step has captured stdout into a variable
- **THEN** the state file includes the captured variable name and value in `capturedVariables`
