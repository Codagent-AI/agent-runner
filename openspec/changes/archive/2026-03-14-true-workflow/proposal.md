## Why

Baton executes workflows as a flat sequence of steps. This works for linear lifecycles but breaks down for inherently iterative patterns — the implement-and-verify cycle requires loops, and complex workflows need composition. Today these loops live inside agent skills, which violates baton's core thesis: **agents are good at execution, bad at orchestration**. Moving control flow into baton makes it deterministic, visible, and debuggable.

## What Changes

- Add **loop** primitives: counted loops (`max: N`) and for-each loops (`over` glob, `as` binding variable), with `break_if` for early exit
- Add **nested steps**: steps can contain child steps, enabling loop bodies and logical grouping
- Add **sub-workflow invocation**: a step can invoke another workflow file with explicit parameter passing
- Add **`session: inherit`**: new session strategy that resumes the parent workflow's session into a sub-workflow
- Add **output capture**: shell steps can capture stdout into named variables for use in subsequent steps
- Add **`continue_on_failure`**: steps can tolerate failure without halting the workflow
- Add **`skip_if`**: skip a step based on the previous step's outcome
- Add **`model`**: per-step model override (e.g., `model: sonnet`)
- **BREAKING**: `baton-state.json` becomes recursive — tracks nested execution position through sub-workflows and loop iterations
- Extend template interpolation to resolve captured variables alongside params
- Add glob expansion at runtime for for-each loop patterns

## Capabilities

### New Capabilities
- `workflow-loops`: Counted loops (`loop: { max: N }`), for-each loops (`loop: { over, as }`), nested steps, and `break_if` for conditional loop exit
- `sub-workflows`: Invoking external workflow files as steps with explicit parameter passing via `params` map, plus `session: inherit` for resuming the parent workflow's session
- `output-capture`: Capturing shell stdout into named variables (`capture: var_name`) with tee behavior (captured and displayed)
- `step-flow-control`: `continue_on_failure` for failure tolerance and `skip_if` for conditional step skipping
- `step-model`: Per-step `model` field to override the default agent model
- `recursive-state`: Recursive state tracking in `baton-state.json` so nested workflow/loop positions are fully captured and resumable

### Modified Capabilities
- `engine-interface`: State file structure changes from flat `currentStep: string` to recursive nesting that tracks position through sub-workflows and loop iterations

## Impact

- **`src/schema.ts`**: New optional fields on `StepSchema` (`steps`, `workflow`, `loop`, `capture`, `continue_on_failure`, `break_if`, `skip_if`, `model`); new `LoopSchema`; `session: inherit` added to `SessionStrategy`; recursive step validation
- **`src/runner.ts`**: Refactored into a slim dispatcher (~150 lines) that routes steps to per-type executors in `src/executors/`. New files: `executors/shell.ts`, `executors/agent.ts`, `executors/loop.ts`, `executors/sub-workflow.ts`. Shared logic extracted to `src/shared/` (flow-control, session resolution, interpolation). New `src/context.ts` for ExecutionContext
- **`src/loader.ts`**: Interpolation extended to include captured variables; glob expansion for `loop.over`
- **`src/state.ts`**: `RunState` becomes recursive to track nested execution position; captured variables stored alongside session IDs
- **`workflows/`**: Decomposed into sub-workflows (`implement-change.yaml`, `implement-task.yaml`, `run-gauntlet.yaml`) that use the new primitives
