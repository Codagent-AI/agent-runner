# native-setup Specification

## Purpose
Define the native first-run setup flow that writes required agent profiles before optionally handing off to the onboarding demo.
## Requirements
### Requirement: Native setup trigger condition

Before entering the bare/list TUI entry point, the runner SHALL evaluate whether native setup should be offered. The native setup trigger SHALL fire when all of the following hold:
- `settings.setup.completed_at` is unset;
- both stdin and stdout are TTYs.

When any condition is false, the runner SHALL proceed to its normal entry point without modifying setup settings.

#### Scenario: Fresh first run starts native setup
- **WHEN** the user runs `agent-runner` with no command on a TTY and setup completion is unset
- **THEN** the runner opens the native setup TUI before starting the onboarding demo workflow or normal home screen

#### Scenario: Completed setup suppresses native setup
- **WHEN** `settings.setup.completed_at` is set
- **THEN** the native setup trigger does not fire

#### Scenario: Non-TTY does not start setup
- **WHEN** the runner starts with stdin or stdout connected to a pipe
- **THEN** native setup does not start and no setup settings are written

#### Scenario: Direct workflow run does not start setup
- **WHEN** the user runs `agent-runner run my-workflow` and setup completion is unset
- **THEN** native setup does not start before the direct workflow run

#### Scenario: Resume does not start setup
- **WHEN** the user runs `agent-runner --resume <id>` and setup completion is unset
- **THEN** native setup does not start before resume handling

### Requirement: Native setup is mandatory

The native setup TUI SHALL begin with a setup surface that offers progression into profile setup. It SHALL NOT offer skip, not-now, or dismiss actions. A user who cancels or interrupts setup leaves setup incomplete, and native setup SHALL be offered again on the next eligible launch.

#### Scenario: Continue enters setup
- **WHEN** the user chooses the continue action
- **THEN** the runner proceeds to the native agent profile setup flow

#### Scenario: Setup cannot be skipped
- **WHEN** native setup renders its first surface
- **THEN** no skip, not-now, or dismiss action is available

### Requirement: Native setup completion tracking

The runner SHALL write `settings.setup.completed_at` only after the native setup flow successfully finishes all required setup actions and writes the selected profile configuration.

#### Scenario: Successful setup records completion
- **WHEN** the user completes native setup and the profile configuration write succeeds
- **THEN** the runner writes `settings.setup.completed_at` with the current RFC3339 timestamp using the settings atomic-write path

#### Scenario: Cancel leaves setup incomplete
- **WHEN** the user cancels native setup before the profile configuration write
- **THEN** the runner does not write `settings.setup.completed_at`

#### Scenario: Failed write leaves setup incomplete
- **WHEN** the profile configuration write fails
- **THEN** the runner surfaces the failure and does not write `settings.setup.completed_at`

### Requirement: Interrupted setup restarts

Native setup SHALL NOT persist partially completed wizard progress. If setup is interrupted before completion, the next eligible launch SHALL start native setup from the beginning.

#### Scenario: Interrupted setup restarts from setup start
- **WHEN** the user starts native setup, makes one or more choices, exits before completion, and later starts Agent Runner on an eligible TTY
- **THEN** native setup starts again from the first setup surface

#### Scenario: Interrupted setup writes no tracking state
- **WHEN** native setup is interrupted before completion
- **THEN** the runner does not write `settings.setup.completed_at`

### Requirement: Native setup handoff to onboarding demo

After native setup reaches a terminal state, the runner SHALL continue to the appropriate next application surface. A successful setup SHALL start `onboarding:onboarding` when onboarding demo completion or dismissal has not been recorded. Cancellation, interruption, or failure SHALL transition to the normal TUI entry point without starting the demo.

#### Scenario: Successful setup starts onboarding demo
- **WHEN** native setup completes successfully and `settings.onboarding.completed_at` and `settings.onboarding.dismissed` are unset
- **THEN** the runner starts `onboarding:onboarding`

#### Scenario: Completed onboarding demo returns home
- **WHEN** the onboarding demo workflow exits successfully without an explicit app-quit request
- **THEN** the runner proceeds to the normal TUI entry point

#### Scenario: Quitting onboarding demo exits app
- **WHEN** the user explicitly quits the onboarding demo workflow with `q`, `Ctrl+C`, or confirmed top-level Escape
- **THEN** the runner exits the app instead of proceeding to the normal TUI entry point

#### Scenario: Completed onboarding demo is not repeated
- **WHEN** native setup completes successfully and `settings.onboarding.completed_at` is already set
- **THEN** the runner proceeds to the normal TUI entry point without starting `onboarding:onboarding`

#### Scenario: Dismissed onboarding demo is not repeated
- **WHEN** native setup completes successfully and `settings.onboarding.dismissed` is already set
- **THEN** the runner proceeds to the normal home TUI without starting `onboarding:onboarding`

#### Scenario: Cancelled setup goes home
- **WHEN** native setup is cancelled or fails
- **THEN** the runner proceeds to the normal home TUI without marking setup complete
