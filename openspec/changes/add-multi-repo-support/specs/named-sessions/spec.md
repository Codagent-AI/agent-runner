## MODIFIED Requirements

### Requirement: First-use creation and reuse

A `session: <name>` reference SHALL first resolve an instantiated binding visible in its execution-context ancestry. When a workspace or other ancestor binding exists, the reference SHALL resume that session. When no visible binding exists, first use SHALL create the session in the current execution namespace. A session first created while an explicit repository is active SHALL be local to that repository, SHALL be reusable by descendants and later structural contexts in that repository, and SHALL NOT become visible to sibling repositories or be promoted to workspace scope. Outside repository fan-out, existing run-wide creation and reuse behavior SHALL remain unchanged.

#### Scenario: Workspace first reference creates inherited session
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

#### Scenario: Same-namespace sibling reuse remains unchanged
- **WHEN** a workspace sub-workflow creates `planner` and a later workspace sibling references it
- **THEN** the later sibling resumes the same planner session

### Requirement: Persistence across runner restarts

Named-session bindings SHALL be persisted with their execution namespace in `RunState`. On `--resume`, workspace bindings and repository-local bindings SHALL be restored before execution so each reference resolves the same visible session it used before interruption.

#### Scenario: Resume preserves inherited workspace binding
- **WHEN** workspace planning created `planner`, the run exits during frontend, and the user resumes
- **THEN** frontend resumes the persisted workspace planner session

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

#### Scenario: Repository call creates local named session
- **WHEN** a successful backend agent call targets a declared named session with no visible ancestor or backend binding
- **THEN** Agent Runner stores the new binding only in backend's repository namespace

#### Scenario: Workflow step resumes call-created repository session
- **WHEN** a backend agent call created `planner` and a later backend workflow step targets it
- **THEN** the workflow step resumes the same backend session

#### Scenario: Sibling call cannot see repository-local session
- **WHEN** frontend calls `planner` after backend created a repository-local binding
- **THEN** frontend does not resume backend's session

#### Scenario: Call resumes workspace-created session
- **WHEN** workspace planning created `planner` before a repository-scoped call targets it
- **THEN** the call resumes the inherited workspace session

#### Scenario: Unsuccessful first call does not bind a repository session
- **WHEN** a backend call targets an unbound declared name and then fails, is canceled, or encounters a runner or transport error
- **THEN** neither the backend repository namespace nor an ancestor namespace gains a binding

#### Scenario: Call resolves declaration through composition
- **WHEN** a repository call targets a named session declared within its visible workflow composition
- **THEN** Agent Runner resolves the declaration using the same composition rules as a workflow-step reference

#### Scenario: Agent drift behavior applies after resume
- **WHEN** a persisted workspace or repository-local session has an agent profile that differs from its current declaration on workflow resume
- **THEN** Agent Runner trusts the persisted session ID and emits the existing drift warning without recreating the session

#### Scenario: Invocation override does not change declaration
- **WHEN** an agent call targets a named session and supplies a valid `model` override
- **THEN** Agent Runner applies the override to that invocation while leaving the declaration's pinned agent profile and resolved CLI unchanged
