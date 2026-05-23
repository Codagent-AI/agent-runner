## ADDED Requirements

### Requirement: Autonomous permission mode selection during setup

After the autonomous backend selection step, native setup SHALL present an "Autonomous Permission Mode" selection screen. The screen SHALL display the two `autonomous_permission_mode` options — Conservative and YOLO — each with explanatory copy. The `Conservative` option SHALL be pre-selected as the recommended default. The `YOLO` option SHALL additionally display risk copy. The selected value SHALL be written to `~/.agent-runner/settings.yaml` as `autonomous_permission_mode` when setup completes successfully.

The Permission Mode screen SHALL appear before the skill installation step. Cancellation on this screen SHALL be treated identically to cancellation on the Autonomous Backend screen: no permission-mode value is persisted, no setup completion is recorded, and native setup SHALL be offered again on the next eligible launch.

#### Scenario: Permission mode screen appears after autonomous backend selection

- **WHEN** the user completes the Autonomous Backend selection step of native setup
- **THEN** the setup presents an Autonomous Permission Mode selection screen before the skill installation step

#### Scenario: Conservative is pre-selected

- **WHEN** the Autonomous Permission Mode selection screen is presented
- **THEN** the `Conservative` option is pre-selected

#### Scenario: Each option has explanatory copy

- **WHEN** the Autonomous Permission Mode selection screen is presented
- **THEN** each of the two options displays explanatory copy

#### Scenario: YOLO option shows risk copy

- **WHEN** the Autonomous Permission Mode selection screen is presented
- **THEN** the `YOLO` option additionally displays risk copy

#### Scenario: Selected permission mode is persisted on setup completion

- **WHEN** the user selects an Autonomous Permission Mode value and setup completes successfully
- **THEN** `~/.agent-runner/settings.yaml` contains the selected `autonomous_permission_mode` value

#### Scenario: Cancelled setup does not persist permission mode

- **WHEN** the user selects an Autonomous Permission Mode value but cancels setup before completion
- **THEN** `~/.agent-runner/settings.yaml` does not contain an `autonomous_permission_mode` key from this setup attempt
