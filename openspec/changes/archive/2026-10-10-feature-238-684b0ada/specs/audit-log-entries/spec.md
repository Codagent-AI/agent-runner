## MODIFIED Requirements

### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `agent_call_start`, `agent_call_end`, `warning`, `error`, `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `terminal_ownership`, `child_stopped`, `child_continued`, `repair_attempt_start`, `repair_attempt_end`, `repair_blocked`, `route_submitted`, `route_accepted`, `route_rejected`, `route_frozen`, `route_launch_attempted`, `route_launch_failed`, and `pull_request_recorded`. Development-audit builds SHALL additionally support `audit_launch_requested`, `audit_launched`, `audit_launch_failed`, `audit_completed`, and `audit_reporting_warning`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Completion events are intermediate
- **WHEN** the audit logger receives control or durability events during an interactive agent step
- **THEN** it writes them as intermediate events distinct from the step's final `step_end`

#### Scenario: Agent-call events are distinct from workflow steps
- **WHEN** the audit logger receives an `agent_call_start` or `agent_call_end` event
- **THEN** it records the event without representing the call as a workflow `step_start` or `step_end`

#### Scenario: Repair events are intermediate
- **WHEN** the audit logger receives `repair_attempt_start`, `repair_attempt_end`, or `repair_blocked` for a check
- **THEN** it writes them as intermediate events between that check's `step_start` and final `step_end`, carrying the check's prefix and the attempt number

#### Scenario: Job-control and terminal-ownership events recognized
- **WHEN** an interactive step's child is suspended or resumed, or Agent Runner transfers, recovers, or reclaims terminal foreground ownership
- **THEN** the audit logger writes `child_stopped`, `child_continued`, or `terminal_ownership` events carrying process and process-group identifiers but no terminal input or output
