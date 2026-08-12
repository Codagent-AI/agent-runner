## ADDED Requirements

### Requirement: Intake workflow embedded under core

The builtin set SHALL include an intake workflow in the `core` namespace, resolvable as `core:intake`. It SHALL declare `hidden: true`, because the new tab already surfaces it through a dedicated "Plan with an agent" entry and listing it a second time as an ordinary workflow row would duplicate the same action. Per existing hidden-workflow behavior, it SHALL remain runnable from the CLI and SHALL appear in the browser when the show-hidden toggle is on.

#### Scenario: Intake workflow runnable by namespace
- **WHEN** the user runs `agent-runner core:intake` in an interactive terminal
- **THEN** the intake workflow loads from the embedded `core` namespace and executes

#### Scenario: Intake workflow omitted from the browser by default
- **WHEN** the new tab renders with the show-hidden toggle in its default "off" state
- **THEN** no ordinary workflow row for `core:intake` appears in the Core group
- **AND** the dedicated "Plan with an agent" entry is still present

#### Scenario: Intake workflow visible under the hidden toggle
- **WHEN** the user presses `h` to show hidden workflows
- **THEN** a row for `core:intake` appears in the Core group alongside the other core workflows
