## ADDED Requirements

### Requirement: Built-in orchestration workflows declare execution scope
Every orchestration workflow shipped with Agent Runner MUST explicitly declare its default execution scope. Reusable context-neutral building blocks that must run once in either workspace or active-repository context MUST intentionally omit scope and retain existing single-execution behavior. Adding scope declarations MUST NOT create separate single-repository and multi-repository workflow families.

#### Scenario: Shipped orchestration workflow has explicit scope
- **WHEN** Agent Runner loads an embedded top-level or orchestration workflow
- **THEN** the workflow declares either `scope: workspace` or `scope: repositories`

#### Scenario: Context-neutral building block follows caller context
- **WHEN** `run-validator`, `validate-feature-branch`, `implement-task`, or `finalize-pr` is invoked beneath an active repository
- **THEN** its intentionally unscoped workflow body executes once in that active repository without starting another fan-out

#### Scenario: Context-neutral building block runs standalone
- **WHEN** the same intentionally unscoped building block is invoked directly in a traditional project
- **THEN** it preserves its existing single execution from the current project context without requiring `repositories`

#### Scenario: Existing change workflow serves both project shapes
- **WHEN** a user selects a built-in `change` or `simple-change` workflow
- **THEN** the same workflow definition supports traditional single-repository projects and configured multi-repository workspaces

#### Scenario: No multi-repository workflow variants
- **WHEN** multi-repository support is enabled
- **THEN** Agent Runner does not add multi-repository copies of the OpenSpec or spec-driven workflow families

### Requirement: Planning and definition remain workspace-scoped
Built-in change definition, planning, shared artifact validation, and OpenSpec lifecycle work MUST execute once in workspace scope. Planning MUST produce an ordered set of task groups, each owned by exactly one configured repository. After planning, a top-level workspace resolver MUST capture first appearance in task-group order as `affected_repositories` and explicitly pass it through the required `repositories` parameter.

#### Scenario: OpenSpec planning from the workspace repository
- **WHEN** an OpenSpec change is planned from a Git-backed workspace containing configured repositories
- **THEN** planning runs once from the workspace and writes and versions the shared OpenSpec artifacts there

#### Scenario: Spec-driven planning from a workspace
- **WHEN** a spec-driven change is planned for multiple repositories
- **THEN** its shared definition and planning artifacts remain workspace-owned while its task groups identify their owning repositories

#### Scenario: Ordered affected repositories
- **WHEN** the approved plan lists backend task groups before frontend task groups
- **THEN** the top-level resolver captures `affected_repositories` as backend then frontend and repository-scoped children receive it explicitly as `repositories`

#### Scenario: Repository ownership is mandatory
- **WHEN** a multi-repository plan contains a task group without a configured repository owner
- **THEN** built-in plan validation fails before repository implementation begins

#### Scenario: OpenSpec change path is workspace-anchored
- **WHEN** a built-in OpenSpec workflow passes its change directory across a repository boundary
- **THEN** it expresses the path as `{{workspace_dir}}/openspec/changes/<change-name>` and planning validation accepts that canonical absolute identity

#### Scenario: Spec-driven change path is resolved explicitly
- **WHEN** a spec-driven workflow receives a relative external change directory
- **THEN** a workspace resolver canonicalizes it against `workspace_dir` before any repository-scoped child receives it

### Requirement: Repository preflight follows planning
Built-in workflows MUST preserve existing workspace branch, working-tree, and Agent Validator checks for workspace-owned changes. They MUST separately perform Git branch, working-tree, and Agent Validator preflight checks in each repository context after planning identifies the affected repositories. Workspace planning and definition MUST NOT depend on an implementation repository's Git state or Validator configuration.

#### Scenario: Workspace preflight remains workspace-local
- **WHEN** Agent Runner starts a change workflow from the Git-backed workspace
- **THEN** existing workspace branch and Validator checks run against the workspace without inspecting configured implementation repositories

#### Scenario: Selected repository is checked
- **WHEN** repository implementation begins for backend
- **THEN** branch, working-tree, and Validator checks execute from backend and use backend's configuration

#### Scenario: Repository preflight fails fast
- **WHEN** backend fails a repository preflight check
- **THEN** backend implementation does not begin and later selected repositories remain pending

### Requirement: Implementation uses one repository-scoped middle section
The built-in `implement-change` orchestration MUST be workspace-scoped. After its workspace preflight, it MUST execute one repository-scoped task-group container that completes all repository-owned implementation, review, validation, Git, and draft-pull-request work for one repository before starting the next. It MUST then return to workspace scope for shared task-index completion and aggregate acceptance preparation.

#### Scenario: Workspace preflight runs once
- **WHEN** `implement-change` starts for backend and frontend
- **THEN** shared change-name and planning-artifact validation execute once before either repository task group begins

#### Scenario: Repository lifecycle remains contiguous
- **WHEN** backend is the first affected repository
- **THEN** Agent Runner implements backend's tasks, reviews backend's assumptions, simplifies and validates backend, verifies its Git state, and prepares its draft pull request before starting frontend

#### Scenario: Repository receives only its tasks
- **WHEN** backend is active in the repository-scoped middle section
- **THEN** a repository-local resolver supplies the authoritative parser's canonical backend task pattern and the implementation loop receives only backend-owned tasks

#### Scenario: Shared task index completes after repositories
- **WHEN** every selected repository finishes successfully
- **THEN** Agent Runner updates and verifies the shared task index once from workspace scope

#### Scenario: Aggregate acceptance runs once
- **WHEN** all repository pull requests and the shared task index are ready
- **THEN** acceptance preparation runs once from workspace scope against the repository evidence index and complete change

### Requirement: Acceptance remediation returns to repository scope
Workspace-scoped acceptance steps MUST review evidence, resolve user decisions, update workspace-owned specifications when appropriate, and record approved remediation without directly implementing, validating, committing, or pushing implementation-repository fixes. Approved remediation MUST be persisted beneath the workspace run output as a structured ledger containing a workspace-owned section and ordered entries keyed only by selected repository names. When remediation is required, the workflow MUST execute a repository-scoped remediation group across the selected repositories. Each repository execution MUST receive only its active repository's ledger entry, apply only those approved changes, run that repository's Validator, commit and push that repository, and verify its draft pull request head before workspace-scoped reacceptance continues.

#### Scenario: Acceptance finds a backend defect
- **WHEN** aggregate acceptance records an approved implementation fix owned by backend
- **THEN** the remediation group applies, validates, commits, pushes, and verifies the fix from backend while frontend performs no unrelated change

#### Scenario: Remediation ledger names an unselected repository
- **WHEN** the workspace remediation ledger contains an implementation entry for a repository outside the persisted selected set
- **THEN** Agent Runner rejects the ledger before repository remediation begins

#### Scenario: Active repository has no remediation entry
- **WHEN** frontend enters the remediation group and the approved ledger has no frontend entry
- **THEN** frontend performs no implementation change, validation, commit, push, or pull-request refresh for that pass

#### Scenario: Acceptance changes a shared specification
- **WHEN** the user approves a normative specification clarification during workspace acceptance
- **THEN** the workspace step updates and validates the workspace-owned artifact while repository remediation implements any corresponding code changes in the owning repositories

#### Scenario: Repository PR head is refreshed after remediation
- **WHEN** repository remediation commits a fix after the draft PR was initially verified
- **THEN** the workflow pushes the commit and verifies that repository's draft pull request head equals its new local `HEAD` before returning to workspace reacceptance

#### Scenario: No remediation required
- **WHEN** workspace acceptance records no implementation-repository changes
- **THEN** the remediation group completes without creating empty commits and workspace reacceptance may rely on the existing repository evidence

### Requirement: Repository-scoped agents receive explicit repository context
Every repository-scoped built-in agent invocation MUST identify the active repository name and canonical directory, identify the canonical workspace directory, and operate from the active repository's effective working directory. A named session already instantiated in an ancestor workspace context MUST be reused by repository descendants. A named session first instantiated inside repository fan-out MUST be local to that repository and MUST NOT become visible to sibling repositories.

#### Scenario: Workspace lead agent moves between repositories
- **WHEN** `lead-agent` is instantiated during workspace planning and later invoked for backend and frontend
- **THEN** each repository invocation explicitly identifies its active repository while reusing the inherited workspace conversation

#### Scenario: Lead agent is first used in repository scope
- **WHEN** a standalone repository-scoped workflow first invokes `lead-agent` for backend and later executes for frontend
- **THEN** backend and frontend instantiate distinct repository-local named sessions

#### Scenario: Repository invocation has matching working directory
- **WHEN** a built-in agent prompt identifies frontend as the active repository
- **THEN** the agent invocation's effective working directory is frontend's repository directory

#### Scenario: Workspace references remain available
- **WHEN** a repository-scoped agent needs shared planning artifacts
- **THEN** its invocation identifies the workspace directory from which those artifacts can be resolved

### Requirement: Simple changes use the same scoped lifecycle
The built-in `simple-change` workflows MUST use the same workspace planning, repository-owned task-group execution, per-repository validation and pull-request finalization, workspace review, and repository remediation model as full changes, while retaining their simpler planning artifacts. This change intentionally adds PR finalization to the existing single-repository simple lifecycle.

#### Scenario: Multi-repository simple change
- **WHEN** a simple change affects backend and frontend
- **THEN** its plan assigns its task groups to those repositories and its implementation processes each repository contiguously in planned order

#### Scenario: Single-repository simple change
- **WHEN** a simple change runs in a traditional single-repository project
- **THEN** it retains the monolithic `tasks.md` input and existing visible hierarchy without an added repository container, then finalizes that repository's pull request

### Requirement: Workspace and repository pull requests are independent
Built-in OpenSpec workflows MUST preserve workspace commits and finalization for shared planning and archive changes while finalizing each affected implementation repository independently. Built-in spec-driven workflows MUST omit workspace finalization when no tracked workspace change exists.

#### Scenario: Workspace commits shared artifacts
- **WHEN** a workspace commit or archive step changes shared OpenSpec artifacts
- **THEN** it retains its existing workspace-repository commit behavior

#### Scenario: OpenSpec finalization includes workspace
- **WHEN** an OpenSpec multi-repository change reaches finalization
- **THEN** it finalizes the workspace pull request and one pull request for each affected implementation repository

#### Scenario: Simple change has no workspace changes
- **WHEN** a spec-driven simple change keeps external planning artifacts outside the workspace repository and leaves the workspace clean
- **THEN** finalization runs for each affected implementation repository without creating a workspace pull request
