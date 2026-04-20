## MODIFIED Requirements

### Requirement: Resume action from run view
Selecting the resume action on an agent step (headless or interactive) SHALL spawn the step's agent CLI with `--resume <session-id>` as a subprocess and hand the terminal to it. This is NOT the same as agent-runner's `--resume <run-id>` flag: the runview resume action targets the individual Claude/Codex/etc. session captured on the step, identified by the CLI's own session ID (e.g. `claude --resume <uuid>`), not an agent-runner workflow run.

When the spawned CLI exits (for any reason, including the user typing `/exit` or `/quit`), agent-runner SHALL re-enter the run view for the same run, re-reading audit and state files so events produced by the resumed session appear. Re-entry preserves the original entry path so back-navigation (e.g. esc to the run list) still works. This behavior applies regardless of how the run view was reached (live-run completion, `--list`, or `--inspect`).

#### Scenario: Resume from headless agent step
- **WHEN** the user triggers the resume action on a headless agent step with a known session ID
- **THEN** the step's agent CLI is spawned as a subprocess with `--resume <session-id>` (e.g. `claude --resume <uuid>`) and the terminal is handed to it
- **AND WHEN** that CLI process exits
- **THEN** agent-runner re-enters the run view for the same run, with audit and state re-read so any new events from the resumed session appear

#### Scenario: Resume from interactive agent step
- **WHEN** the user triggers the resume action on an interactive agent step with a known session ID
- **THEN** the step's agent CLI is spawned as a subprocess with `--resume <session-id>` and the terminal is handed to it
- **AND WHEN** that CLI process exits
- **THEN** agent-runner re-enters the run view for the same run

#### Scenario: User exits resumed CLI with /exit or /quit
- **WHEN** the user has resumed an agent CLI session from the run view and types `/exit` or `/quit` inside that CLI
- **THEN** the CLI process exits and agent-runner returns to the run view rather than exiting the agent-runner process

#### Scenario: Resume unavailable without session ID
- **WHEN** an agent step has no resolved session ID (never started, or crashed before session creation)
- **THEN** the resume action is not available for that step

#### Scenario: Spawn failure
- **WHEN** the user triggers the resume action and the agent CLI cannot be spawned (e.g. binary not found on PATH)
- **THEN** agent-runner does not exit; it returns to the run view and surfaces the spawn error to the user
