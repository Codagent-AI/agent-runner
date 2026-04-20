## ADDED Requirements

### Requirement: Resume run from run view

The run view SHALL provide an `r` keyboard action that resumes the agent-runner workflow run itself (distinct from the existing Enter-triggered agent-CLI session resume). The action SHALL be available at any drill depth. It SHALL be gated on the run's status being `inactive` AND the run view not currently executing a workflow live (i.e., the live-run-view `running` state is false). When triggered, the TUI SHALL exit cleanly and the current process SHALL exec `agent-runner --resume <run-id>`, replacing itself (the same in-place-exec pattern used for agent-CLI session resume on Enter).

When the gate is satisfied, the top-level breadcrumb SHALL render a `(r to resume)` affordance adjacent to the `inactive` status token, and the help bar SHALL include an entry for the `r` binding. When the gate is not satisfied, neither the breadcrumb affordance nor the help-bar entry SHALL appear.

#### Scenario: r on inactive run resumes via agent-runner --resume
- **WHEN** the run's status is `inactive`, the TUI is not running a workflow live, and the user presses `r`
- **THEN** the TUI exits and the current process execs `agent-runner --resume <run-id>` in-place

#### Scenario: r works at any drill depth
- **WHEN** the user is drilled inside a sub-workflow, loop, or iteration in an `inactive` run and presses `r`
- **THEN** the TUI exits and `agent-runner --resume <run-id>` is exec'd (drill depth does not affect the action)

#### Scenario: r is ignored while a workflow is running live
- **WHEN** the run view is in live-run-view mode with `running == true` and the user presses `r`
- **THEN** nothing happens (the key is not bound in this state)

#### Scenario: r is ignored on active run opened from list
- **WHEN** the run's status is `active` (opened from the list TUI) and the user presses `r`
- **THEN** nothing happens

#### Scenario: r is ignored on completed run
- **WHEN** the run's status is `completed` and the user presses `r`
- **THEN** nothing happens

#### Scenario: r is ignored on failed run
- **WHEN** the run's status is `failed` and the user presses `r`
- **THEN** nothing happens

#### Scenario: Breadcrumb affordance shown for inactive run
- **WHEN** the run's status is `inactive` and the TUI is not running a workflow live
- **THEN** the top-level breadcrumb renders `(r to resume)` adjacent to the `inactive` status token

#### Scenario: Breadcrumb affordance hidden during live run
- **WHEN** the TUI is running a workflow live (`running == true`)
- **THEN** the `(r to resume)` affordance is not shown, regardless of status

#### Scenario: Help bar lists r binding when available
- **WHEN** the resume-run gate is satisfied
- **THEN** the help bar includes an entry for the `r` binding

#### Scenario: Help bar omits r binding when unavailable
- **WHEN** the resume-run gate is not satisfied (status is not `inactive`, or the TUI is running live)
- **THEN** the help bar does not include the `r` entry

## MODIFIED Requirements

### Requirement: Keyboard focus and scrolling
The step list SHALL always own the up/down arrow keys for step navigation. The detail pane SHALL scroll via `j` (down) and `k` (up) and via the mouse wheel. Focus SHALL not need to be switched between panes. `PgUp`/`PgDown` SHALL NOT be bound.

#### Scenario: Up/down navigates step list
- **WHEN** the user presses `↑` or `↓`
- **THEN** the step list selection moves one row in that direction and the detail pane updates for the newly selected step

#### Scenario: j/k scrolls detail pane
- **WHEN** the user presses `j` or `k`
- **THEN** the detail pane scrolls one line down (`j`) or up (`k`); the step list selection does not change

#### Scenario: Mouse wheel scrolls detail pane
- **WHEN** the user scrolls the mouse wheel while the pointer is over the detail pane
- **THEN** the detail pane scrolls

#### Scenario: Mouse wheel outside detail pane is ignored
- **WHEN** the user scrolls the mouse wheel while the pointer is over the step list
- **THEN** nothing happens (arrow keys are the only way to navigate the step list)

#### Scenario: PgUp and PgDown are not bound
- **WHEN** the user presses `PgUp` or `PgDown`
- **THEN** nothing happens (the keys are not bound; neither the step list nor the detail pane reacts)
