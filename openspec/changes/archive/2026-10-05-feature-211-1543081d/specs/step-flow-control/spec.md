## ADDED Requirements

### Requirement: Previous step failure signals

Alongside the outcome that `skip_if: previous_success` evaluates, Agent Runner SHALL make the same preceding step's failure kind and crash-observed signal (see `infrastructure-failure-classification`) available to the next step in the same scope. They SHALL be exposed through the built-in variables `{{last_step_failure_kind}}` and `{{last_step_crash_observed}}`. These signals SHALL NOT change the meaning of `continue_on_failure`, `skip_if: previous_success`, or `break_if`. A workflow can branch on a crash only by reading the signals explicitly, for example in a `sh:` `skip_if` expression or a shell command.

#### Scenario: Branch on a crashed sub-workflow
- **WHEN** a sub-workflow step with `continue_on_failure: true` fails because an agent inside it crashed, and the next step has `skip_if: 'sh: test "{{last_step_crash_observed}}" != true'`
- **THEN** the next step runs

#### Scenario: Ordinary failure does not trigger the crash branch
- **WHEN** a sub-workflow step with `continue_on_failure: true` fails because a validator check exited non-zero and no agent inside it crashed, and the next step has `skip_if: 'sh: test "{{last_step_crash_observed}}" != true'`
- **THEN** the next step is skipped

#### Scenario: previous_success unaffected by a recovered crash
- **WHEN** a group absorbed an agent crash, completed successfully, and the next step has `skip_if: previous_success`
- **THEN** the next step is skipped

### Requirement: Preceding-step tracking survives resume

When a run is resumed, each resumable scope (the top-level workflow, a sub-workflow, a loop iteration body) SHALL restore the preceding-step record it held at the interruption before running its first step. The record is the outcome, failure kind, and crash-observed signal. `skip_if: previous_success` and the last-step built-in variables SHALL therefore evaluate the same preceding step after resume as they would have without the interruption. A run persisted without this record SHALL resume with no preceding step, which is today's behavior.

#### Scenario: previous_success after resume
- **WHEN** a step succeeds, the run is interrupted before the next step starts, and the user resumes, and the next step has `skip_if: previous_success`
- **THEN** the next step is skipped

#### Scenario: Crash signals after resume inside a sub-workflow
- **WHEN** inside a sub-workflow an agent step with `continue_on_failure: true` crashes, the run is interrupted before the next child step starts, and the user resumes
- **THEN** the next child step sees `{{last_step_failure_kind}}` = `infrastructure` and `{{last_step_crash_observed}}` = `true`
