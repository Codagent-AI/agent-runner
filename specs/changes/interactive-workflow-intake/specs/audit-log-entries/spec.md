## ADDED Requirements

### Requirement: Route event data

Route lifecycle events SHALL carry enough structured data to answer, from the intake run's evidence alone, which workflow was selected, which exact workflow definition it resolved to, which parameters were supplied, where the sealed handoff lives, and whether a launch was attempted and with what outcome. A `route_rejected` event SHALL record the rejection reason. Events SHALL NOT record a launched run ID, because the launched run does not exist while the intake run's evidence is being written.

Launch-attempt evidence is written **after** normal run finalization has already closed the run's audit log, so it SHALL be appended to the run's audit log through a mechanism that does not depend on the closed run logger, and it SHALL be valid for these entries to follow `run_end`. A launch that fails SHALL be recorded as such, so the evidence distinguishes an attempted launch from a successful one.

#### Scenario: Acceptance records the sealed route
- **WHEN** a route submission is accepted
- **THEN** the `route_accepted` entry records the selected workflow, the exact resolved workflow source reference, the supplied parameters, and the sealed handoff location

#### Scenario: Rejection records the reason
- **WHEN** a route submission is rejected
- **THEN** the `route_rejected` entry records the reason for the rejection

#### Scenario: Launch attempt is recorded after finalization
- **WHEN** an intake run finishes successfully with a frozen route and a launch is attempted
- **THEN** a `route_launch_attempted` entry naming the workflow being launched is present in the intake run's audit log
- **AND** it appears after that run's `run_end` entry
- **AND** it contains no launched-run ID

#### Scenario: Launch failure is recorded
- **WHEN** the launch attempt fails
- **THEN** a `route_launch_failed` entry recording the cause is appended to the intake run's audit log

## MODIFIED Requirements

### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `error`, `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, `child_continued`, `route_submitted`, `route_accepted`, `route_rejected`, `route_frozen`, `route_launch_attempted`, and `route_launch_failed`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Completion events are intermediate
- **WHEN** the audit logger receives control or durability events during an interactive agent step
- **THEN** it writes them as intermediate events distinct from the step's final `step_end`

#### Scenario: Route events are intermediate
- **WHEN** the audit logger receives route lifecycle events during the intake agent step
- **THEN** it writes them as intermediate events distinct from the step's final `step_end`
