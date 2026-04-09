## MODIFIED Requirements

### Requirement: Continue triggers

The PTY layer SHALL intercept continue triggers from two sources: user input (`/next` typed on a line followed by Enter, or a keyboard shortcut) and agent output (a designated sentinel escape sequence). When any trigger is detected, the runner signals the CLI to terminate and advances to the next workflow step.

#### Scenario: /next typed
- **WHEN** the user types `/next` and presses Enter during an interactive step
- **THEN** the runner terminates the CLI process and advances to the next step with outcome success

#### Scenario: Keyboard shortcut pressed
- **WHEN** the user presses the continue keyboard shortcut during an interactive step
- **THEN** the runner terminates the CLI process and advances to the next step with outcome success

#### Scenario: Continue trigger not forwarded to CLI
- **WHEN** the user types `/next` or presses the continue keyboard shortcut
- **THEN** the typed bytes are intercepted by the PTY layer and not delivered to the CLI process as input

#### Scenario: Agent emits sentinel
- **WHEN** the agent writes the designated sentinel escape sequence to stdout during an interactive step
- **THEN** the runner terminates the CLI process and advances to the next step with outcome success

#### Scenario: Agent sentinel not forwarded
- **WHEN** the agent writes the designated sentinel escape sequence to stdout
- **THEN** the sentinel bytes are stripped from the output and not displayed on the user's terminal
