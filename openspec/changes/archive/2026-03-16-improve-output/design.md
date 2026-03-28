## Context

Baton's console output during workflow execution is hard to follow. Step boundaries use a flat `--- step N/M: stepId [type] ---` format with no visual separation. Headless agent steps print `mode: headless` and then go silent until completion. Nesting context (loops, sub-workflows) is tracked internally via `NestingSegment[]` in `ExecutionContext` and formatted for audit logs by `buildPrefix()` in `audit.ts`, but none of this context surfaces in console output.

The project currently has zero terminal UI dependencies — no spinner libraries, no color libraries. All output is raw `console.log()`.

## Goals / Non-Goals

**Goals:**
- Print visual separator lines between workflow steps
- Show breadcrumb-style step headings with full nesting path
- Display the resolved prompt when a headless agent step starts
- Show a spinner animation during headless agent execution

**Non-Goals:**
- Changing the audit log format (stays machine-oriented)
- Adding color or rich terminal formatting beyond the spinner
- Detecting terminal width for separator sizing
- Truncating long breadcrumbs in deeply nested workflows

## Decisions

### 1. Spinner implementation: `ora` library

Add `ora` as a dependency for the headless spinner animation. It provides proven terminal animation, automatic terminal detection, and graceful cleanup — avoids hand-rolling cursor management and ANSI escape sequences.

**Rationale:** The project has no existing terminal UI dependencies, so this is a net-new dependency either way. `ora` is lightweight, well-maintained, and handles edge cases (non-TTY, CI environments) that a hand-rolled solution would need to replicate.

### 2. New `buildBreadcrumb()` function separate from `buildPrefix()`

Create a new `buildBreadcrumb()` function in a new `src/format.ts` module rather than modifying `buildPrefix()` in `audit.ts`. Both functions operate on `NestingSegment[]` but produce different formats for different audiences.

**Rationale:** Clean separation of concerns. `buildPrefix()` produces machine-oriented audit output (`[loop:0, step1]`). `buildBreadcrumb()` produces human-readable display output (`task-loop > iteration 1 > step1`). Coupling them via a format option would tie audit logging to display formatting changes.

### 3. Spinner lifecycle inline in `agent.ts`

Start/stop the `ora` spinner directly in `runHeadlessWithSigint()` rather than wrapping it in a separate utility module. The spinner is only used in one place.

**Rationale:** YAGNI. If spinners are needed elsewhere later, extraction is trivial. For now, co-locating with the headless execution logic keeps things simple.

### 4. Display formatting in new `src/format.ts` module

Create a new `src/format.ts` module for all display-formatting functions rather than adding them to `audit.ts` or `context.ts`.

**Rationale:** `audit.ts` is for structured audit logging. `context.ts` is for execution state. Display formatting is a distinct concern that deserves its own module.

## Design

### New module: `src/format.ts`

Pure display-formatting functions with no side effects beyond writing to stdout:

- **`buildBreadcrumb(nestingPath: NestingSegment[], stepId: string): string`** — Iterates `nestingPath`, emitting `stepId` for plain segments, `stepId > iteration N` for loop iterations (converting 0-indexed to 1-indexed), and appending `subWorkflowName` for sub-workflow segments. Joins with ` > `, appends the current `stepId`.
  - `[] + 'validate'` → `validate`
  - `[{stepId: "task-loop", iteration: 0}] + 'implement'` → `task-loop > iteration 1 > implement`
  - `[{stepId: "task-loop", iteration: 0}, {stepId: "verify", subWorkflowName: "verify-task"}] + 'check'` → `task-loop > iteration 1 > verify > verify-task > check`

- **`printSeparator(): void`** — Writes a fixed-width line of `━` characters to stdout.

- **`printStepHeading(index: number, total: number, breadcrumb: string, stepType: string, skipped: boolean): void`** — Prints the formatted heading. When `skipped` is false: `━━ step N/M: breadcrumb [type] ━━`. When `skipped` is true: `━━ step N/M: breadcrumb [skipped] ━━`.

### Modified: `src/runner.ts`

In the step dispatch loop, replace the current header output:
```
console.log(`--- step ${index + 1}/${workflow.steps.length}: ${step.id} [${stepType}] ---`)
```

With:
```
printSeparator()
printStepHeading(index, workflow.steps.length, buildBreadcrumb(context.nestingPath, step.id), stepType, skipped)
```

Skipped steps still print both the separator and heading (with `[skipped]` label), per spec.

### Modified: `src/executors/agent.ts`

Two changes in the headless code path:

1. **Prompt display** — After printing `mode: headless`, print the resolved prompt indented 2 spaces per line:
   ```
   console.log(prompt.split('\n').map(l => '  ' + l).join('\n'))
   ```

2. **Spinner** — In `runHeadlessWithSigint()`:
   - Create an `ora` spinner with text `agent running...`
   - Call `spinner.start()` before `Bun.spawn()`
   - Call `spinner.stop()` after process exit
   - In the SIGINT handler, call `spinner.stop()` before killing the subprocess

No changes for interactive steps — they already have a separate code path and the spec explicitly excludes them from both prompt display and spinner.

### Unchanged: `src/audit.ts`, `src/context.ts`

Audit log format and nesting data structures are untouched. `buildPrefix()` continues to produce the machine-oriented format for audit events.

## Risks / Trade-offs

- **`ora` compatibility with Bun** — The project uses Bun as its runtime. `ora` should work under Bun but this should be verified during implementation. If incompatible, `nanospinner` or a hand-rolled `setInterval` + `process.stdout.write` fallback is trivial (~20 lines).
- **Fixed separator width** — The spec says "fixed width." If the terminal is narrower, the separator wraps. Acceptable for a CLI tool — detecting terminal width is out of scope.
- **Breadcrumb length** — Deeply nested workflows (4+ levels) produce long breadcrumb strings. No truncation per spec. Acceptable trade-off for clarity over brevity.

## Migration Plan

No migration needed. Changes are purely additive console output formatting. Existing audit logging is untouched. Tests that assert on the old `--- step N/M: ... ---` heading format will need updating to match the new format.

## Open Questions

None — all architectural decisions resolved during design conversation.
