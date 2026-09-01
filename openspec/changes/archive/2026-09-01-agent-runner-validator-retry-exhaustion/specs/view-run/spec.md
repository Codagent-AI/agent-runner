## MODIFIED Requirements

### Requirement: Step list rendering

The run view SHALL render the current manual drill scope as a selectable workflow tree on the left and the selected row's detail on the right. The panes SHALL be separated by whitespace; the run view SHALL NOT render a full-height vertical separator between them.

Each real tree row SHALL display, in order: indentation for its tree depth, a status indicator, the step name, and the type glyph. Loop rows SHALL additionally display an iteration counter in the form `(N/M)`. Iteration rows SHALL NOT display binding values, per-iteration parameters, or arguments.

Step statuses SHALL be `pending`, `in-progress`, `success`, `warning`, `failed`, and `skipped`. The deepest visible active row's `in-progress` indicator SHALL blink only while the run is active. Visible in-progress ancestor containers SHALL retain a static in-progress indicator while the active leaf blinks. An interrupted step in an inactive run SHALL render a static in-progress indicator. Loop exhaustion SHALL render as success unless that exhaustion was explicitly classified as a non-blocking warning. An otherwise successful container with warning descendants SHALL visibly indicate warning ancestry without becoming an additional warning origin.

The sidebar SHALL measure complete visible rows, including indentation, before truncating names. A name SHALL remain untruncated whenever the preferred sidebar width and a usable detail pane fit within the terminal. When they do not fit, the sidebar SHALL truncate names with an ellipsis while preserving indentation, status, suffixes, and type glyphs. The detail pane SHALL retain at least 20 visual columns whenever the terminal can accommodate the minimum tree chrome, whitespace gap, and that width. The sidebar SHALL NOT have a proportional-width ceiling.

When the terminal is too narrow to fit minimum tree chrome, the whitespace gap, and 20 detail columns, layout SHALL first reduce the name to an ellipsis, then preserve fixed tree chrome for as long as it physically fits, and only then allow the detail pane to fall below 20 columns. If the terminal cannot fit even those fixed elements, rendering SHALL clip safely at the terminal boundary without negative dimensions or a panic.

Within one run-view entry, sidebar width MAY grow when a newly visible row needs more space but SHALL NOT shrink until the terminal is resized or the user exits and re-enters the run view. A terminal resize SHALL recompute the pane widths from the new available width.

Status glyphs SHALL remain `●` running, `○` pending, `✓` success, `✗` failed, and `⇥` skipped. Warning status SHALL use a distinct amber warning indicator. Every supported row type, including shell, script, UI, headless agent, interactive agent, agent call, sub-workflow, loop, iteration, and group, SHALL have a type glyph. Exact type glyphs, warning glyph shape, rail glyphs, colors other than the warning's amber distinction, padding, and whitespace are design decisions.

#### Scenario: Shell step row

- **WHEN** a shell step is rendered in the workflow tree
- **THEN** its row shows indentation, a status indicator, the step name, and the shell type glyph

#### Scenario: Loop step row shows iteration counter and type glyph

- **WHEN** a counted or for-each loop has completed 3 of 5 iterations
- **THEN** its row shows the loop type glyph and `(3/5)` after the name

#### Scenario: For-each loop row shows iteration counter and type glyph

- **WHEN** a for-each loop has resolved five matches and completed three iterations
- **THEN** its row shows the loop type glyph and `(3/5)` after the name

#### Scenario: Active step blinks

- **WHEN** an expanded active ancestry contains an in-progress leaf
- **THEN** the leaf displays the blinking running indicator and every visible in-progress ancestor displays a static running indicator

#### Scenario: Selected container with active child suppresses its own indicator

- **WHEN** a selected in-progress container has a visible in-progress child while the run is active
- **THEN** the child owns the blinking indicator while the container retains a static running indicator

#### Scenario: Selected container with no active child keeps its indicator

- **WHEN** a selected in-progress container has no visible in-progress child
- **THEN** its running indicator remains visible and blinks only while it is the deepest active row of an active run

#### Scenario: Stopped active ancestry retains status indicators

- **WHEN** a run stops while an expanded container and its visible descendant retain `in-progress` status
- **THEN** both rows display static running indicators

#### Scenario: Aborted step does not blink when no run is active

- **WHEN** a step retained `in-progress` status from an interrupted run and the run is no longer active
- **THEN** its indicator is static

#### Scenario: Pending steps from workflow file before execution

- **WHEN** the workflow definition is known but the run has no audit entries
- **THEN** the tree shows every defined top-level row with pending status

#### Scenario: Executed steps recovered when the workflow file is gone

- **WHEN** a saved run's workflow file is unavailable but audit history identifies executed top-level steps
- **THEN** the tree reconstructs those executed rows and does not report a missing-workflow error

#### Scenario: Missing workflow and audit history still reports an error

- **WHEN** neither the workflow definition nor recoverable audit history is available
- **THEN** the run view reports the missing-workflow error

#### Scenario: Long step name not truncated in log separator

- **WHEN** a row name exceeds 20 visual characters and the terminal can fit the complete measured tree row plus the minimum detail width
- **THEN** the complete name is shown without truncation

#### Scenario: Long step name truncated in sidebar

- **WHEN** the preferred sidebar and minimum detail pane do not fit together
- **THEN** the sidebar truncates the name with an ellipsis while retaining the row's indentation, status, and type glyph

#### Scenario: Panes have no full-height separator

- **WHEN** the two-pane run view is rendered
- **THEN** whitespace separates the tree and detail pane and no full-height vertical rule appears between them

#### Scenario: Sidebar grows for wider visible row

- **WHEN** a newly visible row needs more width and the terminal can provide it without violating the detail minimum
- **THEN** the sidebar grows to fit the row

#### Scenario: Sidebar does not shrink during same entry

- **WHEN** a wider row disappears after the sidebar has grown and the terminal has not been resized
- **THEN** the sidebar retains its established width

#### Scenario: Resize recomputes pane widths

- **WHEN** the terminal width changes
- **THEN** the tree and detail widths are recomputed from the new terminal width

#### Scenario: Sidebar is not capped at half the terminal

- **WHEN** a complete visible tree row needs more than half the terminal and it still fits beside the whitespace gap and a 20-column detail pane
- **THEN** the sidebar may grow beyond half the terminal so the complete row remains visible

#### Scenario: Extremely narrow terminal degrades deterministically

- **WHEN** the terminal cannot fit minimum tree chrome, the whitespace gap, and a 20-column detail pane
- **THEN** the name is already reduced to an ellipsis, fixed tree chrome is preserved while it fits, the detail width takes the unavoidable remaining reduction, and layout remains valid

#### Scenario: Warning origin uses warning indicator

- **WHEN** a step has terminal warning status
- **THEN** its row displays the distinct amber warning indicator while its detail retains the underlying outcome

#### Scenario: Collapsed ancestor reveals contained warning

- **WHEN** a warning origin is nested beneath a container whose children are not visible
- **THEN** the container visibly indicates that its ancestry contains a warning without increasing the run's warning count

## ADDED Requirements

### Requirement: Jump to warning origins

When a terminal run contains warning origins, the detailed run view SHALL advertise `w warnings`. Pressing `w` SHALL leave any manual drill scope, expand the selected warning's ancestry in root scope, and select the exact originating warning step rather than an ancestor container. Repeated presses SHALL cycle through origins in durable execution order and wrap from the last to the first. The behavior SHALL be available after live completion and when opening the saved run from the run list or through `--inspect`; it SHALL NOT be available while the workflow is still running.

#### Scenario: First warning jump

- **WHEN** a completed run contains warnings and the user presses `w`
- **THEN** the detailed view selects the first originating warning step in execution order and expands its ancestry

#### Scenario: Repeated warning jump cycles

- **WHEN** the selected run has multiple warning origins and the user repeatedly presses `w`
- **THEN** selection advances through each origin in execution order and wraps after the last

#### Scenario: Warning jump leaves manual drill scope

- **WHEN** the user is manually drilled into an unrelated container and presses `w`
- **THEN** the view returns to root manual scope and selects the next warning origin with its ancestry expanded

#### Scenario: Warning navigation is absent without warnings

- **WHEN** a run contains no terminal warning origins
- **THEN** the help bar does not advertise `w warnings` and pressing `w` has no effect

#### Scenario: Warning navigation waits for terminal run

- **WHEN** a warning origin has been recorded but the workflow is still running
- **THEN** the help bar does not advertise `w warnings` and pressing `w` has no effect

### Requirement: Warning detail remains diagnostic and non-blocking

Selected warning detail SHALL display status `warning`, the underlying failed or exhausted outcome, the execution's existing output and diagnostics, and a concise note that the workflow continued. It SHALL NOT require acknowledgment or offer automatic remediation.

#### Scenario: Validator warning detail

- **WHEN** the user jumps to an unresolved validator warning
- **THEN** the detail shows warning status, the final validator failure and output, and a note that workflow execution continued
