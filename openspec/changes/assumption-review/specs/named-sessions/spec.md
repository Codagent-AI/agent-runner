## ADDED Requirements

### Requirement: Named session declaration

A workflow file MAY declare a top-level `sessions:` list. Each entry MUST have a `name` (string) and an `agent` (profile name). A declaration introduces a role-keyed identity that any step in the same workflow — or in any sub-workflow invoked under a root that includes this workflow — MAY reference via `session: <name>`.

#### Scenario: Single declaration is honored
- **WHEN** a workflow declares `sessions: [{name: planner, agent: planner}]` and a step has `session: planner`
- **THEN** the runner uses the `planner` agent profile for that step and treats the session as the role-keyed `planner` session

#### Scenario: Multiple declarations
- **WHEN** a workflow declares both `planner` and `implementor` named sessions and steps reference each
- **THEN** each step uses the agent profile pinned by its declaration

### Requirement: First-use creation and reuse

A `session: <name>` reference SHALL create the session on its first use within a run and SHALL resume the same session on every subsequent reference, regardless of which workflow file (root or sub-workflow) the reference appears in.

#### Scenario: First reference creates the session
- **WHEN** the named-session map has no entry for `planner` and a step has `session: planner`
- **THEN** the runner starts a new agent session, captures its ID, and stores it under `planner` in the named-session map

#### Scenario: Subsequent reference resumes the session
- **WHEN** the named-session map already contains an entry for `planner` and a later step has `session: planner`
- **THEN** the runner resumes the stored session ID via the agent CLI

#### Scenario: Reuse across sibling sub-workflows
- **WHEN** a step in sub-workflow `plan-change.yaml` creates the `planner` session and a step in sibling sub-workflow `implement-change.yaml` has `session: planner`
- **THEN** the second step resumes the planner session created in the first sub-workflow

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

The named-session map (name → session ID) SHALL be persisted in `RunState`. On `--resume`, the map SHALL be restored before any step executes, so subsequent references resume sessions created in earlier processes.

#### Scenario: Resume preserves named sessions
- **WHEN** a workflow run creates the `planner` session, the process exits, and the user runs `agent-runner --resume`
- **THEN** subsequent `session: planner` references resume the session ID created before the restart

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

The named-session map SHALL propagate from a parent execution context to its children — both into sub-workflows and into loop iterations — and writes from a child SHALL be visible to the parent so that a session created in a child is reusable by later siblings.

#### Scenario: Child creates, parent's later step reuses
- **WHEN** a sub-workflow step creates the `planner` session and a later step in the parent workflow has `session: planner`
- **THEN** the parent's later step resumes that session
