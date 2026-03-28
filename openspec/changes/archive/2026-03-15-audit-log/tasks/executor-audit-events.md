# Task: Agent, loop, and sub-workflow audit events

## Goal

Extend audit logging to the agent, loop, and sub-workflow executors. Each executor emits paired step_start/step_end events with its type-specific data. Loop executor additionally emits iteration_start/iteration_end. Sub-workflow executor additionally emits sub_workflow_start/sub_workflow_end.

## Background

### Architecture

AuditLogger is carried on `ExecutionContext` as `auditLogger: AuditLogger | null`. Executors emit events via `context.auditLogger?.emit()` (null-safe). The nesting prefix is built by calling `buildPrefix(context, stepId)` which walks `context.nestingPath`. See `src/audit.ts` for the AuditLogger class, buildPrefix() helper, and event types. See `src/context.ts` for the auditLogger field and NestingSegment with `subWorkflowName`.

### Audit event pattern

Each executor emits a `step_start` event (with type-specific data and a context snapshot of params + capturedVariables) before doing its work, and a `step_end` event (with outcome, duration_ms, and type-specific result data) after. Return types are unchanged — audit logging is a side effect via the logger on context.

### Agent executor: src/executors/agent.ts

**step_start data:** interpolated prompt, mode (`interactive`/`headless`), session strategy (`new`/`resume`/`inherit`), resolved session ID (from `resolveSessionId()`), model (from `step.model`), engine enrichment (from `engine.enrichPrompt()`), context snapshot.

All this data is already computed inside the executor. The prompt comes from `buildPrompt()`, the session ID from `resolveSessionId()`, the enrichment from `engine.enrichPrompt()`. Capture these values before they're passed to `Bun.spawn()`.

**step_end data:** exit code, discovered session ID (from `discoverAndStoreSession()`), outcome, duration_ms.

No I/O changes — agent stdout/stderr remain inherited.

### Loop executor: src/executors/loop.ts

**step_start data:** loop type (`counted`/`for-each`), max (counted) or glob pattern + resolved matches (for-each), context snapshot.

**iteration_start / iteration_end:** emitted inside `executeCountedLoop()` and `executeForEachLoop()`, wrapping each call to `executeIterationBody()`. Include iteration index, loop variable value (for for-each loops), context snapshot (on start), outcome and duration_ms (on end).

**step_end data:** iterations completed, break_triggered (boolean), outcome, duration_ms.

The iteration events use the iteration context's nesting path for prefix building (it includes the loop segment with iteration index).

### Sub-workflow executor: src/executors/sub-workflow.ts

**step_start data:** resolved workflow path (from `resolveWorkflowPath()`), interpolated params (from `resolveParams()`), context snapshot.

**sub_workflow_start:** emitted after child context is created, before executing child steps. Uses child context for prefix.

**sub_workflow_end:** emitted after all child steps complete. Includes outcome, duration_ms.

**step_end data:** outcome, duration_ms.

Update the `createSubWorkflowContext()` call to pass `workflow.name` as `subWorkflowName` so the nesting segment includes it for prefix building.

### Key files

- `src/executors/agent.ts` — add step_start/step_end audit events
- `src/executors/loop.ts` — add step_start/step_end + iteration_start/iteration_end audit events
- `src/executors/sub-workflow.ts` — add step_start/step_end + sub_workflow_start/sub_workflow_end audit events
- `src/audit.ts` — import buildPrefix (already exists)
- `test/agent-executor.test.ts` — verify agent audit events
- `test/loop-executor.test.ts` — verify loop + iteration audit events
- `test/sub-workflow-executor.test.ts` — verify sub-workflow audit events
- E2e tests — verify full event sequence for workflows with all step types

### Testing approach

**Use the audit log as a verification tool in e2e tests.** Instead of only checking side effects, read the log file and verify the event sequence — this makes assertions more expressive. For example, an e2e test for a loop workflow can verify iteration_start/iteration_end events appeared for each iteration with the right prefixes.

**Unit tests:** inject a spy/mock logger via context, verify each executor emits correct events with correct type-specific data.

### Conventions

- Test framework: `bun:test`
- Tests use `spyOn(Bun, 'spawn')` to mock subprocess execution
- Tests use temp directories for file artifacts, cleaned up in `afterEach`
- Helper functions `makeWorkflow()`, `makeStep()`, `makeMockProc()` are defined per test file

## Spec

### Requirement: Agent step-specific data

Agent step entries SHALL include the interpolated prompt, mode, session strategy, resolved session ID, model, and engine enrichment on `step_start`. The `step_end` SHALL include exit code and discovered session ID.

#### Scenario: Agent step start
- **WHEN** a headless agent step starts with session strategy `resume` and resolved session ID `abc-123`
- **THEN** the `step_start` entry includes prompt, mode, session strategy, resolved session ID, model, and enrichment

#### Scenario: Agent step end
- **WHEN** an agent step completes and baton discovers session ID `def-456`
- **THEN** the `step_end` entry includes exit code and discovered session ID `def-456`

### Requirement: Loop step-specific data

Loop step `step_start` entries SHALL include the loop type (counted or for-each), max count or glob pattern with resolved matches. Loop `step_end` entries SHALL include iterations completed and whether a break was triggered.

#### Scenario: Counted loop start
- **WHEN** a counted loop step starts with `max: 5`
- **THEN** the `step_start` entry includes loop type `counted` and `max: 5`

#### Scenario: For-each loop start
- **WHEN** a for-each loop starts with glob `tasks/*.md` resolving to 3 files
- **THEN** the `step_start` entry includes loop type `for-each`, glob pattern, and resolved matches

#### Scenario: Loop end with break
- **WHEN** a loop completes after 3 iterations due to a break_if trigger
- **THEN** the `step_end` entry includes `iterations_completed: 3` and `break_triggered: true`

### Requirement: Iteration-level events

Baton SHALL emit `iteration_start` before executing a loop iteration's child steps and `iteration_end` after all child steps in that iteration complete.

#### Scenario: Loop with 3 iterations
- **WHEN** a counted loop executes 3 iterations
- **THEN** baton emits 3 `iteration_start` / `iteration_end` pairs, nested between the loop step's `step_start` and `step_end`

#### Scenario: Iteration fails
- **WHEN** a child step in iteration 2 fails
- **THEN** baton emits `iteration_end` for iteration 2 with outcome `failed`

### Requirement: Sub-workflow step-specific data

Sub-workflow step `step_start` entries SHALL include the resolved workflow path and interpolated params passed.

#### Scenario: Sub-workflow start
- **WHEN** a sub-workflow step starts with resolved path `workflows/verify.yaml` and params `{task: "tasks/1.md"}`
- **THEN** the `step_start` entry includes the path and params

### Requirement: Sub-workflow-level events

Baton SHALL emit `sub_workflow_start` before executing a sub-workflow's steps and `sub_workflow_end` after all sub-workflow steps complete, nested between the sub-workflow step's `step_start` and `step_end`.

#### Scenario: Sub-workflow executes
- **WHEN** a sub-workflow step invokes `verify-task.yaml`
- **THEN** baton emits `sub_workflow_start`, then child step events, then `sub_workflow_end`, all nested within the step's `step_start` / `step_end`

### Requirement: Context snapshot on start events

Start events (`run_start`, `step_start`, `iteration_start`, `sub_workflow_start`) SHALL include the full context snapshot: all params and all captured variables available at that point.

#### Scenario: Step start includes params and captured variables
- **WHEN** a `step_start` event is emitted and the context has params `{env: "staging"}` and captured variables `{build_output: "/tmp/build"}`
- **THEN** the entry includes both in the context snapshot

#### Scenario: End events omit context snapshot
- **WHEN** a `step_end` event is emitted
- **THEN** the entry does not include a context snapshot

### Requirement: End event data

End events (`step_end`, `run_end`, `iteration_end`, `sub_workflow_end`) SHALL include the outcome (`success`, `failed`, `aborted`, `exhausted`, `skipped`) and duration in milliseconds.

#### Scenario: Step end includes outcome and duration
- **WHEN** a step completes after 1500ms with outcome `success`
- **THEN** the `step_end` entry includes `outcome: "success"` and `duration_ms: 1500`

## Done When

- Agent executor emits step_start/step_end with prompt, mode, session strategy, resolved session ID, model, enrichment, exit code, and discovered session ID
- Loop executor emits step_start/step_end with loop type, max/glob+matches, iterations completed, break_triggered, plus iteration_start/iteration_end for each iteration
- Sub-workflow executor emits step_start/step_end with resolved path and params, plus sub_workflow_start/sub_workflow_end around child execution
- All nesting prefixes are correct for nested scenarios (loop + sub-workflow)
- All existing tests pass
- New unit tests verify each executor's audit events via spy logger
- E2e tests verify full event sequence for workflows using all step types
