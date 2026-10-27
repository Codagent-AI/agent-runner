## MODIFIED Requirements

### Requirement: implement-task activates task-compliance by default

`core/implement-task-v1.0` SHALL accept an optional `task_compliance` param that defaults to `true` and accepts only `true` or `false`. Any other value SHALL fail the workflow with an error that names the param, before the implementation agent starts. Its validator step SHALL run only when `.validator/config.yml` exists in the project root. When its validator step runs, `core/implement-task-v1.0` SHALL pass its `task_file` and `task_compliance` values to `core/run-validator`. When `skip_validator` is `true`, no validator command runs, whatever the `task_compliance` value.

`core/implement-task-v2.0`, which the current `simple-change` workflows reach through `core/complete-simple-change-v2.0`, SHALL have no validator steps and SHALL NOT declare `task_compliance` or `skip_validator`.

#### Scenario: Caller that omits the param keeps task-compliance
- **WHEN** a workflow outside the built-in catalog, such as the Agent Factory feature workflow, invokes `builtin:core/implement-task-v1.0.yaml` with `task_file: "openspec/changes/foo/tasks.md"` and no `task_compliance` param
- **AND** the workflow reaches its validator step
- **THEN** the validator command includes `--enable-review task-compliance --context-file "openspec/changes/foo/tasks.md"`

#### Scenario: Disabled task-compliance runs plain validation
- **WHEN** `core/implement-task-v1.0` is invoked with `task_compliance: "false"`
- **AND** the workflow reaches its validator step
- **THEN** the validator command includes `--report` and omits `--enable-review task-compliance` and `--context-file`

#### Scenario: Invalid task_compliance value fails before implementation
- **WHEN** `core/implement-task-v1.0` is invoked with `task_compliance: "maybe"`
- **THEN** the workflow fails with an error naming `task_compliance` before the implementation step starts

#### Scenario: Skipped validator ignores task_compliance
- **WHEN** `core/implement-task-v1.0` is invoked with `skip_validator: "true"` and `task_compliance: "true"`
- **THEN** no validator command runs

#### Scenario: Unconfigured project runs no per-task validator
- **WHEN** `core/implement-task-v1.0` is invoked with `skip_validator: "false"` in a project without `.validator/config.yml`
- **THEN** no validator command runs, and the task's delivery verification still runs

### Requirement: Built-in workflows run without task-compliance

Every built-in workflow that reaches `core/implement-task` or `core/run-validator` with a task file SHALL disable task-compliance. This SHALL hold across all shipped versions of each workflow. The callers are:
- `core/implement-change-v1.0`, which the v2.0 `spec-driven` and `openspec` `change` workflows reach;
- `core/complete-simple-change-v1.0`, which remains embedded and loadable as a pinned child and recorded resume target;
- `spec-driven/implement-change` v1.0 and `openspec/implement-change` v1.0, which the v1.0 `change` workflows reach and which remain loadable as pinned children and recorded resume targets;
- `spec-driven/simple-change` v1.0 and `openspec/simple-change` v1.0, which call `core/run-validator` directly.

The current v2.0 `spec-driven` and `openspec` `simple-change` workflows SHALL NOT validate per task; their validation points, described in `pluggable-validation`, SHALL NOT enable task-compliance. No built-in workflow SHALL rely on the `core/implement-task` default. Prompts in these workflows SHALL NOT state that task-compliance validation ran. Because unresolved final validator failures are non-blocking, the `complete-task-index` prompt SHALL describe validation without stating its outcome, and SHALL NOT claim that validation passed.

#### Scenario: core implement-change disables task-compliance per task
- **WHEN** `core/implement-change-v1.0` runs its per-task loop with `skip_validator: "false"`
- **THEN** each per-task validator command omits `--enable-review task-compliance` and `--context-file`

#### Scenario: v1.0 implement-change workflows disable task-compliance
- **WHEN** a pinned or resumed `spec-driven/implement-change-v1.0.yaml` or `openspec/implement-change-v1.0.yaml` runs its per-task loop
- **THEN** each per-task validator command omits `--enable-review task-compliance` and `--context-file`

#### Scenario: Current simple-change runs no per-task validation
- **WHEN** a v2.0 `spec-driven/simple-change` or `openspec/simple-change` run implements its tasks
- **THEN** no validator command runs between tasks
- **AND** every validator command at its validation points omits `--enable-review task-compliance` and `--context-file`

#### Scenario: v1.0 simple-change workflows run plain validation
- **WHEN** `spec-driven/simple-change-v1.0.yaml` or `openspec/simple-change-v1.0.yaml` reaches its `run-validator` step
- **THEN** the validator command includes `--report` and omits `--enable-review task-compliance` and `--context-file`

#### Scenario: Every embedded implement-task caller is explicit
- **WHEN** the embedded built-in workflow catalog is inspected across all versions
- **THEN** every step that invokes `core/implement-task-v1.0` passes `task_compliance: "false"`
- **AND** no step invokes `core/implement-task-v2.0` with `task_compliance` or `skip_validator`

#### Scenario: Task index prompt does not claim task-compliance
- **WHEN** `core/implement-change-v1.0`'s `complete-task-index` step runs
- **THEN** its prompt does not state that task-compliance validation ran for the completed tasks

#### Scenario: Task index prompt does not claim validation passed
- **WHEN** `core/implement-change-v1.0` reaches `complete-task-index` after a task's final validator verification failed with a warning
- **THEN** the dispatched prompt does not state that the tasks passed validation
