## ADDED Requirements

### Requirement: Agent-call summary rollup and drill-down

An agent step with accepted agent calls SHALL be a drillable container in the run summary. In its enclosing scope, the parent row SHALL roll up the parent turn's own usage and cost together with every called-agent execution exactly once. Its duration SHALL use the parent step's wall-clock attempt duration, including all repeated attempts, and MUST NOT add called-agent durations because they overlap time spent waiting within the parent.

Entering the parent row SHALL show a `parent turn` row followed by one row per accepted call in invocation order. The `parent turn` row SHALL aggregate only the parent step's own attempts. Each call row SHALL show its independent status and metrics and SHALL use `call session: <name>` or `call agent: <profile>` to identify its target. The scope Total SHALL sum usage and cost from the `parent turn` and call rows while retaining the parent step's wall-clock duration. An agent step without accepted calls SHALL remain an ordinary leaf row.

#### Scenario: Parent row rolls up own and call metrics
- **WHEN** a parent agent step and two called agents report usage or cost
- **THEN** the parent's enclosing summary row includes the parent turn and both calls exactly once

#### Scenario: Parent duration does not double-count calls
- **WHEN** a parent step runs for 60 seconds while synchronously waiting 30 seconds for a called agent
- **THEN** the parent row and its drilled scope report 60 seconds rather than 90 seconds

#### Scenario: Drill-down separates parent turn and calls
- **WHEN** the user enters a summary row whose agent step made two accepted calls
- **THEN** the scope shows `parent turn` followed by two call rows in invocation order

#### Scenario: Call targets are explicit
- **WHEN** the drilled scope contains a named-session call and a profile call
- **THEN** their rows are labeled `call session: <name>` and `call agent: <profile>` respectively

#### Scenario: Failed call remains independently visible
- **WHEN** a parent recovers from a failed call and completes successfully
- **THEN** the drilled summary retains the failed call row beneath the successful parent scope

#### Scenario: Repeated parent attempts are aggregated
- **WHEN** a logical parent agent step runs more than one attempt and those attempts make accepted calls
- **THEN** `parent turn` aggregates the parent attempts while each accepted call remains a separate chronological row

#### Scenario: Agent without calls remains a leaf
- **WHEN** an agent step has no accepted agent calls
- **THEN** its summary row retains ordinary leaf behavior
