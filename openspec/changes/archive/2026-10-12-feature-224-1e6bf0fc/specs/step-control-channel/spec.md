## ADDED Requirements

### Requirement: Agent-call activity snapshot

The control channel SHALL let an authenticated agent-call bridge request the current activity summary of a call. The request is scoped to the bridge's fresh, authenticated parent attempt and SHALL identify the call by exactly one of the request ID of its `call_agent` start or its `call_id`, so a bridge still blocked in `call_agent` can obtain activity before it knows the `call_id`. The response SHALL contain the call's status, elapsed time, and current activity summary, and SHALL be answered without waiting for the child. A snapshot request MUST NOT count as an agent call, accept or start a child, change the call's lifecycle, or extend any deadline. A request for a call that is unknown to the attempt, or a request that is stale or unauthenticated, SHALL be rejected with a structured error. That rejection MUST NOT affect any running child.

#### Scenario: Bridge obtains activity for a running call
- **WHEN** an authenticated bridge requests a snapshot for its attempt's running call
- **THEN** the control channel returns status `running`, elapsed time, and the current activity summary immediately

#### Scenario: Bridge obtains activity by start request ID
- **WHEN** a bridge blocked in `call_agent` requests a snapshot using that start's request ID after the call was accepted
- **THEN** the control channel returns the accepted call's status, elapsed time, and activity summary

#### Scenario: Snapshot does not count as a call
- **WHEN** the bridge requests snapshots while a call is in flight
- **THEN** no `call_in_progress` rejection, child launch, or lifecycle change results

#### Scenario: Stale attempt snapshot is rejected
- **WHEN** a bridge authenticated for a previous attempt requests a snapshot
- **THEN** the control channel rejects it with a structured error and no running child is affected

#### Scenario: Unknown call snapshot is rejected
- **WHEN** a bridge requests a snapshot for a call the active attempt never accepted
- **THEN** the control channel returns a structured error
