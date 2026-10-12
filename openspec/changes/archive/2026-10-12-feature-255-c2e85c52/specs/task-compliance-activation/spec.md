## MODIFIED Requirements

### Requirement: Optional task_file param on run-validator

The `core/run-validator` built-in workflow SHALL accept an optional `task_file` param, defaulting to the empty string, and an optional `task_compliance` param, defaulting to `false`. Callers that do not need task-compliance activation SHALL be able to invoke `run-validator` without supplying either param. `task_compliance` SHALL accept only `true` or `false`. Any other value SHALL fail the workflow with an error that names the param, before any validator command runs.

#### Scenario: Param defaults to empty when caller omits it
- **WHEN** `core/run-validator` is invoked with no `task_file` and no `task_compliance` param
- **THEN** the workflow executes successfully, `{{task_file}}` resolves to the empty string, and task-compliance is not activated

#### Scenario: Existing caller without task_file unaffected
- **WHEN** `spec-driven/implement-change` invokes `core/run-validator` as it does today (no params block, no `task_file`)
- **THEN** the sub-workflow runs without error

#### Scenario: Invalid task_compliance value fails fast
- **WHEN** `core/run-validator` is invoked with `task_compliance: "yes"`
- **THEN** the workflow fails with an error naming `task_compliance` and no `agent-validator` command is issued

## ADDED Requirements

### Requirement: Task-compliance flags activated only by explicit opt-in

`core/run-validator` SHALL activate task-compliance only when it is invoked with `task_compliance: "true"`. In that case, the validator command issued by its validation steps SHALL include `--enable-review task-compliance` and `--context-file <task_file>`, in addition to `--report`. When `task_compliance` is `false` or omitted, the validator command SHALL NOT include either flag, even when `task_file` is non-empty. When `task_compliance` is `true` and `task_file` is empty, the workflow SHALL fail with an error that names `task_file`, before any validator command runs.

When durable metrics correlation is enabled, the command SHALL additionally include `--metrics-consumer agent-runner --metrics-context <id>`, using the context provided by Runner, whether or not task-compliance is activated. These correlation arguments SHALL remain independent of task-compliance activation. If instrumentation cannot be enabled, the original validation command SHALL still run, without the newly added metrics flags.

#### Scenario: Opt-in adds the flags
- **WHEN** `core/run-validator` is invoked with `task_compliance: "true"` and `task_file: "openspec/changes/foo/tasks.md"`
- **THEN** the validator command invokes `agent-validator run` and includes `--report --enable-review task-compliance --context-file "openspec/changes/foo/tasks.md"`

#### Scenario: task_file alone no longer activates
- **WHEN** `core/run-validator` is invoked with `task_file: "openspec/changes/foo/tasks.md"` and `task_compliance` omitted
- **THEN** the validator command invokes `agent-validator run`, includes `--report`, and omits `--enable-review task-compliance` and `--context-file`

#### Scenario: Explicit opt-out omits the flags
- **WHEN** `core/run-validator` is invoked with `task_compliance: "false"` and a non-empty `task_file`
- **THEN** the validator command omits `--enable-review task-compliance` and `--context-file`

#### Scenario: Opt-in without a task file fails
- **WHEN** `core/run-validator` is invoked with `task_compliance: "true"` and an empty `task_file`
- **THEN** the workflow fails with an error naming `task_file` and no `agent-validator` command is issued

#### Scenario: Correlation is independent of task-compliance
- **WHEN** Runner enables durable metrics correlation for a bundled retry or final-verification launch
- **THEN** the command includes the assigned consumer/context flags whether task-compliance is activated or not, and the task-compliance flags continue to follow `task_compliance`

#### Scenario: Instrumentation fallback preserves task compliance
- **WHEN** Runner cannot enable metrics correlation for a launch with `task_compliance: "true"`
- **THEN** validation still runs with the report and task-compliance arguments, but without the newly added metrics flags

### Requirement: implement-task activates task-compliance by default

`core/implement-task` SHALL accept an optional `task_compliance` param that defaults to `true` and accepts only `true` or `false`. Any other value SHALL fail the workflow with an error that names the param, before the implementation agent starts. When its validator step runs, `core/implement-task` SHALL pass its `task_file` and `task_compliance` values to `core/run-validator`. When `skip_validator` is `true`, no validator command runs, whatever the `task_compliance` value.

#### Scenario: Caller that omits the param keeps task-compliance
- **WHEN** a workflow outside the built-in catalog, such as the Agent Factory feature workflow, invokes `builtin:core/implement-task-v1.0.yaml` with `task_file: "openspec/changes/foo/tasks.md"` and no `task_compliance` param
- **AND** the workflow reaches its validator step
- **THEN** the validator command includes `--enable-review task-compliance --context-file "openspec/changes/foo/tasks.md"`

#### Scenario: Disabled task-compliance runs plain validation
- **WHEN** `core/implement-task` is invoked with `task_compliance: "false"`
- **AND** the workflow reaches its validator step
- **THEN** the validator command includes `--report` and omits `--enable-review task-compliance` and `--context-file`

#### Scenario: Invalid task_compliance value fails before implementation
- **WHEN** `core/implement-task` is invoked with `task_compliance: "maybe"`
- **THEN** the workflow fails with an error naming `task_compliance` before the implementation step starts

#### Scenario: Skipped validator ignores task_compliance
- **WHEN** `core/implement-task` is invoked with `skip_validator: "true"` and `task_compliance: "true"`
- **THEN** no validator command runs

### Requirement: Built-in workflows run without task-compliance

Every built-in workflow that reaches `core/implement-task` or `core/run-validator` with a task file SHALL disable task-compliance. This SHALL hold across all shipped versions of each workflow. The callers are:
- `core/implement-change`, which the v2.0 `spec-driven` and `openspec` `change` and `implement-change` workflows and `core/plan-and-implement-change` reach;
- `core/complete-simple-change`, which the v2.0 `spec-driven` and `openspec` `simple-change` workflows reach;
- `spec-driven/implement-change` v1.0 and `openspec/implement-change` v1.0, which the v1.0 `change` workflows reach and which remain loadable as pinned children and recorded resume targets;
- `spec-driven/simple-change` v1.0 and `openspec/simple-change` v1.0, which call `core/run-validator` directly.

No built-in workflow SHALL rely on the `core/implement-task` default. Prompts in these workflows SHALL NOT state that task-compliance validation ran. Because unresolved final validator failures are non-blocking, the `complete-task-index` prompt SHALL describe validation without stating its outcome, and SHALL NOT claim that validation passed.

#### Scenario: core implement-change disables task-compliance per task
- **WHEN** `core/implement-change` runs its per-task loop with `skip_validator: "false"`
- **THEN** each per-task validator command omits `--enable-review task-compliance` and `--context-file`

#### Scenario: v1.0 implement-change workflows disable task-compliance
- **WHEN** a pinned or resumed `spec-driven/implement-change-v1.0.yaml` or `openspec/implement-change-v1.0.yaml` runs its per-task loop
- **THEN** each per-task validator command omits `--enable-review task-compliance` and `--context-file`

#### Scenario: complete-simple-change disables task-compliance
- **WHEN** a v2.0 `spec-driven/simple-change` or `openspec/simple-change` run reaches `core/complete-simple-change`'s implement step
- **THEN** its validator command omits `--enable-review task-compliance` and `--context-file`

#### Scenario: v1.0 simple-change workflows run plain validation
- **WHEN** `spec-driven/simple-change-v1.0.yaml` or `openspec/simple-change-v1.0.yaml` reaches its `run-validator` step
- **THEN** the validator command includes `--report` and omits `--enable-review task-compliance` and `--context-file`

#### Scenario: Every embedded implement-task caller is explicit
- **WHEN** the embedded built-in workflow catalog is inspected across all versions
- **THEN** every step that invokes `core/implement-task` passes `task_compliance: "false"`

#### Scenario: Task index prompt does not claim task-compliance
- **WHEN** `core/implement-change`'s `complete-task-index` step runs
- **THEN** its prompt does not state that task-compliance validation ran for the completed tasks

#### Scenario: Task index prompt does not claim validation passed
- **WHEN** `core/implement-change` reaches `complete-task-index` after a task's final validator verification failed with a warning
- **THEN** the dispatched prompt does not state that the tasks passed validation

## REMOVED Requirements

### Requirement: Task-compliance flags activated when task_file is set

**Reason**: A non-empty `task_file` no longer activates task-compliance implicitly. The and-scene eval showed no benefit from the review in direct runs, so activation must be an explicit caller choice.

**Migration**: Pass `task_compliance: "true"` together with a non-empty `task_file` to `core/run-validator`. See "Task-compliance flags activated only by explicit opt-in".

### Requirement: Per-task workflows propagate task_file

**Reason**: The built-in per-task workflows no longer activate task-compliance. Only `core/implement-task`'s default activates it, which preserves the Agent Factory's direct call.

**Migration**: Callers that want task-compliance invoke `core/implement-task` without overriding `task_compliance`, or pass `task_compliance: "true"` to `core/run-validator`. See "implement-task activates task-compliance by default" and "Built-in workflows run without task-compliance".
