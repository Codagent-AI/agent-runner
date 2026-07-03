## Requirements

### Requirement: Engine loading

Agent Runner SHALL load the engine specified in a workflow's `engine` block at workflow load time. If the engine type is unrecognized or the engine fails to initialize (e.g., missing CLI dependency), Agent Runner SHALL fail immediately with a descriptive error before executing any steps.

#### Scenario: Engine loads successfully
- **WHEN** a workflow has `engine.type: openspec` and the openspec CLI is available
- **THEN** Agent Runner initializes the engine and proceeds to execute steps

#### Scenario: Engine type unrecognized
- **WHEN** a workflow has `engine.type: foo` and no engine named "foo" is registered
- **THEN** Agent Runner fails immediately with an error naming the unknown engine type

#### Scenario: Engine initialization fails
- **WHEN** a workflow has `engine.type: openspec` but the openspec CLI is not installed
- **THEN** Agent Runner fails immediately with an error explaining the missing dependency

### Requirement: Workflow validation

After loading the engine and the workflow, Agent Runner SHALL call the engine's `ValidateWorkflow` hook to verify the workflow is compatible with the engine. If validation fails, Agent Runner SHALL fail immediately with a descriptive error.

#### Scenario: Engine validates workflow successfully
- **WHEN** the engine's `ValidateWorkflow` hook passes
- **THEN** Agent Runner proceeds to execute steps

#### Scenario: Engine validation fails
- **WHEN** the engine's `ValidateWorkflow` hook reports errors
- **THEN** Agent Runner fails immediately with the engine's error messages

### Requirement: State file persistence

Agent Runner SHALL persist workflow state to `state.json` in the run session directory after each step. Engines do not choose the state directory. The state file SHALL contain at the top level: `workflowFile`, `workflowName`, `params`, and `workflowHash`. The `currentStep` field SHALL be a recursive nested object tracking the full nesting path through sub-workflows and loop iterations; each node in this nesting chain SHALL contain its own scope-local `sessionIds` and `capturedVariables`.

#### Scenario: State file written after each step
- **WHEN** a step completes (success or abort)
- **THEN** Agent Runner writes `state.json` in the run session directory

#### Scenario: Engine does not affect state dir
- **WHEN** a workflow has an engine block
- **THEN** Agent Runner still writes `state.json` in the run session directory

#### Scenario: Workflow completes successfully
- **WHEN** all steps complete successfully
- **THEN** Agent Runner preserves `state.json` and marks it completed

#### Scenario: State file captures nested position
- **WHEN** execution is inside a sub-workflow within a loop
- **THEN** the state file's `currentStep` captures the full nesting path, not just the leaf step ID

#### Scenario: State file includes captured variables
- **WHEN** a shell step has captured stdout into a variable
- **THEN** the state file includes the captured variable name and value in `capturedVariables`

### Requirement: Workflow resumption

`agent-runner -resume <run-id>` SHALL load the run's state file, re-load the workflow from the persisted `workflowFile`, and resume from `currentStep`. If the workflow file has changed since the state was written, Agent Runner SHALL warn but proceed.

#### Scenario: Resume from state file
- **WHEN** the user runs `agent-runner -resume <run-id>`
- **THEN** Agent Runner loads the run state, re-loads the workflow, and resumes from `currentStep` with the persisted `sessionIds` and `params`

#### Scenario: Workflow changed since state was written
- **WHEN** resuming and the workflow file differs from when the state was written
- **THEN** Agent Runner warns that the workflow has changed but proceeds if `currentStep` ID still exists in the workflow

#### Scenario: Current step ID no longer exists
- **WHEN** resuming and `currentStep` references a step ID that no longer exists in the workflow
- **THEN** Agent Runner fails with a descriptive error

### Requirement: Prompt enrichment

For steps whose ID matches an engine-managed artifact, Agent Runner SHALL call the engine's `EnrichPrompt` hook to obtain engine-provided context. The enrichment SHALL be kept separate from the step prompt and delivered according to the system prompt routing rules: via native system prompt for supporting adapters in interactive mode, wrapped in `<system>` XML tags for non-supporting adapters in interactive mode, or concatenated into the positional argument for headless mode. The engine determines which step IDs it manages.

#### Scenario: Step ID matches an engine artifact
- **WHEN** a step's ID matches an engine-managed artifact and the engine returns enrichment
- **THEN** Agent Runner calls `EnrichPrompt` and delivers the result separately from the step prompt via system prompt routing

#### Scenario: Step ID does not match any engine artifact
- **WHEN** a step's ID does not match any engine-managed artifact
- **THEN** Agent Runner uses the step's prompt as-is, without calling `EnrichPrompt`

#### Scenario: Engine returns no enrichment
- **WHEN** the engine's `EnrichPrompt` hook returns an empty string
- **THEN** Agent Runner uses the step's prompt as-is

### Requirement: Step validation

After a step whose ID matches an engine-managed artifact completes successfully, Agent Runner SHALL call the engine's `ValidateStep` hook to verify the artifact was created. If validation fails, Agent Runner SHALL offer the user a choice: resume the previous session interactively, or exit.

#### Scenario: Validation passes
- **WHEN** a step completes and `ValidateStep` confirms the artifact exists
- **THEN** Agent Runner proceeds to the next step

#### Scenario: Validation fails — user chooses resume
- **WHEN** a step completes but `ValidateStep` reports the artifact is missing, and the user chooses to resume
- **THEN** Agent Runner re-launches the previous session in interactive mode so the user can fix it

#### Scenario: Validation fails — user chooses exit
- **WHEN** a step completes but `ValidateStep` reports the artifact is missing, and the user chooses to exit
- **THEN** Agent Runner exits the workflow

#### Scenario: Step ID does not match any engine artifact
- **WHEN** a step's ID does not match any engine-managed artifact and the step completes successfully
- **THEN** Agent Runner skips validation and proceeds to the next step
