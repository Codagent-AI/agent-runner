## MODIFIED Requirements

### Requirement: Live run-view navigation remains available during UI steps

When a `mode: ui` step is rendered inside the live run view, it SHALL NOT behave as a modal that blocks workflow-tree navigation. The user SHALL be able to select another real tree row while the UI step remains pending. Navigating away SHALL pause active-step auto-follow, preserve the active ancestry whenever it remains inside the current manual scope, and show ordinary selected detail for the chosen row.

When the active UI row is selected, Left/Right, Tab, Shift-Tab, Enter, and other keys SHALL be routed to the UI form only when applicable to the focused control. Up/Down SHALL remain owned by workflow-tree navigation. Escape SHALL drill out one manually entered scope or perform the live run view's top-level Escape behavior. `j` and `k` SHALL scroll overflowing selected UI content without resolving it.

Enter SHALL remain the ordinary run-view drill action when selection is on a non-UI drillable container, including while a different UI step remains pending. The separate `d` drill shortcut SHALL be removed and SHALL NOT appear in help.

The jump-to-live action `l` SHALL return to root manual scope when necessary, expand the active UI ancestry inline, select the UI leaf, and re-engage auto-follow without drilling into the UI step or resolving it. The run-view `q` and Ctrl+C behavior SHALL remain available. Standalone UI rendering outside the live run view MAY retain its existing input navigation.

#### Scenario: Step navigation leaves UI step pending
- **WHEN** a UI step is pending and the user selects another real tree row
- **THEN** selected detail changes, auto-follow pauses, and the UI step remains unresolved

#### Scenario: Run-view navigation works away from active UI step
- **WHEN** selection is away from a pending active UI step
- **THEN** workflow-tree and selected-detail keys operate on the chosen row rather than the form

#### Scenario: Drill shortcut opens selected container during active UI step
- **WHEN** the user selects a non-UI drillable container and presses Enter while a UI step is pending
- **THEN** the run view manually drills into that container and leaves the UI step unresolved

#### Scenario: Escape drills out during active UI step
- **WHEN** the user is manually drilled into a container and presses Escape while a UI step is pending
- **THEN** the run view returns to the parent manual scope without resolving the UI step

#### Scenario: Existing run-view scroll keys scroll overflowing UI content
- **WHEN** the active UI row is selected and its content exceeds the detail height
- **THEN** `j` and `k` scroll that content without changing selection or resolving the form

#### Scenario: Follow shortcut returns to active UI step
- **WHEN** a UI step is active and auto-follow is paused
- **THEN** pressing `l` returns to root scope, expands and selects the UI leaf, re-engages follow, and leaves the form unresolved

#### Scenario: Quit shortcut remains available during active UI step
- **WHEN** the active UI row is selected and the user presses `q`
- **THEN** the live run view starts its ordinary quit flow rather than routing `q` to the form

#### Scenario: UI action keys still resolve the active UI step
- **WHEN** an active UI form is selected and the user operates Left/Right, Tab, Shift-Tab, or Enter on a control to which that key applies
- **THEN** the form handles the applicable action without triggering tree navigation

#### Scenario: Inapplicable form key is not consumed
- **WHEN** the active UI row is selected and a key is not applicable to the focused form control
- **THEN** the UI model does not consume it or accidentally resolve the form

#### Scenario: d is not a drill shortcut
- **WHEN** the user presses `d` while a UI step is pending
- **THEN** the run view does not treat `d` as drill-in
