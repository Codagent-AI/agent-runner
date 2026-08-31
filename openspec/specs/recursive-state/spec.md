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

