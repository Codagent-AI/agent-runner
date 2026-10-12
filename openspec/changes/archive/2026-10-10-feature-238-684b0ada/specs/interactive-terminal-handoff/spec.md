## ADDED Requirements

### Requirement: Natural exit and crash handling

When the CLI process of an interactive or autonomous-interactive agent step exits without an accepted completion event, and supervision reports no error, the runner SHALL record the step outcome as `aborted` whatever the exit code, print instructions for resuming the run with `agent-runner --resume`, and stop the workflow. If supervision itself reports an error (for example a wait failure or a failure to reclaim the terminal) and no completion was accepted, the step outcome SHALL be `failed` instead.

#### Scenario: User exits the CLI naturally
- **WHEN** the CLI process exits with code 0 and no completion event was accepted
- **THEN** the step outcome is `aborted`, the runner prints resume instructions, and the workflow stops

#### Scenario: CLI crashes
- **WHEN** the CLI process exits with a non-zero code and no completion event was accepted
- **THEN** the step outcome is `aborted`, the runner prints resume instructions, and the workflow stops

#### Scenario: Supervision error fails the step
- **WHEN** the direct child's supervision returns an error and no completion event was accepted
- **THEN** the step outcome is `failed` rather than `aborted`

## MODIFIED Requirements

### Requirement: Full suspension forwarding

One supervisor SHALL exclusively own child waiting with stopped and continued events enabled. When the child group stops for any reason other than a terminal-access stop (SIGTTIN or SIGTTOU), the runner SHALL save child terminal modes, reclaim foreground ownership, restore runner modes, and stop its own process group so the user's shell sees the whole job as suspended. On `fg`, the runner SHALL restore child modes, return foreground ownership, and continue the child. On `bg`, it SHALL not resume the child or touch the terminal.

When the child is stopped by SIGTTIN or SIGTTOU, the runner SHALL NOT suspend the job. It SHALL instead return foreground ownership to the child's process group and continue it, recording the stop and the recovery as `terminal_ownership` audit events. If returning foreground ownership or continuing the child fails, the runner SHALL report the failure, kill the child process group, and reap the child rather than retrying.

#### Scenario: Child stop suspends the job
- **WHEN** Ctrl-Z, a self-suspend, or an external stop signal stops the child group
- **THEN** the stop is not treated as exit and the user's shell observes the Agent Runner job as suspended

#### Scenario: Foreground continuation resumes the child
- **WHEN** the user runs `fg`
- **THEN** terminal ownership and child modes are restored before the child receives SIGCONT

#### Scenario: Background continuation does not resume the child
- **WHEN** the user runs `bg`
- **THEN** Agent Runner leaves the child stopped until the job is foreground

#### Scenario: Terminal-access stop is recovered
- **WHEN** the child group is stopped by SIGTTIN or SIGTTOU while Agent Runner supervises it
- **THEN** Agent Runner returns terminal foreground to the child group and continues it without suspending the job, and records a `terminal_ownership` event for the recovery

#### Scenario: Failed terminal-access recovery ends supervision
- **WHEN** Agent Runner cannot return foreground ownership to a child stopped by SIGTTIN or SIGTTOU
- **THEN** it reports the failure, kills the child process group, and reaps the child instead of retrying

### Requirement: Runner crash does not orphan the child

For every interactive terminal child, Agent Runner SHALL start a watchdog and persist the child PID, process-group ID, and process start identity. If the runner disappears, the watchdog SHALL verify identity before terminating the child group. Resume cleanup SHALL perform the same identity check under the run lock before signaling a survivor. A reused PID SHALL never be signaled. When a run that recorded an in-flight interactive attempt is resumed, Agent Runner SHALL, after acquiring the run lock and before any step runs, terminate a verified surviving child group gracefully and then forcibly, and SHALL remove the crashed attempt's stale control socket and the run's control-socket pointer.

#### Scenario: Runner dies during an interactive step
- **WHEN** the parent exits abruptly while the child is alive
- **THEN** the watchdog terminates the verified child process group

#### Scenario: PID was reused
- **WHEN** the recorded PID now has a different process start identity
- **THEN** watchdog and resume cleanup send no signal

#### Scenario: Resume after a crash cleans up survivors
- **WHEN** a run is resumed after a runner crash and a verified child from the crashed attempt is still running
- **THEN** Agent Runner terminates that child's process group and removes the stale control endpoint before the first resumed step runs
