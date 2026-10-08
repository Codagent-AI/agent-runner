## ADDED Requirements

### Requirement: Failure classification on end events

The `step_end`, `iteration_end`, `sub_workflow_end`, and `run_end` events SHALL carry the failure classification defined by `infrastructure-failure-classification`, alongside and without altering the existing `outcome` field:

- `failure_kind`: `infrastructure` or `step` when the outcome is `failed` or `exhausted`. It SHALL be absent for other outcomes.
- `crash_observed`: a boolean, present on every end event except those for skipped steps.
- `failure_origin`: present when `failure_kind` is `infrastructure`. It SHALL identify the crashed agent execution by step ID, full audit prefix, and attempt, and SHALL carry a bounded excerpt of its error or stderr.

The `run_end` values SHALL describe the whole run, so the classification can be read from `run_end` alone without the workflow definition.

#### Scenario: Crashed agent step end
- **WHEN** a headless agent step's CLI exits non-zero with stderr `Selected model is at capacity`
- **THEN** its `step_end` has `outcome: "failed"`, `failure_kind: "infrastructure"`, `crash_observed: true`, and a `failure_origin` naming the step's prefix and attempt with the stderr excerpt

#### Scenario: Ordinary failed check end
- **WHEN** a shell check exits non-zero
- **THEN** its `step_end` has `outcome: "failed"`, `failure_kind: "step"`, `crash_observed: false`, and no `failure_origin`

#### Scenario: Successful sub-workflow end after absorbed crash
- **WHEN** a sub-workflow completes successfully after absorbing an agent crash
- **THEN** its `sub_workflow_end` has `outcome: "success"`, `crash_observed: true`, and no `failure_kind`

#### Scenario: Run end after a crash stopped the run
- **WHEN** a run stops because an agent inside a builtin sub-workflow crashed
- **THEN** `run_end` has `outcome: "failed"`, `failure_kind: "infrastructure"`, `crash_observed: true`, and a `failure_origin` carrying the crashed agent's full nested prefix

#### Scenario: Run end after an orchestration error following an absorbed crash
- **WHEN** an agent crash is absorbed by `continue_on_failure` and the run then stops because a later step's `skip_if` expression fails to evaluate
- **THEN** `run_end` has `outcome: "failed"`, `failure_kind: "step"`, `crash_observed: true`, and no `failure_origin`

#### Scenario: Clean run end
- **WHEN** a run completes with no agent crash
- **THEN** `run_end` has `crash_observed: false` and no `failure_kind`
