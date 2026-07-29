## ADDED Requirements

### Requirement: Route submission message type

The control channel SHALL accept a route submission message type alongside completion, committed-turn, and agent-call messages. It SHALL apply the same attempt-scoped authentication as completion: only the active run, step, attempt, and credential are accepted, and malformed, stale, unknown, or ineligible messages SHALL be rejected and audited without advancing the workflow. Route submission SHALL maintain its own acceptance state, distinct from completion acceptance.

#### Scenario: Active credential is accepted
- **WHEN** a well-formed route submission carries the active attempt's credential and the step is route-eligible
- **THEN** the server admits it for validation

#### Scenario: Stale credential is rejected
- **WHEN** a route submission carries an earlier attempt's credential
- **THEN** the server rejects and audits it without changing workflow state

#### Scenario: Route acceptance is independent of completion acceptance
- **WHEN** a route submission is accepted for an attempt whose completion has not been requested
- **THEN** the step does not advance, and the attempt remains active

## MODIFIED Requirements

### Requirement: Acknowledgement precedes termination

Completion acceptance SHALL be an intermediate state, not success. The server SHALL capture the adapter's accept-time durability checkpoint, freeze any staged route so no later route submission can change what will launch, record `completion_requested` and `completion_acknowledged`, and return the acknowledgement before sending any termination signal. Early committed-turn hook events that arrive before acceptance SHALL be acknowledged and ignored rather than failing the agent turn.

#### Scenario: Tool call returns before shutdown
- **WHEN** a valid completion request is accepted
- **THEN** its client receives a success acknowledgement before CLI termination begins

#### Scenario: Hook fires before completion acceptance
- **WHEN** a native turn hook sends `turn_committed` with no accepted completion
- **THEN** the server acknowledges and ignores it without failing or advancing the step

#### Scenario: Route freezes before the completion is acknowledged
- **WHEN** a completion request is accepted for an attempt with a staged route
- **THEN** the route is frozen before the completion acknowledgement is returned
- **AND** a route submission arriving afterwards is rejected

### Requirement: Completion integration preserves supervision

Adapters MAY pre-approve only the exact absolute-path runner commands with fixed arguments: the completion command with fixed `step complete` arguments, and the route submission command with fixed `step submit-route` arguments. No runner command carrying caller-supplied or model-authored argument values SHALL be pre-approved. A CLI that cannot express this safely SHALL keep its normal interactive approval prompt rather than broaden permissions. Process-local native commands and hooks SHALL be injected for the spawned process without requiring global user installation or project-file changes. Failure to prepare a required integration SHALL fail before spawn.

#### Scenario: Unrelated commands remain supervised
- **WHEN** an interactive agent runs a command other than the exact pre-approved runner commands
- **THEN** the CLI's normal approval behavior is unchanged

#### Scenario: Native command is process-local
- **WHEN** Agent Runner adds an adapter-native completion command or hook
- **THEN** it is available to the spawned CLI without global installation or project mutation

#### Scenario: Route submission command is pre-approved only in its exact form
- **WHEN** an adapter pre-approves the route submission command for a route-eligible step
- **THEN** only the exact absolute-path command with fixed `step submit-route` arguments is granted
- **AND** a variant carrying additional arguments is not covered by the grant
