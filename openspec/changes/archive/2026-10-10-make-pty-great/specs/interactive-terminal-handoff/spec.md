# Capability: interactive-terminal-handoff

## ADDED Requirements

### Requirement: Direct terminal inheritance

Interactive and autonomous-interactive agent steps and interactive shell steps SHALL spawn the child process with the user's terminal attached directly: the child inherits the runner's stdin, stdout, and stderr. The runner SHALL NOT create an intermediate terminal or read, write, buffer, capture, or modify any bytes flowing between the user's terminal and the child while the step runs. Headless agent and autonomous shell steps are unchanged: they use piped execution.

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

### Requirement: Foreground process group ownership

The runner SHALL place the CLI process in its own process group and SHALL make that group the controlling terminal's foreground process group before the CLI begins reading the terminal, so the child never receives SIGTTIN/SIGTTOU for terminal access. While the child owns the foreground, terminal-generated signals (such as Ctrl-C) SHALL be delivered to the child's process group only; the runner SHALL neither exit nor act on them. After the child exits, the runner SHALL reclaim the foreground process group before restoring the TUI.

When the child's process group stops (user suspension, CLI self-suspend, or external stop signal), the runner SHALL forward the suspension: save the child's terminal modes, reclaim the foreground, restore the runner-side terminal modes, and stop its own entire process group so the user's shell observes the whole job as suspended. When the job is continued in the foreground, the runner SHALL restore the child's saved terminal modes, transfer the foreground back to the child's group, and continue it. When the job is continued in the background, the runner SHALL NOT resume the child and SHALL NOT touch the terminal.

#### Scenario: Child reads the terminal without being stopped
- **WHEN** the CLI process reads from the terminal immediately after spawn
- **THEN** it is the terminal's foreground process group and receives the input without being stopped by SIGTTIN

#### Scenario: Ctrl-C reaches only the child
- **WHEN** the user presses Ctrl-C during an interactive agent step
- **THEN** the interrupt is delivered to the child's process group and the runner continues running and supervising

#### Scenario: Foreground reclaimed after exit
- **WHEN** the CLI process exits
- **THEN** the runner reclaims the foreground process group before repainting the TUI

#### Scenario: Child stop suspends the whole job
- **WHEN** the CLI's process group stops during an interactive agent step (user Ctrl-Z, CLI self-suspend, or external stop signal)
- **THEN** the runner does not treat the stop as an exit, and the user's shell observes the entire job as suspended

#### Scenario: Foreground continue resumes the child
- **WHEN** the suspended job is continued in the foreground (for example via `fg`)
- **THEN** the child's saved terminal modes are restored, the child's group becomes the foreground process group, and the child continues running

#### Scenario: Background continue does not resume the child
- **WHEN** the suspended job is continued in the background (for example via `bg`)
- **THEN** the runner does not resume the child and does not touch the terminal
<!-- resolved-in-design: full job-control forwarding with single-owner child waiting, suspension-paused deadlines, and a real-shell E2E — see design.md "Job control" -->

### Requirement: TUI release and restore around interactive steps

The live-run TUI SHALL fully release the terminal before the CLI process is spawned, and SHALL restore and repaint only after the CLI process has fully exited. If the terminal cannot be released, the step SHALL fail with a descriptive error before the CLI process is spawned. Restore SHALL occur regardless of how the step ended: completion via the control channel, natural exit, or crash; a restore failure SHALL be surfaced as an error without altering the step's recorded outcome. The runner MAY skip the restore/release cycle between consecutive interactive steps.

#### Scenario: Terminal released before spawn
- **WHEN** the workflow dispatches an interactive agent step while the TUI is active
- **THEN** the TUI has fully released the terminal before the CLI process starts

#### Scenario: TUI restored after completion
- **WHEN** an interactive agent step completes via the control channel and the CLI process exits
- **THEN** the TUI re-enters and repaints, and workflow execution continues

#### Scenario: TUI restored after crash
- **WHEN** the CLI process of an interactive agent step exits abnormally
- **THEN** the TUI re-enters and repaints before the runner reports the outcome

#### Scenario: Release failure prevents spawn
- **WHEN** the TUI fails to release the terminal for an interactive agent step
- **THEN** the CLI process is not spawned and the step fails with a descriptive error

#### Scenario: Restore failure surfaced without changing outcome
- **WHEN** the CLI process of a completed interactive step has exited and the TUI fails to restore
- **THEN** the failure is surfaced as an error and the step's recorded outcome is unchanged

### Requirement: Graceful termination on completion

When an accepted completion event's turn durability has been confirmed (acknowledgement delivered and committed-turn evidence observed, per `step-control-channel`), the runner SHALL terminate the CLI by sending SIGTERM to the child's process group. If the process has not exited within 3 seconds of active runtime (the clock pauses while the child is suspended), the runner SHALL send SIGKILL to the process group. The step outcome SHALL be `success` and the workflow SHALL advance to the next step. This requirement does not apply when the durability bound elapses without confirmation: the same termination ladder runs there, but the outcome is `failed` per `step-control-channel`.

#### Scenario: CLI exits promptly after SIGTERM
- **WHEN** a completion event has been accepted, turn durability confirmed, and the CLI exits after SIGTERM
- **THEN** the step outcome is `success` and the runner advances to the next step

#### Scenario: CLI does not exit after SIGTERM
- **WHEN** a completion event has been accepted, turn durability confirmed, and the CLI has not exited within 3 seconds of active runtime after SIGTERM
- **THEN** the runner sends SIGKILL to the process group, the step outcome is `success`, and the runner advances

#### Scenario: CLI exits on its own during the grace window
- **WHEN** a completion event has been accepted, turn durability confirmed, and the CLI exits by itself before any signal is delivered
- **THEN** the step outcome is `success` and the runner advances

### Requirement: Natural exit and crash handling

When the CLI process exits without a completion event having been accepted — whether with exit code 0 (user quit the CLI) or non-zero (crash) — the runner SHALL record the step outcome as `aborted`, stop the workflow, print a message explaining how to resume the run, and exit.

#### Scenario: User exits the CLI naturally
- **WHEN** the CLI process exits with code 0 and no completion event was accepted
- **THEN** the step outcome is `aborted`, the runner prints resume instructions, and the workflow stops

#### Scenario: CLI crashes
- **WHEN** the CLI process exits with a non-zero code and no completion event was accepted
- **THEN** the step outcome is `aborted`, the runner prints resume instructions, and the workflow stops

### Requirement: Runner crash does not orphan the CLI

If the runner process dies while an interactive agent step's CLI child is running, a supervision mechanism that outlives the runner SHALL terminate the child's process group — gracefully, then forcibly — after verifying that the process it signals is still the child it recorded, never a reused process ID. When a run is resumed after such a crash, the runner SHALL, under the run lock, terminate any verified surviving child from the crashed attempt and remove the stale control endpoint before starting a fresh attempt.

<!-- resolved-in-design: sibling watchdog process with pipe-EOF parent-death detection and process-start-time identity verification; resume cleanup from persisted attempt metadata — see design.md "Crash safety" -->

#### Scenario: Runner dies while the CLI is running
- **WHEN** the runner process terminates abruptly during an interactive agent step
- **THEN** the CLI child's process group is terminated rather than left running detached on the user's terminal

#### Scenario: Reused process ID is not signaled
- **WHEN** the crash-cleanup mechanism finds that the recorded child process ID now belongs to a different process
- **THEN** it sends no signal to that process

#### Scenario: Resume after a crash cleans up survivors
- **WHEN** a run is resumed after a runner crash and a verified child from the crashed attempt is still running
- **THEN** the runner terminates that child's process group gracefully and removes the stale control endpoint before the fresh attempt spawns

### Requirement: No terminal transcript for interactive terminal steps

The runner SHALL NOT capture, persist, or parse terminal output for interactive or autonomous-interactive agent steps or interactive shell steps. The durable record of an agent step is the CLI's native session log plus workflow audit events. An interactive shell step records command metadata, exit code, and outcome only.

#### Scenario: No output files created
- **WHEN** an interactive agent step runs and exits
- **THEN** no output files are created for the step in the session directory

#### Scenario: Audit events carry no terminal output
- **WHEN** the runner writes the step-end audit event for an interactive agent step
- **THEN** the event contains step metadata and outcome but no captured terminal output
