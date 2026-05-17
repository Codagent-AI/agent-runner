## ADDED Requirements

### Requirement: Autonomous backend selection during setup

After the implementor CLI selection step (where the programmatic billing disclosure is shown), native setup SHALL present an "Autonomous Backend" selection screen. The screen SHALL display the three `autonomous_backend` options — Headless, Interactive, and Interactive for Claude — each with a one-sentence explanation of what the option means. The `Interactive for Claude` option SHALL be pre-selected as the recommended default. The selected value SHALL be written to `~/.agent-runner/settings.yaml` as `autonomous_backend` when setup completes successfully.

#### Scenario: Autonomous backend screen appears after implementor CLI selection
- **WHEN** the user completes the implementor CLI selection step of native setup
- **THEN** the setup presents an autonomous backend selection screen before proceeding to the next setup step

#### Scenario: Interactive for Claude is pre-selected
- **WHEN** the autonomous backend selection screen is presented
- **THEN** the `Interactive for Claude` option is pre-selected

#### Scenario: Each option has an explanation
- **WHEN** the autonomous backend selection screen is presented
- **THEN** each of the three options displays a one-sentence explanation of what the option means

#### Scenario: Selected backend is persisted on setup completion
- **WHEN** the user selects an autonomous backend value and setup completes successfully
- **THEN** `~/.agent-runner/settings.yaml` contains the selected `autonomous_backend` value

#### Scenario: Cancelled setup does not persist backend
- **WHEN** the user selects an autonomous backend value but cancels setup before completion
- **THEN** `~/.agent-runner/settings.yaml` does not contain an `autonomous_backend` key from this setup attempt
