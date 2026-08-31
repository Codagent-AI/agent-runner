# Task: Persist and Resume Isolated Repository State

## Goal

Make repository fan-out fully durable across process boundaries. Persist workspace and repository identity, recursive per-repository progress, captures, sessions, evidence, outcomes, metrics references, and pull-request results so resume skips completed repositories, restores the failed repository at its deepest nested point, rejects identity or plan drift, and never leaks state between siblings.

## Background

You MUST read:

- `openspec/changes/add-multi-repo-support/design.md`, especially **Persistence and Resume**, **Captures and Sessions**, **Audit, Pull Requests, Metrics, and Run Views** (state contracts), and **Risks and Mitigations**.
- `openspec/changes/add-multi-repo-support/specs/recursive-state/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/output-capture/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/named-sessions/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/run-pull-request-link/spec.md`, especially repository-keyed recording and resume behavior.
- `openspec/changes/add-multi-repo-support/test-plan.md` for `INT-003` and `E2E-002`.

Relevant implementation seams:

- Extend `internal/model/state.go` additively. Model repository fan-out as its own recursive frame around the existing `NestedStepState`; do not replace the loop/sub-workflow state machine. Persist canonical workspace identity, ordered selected repository name/root pairs, per-repository status and nested child state, repository namespaces, and an optional normalized task-plan snapshot/fingerprint.
- Update `internal/runner/runner.go`, `internal/runner/resume.go`, and `internal/stateio/` so selection is written before execution, active progress is flushed at the same durability boundaries as current nested state, and resume validates workspace/root/order/task identity before dispatch.
- Consume the Runner-owned authoritative task-group parser's normalized snapshot/fingerprint API. Persist its output before fan-out and re-parse through that same API on resume; do not implement a second plan parser or compare only raw file bytes.
- Preserve the legacy resume path when repository fields are absent, even if current project config now declares repositories.
- Replace the globally shared mutable capture/session assumptions in `internal/model/context.go`, `internal/session/`, `internal/exec/agent.go`, and `internal/exec/agent_call.go` with an ancestor-visible workspace namespace plus a repository-local overlay. Workspace-created named bindings remain inherited; first use under an explicit repository writes only to that repository.
- Keep `session: inherit` structural: it may resolve a compatible parent in the active repository but cannot fall back across the repository boundary to an unrelated workspace or sibling execution.
- Route Runner-written automatic process output through `repository_output_dir` for explicit repositories. Maintain `{{session_dir}}/output/repository-evidence-index.json` in persisted repository order; keep implicit `default` pointed at the legacy output directory. Built-in workflow-authored report paths are owned by the workflow topology delivery unit.
- Change pull-request capture state in `internal/exec/pull_request.go` and its shared context from one global last URL to a workspace URL plus repository-keyed URLs. Persist and restore them without allowing one scope to overwrite another.
- Update `docs/sessions-and-modes.md` and the named-session sections of `docs/agent-calls.md` for ancestor-visible workspace bindings, repository-local first use/overlays, persistence, shared call/step lookup within one namespace, and `session: inherit` not crossing a repository boundary.
- Add process-level resume fixtures under `cmd/agent-runner` using controlled executables and real Git worktrees. Do not execute `AT-*` or `HT-*`; those remain acceptance responsibilities outside implementation tasks.

## Spec

The following approved requirements and scenarios are binding.

### Requirement: Scope-aware run storage uses the workspace root

Agent Runner MUST require scope-aware workflows to launch from the canonical coordination Git root and MUST key the project run directory, run listing, inspect lookup, state, audit, and resume identity by that root. Legacy unscoped run storage behavior MUST remain unchanged.

#### Scenario: Resume from workspace root
- **WHEN** a scope-aware run launched from `foo` fails during backend and the user resumes from `foo`
- **THEN** Agent Runner finds the same workspace-owned session and restores backend's progress

#### Scenario: Scope-aware launch below root
- **WHEN** the current directory is `foo/openspec` and the canonical workspace root is `foo`
- **THEN** Agent Runner rejects the new scope-aware run rather than creating a second project bucket

#### Scenario: Legacy storage compatibility
- **WHEN** an unscoped legacy workflow runs from a subdirectory or non-Git directory
- **THEN** Agent Runner retains its existing project-bucket and resume behavior

### Requirement: Repository selection persisted

Agent Runner MUST persist the selected repository names and their order before repository-scoped execution begins. Resume MUST use the persisted selection rather than recomputing it from current planning artifacts.

#### Scenario: Ordered repository selection written to state
- **WHEN** repository execution is about to begin with `repositories` ordered as `backend, frontend`
- **THEN** state records backend followed by frontend before either repository starts

#### Scenario: Resume supplies a different selection
- **WHEN** a resume invocation supplies repository names or ordering that differ from persisted state
- **THEN** Agent Runner rejects the resume with an error describing the mismatch

#### Scenario: Planned task groups drift after execution starts
- **WHEN** current planning artifacts assign repositories or task-group order differently from persisted state
- **THEN** Agent Runner rejects resume rather than silently rerouting repository work

### Requirement: Repository execution position persisted

State MUST represent repository fan-out as a recursive execution level containing per-repository status and the existing nested workflow position for the active repository.

#### Scenario: Repository statuses recorded
- **WHEN** a repository-scoped workflow is partway through its selected repositories
- **THEN** state identifies each selected repository as completed, active, or pending

#### Scenario: Active repository nested position
- **WHEN** execution is inside a loop and sub-workflow while repository backend is active
- **THEN** backend's state records the full existing nested execution path beneath its repository level

#### Scenario: Repository fails
- **WHEN** execution fails at a nested step while backend is active
- **THEN** state retains backend's exact nested position and leaves later repositories pending

### Requirement: Repository-scoped runtime state

Unnamed agent execution state, captured variables, outcomes, metrics, evidence references, pull-request links, and named sessions first instantiated in repository scope MUST be associated with the repository execution that produced them. Workspace-scoped state and named sessions already instantiated in an ancestor workspace context MUST remain inherited shared state.

#### Scenario: Same capture name in two repositories
- **WHEN** backend and frontend each capture a value using the same declared capture name
- **THEN** state retains both values under their respective repository executions without one overwriting the other

#### Scenario: Workspace named session spans repository executions
- **WHEN** workspace planning instantiates `lead-agent` before backend and frontend invoke that name
- **THEN** state restores the same inherited workspace session identity in both repository contexts

#### Scenario: Named session first instantiated in repository scope
- **WHEN** a standalone repository workflow first invokes `lead-agent` separately while backend and frontend are active
- **THEN** state records distinct named-session identities under backend and frontend and does not promote either to workspace scope

#### Scenario: Unnamed session execution restored by repository
- **WHEN** backend execution resumes an unnamed repository-scoped agent step
- **THEN** state restores the execution identity belonging to backend rather than one created during another repository's execution

#### Scenario: Repository pull-request outcomes
- **WHEN** backend and frontend each record a pull-request URL and completion outcome
- **THEN** state retains each result under its repository execution

### Requirement: Deterministic repository resume

Before resuming repository-scoped work, Agent Runner MUST restore and validate the original workspace and repository identities. It MUST skip completed repositories and continue at the active repository's recorded position.

#### Scenario: Resume skips completed repository
- **WHEN** backend completed before the run failed during frontend
- **THEN** resume does not execute backend again

#### Scenario: Resume failed repository
- **WHEN** a run failed inside frontend at a nested step
- **THEN** resume restores frontend's active context and continues from its deepest recorded position

#### Scenario: Pending repository remains pending
- **WHEN** a run resumes an earlier failed repository
- **THEN** repositories ordered after it remain pending until the failed repository completes

#### Scenario: Selected repository removed from configuration
- **WHEN** a persisted selected repository name no longer exists in current workspace configuration
- **THEN** resume fails with an error naming the missing repository

#### Scenario: Selected name maps to a different root
- **WHEN** a persisted selected repository name resolves to a different canonical repository root in current configuration
- **THEN** resume fails with an error describing the identity mismatch rather than executing against the different checkout

### Requirement: Repository state compatibility

State written before multi-repository support MUST remain resumable as a legacy single-repository run.

#### Scenario: New configuration exists during legacy resume
- **WHEN** legacy state has no repository selection but current project configuration declares multiple repositories
- **THEN** Agent Runner does not invent repository fan-out for the legacy run

### Requirement: Repository capture persistence

Agent Runner MUST persist repository-scoped captured values with the repository identity that produced them and MUST restore only the active repository's captures on resume.

#### Scenario: Typed repository capture
- **WHEN** a repository-scoped step captures a string, list, or map value
- **THEN** Agent Runner preserves its existing capture type under that repository's identity

### Requirement: Repository captures are not automatically aggregated

After repository fan-out, Agent Runner MUST NOT synthesize repository captures into workspace interpolation variables.

#### Scenario: Workspace references repository-only capture
- **WHEN** backend captured `validator_output` and a later workspace step references `{{validator_output}}` without a workspace value of that name
- **THEN** interpolation fails with an undefined-variable error

#### Scenario: No implicit nested capture map
- **WHEN** multiple repositories capture the same variable name
- **THEN** Agent Runner does not create an implicit map such as `{{validator_output.backend}}`

### Requirement: Captured variable scope

Captured variables SHALL be available to all subsequent steps within the same structural and execution scope. Within one execution scope this includes sibling steps, nested child steps, and subsequent loop iterations. Workspace-scoped captures SHALL also be available as shared inputs within descendant repository executions. Repository-scoped captures SHALL be available only while their repository is active and MUST NOT leak into another repository or back into workspace scope. Captured variables from a sub-workflow are NOT available in the parent workflow after the sub-workflow completes.

When a repository capture uses the same name as an available workspace capture, the repository value SHALL shadow the workspace value only within that repository execution. Returning to workspace scope SHALL reveal the unchanged workspace value.

#### Scenario: Repository capture shadows workspace capture temporarily
- **WHEN** workspace scope captured `output`, backend captures a different `output`, and execution later returns to workspace scope
- **THEN** backend sees its repository value while active and workspace scope subsequently sees its original unchanged value

### Requirement: Repository-written evidence is path-isolated

This delivery unit owns Runner-written automatic process output and the ordered evidence index. Shipped workflow-authored reports and globs consume this contract and must not be implemented by adding a second index or path scheme here.

Agent Runner MUST persist automatic process output for explicit repository-scoped steps beneath `{{repository_output_dir}}`. Built-in repository-scoped workflows MUST write durable reports, handoffs, screenshots, and other directly named evidence beneath the same directory. Agent Runner MUST maintain an ordered workspace-readable `{{session_dir}}/output/repository-evidence-index.json` mapping each selected repository to its evidence directory; the implicit repository entry MUST point to the legacy output directory. Workspace-scoped aggregation MUST read repository evidence through that index and MUST write aggregate evidence only to the workspace run output directory.

#### Scenario: Same evidence filename in two repositories
- **WHEN** backend and frontend each write `acceptance-assumptions.md`
- **THEN** each writes beneath its own `{{repository_output_dir}}` and neither file overwrites the other

#### Scenario: Workspace acceptance aggregates repository evidence
- **WHEN** aggregate acceptance begins after repository implementation completes
- **THEN** it reads `repository-evidence-index.json`, processes every selected repository's evidence directory in persisted order, and writes its aggregate handoff beneath `{{session_dir}}/output`

### Requirement: First-use creation and reuse

A `session: <name>` reference SHALL first resolve an instantiated binding visible in its execution-context ancestry. When a workspace or other ancestor binding exists, the reference SHALL resume that session. When no visible binding exists, first use SHALL create the session in the current execution namespace. A session first created while an explicit repository is active SHALL be local to that repository, SHALL be reusable by descendants and later structural contexts in that repository, and SHALL NOT become visible to sibling repositories or be promoted to workspace scope. Outside repository fan-out, existing run-wide creation and reuse behavior SHALL remain unchanged.

#### Scenario: Workspace first reference creates inherited session
- **WHEN** workspace planning first references `planner` and backend and frontend later reference it
- **THEN** the workspace stores the binding and both repository descendants resume the same planner session

#### Scenario: Sibling repository does not reuse local session
- **WHEN** frontend later references `planner` after backend created it locally and no workspace binding exists
- **THEN** frontend creates a distinct repository-local planner session rather than resuming backend's session

### Requirement: Persistence across runner restarts

Named-session bindings SHALL be persisted with their execution namespace in `RunState`. On `--resume`, workspace bindings and repository-local bindings SHALL be restored before execution so each reference resolves the same visible session it used before interruption.

#### Scenario: Resume preserves repository-local bindings
- **WHEN** backend and frontend created distinct local `planner` sessions before interruption
- **THEN** resume restores each binding only in its owning repository

### Requirement: Named-session map propagation

An execution context SHALL read named-session bindings from its ancestors. Ordinary loop and sub-workflow children SHALL write through to their current namespace so later siblings in that same namespace can reuse a created session. A repository fan-out boundary SHALL create a repository-local overlay: it reads existing ancestor bindings, but new bindings are written only to the active repository overlay and are invisible to sibling repositories and workspace scope.

#### Scenario: Ordinary child creates parent-visible binding
- **WHEN** a workspace sub-workflow creates `planner` outside repository fan-out and a later workspace sibling references it
- **THEN** the sibling resumes that workspace binding

#### Scenario: Repository child writes only to repository overlay
- **WHEN** a backend sub-workflow creates `planner` with no ancestor binding
- **THEN** later backend siblings can reuse it while the workspace and frontend cannot

### Requirement: Agent-call access to named sessions

An agent call targeting `session: <name>` SHALL use the same declaration visibility, pinned agent profile, namespace-aware binding lookup, persistence, composition, and drift behavior as a workflow step targeting that name. Agent calls and workflow steps in the same visible execution namespace SHALL read and update the same binding. A call SHALL add a new binding only after its called child succeeds; a failed, canceled, or runner/transport-error call MUST NOT establish one. A call-level `model` override SHALL apply only to that invocation without changing the declared agent profile, and the invocation SHALL continue to use the CLI resolved from that profile.

#### Scenario: Unsuccessful first call does not bind a repository session
- **WHEN** a backend call targets an unbound declared name and then fails, is canceled, or encounters a runner or transport error
- **THEN** neither the backend repository namespace nor an ancestor namespace gains a binding

### Requirement: Reserved capture records a run's pull request URL

The captured-variable name `pr_url` SHALL be reserved. Whenever a step's capture binds `pr_url` to a string value that is non-empty after trimming surrounding whitespace, a `pull_request_recorded` audit event SHALL be emitted at the point of capture, carrying the trimmed URL in a `url` field of the event data. While a repository is active, the recording SHALL be associated with that repository; otherwise it SHALL remain a run-level recording.

The most recently recorded value within the same scope SHALL win. A repository-scoped recording SHALL replace only the current URL for that repository and SHALL NOT replace another repository's URL. A workspace-scoped recording SHALL replace only the run-level URL.

#### Scenario: Recording survives resume
- **WHEN** a run that recorded repository pull-request URLs is interrupted and resumed
- **THEN** the resumed run still has every repository's recorded URL and the run view still shows them, whether or not the resumed session re-emits the events

## Test Plan

- `INT-003`: Add reconstruction integration tests beside `internal/runner` and `internal/stateio`. Execute two explicit repositories; fail the second inside a loop inside a sub-workflow after same-named string/list/map captures and evidence writes; reconstruct a fresh runner; exercise workspace-first and repository-first named sessions plus unnamed sessions, repository PR outcomes, valid resume, changed order/root/ownership, removed repositories, and legacy state. Run `go test ./internal/runner ./internal/stateio` and the normal `make test` phase.
- `E2E-002`: Add a built-CLI, process-boundary test under `cmd/agent-runner` with three selected repositories and durable invocation markers. Prove fail-fast, an overlapping-run lock rejection, unchanged resume from the workspace root, deepest-position continuation without replay, later-repository deferral, root/order/task-owner drift rejection before commands, and subdirectory launch rejection. Run it in `go test ./...` with a bounded timeout.

## Done When

- State records canonical workspace identity, selected repository names/roots/order before execution, per-boundary repository frames, per-repository status, active nested state, namespace state, PR results, and an optional normalized task-group fingerprint.
- Resume validates launch workspace, selected names, canonical roots, explicit order, and task snapshot before spawning work; it skips completed repositories and restores the active repository's deepest loop/sub-workflow position.
- Workspace captures flow down as read inputs; repository captures persist with type, shadow only locally, never flow sideways/upward, and restore only for their owner.
- Workspace-created named sessions remain shared through ancestry; repository-first named sessions and all unnamed execution identities remain repository-local across restart; `inherit` never crosses the repository boundary.
- Ordinary loop and sub-workflow children outside repository fan-out retain write-through named-session creation for later siblings; repository children write only to their active repository overlay.
- Runner-written automatic process output is isolated under stable repository output directories; the ordered evidence index is atomic/workspace-readable; implicit output paths remain unchanged. Built-in workflow-authored reports consume the same index under the workflow topology contract.
- Workspace and repository pull-request URLs are scope-keyed, deduplicated within scope, persisted, and restored without cross-repository replacement.
- `docs/sessions-and-modes.md` and `docs/agent-calls.md` describe workspace-inherited and repository-local named-session namespaces and the repository boundary for `inherit` without retaining the obsolete single run-wide-map claim.
- Legacy state without repository fields resumes through the existing non-fan-out path even under newly added repository configuration.
- `INT-003` and `E2E-002` pass, including typed capture restoration, configuration/plan drift, lock overlap, and no command execution on invalid resume.
- Go changes are formatted with `make fmt`; focused and broader tests pass before handing off.
