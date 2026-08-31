# Task: Route Plans Through Adaptive Built-In Workflows

## Goal

Complete the user-facing multi-repository lifecycle without adding workflow variants. Consume the authoritative task-group parser to thread ordered affected repositories and canonical task paths through the existing OpenSpec and spec-driven workflow families, keep repository work contiguous, isolate and route acceptance remediation, and finalize workspace and implementation pull requests independently.

## Background

You MUST read:

- `openspec/changes/add-multi-repo-support/design.md`, especially **Repository Selection and Task Groups**, **Built-In Workflow Topology**, **Decisions and Alternatives**, and **Migration and Compatibility**.
- `openspec/changes/add-multi-repo-support/specs/multi-repository-workspaces/spec.md`, especially planned task groups and independent workspace lifecycle.
- `openspec/changes/add-multi-repo-support/specs/builtin-workflows/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/run-pull-request-link/spec.md`, especially built-in URL recording.
- `openspec/changes/add-multi-repo-support/specs/output-capture/spec.md`, especially the evidence index consumed by aggregate acceptance.
- `openspec/changes/add-multi-repo-support/test-plan.md` for `INT-002` and `E2E-001`.

Relevant implementation seams:

- Add `workflows/core/resolve-task-group.sh` as a thin wrapper over the pinned `agent-runner internal task-groups` command. It uses `--output repositories` for workspace selection and `--repository "{{repository_name}}" --output task-pattern` for active-repository loop input. `workflows/core/validate-planning-artifacts.sh` delegates ownership/link/shape validation to the same authority; neither script parses headings or reconstructs task paths itself.
- Update the critical topology in `workflows/core/accept-change-v1.0.yaml`, `workflows/core/complete-simple-change-v1.0.yaml`, `workflows/core/implement-change-v1.0.yaml`, and `workflows/core/plan-change-v1.0.yaml`; the v2 entry/wrapper files `workflows/openspec/{change,implement-change,plan-change,simple-change}-v2.0.yaml` and `workflows/spec-driven/{change,implement-change,plan-change,simple-change}-v2.0.yaml`; and their referenced archive, commit, validation, remediation, and finalization children.
- Exhaustively classify every shipped workflow YAML under `workflows/core/`, `workflows/openspec/`, `workflows/spec-driven/`, and `workflows/onboarding/` (excluding `_group.yaml` metadata). The closed intentionally-unscoped allowlist is `workflows/core/run-validator-v1.0.yaml`, `workflows/core/validate-feature-branch-v1.0.yaml`, `workflows/core/implement-task-v1.0.yaml`, `workflows/core/finalize-pr-v1.0.yaml`, and the new context-neutral `workflows/core/remediate-repository-v1.0.yaml`. Every other shipped workflow YAML MUST explicitly declare `scope: workspace` or `scope: repositories`; hidden status, version, or onboarding/scaffold/intake/debug lineage is not an exception.
- Keep `core/implement-change-v1.0.yaml` workspace-scoped around one repository-scoped group containing repository preflight, the active repository's numeric task loop, review/simplification, Validator/Git verification, and draft PR preparation. The full group finishes for one repository before another starts.
- Anchor OpenSpec change paths to `{{workspace_dir}}`; canonicalize spec-driven external change directories against the workspace before fan-out. Pass parser-produced absolute task paths into repository execution.
- In `workflows/core/implement-change-v1.0.yaml`, change repository review report globs, `acceptance-assumptions.md`, verification commands, and every directly named repository report to read/write beneath `{{repository_output_dir}}`. In `workflows/core/accept-change-v1.0.yaml`, make aggregate acceptance read repository evidence only through `repository-evidence-index.json` in persisted order and keep aggregate handoffs under `{{session_dir}}/output`. Persist `acceptance-remediation.json` with a separate workspace section and selected-repository-keyed entries; validate keys before fan-out and give each repository only its own entry. Missing entries are true no-ops.
- Preserve OpenSpec workspace commits/archive/finalization, then finalize each selected implementation repository independently. Spec-driven/simple flows must not create an empty workspace PR. This change intentionally adds implementation-repository PR finalization to the traditional simple-change lifecycle.
- Record draft/final PR URLs through `capture: pr_url`. Keep `verify-draft-pr` strict; make standalone finalize URL lookup best-effort when `gh` is unavailable/unauthenticated/no matching PR.
- Update `docs/built-in-workflows.md`, `docs/writing-workflows.md`, `docs/setup.md`, `docs/quickstart.md`, and `docs/cli-reference.md` with grouped task syntax, implicit compatibility, evidence/remediation ownership, and independent PR behavior.
- Do not execute `AT-001`, `AT-002`, or `HT-001`. They remain required later acceptance/human obligations, and the implementation must leave the shipped surfaces they describe available.

## Spec

The following approved requirements and scenarios are binding.

### Requirement: Planned task groups

This task consumes the parser contract for workflow interpolation and topology; it MUST NOT reimplement parsing. The shared scenarios below bind how shipped workflows use the parser-produced selection and task inputs.

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

### Requirement: Workspace-owned orchestration state

A multi-repository workflow MUST remain one workspace-owned run while recording repository identity for repository-scoped activity.

#### Scenario: Workspace artifact resolution
- **WHEN** a scoped step resolves configured artifact globs or shared planning artifacts
- **THEN** Agent Runner uses canonical absolute workspace-owned paths regardless of the active repository

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

### Requirement: Built-in orchestration workflows declare execution scope

Every orchestration workflow shipped with Agent Runner MUST explicitly declare its default execution scope. Reusable context-neutral building blocks that must run once in either workspace or active-repository context MUST intentionally omit scope and retain existing single-execution behavior. Adding scope declarations MUST NOT create separate single-repository and multi-repository workflow families.

#### Scenario: Context-neutral building block follows caller context
- **WHEN** `run-validator`, `validate-feature-branch`, `implement-task`, or `finalize-pr` is invoked beneath an active repository
- **THEN** its intentionally unscoped workflow body executes once in that active repository without starting another fan-out

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

#### Scenario: OpenSpec change path is workspace-anchored
- **WHEN** a built-in OpenSpec workflow passes its change directory across a repository boundary
- **THEN** it expresses the path as `{{workspace_dir}}/openspec/changes/<change-name>` and planning validation accepts that canonical absolute identity

#### Scenario: Spec-driven change path is resolved explicitly
- **WHEN** a spec-driven workflow receives a relative external change directory
- **THEN** a workspace resolver canonicalizes it against `workspace_dir` before any repository-scoped child receives it

### Requirement: Repository preflight follows planning

Built-in workflows MUST preserve existing workspace branch, working-tree, and Agent Validator checks for workspace-owned changes. They MUST separately perform Git branch, working-tree, and Agent Validator preflight checks in each repository context after planning identifies the affected repositories. Workspace planning and definition MUST NOT depend on an implementation repository's Git state or Validator configuration.

#### Scenario: Selected repository is checked
- **WHEN** repository implementation begins for backend
- **THEN** branch, working-tree, and Validator checks execute from backend and use backend's configuration

#### Scenario: Repository preflight fails fast
- **WHEN** backend fails a repository preflight check
- **THEN** backend implementation does not begin and later selected repositories remain pending

### Requirement: Implementation uses one repository-scoped middle section

The built-in `implement-change` orchestration MUST be workspace-scoped. After its workspace preflight, it MUST execute one repository-scoped task-group container that completes all repository-owned implementation, review, validation, Git, and draft-pull-request work for one repository before starting the next. It MUST then return to workspace scope for shared task-index completion and aggregate acceptance preparation.

#### Scenario: Repository lifecycle remains contiguous
- **WHEN** backend is the first affected repository
- **THEN** Agent Runner implements backend's tasks, reviews backend's assumptions, simplifies and validates backend, verifies its Git state, and prepares its draft pull request before starting frontend

#### Scenario: Repository receives only its tasks
- **WHEN** backend is active in the repository-scoped middle section
- **THEN** a repository-local resolver supplies the authoritative parser's canonical backend task pattern and the implementation loop receives only backend-owned tasks

#### Scenario: Aggregate acceptance runs once
- **WHEN** all repository pull requests and the shared task index are ready
- **THEN** acceptance preparation runs once from workspace scope against the repository evidence index and complete change

### Requirement: Repository-written evidence is path-isolated

This delivery unit owns the shipped workflow paths and globs that consume Runner's repository output directory and evidence index.

Agent Runner MUST persist automatic process output for explicit repository-scoped steps beneath `{{repository_output_dir}}`. Built-in repository-scoped workflows MUST write durable reports, handoffs, screenshots, and other directly named evidence beneath the same directory. Agent Runner MUST maintain an ordered workspace-readable `{{session_dir}}/output/repository-evidence-index.json` mapping each selected repository to its evidence directory; the implicit repository entry MUST point to the legacy output directory. Workspace-scoped aggregation MUST read repository evidence through that index and MUST write aggregate evidence only to the workspace run output directory.

#### Scenario: Same evidence filename in two repositories
- **WHEN** backend and frontend each write `acceptance-assumptions.md`
- **THEN** each writes beneath its own `{{repository_output_dir}}` and neither file overwrites the other

#### Scenario: Repository review reads only its own reports
- **WHEN** backend assumption review reads session reports during backend execution
- **THEN** its evidence glob is confined to backend's repository output directory and excludes frontend reports

#### Scenario: Workspace acceptance aggregates repository evidence
- **WHEN** aggregate acceptance begins after repository implementation completes
- **THEN** it reads `repository-evidence-index.json`, processes every selected repository's evidence directory in persisted order, and writes its aggregate handoff beneath `{{session_dir}}/output`

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

#### Scenario: Repository PR head is refreshed after remediation
- **WHEN** repository remediation commits a fix after the draft PR was initially verified
- **THEN** the workflow pushes the commit and verifies that repository's draft pull request head equals its new local `HEAD` before returning to workspace reacceptance

### Requirement: Repository-scoped agents receive explicit repository context

Every repository-scoped built-in agent invocation MUST identify the active repository name and canonical directory, identify the canonical workspace directory, and operate from the active repository's effective working directory. A named session already instantiated in an ancestor workspace context MUST be reused by repository descendants. A named session first instantiated inside repository fan-out MUST be local to that repository and MUST NOT become visible to sibling repositories.

#### Scenario: Workspace lead agent moves between repositories
- **WHEN** `lead-agent` is instantiated during workspace planning and later invoked for backend and frontend
- **THEN** each repository invocation explicitly identifies its active repository while reusing the inherited workspace conversation

### Requirement: Simple changes use the same scoped lifecycle

The built-in `simple-change` workflows MUST use the same workspace planning, repository-owned task-group execution, per-repository validation and pull-request finalization, workspace review, and repository remediation model as full changes, while retaining their simpler planning artifacts. This change intentionally adds PR finalization to the existing single-repository simple lifecycle.

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

### Requirement: Built-in pull-request workflows record their URL

The built-in workflows that open or update a pull request SHALL record its URL through the reserved `pr_url` capture, so runs of every shipped change lineage show the link for the finalized workspace repository and each finalized implementation repository.

`core/implement-change-v1.0.yaml` SHALL record the URL from its existing draft-PR verification step once per active repository. Adding repository association SHALL NOT weaken its existing validation: it SHALL continue to fail the repository execution unless exactly one open, draft pull request exists for the current branch whose head matches local `HEAD`. Recording occurs only once those checks have passed.

`core/finalize-pr-v1.0.yaml` SHALL record the URL after its push step once per active repository, since that workflow can open the pull request when run standalone. That step SHALL be best-effort: when `gh` is unavailable, unauthenticated, or finds no open pull request for the current branch, it SHALL record nothing and SHALL NOT fail the run.

#### Scenario: OpenSpec change workflow shows every link
- **WHEN** a multi-repository run of `openspec/change-v2.0.yaml` finalizes the workspace pull request and a pull request for each affected implementation repository
- **THEN** the run-view breadcrumb shows the workspace pull request first followed by every implementation pull request in affected-repository order

#### Scenario: Draft-PR verification still fails on a missing pull request
- **WHEN** `verify-draft-pr` in `core/implement-change-v1.0.yaml` finds no open draft pull request matching local `HEAD` for the active repository
- **THEN** that repository execution fails exactly as it does today, and adding URL recording has not made it tolerant

## Test Plan

- `INT-002`: Add parser/composition integration coverage under the new task-group package and `workflows/`. Exercise flat implicit full plans, monolithic implicit simple plans, strict configured groups, invalid owners/links/files/order, canonical path output, snapshot identity, compact simple groups, remediation routing, and every embedded workflow/child. Assert explicit `affected_repositories` threading, correct scope declarations, context-neutral exceptions, workspace/repository lifecycle placement, simple finalization, and absence of multi-repository variants. Run the focused package tests, `go test ./workflows`, and the normal `make test` phase.
- `E2E-001`: Add a POSIX public-CLI E2E under `cmd/agent-runner` with a real temporary Git workspace, ignored nested backend/frontend/docs repositories plus a sibling-root variant, grouped numbered task files, and controlled agent, Validator, and `gh` executables. Prove workspace planning/versioning and run storage, backend-complete-before-frontend ordering, repository-specific cwd/built-ins/Validator/Git/PR behavior, untouched docs, evidence/capture isolation, one workspace plus two repository PR URLs in order, saved run projection, and successful completion. Run it in the normal `go test ./...` CI job.

## Done When

- Every built-in planning and repository resolver consumes the one authoritative Go parser for affected order and canonical task inputs; no workflow or shell code reparses ownership or hardcodes a competing directory shape.
- Every shipped orchestration workflow has the approved explicit scope, every intended context-neutral block remains unscoped, and no multi-repository workflow family is added.
- `INT-002` contains an exhaustive assertion over every embedded workflow YAML, including core, OpenSpec, spec-driven, onboarding, older shipped lineages, scaffold, intake, and debug: each is classified as explicitly scoped orchestration or as an approved intentionally unscoped context-neutral block.
- OpenSpec and spec-driven planning run once from the workspace, capture `affected_repositories` from task-group order, and pass it explicitly to every repository-scoped child.
- The repository implementation phase is one contiguous group per active repository, runs repository preflight after planning, feeds only owned numbered tasks, uses repository Validator/Git state, and prepares that repository's draft PR before moving on.
- Workspace acceptance consumes the ordered evidence index, records a validated structured remediation ledger, performs shared spec edits in workspace scope, and routes only owner-specific fixes through repository validation/commit/push/PR-head verification before reacceptance.
- Every repository-scoped built-in report, handoff, screenshot, verification command, and report glob is rooted at `{{repository_output_dir}}`; `workflows/core/implement-change-v1.0.yaml` cannot read sibling session reports or overwrite same-named evidence, while `workflows/core/accept-change-v1.0.yaml` aggregates only through the ordered evidence index into workspace output.
- OpenSpec archive/workspace finalization happens once and implementation repositories finalize independently; spec-driven clean workspaces create no empty PR; implicit simple change retains monolithic planning and now finalizes its repository PR.
- Built-in agent prompts identify repository name/root and workspace root; PR capture remains strict in draft verification and best-effort only in standalone finalization lookup.
- `INT-002` and `E2E-001` pass, including the ignored/sibling first-slice fixture, ordered task execution, isolated Validator markers/evidence, workspace-owned state/audit, untouched unselected repository, and independent PR operations.
- Go changes are formatted with `make fmt`; targeted tests and `make test` pass before handing off.
