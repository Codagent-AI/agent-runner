# Task: Persist Call Evidence

## Goal

Persist every accepted called-agent execution as distinct, attributable audit, output, session, usage, cost, outcome, and duration evidence. Extend `run-metrics.json` additively without double-counting the parent or overlapping wall-clock time, and preserve records across interruption and resume.

## Background

You MUST read these approved artifacts before starting:

- `proposal.md`, especially **Audit and metrics** and **Impact**.
- `design.md`, especially **Decision 7: Represent calls as nested execution evidence**, the output/metrics risks, and migration step 5.
- `specs/agent-calls/spec.md`, `specs/audit-log-entries/spec.md`, and `specs/run-metrics-artifact/spec.md` for the acceptance criteria copied below.

Relevant implementation seams:

- `internal/audit/types.go` defines event types; `internal/audit/logger.go` writes JSONL and builds structural prefixes.
- `internal/exec/step_audit.go` and the Runner-owned call wrapper emit start/end metadata. Use exactly one call event pair per accepted execution and no pair for pre-acceptance rejection.
- `internal/liverun/process_runner.go` persists raw stdout/stderr beneath `<sessionDir>/output/` using the invocation's structural prefix. The call ID must prevent collisions with the parent and repeated calls.
- `internal/model/usage.go` carries execution identity and usage provenance.
- `internal/metrics/collector.go` projects terminal audit events into schema-v1 `run-metrics.json`, attributes cumulative-session usage, calculates coverage/totals, rehydrates prior records, and performs atomic writes through existing state I/O.
- `internal/metrics/pipeline.go` normalizes events before forwarding them to the audit sink.
- Existing tests in `internal/audit/`, `internal/metrics/`, `internal/exec/agent_test.go`, and `internal/liverun/liverun_test.go` establish compatibility expectations.

Each accepted call gets a unique `call_id` and a structural child prefix beneath its active parent attempt. Acceptance occurs after Runner validation and request-ID reservation but before CLI launch, so emit start evidence before attempting launch. A CLI launch failure is a failed accepted call with a paired end event. Start metadata includes the requested prompt and resolved execution context; end metadata follows ordinary agent-step rules for outcome, exit/session information, usage, cost, and errors. Do not copy the full child response into `audit.log`; the parent receives it through the tool result, while ordinary headless output persistence retains inspectable stdout/stderr with existing privacy and size behavior.

Keep `run-metrics.json` at schema version 1. Extend the existing `steps[]` execution-record union with `kind: "agent-call"` plus additive call and parent/target fields. Do not add a parallel top-level `agent_calls[]` collection. Parent workflow-step records retain only parent metrics. Every accepted call that reaches a terminal outcome appends exactly one record, including CLI launch failure. Launch failures have failed outcome but do not participate in CLI usage-coverage denominators. Requests rejected before acceptance create no metric record or coverage obligation.

A call's duration is useful on its record but overlaps the synchronously waiting parent. Keep active execution-session wall time authoritative and never add child duration again. Named-session state continues through `state.json`; do not create a second persisted workflow-state tree for calls. Rehydrate older schema-v1 artifacts that have no agent-call fields and preserve completed call records across resumed Runner sessions.

## Spec

### From `specs/agent-calls/spec.md`

### Requirement: Nested execution evidence

Agent Runner SHALL retain each called child's output, session bookkeeping, usage, cost, outcome, and duration as nested execution evidence attributable to the parent attempt. Called-agent usage and cost SHALL contribute to run totals without being exposed in the tool result.

#### Scenario: Successful child contributes evidence
- **WHEN** a called child succeeds with reported usage or cost
- **THEN** Agent Runner records its output, outcome, duration, usage, and cost beneath the parent and includes the reported metrics in run totals

#### Scenario: Failed child retains evidence
- **WHEN** a called child fails
- **THEN** Agent Runner retains its output, failure, duration, and any available usage or cost beneath the parent attempt

### From `specs/audit-log-entries/spec.md`

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

### From `specs/run-metrics-artifact/spec.md`

### Requirement: Agent-call metric records and aggregation

Each accepted agent call that reaches a terminal outcome SHALL append a distinct `agent-call` record to `run-metrics.json`, including an accepted call whose child CLI fails to launch. The record SHALL include its call ID, parent attempt identity, target kind and name, outcome, duration in milliseconds, usage record, `estimated_api_cost_usd`, provenance, and completeness using ordinary agent-step metric semantics. Parent workflow-step records SHALL contain only the parent's own usage and cost; called-agent records SHALL remain separate so consumers can roll up the parent and its calls without counting any execution more than once.

Called-agent usage and cost SHALL contribute to run totals regardless of call outcome when the child reports them. Every called child that invokes its CLI SHALL participate in usage, canonical-total, and cost coverage calculations; a call rejected or failed before CLI launch MUST NOT participate in those coverage denominators. A called child's duration SHALL be retained on its record but MUST NOT be added to run elapsed time because that interval overlaps the waiting parent; the existing active execution-session duration remains authoritative.

Agent Runner SHALL update the artifact through its existing atomic-write path after each agent call completes. Separate calls SHALL append separate records, an idempotent retry MUST NOT append a duplicate record, and completed call records SHALL accumulate across workflow resume with existing step records.

#### Scenario: Successful call appends nested record
- **WHEN** a called agent succeeds
- **THEN** `run-metrics.json` contains one `agent-call` record with its call ID, parent attempt identity, target, outcome, duration, usage, and cost data

#### Scenario: Failed call retains reported metrics
- **WHEN** a called agent fails after its CLI reports usage or cost
- **THEN** its failed call record retains those metrics and they contribute to run totals

#### Scenario: CLI launch failure appends failed record
- **WHEN** an accepted call fails before its child CLI launches
- **THEN** `run-metrics.json` contains a failed `agent-call` record and excludes that call from CLI usage-coverage denominators

#### Scenario: Separate calls append separate records
- **WHEN** one parent completes multiple separate agent calls
- **THEN** each call appends a distinct metric record

#### Scenario: Idempotent retry does not duplicate record
- **WHEN** an accepted agent-call request is retried with the same request ID
- **THEN** only the original called-agent execution appears in the metrics artifact

#### Scenario: Parent and child metrics counted once
- **WHEN** both a parent agent step and its called child report usage or cost
- **THEN** run totals include each execution's reported metrics exactly once

#### Scenario: Child duration does not inflate run time
- **WHEN** a parent waits synchronously for a child call lasting 30 seconds
- **THEN** the child record reports 30 seconds while run elapsed time continues to use active execution-session wall time without adding another 30 seconds

#### Scenario: Invoked child participates in coverage
- **WHEN** a called child invokes its CLI and then succeeds or fails
- **THEN** that execution participates in usage, canonical-total, and cost coverage calculations according to the metrics it reported

#### Scenario: Canceled invoked child participates in coverage
- **WHEN** a called child invokes its CLI and is then canceled
- **THEN** that execution participates in usage, canonical-total, and cost coverage calculations according to the metrics it reported

#### Scenario: Pre-acceptance rejection creates no record
- **WHEN** an agent-call request is rejected before reaching the acceptance boundary
- **THEN** it contributes no metric record and is excluded from coverage denominators

#### Scenario: Call completion updates artifact atomically
- **WHEN** a called child completes
- **THEN** Agent Runner updates `run-metrics.json` through the existing atomic-write behavior

#### Scenario: Call records survive workflow resume
- **WHEN** completed calls exist before a workflow interruption and the run is later resumed
- **THEN** their records remain in `run-metrics.json` alongside records appended after resume

## Done When

- Audit constants, parsing, and summary tests recognize `agent_call_start` and `agent_call_end` while preserving all existing event types and formats.
- Accepted success, post-launch failure, launch failure, and cancellation paths emit one attributable start/end pair with a stable call ID and parent attempt identity; idempotent retries do not duplicate it, and pre-acceptance rejections emit only existing control-rejection evidence.
- Start events contain the parent context snapshot and approved resolved/request metadata. End events contain outcome, duration, exit/session, usage/cost, and error metadata without duplicating the full response.
- Raw output files use the call's structural identity, stay separate from parent/repeated-call output, and retain ordinary headless privacy, truncation, failure, and best-effort persistence behavior.
- `run-metrics.json` remains schema v1 and appends additive `agent-call` records with call ID, parent attempt, target, outcome, duration, session, usage, cost, provenance, and completeness.
- Collector tests prove successful, failed, and canceled invoked children affect totals and coverage once; accepted CLI launch failures append failed records without entering CLI usage-coverage denominators; pre-acceptance rejections append no record; parent and child usage/cost are not folded together; and child duration does not inflate active run duration.
- Atomic-write and rehydration tests prove same-request retries do not append duplicates, separate calls do append separate records, older schema-v1 artifacts remain readable, corrupt-artifact recovery is unchanged, and records survive workflow resume.
- Tests for every scenario copied into this task pass. Run `make fmt`, targeted `internal/audit`, `internal/metrics`, `internal/exec`, and `internal/liverun` tests, then `go test ./...`.
