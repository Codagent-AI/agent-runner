## ADDED Requirements

### Requirement: Engine step applicability

Each engine SHALL report whether it manages a given step ID. This report SHALL depend only on the step ID and the engine's configuration, and SHALL NOT fail. Agent Runner SHALL ask the engine in effect for an agent step before calling any other per-step engine hook for that step. For a step the engine does not manage, Agent Runner SHALL call neither `EnrichPrompt` nor `ValidateStep`.

#### Scenario: Unmanaged step under an engine
- **WHEN** an agent step runs under an engine that reports it does not manage the step's ID
- **THEN** Agent Runner runs the step with its prompt unchanged and skips step validation, without calling `EnrichPrompt` or `ValidateStep`

#### Scenario: Managed step under an engine
- **WHEN** an agent step runs under an engine that reports it manages the step's ID
- **THEN** Agent Runner calls `EnrichPrompt` before launching the agent and `ValidateStep` after the step succeeds

#### Scenario: Step without an engine
- **WHEN** an agent step runs in a workflow scope with no engine in effect
- **THEN** Agent Runner calls no engine hooks for that step

## MODIFIED Requirements

### Requirement: Prompt enrichment

For steps the engine manages, Agent Runner SHALL call the engine's `EnrichPrompt` hook before launching the agent to obtain engine-provided context. The enrichment SHALL be kept separate from the step prompt and delivered according to the system prompt routing rules:
- via native system prompt for supporting adapters in interactive mode;
- wrapped in `<system>` XML tags for non-supporting adapters in interactive mode;
- concatenated into the positional argument for headless mode.

The engine determines which step IDs it manages. If `EnrichPrompt` returns an error for a managed step, Agent Runner SHALL NOT launch the agent and SHALL fail the step with an error that includes the engine's message. That failure SHALL go through the run's normal step-failure handling.

#### Scenario: Step ID matches an engine artifact
- **WHEN** a step is managed by the engine and the engine returns enrichment
- **THEN** Agent Runner calls `EnrichPrompt` and delivers the result separately from the step prompt via system prompt routing

#### Scenario: Step ID does not match any engine artifact
- **WHEN** a step is not managed by the engine
- **THEN** Agent Runner uses the step's prompt as-is, without calling `EnrichPrompt`

#### Scenario: Engine returns no enrichment
- **WHEN** the engine's `EnrichPrompt` hook returns an empty string and no error for a managed step
- **THEN** Agent Runner uses the step's prompt as-is

#### Scenario: Enrichment fails for a managed step
- **WHEN** the engine's `EnrichPrompt` hook returns an error for a managed step
- **THEN** Agent Runner does not launch the agent, and the step fails with an error that includes the engine's message

### Requirement: Step validation

After an agent step that the engine manages completes successfully, Agent Runner SHALL call the engine's `ValidateStep` hook to verify the step's artifacts were created. If validation fails or returns an error, Agent Runner SHALL mark the step failed and SHALL report the step ID and the engine's full explanation in the run's user-facing error output, including in headless runs and for steps inside sub-workflows. The explanation SHALL also be recorded in the step's audit record. An engine-validation failure SHALL NOT be classified as an agent crash. That failure SHALL go through the run's normal step-failure handling, including resume and repair, the same way in interactive, autonomous, and headless modes.

#### Scenario: Validation passes
- **WHEN** a managed step completes and `ValidateStep` confirms the artifact exists
- **THEN** Agent Runner proceeds to the next step

#### Scenario: Validation fails
- **WHEN** a managed step completes but `ValidateStep` reports a required artifact is not done
- **THEN** the step fails with an error naming the artifact, and the workflow does not proceed to the next step

#### Scenario: Validation failure explanation is shown in a headless child workflow
- **WHEN** a managed step inside a sub-workflow completes in a headless run and `ValidateStep` reports that `tasks` is not done
- **THEN** the run's error output shows the step ID and the engine's explanation naming `tasks`, and the run stops as a failed step rather than an infrastructure crash

#### Scenario: Validation failure from a CLI error is shown
- **WHEN** `ValidateStep` fails because `openspec status` exits non-zero with a message
- **THEN** the run's error output includes that message

#### Scenario: Validation fails and the run is resumed
- **WHEN** a managed step failed engine validation and the user resumes the run after writing the missing artifact
- **THEN** the step is retried under the run's normal resume behavior and passes validation

#### Scenario: Validation errors
- **WHEN** `ValidateStep` returns an error (for example the `openspec` CLI fails)
- **THEN** the step fails with an error that includes the engine's message

#### Scenario: Step ID does not match any engine artifact
- **WHEN** a step is not managed by the engine and completes successfully
- **THEN** Agent Runner skips validation and proceeds to the next step

#### Scenario: Managed step fails on its own
- **WHEN** a managed step itself fails
- **THEN** Agent Runner does not call `ValidateStep` for that attempt
