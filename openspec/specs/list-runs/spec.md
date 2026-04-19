## MODIFIED Requirements

### Requirement: Open run from TUI
Pressing Enter on a run in the TUI SHALL navigate from the list view to the run view for that run. The list view's state (cursor, tab, scroll offsets) SHALL be preserved so that returning from the run view restores it. Runs of any status (active, inactive, completed) SHALL be selectable. Resume is no longer triggered directly from the list — it becomes an action inside the run view (see `view-run` spec).

When the target run's run-lock is held by another live process, the list TUI SHALL reject the Enter action with an inline error and SHALL NOT navigate away from the list.

#### Scenario: Enter on inactive run opens run view
- **WHEN** the user presses Enter on an inactive run
- **THEN** the view switches from the list to the run view for that run

#### Scenario: Enter on active run opens run view
- **WHEN** the user presses Enter on an active run whose run-lock belongs to the current process
- **THEN** the view switches from the list to the run view for that run, with live refresh enabled

#### Scenario: Enter on completed run opens run view
- **WHEN** the user presses Enter on a completed run
- **THEN** the view switches from the list to the run view for that run in read-only mode

#### Scenario: Enter on run locked by another process is rejected
- **WHEN** the user presses Enter on a run whose run-lock belongs to another live process
- **THEN** the list TUI displays an inline error message identifying the run as active in another process; the list remains on screen and navigable

#### Scenario: Enter proceeds past a stale lock
- **WHEN** the user presses Enter on a run whose run-lock PID is dead
- **THEN** the lock is treated as stale and the run view opens normally

### Requirement: Resume inactive run from list

Pressing `r` on an inactive run in a run list SHALL exit the TUI and exec `agent-runner --resume <run-id>` in-place (identical behavior to `r` in the run view). The help bar SHALL show `r resume` when the cursor is on an inactive run.

`r` SHALL be ignored on active runs, completed runs, and on picker sub-views (worktree/all drill-in rows).

#### Scenario: r on inactive run resumes the run
- **WHEN** the user presses `r` on an inactive run in any tab's run list
- **THEN** the TUI exits and `agent-runner --resume <run-id>` executes in-place

#### Scenario: r on active run is ignored
- **WHEN** the user presses `r` on an active run
- **THEN** no action is taken

#### Scenario: r on completed run is ignored
- **WHEN** the user presses `r` on a completed run
- **THEN** no action is taken

#### Scenario: r on picker sub-view is ignored
- **WHEN** the user presses `r` while on a worktree or all-tab picker (not a run list)
- **THEN** no action is taken
