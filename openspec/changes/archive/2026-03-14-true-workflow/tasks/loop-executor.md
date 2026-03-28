# Task: Loop Executor

## Goal

Implement the LoopExecutor for counted loops (`loop: { max: N }`) and for-each loops (`loop: { over, as }`), including glob expansion, `break_if` handling, nested steps (bare groups), and session scoping within loops. E2E tests verify loop workflows run correctly end-to-end.

## Background

### Loop Types

**Counted loops** (`loop: { max: N }` + `steps` array): Execute child steps sequentially, then repeat, up to N total iterations. If no `break_if` triggers during any iteration, exhaustion = workflow failure. Counted loops are retry loops (e.g., "try gauntlet up to 3 times"), not iteration loops.

**For-each loops** (`loop: { over: <glob>, as: <var> }` + `steps` array): Expand the glob pattern at runtime, execute child steps once per match. Each match is bound to the variable named in `as`, available via `{{var}}` interpolation in child steps. Successful completion of all iterations = success.

### Execution Model

The LoopExecutor receives a step with `loop` + `steps` and an `ExecutionContext`. For each iteration:

1. Create a child context using the copy-on-enter pattern: child copies parent's `params` (adding loop variable for for-each), gets empty `sessionIds` and `capturedVariables`, pushes a new `NestingSegment` onto `nestingPath` with `iteration` index and `loopVar` (if for-each). `parentContext` points to the parent.
2. Execute child steps sequentially using the dispatcher.
3. After each child step, check for `shouldBreak` signal from `break_if`. If triggered, exit the loop and continue with the next step after the loop.

### ExecutionContext

The `ExecutionContext` interface and child context factories are in `src/context.ts`. Use the factory for creating child contexts per loop iteration:

```typescript
interface ExecutionContext {
  params: Record<string, string>
  sessionIds: Record<string, string>
  capturedVariables: Record<string, string>
  lastStepOutcome: 'success' | 'failed' | null
  nestingPath: NestingSegment[]
  parentContext: ExecutionContext | null
  workflowFile: string
  engine: Engine | null
}

interface NestingSegment {
  stepId: string
  iteration?: number
  loopVar?: Record<string, string>
}
```

### break_if

A step with `break_if: success` or `break_if: failure` is evaluated after the step executes. If the condition matches the step's outcome, return `{ outcome, shouldBreak: true }`. The LoopExecutor sees `shouldBreak` and exits the loop. Execution continues with the next step after the loop.

Add `break_if` evaluation to `src/shared/flow-control.ts` (which already has `continue_on_failure` and `skip_if` evaluation).

### Glob Expansion

For-each loops expand `loop.over` at runtime. The glob pattern supports `{{param}}` interpolation — resolve before expansion using the interpolation utility in `src/shared/interpolation.ts`. Use Bun's native `Bun.Glob` for pattern matching. Zero matches = skip the loop body entirely (not an error).

### Bare Groups

A step with `steps` but no `loop` is a bare group — the dispatcher iterates its children directly with the same context (no copy-on-enter, siblings share scope). This should be handled in the dispatcher in `src/runner.ts`, not in the LoopExecutor.

### Session Scoping Within Loops

Each loop iteration gets fresh `sessionIds` (empty). `session: new` creates a fresh session on the first agent step of each iteration. `session: resume` resumes the most recent session within the current iteration only. Session resolution logic is in `src/shared/session.ts`.

### State File

Loop iteration tracking uses the nested state format already implemented in `src/state.ts`. Each loop iteration pushes a `NestingSegment` with `stepId`, `iteration` index, and `loopVar`.

### Resume Into a Loop

When resuming from a state file that records position inside a loop, the LoopExecutor must skip already-completed iterations. Walk the `currentStep` chain from the state file to find the recorded iteration index, then resume from that iteration.

### Key Files

- `src/executors/loop.ts` — create LoopExecutor here
- `src/runner.ts` — dispatcher routes `loop` + `steps` to LoopExecutor; wire it up. Also handle bare groups (steps without loop) in the dispatcher.
- `src/shared/flow-control.ts` — add `break_if` evaluation alongside existing `continue_on_failure` and `skip_if`
- `src/context.ts` — use the child context factory for loop iterations
- `src/shared/interpolation.ts` — for resolving `{{var}}` in glob patterns
- `src/schema.ts` — loop validation rules are already defined; verify they work

### Codebase Conventions

- **Linting:** biome with max 500 lines per file, max 75 lines per function, max cognitive complexity 15
- **Testing:** `bun test` with `bun:test` framework
- **Formatting:** single quotes, always semicolons

### E2E Tests

Create test fixture workflow YAML files with shell-only loop steps in `test/fixtures/`. E2E tests spawn `bun src/index.ts run <fixture.yaml>` as a subprocess and verify exit code and output.

Suggested e2e scenarios:
- For-each loop iterates over matched files (create temp files, loop over them with a shell step that echoes the filename)
- Counted loop with `break_if: success` exits early on the iteration where the condition is met
- Counted loop without `break_if` triggering fails the workflow (exhaustion)
- Glob with zero matches skips the loop body
- Glob pattern with `{{param}}` interpolation resolves before expansion
- Nested steps (bare group) execute sequentially

## Spec

### Requirement: Counted loop execution

A step with `loop: { max: N }` and a `steps` array SHALL execute its child steps sequentially, then repeat from the first child step, up to N total iterations. The loop body is the `steps` array.

#### Scenario: Loop runs to completion
- **WHEN** a step has `loop: { max: 3 }` and no `break_if` triggers during any iteration
- **THEN** baton executes the loop body 3 times, then fails the workflow (exhaustion)

#### Scenario: Loop with break_if exits early
- **WHEN** a step has `loop: { max: 3 }` and a child step's `break_if` triggers on iteration 2
- **THEN** baton executes 2 iterations, exits the loop, and continues with the next step after the loop

#### Scenario: Max is required for counted loops
- **WHEN** a step has `loop: {}` with no `max` and no `over`
- **THEN** baton fails at load time with a validation error

### Requirement: For-each loop execution

A step with `loop: { over: <glob>, as: <var> }` and a `steps` array SHALL expand the glob pattern at runtime, then execute the child steps once per match. Each match is bound to the variable named in `as`, available via `{{var}}` interpolation in child steps.

#### Scenario: Glob matches multiple files
- **WHEN** a step has `loop: { over: "tasks/*.task.md", as: task_file }` and the glob matches 3 files
- **THEN** baton executes the loop body 3 times, binding `{{task_file}}` to each matched path in order

#### Scenario: Glob matches zero files
- **WHEN** a step has `loop: { over: "tasks/*.task.md", as: task_file }` and the glob matches no files
- **THEN** baton skips the loop body and continues with the next step after the loop

#### Scenario: Glob pattern supports parameter interpolation
- **WHEN** a step has `loop: { over: "openspec/changes/{{change_name}}/tasks/*.task.md", as: task_file }`
- **THEN** baton interpolates `{{change_name}}` before expanding the glob

### Requirement: Nested steps

A step with a `steps` array SHALL execute its child steps sequentially. Child steps inherit the parent's parameter scope, including loop variables and captured variables from prior sibling steps. Nesting is supported to arbitrary depth.

#### Scenario: Steps nested without a loop
- **WHEN** a step has a `steps` array but no `loop` field
- **THEN** baton executes the child steps sequentially as a group

#### Scenario: Child steps inherit parent scope
- **WHEN** a parent step binds `{{task_file}}` via a for-each loop and a child step references `{{task_file}}`
- **THEN** the child step receives the interpolated value

### Requirement: break_if

A step within a loop body with `break_if: success` or `break_if: failure` SHALL be evaluated after the step executes. If the condition matches the step's outcome, baton SHALL exit the enclosing loop immediately, skipping any remaining steps in the current iteration. Execution continues with the next step after the loop.

#### Scenario: break_if success triggers on passing step
- **WHEN** a shell step has `break_if: success` and exits with code 0
- **THEN** baton exits the enclosing loop and skips remaining steps in this iteration

#### Scenario: break_if success does not trigger on failing step
- **WHEN** a shell step has `break_if: success` and exits with non-zero code
- **THEN** baton continues to the next step in the loop body

#### Scenario: break_if outside a loop
- **WHEN** a step has `break_if` but is not inside a loop
- **THEN** baton fails at validation time with a validation error (load time for the parent workflow, execution time for lazily loaded sub-workflows)

### Requirement: Session scoping within loops

Within a loop iteration, all agent steps share one session chain. `session: new` (the default) creates a fresh session on the first agent step of each iteration. `session: resume` resumes the most recent session within the current iteration.

#### Scenario: New session per outer loop iteration
- **WHEN** a for-each loop iterates over tasks and the first agent step has `session: new`
- **THEN** each iteration starts a fresh session, independent of previous iterations

#### Scenario: Session chains within an iteration
- **WHEN** multiple agent steps within a single loop iteration use `session: resume`
- **THEN** each resume extends the same session chain started by the first agent step in that iteration

#### Scenario: Inner loop resumes across iterations
- **WHEN** a `session: inherit` step runs in iteration 2 of an inner loop
- **THEN** it resumes the session from iteration 1's last agent step, forming a chain

Note: Session scoping scenarios involving agent steps cannot be tested end-to-end (agent steps require claude). Cover these with unit tests using mocked agent execution.

### Requirement: Recursive position tracking (loop iteration)

The state file SHALL track loop iteration position.

#### Scenario: Loop iteration tracking
- **WHEN** execution is on iteration 2 of a for-each loop
- **THEN** the state file records the current iteration index and the loop variable's current value

### Requirement: Resume from nested position (loop)

`baton resume` SHALL restore execution to the exact loop iteration recorded in the state file.

#### Scenario: Resume into a loop
- **WHEN** the state file records position inside a for-each loop at iteration 3 of 5
- **THEN** baton resumes at iteration 3, skipping iterations 1 and 2

## Done When

- LoopExecutor handles both counted and for-each loops.
- Glob expansion works with parameter interpolation; zero matches skip the loop.
- `break_if` exits the enclosing loop on condition match.
- Counted loop exhaustion (no break triggered) fails the workflow.
- Bare groups (steps without loop) execute children sequentially with shared scope.
- Loop iterations get isolated session and capture scopes via copy-on-enter.
- State file tracks loop iteration position; resume skips completed iterations.
- E2E tests invoke `baton run` on fixture workflows with shell-step loops and verify correct behavior.
