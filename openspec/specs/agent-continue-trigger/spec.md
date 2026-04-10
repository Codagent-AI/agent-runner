### Requirement: Agent-initiated continue via PTY sentinel

The PTY layer SHALL detect a designated sentinel escape sequence in the PTY output stream. The agent writes the sentinel to the PTY slave device (via the `AGENT_RUNNER_TTY` environment variable) so that the sequence reaches the PTY output scanner even when the agent's stdout is captured by a pipe. When detected, the sentinel SHALL be stripped from the output (never forwarded to the terminal) and the runner SHALL trigger the existing continue and termination protocol, advancing to the next workflow step with outcome success — identical to a user-initiated `/next`.

#### Scenario: Agent emits sentinel
- **WHEN** the agent writes the sentinel sequence to the PTY device during an interactive step
- **THEN** the runner triggers continue and advances to the next step with outcome success

#### Scenario: Sentinel stripped from output
- **WHEN** the agent writes the sentinel sequence to the PTY device
- **THEN** the sentinel bytes are not displayed on the user's terminal

#### Scenario: Sentinel embedded in other output
- **WHEN** the agent writes the sentinel surrounded by other output bytes in the same write
- **THEN** only the sentinel is stripped; surrounding output is forwarded to the terminal normally

#### Scenario: Sentinel detection across chunk boundaries
- **WHEN** the sentinel sequence arrives split across PTY read chunks
- **THEN** the output processor detects and strips the sentinel

#### Scenario: Incomplete sentinel at process exit
- **WHEN** the agent writes a partial sentinel sequence and the process exits before the sequence is completed
- **THEN** the buffered partial bytes are flushed to the terminal as normal output

#### Scenario: Non-matching OSC sequence passed through
- **WHEN** the agent writes an OSC sequence that does not match the sentinel payload
- **THEN** the entire OSC sequence is forwarded to the terminal as normal output

### Requirement: Sentinel instruction injection

The runner SHALL automatically append sentinel completion instructions to the prompt for all interactive agent steps. Headless steps SHALL NOT receive the instruction.

#### Scenario: Interactive step receives sentinel instruction
- **WHEN** the runner builds the prompt for an interactive agent step
- **THEN** the prompt includes an instruction telling the agent how to write the sentinel to `$AGENT_RUNNER_TTY`

#### Scenario: Headless step does not receive sentinel instruction
- **WHEN** the runner builds the prompt for a headless agent step
- **THEN** the prompt does not include the sentinel instruction
