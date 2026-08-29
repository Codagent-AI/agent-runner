## ADDED Requirements

### Requirement: Repository boundary event data
Each explicit configured repository execution MUST emit one `repository_start` and one terminal `repository_end`. The start event MUST identify the repository name, canonical root, zero-based position, and total selected repository count. The end event MUST include the repository outcome and duration. The transparent implicit `default` repository MUST retain legacy audit shape without repository boundary events.

#### Scenario: Repository starts
- **WHEN** backend begins as the first of two selected repositories
- **THEN** Agent Runner emits `repository_start` identifying backend, its canonical root, position 0, and total count 2

#### Scenario: Repository succeeds
- **WHEN** backend completes successfully
- **THEN** Agent Runner emits `repository_end` with outcome `success` and backend's elapsed duration

#### Scenario: Repository fails
- **WHEN** backend reaches a terminal failure
- **THEN** Agent Runner emits `repository_end` with outcome `failed` before the run stops

#### Scenario: Pending repository never starts
- **WHEN** backend fails before pending repository frontend begins
- **THEN** the audit log contains no repository boundary pair for frontend

#### Scenario: Workspace-only run
- **WHEN** a run executes only workspace-scoped work
- **THEN** the audit log contains no repository boundary events

#### Scenario: Implicit repository compatibility
- **WHEN** a scope-aware run executes through the implicit `default` repository
- **THEN** the audit log retains the legacy step prefixes and omits repository boundary events and explicit repository fields

### Requirement: Repository identity on active events
Every audit event emitted while an explicit configured repository is active MUST include `repository_name` and `repository_dir` fields in addition to its nesting prefix. Events emitted without an explicit active repository, including the transparent implicit `default` path, MUST omit these fields rather than writing empty values.

#### Scenario: Repository step events
- **WHEN** a step emits start and end events while backend is active
- **THEN** both events identify backend and its canonical repository root

#### Scenario: Nested and control events
- **WHEN** a loop, sub-workflow, agent call, control event, or error event is emitted while frontend is active
- **THEN** the event includes frontend's repository name and root

#### Scenario: Workspace event
- **WHEN** an event is emitted in workspace scope with no active repository
- **THEN** the event omits `repository_name` and `repository_dir`

#### Scenario: Root error during repository execution
- **WHEN** an unexpected error uses an empty root prefix while a repository is active
- **THEN** its explicit repository fields still identify the active repository

## MODIFIED Requirements

### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `repository_start`, `repository_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `agent_call_start`, `agent_call_end`, `error`, `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, and `child_continued`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Completion events are intermediate
- **WHEN** the audit logger receives control or durability events during an interactive agent step
- **THEN** it writes them as intermediate events distinct from the step's final `step_end`

#### Scenario: Agent-call events are distinct from workflow steps
- **WHEN** the audit logger receives an `agent_call_start` or `agent_call_end` event
- **THEN** it records the event without representing the call as a workflow `step_start` or `step_end`

#### Scenario: Repository events are container boundaries
- **WHEN** the audit logger receives a `repository_start` or `repository_end` event
- **THEN** it records the event as a repository execution boundary rather than an ordinary workflow step

### Requirement: Nesting prefix

Every audit log entry SHALL include a nesting prefix that encodes the full path to the current execution point. Repository executions carry `repo:<repository_name>`. Loop steps carry their iteration index as `step_name:N`. Sub-workflows are marked with `sub:workflow_name`. Top-level steps use `[step_name]`. Root-scoped events (`run_start`, `run_end`, `error`) use an empty prefix string, while explicit repository fields retain active repository identity on a root-scoped error.

#### Scenario: Top-level step
- **WHEN** a step `validate` executes at the workflow root
- **THEN** entries have prefix `[validate]`

#### Scenario: Step inside a loop
- **WHEN** step `implement` executes inside loop `task-loop` at iteration 2
- **THEN** entries have prefix `[task-loop:2, implement]`

#### Scenario: Step inside a sub-workflow inside a loop
- **WHEN** step `check` executes inside sub-workflow `verify-task`, invoked from loop `task-loop` at iteration 0 via step `verify`
- **THEN** entries have prefix `[task-loop:0, verify, sub:verify-task, check]`

#### Scenario: Top-level repository step
- **WHEN** step `validate` executes for repository backend
- **THEN** entries have prefix `[validate, repo:backend]`

#### Scenario: Nested repository step
- **WHEN** step `implement-task` executes inside repository-scoped group `implement-task-groups` and loop `task-loop` at iteration 0 for backend
- **THEN** entries have prefix `[implement-task-groups, repo:backend, task-loop:0, implement-task]`

#### Scenario: Workspace-scoped nesting
- **WHEN** a workspace-scoped step executes
- **THEN** its prefix contains no `repo:` segment

### Requirement: Context snapshot on start events

Start events (`run_start`, `repository_start`, `step_start`, `iteration_start`, `sub_workflow_start`, `agent_call_start`) SHALL include the full context snapshot: all params and all captured variables available at that point. An `agent_call_start` SHALL use the parent attempt's current context snapshot. While a repository is active, the available captures MUST consist of run-level workspace captures plus captures scoped to the active repository and MUST exclude captures belonging to other repositories.

#### Scenario: Step start includes params and captured variables
- **WHEN** a `step_start` event is emitted and the context has params `{env: "staging"}` and captured variables `{build_output: "/tmp/build"}`
- **THEN** the entry includes both in the context snapshot

#### Scenario: Agent-call start includes parent context
- **WHEN** an `agent_call_start` event is emitted
- **THEN** the entry includes the params and captured variables available to its parent attempt

#### Scenario: End events omit context snapshot
- **WHEN** a `step_end` or `agent_call_end` event is emitted
- **THEN** the entry does not include a context snapshot

#### Scenario: Repository start context
- **WHEN** a start event is emitted while backend is active
- **THEN** its context includes workspace captures and backend captures but excludes captures owned by frontend

#### Scenario: Workspace start context
- **WHEN** a start event is emitted in workspace scope
- **THEN** its context includes workspace captures and excludes repository-scoped captures

### Requirement: End event data

End events (`step_end`, `run_end`, `repository_end`, `iteration_end`, `sub_workflow_end`, `agent_call_end`) SHALL include the outcome (`success`, `failed`, `aborted`, `exhausted`, `skipped`) and duration in milliseconds.

#### Scenario: Step end includes outcome and duration
- **WHEN** a step completes after 1500ms with outcome `success`
- **THEN** the `step_end` entry includes `outcome: "success"` and `duration_ms: 1500`

#### Scenario: Agent-call end includes outcome and duration
- **WHEN** an agent call reaches a terminal outcome
- **THEN** its `agent_call_end` entry includes that outcome and the call's duration in milliseconds

#### Scenario: Repository end includes outcome and duration
- **WHEN** a repository execution reaches a terminal outcome
- **THEN** its `repository_end` entry includes that outcome and the repository execution's duration in milliseconds
