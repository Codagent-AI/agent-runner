## Why

In interactive mode, only the user can trigger step transitions (via `/next` or Ctrl-]). An agent that finishes its work has no way to signal "I'm done" — it sits idle until the user notices and manually advances. This blocks autonomous multi-step workflows where the agent should be able to complete a step and move on without human intervention.

## What Changes

- Add agent-initiated continue trigger: the PTY output path detects a sentinel sequence emitted by the agent's stdout and triggers the same continue logic as `/next`
- Introduce an output processor that scans PTY output for the sentinel, strips it before forwarding to the terminal, and fires `tryTriggerContinue()`
- The sentinel uses an OSC private-use escape sequence (`\x1b]999;red-slippers\x07`) so it cannot collide with normal terminal output and is harmless if leaked

## Capabilities

### New Capabilities

- `agent-continue-trigger`: Agent-initiated continue trigger via stdout sentinel detection. Covers sentinel format, output scanning, stripping from display, and integration with the existing continue/termination flow.

### Modified Capabilities

- `pseudo-terminal`: The PTY spec's continue trigger requirements expand to include agent-originated triggers in addition to user-originated triggers. The graceful termination and escape sequence handling requirements also apply to the new trigger source.

## Out of Scope

- Changing the headless execution path — headless steps already run to completion naturally
- Adding bidirectional communication channels between runner and agent beyond the sentinel
- Allowing the agent to pass data (e.g., variables, status codes) through the sentinel — this is a simple "continue" signal only
- Modifications to workflow step definitions or YAML schema

## Impact

- **`internal/pty/pty.go`**: `forwardOutput` gains access to state, cmd, and exitCh; calls new output processor instead of writing directly to stdout
- **`internal/pty/` (new file)**: Output processor analogous to `input.go` — buffers output to detect sentinel across chunk boundaries, strips sentinel bytes, forwards clean output
- **`internal/pty/pty.go`**: `RunInteractive` wires the output processor into the PTY goroutines
- **`internal/exec/agent.go`**: The runner automatically appends sentinel instructions to interactive step prompts — zero workflow-author effort
- **Existing behavior preserved**: User-initiated `/next` and Ctrl-] continue to work unchanged
