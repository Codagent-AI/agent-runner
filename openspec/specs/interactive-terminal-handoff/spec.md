# interactive-terminal-handoff Specification

## Purpose
Define direct terminal inheritance, process supervision, job control, crash cleanup, and TUI coordination for interactive agent and shell steps.

## Requirements

### Requirement: Direct terminal inheritance

Interactive agent steps, autonomous agent steps routed to the interactive backend, and interactive shell steps SHALL spawn their child process with the user's terminal inherited directly as stdin, stdout, and stderr. Agent Runner SHALL NOT create an intermediate terminal, relay bytes, propagate resize events, or read, buffer, capture, inspect, or rewrite terminal traffic. Headless agent and autonomous shell steps remain on their piped execution paths.

#### Scenario: Interactive child inherits the terminal
- **WHEN** any interactive terminal step starts
- **THEN** the child receives Agent Runner's real stdin, stdout, and stderr devices directly

#### Scenario: Terminal protocols work natively
- **WHEN** the child uses mouse reporting, application cursor mode, bracketed paste, resize notifications, or a future terminal protocol
- **THEN** the terminal and child communicate without Agent Runner interpreting or forwarding bytes

#### Scenario: Headless execution is unchanged
- **WHEN** an agent uses the headless backend or a shell step is autonomous
- **THEN** it uses the existing piped process path rather than terminal handoff

### Requirement: Foreground process group ownership

The runner SHALL place each interactive child in its own process group and make that group the controlling terminal's foreground process group before the child reads input. Terminal-generated signals such as Ctrl-C SHALL reach the child group rather than the runner. After exit, the runner SHALL reclaim foreground ownership and restore its saved terminal modes.

#### Scenario: Child reads immediately after spawn
- **WHEN** the child reads from the terminal as it starts
- **THEN** it is already foreground and is not stopped by SIGTTIN

#### Scenario: Ctrl-C reaches the child group
- **WHEN** the user presses Ctrl-C during an interactive step
- **THEN** the terminal signals the child process group while Agent Runner remains supervising

#### Scenario: Foreground is reclaimed
- **WHEN** the child exits
- **THEN** Agent Runner reclaims the terminal before the TUI repaints

### Requirement: Full suspension forwarding

One supervisor SHALL exclusively own child waiting with stopped and continued events enabled. When the child group stops, the runner SHALL save child terminal modes, reclaim foreground ownership, restore runner modes, and stop its own process group so the user's shell sees the whole job as suspended. On `fg`, the runner SHALL restore child modes, return foreground ownership, and continue the child. On `bg`, it SHALL not resume the child or touch the terminal.

#### Scenario: Child stop suspends the job
- **WHEN** Ctrl-Z, a self-suspend, or an external stop signal stops the child group
- **THEN** the stop is not treated as exit and the user's shell observes the Agent Runner job as suspended

#### Scenario: Foreground continuation resumes the child
- **WHEN** the user runs `fg`
- **THEN** terminal ownership and child modes are restored before the child receives SIGCONT

#### Scenario: Background continuation does not resume the child
- **WHEN** the user runs `bg`
- **THEN** Agent Runner leaves the child stopped until the job is foreground

### Requirement: TUI terminal lease

The live-run TUI SHALL release the terminal before an interactive child spawns and restore it after the child is reaped. A release failure SHALL fail the step before spawn. A restore failure SHALL be surfaced without changing an already recorded workflow outcome. Redundant release and restore between consecutive interactive steps MAY be skipped when ownership and modes remain correct.

#### Scenario: Release failure prevents spawn
- **WHEN** the TUI cannot release the terminal
- **THEN** the child is not spawned and the step fails descriptively

#### Scenario: Restore follows every exit path
- **WHEN** an interactive child completes, exits naturally, or crashes
- **THEN** Agent Runner reclaims and restores the terminal before continuing or reporting the result

### Requirement: Process-group termination

When Agent Runner terminates an interactive child, it SHALL send SIGTERM to the verified child process group, wait up to three seconds of active runtime, send SIGKILL if necessary, and reap the direct child. Active-runtime deadlines SHALL pause while the job is suspended.

#### Scenario: Child ignores SIGTERM
- **WHEN** a child group remains after the active-runtime grace period
- **THEN** Agent Runner sends SIGKILL to the group and reaps the child

### Requirement: Runner crash does not orphan the child

For every interactive terminal child, Agent Runner SHALL start a watchdog and persist the child PID, process-group ID, and process start identity. If the runner disappears, the watchdog SHALL verify identity before terminating the child group. Resume cleanup SHALL perform the same identity check under the run lock before signaling a survivor. A reused PID SHALL never be signaled.

#### Scenario: Runner dies during an interactive step
- **WHEN** the parent exits abruptly while the child is alive
- **THEN** the watchdog terminates the verified child process group

#### Scenario: PID was reused
- **WHEN** the recorded PID now has a different process start identity
- **THEN** watchdog and resume cleanup send no signal

### Requirement: No interactive terminal transcript

Agent Runner SHALL NOT capture terminal output for interactive agent, autonomous-interactive agent, or interactive shell steps. Agent sessions are recorded by the CLI's native session store and workflow audit events; interactive shell steps retain only command metadata, exit code, and outcome.

#### Scenario: Interactive terminal output is not persisted
- **WHEN** an interactive terminal child writes output
- **THEN** the bytes are visible live but absent from output files and audit values; an interactive shell `step_end` has no `stdout` and its schema-required `stderr` value is empty
