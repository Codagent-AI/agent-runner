# Task: Shell Executor + Code Structure Foundation

## Goal

Refactor the flat runner into a dispatcher + executor architecture, implement the ShellExecutor with tee capture and flow control, extend the schema and state format for all new features, create the decomposed workflow files, and verify with E2E tests.

## Background

### Current Architecture

The runner (`src/runner.ts`, ~380 lines) is a flat `for` loop over `workflow.steps`. It handles both shell and agent steps inline, tracks state as a flat `currentStep: string`, and stores session IDs in a flat `Record<string, string>`. The schema (`src/schema.ts`) defines `StepSchema` with `id`, `prompt`, `command`, `mode`, and `session` fields. State (`src/state.ts`) persists a flat `RunState` with `workflowFile`, `workflowName`, `currentStep`, `sessionIds`, `params`, and `workflowHash`.

### Target Architecture

Replace the flat loop with a dispatcher that routes each step to one of 4 executors based on which mutually exclusive field is present:

- `command` → **ShellExecutor** — implemented in this task
- `prompt`/`mode` → **AgentExecutor** — not yet implemented; leave existing inline agent logic in the dispatcher or create a minimal stub that preserves current behavior
- `loop` + `steps` → **LoopExecutor** — not yet implemented; the dispatcher should recognize this step type but can throw "not yet implemented" for now
- `workflow` → **SubWorkflowExecutor** — not yet implemented; same as above

A step with `steps` but no `loop` is a bare group — the dispatcher iterates its children directly with the same context.

Shared logic (interpolation, state writing, flow control evaluation) is extracted into common utilities so executors stay DRY.

### ExecutionContext

A central context object is passed to every executor, carrying all scope-aware state:

```typescript
interface ExecutionContext {
  params: Record<string, string>           // workflow params + loop vars + captures
  sessionIds: Record<string, string>       // step ID → conversation ID (this scope only)
  capturedVariables: Record<string, string> // capture name → value (this scope only)
  lastStepOutcome: 'success' | 'failed' | null

  nestingPath: NestingSegment[]            // current position for state file
  parentContext: ExecutionContext | null    // for session:inherit and variable lookup

  workflowFile: string
  engine: Engine | null
}

interface NestingSegment {
  stepId: string
  iteration?: number
  loopVar?: Record<string, string>
}
```

Scope transitions use immutable copy-on-enter:

- **Loop iteration:** Child copies parent's `params` (adding loop variable), gets empty `sessionIds` and `capturedVariables`, pushes a new segment onto `nestingPath`. `parentContext` points to the parent.
- **Sub-workflow:** Child gets only the explicitly passed `params` (no implicit inheritance). Empty `sessionIds` and `capturedVariables`. `parentContext` points to the invoking context so `session: inherit` can walk up.
- **Group (nested steps without loop):** Same context, no copy — siblings share scope.

### File Organization

```
src/
  runner.ts              → dispatcher + orchestration entry point (slimmed)
  context.ts             → ExecutionContext interface + child context factories
  state.ts               → extended with nested serialization/deserialization
  schema.ts              → extended with recursive Step schema + new fields
  loader.ts              → existing interpolation logic
  executors/
    shell.ts             → ShellExecutor (this task)
    agent.ts             → AgentExecutor (placeholder — not implemented in this task)
    loop.ts              → LoopExecutor (placeholder — not implemented in this task)
    sub-workflow.ts      → SubWorkflowExecutor (placeholder — not implemented in this task)
  shared/
    flow-control.ts      → skip_if, continue_on_failure evaluation
    session.ts           → session resolution logic (new/resume/inherit)
    interpolation.ts     → extracted from loader.ts, shared by all executors
```

### ShellExecutor + Tee Capture

When a shell step has `capture`, stdout is piped instead of inherited: each chunk is written to `process.stdout` (terminal) and appended to a buffer. On exit, the trimmed buffer is stored in `context.capturedVariables[captureName]`. When `capture` is absent, `stdout: 'inherit'` is used with no overhead. `stderr` is always inherited.

### Flow Control

- **`continue_on_failure`** — on step failure, record `lastStepOutcome: 'failed'` on context and proceed instead of halting.
- **`skip_if: previous_success`** — check `context.lastStepOutcome` before executing; skip if `'success'`.

`lastStepOutcome` lives on the context (scope-local).

### State File Format

The state file changes from flat `currentStep: string` to a nested object structure:

```json
{
  "workflowFile": "workflows/implement-change.yaml",
  "workflowName": "implement-change",
  "params": { "change_name": "true-workflow" },
  "workflowHash": "abc123...",
  "currentStep": {
    "stepId": "implement-tasks",
    "sessionIds": {},
    "capturedVariables": { "some_var": "value" },
    "child": null
  }
}
```

Existing flat state files (where `currentStep` is a plain string) must continue to work — a string `currentStep` is treated as a single-level nesting path with no child. This ensures backwards compatibility.

### Schema Extensions

Add these optional fields to `StepSchema`:

- `steps: Step[]` — child steps (recursive)
- `workflow: string` — sub-workflow file path
- `loop: { max?: number, over?: string, as?: string }` — loop configuration
- `capture: string` — capture stdout into named variable (shell steps only)
- `continue_on_failure: boolean` — tolerate step failure
- `skip_if: 'previous_success'` — conditional skip
- `break_if: 'success' | 'failure'` — conditional loop exit
- `model: string` — per-step model override (agent steps only)
- `params: Record<string, string>` — parameter passing for sub-workflows

Add `'inherit'` to `SessionStrategy`.

Validation rules to enforce at load time:

- Mutually exclusive step fields: a step must have exactly one of `command`, `prompt`/`mode` (agent), `loop`+`steps` (loop), or `workflow`
- `loop` requires `max` or (`over` + `as`)
- `capture` only on shell steps
- `model` only on agent steps
- `break_if` only inside a loop body
- `skip_if` not on first step in scope
- `session: inherit` not in top-level workflow

### Interpolation

Extract the `interpolateParams` function from `src/loader.ts` into `src/shared/interpolation.ts` so all executors can use it. The function should resolve captured variables alongside params — merge `context.params` and `context.capturedVariables` before interpolation, with captured variables taking precedence if names collide.

### Real Workflow Files

Create decomposed workflow files that use the new primitives. These won't be run as part of tests — they are for manual UAT.

**`workflows/flokay.yaml`** — Update the existing flat workflow. The planning phase (create, proposal, specs, design, tasks, review) stays as flat steps. The implementation phase becomes a sub-workflow invocation calling `implement-change.yaml`.

**`workflows/implement-change.yaml`** — For-each loop over task files in `openspec/changes/{{change_name}}/tasks/*.md`, invoking `implement-task.yaml` per task with the task file path as a parameter.

**`workflows/implement-task.yaml`** — Implements a single task: agent step to implement, then a gauntlet retry sub-workflow invocation (`run-gauntlet.yaml`).

**`workflows/run-gauntlet.yaml`** — Counted retry loop (max: 3): shell step running gauntlet with `capture: gauntlet_output` and `break_if: success`, then agent fix step with `session: inherit`, `skip_if: previous_success`, and `continue_on_failure: true`.

### Codebase Conventions

- **Linting:** biome with max 500 lines per file, max 75 lines per function, max cognitive complexity 15
- **Testing:** `bun test` with `bun:test` framework
- **Formatting:** single quotes, always semicolons
- **Types:** strict TypeScript, no emit (Bun's native TypeScript)
- **Dependencies:** `commander`, `yaml`, `zod` — no new runtime deps

### E2E Tests

Create test fixture workflow YAML files in `test/fixtures/` with shell-only steps. E2E tests should invoke `baton run` by spawning `bun src/index.ts run <fixture.yaml>` as a subprocess (using `Bun.spawn`) and verifying exit code, stdout output, and state file contents. Do NOT mock `Bun.spawn` in e2e tests — let real shell commands execute.

Suggested e2e scenarios:
- Shell step with `capture` stores stdout and subsequent step interpolates it
- `continue_on_failure: true` on a failing step allows workflow to proceed
- `skip_if: previous_success` skips a step when the previous succeeded
- `skip_if: previous_success` runs a step when the previous failed (with `continue_on_failure`)
- Successful workflow deletes state file; failed workflow preserves it

## Spec

### Requirement: Shell stdout capture

A shell step with a `capture` field SHALL capture its stdout into a named variable. The captured value is available to subsequent steps via `{{var_name}}` interpolation. Output SHALL be both captured and displayed to the terminal (tee behavior).

#### Scenario: Capture stores stdout
- **WHEN** a shell step has `capture: gauntlet_output` and produces stdout
- **THEN** the stdout is stored in the variable `gauntlet_output` and available via `{{gauntlet_output}}` in subsequent steps

#### Scenario: Tee behavior
- **WHEN** a shell step has `capture: gauntlet_output`
- **THEN** stdout is displayed to the terminal in real time AND captured into the variable

#### Scenario: Captured variable used in subsequent step prompt
- **WHEN** a step's prompt contains `{{gauntlet_output}}` and a prior step captured into `gauntlet_output`
- **THEN** baton interpolates the captured value into the prompt

#### Scenario: Captured variable not set
- **WHEN** a step references `{{gauntlet_output}}` but no prior step captured into that variable
- **THEN** baton fails with a descriptive error naming the undefined variable

#### Scenario: Capture on non-shell step
- **WHEN** an agent step (headless or interactive) has a `capture` field
- **THEN** baton fails at load time with a validation error

### Requirement: Captured variable scope

Captured variables SHALL be available to all subsequent steps within the same scope — sibling steps, nested child steps, and subsequent loop iterations. Captured variables from a sub-workflow are NOT available in the parent workflow after the sub-workflow completes.

#### Scenario: Variable available to sibling steps
- **WHEN** step A captures `output` and step B (a sibling) references `{{output}}`
- **THEN** step B receives the captured value

#### Scenario: Variable available within loop iterations
- **WHEN** a shell step inside a loop captures `output` on iteration 1
- **THEN** `{{output}}` is available in the same iteration's subsequent steps, and is overwritten on each new iteration

#### Scenario: Variable does not leak from sub-workflow to parent
- **WHEN** a sub-workflow captures `internal_var` and the parent step after the sub-workflow references `{{internal_var}}`
- **THEN** baton fails with an undefined variable error

Note: The loop iteration and sub-workflow leak scenarios become fully testable end-to-end once the LoopExecutor and SubWorkflowExecutor are implemented. Cover the sibling-step scenario in this task's e2e tests.

### Requirement: Continue on failure

A step with `continue_on_failure: true` SHALL allow the workflow to proceed to the next step even if the step fails (non-zero exit code for shell steps, non-zero exit for agent steps). The step's outcome (success or failure) is tracked and available to `skip_if` and `break_if` on subsequent steps.

#### Scenario: Failed step with continue_on_failure proceeds
- **WHEN** a shell step has `continue_on_failure: true` and exits with non-zero code
- **THEN** baton records the failure and continues to the next step

#### Scenario: Failed step without continue_on_failure halts
- **WHEN** a shell step does not have `continue_on_failure` and exits with non-zero code
- **THEN** baton stops the workflow

#### Scenario: Successful step with continue_on_failure proceeds normally
- **WHEN** a step has `continue_on_failure: true` and succeeds
- **THEN** baton proceeds to the next step normally

### Requirement: Skip if previous succeeded

A step with `skip_if: previous_success` SHALL be skipped if the immediately preceding step in the same scope succeeded. If the previous step failed (and had `continue_on_failure: true`), the step executes normally.

#### Scenario: Previous step succeeded — skip
- **WHEN** a step has `skip_if: previous_success` and the immediately preceding step succeeded
- **THEN** baton skips the step and continues to the next step

#### Scenario: Previous step failed — execute
- **WHEN** a step has `skip_if: previous_success` and the immediately preceding step failed (with `continue_on_failure: true`)
- **THEN** baton executes the step normally

#### Scenario: skip_if on first step in scope
- **WHEN** the first step in a workflow or loop body has `skip_if: previous_success`
- **THEN** baton fails at load time with a validation error (no previous step to reference)

### Requirement: State file persistence (modified)

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

### Requirement: Captured variables in state

Captured variables SHALL be persisted in the state file alongside session IDs. This allows resume to restore captured values without re-executing the capture step.

#### Scenario: Captured variable persisted
- **WHEN** a shell step captures stdout into `gauntlet_output` and baton writes the state file
- **THEN** the state file includes `gauntlet_output` and its value

#### Scenario: Resume restores captured variables
- **WHEN** baton resumes from a state file that contains captured variables
- **THEN** the captured variables are available for interpolation in subsequent steps

### Requirement: Recursive position tracking

The state file (`baton-state.json`) SHALL track the current execution position through nested workflows and loops recursively.

#### Scenario: Flat workflow state
- **WHEN** a workflow with no loops or sub-workflows completes step 2 of 4
- **THEN** the state file records `currentStep` as the step 2 ID (unchanged from current behavior)

Note: Scenarios for nested sub-workflow state and loop iteration tracking become fully verifiable once the LoopExecutor and SubWorkflowExecutor are implemented.

## Done When

- The dispatcher routes shell steps to ShellExecutor. Agent steps can fall through to the existing inline logic (or a stub AgentExecutor) until the agent executor task is complete.
- Tee capture works: shell steps with `capture` store stdout in `capturedVariables` and display to terminal.
- `continue_on_failure` and `skip_if` work for shell steps.
- Schema validates all new fields with proper constraints.
- State file uses the nested object format while remaining backwards-compatible with flat state files.
- Decomposed workflow files (`implement-change.yaml`, `implement-task.yaml`, `run-gauntlet.yaml`, updated `flokay.yaml`) exist and pass schema validation.
- E2E tests invoke `baton run` on fixture workflows and verify correct execution.
