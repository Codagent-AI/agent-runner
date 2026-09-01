## ADDED Requirements

### Requirement: Recorded executions can be audited explicitly

The development-audit build SHALL provide an explicit audit invocation that accepts a recorded source run and audits its durable evidence without rerunning or resuming the source workflow. Untagged and release builds SHALL NOT provide this invocation.

When the source run contains more than one execution session, the invocation SHALL require or resolve an unambiguous execution session. It MUST NOT silently combine sessions into one observation.

#### Scenario: Historical execution is audited without rerun
- **WHEN** a user explicitly requests an audit of a recorded source run and execution session
- **THEN** Agent Runner launches the same hidden audit workflow used for automatic auditing without executing the source workflow again

#### Scenario: Replay is absent from production
- **WHEN** an untagged or release binary is invoked
- **THEN** no audit replay command is registered

#### Scenario: Session selection is ambiguous
- **WHEN** a recorded run contains multiple execution sessions and the replay request does not identify one unambiguously
- **THEN** Agent Runner declines to start the audit and identifies the available execution sessions

#### Scenario: Source evidence is unavailable
- **WHEN** the requested run or execution session cannot be resolved to durable evidence
- **THEN** the explicit audit exits with a diagnostic and does not mutate the source run

### Requirement: Replays produce distinct append-only observations

Every explicit replay SHALL have a distinct audit-run identity and SHALL mark its value observations as replay-generated. Replaying an already audited execution SHALL append a new observation set rather than overwriting the earlier local report or dataset rows.

#### Scenario: Previously audited execution is replayed
- **WHEN** an execution session with an existing audit is explicitly audited again
- **THEN** the new audit and its step observations have a new audit-run identity, identify the replay trigger, and preserve the earlier observations

#### Scenario: Retried reporting is not a new replay
- **WHEN** reporting is retried for an existing completed audit report
- **THEN** Agent Runner retries the same observation identities rather than creating a new audit or new observations

### Requirement: Replay is non-mutating

Explicit audit replay SHALL create and use an immutable launch-time snapshot of the selected durable source-run evidence and SHALL treat the source repository as read-only. It SHALL NOT alter the source run's state, outcome, resumability, commits, working tree, or recorded evidence. Later source-run changes SHALL NOT invalidate or silently enter an already launched replay.

#### Scenario: Replay completes
- **WHEN** an explicit replay succeeds or fails
- **THEN** the source run and source repository remain unchanged by the audit

#### Scenario: Source run changes during replay
- **WHEN** the selected source run gains later evidence after the replay's launch snapshot is complete
- **THEN** the replay continues against its snapshot and reports only the selected evidence version
