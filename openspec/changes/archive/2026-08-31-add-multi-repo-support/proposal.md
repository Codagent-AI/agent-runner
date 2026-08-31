## Why

Agent Runner models one Git repository as both the orchestration project and execution target, even though individual steps can use custom working directories. It has no first-class model for one Git-backed orchestration workspace coordinating work across several other independent repositories. Repository ownership, task routing, Validator configuration, Git state, pull requests, and resume identity are therefore ambiguous or run-global.

Projects such as a Git-backed coordination repository `foo/` containing ignored independent repositories `a/`, `b/`, and `c/` need one workflow run from `foo/`. The shared OpenSpec change, workspace commits and pull request, and run records belong to that workspace, while each implementation task, Validator run, commit, pull request, and CI cycle belongs to exactly one affected repository. The existing `change` and `simple-change` workflows must support this without multiplying workflow variants.

This is worth building with caveats. It fits Agent Runner's existing workflow composition and nested run-view model, but requires an explicit separation between workspace orchestration and repository execution. Cross-repository atomicity and parallelism should not be part of the first slice.

## What Changes

- Allow project-level Agent Runner configuration at a Git-backed workspace root to declare stable names and workspace-relative paths for independent repositories, including ignored nested repositories and sibling repositories.
- Add `scope: workspace | repositories` to workflow and step definitions. Orchestration workflows declare their own default so they behave correctly both as children and when invoked directly; a step may override that default. Reusable context-neutral building blocks intentionally omit scope and execute once in the caller's effective context, preserving their existing standalone behavior. Other definitions without `scope` retain current behavior.
- Run workspace-scoped work once from the coordination root. Run repository-scoped work sequentially once for each repository affected by the change, with an active repository context and repository-relative step workdirs.
- Require multi-repository planning output to assign each implementation task or task group to exactly one configured repository. Resolve and persist the ordered affected-repository set after planning, then filter repository-scoped execution so each repository receives only its own tasks.
- Keep the OpenSpec root, shared planning artifacts, configured artifact-glob resolution, `state.json`, `audit.log`, run identity, and resume identity at the workspace. Canonicalize workspace-owned artifact paths before repository fan-out so repository-scoped steps can read shared plans without changing the working directory used by repository tools.
- Preserve the workspace repository's existing branch, Validator, commit, archive, and pull-request lifecycle independently from the affected implementation repositories.
- Run agents, shell commands, bundled scripts, Agent Validator, Git operations, and called agents from the active repository for repository-scoped execution. Scope selects the working context; it does not sandbox filesystem access.
- Associate output captures, audit events, evidence directories, progress, metrics, and pull-request links with the repository that produced them wherever execution is repository-scoped. Named sessions reuse an already-instantiated parent binding; when no parent binding exists, each repository creates and retains its own binding. Resume skips completed repositories and continues the unfinished repository without replaying its siblings.
- Make the existing `change` and `simple-change` workflows adaptive rather than adding multi-repository variants. Shared planning, interactive review, aggregate acceptance, and OpenSpec lifecycle work remain workspace-scoped; implementation, repository checks, Validator, commits, and pull-request finalization run for each affected repository.
- Keep aggregate acceptance conversation and evidence review in workspace scope, but route every requested implementation fix through a repository-scoped remediation group that validates, commits, pushes, and re-verifies the owning draft pull request before workspace reacceptance.
- Move affected-repository preflight checks, including feature-branch validation, after planning has identified the target repositories and before repository implementation begins.
- Finalize the workspace repository and each affected implementation repository independently when they contain workflow changes, including push, pull-request creation or update, CI/review handling, and final verification. A failure in one repository does not roll back completed work in another.
- Add one named repository container level beneath repository-scoped workflows in run views. Labels use the configured repository name directly. An implicit single-repository run is visually flattened so existing runs do not gain needless nesting.
- Prove the first slice with a workflow launched from a Git-backed coordination repository that plans and versions shared artifacts there, modifies an ignored nested or sibling repository, runs that repository's Validator configuration, retains orchestration state at the workspace, and finalizes independent workspace and implementation pull requests.
- Prevent concurrent runs from mutating the same configured checkout by acquiring process-lifetime advisory locks for every selected repository before repository work begins.

## Capabilities

### New Capabilities

- `multi-repository-workspaces`: Declares workspace repositories, defines workspace and repository execution scopes, routes task groups to their owning repositories, and separates orchestration storage from repository execution.

### Modified Capabilities

- `step-model`: Represents workflow defaults, step overrides, and validated scope values.
- `workflow-execution`: Dispatches workspace-once and deterministic affected-repository executions while preserving legacy behavior.
- `sub-workflows`: Propagates workspace and active-repository context without overriding a child workflow's independently valid default scope.
- `builtin-vars`: Exposes canonical workspace and active-repository identity and paths to scoped steps.
- `recursive-state`: Persists affected repositories and per-repository nested progress for deterministic resume.
- `named-sessions`: Defines inherited workspace bindings and repository-local first-use bindings.
- `audit-log-entries`: Records workspace and repository identity on scoped events.
- `output-capture`: Keeps repository-scoped captures distinct while retaining them in the workspace run record.
- `run-pull-request-link`: Stores and presents one pull-request link per affected repository instead of a single last-written link.
- `agent-calls`: Uses the active repository as the default working context for repository-scoped calls without adding scope-based access restrictions.
- `view-run`: Displays collapsible named repository containers, repository-aware breadcrumbs, and repository details.
- `live-run-view`: Tracks and focuses the active repository branch during a live run.
- `run-complete-screen`: Aggregates repository metrics and supports drilling into repository results.
- `run-metrics-artifact`: Adds explicit repository identity to externally consumed execution records.
- `builtin-workflows`: Makes the OpenSpec and spec-driven `change` and `simple-change` lifecycles scope-aware without adding workflow variants.

## Technical Approach

Introduce a run-level workspace context containing the canonical coordination Git root and a validated, ordered map of configured repository names to canonical repository roots. Repository paths resolve relative to the workspace and must use unique lowercase slug identifiers suitable for persisted state, paths, and audit identity; `default` is reserved for the implicit repository. Normalize a traditional single-repository launch into the same model with one transparent implicit repository.

Add scope dispatch above ordinary workflow execution. `workspace` executes once at the coordination root. `repositories` creates deterministic sequential repository containers for the persisted affected set. A repository-scoped workflow entered with an active repository executes only for that repository, preventing nested fan-out. Workflow-level defaults are declared explicitly in shared child workflows so invoking those workflows directly has the same semantics as invoking them through `change` or `simple-change`.

Planning emits machine-readable repository ownership. Explicitly configured workspaces group task files beneath `tasks/<repository>/`; implicit full changes retain flat `tasks/*.md`, and implicit simple changes retain their monolithic `tasks.md`. One Runner-owned Go parser is authoritative for all three modes. After approval, a top-level workspace step captures the ordered set as `affected_repositories`, persists it, and explicitly passes it through the required `repositories` parameter. A repository-local resolver then supplies the parser-produced canonical task pattern to the existing loop. This avoids hidden parameter inheritance, conflicting validators, and running every task once in every repository.

Repository-scoped execution receives the active repository name, root, repository-relative workdir base, canonical workspace references, and a repository-specific evidence directory. Captures, pull-request links, evidence, and recursive execution progress use the repository identity as part of their key. A named session inherited from workspace scope remains shared; a name first instantiated in repository scope is local to that repository. Repository scope changes the default working context but does not prevent agents or commands from accessing other paths permitted by their tools and operating-system permissions.

Model fan-out as a named container boundary in state, audit, and the run tree, reusing existing nested-workflow navigation and metric rollups. Multi-repository views add a node labelled only with the configured repository name, while implicit single-repository views flatten that node.

Update shared workflow building blocks rather than cloning top-level workflows. Context-neutral blocks such as branch validation, Validator, task implementation, and PR finalization intentionally run once in the caller's effective context. The workspace-scoped `implement-change` orchestration keeps its existing shape, with one repository-scoped task-group container around repository preflight, implementation, validation, and draft-PR preparation. The complete body runs for one repository before the next begins. Planning, aggregate review, OpenSpec archive, and workspace PR finalization run once; affected-repository finalization then runs independently for each selected repository. Repository-scoped leaf steps remain supported and form their own fan-out boundaries, so workflow authors use a group when several steps must remain contiguous per repository.

## Out of Scope

- Atomic cross-repository transactions, rollback, or all-or-nothing delivery.
- Coordinated or atomic commits across repositories.
- A single pull request spanning multiple repositories; each repository owns an independent pull request.
- Parallel repository execution in the initial implementation.
- Automatic repository cloning, fetching, branch creation, worktree creation, dependency management, or workspace provisioning.
- Inferring task ownership from changed files or agent behavior; multi-repository tasks declare their repository explicitly.
- Managing OpenSpec stores, worksets, or workspace registries. Agent Runner's workspace model remains compatible with those features but does not own their configuration.

## Impact

- **Configuration and discovery:** project configuration gains named repository declarations, path and identifier validation, canonicalization, and single-repository normalization.
- **Model and loader:** workflow and step definitions gain scope metadata; planning artifacts gain machine-readable task ownership.
- **Runner and executors:** dispatch, sub-workflow context, path resolution, calls, captures, evidence, branch preflight, metrics, and resume become repository-aware while retaining inherited named-session bindings and workspace orchestration.
- **Persistence and audit:** state and events gain additive repository identity and per-repository progress, results, and pull-request links, with compatibility for earlier state.
- **Terminal UI:** run trees, breadcrumbs, selected-step details, live focus, completion summaries, and metric drill-down gain a named repository container for multi-repository execution.
- **Built-in workflows:** shared OpenSpec and spec-driven planning, implementation, validation, acceptance, archive, and PR-finalization workflows receive appropriate scope declarations and repository-aware task filtering; no duplicate workflow family is introduced.
- **Documentation and tests:** workflow schema, Git-backed workspace setup, task authoring, run/resume behavior, single-repository regressions, multi-repository end-to-end fixtures, repository-specific Validator execution, and workspace plus per-repository PR finalization require coverage.
