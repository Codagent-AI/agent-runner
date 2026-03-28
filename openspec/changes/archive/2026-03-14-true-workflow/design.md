## Context

Baton is a TypeScript/Bun CLI that orchestrates Claude agent sessions through linear YAML workflows. Today, the runner is a flat `for` loop over `workflow.steps`, state tracks a single `currentStep` string, and sessions support `new`/`resume` within that flat list. The runner is ~380 lines in a single `runner.ts`.

This change adds loops (counted + for-each), nested steps, sub-workflows with parameter passing, output capture, flow control (`continue_on_failure`, `skip_if`, `break_if`), `session: inherit`, and per-step model override. The core challenge is turning the flat execution model into a recursive one while keeping the state file resumable and the session model coherent across nesting boundaries.

## Goals / Non-Goals

**Goals:**
- Support recursive step execution (loops, nested steps, sub-workflows) without losing resumability
- Keep session scoping correct across nesting boundaries (`resume` stays in-scope, `inherit` crosses sub-workflow boundaries)
- Maintain backwards compatibility with existing flat workflows and state files
- Keep the codebase modular — each step type has clear ownership

**Non-Goals:**
- Parallel step execution (all execution remains sequential)
- Dynamic workflow modification at runtime
- Cross-workflow session sharing beyond explicit `inherit`

## Decisions

### 1. Dispatcher + 4 Step Executors (Strategy Pattern)

The runner dispatches each step to one of 4 executors based on which mutually exclusive field is present:

- `command` → **ShellExecutor** — spawn, tee capture, exit code
- `prompt`/`mode` → **AgentExecutor** — session resolution, claude spawning, enrichment
- `loop` + `steps` → **LoopExecutor** — counted and for-each iteration, break handling
- `workflow` → **SubWorkflowExecutor** — lazy load, param scoping, session inherit

A step with `steps` but no `loop` is a bare group — the dispatcher iterates its children directly with the same context.

Shared logic (interpolation, state writing, flow control evaluation) is extracted into common utilities so executors stay DRY.

### 2. ExecutionContext (Immutable Copy-on-Enter)

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

**Scope transitions use immutable copy-on-enter:**

- **Loop iteration:** Child copies parent's `params` (adding loop variable), gets empty `sessionIds` and `capturedVariables`, pushes a new segment onto `nestingPath`. `parentContext` points to the parent.
- **Sub-workflow:** Child gets only the explicitly passed `params` (no implicit inheritance per spec). Empty `sessionIds` and `capturedVariables`. `parentContext` points to the invoking context so `session: inherit` can walk up.
- **Group (nested steps without loop):** Same context, no copy — siblings share scope.

Captured variables from sub-workflows don't leak to parents because the child context is discarded on completion.

### 3. Session Resolution on the Context

Session lookup strategies:

- **`session: new`** — no flag, start fresh. Store new conversation ID in current context's `sessionIds`.
- **`session: resume`** — scan `sessionIds` in the current context for the most recent entry. Error if none found (spec requirement).
- **`session: inherit`** — walk the `parentContext` chain until crossing a sub-workflow boundary (different `workflowFile`), return that context's most recent session ID. Error if no parent session exists.

### 4. Nested Object State File

The state file uses a nested object structure where each node owns its scope's session IDs and captured variables:

```json
{
  "workflowFile": "workflows/implement-change.yaml",
  "workflowName": "implement-change",
  "params": { "change_name": "true-workflow" },
  "workflowHash": "abc123...",
  "currentStep": {
    "stepId": "implement-tasks",
    "iteration": 1,
    "loopVar": { "task_file": "002-add-runner.task.md" },
    "sessionIds": {},
    "capturedVariables": {},
    "child": {
      "stepId": "run-gauntlet",
      "iteration": 1,
      "sessionIds": { "implement": "conv-B" },
      "capturedVariables": {},
      "child": {
        "stepId": "gauntlet",
        "sessionIds": {},
        "capturedVariables": { "gauntlet_output": "FAIL: 2 issues..." }
      }
    }
  }
}
```

**Resume:** Walk the `currentStep` chain from root to leaf, reconstruct `ExecutionContext` at each level with persisted state, resume from the step after the leaf. Existing flat state files (no `child`) work as single-level nesting.

### 5. Lazy Sub-Workflow Loading

Sub-workflow YAML files are loaded at execution time, not at load time. This handles interpolated paths (e.g., `workflows/{{name}}.yaml`) with a single code path. Missing files and validation errors surface at execution time with descriptive errors.

### 6. Flow Control

- **`continue_on_failure`** — on step failure, record `lastStepOutcome: 'failed'` on context and proceed instead of halting.
- **`skip_if: previous_success`** — check `context.lastStepOutcome` before executing; skip if `'success'`.
- **`break_if: success|failure`** — evaluated after step execution. If condition matches outcome, executor returns `{ outcome, shouldBreak: true }`. The enclosing LoopExecutor sees `shouldBreak` and exits the loop.

`lastStepOutcome` lives on the context (scope-local). `shouldBreak` propagates via return value to the LoopExecutor.

### 7. Headless Mode SIGINT Handling

When a headless agent step is running, the AgentExecutor registers a SIGINT handler before spawning the subprocess. If the user presses ctrl-c:

1. The handler calls `proc.kill()` on the spawned agent subprocess.
2. The normal exit flow runs — the step is recorded as failed, and the state file is written with the current step position (so `baton resume` works).
3. Baton exits with a non-zero exit code.

The handler is removed after the process exits normally. Interactive mode does not need special SIGINT handling because the user controls the terminal directly.

### 8. Tee Capture for Shell Steps

When a shell step has `capture`, stdout is piped instead of inherited: each chunk is written to `process.stdout` (terminal) and appended to a buffer. On exit, the trimmed buffer is stored in `context.capturedVariables`. When `capture` is absent, `stdout: 'inherit'` is used with no overhead. `stderr` is always inherited.

### 9. Validation Timing

Validation happens at two points:

**Validation time (load time for parent workflow, execution time for lazily loaded sub-workflows):**
- Mutually exclusive step fields
- `loop` requires `max` or `over`+`as`
- `capture` only on shell steps
- `model` only on agent steps
- `break_if` only inside a loop body
- `skip_if` not on first step in scope
- `session: inherit` not in top-level workflow

**Execution time:**
- Sub-workflow file exists and parses
- Required sub-workflow parameters present
- `session: resume` has a prior session in scope
- `session: inherit` has a parent session
- `{{var}}` references resolve to defined values
- Glob expansion (zero matches = skip, not error)

### 10. File Organization

```
src/
  runner.ts              → dispatcher + orchestration entry point (slimmed)
  context.ts             → ExecutionContext interface + child context factories
  state.ts               → extended with nested serialization/deserialization
  schema.ts              → extended with recursive Step schema + new fields
  loader.ts              → existing interpolation logic
  executors/
    shell.ts             → ShellExecutor
    agent.ts             → AgentExecutor
    loop.ts              → LoopExecutor
    sub-workflow.ts      → SubWorkflowExecutor
  shared/
    flow-control.ts      → skip_if, break_if, continue_on_failure evaluation
    session.ts           → session resolution logic (new/resume/inherit)
    interpolation.ts     → extracted from loader.ts, shared by all executors
```

## Risks / Trade-offs

- **Nested state complexity:** The nested object state is more complex to serialize/deserialize than a flat structure, but it directly mirrors the execution model and makes resume straightforward.
- **Lazy loading delays errors:** Sub-workflow validation errors surface at execution time rather than upfront. Accepted because interpolated paths make eager loading incomplete anyway, and one code path is simpler than two.
- **Copy-on-enter allocations:** Each loop iteration and sub-workflow entry creates a new context object. Nesting depth is shallow in practice (2-3 levels), so this is negligible.
- **Counted loop exhaustion = failure:** Running all N iterations without a `break_if` triggering fails the workflow. This is by spec design — counted loops are retry loops, not iteration loops. For-each loops are for iteration.

## Migration Plan

- Existing flat workflows and state files continue to work — a `currentStep` with no `child` is a single-level nesting path.
- The Zod schema is extended (new optional fields), not changed — existing workflow YAML files validate without modification.
- No changes to the engine interface — engines continue to implement the same 4 optional hooks.
- Rollback: revert to the previous runner. State files written by the new version with nesting cannot be resumed by the old version, but the old flat format is still understood by the new version.

## Open Questions

None — all architectural decisions have been resolved through the design conversation.
