## ADDED Requirements

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
