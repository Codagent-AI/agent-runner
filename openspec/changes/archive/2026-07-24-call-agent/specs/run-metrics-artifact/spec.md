## ADDED Requirements

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
