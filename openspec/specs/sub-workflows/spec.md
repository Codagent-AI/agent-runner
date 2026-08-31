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

### Requirement: Child workflow scope is authoritative
A sub-workflow invocation MUST use the referenced child workflow's declared scope. The parent workflow's default scope MUST NOT replace the child's declared scope, and a sub-workflow step MUST NOT override it.

#### Scenario: Workspace parent invokes workspace child
- **WHEN** a workspace-scoped parent invokes a workspace-scoped child
- **THEN** the child executes once in workspace scope

#### Scenario: Workspace parent invokes repository child
- **WHEN** a workspace-scoped parent invokes a repository-scoped child with the required `repositories` parameter
- **THEN** the child executes once for each selected repository in order

#### Scenario: Repository child invoked directly
- **WHEN** the same repository-scoped child is invoked as a top-level workflow with `repositories`
- **THEN** it uses the same repository-scoped behavior as when composed under a parent

#### Scenario: Parent default does not replace child default
- **WHEN** a parent and child declare different scopes
- **THEN** the child uses its own declared scope rather than inheriting the parent's default

#### Scenario: Scope override on sub-workflow step
- **WHEN** a sub-workflow step declares `scope`
- **THEN** workflow validation fails before the child can execute

### Requirement: Valid scoped workflow composition
A workspace-scoped workflow MAY invoke workspace-scoped, repository-scoped, or legacy unscoped children. A repository-scoped workflow MAY invoke repository-scoped or legacy unscoped children but MUST NOT invoke a workspace-scoped child.

#### Scenario: Repository parent invokes repository child
- **WHEN** a repository-scoped parent invokes a repository-scoped child while a repository is active
- **THEN** the child executes once in the active repository context without starting another fan-out

#### Scenario: Workspace parent invokes mixed-scope children
- **WHEN** a workspace-scoped parent invokes a workspace child and a repository child in sequence
- **THEN** the workspace child executes once and the repository child executes for each selected repository

#### Scenario: Repository parent invokes workspace child
- **WHEN** a repository-scoped parent references a workspace-scoped child
- **THEN** workflow composition validation fails and requires the workspace child to be invoked from a workspace-scoped parent

#### Scenario: Scoped parent invokes legacy child
- **WHEN** a scoped parent invokes an unscoped legacy child
- **THEN** the child retains legacy behavior and executes once in the parent's effective execution context

### Requirement: Scoped context propagation
Sub-workflows MUST receive workspace and active-repository execution context without implicitly inheriting ordinary workflow parameters.

#### Scenario: Workspace context crosses child boundary
- **WHEN** a parent invokes a child workflow
- **THEN** the canonical workspace root and workspace-owned run identity, state, and audit context remain unchanged in the child

#### Scenario: Active repository crosses child boundary
- **WHEN** a child workflow is invoked while repository `backend` is active
- **THEN** the child receives backend as its active repository context

#### Scenario: Repository targets passed explicitly
- **WHEN** a parent invokes a repository-scoped child
- **THEN** the child receives `repositories` only when the parent includes it in the sub-workflow `params` map

#### Scenario: Missing repository targets
- **WHEN** a parent in a configured workspace invokes a repository-scoped child without passing its required `repositories` parameter
- **THEN** Agent Runner fails before the child begins execution

#### Scenario: Implicit child target remains compatible
- **WHEN** a parent with no repository declarations invokes a repository-scoped child without an explicit target value
- **THEN** Agent Runner supplies the transparent implicit repository and executes the child once without exposing `default`

