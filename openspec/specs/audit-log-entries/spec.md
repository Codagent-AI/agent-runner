# audit-log-entries Specification

## Purpose
Define the structure, types, and content requirements for individual audit log entries emitted during workflow execution.
## Requirements
### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `agent_call_start`, `agent_call_end`, `error`, `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, and `child_continued`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Completion events are intermediate
- **WHEN** the audit logger receives control or durability events during an interactive agent step
- **THEN** it writes them as intermediate events distinct from the step's final `step_end`

#### Scenario: Agent-call events are distinct from workflow steps
- **WHEN** the audit logger receives an `agent_call_start` or `agent_call_end` event
- **THEN** it records the event without representing the call as a workflow `step_start` or `step_end`

### Requirement: Nesting prefix

Every audit log entry SHALL include a nesting prefix that encodes the full path to the current execution point. Loop steps carry their iteration index as `step_name:N`. Sub-workflows are marked with `sub:workflow_name`. Top-level steps use `[step_name]`. Root-scoped events (`run_start`, `run_end`, `error`) use an empty prefix string.

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

Start events (`run_start`, `step_start`, `iteration_start`, `sub_workflow_start`, `agent_call_start`) SHALL include the full context snapshot: all params and all captured variables available at that point. An `agent_call_start` SHALL use the parent attempt's current context snapshot.

#### Scenario: Step start includes params and captured variables
- **WHEN** a `step_start` event is emitted and the context has params `{env: "staging"}` and captured variables `{build_output: "/tmp/build"}`
- **THEN** the entry includes both in the context snapshot

#### Scenario: Agent-call start includes parent context
- **WHEN** an `agent_call_start` event is emitted
- **THEN** the entry includes the params and captured variables available to its parent attempt

#### Scenario: End events omit context snapshot
- **WHEN** a `step_end` or `agent_call_end` event is emitted
- **THEN** the entry does not include a context snapshot

### Requirement: End event data

End events (`step_end`, `run_end`, `iteration_end`, `sub_workflow_end`, `agent_call_end`) SHALL include the outcome (`success`, `failed`, `aborted`, `exhausted`, `skipped`) and duration in milliseconds.

#### Scenario: Step end includes outcome and duration
- **WHEN** a step completes after 1500ms with outcome `success`
- **THEN** the `step_end` entry includes `outcome: "success"` and `duration_ms: 1500`

#### Scenario: Agent-call end includes outcome and duration
- **WHEN** an agent call reaches a terminal outcome
- **THEN** its `agent_call_end` entry includes that outcome and the call's duration in milliseconds

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

Agent step entries SHALL include the interpolated prompt, mode, session strategy, resolved session ID, model, and engine enrichment on `step_start`. The `step_end` SHALL include exit code, discovered session ID, the step's token-usage record, and `estimated_api_cost_usd`.

The `model` field on `step_start` SHALL be the **resolved** model — that is, the model that agent-runner launched the CLI with, computed by composing the step-level `model:` override (if any) over the resolved agent profile's default model. For `session: resume` and `session: inherit` steps, the profile used SHALL be the profile of the session-originating step (i.e. the profile already recorded in the execution context under that step's ID), so the `model` value matches the model the CLI was originally invoked with when the session was created.

If no model can be resolved (the step has no `model:` override and no profile store / profile default is available to fall back on), the `model` field SHALL be emitted as an empty string.

The usage record on `step_end` SHALL follow the `agent-usage-collection` capability: distinct token categories with provenance and completeness, or an explicit unavailable state. `estimated_api_cost_usd` SHALL follow the `cost-capture` capability: the CLI-reported USD value, or null. Unavailable usage and absent cost SHALL be emitted as explicit null/unavailable values, never as zeros.

#### Scenario: Agent step start
- **WHEN** a headless agent step starts with session strategy `resume` and resolved session ID `abc-123`
- **THEN** the `step_start` entry includes prompt, mode, session strategy, resolved session ID, model, and enrichment

#### Scenario: Agent step end
- **WHEN** an agent step completes and Agent Runner discovers session ID `def-456`
- **THEN** the `step_end` entry includes exit code and discovered session ID `def-456`

#### Scenario: Agent step end includes usage and cost
- **WHEN** an autonomous-headless agent step completes with collected usage and a CLI-reported cost
- **THEN** the `step_end` entry includes the token-usage record (categories, provenance, completeness) and `estimated_api_cost_usd`

#### Scenario: Agent step end with unavailable usage
- **WHEN** an agent step completes but usage could not be collected (PTY-backed context or parse failure)
- **THEN** the `step_end` entry carries an explicit unavailable usage state and a null `estimated_api_cost_usd`; no zero counts are emitted

#### Scenario: Resolved model populated from profile default
- **WHEN** an agent step has no step-level `model:` override and its resolved profile specifies model `sonnet`
- **THEN** the `step_start` entry's `model` field is `sonnet`

#### Scenario: Resolved model populated from step-level override
- **WHEN** an agent step sets `model: opus` inline and its resolved profile's default model is `sonnet`
- **THEN** the `step_start` entry's `model` field is `opus`

#### Scenario: Resolved model for resumed session uses originating profile
- **WHEN** an agent step uses `session: resume` or `session: inherit` to reuse the CLI session of an earlier step whose profile had default model `opus`, and this step has no step-level override
- **THEN** the `step_start` entry's `model` field is `opus`

#### Scenario: Resolved model empty when nothing can be resolved
- **WHEN** an agent step has no step-level override and no profile store is available to supply a default
- **THEN** the `step_start` entry's `model` field is an empty string

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

When a step is skipped due to `skip_if`, Agent Runner SHALL emit a `step_start` / `step_end` pair with outcome `skipped` and the skip_if condition that triggered it.

#### Scenario: Step skipped due to skip_if
- **WHEN** a step has `skip_if: previous_success` and the previous step succeeded
- **THEN** Agent Runner emits `step_start` and `step_end` with outcome `skipped` and condition `previous_success`

### Requirement: Error event for unexpected crashes

When an uncaught exception occurs during workflow execution, Agent Runner SHALL emit an `error` event with the exception message and stack trace, followed by a `run_end` event, before the process exits.

#### Scenario: Uncaught exception mid-run
- **WHEN** an unexpected error occurs during step execution
- **THEN** Agent Runner emits an `error` event with the exception message, then a `run_end` event with outcome `failed`

### Requirement: Runtime error details on step failure

When a step fails due to a caught runtime error (interpolation failure, missing file, missing params), the `step_end` entry SHALL include the error message in an error field.

#### Scenario: Interpolation failure
- **WHEN** a step fails because variable `{{foo}}` is undefined
- **THEN** the `step_end` entry has outcome `failed` and error `"Undefined variable: {{foo}}"`

### Requirement: Run end usage and cost totals

The `run_end` entry SHALL include the run's aggregated metrics: per-category token totals across all steps of the run (cumulative across resume sessions) with the usage-coverage indicator; canonical processed input, output, and overall token totals with their coverage indicator; and the run-level cost total with its coverage indicator, per the `cost-capture` capability. Token totals SHALL follow the aggregation semantics of the `run-metrics-artifact` capability: only reported values are summed, canonical totals are included only when an adapter can obtain them without double-counting, steps with unavailable usage contribute nothing, and unreported categories are absent rather than zero. When no step reported canonical totals or cost, the corresponding total SHALL be null with coverage `none`.

#### Scenario: Run end carries aggregated totals
- **WHEN** a run ends after agent steps consumed tokens and some reported cost
- **THEN** the `run_end` entry includes per-category token totals, canonical processed-token totals, and the cost total with their coverage indicators

#### Scenario: Run end with partial canonical-total coverage
- **WHEN** a run ends after one agent step reports reliable canonical processed-token totals and another reports only raw categories
- **THEN** the `run_end` entry sums the known canonical totals and marks canonical-total coverage `partial`

#### Scenario: Run end with no cost data
- **WHEN** a run ends and no step reported a USD cost
- **THEN** the `run_end` entry's cost total is null and its coverage is `none`

#### Scenario: Run end with mixed usage availability
- **WHEN** a run ends containing one agent step with a full usage record and one whose usage is unavailable
- **THEN** the `run_end` entry's token totals equal the reporting step's values with usage coverage `partial`; no zeros are substituted

### Requirement: Agent-call event data

Every accepted agent call SHALL emit exactly one `agent_call_start` and one `agent_call_end` beneath the parent step's existing nesting prefix. Agent Runner SHALL emit `agent_call_start` after the call passes the acceptance boundary and before attempting to launch its child CLI. Both entries SHALL include a unique `call_id` and the parent attempt identity. Repeated calls from one parent SHALL have distinct call IDs, while an idempotent retry of one request MUST NOT duplicate its event pair.

The `agent_call_start` entry SHALL include the requested prompt; target kind and name; effective working directory; and resolved profile, CLI, model, and session metadata available at start. The `agent_call_end` entry SHALL include the outcome, duration, exit code, discovered session ID, usage, cost, and error information following ordinary agent-step rules.

The full child response MUST NOT be duplicated in `audit.log`. Called-child output SHALL follow ordinary headless agent-step output persistence and privacy behavior. When that execution path persists output files, the call identity SHALL distinguish them from the parent and from other calls.

#### Scenario: Successful call emits attributable pair
- **WHEN** an accepted agent call succeeds
- **THEN** the audit log contains one start/end pair with the same call ID and parent attempt identity

#### Scenario: Failed child emits end event
- **WHEN** an accepted agent call starts a child that fails
- **THEN** the audit log retains its start event and writes an end event containing the failed outcome and error metadata

#### Scenario: CLI launch failure emits failed pair
- **WHEN** an accepted agent call fails while launching its child CLI
- **THEN** the audit log contains its start event and a failed end event with the launch error

#### Scenario: Repeated calls have distinct identities
- **WHEN** one parent completes multiple separate agent calls
- **THEN** each call's event pair has a distinct call ID

#### Scenario: Idempotent retry does not duplicate events
- **WHEN** an accepted agent-call request is retried with the same request ID
- **THEN** the audit log contains only the original call's start/end pair

#### Scenario: Pre-acceptance rejection emits no call pair
- **WHEN** an agent-call request fails before reaching the acceptance boundary
- **THEN** Agent Runner records the rejection through existing control-rejection auditing and emits no agent-call start/end pair

#### Scenario: Full response omitted from audit entries
- **WHEN** a called child produces a final response and process output
- **THEN** the agent-call audit entries contain execution metadata but not the full response text

#### Scenario: Persisted output remains distinguishable
- **WHEN** the ordinary headless execution path persists output for multiple calls beneath one parent
- **THEN** each call's output is distinguishable by its call identity

