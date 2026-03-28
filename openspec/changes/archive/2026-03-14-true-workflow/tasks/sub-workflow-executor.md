# Task: Sub-Workflow Executor

## Goal

Implement the SubWorkflowExecutor for invoking external workflow files as steps, with lazy loading, explicit parameter passing, `session: inherit` for crossing sub-workflow boundaries, and session resume scoping that stays within workflow boundaries. E2E tests verify sub-workflow invocation works end-to-end.

## Background

### Sub-Workflow Invocation

A step with a `workflow` field loads and executes the referenced workflow file. The step MUST NOT have `prompt`, `command`, or `mode` — it delegates entirely to the sub-workflow. The sub-workflow executes in the same process as the parent. The `workflow` field supports `{{var}}` interpolation for dynamic paths.

### Lazy Loading

Sub-workflow YAML files are loaded at execution time, not at load time. This handles interpolated paths (e.g., `workflows/{{name}}.yaml`) with a single code path. Missing files and validation errors surface at execution time with descriptive errors. Use the existing `loadWorkflow()` function in `src/loader.ts`.

### Parameter Passing

A step with `workflow` MAY include a `params` map. Values support `{{var}}` interpolation. The sub-workflow receives ONLY the parameters explicitly passed — no implicit inheritance of parent params. Required parameters in the sub-workflow's `params` definition that are missing from the step's `params` map cause an execution-time error.

### Context Scoping

When entering a sub-workflow, create a new `ExecutionContext`:

- `params`: only the explicitly passed params (not the parent's params)
- `sessionIds`: empty
- `capturedVariables`: empty
- `parentContext`: points to the invoking context (for `session: inherit` to walk up)
- `workflowFile`: the sub-workflow's file path
- `nestingPath`: push a new segment for the sub-workflow step

Captured variables from the sub-workflow don't leak to the parent because the child context is discarded on completion.

### Session Inheritance

`session: inherit` walks the `parentContext` chain until crossing a sub-workflow boundary (a context with a different `workflowFile`), then returns that context's most recent session ID. This allows a sub-workflow's agent steps to continue the session chain started in the parent.

The session resolution logic is in `src/shared/session.ts`. The `inherit` strategy needs to:
1. Walk up `parentContext` chain
2. Find the first context with a different `workflowFile`
3. Return the most recent session ID from that context's `sessionIds`
4. Error if no parent session exists

### Session Resume Scoping

`session: resume` only resumes sessions created within the same workflow file. It does NOT reach across sub-workflow boundaries to resume a session from a parent or child workflow. This is enforced by the session resolution logic — `resume` only searches the current context's `sessionIds`, not parent contexts.

### Validation

Validation rules (most already defined in `src/schema.ts`):

- `session: inherit` is invalid in a top-level workflow (no parent to inherit from). For the parent workflow, this is caught at load time. For lazily loaded sub-workflows, it's caught at execution time.
- Missing required sub-workflow parameters fail at execution time with a descriptive error naming the missing parameter.
- Sub-workflow file not found fails at execution time with a descriptive error naming the missing file.
- `workflow` is mutually exclusive with `prompt`/`command`/`mode` (schema validation).

### State File

Entering a sub-workflow pushes a child `NestingSegment` in the state file's `currentStep` chain. The nested state serialization/deserialization in `src/state.ts` handles this format.

### Resume Into a Sub-Workflow

When resuming from a state file that records position inside a sub-workflow, walk the `currentStep` chain, load the sub-workflow file, reconstruct the `ExecutionContext` at each nesting level with persisted `sessionIds` and `capturedVariables`, and resume from the step after the last completed step at the deepest level.

If the sub-workflow file has changed since the state was written and the recorded step ID no longer exists, fail with a descriptive error identifying the missing step and which workflow file changed.

### Key Files

- `src/executors/sub-workflow.ts` — create SubWorkflowExecutor here
- `src/runner.ts` — dispatcher routes `workflow` steps to SubWorkflowExecutor; wire it up
- `src/context.ts` — use the child context factory for sub-workflow entry
- `src/shared/session.ts` — implement `session: inherit` resolution (walk `parentContext` chain)
- `src/loader.ts` — `loadWorkflow()` for loading sub-workflow YAML at execution time
- `src/shared/interpolation.ts` — for resolving `{{var}}` in workflow path and params values

### Codebase Conventions

- **Linting:** biome with max 500 lines per file, max 75 lines per function, max cognitive complexity 15
- **Testing:** `bun test` with `bun:test` framework
- **Formatting:** single quotes, always semicolons

### E2E Tests

Create test fixture workflow YAML files: a parent workflow and a child sub-workflow, both with shell-only steps, in `test/fixtures/`. E2E tests spawn `bun src/index.ts run <parent-fixture.yaml>` and verify correct behavior.

Suggested e2e scenarios:
- Parent invokes sub-workflow with params; sub-workflow shell step echoes the param value
- Captured variables in sub-workflow don't leak to parent (parent step referencing sub-workflow capture fails)
- Missing sub-workflow file produces a descriptive error
- Sub-workflow does not inherit parent params implicitly (sub-workflow referencing parent-only param fails)

## Spec

### Requirement: Sub-workflow invocation

A step with a `workflow` field SHALL load and execute the referenced workflow file. The step MUST NOT have `prompt`, `command`, or `mode` — it delegates entirely to the sub-workflow. The sub-workflow executes in the same process as the parent.

#### Scenario: Sub-workflow executes successfully
- **WHEN** a step has `workflow: workflows/run-gauntlet.yaml` and the referenced file exists
- **THEN** baton loads the sub-workflow, executes its steps, and continues with the next step in the parent

#### Scenario: Sub-workflow file not found
- **WHEN** a step has `workflow: workflows/missing.yaml` and the file does not exist
- **THEN** baton fails with a descriptive error naming the missing file

#### Scenario: Sub-workflow step is mutually exclusive with prompt/command/mode
- **WHEN** a step has both `workflow` and `prompt` (or `command` or `mode`)
- **THEN** baton fails at load time with a validation error

### Requirement: Parameter passing to sub-workflows

A step with `workflow` MAY include a `params` map that passes values to the sub-workflow. Values support `{{var}}` interpolation. The sub-workflow receives only the parameters explicitly passed — it does not implicitly inherit the parent's parameter scope.

#### Scenario: Parameters passed to sub-workflow
- **WHEN** a step has `workflow: workflows/implement-task.yaml` and `params: { task_file: "{{task_file}}" }`
- **THEN** the sub-workflow receives `task_file` as a parameter and can reference it via `{{task_file}}`

#### Scenario: Missing required parameter
- **WHEN** a sub-workflow declares a required parameter and the parent step's `params` map does not include it
- **THEN** baton fails with a descriptive error naming the missing parameter

#### Scenario: Sub-workflow does not inherit parent params implicitly
- **WHEN** the parent workflow has a parameter `change_name` but the step's `params` map does not pass it
- **THEN** the sub-workflow cannot reference `{{change_name}}`

### Requirement: Session inheritance

A step with `session: inherit` SHALL resume the most recent session from the parent workflow that invoked the current sub-workflow. This allows a sub-workflow's agent steps to continue the session chain started in the parent.

#### Scenario: Inherit resumes parent session
- **WHEN** a sub-workflow step has `session: inherit` and the parent workflow has an active session
- **THEN** the step resumes the parent's most recent session

#### Scenario: Inherit with no parent session
- **WHEN** a sub-workflow step has `session: inherit` but no parent workflow session exists
- **THEN** baton fails with a descriptive error

#### Scenario: Inherit in a top-level workflow
- **WHEN** a step in a top-level workflow (not a sub-workflow) has `session: inherit`
- **THEN** baton fails at validation time with a validation error (load time for the parent workflow, execution time for lazily loaded sub-workflows)

Note: Session inheritance scenarios involving agent steps cannot be tested end-to-end (agent steps require claude). Cover these with unit tests using mocked agent execution.

### Requirement: Session resume scoping

`session: resume` SHALL only resume sessions created within the same workflow file. It MUST NOT reach across sub-workflow boundaries to resume a session from a parent or child workflow.

#### Scenario: Resume finds session in same workflow
- **WHEN** a step has `session: resume` and a prior step in the same workflow file created a session
- **THEN** the step resumes that session

#### Scenario: Resume with no prior session in same workflow
- **WHEN** a step has `session: resume` but no prior step in the same workflow file created a session
- **THEN** baton fails with a descriptive error

#### Scenario: Resume does not cross sub-workflow boundary
- **WHEN** a parent workflow invokes a sub-workflow that created sessions, and the next parent step has `session: resume`
- **THEN** the parent step resumes the parent's own most recent session, not the sub-workflow's

Note: Session resume scoping scenarios involving agent steps cannot be tested end-to-end. Cover these with unit tests using mocked agent execution.

### Requirement: Recursive position tracking (sub-workflow)

The state file SHALL track position through sub-workflow nesting.

#### Scenario: Nested sub-workflow state
- **WHEN** execution is inside step `run-gauntlet` of `implement-task.yaml`, which is itself inside the `implement-tasks` loop of `implement-change.yaml`, on the `gauntlet` step
- **THEN** the state file captures the full path: implement-change → implement-tasks (iteration index) → implement-task → run-gauntlet → gauntlet

### Requirement: Resume from nested position (sub-workflow)

`baton resume` SHALL restore execution to the exact nested position recorded in the state file, including sub-workflow depth.

#### Scenario: Resume into a sub-workflow
- **WHEN** the state file records position inside a sub-workflow at step 2 of 3
- **THEN** baton resumes inside the sub-workflow at step 2, within the parent's context

#### Scenario: Resume with stale nested state
- **WHEN** the sub-workflow file has changed since the state was written and the recorded step ID no longer exists
- **THEN** baton fails with a descriptive error identifying the missing step and which workflow file changed

## Done When

- SubWorkflowExecutor loads and executes sub-workflow files at runtime.
- Parameter passing works with `{{var}}` interpolation; sub-workflows receive only explicit params.
- `session: inherit` walks the parent context chain to find the parent session.
- `session: resume` stays within workflow boundaries.
- Missing files and parameters produce descriptive errors.
- State file tracks sub-workflow nesting; resume restores to correct depth.
- Stale state detection works when sub-workflow files change.
- E2E tests invoke `baton run` on fixture workflows with parent→sub-workflow invocation (shell steps) and verify parameter passing, variable isolation, and error handling.
