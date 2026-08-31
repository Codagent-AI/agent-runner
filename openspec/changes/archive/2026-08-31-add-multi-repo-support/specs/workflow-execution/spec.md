## ADDED Requirements

### Requirement: Workspace-scoped dispatch
Agent Runner MUST execute workspace-scoped work exactly once from the coordination workspace.

#### Scenario: Workspace workflow with multiple selected repositories
- **WHEN** a `scope: workspace` workflow executes while `repositories` names multiple repositories
- **THEN** Agent Runner executes the workflow once rather than once per repository

#### Scenario: Step uses workspace default
- **WHEN** an unscoped step executes in a `scope: workspace` workflow
- **THEN** Agent Runner executes the step once in workspace scope

#### Scenario: Workspace-relative workdir
- **WHEN** a workspace-scoped step declares a relative `workdir`
- **THEN** Agent Runner resolves the workdir from the coordination workspace

#### Scenario: Workspace activity placement
- **WHEN** a workspace-scoped step records execution activity
- **THEN** Agent Runner records it outside every repository execution container

### Requirement: Repository-scoped workflow dispatch
Agent Runner MUST execute an entire `scope: repositories` workflow once for each selected repository, sequentially in `repositories` order.

#### Scenario: Two-repository workflow execution
- **WHEN** a repository-scoped workflow receives `repositories` ordered as `backend, frontend`
- **THEN** Agent Runner completes the entire workflow for backend before starting the workflow for frontend

#### Scenario: Repository default working directory
- **WHEN** a repository-scoped workflow begins execution for a selected repository
- **THEN** Agent Runner uses that repository root as the default working directory

#### Scenario: Repository-relative workdir
- **WHEN** a repository-scoped step declares a relative `workdir`
- **THEN** Agent Runner resolves the workdir from the active repository root

#### Scenario: Persisted repository order on resume
- **WHEN** a repository-scoped workflow is resumed
- **THEN** Agent Runner uses the same persisted repository selection and order as the original run

#### Scenario: No workflow-level interleaving
- **WHEN** a repository-scoped workflow executes for multiple repositories
- **THEN** Agent Runner does not execute a step from a later repository before the current repository's workflow has finished

### Requirement: Repository-scoped step dispatch
Agent Runner MUST execute a `scope: repositories` step in a workspace-scoped workflow once for each selected repository, sequentially in `repositories` order.

#### Scenario: Leaf step repository dispatch
- **WHEN** an agent, shell, script, or UI step overrides scope to `repositories`
- **THEN** Agent Runner executes the complete step once per selected repository in order

#### Scenario: Group or loop repository dispatch
- **WHEN** a group or loop step overrides scope to `repositories`
- **THEN** Agent Runner executes the complete nested body for the active repository before beginning the body for the next repository

#### Scenario: Unscoped nested step inherits repository container
- **WHEN** an unscoped step executes inside a repository-scoped group or loop in a workspace-scoped workflow
- **THEN** Agent Runner executes it in the active repository context rather than reverting to the workflow's workspace default

#### Scenario: Active repository context per execution
- **WHEN** a repository-scoped step executes for a selected repository
- **THEN** the step and its nested execution receive that repository's active context

#### Scenario: Separate leaf fan-outs may revisit repositories
- **WHEN** a workspace workflow contains consecutive repository-scoped leaf steps A and B for backend and frontend
- **THEN** Agent Runner executes A for backend and frontend, then independently executes B for backend and frontend

### Requirement: Scoped step control semantics
For a repository-scoped step, Agent Runner MUST evaluate `skip_if`, execute capture sinks, and record `outcome_capture` within each active repository context. It MUST apply `break_if` and top-level `continue_on_failure` to the aggregate outcome returned by the completed fan-out. Repository-local captures and outcomes MUST NOT become inputs to a sibling repository.

#### Scenario: Repository-specific skip condition
- **WHEN** a repository-scoped step's `skip_if` is true for backend and false for frontend
- **THEN** Agent Runner records backend's execution as skipped and executes the step for frontend

#### Scenario: Capture during leaf fan-out
- **WHEN** backend and frontend executions of the same leaf step capture `result`
- **THEN** each value is stored only in its repository context

#### Scenario: Break condition observes aggregate outcome
- **WHEN** a repository-scoped step inside a loop returns its fan-out result to the enclosing loop
- **THEN** the enclosing loop evaluates that step's `break_if` once against the aggregate result

#### Scenario: Continue on failure observes aggregate outcome
- **WHEN** backend fails a repository-scoped step with `continue_on_failure: true`
- **THEN** later repositories in that fan-out remain pending and the containing workflow applies continue-on-failure only after the failed aggregate returns

### Requirement: Active repository prevents nested fan-out
Repository-scoped execution entered with an active repository MUST execute once for that repository rather than dispatching the full repository list again.

#### Scenario: Repository workflow invokes repository workflow
- **WHEN** a repository-scoped workflow invokes another repository-scoped workflow while repository `backend` is active
- **THEN** the child workflow executes once for backend

#### Scenario: Repository container has repository child
- **WHEN** a repository-scoped group or loop contains a repository-scoped child while a repository is active
- **THEN** the child executes once in the active repository context

#### Scenario: Repository list remains available
- **WHEN** nested repository-scoped execution runs for an active repository
- **THEN** the complete ordered `repositories` value remains available as run context without causing nested repetition

### Requirement: Repository execution does not add isolation
Scope MUST select execution context and default working directory without adding filesystem-access restrictions.

#### Scenario: Repository execution starts in selected repository
- **WHEN** a command or agent executes in repository scope
- **THEN** it starts from the selected repository's effective working directory

#### Scenario: Access outside active repository
- **WHEN** repository-scoped work accesses another path permitted by its CLI, operating-system permissions, and project instructions
- **THEN** Agent Runner does not reject the access based solely on repository scope

### Requirement: Repository failure is fail-fast
A repository execution failure MUST stop the current repository fan-out before another repository starts.

#### Scenario: First selected repository fails
- **WHEN** backend execution fails before frontend execution begins
- **THEN** Agent Runner leaves frontend pending and does not roll back repositories completed earlier

#### Scenario: Resume failed repository execution
- **WHEN** the user resumes after backend execution failed
- **THEN** Agent Runner resumes backend from its persisted progress before allowing frontend to begin

#### Scenario: Continue-on-failure on scoped aggregate
- **WHEN** a repository-scoped step fails and declares `continue_on_failure: true`
- **THEN** Agent Runner stops the remaining repository fan-out and applies existing continue-on-failure behavior to the failed aggregate step without silently executing the remaining repositories

### Requirement: Legacy execution compatibility
An external workflow that omits `scope` MUST retain its execution behavior from before scope support and MUST NOT fan out merely because repository configuration exists.

#### Scenario: Existing project workflow remains unscoped
- **WHEN** a project-local workflow created before scope support omits `scope`
- **THEN** Agent Runner executes it once from its existing run base

#### Scenario: Repository configuration does not change legacy workflow
- **WHEN** an unscoped user or project workflow runs from a workspace that declares multiple repositories
- **THEN** Agent Runner does not fan out that workflow

#### Scenario: Implicit single repository
- **WHEN** a scope-aware workflow executes in a traditional project with one implicit repository
- **THEN** it executes once with existing working-directory and presentation behavior
