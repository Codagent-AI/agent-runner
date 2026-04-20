## MODIFIED Requirements

### Requirement: Resume action from run view
Selecting the resume action on an agent step (headless or interactive) SHALL launch the step's agent CLI with `--resume <session-id>`, resuming that agent's conversation directly. This is NOT the same as agent-runner's `--resume <run-id>` flag: the runview resume action targets the individual Claude/Codex/etc. session captured on the step, identified by the CLI's own session ID (e.g. `claude --resume <uuid>`), not an agent-runner workflow run.

The launch mechanism SHALL depend on how the run view was entered:

- **Live-run entry path** (run view opened after a live workflow run completed in the same process): the agent CLI SHALL be spawned as a subprocess. When the CLI exits (for any reason, including the user typing `/exit` or `/quit`), agent-runner SHALL re-enter the run view for the same run, re-reading audit and state files so events produced by the resumed session are visible.
- **Snapshot entry path** (run view opened via `--list` or `--inspect <run-id>`): agent-runner SHALL exec-replace its own process with the agent CLI; there is no return path, and the agent CLI's exit terminates agent-runner.

#### Scenario: Resume from headless agent step in live-run view
- **WHEN** the user triggers the resume action on a headless agent step with a known session ID, from a run view entered via the live-run path
- **THEN** the step's agent CLI is spawned as a subprocess with `--resume <session-id>` and the terminal is handed to it
- **AND WHEN** that CLI process exits
- **THEN** agent-runner re-enters the run view for the same run, with audit and state re-read so any new events from the resumed session appear

#### Scenario: Resume from interactive agent step in live-run view
- **WHEN** the user triggers the resume action on an interactive agent step with a known session ID, from a run view entered via the live-run path
- **THEN** the step's agent CLI is spawned as a subprocess with `--resume <session-id>` and the terminal is handed to it
- **AND WHEN** that CLI process exits
- **THEN** agent-runner re-enters the run view for the same run, with audit and state re-read so any new events from the resumed session appear

#### Scenario: User exits resumed CLI with /exit or /quit from live-run view
- **WHEN** the user has resumed an agent CLI session from a live-run run view and types `/exit` or `/quit` inside that CLI
- **THEN** the CLI process exits and agent-runner returns to the run view rather than exiting the agent-runner process

#### Scenario: Resume from headless agent step in snapshot view
- **WHEN** the user triggers the resume action on a headless agent step with a known session ID, from a run view entered via `--list` or `--inspect`
- **THEN** the TUI exits and the step's agent CLI is exec'd with `--resume <session-id>` (e.g. `claude --resume <uuid>`)

#### Scenario: Resume from interactive agent step in snapshot view
- **WHEN** the user triggers the resume action on an interactive agent step with a known session ID, from a run view entered via `--list` or `--inspect`
- **THEN** the TUI exits and the step's agent CLI is exec'd with `--resume <session-id>` (e.g. `claude --resume <uuid>`)

#### Scenario: Resume unavailable without session ID
- **WHEN** an agent step has no resolved session ID (never started, or crashed before session creation)
- **THEN** the resume action is not available for that step

#### Scenario: Spawn failure in live-run view
- **WHEN** the user triggers the resume action from a live-run run view and the agent CLI cannot be spawned (e.g. binary not found on PATH)
- **THEN** agent-runner does not exit; it returns to the run view and surfaces the spawn error to the user
