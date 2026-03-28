# Task: Engine framework, state file, and runner integration

## Goal

Add the engine abstraction to baton: the engine interface, hardcoded registry, state file module, runner integration with engine hooks, validation failure UX, and the `baton resume` command. This delivers the full engine framework without any concrete engine implementation.

## Background

Baton has a commander-based CLI with commands in `src/commands/` (`run.ts`, `validate.ts`) and unit tests in `test/`. This task adds the engine layer.

**Engine interface** — A plain object with all-optional methods: `getStateDir(params)` returns a directory path for the state file, `validateWorkflow(workflow)` throws on incompatibility, `enrichPrompt(stepId, params)` returns a string to prepend to the prompt (or `undefined` for step IDs it doesn't manage), and `validateStep(stepId, params)` returns `true` if the step produced its expected output (or `true` for step IDs it doesn't manage). All methods are optional. Define the interface and a `createEngine` function with a hardcoded registry map in `src/engine.ts`. For now the registry is empty (no concrete engines yet) — but the infrastructure is in place.

**Engine registry** — A simple `Record<string, ConstructorType>` map in `src/engine.ts`. `createEngine` extracts `type` from the engine config block, looks up the constructor, passes the remaining config fields. Throws if type is unrecognized.

**Workflow schema changes** — Add an optional `engine` block to `WorkflowSchema` in `src/schema.ts`. The `type` field is required, everything else is passthrough (`z.record`). Example: `engine: { type: openspec, change_param: change_name }`.

**State file** — Create `src/state.ts` with read/write/delete functions. The state file is named `baton-state.json` (visible, not hidden). Location: engine's `getStateDir(params)` or project root if no engine. Contents:

```typescript
interface RunState {
  workflowFile: string;
  workflowName: string;
  currentStep: string;        // step ID, not index
  sessionIds: Record<string, string>;
  params: Record<string, string>;
  workflowHash: string;       // hash of workflow file content for change detection
}
```

State file is written after each step completes (success or abort). Deleted when the workflow completes successfully.

**Runner refactor** — `runWorkflow` accepts an optional `Engine` parameter. The execution loop changes:
1. At startup: call `engine.validateWorkflow(workflow)` if implemented (throws on failure = abort)
2. Before each agent step: call `engine.enrichPrompt(step.id, params)` — if it returns a string, prepend it to the step's prompt
3. After each successful agent step: call `engine.validateStep(step.id, params)` — if it returns false, prompt the user with `[r] Resume previous session / [q] Exit workflow`. On "r", re-launch the session in interactive mode via `claude --resume <sessionId>`, then re-validate. On "q", exit (state file persists).
4. Remove the old write-only `saveState` / `STATE_FILE` / `RunState` code entirely. Replace with the new state module.

**`baton resume` command** — New file `src/commands/resume.ts`. Takes a state file path as argument. Loads the state, re-loads the workflow from `workflowFile`, computes the current workflow file hash and compares to `workflowHash` (warn if different), finds the step index for `currentStep` ID (error if not found), then calls `runWorkflow` from that point with the persisted `sessionIds` and `params`.

**`--from` and state file interaction** — When `baton run` is invoked with `--from`, it overrides whatever the state file says. The state file is still written during execution.

**Key files:**
- `src/engine.ts` — new file: Engine interface, createEngine, registry
- `src/state.ts` — new file: RunState type, readState, writeState, deleteState
- `src/schema.ts` — add engine block to WorkflowSchema
- `src/runner.ts` — accept optional Engine, call hooks, replace old state code, add validation failure UX
- `src/commands/resume.ts` — new file: baton resume command
- `src/commands/run.ts` — pass engine to runWorkflow
- `src/commands/index.ts` — re-export registerResumeCommand
- `test/engine.test.ts` — createEngine with known/unknown types, config passthrough
- `test/state.test.ts` — read/write/delete state file, hash comparison
- `test/runner.test.ts` — extend existing tests: engine hook calls, enrichment prepending, validation failure flow, state file writes

## Spec

### Requirement: Engine loading

Baton SHALL load the engine specified in a workflow's `engine` block at workflow load time. If the engine type is unrecognized or the engine fails to initialize, baton SHALL fail immediately with a descriptive error before executing any steps.

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

Baton SHALL persist workflow state to a JSON file after each step. The engine's `getStateDir(params)` (if implemented) determines the directory; otherwise baton defaults to the project root. The state file SHALL contain: `workflowFile`, `workflowName`, `currentStep` (step ID), `sessionIds`, and `params`.

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

For steps whose ID matches an engine-managed artifact, baton SHALL call the engine's `enrichPrompt` (if implemented) to prepend engine-provided context to the step's prompt before passing it to the agent.

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

### Requirement: Engine block in workflow schema

The workflow schema SHALL accept an optional `engine` block. The `type` field SHALL be required. All other fields SHALL be passed through to the engine as opaque configuration.

#### Scenario: Workflow with engine block
- **WHEN** a workflow YAML has `engine: { type: openspec, change_param: change_name }`
- **THEN** baton parses the engine block, extracts `type`, and passes the remaining fields to the engine

#### Scenario: Workflow without engine block
- **WHEN** a workflow YAML has no `engine` block
- **THEN** baton runs with no engine (existing behavior)

#### Scenario: Engine block missing type
- **WHEN** a workflow YAML has `engine: { change_param: change_name }` without `type`
- **THEN** baton fails at load time with a descriptive error

### Requirement: Engine-aware step matching

Engines MAY use workflow step IDs to identify which steps they manage. Baton passes the step ID to engine hooks (`enrichPrompt`, `validateStep`), and the engine determines whether it applies.

#### Scenario: Engine receives step ID in hooks
- **WHEN** baton calls `enrichPrompt` or `validateStep`
- **THEN** baton passes the current step's ID to the engine, which decides whether to act on it

#### Scenario: Engine ignores unrecognized step ID
- **WHEN** the engine receives a step ID it does not manage
- **THEN** it returns no enrichment / skips validation (no-op)

## Done When

- Engine interface defined and createEngine works with registry lookup
- State file is written after each step, deleted on completion, read by `baton resume`
- Runner calls engine hooks at the right points (validateWorkflow, enrichPrompt, validateStep)
- Validation failure prompts user with resume/exit choice
- `baton resume <path>` works end-to-end
- Old write-only state code is removed
- Schema accepts optional engine block
- All new code has unit tests, all tests pass via `bun test`
