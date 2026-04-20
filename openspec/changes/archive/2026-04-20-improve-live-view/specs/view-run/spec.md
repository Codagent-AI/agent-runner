## MODIFIED Requirements

### Requirement: Detail pane per step type
The run view SHALL render a single continuous, scrollable log pane that stacks a detail block for every started step at the current drill-in level, in execution order. Sub-workflow and loop blocks SHALL contain their started children's blocks inline beneath the parent header, recursively at arbitrary depth. Selection SHALL NOT swap the pane's content; it scrolls the pane so the selected step's block is visible.

Each block SHALL open with a header containing the step name and its type glyph, and SHALL contain the same content contract previously rendered on selection:

- **Shell**: interpolated command, exit code, duration, captured-variable name if `capture:` is set, full stdout and stderr (distinguishable).
- **Headless agent**: agent profile, model, CLI, resolved session ID, interpolated prompt, exit code, duration, full stdout and stderr, resume action.
- **Interactive agent**: agent profile, model, CLI, session ID, interpolated prompt, outcome, duration, resume action.
- **Sub-workflow**: resolved workflow path, interpolated params, outcome, duration. Children's blocks render inline beneath this header; no "drill in" hint appears because the content is already inline.
- **Loop**: loop type (counted or for-each), iteration counter `(N/M)`, iterations completed, break_triggered, outcome, duration. Each started iteration renders as a block inline beneath this header, with that iteration's children inline beneath the iteration block; no "drill in" hint appears.

#### Scenario: Shell step block
- **WHEN** a shell step has started and is rendered in the log
- **THEN** the log contains a block with the shell step's header (name, `$` glyph), interpolated command, exit code, duration, captured-variable name if any, and stdout/stderr

#### Scenario: Headless agent block
- **WHEN** a headless agent step has started
- **THEN** the log contains a block with profile, model, CLI, session ID, interpolated prompt, exit code, duration, stdout/stderr, and a resume action

#### Scenario: Interactive agent block
- **WHEN** an interactive agent step has started
- **THEN** the log contains a block with profile, model, CLI, session ID, interpolated prompt, outcome, duration, and a resume action

#### Scenario: Sub-workflow block contains children inline
- **WHEN** a sub-workflow step has started
- **THEN** the log contains a sub-workflow header (resolved path, params, outcome, duration) and the sub-workflow's started child steps are rendered as blocks inline beneath the header

#### Scenario: Loop block contains iterations inline
- **WHEN** a loop step has started
- **THEN** the log contains a loop header (type, counter, completed, break_triggered, outcome, duration) and each started iteration is rendered as a block inline beneath the header, with that iteration's children inline beneath the iteration block

#### Scenario: Pending step detail is suppressed unless selected
- **WHEN** a step with status `pending` exists and is not selected by the cursor
- **THEN** the log does NOT contain a block for it (pending blocks are covered by the separate "Temporary detail for selected pending step" requirement)

#### Scenario: Selecting a step scrolls log to its block
- **WHEN** the user selects a step in the step list whose block is not currently in the viewport
- **THEN** the log scrolls so that step's block is in view; the log's content is not replaced

### Requirement: Drill-in navigation with breadcrumbs
The run view SHALL support drilling into sub-workflows and loops via a drill-in model. Enter on a drillable row SHALL scope both the step list AND the log to that container's subtree: the step list shows that container's children, and the log shows those children's blocks (with descendants inline). A breadcrumb line at the top SHALL show the current depth path (run name, then each entered container in order).

Drillable rows SHALL be: sub-workflow steps and loop steps. Drill-in SHALL be available on `pending` containers (children read from the workflow file or resolved statically) as well as executed ones.

#### Scenario: Top-level breadcrumb rendering
- **WHEN** the run view is at the top level (no drill-in)
- **THEN** the breadcrumb shows the workflow's canonical runnable name, the start time, and the run status (active/failed/completed/inactive)

#### Scenario: Enter on sub-workflow drills in and scopes log
- **WHEN** the user presses Enter on a sub-workflow step row
- **THEN** the step list is replaced by the sub-workflow's children, the log is scoped to show only that sub-workflow's children's blocks (and their descendants inline), and the breadcrumb appends the sub-workflow entry

#### Scenario: Enter on loop drills into iteration list and scopes log
- **WHEN** the user presses Enter on a loop step row
- **THEN** the step list is replaced by a list of iterations, the log is scoped to show only that loop's iteration blocks (and their descendants inline), and the breadcrumb appends the loop entry

#### Scenario: Enter on iteration drills into iteration children
- **WHEN** the user presses Enter on an iteration row in the iteration list
- **THEN** the step list is replaced by that iteration's child steps, the log is scoped to show only those children's blocks (and their descendants inline), and the breadcrumb appends the iteration identifier

#### Scenario: Drill in to pending sub-workflow
- **WHEN** the user presses Enter on a sub-workflow step that has not yet executed
- **THEN** the sub-workflow file is read and its children are displayed with status `pending`; the log contains no blocks at that level (pending steps are hidden from the log)

#### Scenario: Enter on shell step is a no-op
- **WHEN** the user presses Enter on a shell step row
- **THEN** nothing happens (shell steps are neither drillable nor resumable)

#### Scenario: Enter on agent step without session ID is a no-op
- **WHEN** the user presses Enter on an agent step that has no resolved session ID
- **THEN** nothing happens (the resume action requires a session ID)

### Requirement: Keyboard focus and scrolling
The step list SHALL always own the up/down arrow keys for step navigation. The log pane SHALL scroll via `j` (down) and `k` (up) and via the mouse wheel. Focus SHALL not need to be switched between panes. `PgUp`/`PgDown` SHALL NOT be bound.

The step list and the log SHALL share a single scrolling relationship:

- Arrow-key step-list navigation SHALL scroll the log so the newly selected step's block is visible.
- Scrolling the log via `j`/`k` or mouse wheel SHALL update the step-list cursor to the latest (furthest-down in execution order) started step that has any content currently visible in the log viewport.
- When the latest in-viewport started step is nested below the current drill-in level (its ancestors are visible in the log but the step itself is not a direct child of the current level), the cursor SHALL be set to that step's ancestor-at-current-level.

#### Scenario: Up/down navigates step list and scrolls log
- **WHEN** the user presses `↑` or `↓`
- **THEN** the step list selection moves one row in that direction and the log scrolls so the newly selected step's block is in view

#### Scenario: j/k scrolls log and updates cursor
- **WHEN** the user presses `j` or `k`
- **THEN** the log scrolls one line down (`j`) or up (`k`) and the step-list cursor updates to the latest started step with content in the viewport

#### Scenario: Mouse wheel scrolls log and updates cursor
- **WHEN** the user scrolls the mouse wheel anywhere in the view
- **THEN** the log scrolls and the step-list cursor updates to the latest started step with content in the viewport

#### Scenario: Cursor maps nested step to ancestor-at-current-level
- **WHEN** the log viewport shows content belonging to a step that is nested below the current drill-in level (e.g., the user is at the top level and a sub-workflow's grandchild is visible)
- **THEN** the step-list cursor highlights the top-level ancestor of that step (not the nested step itself)

#### Scenario: Cursor follows latest step in viewport
- **WHEN** multiple started steps' blocks are visible in the log viewport simultaneously
- **THEN** the step-list cursor highlights the step whose execution order is the latest (furthest down) among those whose content is visible

#### Scenario: PgUp and PgDown are not bound
- **WHEN** the user presses `PgUp` or `PgDown`
- **THEN** nothing happens (the keys are not bound; neither the step list nor the log reacts)

### Requirement: Live refresh for active runs
While the viewed run is active, the run view SHALL poll run state every 2 seconds (matching the list TUI cadence) and re-render when state changes. On each poll the run view SHALL re-check `run.lock` (for active status and blink gating) and tail `audit.log` (reading only bytes appended since the last poll, buffering any partial trailing line). Inactive runs SHALL render once and remain static until user input.

While active and auto-follow is engaged (auto-follow is engaged by default on entering the run view, and re-engaged by pressing `l`), the step-list cursor SHALL be set to the top-level ancestor of the currently active step at the current drill-in level. Auto-follow SHALL NOT drill into sub-workflows, loops, or iterations. Any manual user navigation (arrow keys, `j`/`k`, mouse wheel) SHALL disengage auto-follow.

#### Scenario: Active run refreshes on interval
- **WHEN** the viewed run is active
- **THEN** the run view polls state on the same interval as the list TUI and re-renders on any change

#### Scenario: Inactive run does not poll
- **WHEN** the viewed run is not active
- **THEN** the run view renders once and does not poll

#### Scenario: Missing or empty audit log
- **WHEN** the run's `audit.log` file is missing or empty
- **THEN** the step list is populated from the workflow file with every row in `pending`; no error is shown

#### Scenario: Auto-follow tracks active step at top level
- **WHEN** the run is active, auto-follow is engaged, and the active step advances into a nested sub-workflow or loop iteration
- **THEN** the step-list cursor is set to the top-level ancestor of the active step at the current drill-in level; no drill-in occurs

#### Scenario: Manual navigation disengages auto-follow
- **WHEN** the user presses an arrow key, `j`, `k`, or scrolls the mouse wheel while auto-follow is engaged
- **THEN** auto-follow disengages; the cursor stops tracking the active step

#### Scenario: Pressing l re-engages auto-follow
- **WHEN** the user presses `l` while auto-follow is disengaged
- **THEN** auto-follow re-engages and the cursor snaps back to the ancestor of the active step at the current drill-in level

## ADDED Requirements

### Requirement: Pending steps hidden from log
Steps with status `pending` SHALL NOT appear as blocks in the log except when selected by the cursor (see "Temporary detail for selected pending step"). Pending steps SHALL remain visible in the step list so the user knows they exist.

#### Scenario: Pending top-level step absent from log
- **WHEN** a top-level step has status `pending` and is not currently selected by the cursor
- **THEN** the log contains no block for that step, and the step list shows the step with its pending status indicator

#### Scenario: Pending iteration absent from log
- **WHEN** a loop has started but some iterations are still pending and are not selected by the cursor
- **THEN** the loop's block in the log contains only iteration sub-blocks for started iterations; pending iterations have no sub-block

#### Scenario: Pending child of started sub-workflow absent from log
- **WHEN** a sub-workflow has started and some of its child steps are still pending and not selected
- **THEN** the sub-workflow block contains inline sub-blocks only for started children; pending children have no sub-block

### Requirement: Temporary detail for selected pending step
When the user moves the step-list cursor onto a pending step, a temporary detail block SHALL be rendered in the log at that step's would-be position (preserving execution order), showing only statically knowable fields: step name, type, and configured command/prompt/sub-workflow path/params (as raw template strings, not interpolated). The block SHALL be visually distinguished from real blocks (e.g., dimmed color or dashed separator). The temporary block SHALL disappear when the cursor leaves that step.

#### Scenario: Temporary block appears on pending selection
- **WHEN** the user selects a pending step in the step list
- **THEN** a temporary detail block appears in the log at that step's execution-order position, showing only statically knowable fields; no runtime fields (exit code, output, duration) are shown

#### Scenario: Temporary block disappears on deselection
- **WHEN** the user moves the cursor off a pending step onto any other step
- **THEN** the temporary detail block is removed from the log

#### Scenario: Temporary block is visually distinguished
- **WHEN** a temporary block is rendered in the log
- **THEN** the block's visual treatment (e.g., dim color or dashed separator) makes clear it is not a real executed block

#### Scenario: Pending sub-workflow shows raw params
- **WHEN** the user selects a pending sub-workflow step
- **THEN** the temporary block shows the resolved workflow path and each param as its raw template string (e.g., `task_file = {{task_file}}`)

### Requirement: Cross-step auto-scroll while run is active
While the viewed run is active, any new content appended to the log (a new step block, new output streamed into an existing block, a new iteration) SHALL auto-scroll the log so the newly appended content is visible. Auto-scroll SHALL apply unconditionally regardless of the user's current scroll position.

#### Scenario: New step starting auto-scrolls
- **WHEN** a new step starts while the run is active
- **THEN** the log auto-scrolls so that step's newly created block is visible

#### Scenario: Streaming output auto-scrolls
- **WHEN** a running step streams new stdout or stderr
- **THEN** the log auto-scrolls so the newly streamed content is visible

#### Scenario: Auto-scroll overrides user scroll-up
- **WHEN** the user has scrolled up to view earlier output and new content is appended to the log
- **THEN** the log auto-scrolls to the newly appended content, even though the user had scrolled away from the bottom

#### Scenario: No auto-scroll when run is inactive
- **WHEN** the viewed run is not active
- **THEN** no auto-scroll occurs; the user's scroll position is preserved

### Requirement: Step list recursive expansion under selected step
The step list SHALL show an inline read-only expansion only under the currently selected step. Expansion SHALL recurse through every nesting level (sub-workflow → loop → iteration → sub-workflow → …) down to the deepest active descendant (the currently running descendant, or the last completed descendant if nothing is running). Non-selected ancestors and siblings SHALL render as a single collapsed row each. When selection changes, the previous expansion SHALL collapse and the new selection's expansion SHALL be rendered. Arrow-key navigation SHALL move selection only among the current drill-in level's steps and SHALL skip expansion rows (expansion is read-only).

#### Scenario: Expansion recurses to deepest active descendant
- **WHEN** the selected step is a sub-workflow whose active descendant is several nesting levels deep (e.g., sub-workflow → loop iteration → sub-workflow → agent step)
- **THEN** the step list renders an indented row for each intermediate level down to the deepest active descendant

#### Scenario: Non-selected container collapses
- **WHEN** the user moves selection off an expanded container onto another step
- **THEN** the previously expanded container collapses to a single row, and the newly selected step expands recursively under itself (if it has active descendants)

#### Scenario: Selected step with no active descendant
- **WHEN** the selected step is a leaf step, or a container whose descendants are all pending
- **THEN** no expansion is rendered; the selected step occupies a single row like any other

#### Scenario: Expansion rows are read-only
- **WHEN** the user presses an arrow key while the cursor is on a container with an inline expansion
- **THEN** the cursor moves to the next or previous direct child of the current drill-in level, skipping the expansion rows; to interact with nested content the user drills in with Enter

### Requirement: Recursive log nesting with progressive separators
Log blocks for sub-workflows and loops SHALL contain their children's blocks inline beneath their header, recursively at arbitrary depth. Nesting SHALL be visually conveyed by indentation and by using progressively lighter separator characters as depth increases. Exact separator glyphs and indent widths are design decisions, but separator weight MUST decrease monotonically with depth until reaching a floor style at which further-nested blocks keep the same weight.

#### Scenario: Nested sub-workflow children indented under parent
- **WHEN** a sub-workflow block is rendered with started children
- **THEN** each child's block is indented beneath the sub-workflow header and uses a lighter separator than the parent

#### Scenario: Loop iterations indented under loop header
- **WHEN** a loop block is rendered with started iterations
- **THEN** each iteration's block is indented beneath the loop header and uses a lighter separator than the loop header; each iteration's children are indented further still with an even lighter separator

#### Scenario: Separators remain distinguishable at typical depth
- **WHEN** log nesting reaches up to 3 levels (e.g., sub-workflow → loop iteration → child step)
- **THEN** the separator weight at each level is visually distinct from its parent and child levels

#### Scenario: Separator weight floors at maximum depth
- **WHEN** log nesting exceeds the floor depth (e.g., 4 or more levels deep)
- **THEN** all blocks at the floor depth and beyond use the same floor separator style (no further reduction in weight)
