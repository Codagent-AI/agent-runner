# PTY POC

This document describes the interactive PTY proof of concept in `cmd/pty-poc`.

## Goal

The POC answers one narrow question:

- can Agent Runner host Claude or Codex inside a PTY
- let the user interact with the agent normally
- reserve a host-side escape for "continue"
- shut the agent down cleanly enough to return control to Agent Runner

It is intentionally not integrated into the real workflow engine yet. It is a standalone experiment.

## Files

- `cmd/pty-poc/main.go`: main event loop, PTY lifecycle, host controls, agent launch, signal file write, logging, escape sequence parser
- `cmd/pty-poc/terminal.go`: raw terminal mode detection and restore

## How To Run

From the repository root:

```bash
go run ./cmd/pty-poc
```

## High-Level Model

The POC is a terminal proxy.

- Agent Runner owns the real terminal
- Agent Runner creates a PTY
- The agent (Claude or Codex) runs attached to the PTY slave
- Agent Runner reads stdin from the user
- Agent Runner forwards most bytes to the agent through the PTY master
- Agent Runner forwards PTY output directly to stdout

This means Agent Runner is not rendering a fake agent UI. The agent still emits its normal terminal control sequences and the real terminal emulator still renders them.

## Control Model

### Home Screen

When the POC starts, it shows a simple home screen with three controls:

- `c`: launch Claude in a PTY
- `x`: launch Codex in a PTY
- `esc` or `Ctrl-C`: exit the POC

### Inside an Agent

While an agent is running:

- `/next` (typed and submitted with Enter): reserved by Agent Runner as "continue"
- `Ctrl-]`: reserved by Agent Runner as "continue" (keyboard shortcut alternative)
- all other input is forwarded to the agent (including `Ctrl-C`)

The `Ctrl-]` shortcut also matches enhanced keyboard protocol encodings:

- `\x1b[93;5u`
- `\x1b[27;5;93~`

### Normal Agent Exit

If the user exits the agent normally (e.g. `/exit` in Claude, `/quit` in Codex), the POC exits entirely. This matches user intent — they chose to leave the agent.

## Agent Launch

### Claude

Claude is launched as:

```text
claude
```

This starts Claude Code in interactive mode. The user types their own prompt.

### Codex

Codex is launched as:

```text
codex --no-alt-screen <prompt>
```

`--no-alt-screen` keeps the terminal behavior simpler during the POC and makes debugging easier.

If no command-line arguments are provided, Codex uses this default prompt:

```text
You are running inside an Agent Runner PTY proof of concept. Keep responses short.
```

## Continue Behavior

When the user types `/next` and presses Enter (or presses `Ctrl-]`), the POC does the following:

1. writes `.agent-runner-signal` in the current working directory with JSON payload `{"action":"continue"}`
2. sets internal state `pendingContinue = true`
3. sends `SIGTERM` to the child process
4. when the agent exits, restores terminal modes and returns to the home screen

The `/next` command is intercepted by the host before the Enter byte reaches the agent. The agent sees `/next` being typed in its input field but never receives the submission. Then SIGTERM terminates it.

No agent-runner messages are printed to stdout while an agent is running. The PTY proxy is invisible to both the user and the agent.

## `/next` Interception

The POC tracks user input in a line buffer to detect `/next`:

- Each stdin chunk is processed byte-by-byte to update the line buffer and the escape sequence parser state
- Printable ASCII bytes (0x20-0x7e) are appended to the buffer
- Backspace (0x7f/0x08) removes the last character
- Ctrl-U (0x15) clears the buffer
- Enter (0x0d/0x0a) checks the buffer against `/next` and resets it
- Escape sequences are consumed by the state machine without touching the buffer

Bytes are accumulated and flushed to the PTY in batches to preserve original chunk boundaries. Writing byte-by-byte would break escape sequences because the receiving application may interpret a lone `\x1b` as a standalone Escape keypress.

## Escape Sequence Parser

Terminal escape sequences (CSI, OSC, DCS, PM, APC, SOS) arrive on stdin as responses to agent queries (e.g. terminal capabilities, color values). These must be consumed without polluting the line buffer.

The parser is a state machine with four states:

- `escNone`: normal input processing
- `escSawEsc`: saw `\x1b`, waiting for the next byte to determine sequence type
- `escInCSI`: inside a CSI sequence (`\x1b[`), waiting for a final byte (0x40-0x7e)
- `escInStringSeq`: inside a string sequence (`\x1b]`, `\x1bP`, `\x1b^`, `\x1b_`, `\x1bX`), waiting for BEL (0x07) or ST (`\x1b\`)

State persists across stdin read calls so sequences split across chunks are handled correctly.

## Exit Behavior

The POC can exit in several ways:

- `esc` or `Ctrl-C` on the home screen exits the POC
- normal agent exit (user types `/exit`, `/quit`, etc.) exits the POC
- `SIGINT` exits with status 130
- `SIGTERM` exits with status 143
- EOF on stdin exits cleanly

When shutting down, the POC:

- sets `shuttingDown = true`
- sends `SIGTERM` to the child if still running
- closes the PTY
- restores terminal feature flags
- restores the original terminal mode captured before raw mode was enabled
- closes the debug log

This explicit restore path is important because `os.Exit` bypasses deferred cleanup.

## Terminal Mode Handling

The POC puts stdin into raw mode using `golang.org/x/sys/unix` in `terminal.go`.

Raw mode matters because the host needs direct access to control-key input like `Ctrl-]` instead of waiting for line-buffered shell input.

The raw-mode implementation disables:

- canonical input processing
- echo
- signal generation in the kernel
- output post-processing

Before exiting, the POC restores the original termios state that was captured at startup.

## PTY Handling

The POC uses `github.com/creack/pty`.

Behavior:

- resolves the agent binary from `PATH`
- sets a fallback `TERM=xterm-256color` if `TERM` is missing
- reads the current terminal size
- starts the agent with `pty.StartWithSize(...)` when the size is available
- falls back to `pty.Start(...)` if size lookup fails
- forwards `SIGWINCH` resizes to the PTY with `pty.Setsize(...)`

This keeps the agent TUI aligned with the user terminal size.

## Logging

The POC writes a debug log to:

```text
pty-poc.log
```

The log includes:

- startup
- PTY launch and exit
- raw stdin chunks
- stdin chunks while in agent mode
- line buffer state on Enter
- continue trigger matches (both `/next` and `Ctrl-]`)

Input chunks are logged in both hex and JSON-escaped text form. This is useful when debugging how a specific terminal encodes shortcuts.

## Signal File

The POC writes:

```text
.agent-runner-signal
```

Current payload:

```json
{"action":"continue"}
```

This mirrors the kind of "continue" handoff the real workflow runner would eventually need.

## Main State Variables

The main runtime state in `main.go` is:

- `activePTY`: currently running PTY handle
- `activeCmd`: currently running agent process
- `mode`: `"home"`, `"claude"`, or `"codex"`
- `shuttingDown`: process is exiting
- `pendingContinue`: agent exit should be interpreted as continue, not ordinary child termination
- `logFile`: debug log handle
- `lineBuffer`: tracks user-typed characters on the current input line
- `escState`: current escape sequence parser state
- `hintTimer`: timer that draws an idle hint after PTY output goes silent

The state is guarded with a mutex because PTY exit, signal handling, and stdin processing all run concurrently.

## Event Flow

### Startup

1. verify stdin and stdout are both TTYs
2. create the debug log
3. switch stdin to raw mode
4. install signal handlers
5. install resize handler
6. render the home screen
7. begin reading stdin

### Launch Agent

1. remove any previous `.agent-runner-signal`
2. set mode to `"claude"` or `"codex"`
3. reset line buffer and escape state
4. clear the screen
5. spawn the agent in a PTY
6. forward PTY output to stdout with idle hint timer
7. wait for agent exit in a goroutine

### Continue

1. detect `/next` + Enter or `Ctrl-]`
2. write `.agent-runner-signal`
3. mark `pendingContinue`
4. send SIGTERM to the child process
5. when agent exits, restore terminal modes and render home

### Normal Exit

1. agent exits on its own (user typed `/exit`, `/quit`, etc.)
2. `pendingContinue` is false
3. POC calls `cleanupAndExit(0)` — full exit, not return to home

## Ctrl-C Handling

`Ctrl-C` behavior differs by mode:

- **Home screen**: `Ctrl-C` exits the POC (same as `esc`)
- **Inside an agent**: `Ctrl-C` is forwarded to the agent (Claude uses it to interrupt generation, Codex uses `Esc`)

Only `/next` and `Ctrl-]` are reserved by the host in agent mode.

## Idle Hint

When an agent is running and its PTY output goes silent for 800ms, the POC draws a dim hint bar on the bottom row of the terminal:

```
 /next or Ctrl-] to continue to next step
```

The hint is rendered with dim + reverse video (`\x1b[2;7m`) and spans the full terminal width. Cursor position is saved before drawing and restored after, so the agent's display is not disturbed.

The hint disappears naturally when the agent produces more output — no explicit erase is needed. The timer resets on every PTY output chunk, so the hint only appears during genuine idle periods (e.g. when the agent is waiting for user input).

The hint timer is cancelled on continue, agent exit, and POC shutdown.

## What This POC Proves

This POC successfully demonstrates that Agent Runner can:

- host Claude or Codex inside a PTY
- pass agent terminal output straight through
- intercept a host-owned command (`/next`) from the user's input stream
- intercept a host-owned keyboard shortcut (`Ctrl-]`)
- parse and skip terminal escape sequences without corrupting command detection
- write a continue signal file
- resize the PTY with the terminal
- recover terminal state on shutdown
- support multiple agent backends from the same host
- show a non-intrusive idle hint without conflicting with the agent's TUI
