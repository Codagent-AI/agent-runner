## Architecture

AuditLogger is a class carried on ExecutionContext (same pattern as `engine`). Each executor emits its own events directly via `context.auditLogger.emit()` — no event bus, no return type changes. The runner handles run-level events and crash handling.

```
runner.ts
  ├── Creates AuditLogger, adds to context
  ├── Registers crash handler
  ├── Emits run_start / run_end
  ├── Emits step_start/step_end for skipped steps
  └── Dispatches to executors (unchanged routing)

executors (shell, agent, loop, sub-workflow)
  └── Each emits its own step_start/step_end with type-specific data
      Loop also emits iteration_start/iteration_end
      Sub-workflow also emits sub_workflow_start/sub_workflow_end
```

## New File: src/audit.ts

### AuditLogger class

```typescript
class AuditLogger {
  private fd: number;

  constructor(logFilePath: string)  // mkdirSync + openSync(append)
  emit(event: AuditEvent): void    // formatLine + writeSync (flush-on-write)
  close(): void                     // closeSync
}
```

Uses `openSync` with append flags and `writeSync` for each line. Sync I/O guarantees flush-on-write with zero complexity. Performance is irrelevant — steps take seconds to minutes.

### AuditEvent type

```typescript
interface AuditEvent {
  timestamp: string;      // ISO-8601
  prefix: string;         // nesting prefix, e.g. "[task-loop:0, implement]"
  event: EventType;       // run_start, step_end, etc.
  data: Record<string, unknown>; // JSON payload (context, type-specific, outcome)
}
```

### Line format

```
2026-03-15T18:30:00Z [validate] step_start {"command":"npm test","context":{...}}
2026-03-15T18:30:02Z [validate] step_end {"outcome":"success","duration_ms":2000,"exit_code":0,"stderr":""}
2026-03-15T18:31:00Z run_end {"outcome":"success","duration_ms":60000}
```

Run-level events (`run_start`, `run_end`, `error`) have no prefix. All others carry the full nesting prefix.

### buildPrefix() helper

Walks `context.nestingPath` and appends the current step ID:

| Segment | Prefix token |
|---------|-------------|
| Loop iteration (`iteration !== undefined`) | `stepId:iteration` |
| Sub-workflow (`subWorkflowName` set) | `stepId, sub:subWorkflowName` |
| Other (group) | `stepId` |
| Current step (appended last) | `stepId` |

Examples:
- `nestingPath: []`, step `validate` → `[validate]`
- `nestingPath: [{stepId: "task-loop", iteration: 0}]`, step `implement` → `[task-loop:0, implement]`
- `nestingPath: [{stepId: "task-loop", iteration: 0}, {stepId: "verify", subWorkflowName: "verify-task"}]`, step `check` → `[task-loop:0, verify, sub:verify-task, check]`

### Log file path

```
~/.baton/projects/{encoded-cwd}/logs/{workflow-name}-{timestamp}.log
```

Path encoding: replace `/`, `.`, `_` with `-` (matches Claude Code convention). Timestamp uses ISO-8601 with colons replaced by dashes for filesystem safety.

### createAuditLogger() factory

Exported function that computes the log path from workflow name + cwd, creates directories, and returns an `AuditLogger` instance. Called by `runner.ts` at the start of `runWorkflow()`.

## Changes to context.ts

### ExecutionContext

Add `auditLogger` field:

```typescript
interface ExecutionContext {
  // ... existing fields ...
  auditLogger: AuditLogger | null;
}
```

Null when testing without audit (keeps tests simple). All `context.auditLogger?.emit()` calls are null-safe.

### NestingSegment

Add `subWorkflowName` field:

```typescript
interface NestingSegment {
  stepId: string;
  iteration?: number;
  loopVar?: Record<string, string>;
  subWorkflowName?: string;  // NEW
}
```

### Context creation functions

- `createRootContext()`: accepts optional `auditLogger`, stores it
- `createLoopIterationContext()`: inherits `auditLogger` from parent
- `createSubWorkflowContext()`: inherits `auditLogger` from parent, accepts `subWorkflowName` in options, stores it on the nesting segment

## Changes to runner.ts

### runWorkflow()

After validation, before the step loop:
1. Call `createAuditLogger(workflow.name)` to get logger instance
2. Pass logger to `createRootContext()`
3. Register crash handler via `process.on('uncaughtException')` and `process.on('unhandledRejection')`
4. Emit `run_start` with workflow file, name, hash, params, and resume info if applicable
5. Wrap the step loop in try/finally — emit `run_end` in the finally block, then `logger.close()`

### dispatchStep()

For skipped steps: emit `step_start` then `step_end` with outcome `skipped` and the `skip_if` condition. Currently skipped steps never enter an executor, so the runner handles this.

No changes to `executeByType()`, `handleOutcome()`, or `writeStepState()` — audit logging is orthogonal to state management.

## Changes to executors

### shell.ts — executeShellStep()

**Stderr capture:**

Change `stderr: 'inherit'` to `stderr: 'pipe'` in all cases. Add tee logic for stderr (same chunked reader pattern as existing stdout capture):

```
const stderrChunks: Uint8Array[] = [];
// Read stderr stream, tee to process.stderr, collect chunks
```

When `capture` is set: stdout is piped (existing behavior, unchanged). When `capture` is not set: stdout is inherited (existing behavior, unchanged). Stderr is always piped regardless.

**Audit events:**

- `step_start`: interpolated command, context snapshot
- `step_end`: exit code, stderr (always), captured stdout (only if `capture` set), outcome, duration

Duration: record `Date.now()` at entry, compute delta at exit.

### agent.ts — executeAgentStep()

No I/O changes — agent stdout/stderr remain inherited.

**Audit events:**

- `step_start`: interpolated prompt, mode, session strategy (`new`/`resume`/`inherit`), resolved session ID, model, engine enrichment, context snapshot
- `step_end`: exit code, discovered session ID, outcome, duration

All this data is already computed inside the executor — prompt via `buildPrompt()`, session via `resolveSessionId()`, enrichment via `engine.enrichPrompt()`. Just need to capture the values before they're passed to `Bun.spawn()`.

### loop.ts — executeLoopStep()

**Audit events:**

- `step_start`: loop type (`counted`/`for-each`), max or glob pattern + resolved matches, context snapshot
- `iteration_start` / `iteration_end`: iteration index, loop variable (for-each), outcome, duration
- `step_end`: iterations completed, break triggered, outcome, duration

The iteration events are emitted inside `executeCountedLoop()` and `executeForEachLoop()`, wrapping each call to `executeIterationBody()`.

### sub-workflow.ts — executeSubWorkflowStep()

**Audit events:**

- `step_start`: resolved workflow path, interpolated params, context snapshot
- `sub_workflow_start`: emitted after child context is created, before executing child steps
- `sub_workflow_end`: emitted after all child steps complete
- `step_end`: outcome, duration

Update `createSubWorkflowContext()` call to pass `workflow.name` as `subWorkflowName`.

## Crash Handling

In `runWorkflow()`, after creating the logger:

```typescript
const crashHandler = (err: Error) => {
  auditLogger.emit({ event: 'error', data: { message: err.message, stack: err.stack } });
  auditLogger.emit({ event: 'run_end', data: { outcome: 'failed' } });
  auditLogger.close();
};
process.on('uncaughtException', crashHandler);
process.on('unhandledRejection', crashHandler);
```

Remove handlers in the finally block of `runWorkflow()` to avoid leaking between test runs.

## Testing

- **AuditLogger unit tests**: write to temp file, verify line format (timestamp, prefix, event type, JSON payload), verify flush-on-write (file readable after each emit without close)
- **buildPrefix() unit tests**: cover all nesting combinations — top-level, loop, sub-workflow, nested loop+sub-workflow
- **Executor audit tests**: inject a spy logger via context, verify each executor emits correct events with correct type-specific data
- **Integration test**: run a small workflow with loops and sub-workflows, read the log file, verify event sequence and nesting prefixes
- **Crash test**: trigger an error mid-workflow, verify log contains error event + run_end and file is not corrupted
- **Stderr capture test**: shell step that writes to stderr, verify it appears in both terminal output and audit log
