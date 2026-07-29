## ADDED Requirements

### Requirement: Breadcrumb shows a non-default profile set

When a run's resolved profile set is any name other than `default`, the run-view breadcrumb SHALL include a
`profile: <name>` segment, rendered in the same dim style and separated by the same `·` separator as the
existing recorded-version segment that follows the top-level workflow name. When the resolved profile set
is `default`, the breadcrumb SHALL be unchanged from its current form.

The rule depends only on the resolved name, not on where the selection came from, so an explicit
`--profile default` displays nothing extra.

The run view SHALL take the name from the profile set recorded in the run's state file, and SHALL render the
segment for every entry mode that displays a run, including a live run in progress. This differs from the
adjacent recorded-version segment, which is deliberately populated only when the run view is entered from
the run list or from `--inspect`; the profile segment SHALL NOT inherit that restriction.

#### Scenario: Overridden profile set shown

- **WHEN** the run view displays a run whose resolved profile set is `copilot`
- **THEN** the breadcrumb includes a dim `· profile: copilot` segment

#### Scenario: Config-selected non-default profile set shown

- **WHEN** the run view displays a run started with no `--profile` flag in a project whose config sets
  `active_profile: work`
- **THEN** the breadcrumb includes a dim `· profile: work` segment

#### Scenario: Default profile set not shown

- **WHEN** the run view displays a run whose resolved profile set is `default`
- **THEN** the breadcrumb includes no profile segment

#### Scenario: Explicit default override not shown

- **WHEN** the run view displays a run started with `--profile default`
- **THEN** the breadcrumb includes no profile segment

#### Scenario: Segment renders during a live run

- **WHEN** a run started with `--profile copilot` is in progress and its live run view is displayed
- **THEN** the breadcrumb includes the `profile: copilot` segment while the run is still executing

#### Scenario: Shell-only workflow with a non-default profile set

- **WHEN** the run view displays a run of a shell-only workflow whose resolved profile set is `copilot`
- **THEN** the breadcrumb includes the `profile: copilot` segment, since a profile set is resolved for every
  run

#### Scenario: Coexists with the recorded version segment

- **WHEN** the run view displays a run that has both a recorded workflow version and resolved profile set
  `copilot`
- **THEN** the breadcrumb shows both segments after the top-level workflow name, and neither replaces the
  other

#### Scenario: Profile segment survives navigation into nested scopes

- **WHEN** the run view for a run with resolved profile set `copilot` is navigated into a sub-workflow so the
  breadcrumb shows the nesting path
- **THEN** the `profile: copilot` segment is still present

#### Scenario: Inspecting a finished run

- **WHEN** `agent-runner --inspect <run-id>` opens a finished run whose state file records profile set
  `copilot`
- **THEN** the breadcrumb includes the `profile: copilot` segment

#### Scenario: Profile segment truncates with the breadcrumb

- **WHEN** the terminal is too narrow to render the breadcrumb and the profile segment alongside the chrome
  logo
- **THEN** the breadcrumb line truncates as it does today without wrapping or overflowing the chrome
