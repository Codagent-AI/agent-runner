# Capability: interactive-terminal-handoff

## ADDED Requirements

### Requirement: Direct terminal inheritance

Interactive and autonomous-interactive agent steps SHALL spawn the CLI process with the user's terminal attached directly: the child inherits the runner's stdin, stdout, and stderr. The runner SHALL NOT create a pseudo-terminal for these steps and SHALL NOT read, write, buffer, or modify any bytes flowing between the user's terminal and the CLI process while the step runs. Headless agent steps are unchanged: they run via direct exec with piped output and no terminal.

#### Scenario: Interactive step inherits the terminal
- **WHEN** the runner executes an interactive agent step
- **THEN** the CLI process runs with the user's terminal attached directly (inherited stdin, stdout, stderr) and no runner-created PTY exists between them

#### Scenario: Autonomous-interactive step uses the same path
- **WHEN** the runner executes an autonomous agent step routed to the interactive backend
- **THEN** the CLI process runs with the user's terminal attached directly, identically to an interactive step

#### Scenario: Terminal features work natively
- **WHEN** the CLI negotiates terminal features with the user's terminal (mouse reporting, application cursor mode, bracketed paste, or any future protocol)
- **THEN** the features behave exactly as if the CLI had been launched manually from the shell; the runner does not observe or rewrite any of the involved bytes

#### Scenario: Resize reaches the CLI without runner involvement
- **WHEN** the user resizes the terminal while an interactive agent step is running
- **THEN** the CLI receives the size change through the terminal it inherited; the runner performs no resize propagation

#### Scenario: Headless step unchanged
- **WHEN** the runner executes a headless agent step
- **THEN** the CLI process runs via direct exec with piped output and no terminal attachment

### Requirement: TUI release and restore around interactive steps

The live-run TUI SHALL fully release the terminal before the CLI process is spawned, and SHALL restore and repaint only after the CLI process has fully exited. Restore SHALL occur regardless of how the step ended: completion via the control channel, natural exit, or crash.

#### Scenario: Terminal released before spawn
- **WHEN** the workflow dispatches an interactive agent step while the TUI is active
- **THEN** the TUI has fully released the terminal before the CLI process starts

#### Scenario: TUI restored after completion
- **WHEN** an interactive agent step completes via the control channel and the CLI process exits
- **THEN** the TUI re-enters and repaints, and workflow execution continues

#### Scenario: TUI restored after crash
- **WHEN** the CLI process of an interactive agent step exits abnormally
- **THEN** the TUI re-enters and repaints before the runner reports the outcome

### Requirement: Graceful termination on completion

When step completion is signaled via the control channel, the runner SHALL terminate the CLI by sending SIGTERM to the child's process group. If the process has not exited within 3 seconds, the runner SHALL send SIGKILL to the process group. The step outcome SHALL be `success` and the workflow SHALL advance to the next step.

#### Scenario: CLI exits promptly after SIGTERM
- **WHEN** a completion event is accepted and the CLI exits after SIGTERM
- **THEN** the step outcome is `success` and the runner advances to the next step

#### Scenario: CLI does not exit after SIGTERM
- **WHEN** a completion event is accepted and the CLI has not exited within 3 seconds of SIGTERM
- **THEN** the runner sends SIGKILL to the process group, the step outcome is `success`, and the runner advances

#### Scenario: CLI exits on its own during the grace window
- **WHEN** a completion event is accepted and the CLI exits by itself before any signal is delivered
- **THEN** the step outcome is `success` and the runner advances

### Requirement: Natural exit and crash handling

When the CLI process exits without a completion event having been accepted — whether with exit code 0 (user quit the CLI) or non-zero (crash) — the runner SHALL record the step outcome as `aborted`, stop the workflow, print a message explaining how to resume the run, and exit.

#### Scenario: User exits the CLI naturally
- **WHEN** the CLI process exits with code 0 and no completion event was accepted
- **THEN** the step outcome is `aborted`, the runner prints resume instructions, and the workflow stops

#### Scenario: CLI crashes
- **WHEN** the CLI process exits with a non-zero code and no completion event was accepted
- **THEN** the step outcome is `aborted`, the runner prints resume instructions, and the workflow stops

### Requirement: No terminal transcript for interactive agent steps

The runner SHALL NOT capture, persist, or parse the terminal output of interactive or autonomous-interactive agent steps. The durable record of such a step is the agent CLI's native session log plus the runner's workflow audit events.

#### Scenario: No output files created
- **WHEN** an interactive agent step runs and exits
- **THEN** no output files are created for the step in the session directory

#### Scenario: Audit events carry no terminal output
- **WHEN** the runner writes the step-end audit event for an interactive agent step
- **THEN** the event contains step metadata and outcome but no captured terminal output
