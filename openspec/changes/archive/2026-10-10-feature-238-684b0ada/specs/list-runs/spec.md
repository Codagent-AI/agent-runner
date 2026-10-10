## MODIFIED Requirements

### Requirement: Open run from TUI
Pressing Enter on a run in the TUI SHALL navigate from the list view to the run view for that run. The list view's state (cursor, tab, scroll offsets) SHALL be preserved so that returning from the run view restores it, except where the run view's post-run Escape behavior (see `view-run`) replaces the list with a fresh one for a completed or failed run. Runs of any status (active, inactive, completed) SHALL be selectable. Resume is no longer triggered directly from the list — it becomes an action inside the run view (see `view-run` spec).

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

## ADDED Requirements

### Requirement: Tab navigation includes new tab
The list TUI's tab bar SHALL show the tabs New, Current Dir, Worktrees, and All, in that order. The Worktrees tab SHALL appear only when the current directory is inside a Git repository whose worktrees can be listed. The keys `n`, `c`, `w`, and `a` SHALL switch directly to the New, Current Dir, Worktrees, and All tabs respectively; `w` SHALL be ignored when the Worktrees tab is absent. `→`/`Tab` SHALL cycle forward and `←`/`Shift+Tab` SHALL cycle backward through the visible tabs, wrapping at either end. The New tab's content is the workflow browser defined by `new-tab-layout`, not a run list. Entering the New tab SHALL reset its show-hidden toggle and rebuild its rows.

#### Scenario: n switches to new tab
- **WHEN** the list has focus on any tab and the user presses `n`
- **THEN** the active tab switches to the New tab displaying the workflow list

#### Scenario: Tab order in tab bar
- **WHEN** the list TUI is open inside a Git repository
- **THEN** the tab bar displays tabs in order: New, Current Dir, Worktrees, All

#### Scenario: Worktrees tab absent outside a Git repository
- **WHEN** the list TUI is open in a directory where Git worktrees cannot be listed
- **THEN** the tab bar displays New, Current Dir, All, pressing `w` does nothing, and arrow or Tab cycling skips the Worktrees tab

### Requirement: Default tab on entry
The initial focused tab when the list TUI opens SHALL depend on how it was invoked:
- Bare `agent-runner` (no subcommand, workflow, or flags) SHALL focus the New tab.
- `--resume` with no run ID SHALL focus the Current Dir tab.
- `--list` SHALL focus the Current Dir tab.
- Returning from a workflow definition view, or from a run view that restores the list (see `view-run` exit behavior), SHALL keep whichever tab, cursor, and scroll state were active when the user left the list.

#### Scenario: Bare invocation opens new tab
- **WHEN** the user runs `agent-runner` with no subcommand, workflow, or flags
- **THEN** the list TUI opens with the New tab focused

#### Scenario: --resume no arg opens current-dir tab
- **WHEN** the user runs `agent-runner --resume` with no run ID argument
- **THEN** the list TUI opens with the Current Dir tab focused

#### Scenario: --list opens current-dir tab
- **WHEN** the user runs `agent-runner --list`
- **THEN** the list TUI opens with the Current Dir tab focused

#### Scenario: Return from definition view restores previous tab
- **WHEN** the user was on the New tab, opened a workflow definition view, and presses Escape at its top level
- **THEN** the list TUI is restored with the New tab focused and its prior cursor, search text, and scroll state
