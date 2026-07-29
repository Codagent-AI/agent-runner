## ADDED Requirements

### Requirement: Profile flag surface

The CLI SHALL accept a `--profile <name>` flag whose value names a profile set to use for the current
invocation. The flag SHALL be documented in the CLI usage text. Because the flag is parsed by the
standard flag package like every other Agent Runner flag, `-profile` and `--profile` SHALL behave
identically, and both `--profile name` and `--profile=name` SHALL be accepted.

#### Scenario: Flag selects a profile set for the run

- **WHEN** `agent-runner --profile copilot my-workflow` is invoked and a `copilot` profile set exists in
  the merged config
- **THEN** every `agent: <name>` reference in the run resolves against the `copilot` profile set's
  agents

#### Scenario: Single-dash and equals forms

- **WHEN** `agent-runner -profile copilot my-workflow` or `agent-runner --profile=copilot my-workflow`
  is invoked
- **THEN** the invocation behaves identically to `agent-runner --profile copilot my-workflow`

#### Scenario: Flag appears in usage text

- **WHEN** the CLI prints its usage text
- **THEN** the output lists `--profile <name>` with a description stating that it selects the profile
  set for this invocation

#### Scenario: Flag omitted

- **WHEN** `agent-runner my-workflow` is invoked with no `--profile` flag
- **THEN** profile-set selection is unchanged from existing behavior (`active_profile` when set,
  otherwise `default`)

### Requirement: Profile flag value validation

The CLI SHALL reject a `--profile` value that is empty or contains only whitespace, with an error
stating that `--profile` requires a profile set name. Leading and trailing whitespace SHALL be trimmed
from an otherwise non-empty value before it is used for selection.

#### Scenario: Empty value

- **WHEN** `agent-runner --profile "" my-workflow` is invoked
- **THEN** the CLI exits non-zero with an error stating that `--profile` requires a profile set name,
  and no run starts

#### Scenario: Whitespace-only value

- **WHEN** `agent-runner --profile "   " my-workflow` is invoked
- **THEN** the CLI exits non-zero with an error stating that `--profile` requires a profile set name,
  and no run starts

#### Scenario: Surrounding whitespace trimmed

- **WHEN** `agent-runner --profile " copilot " my-workflow` is invoked and a `copilot` profile set
  exists
- **THEN** the run uses the `copilot` profile set

### Requirement: Unknown profile set fails fast

When `--profile` names a profile set that does not exist in the merged config, the CLI SHALL exit
non-zero with an error that names the `--profile` flag, the requested name, and the available profile
set names in sorted order. The failure SHALL occur before any step executes and before the TUI is
launched, and SHALL NOT create a run or write a state file.

#### Scenario: Requested profile set does not exist

- **WHEN** `agent-runner --profile missing my-workflow` is invoked and the merged config defines profile
  sets `default` and `copilot`
- **THEN** the CLI exits non-zero with an error naming `--profile`, the value `missing`, and the
  available sets `copilot, default`, no step executes, and no TUI is launched

#### Scenario: No run artifacts created on failure

- **WHEN** an invocation fails because `--profile` named a nonexistent profile set
- **THEN** no state file, run directory, or audit log entry is created for that invocation

#### Scenario: Failed resume leaves existing run state intact

- **WHEN** `agent-runner --resume --profile missing` is invoked for an interrupted run and no `missing`
  profile set exists
- **THEN** the CLI exits non-zero with the unknown-profile error, no step executes, and the run's existing
  state file is unchanged, including its recorded profile set

### Requirement: Profile flag combinations

`--profile` SHALL be accepted together with `--validate`, `--resume`, `--reset-onboarding`, and
`--onboarding-from`. `--profile` SHALL be rejected together with `--list` or `--inspect`, which display
existing runs and resolve no agents, with an error stating that the flags are mutually exclusive.

#### Scenario: Combined with validate

- **WHEN** `agent-runner --validate --profile copilot my-workflow` is invoked
- **THEN** the flag combination is accepted and validation proceeds against the `copilot` profile set

#### Scenario: Combined with resume and a session ID

- **WHEN** `agent-runner --resume <session-id> --profile copilot` is invoked
- **THEN** the flag combination is accepted and the resumed run applies the override

#### Scenario: Combined with list

- **WHEN** `agent-runner --list --profile copilot` is invoked
- **THEN** the CLI exits non-zero with an error stating that `--profile` and `--list` are mutually
  exclusive

#### Scenario: Combined with inspect

- **WHEN** `agent-runner --inspect run-123 --profile copilot` is invoked
- **THEN** the CLI exits non-zero with an error stating that `--profile` and `--inspect` are mutually
  exclusive

### Requirement: Override survives run selection on a bare resume

`agent-runner --resume` with no session ID opens the run list so the user can choose a run, then continues
that run in a fresh process. When `--profile` accompanies a bare `--resume`, the override SHALL be carried
through run selection and applied to the run that is chosen, rather than being dropped when the new process
starts. Selecting a run from the list in any other way (without a `--profile` override) SHALL be unaffected.

#### Scenario: Bare resume with an override applies it to the selected run

- **WHEN** `agent-runner --resume --profile copilot` is invoked with no session ID, the run list opens, and the
  user selects an interrupted run
- **THEN** the selected run resumes with the `copilot` profile set applied to its remaining steps

#### Scenario: Bare resume without an override

- **WHEN** `agent-runner --resume` is invoked with no session ID and no `--profile` flag, and a run is selected
  from the list
- **THEN** the selected run resumes with the profile set recorded in its state file, unchanged from existing
  behavior

### Requirement: Resume validates the profile before any interactive setup

On a resume invocation, the effective profile set (the `--profile` override when supplied, otherwise the
profile set recorded in the run's state file) SHALL be validated against the merged config before any
interactive or terminal setup occurs, including theme selection and run-view initialization, and before the
run's state file is rewritten. An unknown profile set SHALL therefore surface as a plain error on the
terminal, never after a theme prompt or a partially drawn TUI.

#### Scenario: Unknown override on resume reports before theme setup

- **WHEN** `agent-runner --resume <session-id> --profile missing` is invoked in a session that has not yet
  selected a theme, and no `missing` profile set exists
- **THEN** the CLI prints the unknown-profile error and exits non-zero without presenting a theme prompt or
  launching the run view

#### Scenario: Unknown recorded profile set on resume reports before theme setup

- **WHEN** `agent-runner --resume <session-id>` is invoked with no `--profile` flag, the run's state file records
  a profile set that no longer exists, and the session has not yet selected a theme
- **THEN** the CLI prints the error naming the recorded profile set and exits non-zero without presenting a
  theme prompt or launching the run view

### Requirement: Profile flag does not modify configuration

An invocation that passes `--profile` SHALL NOT write, create, or modify any config file. In
particular, it SHALL NOT set or change `active_profile` in the project config.

#### Scenario: Config untouched after an overridden run

- **WHEN** a project config sets `active_profile: work` and `agent-runner --profile copilot my-workflow`
  runs to completion
- **THEN** the project config still contains `active_profile: work`, byte-for-byte unchanged

#### Scenario: Subsequent run without the flag

- **WHEN** `agent-runner --profile copilot my-workflow` completes and `agent-runner my-workflow` is then
  invoked in the same project whose config sets `active_profile: work`
- **THEN** the second run uses the `work` profile set
