# builtin-vars Specification

## Purpose

Defines the set of built-in template variables the runner exposes to every step, and the precedence rules governing their interaction with workflow-declared params and captured variables.
## Requirements
### Requirement: session_dir built-in variable

The runner SHALL expose `{{session_dir}}` as a built-in template variable in every step that executes within a named run. Its value is the absolute path of the current run's session directory (`~/.agent-runner/projects/<encoded-cwd>/runs/<run-id>/`).

When no session directory is set (e.g., in tests or detached execution contexts), `{{session_dir}}` SHALL NOT be available and attempts to interpolate it fail as an unresolved variable.

#### Scenario: session_dir resolves in a normal run
- **WHEN** a step's prompt or command contains `{{session_dir}}`
- **THEN** the runner replaces it with the absolute path of the run's session directory

#### Scenario: session_dir unavailable without session directory
- **WHEN** the execution context has no session directory configured
- **THEN** `{{session_dir}}` is not present in the built-in variable set

### Requirement: step_id built-in variable

The runner SHALL expose `{{step_id}}` as a built-in template variable whose value is the `id` field of the currently executing step.

`{{step_id}}` is available in: step `prompt`, `command`, `params` values, `skip_if` shell expressions, and sub-workflow `workflow` path fields.

#### Scenario: step_id resolves to current step id
- **WHEN** a step with `id: my-step` contains `{{step_id}}` in its prompt or command
- **THEN** the runner replaces it with `my-step`

#### Scenario: step_id is step-scoped
- **WHEN** two steps in the same workflow each reference `{{step_id}}`
- **THEN** each step sees its own `id`, not the other step's

### Requirement: Built-in precedence

Built-in variables have the **lowest** interpolation precedence except for the reserved `workspace_dir`, `repository_dir`, `repository_name`, and `repository_output_dir` variables. A workflow `params` entry or a captured variable with the same name as a non-reserved built-in SHALL shadow that built-in. Workflow declarations and capture sinks MUST reject the reserved scoped variable names so their canonical values cannot be shadowed. Prevalidation MUST evaluate scoped built-in availability against the effective scope of the referencing step.

#### Scenario: Param shadows built-in
- **WHEN** a workflow declares `params: [step_id]` and a caller passes `step_id: custom-value`
- **THEN** `{{step_id}}` in that step resolves to `custom-value`, not the actual step ID

#### Scenario: Captured variable shadows built-in
- **WHEN** a prior step captures output into a variable named `session_dir`
- **THEN** `{{session_dir}}` in subsequent steps resolves to the captured value, not the session directory path

#### Scenario: Param uses reserved scoped name
- **WHEN** a workflow declares a parameter named `workspace_dir`, `repository_dir`, `repository_name`, or `repository_output_dir`
- **THEN** workflow validation fails with a reserved-name error

#### Scenario: Capture uses reserved scoped name
- **WHEN** a step declares a capture sink named `workspace_dir`, `repository_dir`, `repository_name`, or `repository_output_dir`
- **THEN** workflow validation fails with a reserved-name error

#### Scenario: Repository built-in referenced from workspace scope
- **WHEN** a workspace-scoped step references `repository_name`, `repository_dir`, or `repository_output_dir`
- **THEN** prevalidation fails before execution rather than treating the name as globally available

#### Scenario: Repository built-in referenced from repository override
- **WHEN** a repository-scoped step inside a workspace workflow references a repository-only built-in
- **THEN** prevalidation accepts the reference against that step's effective repository scope

### Requirement: workspace_dir built-in variable
Agent Runner SHALL expose `{{workspace_dir}}` as a reserved built-in variable. For a scope-aware run it MUST be the canonical absolute root from which the coordination Git worktree was launched. For a legacy unscoped run it MUST retain that run's existing launch-workspace value, including a non-Git launch directory. Its value MUST remain unchanged throughout the run, including during execution in repositories outside the workspace directory.

#### Scenario: workspace_dir in workspace execution
- **WHEN** a workspace-scoped step references `{{workspace_dir}}`
- **THEN** Agent Runner resolves it to the canonical coordination workspace path

#### Scenario: workspace_dir in sibling repository execution
- **WHEN** a repository-scoped step executes in a repository outside the workspace directory and references `{{workspace_dir}}`
- **THEN** Agent Runner resolves it to the original coordination workspace rather than the active repository

#### Scenario: workspace_dir in legacy workflow
- **WHEN** an unscoped legacy workflow references `{{workspace_dir}}`
- **THEN** Agent Runner resolves it to that run's existing launch workspace

### Requirement: repository_dir built-in variable
Agent Runner SHALL expose `{{repository_dir}}` as a reserved built-in variable whose value is the canonical absolute root of the active repository. The variable MUST be available only while a repository context is active.

#### Scenario: repository_dir in repository execution
- **WHEN** a repository-scoped step for `backend` references `{{repository_dir}}`
- **THEN** Agent Runner resolves it to backend's canonical repository root even when the step's effective workdir is a subdirectory

#### Scenario: repository_dir changes between repositories
- **WHEN** repository execution advances from backend to frontend
- **THEN** `{{repository_dir}}` resolves to the root of the repository currently active in each execution

#### Scenario: repository_dir in workspace execution
- **WHEN** a workspace-scoped step with no active repository references `{{repository_dir}}`
- **THEN** interpolation fails because the variable is unavailable rather than resolving to an empty value

### Requirement: repository_name built-in variable
Agent Runner SHALL expose `{{repository_name}}` as a reserved built-in variable whose value is the stable configured name of the active repository. An implicit single repository MUST use `default` as its repository name. The variable MUST be available only while a repository context is active.

#### Scenario: Configured repository name
- **WHEN** a repository-scoped step executes for configured repository `backend`
- **THEN** `{{repository_name}}` resolves to `backend`

#### Scenario: Implicit repository name
- **WHEN** a repository-scoped step executes in a traditional project with one implicit repository
- **THEN** `{{repository_name}}` resolves to `default`

#### Scenario: repository_name in workspace execution
- **WHEN** a workspace-scoped step with no active repository references `{{repository_name}}`
- **THEN** interpolation fails because the variable is unavailable rather than resolving to an empty value

### Requirement: repository_output_dir built-in variable
Agent Runner SHALL expose `{{repository_output_dir}}` as a reserved built-in variable while a repository context is active. For an explicitly configured repository it MUST identify a canonical repository-specific directory beneath the run's workspace-owned output directory. For the transparent implicit `default` repository it MUST resolve to the existing run output directory so legacy output paths remain unchanged.

#### Scenario: Explicit repository evidence directory
- **WHEN** a repository-scoped step for backend references `{{repository_output_dir}}`
- **THEN** Agent Runner resolves it to backend's dedicated directory beneath `{{session_dir}}/output/repositories/`

#### Scenario: Evidence directories differ between repositories
- **WHEN** backend and frontend each write `acceptance-assumptions.md` beneath `{{repository_output_dir}}`
- **THEN** the files have distinct paths and neither repository overwrites the other's evidence

#### Scenario: Implicit repository output compatibility
- **WHEN** a scope-aware workflow uses the implicit `default` repository
- **THEN** `{{repository_output_dir}}` resolves to `{{session_dir}}/output` without adding a `repositories/default` path segment

#### Scenario: repository_output_dir in workspace execution
- **WHEN** a workspace-scoped step with no active repository references `{{repository_output_dir}}`
- **THEN** interpolation fails because the variable is unavailable

### Requirement: Scoped built-in variables do not impose isolation
The scoped path and identity built-ins MUST expose execution context without adding filesystem-access restrictions.

#### Scenario: Cross-repository path use
- **WHEN** a step uses a known path outside `{{repository_dir}}` that its ordinary permissions allow
- **THEN** Agent Runner does not reject the access based on `repository_dir` or `repository_name`

