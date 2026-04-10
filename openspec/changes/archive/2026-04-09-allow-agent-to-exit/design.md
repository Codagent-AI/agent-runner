## Context

Interactive agent steps can only advance to the next workflow step via user-initiated triggers (`/next` or Ctrl-]). The agent has no mechanism to signal "I'm done" — it sits idle until the user manually advances. This blocks autonomous multi-step workflows.

The PTY layer (`internal/pty/`) already has the infrastructure for continue triggers: `tryTriggerContinue` for atomic state transitions, SIGTERM/SIGKILL cascade for graceful termination, and a byte-by-byte input processor that detects triggers in stdin. This design adds a parallel detection path in the output stream.

## Goals / Non-Goals

**Goals:**
- Allow agents to signal task completion by emitting a sentinel sequence in stdout
- Trigger the existing continue and termination protocol when the sentinel is detected
- Strip the sentinel from terminal output so users never see it
- Automatically inject sentinel instructions into interactive step prompts

**Non-Goals:**
- Changing the headless execution path (headless steps already run to completion)
- Bidirectional communication between runner and agent beyond the continue signal
- Allowing the agent to pass data or status codes through the sentinel
- Modifying workflow YAML schema

## Approach

```
Agent stdout → PTY master → forwardOutput → outputProcessor.process(chunk)
                                              │
                                  ┌───────────┴───────────┐
                                  │                       │
                            Normal bytes            Sentinel detected
                                  │                       │
                            Forward to              Strip from output
                            os.Stdout               tryTriggerContinue()
                                                    SIGTERM → SIGKILL
```

### Sentinel format

OSC private-use escape sequence: `\x1b]999;red-slippers\x07` (21 bytes).

Terminals silently ignore unknown OSC sequences, so the sentinel is harmless if leaked. The `999` prefix is in the private-use range (not assigned by any standard). The payload `red-slippers` is unique enough to avoid collisions with any real terminal protocol.

### Output processor (`internal/pty/output.go`)

New file containing `outputProcessor` — a byte-by-byte state machine mirroring the existing `inputProcessor` in `input.go`.

Tracks escape sequence state (ESC, OSC). When inside an OSC sequence, accumulates the payload into a buffer. On BEL termination:
- If payload matches `999;red-slippers`: strip the sentinel bytes, return `triggered=true`
- If payload does not match: flush the accumulated OSC bytes to the output as normal terminal data

State persists across `process()` calls, so a sentinel split across PTY read chunk boundaries is detected correctly.

Returns an `outputResult` with:
- `forward []byte` — bytes to write to `os.Stdout`
- `triggered bool` — whether the sentinel was detected

### forwardOutput changes (`internal/pty/pty.go`)

Signature expands to accept `cmd *exec.Cmd`, `state *ptyState`, and `exitCh chan struct{}` (same parameters as `processStdin`).

The read loop feeds each chunk through `outputProcessor.process()`. Normal output is forwarded to `os.Stdout`. On trigger, the function runs the SIGTERM + SIGKILL timeout logic (inlined, mirroring `processStdin`):

1. Call `state.tryTriggerContinue()` — if it returns false (process already exited), return
2. Cancel the idle hint
3. Send SIGTERM to the child process
4. Spawn a goroutine that sends SIGKILL after `killTimeout` unless `exitCh` closes first

### Prompt injection (`internal/exec/agent.go`)

In `ExecuteAgentStep`, after building the prompt and enrichment, append the sentinel instruction for interactive steps:

```
When you have completed your task, signal completion by running this command in the terminal:
printf '\x1b]999;red-slippers\x07'
```

This is appended unconditionally for all interactive steps. The agent always knows how to signal completion. Users can still use `/next` or Ctrl-] — the sentinel is additive.

## Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Sentinel format | OSC `\x1b]999;red-slippers\x07` | Private-use range, terminals ignore unknown OSC, cannot collide with normal output |
| Output processor architecture | Byte-by-byte state machine | Mirrors existing `inputProcessor` pattern, handles chunk splits naturally via persistent state, structurally aware of escape sequences |
| Agent instruction delivery | Runner auto-injects into prompt | Zero workflow-author effort, consistent behavior across all interactive steps |
| Trigger wiring | Pass state/cmd/exitCh as parameters to forwardOutput | Same pattern as `processStdin`, straightforward |
| Trigger logic sharing | Inline in both processStdin and forwardOutput | Two call sites does not justify extraction; revisit if a third appears |

## Risks / Trade-offs

- **[Agent ignores instruction]** → The agent might not emit the sentinel. Mitigation: user can still `/next` manually — this is additive, not replacing existing triggers.
- **[Duplicated SIGTERM/SIGKILL logic]** → Both `processStdin` and `forwardOutput` have the same ~8 lines of trigger code. Acceptable duplication for two call sites; extract to a shared helper if a third caller appears.
- **[Prompt bloat]** → Every interactive step gets the sentinel instruction appended (~2 sentences). Negligible token cost.
- **[Sentinel in agent reasoning output]** → The agent could theoretically print the sentinel while explaining what it does rather than executing it. Mitigation: the OSC escape uses non-printable bytes that won't appear in explanatory text — only in a deliberate `printf`.

## Open Questions

None — all architectural decisions are resolved.
