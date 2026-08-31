# Task: Execute Repository-Scoped Fan-Out Safely

## Goal

Implement the first-class repository dispatcher that executes complete workflow or step bodies sequentially in explicit target order, supplies the correct active execution root, suppresses nested fan-out, applies control semantics at the right boundary, and locks every selected checkout before mutation.

## Background

You MUST read:

- `openspec/changes/add-multi-repo-support/design.md`, especially **Execution Context and Workdir Resolution**, **Fan-Out Execution and Failure Semantics**, **Captures and Sessions** (context construction only), and **Decisions and Alternatives**.
- `openspec/changes/add-multi-repo-support/specs/workflow-execution/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/multi-repository-workspaces/spec.md`, especially explicit targets, contiguous execution, scoped roots, fail-fast behavior, unselected repositories, and checkout locking.
- `openspec/changes/add-multi-repo-support/specs/sub-workflows/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/agent-calls/spec.md`.
- `openspec/changes/add-multi-repo-support/test-plan.md` for `INT-001`.

Relevant implementation seams:

- Put one scope dispatcher around ordinary workflow and non-sub-workflow step execution in `internal/runner/` and `internal/exec/`; do not duplicate fan-out logic in each leaf executor and do not lower repositories into the generic loop executor.
- Construct active repository contexts in `internal/model/context.go` so workspace identity, run directory, audit logger, engine, profile store, control, hooks, and visible ancestor named-session bindings are retained while `ProjectRoot` and default `WorkingDir` become the repository root.
- Centralize effective-root and step-workdir resolution used by agent, shell, script, UI, group, loop, glob, and sub-workflow execution. Preserve existing absolute-workdir behavior and keep configured artifact globs workspace-owned.
- Update `internal/exec/agent_call.go` and related invocation helpers so called-agent starting directories are bounded by the active workspace/repository after symlink resolution, without treating scope as a filesystem sandbox.
- Update `docs/agent-calls.md` for workspace/repository starting-directory boundaries, sibling repositories outside the workspace, symlink escape rejection, and the fact that the boundary constrains only process start rather than later filesystem access.
- Add a repository checkout lock service beside `internal/runlock/` (or a focused new internal package) using the existing user-level Agent Runner state location. Key locks by a collision-resistant digest of canonical repository root, acquire all targets atomically/in deterministic canonical-path order, include root/run/PID diagnostics, recover stale process owners, and hold locks for process lifetime.
- Preserve aggregate `skip_if`, `break_if`, `capture`, `outcome_capture`, and `continue_on_failure` behavior exactly as designed. A repository failure stops remaining siblings even when the aggregate step has `continue_on_failure`.

This task must leave repository execution observable enough for later persistence and run-view work, but it must not invent a separate run per repository or a synthetic workflow variant.

## Spec

The following approved requirements and scenarios are binding.

### Requirement: Workspace-scoped dispatch

Agent Runner MUST execute workspace-scoped work exactly once from the coordination workspace.

#### Scenario: Workspace workflow with multiple selected repositories
- **WHEN** a `scope: workspace` workflow executes while `repositories` names multiple repositories
- **THEN** Agent Runner executes the workflow once rather than once per repository

#### Scenario: Workspace-relative workdir
- **WHEN** a workspace-scoped step declares a relative `workdir`
- **THEN** Agent Runner resolves the workdir from the coordination workspace

### Requirement: Repository-scoped workflow dispatch

Agent Runner MUST execute an entire `scope: repositories` workflow once for each selected repository, sequentially in `repositories` order.

#### Scenario: Two-repository workflow execution
- **WHEN** a repository-scoped workflow receives `repositories` ordered as `backend, frontend`
- **THEN** Agent Runner completes the entire workflow for backend before starting the workflow for frontend

#### Scenario: Repository-relative workdir
- **WHEN** a repository-scoped step declares a relative `workdir`
- **THEN** Agent Runner resolves the workdir from the active repository root

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

#### Scenario: Separate leaf fan-outs may revisit repositories
- **WHEN** a workspace workflow contains consecutive repository-scoped leaf steps A and B for backend and frontend
- **THEN** Agent Runner executes A for backend and frontend, then independently executes B for backend and frontend

### Requirement: Scoped step control semantics

For a repository-scoped step, Agent Runner MUST evaluate `skip_if`, execute capture sinks, and record `outcome_capture` within each active repository context. It MUST apply `break_if` and top-level `continue_on_failure` to the aggregate outcome returned by the completed fan-out. Repository-local captures and outcomes MUST NOT become inputs to a sibling repository.

#### Scenario: Continue on failure observes aggregate outcome
- **WHEN** backend fails a repository-scoped step with `continue_on_failure: true`
- **THEN** later repositories in that fan-out remain pending and the containing workflow applies continue-on-failure only after the failed aggregate returns

### Requirement: Active repository prevents nested fan-out

Repository-scoped execution entered with an active repository MUST execute once for that repository rather than dispatching the full repository list again.

#### Scenario: Repository workflow invokes repository workflow
- **WHEN** a repository-scoped workflow invokes another repository-scoped workflow while repository `backend` is active
- **THEN** the child workflow executes once for backend

#### Scenario: Repository list remains available
- **WHEN** nested repository-scoped execution runs for an active repository
- **THEN** the complete ordered `repositories` value remains available as run context without causing nested repetition

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

### Requirement: Sequential fail-fast repository execution

Agent Runner MUST execute selected repositories sequentially in `repositories` order and MUST stop the fan-out at the first repository failure.

#### Scenario: Repository failure
- **WHEN** backend execution fails before frontend has started
- **THEN** Agent Runner stops the run without starting frontend and does not roll back any repository that completed earlier

### Requirement: Repository failure is fail-fast

A repository execution failure MUST stop the current repository fan-out before another repository starts.

#### Scenario: First selected repository fails
- **WHEN** backend execution fails before frontend execution begins
- **THEN** Agent Runner leaves frontend pending and does not roll back repositories completed earlier

#### Scenario: Continue-on-failure on scoped aggregate
- **WHEN** a repository-scoped step fails and declares `continue_on_failure: true`
- **THEN** Agent Runner stops the remaining repository fan-out and applies existing continue-on-failure behavior to the failed aggregate step without silently executing the remaining repositories

### Requirement: Unselected repositories remain untouched

Agent Runner MUST perform repository-scoped work only for repositories explicitly named by `repositories`.

#### Scenario: Configured but unselected repository
- **WHEN** repository `docs` is configured but `repositories` contains only `backend` and `frontend`
- **THEN** Agent Runner performs no branch validation, implementation, Validator, Git, or pull-request-finalization work in `docs`

### Requirement: Repository execution does not add isolation

Scope MUST select execution context and default working directory without adding filesystem-access restrictions.

#### Scenario: Access outside active repository
- **WHEN** repository-scoped work accesses another path permitted by its CLI, operating-system permissions, and project instructions
- **THEN** Agent Runner does not reject the access based solely on repository scope

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

### Requirement: Working-directory behavior

Agent Runner SHALL establish a starting-directory boundary for a called child from the parent's active execution scope. In a scope-aware workspace, the boundary SHALL be the canonical coordination Git worktree root and the workflow MUST have been launched from that root. In repository scope, the boundary SHALL be the canonical active repository directory, including when that configured repository is outside the workspace tree. For a legacy unscoped run whose launch directory is not inside a Git worktree, the existing launch-directory boundary SHALL remain unchanged. Loading a workflow file from a different repository MUST NOT change the boundary.

When `workdir` is omitted, the child SHALL use the parent's effective working directory. A relative `workdir` SHALL resolve from the parent's effective working directory. Every resolved starting `workdir`, including a path reached through a symbolic link, MUST remain inside the active scope boundary.

The starting-directory boundary SHALL validate only where the child process starts. It MUST NOT sandbox the called child or prevent the child, after launch, from accessing other paths permitted by its CLI, operating-system permissions, and project instructions.

#### Scenario: Workdir cannot escape the active scope
- **WHEN** a call supplies a path or symbolic link that resolves outside the active workspace or repository boundary
- **THEN** Agent Runner rejects the starting workdir without spawning the child

#### Scenario: External workflow file does not move the boundary
- **WHEN** Agent Runner loads a workflow file from a location outside the active execution scope
- **THEN** called children continue to use the active workspace or repository as their starting-directory boundary

### Requirement: Selected repository checkout locking

Before any repository-scoped work begins, Agent Runner MUST acquire advisory process locks for every selected canonical repository root in deterministic canonical-path order. Lock identity MUST be global to the current Agent Runner user and keyed by canonical repository root rather than workspace, repository name, or run directory. Acquisition MUST be atomic. Lock metadata MUST identify the owning run and process for diagnostics, and a lock whose recorded process is no longer live MUST be recoverable. Agent Runner MUST hold acquired locks until the run process ends and MUST fail before repository mutation when another live run owns any selected lock.

#### Scenario: Two runs select the same checkout
- **WHEN** a second live run selects backend while another run holds backend's repository lock
- **THEN** the second run fails before preflight or mutation with an error identifying the locked repository and owning run or process

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

## Test Plan

- `INT-001`: Add integration coverage spanning project config canonicalization, workflow validation, execution-context construction, scope dispatch, executor cwd, sub-workflow composition, called-agent workdir boundaries, unselected repositories, global checkout locking, implicit targets, and legacy unscoped behavior. Use real temporary Git worktrees plus recording process/call adapters. Run `go test ./internal/config ./internal/runner ./internal/exec` and ensure the normal `make test` CI phase covers it.

## Done When

- Workspace work executes once; repository leaf, group, loop, and whole-workflow bodies fan out sequentially in requested order; an active repository prevents nested repetition.
- All selected names are parsed and validated before work starts. Unknown, duplicate, or empty entries fail without mutating any repository; no configured-but-unselected repository receives work.
- Default and relative process workdirs resolve from the effective workspace/repository root across every executor; generic relative loop globs use that root; absolute paths retain existing semantics; workspace-owned artifacts remain anchored to the workspace.
- Called-agent start directories obey the active canonical boundary after symlink resolution while ordinary post-launch filesystem access remains unrestricted by scope.
- `docs/agent-calls.md` accurately describes effective-scope starting directories and no longer characterizes every call as restricted to the original single worktree.
- Sub-workflows inherit canonical workspace and active-repository context, but never ordinary params implicitly: a configured repository child receives `repositories` only through an explicit sub-workflow `params` entry, while the implicit compatibility path is injected internally.
- Per-repository `skip_if`, capture, and outcome capture execute inside the active context; `break_if` and `continue_on_failure` see one aggregate result; failure leaves later repositories pending.
- Repository locks are globally root-keyed, atomically acquired in sorted canonical-path order, diagnostic, stale-owner recoverable, and retained for process lifetime on success, failure, and termination paths.
- Repository configuration alone never changes the execution count or run base of an unscoped legacy workflow.
- `INT-001` passes with real nested/sibling Git worktrees, overlapping/disjoint runs, stale locks, nested scoped workflows, all supported step kinds, and legacy/implicit controls.
- Go changes are formatted with `make fmt`; targeted tests pass before handing off.
