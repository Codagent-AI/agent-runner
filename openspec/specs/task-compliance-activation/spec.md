# task-compliance-activation Specification

## Purpose
Define task-compliance activation in validator setup and the optional task-file context passed through validation workflows.
## Requirements
### Requirement: Optional task_file param on run-validator

The `core/run-validator` built-in workflow SHALL accept an optional `task_file` param defaulting to the empty string. Callers that do not need task-compliance activation SHALL be able to invoke `run-validator` without supplying the param.

#### Scenario: Param defaults to empty when caller omits it
- **WHEN** `core/run-validator` is invoked with no `task_file` param
- **THEN** the workflow executes successfully and `{{task_file}}` resolves to the empty string

#### Scenario: Existing caller without task_file unaffected
- **WHEN** `spec-driven/implement-change` invokes `core/run-validator` as it does today (no params block, no `task_file`)
- **THEN** the sub-workflow runs without error

### Requirement: Task-compliance flags activated when task_file is set

When `core/run-validator` is invoked with a non-empty `task_file` param, the validator command issued in the `run-validator` step SHALL include `--enable-review task-compliance` and `--context-file <task_file>` in addition to `--report`. When `task_file` is empty, the validator command SHALL NOT include either flag.

#### Scenario: Task_file set adds the flags
- **WHEN** `core/run-validator` is invoked with `task_file: "openspec/changes/foo/tasks.md"`
- **THEN** the validator command executed is `agent-validator run --report --enable-review task-compliance --context-file "openspec/changes/foo/tasks.md"`

#### Scenario: Task_file empty omits the flags
- **WHEN** `core/run-validator` is invoked with `task_file: ""` (or the param omitted)
- **THEN** the validator command executed is `agent-validator run --report`

### Requirement: Per-task workflows propagate task_file

Built-in workflows that implement against a single task file SHALL pass that task file to the `core/run-validator` sub-workflow call so that task-compliance is activated. This applies to `core/implement-task`, `spec-driven/simple-change`, and `openspec/simple-change`.

#### Scenario: implement-task forwards task_file
- **WHEN** `core/implement-task` is invoked with `task_file: "openspec/changes/foo/tasks/01.md"`
- **AND** the workflow reaches the `run-validator` step
- **THEN** the sub-workflow is invoked with `task_file: "openspec/changes/foo/tasks/01.md"` and the validator command includes the activation flags

#### Scenario: spec-driven simple-change forwards captured task_file
- **WHEN** `spec-driven/simple-change` runs and its `locate-task` step captures `task_file`
- **AND** the workflow reaches the `run-validator` step
- **THEN** the sub-workflow is invoked with that captured `task_file` value and the validator command includes the activation flags

#### Scenario: openspec simple-change forwards the change tasks path
- **WHEN** `openspec/simple-change` is invoked with `change_name: "foo"`
- **AND** the workflow reaches the `run-validator` step
- **THEN** the sub-workflow is invoked with `task_file: "openspec/changes/foo/tasks.md"` and the validator command includes the activation flags

### Requirement: validator-init requests task-compliance scaffolding

The `agent-runner internal validator-init` command SHALL invoke `agent-validator init` with `--enable-builtin task-compliance` so that fresh validator scaffolds in consumer projects include the opt-in review entry.

#### Scenario: Fresh project scaffolds the entry
- **WHEN** a user runs the agent-runner onboarding flow in a project without a `.validator/` directory
- **AND** the onboarding workflow invokes `agent-runner internal validator-init`
- **THEN** `agent-validator init` is invoked with `--enable-builtin task-compliance`
- **AND** the resulting `.validator/config.yml` contains a `task-compliance` review entry with `builtin: task-compliance` and `enabled: false`

#### Scenario: Existing .validator/ directory surfaces a warning
- **WHEN** a user runs `agent-runner internal validator-init` in a project that already has a `.validator/` directory
- **THEN** `agent-validator init` runs with `--enable-builtin task-compliance`
- **AND** agent-validator prints its "paste this into your config" warning (the agent-runner side does not retry or patch the config)

### Requirement: agent-runner repo config carries the entry

The `.validator/config.yml` checked into the agent-runner repository SHALL include a `task-compliance` review entry under the root entry point's `reviews:` list with `builtin: task-compliance` and `enabled: false`, alongside the activation comment.

#### Scenario: Entry present in repo config
- **WHEN** the file `.validator/config.yml` is read at the repo root
- **THEN** the entry point with `path: "."` contains a review entry keyed `task-compliance` whose `builtin` is `task-compliance` and whose `enabled` is `false`

#### Scenario: Activator runs against repo locally
- **WHEN** a developer runs `agent-validator run --report --enable-review task-compliance --context-file <some-task-file>` in the agent-runner repo
- **THEN** the `task-compliance` review is activated (the config entry is present to flip)
