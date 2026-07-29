## ADDED Requirements

### Requirement: Run start profile set data

The `run_start` audit entry SHALL include the name of the profile set the run resolved and the source of
that selection, using the values `flag` (a `--profile` override), `config` (the project config's
`active_profile`), `state` (a profile set recorded in the state file and reused on resume), and `default`
(the `default` fallback). The fields SHALL be serialized as `profile_set` and `profile_source`, matching the
entry's existing snake_case key style. Because a profile set is resolved for every run (see the
`config-profiles` capability), both fields SHALL be present on every `run_start` entry.

#### Scenario: Flag-sourced profile set

- **WHEN** `agent-runner --profile copilot my-workflow` starts and `run_start` is written
- **THEN** the entry records profile set `copilot` with source `flag`

#### Scenario: Config-sourced profile set

- **WHEN** a run starts with no `--profile` flag in a project whose config sets `active_profile: work`
- **THEN** the `run_start` entry records profile set `work` with source `config`

#### Scenario: Default-sourced profile set

- **WHEN** a run starts with no `--profile` flag in a project that sets no `active_profile`
- **THEN** the `run_start` entry records profile set `default` with source `default`

#### Scenario: State-sourced profile set on resume

- **WHEN** a run that recorded profile set `copilot` is resumed with no `--profile` flag
- **THEN** the resumed run's `run_start` entry records profile set `copilot` with source `state`

#### Scenario: Override on resume is recorded as flag-sourced

- **WHEN** a run that recorded profile set `copilot` is resumed with `--profile work`
- **THEN** the resumed run's `run_start` entry records profile set `work` with source `flag`

#### Scenario: Shell-only workflow

- **WHEN** a workflow containing only shell steps starts with no `--profile` flag in a project that sets no
  `active_profile`
- **THEN** the `run_start` entry records profile set `default` with source `default`, the same as any other
  run

#### Scenario: Profile set data does not appear on other events

- **WHEN** `step_start`, `sub_workflow_start`, `iteration_start`, or any end event is written
- **THEN** the entry does not carry profile set fields
