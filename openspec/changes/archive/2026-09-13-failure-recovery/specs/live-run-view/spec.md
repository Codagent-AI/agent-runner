# Capability: live-run-view

## Purpose

Defines the live run view launched for an active workflow invocation.

## MODIFIED Requirements

### Requirement: Cursor auto-follows the active step

While the workflow is running, active-step auto-follow SHALL begin engaged. It SHALL expand the active ancestry in the root workflow tree, select the active leaf itself, and keep that row visible. Active leaves include ordinary steps, iterations when they are the execution frontier, UI steps, agent calls, and steps executing inside a repair attempt (the inline repair agent, or a replayed step under a `rerun` container). While a repair attempt is active, the owning check row SHALL show a static in-progress indicator and the `(repairing N/M)` suffix, and its attempt children SHALL be expanded inline.

Auto-follow SHALL NEVER drill into or out of a sub-workflow, loop, iteration, group, or agent parent. Entering and leaving nested execution SHALL change inline expansion and selection without changing the manual breadcrumb scope.

Up/Down tree navigation, manual drill-in or drill-out, and scrolling upward within the selected response SHALL pause active-step auto-follow. While paused, execution progress SHALL NOT change selection. When the active ancestry remains inside the current manual scope, it SHALL remain expanded while the user inspects another row.

Pressing `l` SHALL return to root manual scope when needed, expand the current active ancestry, select the active leaf, scroll its response to the tail, and re-engage both active-step follow and response tail-follow.

#### Scenario: Active step advances to peer
- **WHEN** auto-follow is engaged and execution moves to a peer
- **THEN** selection moves to that peer and its selected detail is shown

#### Scenario: Active step enters a sub-workflow
- **WHEN** auto-follow is engaged and execution enters a nested sub-workflow
- **THEN** the sub-workflow ancestry expands inline, the active leaf is selected, and the breadcrumb scope does not change

#### Scenario: Active step enters a loop iteration
- **WHEN** auto-follow is engaged and execution enters a loop iteration
- **THEN** the loop and iteration ancestry expand inline, the active leaf is selected, and the breadcrumb scope does not change

#### Scenario: Active call becomes selected leaf
- **WHEN** an accepted agent call becomes the active execution frontier
- **THEN** its parent expands inline and the call row becomes selected without drill-in

#### Scenario: Active step leaves a sub-workflow
- **WHEN** execution leaves nested work and advances elsewhere
- **THEN** inline active expansion updates without changing manual drill scope

#### Scenario: Manual navigation pauses auto-follow
- **WHEN** the user moves tree selection with Up or Down
- **THEN** auto-follow pauses and execution progress does not steal selection

#### Scenario: Manual drill pauses auto-follow
- **WHEN** the user drills in or out manually
- **THEN** auto-follow pauses and the chosen scope remains under user control

#### Scenario: Response scroll-up pauses auto-follow
- **WHEN** the user presses `k` or scrolls upward with the mouse within the selected response
- **THEN** active-step follow and response tail-follow both pause

#### Scenario: Paused active ancestry remains visible in scope
- **WHEN** auto-follow is paused, the user selects another row, and the active leaf remains inside the current scope
- **THEN** the active ancestry remains expanded while selection stays where the user placed it

#### Scenario: Jump-to-live re-engages auto-follow
- **WHEN** the user presses `l` while follow is paused
- **THEN** the view returns to root scope if needed, expands and selects the active leaf, moves its response to the tail, and resumes both follow modes without drilling

#### Scenario: Failure jumps cursor to the failed step
- **WHEN** the workflow reaches failed terminal state
- **THEN** the root tree expands and selects the failed leaf without creating a drill scope, regardless of the prior follow state

#### Scenario: Active step enters an inline repair
- **WHEN** auto-follow is engaged and a check starts its inline repair agent
- **THEN** the check row shows `(repairing 1/1)`, its attempt children expand, and selection moves to the `repair 1` row

#### Scenario: Active step replays under a rerun
- **WHEN** auto-follow is engaged and a rerun replays `open-draft-pr`
- **THEN** selection moves to the replayed `open-draft-pr` row under `rerun 1` without changing the breadcrumb scope

#### Scenario: Recovery collapses to the check
- **WHEN** the replayed check passes
- **THEN** the check row shows `✓` with `(repaired 1/1)` and auto-follow moves to the next peer
