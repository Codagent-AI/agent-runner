# Task: Establish Scope and Workspace Repository Contracts

## Goal

Add the validated model and preparation-time contracts that distinguish legacy, workspace, and repository execution. A scope-aware run must resolve one canonical Git-backed coordination workspace, validate project-local named repository declarations, preserve a transparent implicit repository for traditional projects, and expose only the built-ins valid for the effective scope.

## Background

You MUST read:

- `openspec/changes/add-multi-repo-support/design.md`, especially **Workspace and Repository Configuration**, **Scope Model and Validation**, **Execution Context and Workdir Resolution**, and **Migration and Compatibility**.
- `openspec/changes/add-multi-repo-support/specs/step-model/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/multi-repository-workspaces/spec.md`, especially repository declarations, implicit compatibility, and explicit targets.
- `openspec/changes/add-multi-repo-support/specs/builtin-vars/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/sub-workflows/spec.md`, especially static and runtime composition rules.

Relevant implementation seams:

- Add the string-backed scope type and fields without collapsing omitted scope into `workspace` in `internal/model/step.go`; update validation and adjacent tests in `internal/model/step_test.go`.
- Add model-only workspace/repository identity types and execution-context propagation in `internal/model/context.go`; model types must not depend on engine or executor packages.
- Extend the project-only portion of `.agent-runner/config.yaml` in `internal/config/config.go`. Global configuration may continue to supply profiles but must reject repository declarations. Preserve declaration identity independently of path and do not use map iteration as execution order.
- Centralize canonical Git worktree-root validation and root-only scope-aware launch preparation around `internal/runner/runner.go`, `internal/discovery/`, and `cmd/agent-runner/`. Reuse Git-aware discovery where practical; a directory merely inside a worktree is not a valid declared root.
- Update parameter collection in `internal/paramform/`, interactive launch wiring in `internal/interactive/`, and CLI launch wiring in `cmd/agent-runner/` so the internally satisfied implicit `repositories` control parameter is neither rendered as required user input nor rejected as blank in a traditional project.
- Extend static composition validation in `internal/loader/composition.go` and runtime validation in `internal/exec/subworkflow.go`. A child workflow's declared scope is authoritative; a sub-workflow step cannot override it.
- Extend built-in interpolation and effective-scope prevalidation in `internal/model/context.go`, `internal/prevalidate/`, and `internal/textfmt/`. Reserve `workspace_dir`, `repository_name`, `repository_dir`, `repository_output_dir`, and the `repositories` control capture name as specified.
- Update user-facing schema/setup guidance in `docs/writing-workflows.md`, `docs/setup.md`, and `docs/cli-reference.md` for repository declarations, scope, root-only launches, and explicit targets.

Keep legacy workflows truly legacy: an external workflow that omits scope must still execute once with its previous launch/project-root behavior, including from non-Git directories. Do not introduce repository execution or locking in this task; this task establishes the independently testable contract consumed by dispatch.

## Spec

The following approved requirements and scenarios are binding.

### Requirement: Git-backed workspace repository declarations

An Agent Runner scope-aware workspace MUST be launched from the canonical root of a Git worktree. Its project configuration SHALL allow uniquely named independent Git repository roots using paths relative to that workspace. Repository paths MAY resolve inside or outside the workspace after canonicalization, and nested repositories MAY be ignored by the workspace repository. An explicit configured repository MUST NOT resolve to the workspace root itself.

#### Scenario: Git workspace declares sibling repositories
- **WHEN** a Git-backed workspace declares `backend` at `../backend` and `frontend` at `../frontend`
- **THEN** Agent Runner accepts both repositories when the paths resolve to distinct Git worktree roots

#### Scenario: Workspace declares an ignored nested repository
- **WHEN** a workspace declares repository `api` at ignored directory `services/api`
- **THEN** Agent Runner resolves the path relative to the workspace and accepts `services/api` when it is an independent Git worktree root

#### Scenario: Scope-aware launch starts below workspace root
- **WHEN** a scope-aware workflow is launched from a subdirectory of a Git worktree
- **THEN** Agent Runner rejects the run and directs the user to launch from the canonical workspace root so run storage and resume identity remain stable

#### Scenario: Invalid repository declarations
- **WHEN** repository declarations contain a duplicate name, duplicate canonical root, missing path, or path that is not a Git worktree root
- **THEN** Agent Runner rejects the workspace configuration before workflow execution begins

#### Scenario: Configured repository equals workspace
- **WHEN** a repository declaration resolves to the coordination workspace Git root
- **THEN** Agent Runner rejects it to prevent duplicate workspace and repository lifecycle execution on the same branch

#### Scenario: Invalid repository name
- **WHEN** a repository name is `default`, is longer than 63 characters, or does not match `[a-z0-9][a-z0-9._-]*`
- **THEN** Agent Runner rejects the workspace configuration before workflow execution begins

### Requirement: Implicit single-repository compatibility

Agent Runner MUST preserve existing single-repository behavior when project configuration does not declare repositories.

#### Scenario: Git project without repository declarations
- **WHEN** a workflow starts from a Git repository whose project configuration does not declare repositories
- **THEN** Agent Runner treats the project as one implicit repository without requiring repository configuration

#### Scenario: Implicit repository remains transparent
- **WHEN** scope-aware execution uses the implicit `default` repository
- **THEN** Agent Runner retains legacy audit prefixes and output filenames and does not expose a repository container in user-facing presentation

### Requirement: Explicit repository targets

Every repository-scoped workflow invocation in a configured workspace MUST receive a non-empty ordered `repositories` parameter naming its execution targets. Agent Runner MUST NOT implicitly substitute every configured repository. When no repositories are configured, Agent Runner MUST internally supply the transparent implicit repository without requiring the user to name `default`.

#### Scenario: Parent supplies affected repositories
- **WHEN** a workspace workflow invokes a repository-scoped child after determining the affected repositories
- **THEN** the parent supplies the ordered `repositories` value to the child

#### Scenario: Standalone repository-scoped launch
- **WHEN** a user launches a repository-scoped workflow directly in a configured workspace
- **THEN** the user must provide the ordered `repositories` value explicitly

#### Scenario: Standalone launch in traditional project
- **WHEN** a user launches a repository-scoped workflow directly in a project with no repository declarations
- **THEN** Agent Runner supplies the implicit repository internally and does not require or expose the name `default`

#### Scenario: Missing repository targets
- **WHEN** a repository-scoped workflow is invoked without `repositories` or with an empty value
- **THEN** Agent Runner rejects the invocation before any repository execution begins

#### Scenario: Invalid repository targets
- **WHEN** `repositories` contains an unknown or duplicate repository name
- **THEN** Agent Runner rejects the invocation before any repository execution begins

#### Scenario: Repository target capture is reserved
- **WHEN** a step declares `capture: repositories` or `outcome_capture: repositories`
- **THEN** workflow validation rejects the capture so the explicit target parameter cannot be shadowed

### Requirement: Workflow scope declaration

A scope-aware workflow MUST declare exactly one default `scope` value of `workspace` or `repositories`. A workflow that omits `scope` MUST retain legacy execution behavior.

#### Scenario: Invalid workflow scope
- **WHEN** a workflow declares a scope other than the string `workspace` or `repositories`
- **THEN** workflow validation fails with an invalid-scope error

#### Scenario: Legacy workflow without scope
- **WHEN** a workflow and all of its steps omit `scope`
- **THEN** Agent Runner preserves the workflow's execution behavior from before scope support

#### Scenario: Scoped step without workflow default
- **WHEN** a workflow omits `scope` but one of its steps declares `scope`
- **THEN** workflow validation fails and requires the workflow to declare its default scope

### Requirement: Step scope override

A non-sub-workflow step in a workspace-scoped workflow MAY override its execution scope. The `scope` field MUST accept the same `workspace` and `repositories` values for agent, shell, script, UI, group, and loop steps. A sub-workflow step MUST NOT declare `scope` because the referenced workflow's declared scope is authoritative.

#### Scenario: Repository-scoped step override
- **WHEN** a step in a `scope: workspace` workflow declares `scope: repositories`
- **THEN** workflow validation accepts the repository override

#### Scenario: Scope on each supported step type
- **WHEN** an agent, shell, script, UI, group, or loop step declares a permitted scope in a workspace-scoped workflow
- **THEN** workflow validation accepts the scope field for that step type

#### Scenario: Scope on sub-workflow step
- **WHEN** a sub-workflow step declares `scope`
- **THEN** workflow validation fails and directs the author to declare the intended default on the referenced workflow

### Requirement: Repository workflows remain repository-scoped

A workflow whose default scope is `repositories` MUST NOT contain a step that overrides scope to `workspace`.

#### Scenario: Workspace override inside repository workflow
- **WHEN** a step in a `scope: repositories` workflow declares `scope: workspace`
- **THEN** workflow validation fails and requires the workspace work to move into a workspace-scoped parent

### Requirement: Repository workflow target parameter

A workflow whose default scope is `repositories` MUST declare a required ordered `repositories` parameter. Configured workspaces MUST supply it explicitly. In a project with no repository declarations, Agent Runner MUST satisfy the parameter internally with the transparent implicit repository so existing standalone entry points remain compatible.

#### Scenario: Repository workflow omits targets
- **WHEN** a `scope: repositories` workflow omits the `repositories` parameter or declares it as optional
- **THEN** workflow validation fails before the workflow can be invoked

#### Scenario: Parent invokes repository workflow
- **WHEN** a parent invokes a valid `scope: repositories` child workflow
- **THEN** the child invocation requires the parent to supply `repositories`

### Requirement: Child workflow scope is authoritative

A sub-workflow invocation MUST use the referenced child workflow's declared scope. The parent workflow's default scope MUST NOT replace the child's declared scope, and a sub-workflow step MUST NOT override it.

#### Scenario: Parent default does not replace child default
- **WHEN** a parent and child declare different scopes
- **THEN** the child uses its own declared scope rather than inheriting the parent's default

### Requirement: Valid scoped workflow composition

A workspace-scoped workflow MAY invoke workspace-scoped, repository-scoped, or legacy unscoped children. A repository-scoped workflow MAY invoke repository-scoped or legacy unscoped children but MUST NOT invoke a workspace-scoped child.

#### Scenario: Repository parent invokes workspace child
- **WHEN** a repository-scoped parent references a workspace-scoped child
- **THEN** workflow composition validation fails and requires the workspace child to be invoked from a workspace-scoped parent

#### Scenario: Scoped parent invokes legacy child
- **WHEN** a scoped parent invokes an unscoped legacy child
- **THEN** the child retains legacy behavior and executes once in the parent's effective execution context

### Requirement: workspace_dir built-in variable

Agent Runner SHALL expose `{{workspace_dir}}` as a reserved built-in variable. For a scope-aware run it MUST be the canonical absolute root from which the coordination Git worktree was launched. For a legacy unscoped run it MUST retain that run's existing launch-workspace value, including a non-Git launch directory. Its value MUST remain unchanged throughout the run, including during execution in repositories outside the workspace directory.

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

### Requirement: Built-in precedence

Built-in variables have the **lowest** interpolation precedence except for the reserved `workspace_dir`, `repository_dir`, `repository_name`, and `repository_output_dir` variables. A workflow `params` entry or a captured variable with the same name as a non-reserved built-in SHALL shadow that built-in. Workflow declarations and capture sinks MUST reject the reserved scoped variable names so their canonical values cannot be shadowed. Prevalidation MUST evaluate scoped built-in availability against the effective scope of the referencing step.

#### Scenario: Param uses reserved scoped name
- **WHEN** a workflow declares a parameter named `workspace_dir`, `repository_dir`, `repository_name`, or `repository_output_dir`
- **THEN** workflow validation fails with a reserved-name error

#### Scenario: Repository built-in referenced from workspace scope
- **WHEN** a workspace-scoped step references `repository_name`, `repository_dir`, or `repository_output_dir`
- **THEN** prevalidation fails before execution rather than treating the name as globally available

#### Scenario: Repository built-in referenced from repository override
- **WHEN** a repository-scoped step inside a workspace workflow references a repository-only built-in
- **THEN** prevalidation accepts the reference against that step's effective repository scope

## Done When

- Scope omission is preserved through YAML decoding, defaults, validation, JSON/state-facing model representations, and loader composition; it is never silently converted to `workspace`.
- Project config accepts valid ignored nested and sibling Git worktree roots and rejects global repository declarations, invalid/reserved names, missing/non-root paths, duplicate canonical roots, and roots equal to the workspace.
- Scope-aware launches require the canonical coordination Git root before definition/planning, while legacy unscoped launch and run-bucket behavior remains unchanged.
- Config-free Git projects receive an internal `default` repository identity without requiring it in user-supplied parameters or exposing an extra audit prefix, output path segment, or presentation container; `{{repository_name}}` still resolves to `default` when an active implicit repository references it.
- Repository workflows require a required string `repositories` parameter; configured invocations cannot omit it, and capture/outcome-capture sinks cannot shadow it.
- Launching a repository-scoped workflow in a project with no repository declarations neither prompts for `repositories` nor blocks submission on it; focused `internal/paramform`, interactive launch, and CLI tests prove the internal implicit target is injected before required-parameter enforcement without displaying `default`.
- Static and dynamically resolved child compositions enforce the approved scope matrix and explicit parameter threading.
- `workspace_dir` is immutable and always available, including with the legacy launch value for unscoped non-Git runs; active repository contexts bind canonical `repository_dir`, configured or implicit `repository_name`, and `repository_output_dir`; unavailable repository built-ins fail interpolation rather than resolving empty; all four names are reserved against params and captures.
- Explicit repository evidence directories use `{{session_dir}}/output/repositories/<repository_name>`; the implicit repository uses `{{session_dir}}/output` with no `repositories/default` segment.
- Focused model, config, loader, prevalidation, root-discovery, and CLI tests cover every scenario above, including symlink canonicalization and legacy non-Git workflows.
- Go changes are formatted with `make fmt`; focused tests pass before handing off.
