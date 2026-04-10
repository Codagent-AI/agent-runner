### Requirement: PTY hosting for interactive steps

Interactive agent steps SHALL be executed inside a pseudo-terminal. The runner creates a PTY, attaches the CLI process to the PTY slave, and proxies I/O between the user's terminal and the PTY master. Headless steps SHALL NOT use a PTY.

#### Scenario: Interactive step launches in PTY
- **WHEN** the runner executes an interactive agent step
- **THEN** the CLI process runs inside a PTY with I/O proxied to the user's terminal

#### Scenario: Headless step does not use PTY
- **WHEN** the runner executes a headless agent step
- **THEN** the CLI process runs via direct exec without a PTY

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
- **WHEN** the agent writes the designated sentinel escape sequence to the interactive terminal stream during an interactive step
- **THEN** the runner terminates the CLI process and advances to the next step with outcome success

#### Scenario: Agent sentinel not forwarded
- **WHEN** the agent writes the designated sentinel escape sequence to the interactive terminal stream
- **THEN** the sentinel bytes are stripped from the output and not displayed on the user's terminal

### Requirement: Graceful CLI termination on continue

When a continue trigger is detected, the runner SHALL send SIGTERM to the CLI process. If the CLI does not exit within 3 seconds, the runner SHALL send SIGKILL.

#### Scenario: CLI exits promptly after SIGTERM
- **WHEN** a continue trigger fires and the CLI exits after SIGTERM
- **THEN** the runner proceeds to the next step

#### Scenario: CLI does not exit after SIGTERM
- **WHEN** a continue trigger fires and the CLI does not exit within the timeout
- **THEN** the runner sends SIGKILL and proceeds to the next step

### Requirement: Natural exit and crash handling

When the CLI exits on its own (user typed `/exit`, `quit`, etc.) or crashes, the runner SHALL stop the workflow, print a message explaining how to resume the agent-runner session, and exit.

#### Scenario: User exits CLI naturally
- **WHEN** the CLI process exits with code 0 without a continue trigger
- **THEN** the runner prints a resume message and exits (outcome: aborted)

#### Scenario: CLI crashes
- **WHEN** the CLI process exits with a non-zero exit code without a continue trigger
- **THEN** the runner prints a resume message and exits (outcome: aborted)

### Requirement: Escape sequence passthrough

The PTY layer SHALL track ANSI escape sequence state and only evaluate continue triggers outside of escape sequences. Bytes that are part of CSI, OSC, DCS, or other escape sequences SHALL be forwarded to the CLI without interpretation. Exception: escape sequences that encode a continue-trigger keyboard shortcut SHALL still be intercepted and treated as a continue trigger.

#### Scenario: Escape sequence containing trigger bytes
- **WHEN** the user's terminal sends an escape sequence that contains bytes matching `/next`
- **THEN** the bytes are forwarded to the CLI and not treated as a continue trigger

#### Scenario: Continue-trigger shortcut encoded as escape sequence
- **WHEN** the user presses a continue-trigger keyboard shortcut that the terminal encodes as an escape sequence
- **THEN** the PTY layer intercepts the sequence and treats it as a continue trigger

### Requirement: Idle hint

When the PTY has been silent (no output from the CLI) for a threshold duration, the runner SHALL display a hint indicating available continue triggers. The hint SHALL disappear when the CLI produces new output.

#### Scenario: Idle hint appears after silence
- **WHEN** the CLI produces no output for the threshold duration
- **THEN** the runner displays a continue hint on the terminal

#### Scenario: Idle hint disappears on output
- **WHEN** the idle hint is displayed and the CLI produces new output
- **THEN** the hint is removed from the terminal

### Requirement: Terminal resize propagation

The PTY layer SHALL handle terminal resize events (SIGWINCH). When the user's terminal is resized, the new dimensions SHALL be propagated to the PTY so the hosted CLI renders correctly.

#### Scenario: Terminal resized during interactive step
- **WHEN** the user resizes their terminal while a CLI is running in the PTY
- **THEN** the PTY dimensions are updated and the CLI receives the new size
