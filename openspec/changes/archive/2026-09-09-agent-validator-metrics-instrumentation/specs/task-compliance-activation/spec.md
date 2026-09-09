## MODIFIED Requirements

### Requirement: Task-compliance flags activated when task_file is set

When `core/run-validator` is invoked with a non-empty `task_file` param, the validator command issued in the `run-validator` step SHALL include `--enable-review task-compliance` and `--context-file <task_file>` in addition to `--report`. When `task_file` is empty, the validator command SHALL NOT include either flag. In either branch, when durable metrics correlation is enabled, the command SHALL additionally include `--metrics-consumer agent-runner --metrics-context <id>` using the context provided by Runner. These correlation arguments SHALL remain independent of task-compliance activation. If instrumentation cannot be enabled, the original validation command SHALL still run without newly added metrics flags.

#### Scenario: Task_file set adds the flags
- **WHEN** `core/run-validator` is invoked with `task_file: "openspec/changes/foo/tasks.md"`
- **THEN** the validator command invokes `agent-validator run` and includes `--report --enable-review task-compliance --context-file "openspec/changes/foo/tasks.md"`

#### Scenario: Task_file empty omits the flags
- **WHEN** `core/run-validator` is invoked with `task_file: ""` (or the param omitted)
- **THEN** the validator command invokes `agent-validator run`, includes `--report`, and omits `--enable-review task-compliance` and `--context-file`

#### Scenario: Correlation is independent of task context
- **WHEN** Runner enables durable metrics correlation for a bundled retry or final-verification launch
- **THEN** the command includes the assigned consumer/context flags whether task_file is populated or empty, while task-compliance flags continue to follow task_file

#### Scenario: Instrumentation fallback preserves task compliance
- **WHEN** Runner cannot enable metrics correlation for a launch with a non-empty task_file
- **THEN** validation still runs with the report and task-compliance arguments but without newly added metrics flags

