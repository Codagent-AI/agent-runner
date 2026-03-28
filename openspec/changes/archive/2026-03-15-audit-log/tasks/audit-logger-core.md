# Task: Audit logger core + runner + shell executor

## Goal

Create the AuditLogger system, wire it through ExecutionContext, integrate it into the runner (run-level events, crash handling, skipped steps), and update the shell executor to pipe stderr and emit audit events. This delivers a working audit log for shell-only workflows.

## Background

### Why this exists

When a workflow fails, there's no record of what happened. The state file only tracks where execution stopped (for resume), not what each step did, what it received, or why it failed. The audit log captures the full execution history for post-failure review.

### Architecture

AuditLogger is a class carried on `ExecutionContext` (same pattern as `engine`). Each executor emits its own events directly via `context.auditLogger?.emit()`. The runner handles run-level events and crash handling.

Null-safe: `auditLogger` is `AuditLogger | null` on context. All emit calls use optional chaining. This keeps tests simple — existing tests pass `null` and don't need to set up a logger.

### New file: src/audit.ts

**AuditLogger class:**
- Constructor takes a file path, creates directories with `mkdirSync`, opens file with `openSync` (append flags)
- `emit(event: AuditEvent)`: formats the line and writes with `writeSync` (guarantees flush-on-write)
- `close()`: `closeSync`
- Sync I/O is fine — steps take seconds to minutes

**AuditEvent fields:** Each event carries a timestamp (ISO-8601), a nesting prefix string (e.g. `[task-loop:0, implement]`), an event type, and a data payload (arbitrary key-value map for the JSON portion of the line).

**Event types:** `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `error`

**Line format:** Each line is a hybrid of human-scannable prefix and structured JSON — an ISO-8601 timestamp, the nesting prefix in brackets, the event type, then a JSON payload. Run-level events (`run_start`, `run_end`, `error`) have no prefix bracket. See the `audit-log-storage` spec for the authoritative format scenarios.

**buildPrefix() helper** — walks `context.nestingPath` and appends current step ID:

| Segment | Prefix token |
|---------|-------------|
| Loop iteration (`iteration !== undefined`) | `stepId:iteration` |
| Sub-workflow (`subWorkflowName` set) | `stepId, sub:subWorkflowName` |
| Other | `stepId` |
| Current step (appended last) | `stepId` |

Examples:
- `nestingPath: []`, step `validate` → `[validate]`
- `nestingPath: [{stepId: "task-loop", iteration: 0}]`, step `implement` → `[task-loop:0, implement]`
- `nestingPath: [{stepId: "task-loop", iteration: 0}, {stepId: "verify", subWorkflowName: "verify-task"}]`, step `check` → `[task-loop:0, verify, sub:verify-task, check]`

**createAuditLogger() factory** — computes log path from workflow name + cwd. The path is `~/.baton/projects/{encoded-cwd}/logs/{workflow-name}-{timestamp}.log`. Path encoding replaces `/`, `.`, `_` with `-`. Timestamp uses ISO-8601 with colons replaced by dashes for filesystem safety.

### Changes to src/context.ts

Add `auditLogger: AuditLogger | null` to `ExecutionContext`.

Add `subWorkflowName?: string` to `NestingSegment`.

Update context creation functions:
- `createRootContext()`: accepts optional `auditLogger`, stores it
- `createLoopIterationContext()`: inherits `auditLogger` from parent
- `createSubWorkflowContext()`: inherits `auditLogger` from parent, accepts `subWorkflowName` in options, stores it on the nesting segment

### Changes to src/runner.ts

In `runWorkflow()`, after validation:
1. Call `createAuditLogger(workflow.name)` to get logger instance
2. Pass logger to `createRootContext()`
3. Register crash handler via `process.on('uncaughtException')` and `process.on('unhandledRejection')`
4. Emit `run_start` with workflow file, name, hash, params, and resume info if applicable
5. Wrap the step loop in try/finally — emit `run_end` in finally, then `logger.close()`
6. Remove crash handlers in finally to avoid leaking between test runs

In `dispatchStep()`, for skipped steps: emit `step_start` then `step_end` with outcome `skipped` and the `skip_if` condition.

### Changes to src/executors/shell.ts

**Stderr capture:** Change `stderr: 'inherit'` to `stderr: 'pipe'` in all cases. Add tee logic for stderr using the same chunked reader pattern as existing stdout capture — collect chunks, write each to `process.stderr` in real-time. When `capture` is not set, stdout remains `'inherit'` (unchanged).

**Audit events:**
- `step_start`: interpolated command, context snapshot (params + capturedVariables)
- `step_end`: exit code, stderr (always), captured stdout (only if `capture` set), outcome, duration_ms

Duration: `Date.now()` at entry, compute delta at exit.

### Key files

- `src/audit.ts` — create new file (AuditLogger class, types, buildPrefix, createAuditLogger)
- `src/context.ts` — add auditLogger field, subWorkflowName on NestingSegment, update creation functions
- `src/runner.ts` — create logger, run_start/run_end, crash handler, skipped step events
- `src/executors/shell.ts` — stderr piping + audit events
- `test/shell-executor.test.ts` — existing tests; update mock proc to include stderr stream since stderr is now always piped
- `test/runner.test.ts` — existing tests; may need updates since runner now creates audit logger

### Testing approach

**Use the audit log as a verification tool in e2e tests.** Instead of only checking side effects (spawn call counts, state files), read the log file and verify the event sequence. This makes assertions more expressive and tests what actually matters.

**Unit tests for new code:**
- `buildPrefix()`: cover all nesting combinations (top-level, loop, sub-workflow, nested)
- `AuditLogger`: write to temp file, verify line format, verify flush-on-write
- `createAuditLogger()`: verify directory creation and path encoding

**Update existing e2e tests where the log makes verification cleaner.** For example, verify step execution order by reading log events instead of counting spawn calls.

**One focused audit log e2e test** for audit-specific concerns: log file creation, line format, stderr capture content in log, crash handling (error event + run_end before exit).

### Conventions

- Test framework: `bun:test` (see existing tests for patterns)
- Tests use `spyOn(Bun, 'spawn')` to mock subprocess execution
- Tests use temp directories (`tmpdir()`) for file artifacts, cleaned up in `afterEach`
- Helper functions `makeWorkflow()`, `makeStep()`, `makeMockProc()` are defined per test file

## Spec

### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, and `error`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

### Requirement: Nesting prefix

Every audit log entry SHALL include a nesting prefix that encodes the full path to the current execution point. Loop steps carry their iteration index as `step_name:N`. Sub-workflows are marked with `sub:workflow_name`. Top-level steps use `[step_name]`.

#### Scenario: Top-level step
- **WHEN** a step `validate` executes at the workflow root
- **THEN** entries have prefix `[validate]`

#### Scenario: Step inside a loop
- **WHEN** step `implement` executes inside loop `task-loop` at iteration 2
- **THEN** entries have prefix `[task-loop:2, implement]`

#### Scenario: Step inside a sub-workflow inside a loop
- **WHEN** step `check` executes inside sub-workflow `verify-task`, invoked from loop `task-loop` at iteration 0 via step `verify`
- **THEN** entries have prefix `[task-loop:0, verify, sub:verify-task, check]`

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

### Requirement: Shell step-specific data

Shell step entries SHALL include the interpolated command on `step_start`, and exit code, captured stdout (if capture set), and stderr on `step_end`.

#### Scenario: Shell step start
- **WHEN** a shell step starts with interpolated command `npm test`
- **THEN** the `step_start` entry includes `command: "npm test"`

#### Scenario: Shell step end with capture
- **WHEN** a shell step with `capture: test_output` completes with exit code 0
- **THEN** the `step_end` entry includes exit code, captured stdout, and stderr

#### Scenario: Shell step end without capture
- **WHEN** a shell step without `capture` completes with exit code 1
- **THEN** the `step_end` entry includes exit code and stderr, but no stdout

### Requirement: Skipped step entries

When a step is skipped due to `skip_if`, baton SHALL emit a `step_start` / `step_end` pair with outcome `skipped` and the skip_if condition that triggered it.

#### Scenario: Step skipped due to skip_if
- **WHEN** a step has `skip_if: previous_success` and the previous step succeeded
- **THEN** baton emits `step_start` and `step_end` with outcome `skipped` and condition `previous_success`

### Requirement: Error event for unexpected crashes

When an uncaught exception occurs during workflow execution, baton SHALL emit an `error` event with the exception message and stack trace, followed by a `run_end` event, before the process exits.

#### Scenario: Uncaught exception mid-run
- **WHEN** an unexpected error occurs during step execution
- **THEN** baton emits an `error` event with the exception message, then a `run_end` event with outcome `failed`

### Requirement: Runtime error details on step failure

When a step fails due to a caught runtime error (interpolation failure, missing file, missing params), the `step_end` entry SHALL include the error message in an error field.

#### Scenario: Interpolation failure
- **WHEN** a step fails because variable `{{foo}}` is undefined
- **THEN** the `step_end` entry has outcome `failed` and error `"Undefined variable: {{foo}}"`

### Requirement: Run-level events

Baton SHALL emit a `run_start` event after workflow and param validation succeeds, before the first step executes. Baton SHALL emit a `run_end` event after the last step completes or after an error event.

#### Scenario: Successful run
- **WHEN** a workflow passes validation and all steps complete
- **THEN** baton emits `run_start` before the first step and `run_end` after the last step

#### Scenario: Validation failure
- **WHEN** workflow validation fails (schema error, missing params, engine validation)
- **THEN** no audit log file is created

#### Scenario: Run fails mid-execution
- **WHEN** a step fails and halts the workflow
- **THEN** baton emits `run_end` with outcome `failed` after the failed step's `step_end`

### Requirement: Run start context

The `run_start` event SHALL include the workflow file path, workflow name, workflow hash, and all params.

#### Scenario: Run start captures workflow metadata
- **WHEN** a run begins for workflow `workflows/deploy.yaml` with params `{env: "staging"}`
- **THEN** the `run_start` entry includes the file path, name, hash, and params

### Requirement: Resumed run indicator

When a run is resumed via `baton resume`, the `run_start` event SHALL indicate it is a resume, include the step it is resuming from, and create a new log file.

#### Scenario: Resumed run
- **WHEN** a run is resumed from step `design`
- **THEN** a new audit log file is created and the `run_start` entry includes a resume indicator and the resuming step ID

### Requirement: Step-level events

Baton SHALL emit a `step_start` event before dispatching a step and a `step_end` event after the step completes, for every step type (shell, agent, loop, sub-workflow, group).

#### Scenario: Step executes normally
- **WHEN** a step `build` starts and completes with outcome `success`
- **THEN** baton emits `step_start` before execution and `step_end` after, with no events from other steps in between (except child events for loops/groups/sub-workflows)

#### Scenario: Step fails
- **WHEN** a step fails with a runtime error
- **THEN** baton emits `step_end` with outcome `failed` and the error details

### Requirement: Crash handling

When an uncaught exception occurs, baton SHALL emit an `error` event followed by `run_end` before the process exits. The audit log file SHALL be flushed to disk before exit.

#### Scenario: Crash mid-step
- **WHEN** an uncaught exception occurs during step execution
- **THEN** baton emits `error`, then `run_end` with outcome `failed`, and flushes the log file before exiting

### Requirement: Audit log persists regardless of outcome

The audit log file SHALL remain on disk after the run completes, regardless of whether the run succeeded or failed.

#### Scenario: Successful run preserves log
- **WHEN** a workflow completes successfully and baton deletes the state file
- **THEN** the audit log file is not deleted

### Requirement: Log directory location

Baton SHALL store audit logs in `~/.baton/projects/{encoded-path}/logs/` where `{encoded-path}` is the project directory path with `/`, `.`, and `_` replaced by `-`.

#### Scenario: Log directory created
- **WHEN** a workflow run begins and the log directory does not exist
- **THEN** baton creates `~/.baton/projects/{encoded-path}/logs/`

#### Scenario: Path encoding
- **WHEN** the project directory is `/Users/foo/my_project`
- **THEN** the log directory is `~/.baton/projects/-Users-foo-my-project/logs/`

### Requirement: Log file naming

Each workflow execution SHALL create a new log file named `{workflow-name}-{ISO-8601-timestamp}.log` in the log directory.

#### Scenario: New run creates log file
- **WHEN** workflow `deploy` starts at `2026-03-15T18:30:00Z`
- **THEN** baton creates `deploy-2026-03-15T18-30-00Z.log`

#### Scenario: Resumed run creates new log file
- **WHEN** workflow `deploy` is resumed at `2026-03-15T19:00:00Z`
- **THEN** baton creates a new file `deploy-2026-03-15T19-00-00Z.log`

### Requirement: Log file format

Each line in the log file SHALL be a hybrid format: ISO-8601 timestamp, nesting prefix, event type, followed by a JSON payload. The JSON payload contains all structured data for the event.

#### Scenario: Log line format
- **WHEN** a `step_start` event is emitted for step `validate` at `2026-03-15T18:30:00Z`
- **THEN** the log line is formatted as `2026-03-15T18:30:00Z [validate] step_start {...}`

### Requirement: Log persistence

Audit log files SHALL never be automatically deleted. No rotation or cleanup is performed by baton.

#### Scenario: Logs accumulate
- **WHEN** a workflow is run 100 times
- **THEN** 100 log files exist in the log directory

### Requirement: Flush on write

Each log entry SHALL be flushed to disk immediately after being written, to ensure the log is complete even if the process crashes.

#### Scenario: Crash preserves log
- **WHEN** baton crashes mid-execution
- **THEN** all entries written before the crash are present in the log file

### Requirement: Shell stderr piping

Baton SHALL pipe stderr from all shell steps instead of inheriting it. The stderr output SHALL be teed to the terminal in real-time and stored for inclusion in the audit log.

#### Scenario: Stderr displayed and captured
- **WHEN** a shell step produces stderr output
- **THEN** the stderr is displayed to the terminal in real-time and stored for the audit log `step_end` entry

#### Scenario: Stderr captured regardless of capture field
- **WHEN** a shell step has no `capture` field
- **THEN** stderr is still piped, teed, and stored for the audit log

#### Scenario: Stderr captured alongside stdout capture
- **WHEN** a shell step has `capture: output` and produces both stdout and stderr
- **THEN** stdout is captured into the variable and teed, stderr is separately captured and teed, both are included in the audit log

#### Scenario: No stderr output
- **WHEN** a shell step produces no stderr
- **THEN** the audit log `step_end` entry includes an empty stderr field

### Requirement: Shell stderr capture

Shell steps SHALL pipe stderr in addition to stdout. Stderr SHALL be teed to the terminal in real-time and stored for the audit log. See `stderr-capture` spec for full requirements.

#### Scenario: Stderr piped alongside stdout
- **WHEN** a shell step with `capture: output` executes and produces both stdout and stderr
- **THEN** stdout is captured into the variable and teed, stderr is separately teed and stored for the audit log

## Done When

- `src/audit.ts` exists with AuditLogger class, buildPrefix(), createAuditLogger()
- Running a shell-only workflow produces a correctly formatted log file at `~/.baton/projects/{encoded-cwd}/logs/`
- Log file contains run_start, step_start/step_end for each step, and run_end with correct prefixes, context snapshots, and timing
- Shell stderr is piped, teed to terminal, and captured in audit log
- Crash handler emits error + run_end before exit
- Skipped steps emit step_start/step_end with outcome "skipped"
- All existing tests pass (with necessary updates for stderr piping change)
- New unit tests cover buildPrefix(), AuditLogger, and path encoding
- E2e tests use audit log for verification where it makes assertions cleaner
