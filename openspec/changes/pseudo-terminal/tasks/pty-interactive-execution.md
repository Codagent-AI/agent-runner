# Task: PTY interactive execution

## Goal

Extract the PTY proof-of-concept into a production `internal/pty` package and wire it into the agent step executor so interactive steps run inside a pseudo-terminal with `/next` and keyboard shortcut interception. Remove the POC directory and the `/continue` skill.

## Background

Interactive agent steps currently hand off the terminal to the CLI process directly. The runner has no way to intercept user input while the CLI runs — it previously relied on a `.agent-runner-signal` file to detect when to advance. The PTY approach solves this: Agent Runner creates a pseudo-terminal, attaches the CLI to it, proxies I/O, and intercepts continue triggers at the PTY layer before they reach the CLI.

A working proof-of-concept lives at `cmd/pty-poc/` (two files: `main.go` at ~580 lines and `terminal.go` at ~40 lines). It demonstrates PTY lifecycle via `github.com/creack/pty`, raw terminal mode via `golang.org/x/sys/unix`, I/O proxying, an escape sequence state machine, `/next` and `Ctrl-]` detection, idle hint rendering, and SIGWINCH handling. The POC also includes a home screen for picking Claude vs Codex — that is POC-only and should not be extracted.

The new `internal/pty` package should expose a single entrypoint `RunInteractive` that takes a command, args, and options (environment variables), and returns a result indicating whether a continue trigger was fired and the CLI's exit code. All internal components (PTY lifecycle, I/O proxy, input processor with escape state machine, graceful termination, idle hint, resize handler, terminal restore) are unexported.

The input processor is the core of the package. It tracks ANSI escape sequence state (`escNone`/`escSawEsc`/`escInCSI`/`escInStringSeq`) and only evaluates continue triggers outside of escape sequences. It maintains a line buffer to detect `/next` followed by Enter. It detects `Ctrl-]` (byte `0x1d`) and enhanced-keyboard variants (`\x1b[93;5u`, `\x1b[27;5;93~`). Bytes are batched and flushed to preserve escape sequence integrity — writing byte-by-byte would break sequences because the CLI may interpret a lone `\x1b` as a standalone Escape keypress.

On continue trigger: send SIGTERM to the child process, wait up to 3 seconds, then SIGKILL if it hasn't exited. On natural CLI exit (user typed `/exit`, `quit`, etc.) or crash (non-zero exit without continue trigger): the runner should stop the workflow, print a message explaining how to resume the agent-runner session, and exit.

The idle hint renders after 800ms of PTY silence as a dim/reverse bar at the bottom row showing available continue triggers. It disappears when the CLI produces new output.

Both `github.com/creack/pty` and `golang.org/x/sys/unix` are already in `go.mod` (used by the POC). They just need to be imported from `internal/pty` instead.

In `internal/exec/agent.go`, the `ExecuteAgentStep` function has separate paths for headless and interactive. The interactive path should call the new PTY package's `RunInteractive`. The command and args come from the CLI adapter's `BuildArgs()` which is already wired in. If the result indicates a continue trigger, the outcome is `OutcomeSuccess` — discover and store session ID via the CLI adapter, advance to next step. If the CLI exited naturally or crashed, the outcome is `OutcomeAborted` — print a resume message and exit the runner.

After the PTY package is proven working, delete the `cmd/pty-poc/` directory entirely and the `.claude/skills/continue/` directory (the skill that wrote the signal file). Also remove `StartAgent` from the `ProcessRunner` interface in `internal/exec/interfaces.go` and the `AgentProcess` interface if no longer used.

The input processor (escape sequence parsing, `/next` detection, shortcut detection) is a pure function and should be extracted so it can be unit tested with crafted byte sequences. The full PTY lifecycle can be integration tested by launching a simple program (e.g., `cat` or `echo`) inside the PTY and verifying the result.

## Spec

### Requirement: PTY hosting for interactive steps

Interactive agent steps SHALL be executed inside a pseudo-terminal. The runner creates a PTY, attaches the CLI process to the PTY slave, and proxies I/O between the user's terminal and the PTY master. Headless steps SHALL NOT use a PTY.

#### Scenario: Interactive step launches in PTY
- **WHEN** the runner executes an interactive agent step
- **THEN** the CLI process runs inside a PTY with I/O proxied to the user's terminal

#### Scenario: Headless step does not use PTY
- **WHEN** the runner executes a headless agent step
- **THEN** the CLI process runs via direct exec without a PTY

### Requirement: Continue triggers

The PTY layer SHALL intercept two continue triggers: `/next` typed on a line followed by Enter, and a keyboard shortcut. When either is detected, the runner signals the CLI to terminate and advances to the next workflow step.

#### Scenario: /next typed
- **WHEN** the user types `/next` and presses Enter during an interactive step
- **THEN** the runner terminates the CLI process and advances to the next step with outcome success

#### Scenario: Keyboard shortcut pressed
- **WHEN** the user presses the continue keyboard shortcut during an interactive step
- **THEN** the runner terminates the CLI process and advances to the next step with outcome success

#### Scenario: Continue trigger not forwarded to CLI
- **WHEN** the user types `/next` or presses the continue keyboard shortcut
- **THEN** the typed bytes are intercepted by the PTY layer and not delivered to the CLI process as input

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

The PTY layer SHALL track ANSI escape sequence state and only evaluate continue triggers outside of escape sequences. Bytes that are part of CSI, OSC, DCS, or other escape sequences SHALL be forwarded to the CLI without interpretation.

#### Scenario: Escape sequence containing trigger bytes
- **WHEN** the user's terminal sends an escape sequence that contains bytes matching `/next`
- **THEN** the bytes are forwarded to the CLI and not treated as a continue trigger

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

### Requirement: Agent step execution dispatch

The runner's agent step executor SHALL delegate CLI invocation to the resolved CLI adapter. Interactive steps SHALL execute via the PTY layer. Headless steps SHALL execute via direct process execution. Both paths use the adapter for arg construction.

#### Scenario: Interactive step executes via PTY
- **WHEN** the runner executes an interactive agent step
- **THEN** the executor delegates arg construction to the CLI adapter and launches the process inside a PTY

## Done When

- `internal/pty` package exists with `RunInteractive` entrypoint
- Input processor correctly detects `/next` + Enter and keyboard shortcut (Ctrl-]) while ignoring these bytes inside escape sequences
- Continue trigger causes SIGTERM then SIGKILL after 3 seconds if needed
- Natural CLI exit or crash causes the runner to print a resume message and exit with outcome aborted
- Idle hint appears after 800ms of PTY silence and disappears on output
- Terminal resize is propagated to PTY
- Terminal state is always restored on exit (raw mode, terminal overrides)
- `ExecuteAgentStep` uses `pty.RunInteractive` for interactive steps
- `cmd/pty-poc/` directory is deleted
- `.claude/skills/continue/` directory is deleted
- Unit tests cover the input processor (escape sequences, `/next` detection, shortcut detection)
