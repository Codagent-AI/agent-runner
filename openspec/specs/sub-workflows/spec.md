# Capability: sub-workflows

## Purpose

Defines how workflow steps can delegate to other workflow files, including parameter passing and session inheritance across workflow boundaries.
## Requirements
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

### Requirement: Session inheritance

A step with `session: inherit` SHALL resume the most recent session from the parent workflow that invoked the current sub-workflow. This allows a sub-workflow's agent steps to continue the session chain started in the parent.

#### Scenario: Inherit resumes parent session
- **WHEN** a sub-workflow step has `session: inherit` and the parent workflow has an active session
- **THEN** the step resumes the parent's most recent session

#### Scenario: Inherit with no parent session
- **WHEN** a sub-workflow step has `session: inherit` but no parent workflow session exists
- **THEN** Agent Runner fails with a descriptive error

#### Scenario: Inherit in a top-level workflow
- **WHEN** a step in a top-level workflow (not a sub-workflow) has `session: inherit`
- **THEN** Agent Runner logs a warning and falls back to a new session (the agent executor's existing try/catch ensures this is non-fatal)

### Requirement: Session resume scoping

`session: resume` SHALL only resume sessions created within the same workflow file. It MUST NOT reach across sub-workflow boundaries to resume a session from a parent or child workflow.

#### Scenario: Resume finds session in same workflow
- **WHEN** a step has `session: resume` and a prior step in the same workflow file created a session
- **THEN** the step resumes that session

#### Scenario: Resume with no prior session in same workflow
- **WHEN** a step has `session: resume` but no prior step in the same workflow file created a session
- **THEN** the runner starts a fresh session (no resume flag passed to the CLI adapter)

#### Scenario: Resume does not cross sub-workflow boundary
- **WHEN** a parent workflow invokes a sub-workflow that created sessions, and the next parent step has `session: resume`
- **THEN** the parent step resumes the parent's own most recent session, not the sub-workflow's

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

### Requirement: Sub-workflow failure signals

A sub-workflow step SHALL report to its calling workflow the failure kind of the failure that ended the child workflow and whether any agent crash was observed anywhere inside the child workflow, as defined by `infrastructure-failure-classification`. This SHALL hold for builtin and user sub-workflows alike, at any nesting depth, and whether the child workflow failed or completed successfully.

#### Scenario: Crash inside a builtin sub-workflow
- **WHEN** a parent step runs `builtin:core/implement-task-v1.0.yaml` and its `generate-code` agent crashes
- **THEN** the parent's sub-workflow step is `failed` with failure kind `infrastructure` and crash observed true

#### Scenario: Red validator inside a builtin sub-workflow
- **WHEN** a parent step runs `builtin:core/verify-change-v1.0.yaml`, every agent inside it finishes, and the validator result check fails
- **THEN** the parent's sub-workflow step is `failed` with failure kind `step` and crash observed false

#### Scenario: Absorbed crash in finalize-pr
- **WHEN** a parent step runs `builtin:core/finalize-pr-v1.0.yaml`, its `fix-pr` agent crashes in the first CI cycle, and a later cycle passes so the sub-workflow completes
- **THEN** the parent's sub-workflow step is `success` with crash observed true

#### Scenario: Crash in a nested sub-workflow
- **WHEN** a sub-workflow calls another sub-workflow whose agent step crashes, and neither absorbs the failure
- **THEN** both sub-workflow steps report failure kind `infrastructure` and crash observed true

### Requirement: Workflow workspace directories

A workflow MAY declare `workspace_dirs`, a list of directory strings at the workflow level. The values are interpolated against the workflow's own parameters and the captured variables visible at that point. Interpolation happens when an agent step, `call_agent` child, or external-user turn in that workflow is about to start, and when one of its sub-workflow steps starts, so values may reference captures from earlier steps. Resume uses the restored captures. An undefined variable at that point SHALL fail that step before any CLI spawns. The resulting directories SHALL apply to every agent step in that workflow and in all of its nested sub-workflows. A nested workflow's directories are the union of its parent's directories and its own. Before any directory is used, the runner SHALL normalize the list:

- empty values are dropped;
- symlinks are resolved;
- duplicates are removed;
- directories equal to the run's project root, or inside it, are dropped.

Every remaining directory SHALL be absolute and SHALL exist. Otherwise the step that triggered evaluation SHALL fail before any CLI spawns or child step runs, with an error naming the value. Agent steps, `call_agent` child agents, and external-user turns SHALL receive the normalized list as additional workspace directories (see `cli-adapter`). A workflow without `workspace_dirs` SHALL inherit its parent's list unchanged.

#### Scenario: Directory applies to nested agent steps
- **WHEN** a workflow with `workspace_dirs: ["{{spec_root}}"]` and `spec_root=/work/specs` invokes a core sub-workflow that runs an agent step
- **THEN** that agent step receives `/work/specs` as an additional workspace directory

#### Scenario: Directory taken from an earlier capture
- **WHEN** a workflow declares `workspace_dirs: ["{{openspec.spec_root}}"]`, its first step captures `openspec` with `spec_root=/work/specs`, and a later step runs an agent
- **THEN** that agent step receives `/work/specs` as an additional workspace directory

#### Scenario: Directory inside the project root is dropped
- **WHEN** a workflow declares `workspace_dirs: ["{{spec_root}}"]` and `spec_root` equals the run's project root
- **THEN** agent steps receive no additional workspace directories

#### Scenario: Missing directory
- **WHEN** a workflow declares `workspace_dirs: ["{{spec_root}}"]` and `spec_root=/work/missing` does not exist
- **THEN** the first agent or sub-workflow step that evaluates the list fails with an error naming `/work/missing`, and no agent spawns

