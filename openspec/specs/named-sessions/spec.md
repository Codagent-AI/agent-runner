# named-sessions Specification

## Purpose
Define top-level named session declarations and how role-keyed sessions are shared across workflow and sub-workflow scopes.
## Requirements
### Requirement: Named session declaration

A workflow file MAY declare a top-level `sessions:` list. Each entry MUST have a `name` (string) and an `agent` (profile name). A declaration introduces a role-keyed identity that any step in the same workflow — or in any sub-workflow invoked under a root that includes this workflow — MAY reference via `session: <name>`.

#### Scenario: Single declaration is honored
- **WHEN** a workflow declares `sessions: [{name: planner, agent: planner}]` and a step has `session: planner`
- **THEN** the runner uses the `planner` agent profile for that step and treats the session as the role-keyed `planner` session

#### Scenario: Multiple declarations
- **WHEN** a workflow declares both `planner` and `implementor` named sessions and steps reference each
- **THEN** each step uses the agent profile pinned by its declaration

### Requirement: First-use creation and reuse

A `session: <name>` reference SHALL first resolve an instantiated binding visible in its execution-context ancestry. When a workspace or other ancestor binding exists, the reference SHALL resume that session. When no visible binding exists, first use SHALL create the session in the current execution namespace. A session first created while an explicit repository is active SHALL be local to that repository, SHALL be reusable by descendants and later structural contexts in that repository, and SHALL NOT become visible to sibling repositories or be promoted to workspace scope. Outside repository fan-out, existing run-wide creation and reuse behavior SHALL remain unchanged.

#### Scenario: First reference creates the session
- **WHEN** workspace planning first references `planner` and backend and frontend later reference it
- **THEN** the workspace stores the binding and both repository descendants resume the same planner session

#### Scenario: First reference occurs in repository scope
- **WHEN** backend first references `planner` with no instantiated ancestor binding
- **THEN** backend creates and stores a repository-local planner session

#### Scenario: Sibling repository does not reuse local session
- **WHEN** frontend later references `planner` after backend created it locally and no workspace binding exists
- **THEN** frontend creates a distinct repository-local planner session rather than resuming backend's session

#### Scenario: Repository descendants reuse local session
- **WHEN** a backend child sub-workflow references `planner` after backend created its local binding
- **THEN** the child resumes backend's planner session

#### Scenario: Subsequent reference resumes the session
- **WHEN** a later reference resolves a visible workspace or repository-local `planner` binding
- **THEN** the runner resumes the stored session ID via the agent CLI

#### Scenario: Reuse across sibling sub-workflows
- **WHEN** a workspace sub-workflow creates `planner` and a later workspace sibling references it
- **THEN** the later sibling resumes the same planner session

### Requirement: Compatible declarations across composition

When workflows are composed under a root, declarations MAY appear in multiple files. Two declarations sharing a `name` MUST also share their `agent` value. Compatible duplicates merge silently; incompatible declarations cause validation to fail.

#### Scenario: Compatible duplicate declarations merge
- **WHEN** both the root and a sub-workflow declare `{name: planner, agent: planner}`
- **THEN** validation passes and either file remains valid when run independently

#### Scenario: Incompatible declarations fail validation
- **WHEN** the root declares `{name: planner, agent: planner}` and a reachable sub-workflow declares `{name: planner, agent: implementor}`
- **THEN** validation fails with an error naming the conflicting name and both source file paths

### Requirement: Standalone sub-workflow validity

A sub-workflow file MUST be valid when validated or executed independently of any root. Every `session: <name>` reference in a workflow MUST resolve to a declaration in that file or, when invoked under a root, in some workflow in the composition tree.

#### Scenario: Sub-workflow self-contained
- **WHEN** `plan-change.yaml` declares `planner` and references `session: planner`
- **THEN** the file validates and runs standalone

#### Scenario: Reference resolved only through composition
- **WHEN** `implement-change.yaml` references `session: implementor` without declaring it, and a root that declares `implementor` invokes it
- **THEN** validation from the root passes; standalone validation of the sub-workflow fails with an error naming the unresolved reference

### Requirement: Reserved session names

The names `new`, `resume`, and `inherit` are reserved by existing session-strategy keywords. A `sessions:` declaration MUST NOT use a reserved name.

#### Scenario: Declaration uses a reserved name
- **WHEN** a workflow declares `{name: resume, agent: planner}`
- **THEN** validation fails with an error identifying the reserved keyword

### Requirement: Agent conflict on step

A step MUST NOT set both `session: <name>` and `agent: <x>`. The named-session declaration pins the agent.

#### Scenario: Step sets both session name and agent
- **WHEN** a step has `session: planner` and `agent: implementor`
- **THEN** validation fails with an error indicating that named sessions pin the agent

### Requirement: Unresolved named-session reference

A `session: <name>` reference that does not resolve to any declaration in the composition tree MUST cause validation to fail.

#### Scenario: Reference without declaration anywhere
- **WHEN** a workflow has `session: planner` but no workflow in the composition tree declares `planner`
- **THEN** validation fails with an error naming the missing declaration

### Requirement: Loop iterations share the named session

All iterations of a loop step (including `for-each`) that reference `session: <name>` SHALL share the same named session. Iteration `N+1` resumes the session created in iteration `1`. Per-iteration isolation requires `session: new`.

#### Scenario: For-each iterations share planner session
- **WHEN** a `for-each` loop has body steps with `session: planner` and runs three iterations
- **THEN** iteration 1 creates the planner session and iterations 2 and 3 resume it

### Requirement: Persistence across runner restarts

Named-session bindings SHALL be persisted with their execution namespace in `RunState`. On `--resume`, workspace bindings and repository-local bindings SHALL be restored before execution so each reference resolves the same visible session it used before interruption.

#### Scenario: Resume preserves named sessions
- **WHEN** workspace planning created `planner`, the run exits during frontend, and the user resumes
- **THEN** frontend resumes the persisted workspace planner session

#### Scenario: Resume preserves repository-local bindings
- **WHEN** backend and frontend created distinct local `planner` sessions before interruption
- **THEN** resume restores each binding only in its owning repository

### Requirement: Agent drift on resume

On resume, if the workflow's current `sessions:` declaration for a name has a different `agent` than the agent that originally created the persisted session, the runner SHALL trust the persisted session ID and emit a warning. The runner MUST NOT auto-recreate the session.

#### Scenario: Declared agent differs from persisted session's agent
- **WHEN** `planner` was created with agent profile `planner-v1` and the workflow now declares `agent: planner-v2` for `planner`
- **THEN** resume continues with the persisted session ID and a warning identifies the drift

### Requirement: Coexistence with existing session strategies

`session: new`, `session: resume`, `session: inherit`, and the bare `agent:` form SHALL continue to function unchanged. Named sessions are an additional mechanism that MUST NOT alter resolution of the existing strategies.

#### Scenario: inherit unaffected
- **WHEN** a sub-workflow step has `session: inherit` and the parent's most recent session is unrelated to any named session
- **THEN** `inherit` resolves to the parent's most recent session as before

#### Scenario: resume scoping unchanged
- **WHEN** a step has `session: resume` and a prior step in the same file created an unnamed session
- **THEN** `resume` resolves to that unnamed session and ignores the named-session map

### Requirement: Named-session map propagation

An execution context SHALL read named-session bindings from its ancestors. Ordinary loop and sub-workflow children SHALL write through to their current namespace so later siblings in that same namespace can reuse a created session. A repository fan-out boundary SHALL create a repository-local overlay: it reads existing ancestor bindings, but new bindings are written only to the active repository overlay and are invisible to sibling repositories and workspace scope.

#### Scenario: Child creates, parent's later step reuses
- **WHEN** a workspace sub-workflow creates `planner` outside repository fan-out and a later workspace sibling references it
- **THEN** the sibling resumes that workspace binding

#### Scenario: Repository child writes only to repository overlay
- **WHEN** a backend sub-workflow creates `planner` with no ancestor binding
- **THEN** later backend siblings can reuse it while the workspace and frontend cannot

### Requirement: Agent-call access to named sessions

An agent call targeting `session: <name>` SHALL use the same declaration visibility, pinned agent profile, namespace-aware binding lookup, persistence, composition, and drift behavior as a workflow step targeting that name. Agent calls and workflow steps in the same visible execution namespace SHALL read and update the same binding. A call SHALL add a new binding only after its called child succeeds; a failed, canceled, or runner/transport-error call MUST NOT establish one. A call-level `model` override SHALL apply only to that invocation without changing the declared agent profile, and the invocation SHALL continue to use the CLI resolved from that profile.

#### Scenario: Call creates named session on first use
- **WHEN** a successful backend agent call targets a declared named session with no visible ancestor or backend binding
- **THEN** Agent Runner stores the new binding only in backend's repository namespace

#### Scenario: Workflow step resumes call-created named session
- **WHEN** a backend agent call created `planner` and a later backend workflow step targets it
- **THEN** the workflow step resumes the same backend session

#### Scenario: Sibling call cannot see repository-local session
- **WHEN** frontend calls `planner` after backend created a repository-local binding
- **THEN** frontend does not resume backend's session

#### Scenario: Call resumes workflow-created named session
- **WHEN** workspace planning created `planner` before a repository-scoped call targets it
- **THEN** the call resumes the inherited workspace session

#### Scenario: Unsuccessful first call does not update shared state
- **WHEN** a backend call targets an unbound declared name and then fails, is canceled, or encounters a runner or transport error
- **THEN** neither the backend repository namespace nor an ancestor namespace gains a binding

#### Scenario: Call resolves declaration through composition
- **WHEN** a repository call targets a named session declared within its visible workflow composition
- **THEN** Agent Runner resolves the declaration using the same composition rules as a workflow-step reference

#### Scenario: Call-created named session survives workflow resume
- **WHEN** an agent call creates a workspace or repository-local named session, the runner process exits, and the workflow is resumed
- **THEN** a later call or workflow-step reference in the same visible namespace resumes the persisted CLI session

#### Scenario: Agent drift behavior applies to call-created session
- **WHEN** a persisted workspace or repository-local session has an agent profile that differs from its current declaration on workflow resume
- **THEN** Agent Runner trusts the persisted session ID and emits the existing drift warning without recreating the session

#### Scenario: Invocation overrides do not change declaration
- **WHEN** an agent call targets a named session and supplies a valid `model` override
- **THEN** Agent Runner applies the override to that invocation while leaving the declaration's pinned agent profile and resolved CLI unchanged

