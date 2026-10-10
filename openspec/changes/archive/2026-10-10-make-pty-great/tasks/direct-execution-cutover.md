# Task: Implement direct terminal execution and cut over production

## Goal

Build the direct-handoff execution path — terminal inheritance, foreground process-group ownership, full job-control forwarding, suspension-aware deadlines, the parent-death watchdog, and the error-returning TUI terminal lease — then switch production interactive execution to it. This is the cutover: after this task, interactive and autonomous-interactive agent steps run on the user's real terminal and complete via the control channel.

## Background

You MUST read these files before starting:

- `openspec/changes/make-pty-great/design.md` — the binding design; especially "Completion state machine", "Job control", "Crash safety", "Prompt and adapter integration", "Migration Plan", and "Testing"
- `openspec/changes/make-pty-great/phase0-findings.md` — spike outcomes for wait ownership, foreground spawn, and the watchdog pattern
- `openspec/changes/make-pty-great/specs/interactive-terminal-handoff/spec.md` — the primary behavioral contract for this task
- `openspec/changes/make-pty-great/specs/step-control-channel/spec.md` — "Completion instruction injection" plus the durability scenarios this task proves end-to-end

### What to build

- **DirectRunner** (`internal/interactive/runner.go`): orchestrates one interactive step — acquire the terminal lease, ensure the control endpoint exists with a fresh attempt credential, spawn the child with inherited `os.Stdin`/`os.Stdout`/`os.Stderr` and `SysProcAttr{Setpgid: true, Foreground: true, Ctty: <tty fd>}`, supervise until completion or exit, terminate, release.
- **Supervisor** (`internal/interactive/process.go`): a single goroutine exclusively owns child waiting via `unix.Wait4(pid, &status, unix.WUNTRACED|unix.WCONTINUED, nil)`; `cmd.Wait()` is never called. Job-control state machine (binding): on child stop — save the child's terminal modes, reclaim the foreground (`tcsetpgrp`), restore the runner-side terminal modes, SIGSTOP the runner's entire process group; on foreground continue — verify the runner is foreground, restore the child's saved terminal modes, transfer the foreground to the child's group, SIGCONT it; on background continue — do not resume the child, do not touch the terminal. An initial SIGTTIN stop is recoverable (re-assert foreground, SIGCONT), not fatal. The runner neither exits on nor consumes terminal-generated signals while the child owns the foreground. The 30 s durability bound and 3 s termination grace count active runtime only — both clocks pause while the job is suspended. Termination: SIGTERM to the child's process group, SIGKILL after 3 s of active runtime. Audit `child_stopped`/`child_continued` events.
- **Watchdog**: `agent-runner internal watchdog` subcommand spawned after the child, receiving the child's pid/pgid/process-start-time as arguments and the read end of a pipe whose write end only the runner holds. Pipe EOF (runner died): verify child identity via pid + start time (no PID-reuse kills; start time from `/proc/<pid>/stat` on Linux, `sysctl kern.proc.pid` on macOS), SIGTERM the group, wait the grace, SIGKILL, exit. Normal completion: runner closes the pipe after reaping; watchdog verifies the child is gone and exits. Resume cleanup: run state persists per-attempt metadata (`child_pid`, `pgid`, `start_time`, socket path); on resume, under the run lock, a surviving verified process is terminated gracefully before retrying, and the stale socket is unlinked.
- **Terminal lease** (`internal/liverun/coordinator.go`): `BeforeInteractive` currently logs and swallows `ReleaseTerminal()` errors, and the `ctx.SuspendHook`/`ctx.ResumeHook` wiring (see `internal/exec/agent.go`) is `func()` with no error. Make release error-returning end-to-end: a release failure fails the step before the child is spawned. Restore failures are surfaced as errors without altering the step's recorded outcome. Preserve the existing idempotent skip that avoids restore/release flicker between consecutive interactive steps (`suspended`/`pendingResume` logic).
- **Cutover** (`internal/exec/agent.go`): replace the `interactiveRunnerFn = pty.RunInteractive` seam with the DirectRunner. Replace `completionInstruction()`/`newContinueMarker()`/`continueMarkerForContext()` with control-channel completion instructions telling the agent to run the completion client by absolute path (`os.Executable()`); the socket and token travel via the injected env vars. Injection rules are unchanged: interactive and autonomous-interactive prompts receive the instructions (refresh on resume, as `continueMarkerPromptNeedsRefresh` does today); headless prompts do not. Natural exit and crash keep today's behavior: outcome `aborted`, resume message (`agent-runner --resume`), workflow stops. No transcript: the interactive result carries no captured terminal output (the current code already discards it — keep it that way deliberately).
- **Design decisions that govern this task**: adapter-owned semantic durability with timeout-as-failure (`success` must mean a later `session: resume` is safe); full job-control forwarding (auto-resume would fight CLIs that deliberately self-suspend; doing nothing leaves the terminal owned by a stopped group); supervisor owns Wait4 (one reaper, no race with `os/exec`); kernel-side foreground transfer at spawn; watchdog as a separate pipe-EOF-triggered process (portable to macOS, PID-reuse-safe).

### Tests this task must deliver

- **Extended deterministic fake-agent harness** (`cmd/agent-runner/smoke_interactive_integration_test.go` and the fake fixture it drives): the fake agent completes via the control socket instead of the output marker; all existing terminal-fidelity cases retained; release/restore failure injection.
- **Real-shell job-control E2E**: an outer PTY runs a real job-control shell, launches agent-runner as a foreground job, suspends (both a cooperative self-suspend and an external SIGSTOP), verifies the shell reports the job stopped, runs `fg`, verifies the child resumes with correct terminal modes.
- **Five per-CLI recall/resume durability E2Es** (`cmd/agent-runner/real_agent_e2e_test.go`): step 1 tells the agent a unique recall phrase and the agent completes via the control channel; step 2 resumes the same session and asks the agent to repeat the phrase. The phrase appears ONLY in step 1's prompt — never in step 2's prompt, environment, state summary, or fixture — so recall proves the completing turn survived termination. (Called a "recall phrase", not a nonce, to avoid collision with the completion credential.)

The old `pty.RunInteractive` path and its unit tests remain in the tree (deleted by a later cleanup); only production wiring switches.

### Conventions

TDD; tests next to the source package; `google/go-cmp`; local fakes; `make fmt`; run targeted tests while iterating, `make test` and `make lint` before finishing.

## Spec

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

#### Scenario: Runner dies while the CLI is running
- **WHEN** the runner process terminates abruptly during an interactive agent step
- **THEN** the CLI child's process group is terminated rather than left running detached on the user's terminal

#### Scenario: Reused process ID is not signaled
- **WHEN** the crash-cleanup mechanism finds that the recorded child process ID now belongs to a different process
- **THEN** it sends no signal to that process

#### Scenario: Resume after a crash cleans up survivors
- **WHEN** a run is resumed after a runner crash and a verified child from the crashed attempt is still running
- **THEN** the runner terminates that child's process group gracefully and removes the stale control endpoint before the fresh attempt spawns

### Requirement: Agent step execution dispatch (workflow-execution, modified)

The runner's agent step executor SHALL resolve the agent profile before delegating CLI invocation. For `session: new` steps, the profile is resolved from the step's `agent` field. For `session: resume` or `session: inherit` steps, the profile is inherited from the session-originating step. The step's optional `mode` override is applied on top of the resolved profile's `default_mode`. Per-step `model` and `cli` overrides, if present, take precedence over the profile's values. Interactive steps (and autonomous steps routed to the interactive backend) SHALL execute via direct terminal handoff per `interactive-terminal-handoff`. Autonomous-headless steps SHALL execute via direct process execution with piped output. Both paths use the adapter for arg construction.

> The profile/override resolution above is existing behavior and unchanged; this task changes only the interactive dispatch target (direct terminal handoff instead of the PTY layer). The full modified requirement with all scenarios is in `openspec/changes/make-pty-great/specs/workflow-execution/spec.md`.

#### Scenario: New session step dispatched
- **WHEN** the runner executes an agent step with `session: new` and `agent: interactive_base`
- **THEN** the runner resolves the `interactive_base` profile, determines mode from the profile's `default_mode` (or the step's `mode` override), and dispatches via direct terminal handoff for interactive or direct headless exec for autonomous

### Requirement: No terminal transcript for interactive agent steps

The runner SHALL NOT capture, persist, or parse the terminal output of interactive or autonomous-interactive agent steps. The durable record of such a step is the agent CLI's native session log plus the runner's workflow audit events.

#### Scenario: No output files created
- **WHEN** an interactive agent step runs and exits
- **THEN** no output files are created for the step in the session directory

#### Scenario: Audit events carry no terminal output
- **WHEN** the runner writes the step-end audit event for an interactive agent step
- **THEN** the event contains step metadata and outcome but no captured terminal output

### Requirement: Completion instruction injection

The runner SHALL append completion instructions to the prompt for every interactive and autonomous-interactive agent step, telling the agent how to signal step completion through the control channel. Headless agent steps SHALL NOT receive completion instructions. These instructions replace the retired sentinel instructions.

#### Scenario: Interactive step receives completion instructions
- **WHEN** the runner builds the prompt for an interactive agent step
- **THEN** the prompt includes instructions telling the agent how to signal completion through the control channel

#### Scenario: Autonomous-interactive step receives completion instructions
- **WHEN** the runner builds the prompt for an autonomous-interactive agent step
- **THEN** the prompt includes the same completion instructions

#### Scenario: Headless step receives no completion instructions
- **WHEN** the runner builds the prompt for a headless agent step
- **THEN** the prompt does not include completion instructions

### Requirement: Completion acknowledgement and turn durability (end-to-end proof)

> The state machine already exists; this task proves the following scenarios end-to-end with real CLIs.

#### Scenario: Session resumable after completion
- **WHEN** an interactive step completes via the control channel and a later step resumes the same agent session
- **THEN** the resumed session includes the turn during which the completion event was sent

#### Scenario: Resumed workflow retries after durability failure
- **WHEN** a run whose step failed on durability timeout is resumed
- **THEN** the step runs a fresh attempt with a newly issued completion credential

## Done When

Production interactive and autonomous-interactive execution runs on the direct path. The extended deterministic harness, the real-shell job-control E2E (both suspension variants plus `fg`), and all five recall/resume durability E2Es pass, alongside the five existing headless real-agent E2Es. Every scenario above is covered. `make test` and `make lint` pass.
