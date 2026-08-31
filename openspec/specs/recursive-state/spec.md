# Capability: recursive-state

## Purpose

Defines how the state file tracks execution position recursively through nested workflows and loops, and how resume restores that position.
## Requirements
### Requirement: Recursive position tracking

The state file (`state.json`) SHALL track the current execution position through nested workflows and loops recursively. When execution is inside a sub-workflow or loop, the state file SHALL capture the full nesting path from the top-level workflow down to the currently executing step.

#### Scenario: Flat workflow state
- **WHEN** a workflow with no loops or sub-workflows completes step 2 of 4
- **THEN** the state file records `currentStep` as the step 2 ID (unchanged from current behavior)

#### Scenario: Nested sub-workflow state
- **WHEN** execution is inside step `run-validator` of `implement-task.yaml`, which is itself inside the `implement-tasks` loop of `implement-change.yaml`, on the `validator` step
- **THEN** the state file captures the full path: implement-change → implement-tasks (iteration index) → implement-task → run-validator → validator

#### Scenario: Loop iteration tracking
- **WHEN** execution is on iteration 2 of a for-each loop
- **THEN** the state file records the current iteration index and the loop variable's current value

### Requirement: Captured variables in state

Captured variables SHALL be persisted in the state file alongside session IDs. This allows resume to restore captured values without re-executing the capture step.

#### Scenario: Captured variable persisted
- **WHEN** a shell step captures stdout into `validator_output` and Agent Runner writes the state file
- **THEN** the state file includes `validator_output` and its value

#### Scenario: Resume restores captured variables
- **WHEN** Agent Runner resumes from a state file that contains captured variables
- **THEN** the captured variables are available for interpolation in subsequent steps

### Requirement: Resume from nested position

`agent-runner -resume` SHALL restore execution to the exact nested position recorded in the state file, including loop iteration and sub-workflow depth. Execution continues from the step after the last completed step at the deepest nesting level.

#### Scenario: Resume into a loop
- **WHEN** the state file records position inside a for-each loop at iteration 3 of 5
- **THEN** Agent Runner resumes at iteration 3, skipping iterations 1 and 2

#### Scenario: Resume into a sub-workflow
- **WHEN** the state file records position inside a sub-workflow with step 2 of 3 as the last completed step
- **THEN** Agent Runner resumes inside the sub-workflow at step 3, within the parent's context

#### Scenario: Resume with stale nested state
- **WHEN** the sub-workflow file has changed since the state was written and the recorded step ID no longer exists
- **THEN** Agent Runner fails with a descriptive error identifying the missing step and which workflow file changed

### Requirement: Resolved profile set persisted in state

The state file SHALL record the name of the profile set the run resolved at launch, regardless of whether
that name came from a `--profile` override, the project config's `active_profile`, or the `default`
fallback. The field SHALL be serialized as `profileSet`, matching the state file's existing camelCase key
style, and SHALL be written when the run's state is first persisted. Because a profile set is resolved for
every run (see the `config-profiles` capability), the field SHALL be present for every run this version
writes; it is omitted only from state files written by earlier versions.

#### Scenario: Overridden profile set recorded

- **WHEN** `agent-runner --profile copilot my-workflow` starts and the runner writes state
- **THEN** the state file records `copilot` as the run's profile set

#### Scenario: Config-selected profile set recorded

- **WHEN** a run starts with no `--profile` flag in a project whose config sets `active_profile: work`
- **THEN** the state file records `work` as the run's profile set

#### Scenario: Default profile set recorded

- **WHEN** a run starts with no `--profile` flag in a project that sets no `active_profile`
- **THEN** the state file records `default` as the run's profile set

#### Scenario: Shell-only workflow records a profile set

- **WHEN** a workflow containing only shell steps runs with no `--profile` flag in a project that sets no
  `active_profile`
- **THEN** the state file records `default` as the run's profile set, the same as any other run

### Requirement: Resume reuses the recorded profile set

When `agent-runner --resume` restores a run whose state file records a profile set and no `--profile`
override is supplied, the runner SHALL select that recorded profile set for the remaining steps, in
preference to the project config's `active_profile` and to the `default` fallback. It SHALL do so by
supplying the recorded name to config loading as the caller-supplied override, labelled as originating
from the run's state file (see the `config-profiles` capability). When the recorded profile set no longer
exists in the merged config, resume SHALL fail with an error naming the recorded profile set and stating
that it came from the run's state file.

Resume selection precedence SHALL therefore be, highest first: the `--profile` override, the profile set
recorded in the state file, the project config's `active_profile`, then `default`.

#### Scenario: Resume continues with the launch profile set

- **WHEN** a run started with `--profile copilot` is interrupted and `agent-runner --resume` is invoked with
  no `--profile` flag
- **THEN** the remaining steps resolve agents from the `copilot` profile set

#### Scenario: Recorded profile set outranks active_profile on resume

- **WHEN** a run started with `--profile copilot` is interrupted, the project config sets
  `active_profile: work`, and `agent-runner --resume` is invoked with no `--profile` flag
- **THEN** the remaining steps resolve agents from `copilot`, not `work`

#### Scenario: Recorded profile set no longer exists

- **WHEN** a run recorded profile set `copilot`, that profile set has since been removed from the config,
  and `agent-runner --resume` is invoked with no `--profile` flag
- **THEN** resume fails with an error naming `copilot` and identifying the run's state file as the source of
  the name

#### Scenario: State file predates this change

- **WHEN** `agent-runner --resume` restores a state file that records no profile set
- **THEN** profile-set selection falls back to existing behavior (`active_profile` when set, otherwise
  `default`) and resume proceeds

### Requirement: Override on resume replaces the recorded profile set

When `agent-runner --resume` is invoked with `--profile`, the override SHALL take precedence over the
profile set recorded in the state file for all remaining steps, and the runner SHALL update the recorded
profile set to the overridden name so that any later resume of the same run continues with it. Steps that
already completed under the previous profile set SHALL NOT be re-executed on account of the change.

#### Scenario: Resume with an override switches profile sets

- **WHEN** a run started with `--profile copilot` is interrupted and `agent-runner --resume --profile work`
  is invoked
- **THEN** the remaining steps resolve agents from the `work` profile set

#### Scenario: Override on resume is re-recorded

- **WHEN** `agent-runner --resume --profile work` continues a run that had recorded `copilot`, and the run is
  interrupted again
- **THEN** the state file records `work`, and a subsequent `agent-runner --resume` with no flag continues
  with `work`

#### Scenario: Override on resume does not rewind progress

- **WHEN** `agent-runner --resume --profile work` continues a run that had completed three steps under
  `copilot`
- **THEN** execution continues from the recorded position and the three completed steps are not re-executed

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

