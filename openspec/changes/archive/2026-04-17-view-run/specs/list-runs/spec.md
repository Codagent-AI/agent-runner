## MODIFIED Requirements

### Requirement: Open run from TUI
Pressing Enter on a run in the TUI SHALL navigate from the list view to the run view for that run. The list view's state (cursor, tab, scroll offsets) SHALL be preserved so that returning from the run view restores it. Runs of any status (active, inactive, completed) SHALL be selectable. Resume is no longer triggered directly from the list — it becomes an action inside the run view (see `view-run` spec).

#### Scenario: Enter on inactive run opens run view
- **WHEN** the user presses Enter on an inactive run
- **THEN** the view switches from the list to the run view for that run

#### Scenario: Enter on active run opens run view
- **WHEN** the user presses Enter on an active run
- **THEN** the view switches from the list to the run view for that run, with live refresh enabled

#### Scenario: Enter on completed run opens run view
- **WHEN** the user presses Enter on a completed run
- **THEN** the view switches from the list to the run view for that run in read-only mode
