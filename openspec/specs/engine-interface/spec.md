## ADDED Requirements

### Requirement: Engine loading

Baton SHALL load the engine specified in a workflow's `engine` block at workflow load time. If the engine type is unrecognized or the engine fails to initialize (e.g., missing CLI dependency), baton SHALL fail immediately with a descriptive error before executing any steps.

#### Scenario: Engine loads successfully
- **WHEN** a workflow has `engine.type: openspec` and the openspec CLI is available
- **THEN** baton initializes the engine and proceeds to execute steps

#### Scenario: Engine type unrecognized
- **WHEN** a workflow has `engine.type: foo` and no engine named "foo" is registered
- **THEN** baton fails immediately with an error naming the unknown engine type

#### Scenario: Engine initialization fails
- **WHEN** a workflow has `engine.type: openspec` but the openspec CLI is not installed
- **THEN** baton fails immediately with an error explaining the missing dependency

### Requirement: Workflow validation

After loading the engine and the workflow, baton SHALL call the engine's `validateWorkflow` (if implemented) to verify the workflow is compatible with the engine. If validation fails, baton SHALL fail immediately with a descriptive error.

#### Scenario: Engine validates workflow successfully
- **WHEN** the engine implements `validateWorkflow` and the workflow passes validation
- **THEN** baton proceeds to execute steps

#### Scenario: Engine validation fails
- **WHEN** the engine implements `validateWorkflow` and it reports errors
- **THEN** baton fails immediately with the engine's error messages

#### Scenario: Engine does not implement validateWorkflow
- **WHEN** the engine does not implement `validateWorkflow`
- **THEN** baton skips workflow validation and proceeds

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

### Requirement: Workflow resumption

`baton resume <state-file-path>` SHALL load the state file, re-load the workflow from the persisted `workflowFile`, and resume from `currentStep`. If the workflow file has changed since the state was written, baton SHALL warn but proceed. `--from` on a normal `baton run` SHALL override the state file if one exists.

#### Scenario: Resume from state file
- **WHEN** the user runs `baton resume path/to/baton-state.json`
- **THEN** baton loads the state, re-loads the workflow, and resumes from `currentStep` with the persisted `sessionIds` and `params`

#### Scenario: Workflow changed since state was written
- **WHEN** resuming and the workflow file differs from when the state was written
- **THEN** baton warns that the workflow has changed but proceeds if `currentStep` ID still exists in the workflow

#### Scenario: Current step ID no longer exists
- **WHEN** resuming and `currentStep` references a step ID that no longer exists in the workflow
- **THEN** baton fails with a descriptive error

#### Scenario: --from overrides state file
- **WHEN** a state file exists and the user runs `baton run workflow.yaml --from design`
- **THEN** baton starts from `design`, ignoring the state file's `currentStep`

### Requirement: Prompt enrichment

For steps whose ID matches an engine-managed artifact, baton SHALL call the engine's `enrichPrompt` (if implemented) to prepend engine-provided context to the step's prompt before passing it to the agent. The engine determines which step IDs it manages.

#### Scenario: Step ID matches an engine artifact
- **WHEN** a step's ID matches an engine-managed artifact and the engine implements `enrichPrompt`
- **THEN** baton calls `enrichPrompt` with the step ID, prepends the result to the step's prompt, and passes the combined prompt to the agent

#### Scenario: Step ID does not match any engine artifact
- **WHEN** a step's ID does not match any engine-managed artifact
- **THEN** baton uses the step's prompt as-is, without calling `enrichPrompt`

#### Scenario: Engine does not implement enrichPrompt
- **WHEN** the engine does not implement `enrichPrompt`
- **THEN** baton uses the step's prompt as-is

### Requirement: Step validation

After a step whose ID matches an engine-managed artifact completes successfully, baton SHALL call the engine's `validateStep` (if implemented) to verify the artifact was created. If validation fails, baton SHALL offer the user a choice: resume the previous session interactively, or exit.

#### Scenario: Validation passes
- **WHEN** a step completes and `validateStep` confirms the artifact exists
- **THEN** baton proceeds to the next step

#### Scenario: Validation fails — user chooses resume
- **WHEN** a step completes but `validateStep` reports the artifact is missing, and the user chooses to resume
- **THEN** baton re-launches the previous session in interactive mode so the user can fix it

#### Scenario: Validation fails — user chooses exit
- **WHEN** a step completes but `validateStep` reports the artifact is missing, and the user chooses to exit
- **THEN** baton exits the workflow

#### Scenario: Step ID does not match any engine artifact
- **WHEN** a step's ID does not match any engine-managed artifact and the step completes successfully
- **THEN** baton skips validation and proceeds to the next step
