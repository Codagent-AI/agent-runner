# Capability: audit-log-entries (delta)

## MODIFIED Requirements

### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `error`, and — for interactive-step control and supervision — `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, and `child_continued`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Control-plane events recognized
- **WHEN** the audit logger receives a `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, or `control_rejected` event during an interactive step
- **THEN** it writes the entry without error, as an intermediate event distinct from `step_end`

#### Scenario: Job-control events recognized
- **WHEN** the audit logger receives a `child_stopped` or `child_continued` event while an interactive step's child is suspended or resumed
- **THEN** it writes the entry without error
