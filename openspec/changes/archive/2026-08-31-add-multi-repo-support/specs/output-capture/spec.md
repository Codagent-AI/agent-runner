## ADDED Requirements

### Requirement: Repository capture persistence
Agent Runner MUST persist repository-scoped captured values with the repository identity that produced them and MUST restore only the active repository's captures on resume.

#### Scenario: Same capture name in multiple repositories
- **WHEN** backend and frontend each capture `validator_output`
- **THEN** Agent Runner persists two repository-keyed values without one overwriting the other

#### Scenario: Resume restores active repository capture
- **WHEN** a run resumes during frontend execution after frontend captured `validator_output`
- **THEN** interpolation restores frontend's value rather than backend's value

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

#### Scenario: Evidence remains available
- **WHEN** repository captures are unavailable to workspace interpolation
- **THEN** their persisted state and audit evidence remain available through repository-aware run records

### Requirement: Repository-written evidence is path-isolated
Agent Runner MUST persist automatic process output for explicit repository-scoped steps beneath `{{repository_output_dir}}`. Built-in repository-scoped workflows MUST write durable reports, handoffs, screenshots, and other directly named evidence beneath the same directory. Agent Runner MUST maintain an ordered workspace-readable `{{session_dir}}/output/repository-evidence-index.json` mapping each selected repository to its evidence directory; the implicit repository entry MUST point to the legacy output directory. Workspace-scoped aggregation MUST read repository evidence through that index and MUST write aggregate evidence only to the workspace run output directory.

#### Scenario: Repository agent output is isolated automatically
- **WHEN** backend and frontend each execute an agent step that persists `.out` or `.err` output
- **THEN** Agent Runner writes each process output beneath that repository's `{{repository_output_dir}}`

#### Scenario: Same evidence filename in two repositories
- **WHEN** backend and frontend each write `acceptance-assumptions.md`
- **THEN** each writes beneath its own `{{repository_output_dir}}` and neither file overwrites the other

#### Scenario: Repository review reads only its own reports
- **WHEN** backend assumption review reads session reports during backend execution
- **THEN** its evidence glob is confined to backend's repository output directory and excludes frontend reports

#### Scenario: Workspace acceptance aggregates repository evidence
- **WHEN** aggregate acceptance begins after repository implementation completes
- **THEN** it reads `repository-evidence-index.json`, processes every selected repository's evidence directory in persisted order, and writes its aggregate handoff beneath `{{session_dir}}/output`

#### Scenario: Implicit evidence index entry
- **WHEN** an implicit single-repository run writes its evidence index
- **THEN** the index points to the existing `{{session_dir}}/output` directory without exposing `default` in user-facing evidence paths

## MODIFIED Requirements

### Requirement: Captured variable scope

Captured variables SHALL be available to all subsequent steps within the same structural and execution scope. Within one execution scope this includes sibling steps, nested child steps, and subsequent loop iterations. Workspace-scoped captures SHALL also be available as shared inputs within descendant repository executions. Repository-scoped captures SHALL be available only while their repository is active and MUST NOT leak into another repository or back into workspace scope. Captured variables from a sub-workflow are NOT available in the parent workflow after the sub-workflow completes.

When a repository capture uses the same name as an available workspace capture, the repository value SHALL shadow the workspace value only within that repository execution. Returning to workspace scope SHALL reveal the unchanged workspace value.

#### Scenario: Variable available to sibling steps
- **WHEN** step A captures `output` and step B (a sibling in the same execution scope) references `{{output}}`
- **THEN** step B receives the captured value

#### Scenario: Variable available within loop iterations
- **WHEN** a shell step inside a loop captures `output` on iteration 1
- **THEN** `{{output}}` is available in the same iteration's subsequent steps, and is overwritten on each new iteration

#### Scenario: Variable does not leak from sub-workflow to parent
- **WHEN** a sub-workflow captures `internal_var` and the parent step after the sub-workflow references `{{internal_var}}`
- **THEN** Agent Runner fails with an undefined variable error

#### Scenario: Workspace capture available in repository execution
- **WHEN** workspace planning captures `change_dir` before repository execution begins
- **THEN** subsequent backend and frontend executions can interpolate the shared workspace value

#### Scenario: Repository capture isolated from another repository
- **WHEN** backend captures `output` and frontend later references `{{output}}` without creating its own value or receiving a workspace value
- **THEN** interpolation in frontend fails rather than receiving backend's capture

#### Scenario: Repository capture unavailable after fan-out
- **WHEN** backend captures `output` and execution later returns to workspace scope
- **THEN** the workspace cannot interpolate backend's repository-scoped value

#### Scenario: Repository capture shadows workspace capture temporarily
- **WHEN** workspace scope captured `output`, backend captures a different `output`, and execution later returns to workspace scope
- **THEN** backend sees its repository value while active and workspace scope subsequently sees its original unchanged value
