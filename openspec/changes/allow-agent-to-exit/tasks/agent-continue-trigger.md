# Task: Add Agent-Initiated Continue Trigger

## Goal

Enable agents to signal task completion by emitting a sentinel escape sequence in stdout, triggering the same continue and termination protocol as the user-initiated `/next`. This is the full vertical slice: output processor, PTY wiring, prompt injection, and tests.

## Background

You MUST read these files before starting:
- `openspec/changes/allow-agent-to-exit/design.md` — full design details (see "Approach" section for architecture, "Decisions" table for rationale)
- `openspec/changes/allow-agent-to-exit/specs/agent-continue-trigger/spec.md` — "Requirement: Agent-initiated continue via stdout sentinel" and "Requirement: Sentinel instruction injection" for acceptance criteria
- `openspec/changes/allow-agent-to-exit/specs/pseudo-terminal/spec.md` — "Requirement: Continue triggers" (modified) for updated trigger requirements

**Key files to study:**
- `internal/pty/input.go` — the existing `inputProcessor` is the direct template for the new output processor. Study its state machine and `processResult` structure.
- `internal/pty/pty.go` — `forwardOutput` (output path to modify) and `processStdin` (reference for trigger + termination logic)
- `internal/exec/agent.go` — `ExecuteAgentStep` where prompt injection should happen
- `internal/pty/input_test.go` and `internal/exec/agent_test.go` — test conventions to follow

**What to build:**
1. An `outputProcessor` in a new file `internal/pty/output.go` — a byte-by-byte state machine that detects the sentinel OSC sequence `\x1b]999;red-slippers\x07` in the output stream, strips it, and signals a trigger. Must handle chunk boundary splits via persistent state. Non-matching OSC sequences and partial sentinels at EOF must be flushed as normal output.
2. Wire the output processor into `forwardOutput`, giving it access to the same termination infrastructure as `processStdin` so it can fire the continue protocol on detection.
3. Auto-inject sentinel instructions into interactive step prompts (not headless) in `ExecuteAgentStep`.

## Spec

### Requirement: Agent-initiated continue via stdout sentinel

The PTY layer SHALL detect a designated sentinel escape sequence in the agent's stdout output. When detected, the sentinel SHALL be stripped from the output (never forwarded to the terminal) and the runner SHALL trigger the existing continue and termination protocol, advancing to the next workflow step with outcome success — identical to a user-initiated `/next`.

#### Scenario: Agent emits sentinel
- **WHEN** the agent writes the sentinel sequence to stdout during an interactive step
- **THEN** the runner triggers continue and advances to the next step with outcome success

#### Scenario: Sentinel stripped from output
- **WHEN** the agent writes the sentinel sequence to stdout
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
- **THEN** the prompt includes an instruction telling the agent how to emit the sentinel

#### Scenario: Headless step does not receive sentinel instruction
- **WHEN** the runner builds the prompt for a headless agent step
- **THEN** the prompt does not include the sentinel instruction

## Done When

All eight spec scenarios above are covered by tests and passing. The output processor correctly detects, strips, and triggers on the sentinel (including edge cases: chunk splits, partial at EOF, non-matching OSC). Interactive step prompts include the sentinel instruction; headless prompts do not. All existing tests continue to pass.
