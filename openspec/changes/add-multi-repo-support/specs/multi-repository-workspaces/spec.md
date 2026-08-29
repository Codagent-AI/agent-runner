## ADDED Requirements

### Requirement: Git-backed workspace repository declarations
An Agent Runner scope-aware workspace MUST be launched from the canonical root of a Git worktree. Its project configuration SHALL allow uniquely named independent Git repository roots using paths relative to that workspace. Repository paths MAY resolve inside or outside the workspace after canonicalization, and nested repositories MAY be ignored by the workspace repository. An explicit configured repository MUST NOT resolve to the workspace root itself.

#### Scenario: Git workspace declares sibling repositories
- **WHEN** a Git-backed workspace declares `backend` at `../backend` and `frontend` at `../frontend`
- **THEN** Agent Runner accepts both repositories when the paths resolve to distinct Git worktree roots

#### Scenario: Workspace declares an ignored nested repository
- **WHEN** a workspace declares repository `api` at ignored directory `services/api`
- **THEN** Agent Runner resolves the path relative to the workspace and accepts `services/api` when it is an independent Git worktree root

#### Scenario: Workspace is not Git-backed
- **WHEN** a scope-aware workflow starts from a directory that does not resolve to a Git worktree root
- **THEN** Agent Runner rejects the run before definition or planning begins

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

#### Scenario: Implicit repository presentation
- **WHEN** repository-scoped work executes for an implicit single repository
- **THEN** Agent Runner presents the existing workflow shape without an additional repository container

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

#### Scenario: Nested repository-scoped workflow
- **WHEN** a repository-scoped workflow invokes another repository-scoped workflow while a repository is active
- **THEN** the child executes once for the active repository rather than starting another fan-out

#### Scenario: Repository target capture is reserved
- **WHEN** a step declares `capture: repositories` or `outcome_capture: repositories`
- **THEN** workflow validation rejects the capture so the explicit target parameter cannot be shadowed

### Requirement: Planned task groups
A multi-repository change plan MUST contain an ordered list of task groups. Each task group MUST target exactly one configured repository, MUST contain all tasks assigned to that repository, and a repository MUST NOT appear in more than one task group for the change. One Runner-owned Go parser MUST be authoritative for validating the index, deriving ordered ownership, producing task patterns, and comparing task-group identity on resume. It MUST support grouped configured-workspace plans, flat implicit full-change plans, and monolithic implicit simple-change plans.

#### Scenario: Valid task groups
- **WHEN** an approved plan contains a `backend` task group followed by a `frontend` task group
- **THEN** the affected repository order is `backend`, then `frontend`, and tasks retain their order within each task group

#### Scenario: Task without repository ownership
- **WHEN** an implementation task does not belong to a task group targeting exactly one configured repository
- **THEN** plan validation fails before repository execution begins

#### Scenario: Unknown task-group repository
- **WHEN** a task group targets a repository not declared by the workspace
- **THEN** plan validation fails before repository execution begins

#### Scenario: Repeated repository task group
- **WHEN** more than one task group targets the same repository
- **THEN** plan validation fails rather than permitting interleaved repository execution

#### Scenario: Simple-change task groups
- **WHEN** a multi-repository `simple-change` plan is approved
- **THEN** it creates one small linked task file beneath `tasks/<repository>/` for each ordered repository group

#### Scenario: Implicit simple-change task shape
- **WHEN** `simple-change` runs in a project with no configured repositories
- **THEN** the parser treats the existing monolithic `tasks.md` as the one implicit repository task and preserves that planning shape

#### Scenario: Repository parameter derived from plan
- **WHEN** a top-level change advances from approved planning to repository execution
- **THEN** a top-level workspace step uses the authoritative parser to capture `affected_repositories`, persists the selection, and explicitly passes that value as `repositories` to repository-scoped children

#### Scenario: Workspace-owned task paths cross repository scope
- **WHEN** the parser returns task files for a repository-scoped implementation loop
- **THEN** it returns canonical absolute task paths so their resolution does not depend on the active repository working directory

#### Scenario: Repository task resolver supplies loop pattern
- **WHEN** a selected repository enters the built-in implementation group
- **THEN** a repository-local resolver asks the authoritative parser for that repository's canonical task pattern and the loop uses the captured pattern rather than hardcoding a directory shape

#### Scenario: Existing planning validator delegates task checks
- **WHEN** built-in plan validation checks task ownership, links, or filesystem shape
- **THEN** `validate-planning-artifacts.sh` delegates those checks to the authoritative parser instead of independently enforcing only `tasks/*.md`

### Requirement: Contiguous repository task-group execution
The built-in implementation lifecycle MUST place all repository-owned work in one repository-scoped group so it completes one selected repository before beginning the next. Separate repository-scoped leaf steps remain independent fan-out boundaries and MAY revisit repositories in a later step.

#### Scenario: Two ordered task groups
- **WHEN** `repositories` is `backend, frontend` and both repositories have assigned tasks
- **THEN** Agent Runner completes every backend task and repository validation before starting any frontend task

#### Scenario: No cross-repository interleaving
- **WHEN** a built-in change contains tasks for multiple selected repositories
- **THEN** Agent Runner does not alternate execution between those repositories

#### Scenario: Consecutive repository leaf steps
- **WHEN** a workspace workflow declares two separate repository-scoped leaf steps
- **THEN** each leaf performs its own ordered fan-out, so a repository completed by the first leaf may be revisited by the second

### Requirement: Scoped execution roots without isolation
Workspace scope and repository scope MUST select execution context and default working-directory roots without adding filesystem-access restrictions.

#### Scenario: Workspace-scoped execution
- **WHEN** a workspace-scoped workflow executes
- **THEN** it runs once with the coordination workspace as its default working-directory root

#### Scenario: Repository-scoped execution
- **WHEN** a repository-scoped workflow executes for a selected repository
- **THEN** it uses that repository as its default working-directory root and resolves step workdirs relative to that repository

#### Scenario: Cross-repository project instruction
- **WHEN** an agent working in repository `b` follows project instructions that access a service or files in repository `a`
- **THEN** Agent Runner does not block that access based solely on repository scope

### Requirement: Workspace-owned orchestration state
A multi-repository workflow MUST remain one workspace-owned run while recording repository identity for repository-scoped activity.

#### Scenario: Shared run identity
- **WHEN** a workflow executes across multiple repositories
- **THEN** its run identity, state, audit history, and resume identity remain associated with the coordination workspace rather than becoming separate repository runs

#### Scenario: Workspace artifact resolution
- **WHEN** a scoped step resolves configured artifact globs or shared planning artifacts
- **THEN** Agent Runner uses canonical absolute workspace-owned paths regardless of the active repository

#### Scenario: Repository-aware records
- **WHEN** repository-scoped activity records progress, evidence, metrics, or an outcome
- **THEN** the workspace run record identifies the active repository for that activity

### Requirement: Independent workspace repository lifecycle
The Git-backed workspace MUST retain its own branch, working-tree, Agent Validator, commit, archive, and pull-request lifecycle independently from configured implementation repositories.

#### Scenario: Workspace versions shared OpenSpec artifacts
- **WHEN** an OpenSpec change defines, plans, or archives shared artifacts
- **THEN** those changes are committed in the workspace repository rather than copied into an implementation repository

#### Scenario: Workspace pull request finalization
- **WHEN** the OpenSpec workflow reaches finalization after changing the workspace repository
- **THEN** Agent Runner finalizes one workspace pull request in addition to the pull request for each affected implementation repository

#### Scenario: Spec-driven artifacts do not change workspace repository
- **WHEN** a spec-driven simple change keeps its planning artifacts external and produces no tracked workspace change
- **THEN** it finalizes only the affected implementation repositories and does not create an empty workspace pull request

### Requirement: Sequential fail-fast repository execution
Agent Runner MUST execute selected repositories sequentially in `repositories` order and MUST stop the fan-out at the first repository failure.

#### Scenario: Repository execution order
- **WHEN** `repositories` names `backend` before `frontend`
- **THEN** Agent Runner begins backend execution before frontend execution

#### Scenario: Repository failure
- **WHEN** backend execution fails before frontend has started
- **THEN** Agent Runner stops the run without starting frontend and does not roll back any repository that completed earlier

#### Scenario: Resume after repository failure
- **WHEN** the user resumes a run that failed during backend execution
- **THEN** Agent Runner uses the persisted repository order, skips completed repositories, resumes backend from its persisted progress, and leaves frontend pending until backend completes

### Requirement: Unselected repositories remain untouched
Agent Runner MUST perform repository-scoped work only for repositories explicitly named by `repositories`.

#### Scenario: Configured but unselected repository
- **WHEN** repository `docs` is configured but `repositories` contains only `backend` and `frontend`
- **THEN** Agent Runner performs no branch validation, implementation, Validator, Git, or pull-request-finalization work in `docs`

#### Scenario: Declaration does not imply selection
- **WHEN** a repository appears in workspace configuration but not in `repositories`
- **THEN** Agent Runner does not treat the repository as affected

### Requirement: Selected repository checkout locking
Before any repository-scoped work begins, Agent Runner MUST acquire advisory process locks for every selected canonical repository root in deterministic canonical-path order. Lock identity MUST be global to the current Agent Runner user and keyed by canonical repository root rather than workspace, repository name, or run directory. Acquisition MUST be atomic. Lock metadata MUST identify the owning run and process for diagnostics, and a lock whose recorded process is no longer live MUST be recoverable. Agent Runner MUST hold acquired locks until the run process ends and MUST fail before repository mutation when another live run owns any selected lock.

#### Scenario: Two runs select the same checkout
- **WHEN** a second live run selects backend while another run holds backend's repository lock
- **THEN** the second run fails before preflight or mutation with an error identifying the locked repository and owning run or process

#### Scenario: Runs select disjoint checkouts
- **WHEN** two runs select no common canonical repository roots
- **THEN** their repository lock acquisition does not block either run

#### Scenario: Multiple locks avoid deadlock
- **WHEN** concurrent runs request overlapping repositories in different task-group orders
- **THEN** Agent Runner acquires locks in canonical-path order so the runs cannot deadlock

#### Scenario: Process termination releases locks
- **WHEN** a run exits, fails, or is killed
- **THEN** its process-lifetime repository locks are released so a later run can acquire them

#### Scenario: Different workspaces select the same checkout
- **WHEN** two coordination workspaces configure different repository names that canonicalize to the same Git worktree root
- **THEN** both resolve the same lock identity and only one live run may acquire it

#### Scenario: Stale lock owner
- **WHEN** lock metadata names a process that is no longer live
- **THEN** a later run recovers the stale lock and records its own run and process identity
