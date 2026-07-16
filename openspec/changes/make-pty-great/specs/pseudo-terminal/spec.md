# Capability: pseudo-terminal (delta)

## REMOVED Requirements

### Requirement: PTY hosting for interactive steps
**Reason**: Interactive and autonomous-interactive agent steps no longer run inside a runner-created PTY; the CLI process inherits the user's real terminal directly.
**Migration**: See `interactive-terminal-handoff` "Direct terminal inheritance". The headless no-PTY rule is restated there as well.

### Requirement: Continue triggers
**Reason**: In-band trigger detection (`/next` input scanning, keyboard shortcut, output sentinel) is retired along with the byte stream it depended on. The Ctrl-]-style shortcut is removed without replacement; recovery from a stuck agent is quit-and-resume.
**Migration**: Step completion is signaled out-of-band via `step-control-channel` (completion event, in-session `step complete` command, and any CLI-native surface routed through the control channel).

### Requirement: Graceful CLI termination on continue
**Reason**: Termination is no longer triggered by in-band continue detection.
**Migration**: See `interactive-terminal-handoff` "Graceful termination on completion" — same SIGTERM-then-SIGKILL-after-3-seconds contract, now applied to the child's process group and triggered by an accepted control-channel completion event.

### Requirement: Natural exit and crash handling
**Reason**: Behavior is unchanged but no longer belongs to the PTY layer, which no longer hosts agent steps.
**Migration**: See `interactive-terminal-handoff` "Natural exit and crash handling" — identical outcome (`aborted`, resume instructions, workflow stops).

### Requirement: Escape sequence passthrough
**Reason**: With no trigger detection there is no byte interpretation to scope; the retained shell-step relay forwards all bytes without inspection (see ADDED "Opaque byte relay for interactive shell steps").
**Migration**: None needed — passthrough becomes the unconditional behavior.

### Requirement: Idle hint
**Reason**: The runner can no longer draw on a terminal the child process owns, and the in-band triggers the hint advertised no longer exist. Dropped without replacement.
**Migration**: None. Completion guidance is delivered to the agent via injected completion instructions (`step-control-channel`).

## MODIFIED Requirements

### Requirement: Terminal resize propagation

The PTY layer used by interactive shell steps SHALL handle terminal resize events (SIGWINCH). When the user's terminal is resized during an interactive shell step, the new dimensions SHALL be propagated to the PTY so the hosted command renders correctly. Interactive agent steps do not use this mechanism: they inherit the user's terminal directly and receive size changes natively.

#### Scenario: Terminal resized during interactive shell step
- **WHEN** the user resizes their terminal while a command is running in an interactive shell step's PTY
- **THEN** the PTY dimensions are updated and the command receives the new size

## ADDED Requirements

### Requirement: Opaque byte relay for interactive shell steps

For interactive shell steps, the PTY layer SHALL act as an opaque relay: it creates the PTY, attaches the command, and copies bytes bidirectionally between the user's terminal and the PTY without inspecting, interpreting, or rewriting them. No input scanning, output scanning, or escape-sequence tracking SHALL occur.

#### Scenario: Output control sequences forwarded untouched
- **WHEN** the command in an interactive shell step emits ANSI control sequences (colors, cursor movement, mouse-mode negotiation)
- **THEN** the bytes reach the user's terminal exactly as emitted

#### Scenario: Input delivered without interpretation
- **WHEN** the user types text that resembles a historical continue trigger (such as `/next`) or presses key chords during an interactive shell step
- **THEN** the bytes are delivered to the command as normal input and the runner takes no action on them

### Requirement: Relay lifecycle hardening

The shell-step PTY relay SHALL have a deterministic lifecycle. Any signal the runner sends to terminate the command SHALL target the command's process group. When the command exits, the relay SHALL drain remaining PTY output within a bounded interval and close its resources in an explicit order that cannot deadlock. If the relay encounters an unrecoverable I/O error while the command is still running, the runner SHALL terminate the command's process group and fail the step with a descriptive error; normal failed-shell-step handling applies.

<!-- deferred-to-design: the exact drain bound and close ordering -->

#### Scenario: Output drained after command exit
- **WHEN** the command of an interactive shell step exits while output remains buffered in the PTY
- **THEN** the remaining output is written to the user's terminal within a bounded interval and the step completes without hanging

#### Scenario: Relay I/O error fails the step
- **WHEN** the relay hits an unrecoverable I/O error while the command is still running
- **THEN** the runner terminates the command's process group and the step outcome is `failed` with a descriptive error

#### Scenario: Termination signals reach the whole process group
- **WHEN** the runner terminates an interactive shell step's command
- **THEN** the termination signal is delivered to the command's process group, not only the immediate child
