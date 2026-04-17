## ADDED Requirements

### Requirement: --list launches the run list TUI

The CLI SHALL accept a `--list` flag that launches a terminal UI showing workflow runs. The TUI is the entry point for all run-list navigation, view-switching, and status inspection. `--list`, `--resume` without a session ID, and bare `agent-runner` with no arguments SHALL all launch the TUI.

#### Scenario: --list launches TUI
- **WHEN** `--list` is passed
- **THEN** the terminal UI launches showing runs for the current project directory

#### Scenario: --resume without session ID launches TUI
- **WHEN** `--resume` is passed without a session ID
- **THEN** the terminal UI launches, identical to `--list`

#### Scenario: No arguments launches TUI
- **WHEN** `agent-runner` is invoked with no flags and no arguments
- **THEN** the terminal UI launches, identical to `--list`

### Requirement: Current directory view

The default TUI view SHALL show runs associated with the current project directory, sorted most recent first.

#### Scenario: Runs exist for current directory
- **WHEN** the TUI opens and runs exist for the current project directory
- **THEN** those runs are shown sorted most recent first

#### Scenario: No runs for current directory
- **WHEN** the TUI opens and no runs exist for the current project directory
- **THEN** an empty state is shown with the option to switch to the all-directories view

### Requirement: All-directories view

The TUI SHALL provide a view showing runs across all known project directories. This view SHALL always be reachable from the current directory view.

#### Scenario: Navigate to all-directories view
- **WHEN** the user switches to the all-directories view
- **THEN** runs from all project directories that have runs are shown

### Requirement: Worktree view

When the current directory is inside a git repository with sibling worktrees, the TUI SHALL detect those worktrees and allow the user to view runs for each one.

#### Scenario: Sibling worktrees detected
- **WHEN** the current directory is inside a git repo and sibling worktrees exist
- **THEN** the Worktrees tab is shown, listing all working copies (main checkout plus worktrees), including those with no runs

#### Scenario: No sibling worktrees
- **WHEN** the current directory is not inside a git repo or no sibling worktrees exist
- **THEN** the Worktrees tab is not shown

### Requirement: Run fields

Each run entry in the TUI SHALL display: workflow name, current step, status (active / inactive / completed), and start time.

#### Scenario: Active run fields
- **WHEN** a run is active (lock file present with alive PID)
- **THEN** its entry shows workflow name, current step, status=active, and start time

#### Scenario: Inactive run fields
- **WHEN** a run is inactive (interrupted before completion)
- **THEN** its entry shows workflow name, last recorded step, status=inactive, and start time

#### Scenario: Completed run fields
- **WHEN** a run completed successfully (no lock file and no state file)
- **THEN** its entry shows workflow name, status=completed, and start time (current step is omitted)

### Requirement: Resume from TUI

Pressing Enter on a run in the TUI SHALL exit the TUI and resume that run. Only inactive runs (resumable) SHALL be selectable for resume. Active and completed runs SHALL not be resumable from the TUI.

#### Scenario: Resume inactive run from TUI
- **WHEN** the user presses Enter on an inactive run
- **THEN** the TUI exits and the selected run is resumed

#### Scenario: Completed run not resumable
- **WHEN** the user presses Enter on a completed run
- **THEN** nothing happens (the run is not selectable for resume)

#### Scenario: Active run not resumable
- **WHEN** the user presses Enter on an active run
- **THEN** nothing happens (the run is not selectable for resume)

### Requirement: Run status determination

The TUI SHALL determine each run's status from the run-lock and state file: active (live lock), inactive (stale lock or state file present without lock), or completed (no lock and no state file).

#### Scenario: Status from live lock
- **WHEN** a session has a lock file with an alive PID
- **THEN** the run is shown as active

#### Scenario: Status from stale lock
- **WHEN** a session has a lock file with a dead PID
- **THEN** the run is shown as inactive

#### Scenario: Status from state file only
- **WHEN** a session has a state file but no lock file
- **THEN** the run is shown as inactive

#### Scenario: Status as completed
- **WHEN** a session has no lock file and no state file
- **THEN** the run is shown as completed
