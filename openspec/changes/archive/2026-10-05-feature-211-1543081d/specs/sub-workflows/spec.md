## ADDED Requirements

### Requirement: Sub-workflow failure signals

A sub-workflow step SHALL report to its calling workflow the failure kind of the failure that ended the child workflow and whether any agent crash was observed anywhere inside the child workflow, as defined by `infrastructure-failure-classification`. This SHALL hold for builtin and user sub-workflows alike, at any nesting depth, and whether the child workflow failed or completed successfully.

#### Scenario: Crash inside a builtin sub-workflow
- **WHEN** a parent step runs `builtin:core/implement-task-v1.0.yaml` and its `generate-code` agent crashes
- **THEN** the parent's sub-workflow step is `failed` with failure kind `infrastructure` and crash observed true

#### Scenario: Red validator inside a builtin sub-workflow
- **WHEN** a parent step runs `builtin:core/verify-change-v1.0.yaml`, every agent inside it finishes, and the validator result check fails
- **THEN** the parent's sub-workflow step is `failed` with failure kind `step` and crash observed false

#### Scenario: Absorbed crash in finalize-pr
- **WHEN** a parent step runs `builtin:core/finalize-pr-v1.0.yaml`, its `wait-ci` agent crashes in the first CI cycle, and a later cycle passes so the sub-workflow completes
- **THEN** the parent's sub-workflow step is `success` with crash observed true

#### Scenario: Crash in a nested sub-workflow
- **WHEN** a sub-workflow calls another sub-workflow whose agent step crashes, and neither absorbs the failure
- **THEN** both sub-workflow steps report failure kind `infrastructure` and crash observed true
