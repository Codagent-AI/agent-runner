### Requirement: Agent-initiated continue via PTY sentinel

The PTY layer SHALL detect a designated sentinel in the PTY output stream. The default agent-facing sentinel is a fresh plain text marker for each interactive step attempt, with prefix `AGENT_RUNNER_CONTINUE_`, emitted as the only content on a line in the agent's final reply so it does not require shell command approval in hosted CLIs. The runner SHALL only accept the marker generated for the current PTY session, so replayed transcript output from earlier attempts cannot advance a later step. Because hosted CLIs may render final assistant messages with prompts, bullets, cursor movement, and ANSI styling, the current text sentinel SHALL also trigger when it appears as a standalone visible token after a token boundary, including when it is terminated by either a line break or terminal control sequence. The legacy OSC escape sentinel SHALL remain supported for compatibility. When detected, the sentinel SHALL be stripped from the output (never forwarded to the terminal) and the runner SHALL trigger the existing continue and termination protocol, advancing to the next workflow step with outcome success -- identical to a user-initiated `/next`.

#### Scenario: Agent emits sentinel
- **WHEN** the agent emits the sentinel during an interactive step
- **THEN** the runner triggers continue and advances to the next step with outcome success

#### Scenario: Sentinel stripped from output
- **WHEN** the agent emits the sentinel
- **THEN** the sentinel bytes are not displayed on the user's terminal

#### Scenario: Sentinel embedded in other output
- **WHEN** the agent emits the sentinel surrounded by other output bytes in the same write
- **THEN** only the sentinel is stripped; surrounding output is forwarded to the terminal normally

#### Scenario: Sentinel detection across chunk boundaries
- **WHEN** the sentinel sequence arrives split across PTY read chunks
- **THEN** the output processor detects and strips the sentinel

#### Scenario: Stale text sentinel ignored
- **WHEN** PTY output contains a text sentinel generated for a previous interactive step attempt
- **THEN** the runner forwards it as normal output and does not trigger continuation

#### Scenario: Incomplete sentinel at process exit
- **WHEN** the agent emits a partial sentinel sequence and the process exits before the sequence is completed
- **THEN** the buffered partial bytes are flushed to the terminal as normal output

#### Scenario: Non-matching OSC sequence passed through
- **WHEN** the agent writes an OSC sequence that does not match the sentinel payload
- **THEN** the entire OSC sequence is forwarded to the terminal as normal output

### Requirement: Sentinel instruction injection

The runner SHALL automatically append sentinel completion instructions to the prompt for all interactive agent steps. Headless steps SHALL NOT receive the instruction.

#### Scenario: Interactive step receives sentinel instruction
- **WHEN** the runner builds the prompt for an interactive agent step
- **THEN** the prompt includes an instruction telling the agent how to emit that step attempt's text sentinel without running a shell command
- **AND** the prompt includes the marker prefix and suffix separately, not the exact sentinel marker contiguously

#### Scenario: Headless step does not receive sentinel instruction
- **WHEN** the runner builds the prompt for a headless agent step
- **THEN** the prompt does not include the sentinel instruction
