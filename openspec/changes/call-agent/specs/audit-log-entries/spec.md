## ADDED Requirements

### Requirement: Agent-call event data

Every accepted agent call SHALL emit exactly one `agent_call_start` and one `agent_call_end` beneath the parent step's existing nesting prefix. Both entries SHALL include a unique `call_id` and the parent attempt identity. Repeated calls from one parent SHALL have distinct call IDs, while an idempotent retry of one request MUST NOT duplicate its event pair.

The `agent_call_start` entry SHALL include the requested prompt; target kind and name; effective working directory; and resolved profile, CLI, model, and session metadata available at start. The `agent_call_end` entry SHALL include the outcome, duration, exit code, discovered session ID, usage, cost, and error information following ordinary agent-step rules.

The full child response MUST NOT be duplicated in `audit.log`. Called-child output SHALL follow ordinary headless agent-step output persistence and privacy behavior. When that execution path persists output files, the call identity SHALL distinguish them from the parent and from other calls.

#### Scenario: Successful call emits attributable pair
- **WHEN** an accepted agent call succeeds
- **THEN** the audit log contains one start/end pair with the same call ID and parent attempt identity

#### Scenario: Failed child emits end event
- **WHEN** an accepted agent call starts a child that fails
- **THEN** the audit log retains its start event and writes an end event containing the failed outcome and error metadata

#### Scenario: Repeated calls have distinct identities
- **WHEN** one parent completes multiple separate agent calls
- **THEN** each call's event pair has a distinct call ID

#### Scenario: Idempotent retry does not duplicate events
- **WHEN** an accepted agent-call request is retried with the same request ID
- **THEN** the audit log contains only the original call's start/end pair

#### Scenario: Rejected request emits no call pair
- **WHEN** an agent-call request is rejected before a child starts
- **THEN** Agent Runner records the rejection through existing control-rejection auditing and emits no agent-call start/end pair

#### Scenario: Full response omitted from audit entries
- **WHEN** a called child produces a final response and process output
- **THEN** the agent-call audit entries contain execution metadata but not the full response text

#### Scenario: Persisted output remains distinguishable
- **WHEN** the ordinary headless execution path persists output for multiple calls beneath one parent
- **THEN** each call's output is distinguishable by its call identity

## MODIFIED Requirements

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
