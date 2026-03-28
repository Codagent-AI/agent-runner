## ADDED Requirements

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

### Requirement: Sub-workflow step-specific data

Sub-workflow step `step_start` entries SHALL include the resolved workflow path and interpolated params passed.

#### Scenario: Sub-workflow start
- **WHEN** a sub-workflow step starts with resolved path `workflows/verify.yaml` and params `{task: "tasks/1.md"}`
- **THEN** the `step_start` entry includes the path and params

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
