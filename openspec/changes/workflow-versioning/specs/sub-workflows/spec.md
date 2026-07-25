## MODIFIED Requirements

### Requirement: Sub-workflow invocation

A step with a `workflow` field SHALL load and execute the exact referenced workflow file. The resolved reference MUST use a valid versioned workflow filename and MUST NOT be replaced by a newer available version. The step MUST NOT have `prompt`, `command`, or `mode` — it delegates entirely to the sub-workflow. The sub-workflow executes in the same process as the parent.

#### Scenario: Sub-workflow executes successfully
- **WHEN** a step has `workflow: workflows/run-validator-v1.0.yaml` and the referenced file exists
- **THEN** Agent Runner loads that exact sub-workflow version, executes its steps, and continues with the next step in the parent

#### Scenario: Sub-workflow file not found
- **WHEN** a step has `workflow: workflows/missing-v1.0.yaml` and the file does not exist
- **THEN** Agent Runner fails with a descriptive error naming the missing versioned file

#### Scenario: Unversioned sub-workflow rejected
- **WHEN** a step has `workflow: workflows/run-validator.yaml`
- **THEN** Agent Runner fails with the actionable versioned-filename error

#### Scenario: Sub-workflow step is mutually exclusive with prompt/command/mode
- **WHEN** a step has both `workflow` and `prompt` (or `command` or `mode`)
- **THEN** Agent Runner fails at load time with a validation error

### Requirement: Parameter passing to sub-workflows

A step with `workflow` MAY include a `params` map that passes values to the sub-workflow. Values support `{{var}}` interpolation. The sub-workflow SHALL receive only the parameters explicitly passed — it MUST NOT implicitly inherit the parent's parameter scope.

#### Scenario: Parameters passed to sub-workflow
- **WHEN** a step has `workflow: workflows/implement-task-v1.0.yaml` and `params: { task_file: "{{task_file}}" }`
- **THEN** the sub-workflow receives `task_file` as a parameter and can reference it via `{{task_file}}`

#### Scenario: Missing required parameter
- **WHEN** a sub-workflow declares a required parameter and the parent step's `params` map does not include it
- **THEN** Agent Runner fails with a descriptive error naming the missing parameter

#### Scenario: Sub-workflow does not inherit parent params implicitly
- **WHEN** the parent workflow has a parameter `change_name` but the step's `params` map does not pass it
- **THEN** the sub-workflow cannot reference `{{change_name}}`

### Requirement: Pre-validation catches broken sub-workflows before run start

For fresh runs that are not builtin workflows (i.e., not skipped by the pre-validation skip rule defined in `workflow-pre-validation`), every reachable sub-workflow SHALL be loaded and validated before any step in the root workflow executes. Every resolved sub-workflow target MUST use a valid versioned workflow filename. Errors that today would surface lazily at sub-workflow dispatch SHALL surface at run start.

For builtin workflow runs (which the skip rule excludes from pre-validation), broken sub-workflows continue to surface lazily at dispatch — the agent-runner repo's build-time agent-validator check is responsible for ensuring builtins do not ship broken.

#### Scenario: Missing sub-workflow file fails at run start, not at dispatch
- **WHEN** a non-builtin root workflow references `workflow: workflows/missing-v1.0.yaml` and the file does not exist
- **THEN** pre-validation fails before any root step executes, with an error naming the missing file and the referencing step

#### Scenario: Sub-workflow with broken sessions fails at run start
- **WHEN** a non-builtin root reaches a versioned sub-workflow that has a `session: implementor` reference but no workflow in the composition tree declares `implementor`
- **THEN** pre-validation fails before any root step executes, with an error naming the unresolved reference and the file that contains it

#### Scenario: Project workflow with a broken sub-workflow fails at run start
- **WHEN** the cwd contains `.agent-runner/workflows/deploy-v1.0.yaml` referencing a versioned sub-workflow with a syntax error and `agent-runner deploy` is invoked
- **THEN** pre-validation fails before any deploy step executes (project workflows are not skipped)

#### Scenario: Builtin run falls back to lazy dispatch failure
- **WHEN** a builtin root workflow (e.g., `agent-runner core:finalize-pr`) reaches a broken versioned sub-workflow at runtime
- **THEN** the failure surfaces at dispatch, as in pre-existing behavior — pre-validation does not run for builtin roots, since builtins are gated by the agent-runner repo's build-time check
