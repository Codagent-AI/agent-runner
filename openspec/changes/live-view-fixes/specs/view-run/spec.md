## MODIFIED Requirements

### Requirement: Step list rendering
The run view SHALL render the current level's steps as a vertical list on the left. Each row SHALL display, in order: a status indicator, the step name, and the type glyph to the right of the name. Loop step rows SHALL additionally display an iteration counter in the form `(N/M)` after the name.

If a step name exceeds 20 visual characters, the sidebar row SHALL truncate the displayed name to the first 17 characters followed by an ellipsis (`…`). Log block separators are unaffected by this truncation and continue to render the full name.

Step types distinguished by glyph SHALL be: shell, headless agent, interactive agent, sub-workflow, and loop. Every step type SHALL have a type glyph.

Step statuses SHALL be: `pending`, `in-progress`, `success`, `failed`, `skipped`. The `in-progress` indicator SHALL blink only when a run is currently active; otherwise it renders static (covering steps that were aborted mid-execution and will resume on the next run). Loop "exhausted" outcomes SHALL render as `success`.

Status glyphs SHALL be: `●` running, `○` pending, `✓` success, `✗` failed, `⇥` skipped. Type glyphs SHALL be: `$` shell, ⚙️ headless agent, 💬 interactive agent, ↳ sub-workflow, and a distinct glyph for loop (exact symbol is a design choice). Pulse cadence for the running indicator SHALL match the list TUI's 50 ms tick, lerping between the running and dim-running color tokens. Color palette additions (failed red) and reuses are specified in the design document.

#### Scenario: Shell step row
- **WHEN** a shell step is rendered in the step list
- **THEN** the row shows a status indicator, the step name, and the shell type glyph to the right of the name

#### Scenario: Loop step row shows iteration counter and type glyph
- **WHEN** a loop step with `max: 5` has completed 3 iterations
- **THEN** the row shows a status indicator, the step name, the loop type glyph, and `(3/5)` after the name

#### Scenario: For-each loop row shows iteration counter and type glyph
- **WHEN** a for-each loop has resolved to 4 matches and completed 2 iterations
- **THEN** the row shows a status indicator, the step name, the loop type glyph, and `(2/4)` after the name

#### Scenario: Active step blinks
- **WHEN** a step is currently executing and the run is active
- **THEN** the step's status indicator blinks

#### Scenario: Aborted step does not blink when no run is active
- **WHEN** a step was interrupted by an earlier run and no run is currently active
- **THEN** the step's status indicator shows `in-progress` without blinking

#### Scenario: Pending steps from workflow file before execution
- **WHEN** the run has no audit entries yet but the workflow file is known
- **THEN** the step list is populated from the workflow file with every row in `pending`

#### Scenario: Long step name truncated in sidebar
- **WHEN** a step name longer than 20 visual characters is rendered in the step list
- **THEN** the sidebar row displays the first 17 characters of the name followed by `…`

#### Scenario: Long step name not truncated in log separator
- **WHEN** a step with a name longer than 20 characters has a block rendered in the log
- **THEN** the log block's separator displays the full untruncated step name

### Requirement: Step list inline expansion of direct children under selected step
The step list SHALL show an inline read-only expansion only under the currently selected step, and only when that step is a container (sub-workflow, loop, or iteration). Expansion SHALL list the container's **direct children only** — it SHALL NOT recurse further into grandchildren.

- For a selected **sub-workflow**, the expansion SHALL list every direct child step of that sub-workflow (whether started, pending, or completed).
- For a selected **loop**, the expansion SHALL list each iteration as a row with the iteration identifier and its status. Iteration expansion rows SHALL NOT display loop binding values, per-iteration parameters, or arguments.
- For a selected **iteration**, the expansion SHALL list every direct child step of that iteration.
- For a selected leaf step (or a container with no children to list), no expansion is rendered.

Expansion rows SHALL be visually indented to a positive offset under the selected parent. Non-selected ancestors and siblings SHALL render as a single collapsed row each. When selection changes, the previous expansion SHALL collapse and the new selection's expansion SHALL be rendered. Arrow-key navigation SHALL move selection only among the current drill-in level's steps and SHALL skip expansion rows (expansion is read-only).

Iteration rows SHALL NEVER display parameter or binding-value text — this rule applies whether the iteration appears as a direct entry in the step list (after drilling into a loop), as an inline expansion row under a selected loop, or anywhere else in the sidebar.

#### Scenario: Selected sub-workflow expands to its direct children only
- **WHEN** the selected step is a sub-workflow whose first child is itself a sub-workflow containing further nested steps
- **THEN** the step list renders one indented row for each direct child of the selected sub-workflow, and does NOT render any grandchild rows

#### Scenario: Selected loop expands to iteration rows without params
- **WHEN** the selected step is a loop that has started some iterations
- **THEN** the step list renders one indented row per iteration, showing the iteration identifier and status; no row displays binding values, per-iteration parameters, or arguments

#### Scenario: Expansion indent is positive under parent
- **WHEN** a container's inline expansion rows are rendered
- **THEN** each expansion row is indented to the right of the selected parent row (never outdented or at the same or lesser indent level than the parent)

#### Scenario: Non-selected container collapses
- **WHEN** the user moves selection off an expanded container onto another step
- **THEN** the previously expanded container collapses to a single row, and the newly selected step expands (if it is a container with children)

#### Scenario: Selected leaf step has no expansion
- **WHEN** the selected step is a leaf (shell or agent step), or a container with zero children to list
- **THEN** no expansion is rendered; the selected step occupies a single row like any other

#### Scenario: Expansion rows are read-only
- **WHEN** the user presses an arrow key while the cursor is on a container with an inline expansion
- **THEN** the cursor moves to the next or previous direct child of the current drill-in level, skipping the expansion rows; to interact with nested content the user drills in with Enter

#### Scenario: Drilled-in iteration row hides params
- **WHEN** the user has drilled into a loop and the step list shows iteration rows directly
- **THEN** no iteration row displays binding values, per-iteration parameters, or arguments
