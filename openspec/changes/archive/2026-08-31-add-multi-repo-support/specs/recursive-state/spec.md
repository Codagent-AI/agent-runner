## ADDED Requirements

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

#### Scenario: Resume uses persisted selection
- **WHEN** a run with a persisted repository selection is resumed
- **THEN** Agent Runner uses that selection and order without recomputing it from current task files

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

#### Scenario: Repository completes
- **WHEN** backend completes successfully before frontend begins
- **THEN** state marks backend complete before marking frontend active

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

#### Scenario: Workspace-scoped state remains shared
- **WHEN** workspace-scoped planning records progress or a capture
- **THEN** state records it once at run scope rather than copying it into every repository execution

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

#### Scenario: Legacy state has no repository selection
- **WHEN** Agent Runner resumes state written before repository selection was persisted
- **THEN** it uses existing single-repository resume behavior

#### Scenario: New configuration exists during legacy resume
- **WHEN** legacy state has no repository selection but current project configuration declares multiple repositories
- **THEN** Agent Runner does not invent repository fan-out for the legacy run

#### Scenario: New implicit single-repository state
- **WHEN** a new scope-aware run uses one implicit repository
- **THEN** state may identify it internally as `default` while audit prefixes, output filenames, and the user-visible workflow shape remain compatible with existing single-repository execution
