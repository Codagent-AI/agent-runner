# workflow-resume-handoff Specification

## Purpose
Define the marker-file contract that lets a completed workflow ask Agent Runner to resume another run in place.

## Requirements
### Requirement: Workflow can signal a resume target during a run

A workflow run SHALL be able to signal a *resume target* — an existing run id that agent-runner should resume after the current workflow ends — at any point during the run. The signalling mechanism SHALL be a **marker file** at the path `<sessionDir>/resume-target` within the **signalling workflow's own session directory** (not the target run's session directory). The marker file SHALL contain the failed run's id as a single line of text, with surrounding whitespace and trailing newlines ignored on read. The runner SHALL accept at most one effective resume target per workflow run: when the workflow ends, the runner reads the marker file once; if multiple writes occurred during the run, the file's final contents are the effective target (last writer wins). Empty or whitespace-only contents SHALL be treated as no signal. If the file does not exist at workflow-end time, no resume target is recorded.

#### Scenario: Single write recorded
- **WHEN** during a workflow run the agent writes a single non-empty run id to `<sessionDir>/resume-target`
- **THEN** the runner reads that id at workflow-end and treats it as the recorded resume target

#### Scenario: Last writer wins
- **WHEN** the marker file is written multiple times during a workflow run with different values
- **THEN** at workflow-end the runner reads the file's final contents; earlier values are discarded by the act of overwriting

#### Scenario: No file means no resume
- **WHEN** a workflow run completes without the marker file ever being created
- **THEN** no resume target is recorded for the run

#### Scenario: Empty or whitespace-only contents ignored
- **WHEN** a workflow run completes and `<sessionDir>/resume-target` exists but contains only whitespace, only a newline, or nothing
- **THEN** the runner treats it as no recorded resume target

#### Scenario: Trimmed on read
- **WHEN** the marker file contains the id surrounded by whitespace or followed by a trailing newline (e.g. `  abc123\n`)
- **THEN** the runner trims surrounding whitespace before treating the contents as the resume target id

#### Scenario: Marker lives in signalling workflow's own session dir
- **WHEN** the runner checks for the marker
- **THEN** it reads `<sessionDir>/resume-target` where `<sessionDir>` is the session directory of the workflow run that just ended, not the directory of any other run

#### Scenario: Marker file read error treated as no signal
- **WHEN** `<sessionDir>/resume-target` exists but cannot be read (permissions, IO error, or other filesystem failure on the marker itself)
- **THEN** the runner treats it as no recorded resume target, does not attempt to exec, does not surface an error to the user, and returns to the launching TUI screen as for any other completed workflow

### Requirement: Resume target acted on at successful completion

When a workflow run ends with a **success** outcome AND a resume target was recorded during the run, agent-runner SHALL exec `agent-runner --resume <target-run-id>` in place, replacing the current process — using the same in-place-exec pattern that the run-view `r` keybinding uses. If the workflow ends with any non-success outcome (failed, aborted, cancelled), the resume target SHALL be discarded silently and agent-runner SHALL return to the launching TUI screen as it would for any other workflow run.

#### Scenario: Success with target execs --resume in place
- **WHEN** a workflow run ends with outcome success and a resume target is recorded
- **THEN** agent-runner execs `agent-runner --resume <target-run-id>` in place, replacing the current process; the TUI is not returned to

#### Scenario: Failure discards target
- **WHEN** a workflow run ends with outcome failed and a resume target was recorded
- **THEN** the recorded resume target is discarded, no exec is attempted, and the user is returned to the launching TUI screen as for any other failed workflow run

#### Scenario: Cancellation discards target
- **WHEN** a workflow run ends with outcome aborted or cancelled and a resume target was recorded
- **THEN** the recorded resume target is discarded, no exec is attempted, and the user is returned to the launching TUI screen

#### Scenario: Success without target returns to launching screen
- **WHEN** a workflow run ends with outcome success and no resume target was recorded
- **THEN** no exec is attempted; the user is returned to the launching TUI screen as for any other successful workflow run

### Requirement: Invalid resume target falls back gracefully

If the recorded resume target run id does not resolve to a known run at workflow-end time (the session directory does not exist or is unreadable), agent-runner SHALL NOT attempt the exec. It SHALL surface an inline error on the launching TUI screen identifying the invalid target id and the reason (not found, unreadable, etc.), and SHALL otherwise return to that screen as for any other completed workflow.

#### Scenario: Unknown target id falls back
- **WHEN** the recorded resume target run id has no corresponding session directory at workflow-end time
- **THEN** no exec is attempted, an inline error is shown on the launching TUI screen naming the missing run id, and the user is returned to that screen

#### Scenario: Unreadable target session falls back
- **WHEN** the recorded resume target run id resolves to a session directory that is unreadable (permissions, IO error, missing `state.json`)
- **THEN** no exec is attempted, an inline error is shown on the launching TUI screen naming the run id and the read error, and the user is returned to that screen

### Requirement: Resume-handoff is launch-source-independent

The resume-handoff exec SHALL execute regardless of how the originating workflow was launched (home-tab menu, run-view keybinding, modal action, programmatic invocation). The replacement process SHALL behave as if `agent-runner --resume <target-run-id>` had been invoked from a fresh shell — it does NOT inherit the launching screen's drill-in path, selection, or any other TUI state. The resumed run SHALL be the target run identified by the signal, not the workflow run that emitted the signal.

#### Scenario: Launched from home menu
- **WHEN** the originating workflow was launched from the home TUI's new-workflow tab, ends success, and has a valid resume target
- **THEN** the in-place exec runs; the user lands in the run view of the target run as specified by the existing `--resume` behavior

#### Scenario: Launched from run-view keybinding
- **WHEN** the originating workflow was launched from a run-view keybinding (e.g. `d`), ends success, and has a valid resume target
- **THEN** the in-place exec runs; the previous run-view is not restored; the user lands in the run view of the target run

#### Scenario: Launched from modal action
- **WHEN** the originating workflow was launched from a TUI modal action (e.g. the onboarding-failure modal's Debug-now button), ends success, and has a valid resume target
- **THEN** the in-place exec runs; the modal is gone; the user lands in the run view of the target run

#### Scenario: Resumed run is target, not originating workflow
- **WHEN** the in-place exec runs
- **THEN** the run that is resumed is the run identified by the resume target, not the workflow run that emitted the signal
