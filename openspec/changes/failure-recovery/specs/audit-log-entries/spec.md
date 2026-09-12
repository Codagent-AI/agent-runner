# Capability: audit-log-entries

## Purpose

Defines the structure and content of audit log entries.

## MODIFIED Requirements

### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `agent_call_start`, `agent_call_end`, `error`, `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, `child_continued`, `repair_attempt_start`, `repair_attempt_end`, and `repair_blocked`.

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

### Requirement: Shell step-specific data

Shell step entries SHALL include the interpolated command on `step_start`, and exit code, captured stdout (if capture set), and stderr on `step_end`. A failed shell or script `step_end` SHALL also include the failure record: the guarded agent execution's prefix and attempt when one exists, and, for a step with `repair`, the repair form, target, attempts used, and whether repair ended blocked. Each internal check run inside a repair cycle SHALL be recorded as its own step execution under the attempt prefix (`[<check>, attempt:N, <check>]`) with ordinary shell or script start and end data; `repair_attempt_end` SHALL summarize the attempt with its number, form, and the internal run's outcome and exit code. The owning check SHALL emit exactly one `step_start` and one `step_end`.

#### Scenario: Shell step start
- **WHEN** a shell step starts with interpolated command `npm test`
- **THEN** the `step_start` entry includes `command: "npm test"`

#### Scenario: Shell step end with capture
- **WHEN** a shell step with `capture: test_output` completes with exit code 0
- **THEN** the `step_end` entry includes exit code, captured stdout, and stderr

#### Scenario: Shell step end without capture
- **WHEN** a shell step without `capture` completes with exit code 1
- **THEN** the `step_end` entry includes exit code and stderr, but no stdout

#### Scenario: Failed check end records evidence linkage
- **WHEN** `verify-draft-pr` fails after `open-draft-pr` declared blocked
- **THEN** its `step_end` includes the `open-draft-pr` execution prefix and attempt, `repair_form: rerun`, `repair_target: open-draft-pr`, `repair_attempts: 0`, and `repair_blocked: true`

#### Scenario: Repair attempt end records the check run
- **WHEN** a check reruns after an inline repair and exits 0
- **THEN** a step execution with prefix `[<check>, attempt:1, <check>]` records exit code 0 and the output, the `repair_attempt_end` for attempt 1 records outcome `success` and exit code 0, and the owning check's single `step_end` records outcome `success`
