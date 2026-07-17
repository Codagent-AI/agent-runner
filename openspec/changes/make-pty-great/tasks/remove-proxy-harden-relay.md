# Task: Remove the agent PTY proxy and harden the retained shell relay

## Goal

Delete the now-unreachable agent-path PTY machinery — input parsing, sentinel scanning, mouse tracking, marker stripping, idle hints — and reduce `internal/pty` to an opaque byte relay for interactive shell steps with a hardened lifecycle: process-group signaling, bounded draining with defined timeout behavior, explicit close ordering, and error propagation. Rewrite obsolete tests and run the complete deterministic and real-agent suites.

## Background

You MUST read these files before starting:

- `openspec/changes/make-pty-great/design.md` — especially "Package layout", the Migration Plan's final phase, and the drain/close decisions
- `openspec/changes/make-pty-great/specs/pseudo-terminal/spec.md` — the delta narrowing this capability to the shell relay (the full contract for this task)
- `openspec/changes/make-pty-great/specs/agent-continue-trigger/spec.md` — the retirement of the sentinel capability
- `openspec/specs/interactive-shell-steps/spec.md` — the existing shell-step behavior that must keep working (PTY execution with TUI suspend/resume, shell-native exit semantics, no trigger detection, workdir)

### Context

Production interactive agent execution already runs on the direct-handoff path (`internal/interactive`); agent steps no longer touch `internal/pty`. The PTY package still serves `mode: interactive` **shell** steps, which remain PTY-backed. This task removes the dead agent-path code and hardens what remains.

### What to delete

In `internal/pty/`: the terminal input parser and `/next` line tracking plus enhanced-key/SS3 interpretation (`input.go`), output sentinel/continuation-marker scanning and stripping (`output.go`), output-driven mouse-mode tracking (`mouse.go`), idle hints (`hint.go`), and their tests. Remove the agent-facing entry point (`RunInteractive` and `Options.ContinueMarker`) and any remaining sentinel constants or references elsewhere (for example `continuationMarkerPrefix` remnants in `internal/exec`); the compiler and a repository-wide search for the retired continuation-marker prefix are your guides. Integration tests that assert marker stripping or injected `/next` behavior (see `cmd/agent-runner/smoke_interactive_integration_test.go`) are rewritten to assert the structured control contract or deleted where the deterministic harness already covers the behavior. A tactical SS3/application-cursor fix may exist on the agent input path — it dies with the parser.

### What to keep and harden

The retained relay (`internal/pty/pty.go`, `terminal*.go`, transcript/copy helpers as needed by shell steps):

- **Opaque copy**: bidirectional byte copying with no inspection, interpretation, or rewriting — no input scanning, no output scanning, no escape-sequence tracking.
- **Process-group signaling**: any runner-initiated termination targets the command's process group, not only the immediate child.
- **Bounded drain**: after the command exits, drain remaining PTY output for at most **1 second**. On timeout (for example, a descendant still holds the PTY slave): terminate any surviving child process group, close the PTY, record a prominent drain-timeout warning indicating possible output truncation, and preserve the outcome derived from the command's exit code — never rerun a potentially side-effecting shell command automatically.
- **Explicit close ordering** that cannot deadlock, and **error propagation**: an unrecoverable relay I/O error while the command runs terminates the process group and fails the step with a descriptive error instead of hanging or being silently discarded.
- **Resize propagation** (SIGWINCH → PTY dimensions) stays, scoped to shell steps.

The interactive shell step executor lives in `internal/exec/`; its observable behavior (exit-code semantics, TUI suspend/resume, no trigger detection) must not change.

### Conventions

TDD for the hardening behaviors (failing test first); tests next to the source package; `google/go-cmp`; `make fmt`. Finish with the full suites: `make test`, `make lint`, the deterministic interactive harness, and the real-agent E2E suite (five interactive + five headless).

## Spec

### Capability: pseudo-terminal (delta)

#### REMOVED Requirements (delete the implementing code and obsolete tests)

- **PTY hosting for interactive steps** — interactive and autonomous-interactive agent steps inherit the real terminal (`interactive-terminal-handoff`).
- **Continue triggers** — in-band trigger detection (`/next` scanning, keyboard shortcut, output sentinel) is retired; completion is out-of-band (`step-control-channel`). The Ctrl-]-style shortcut is removed without replacement; recovery from a stuck agent is quit-and-resume.
- **Graceful CLI termination on continue** — moved to `interactive-terminal-handoff` (SIGTERM/SIGKILL on the process group, control-channel triggered).
- **Natural exit and crash handling** — moved to `interactive-terminal-handoff` (identical `aborted` + resume-instructions behavior).
- **Escape sequence passthrough** — with no trigger detection there is no byte interpretation to scope; passthrough is unconditional.
- **Idle hint** — the runner cannot draw on a terminal the child owns; dropped without replacement.

### Requirement: Terminal resize propagation (modified)

The PTY layer used by interactive shell steps SHALL handle terminal resize events (SIGWINCH). When the user's terminal is resized during an interactive shell step, the new dimensions SHALL be propagated to the PTY so the hosted command renders correctly. Interactive agent steps do not use this mechanism: they inherit the user's terminal directly and receive size changes natively.

#### Scenario: Terminal resized during interactive shell step
- **WHEN** the user resizes their terminal while a command is running in an interactive shell step's PTY
- **THEN** the PTY dimensions are updated and the command receives the new size

### Requirement: Opaque byte relay for interactive shell steps

For interactive shell steps, the PTY layer SHALL act as an opaque relay: it creates the PTY, attaches the command, and copies bytes bidirectionally between the user's terminal and the PTY without inspecting, interpreting, or rewriting them. No input scanning, output scanning, or escape-sequence tracking SHALL occur.

#### Scenario: Output control sequences forwarded untouched
- **WHEN** the command in an interactive shell step emits ANSI control sequences (colors, cursor movement, mouse-mode negotiation)
- **THEN** the bytes reach the user's terminal exactly as emitted

#### Scenario: Input delivered without interpretation
- **WHEN** the user types text that resembles a historical continue trigger (such as `/next`) or presses key chords during an interactive shell step
- **THEN** the bytes are delivered to the command as normal input and the runner takes no action on them

### Requirement: Relay lifecycle hardening

The shell-step PTY relay SHALL have a deterministic lifecycle. Any signal the runner sends to terminate the command SHALL target the command's process group. When the command exits, the relay SHALL drain remaining PTY output within a bounded interval (one second). If the drain bound elapses — for example because a surviving descendant still holds the PTY — the runner SHALL terminate any surviving child process group, close the PTY, and record a prominent drain-timeout warning indicating possible output truncation, while preserving the outcome derived from the command's exit code (the step is not automatically rerun). Resources SHALL be closed in an explicit order that cannot deadlock. If the relay encounters an unrecoverable I/O error while the command is still running, the runner SHALL terminate the command's process group and fail the step with a descriptive error; normal failed-shell-step handling applies.

#### Scenario: Output drained after command exit
- **WHEN** the command of an interactive shell step exits while output remains buffered in the PTY
- **THEN** the remaining output is written to the user's terminal within the drain bound and the step completes without hanging

#### Scenario: Drain timeout with a surviving descendant
- **WHEN** the command of an interactive shell step has exited but a descendant process keeps the PTY open past the drain bound
- **THEN** the runner terminates the surviving process group, closes the PTY, records a drain-timeout warning indicating possible output truncation, and the step outcome remains the one derived from the command's exit code

#### Scenario: Relay I/O error fails the step
- **WHEN** the relay hits an unrecoverable I/O error while the command is still running
- **THEN** the runner terminates the command's process group and the step outcome is `failed` with a descriptive error

#### Scenario: Termination signals reach the whole process group
- **WHEN** the runner terminates an interactive shell step's command
- **THEN** the termination signal is delivered to the command's process group, not only the immediate child

### Capability: agent-continue-trigger (delta)

#### REMOVED Requirements (delete the implementing code and obsolete tests)

- **Agent-initiated continue via PTY sentinel** — sentinel detection, stripping, and per-attempt marker freshness are retired; the control channel's per-step single-use credential carries the freshness guarantee forward.
- **Sentinel instruction injection** — sentinel instructions are meaningless without a sentinel detector; replaced by control-channel completion instructions (already live in production).

## Done When

No agent-path parsing, sentinel, mouse, or hint code remains in the tree (a repository-wide search finds no retired continuation-marker prefix; the control channel's env vars are named `AGENT_RUNNER_CONTROL_*`); interactive shell steps still pass their existing spec behavior; the four hardening scenarios are covered by tests and passing; `make test`, `make lint`, the deterministic interactive harness, and all ten real-agent E2Es pass.
