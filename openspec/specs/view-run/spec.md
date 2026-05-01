# view-run Specification

## Purpose
TBD - created by archiving change view-run. Update Purpose after archive.
## Requirements
### Requirement: Run-view entry points
The CLI SHALL provide two entry points to the run view: a `--inspect <run-id>` flag for direct entry, and an Enter action from the list TUI (covered by the `list-runs` delta). Direct entry SHALL require a full run ID (no prefix matching). When the target run's run-lock is held by another live process, `--inspect` SHALL reject the entry with an error and not launch the TUI.

#### Scenario: --inspect launches run view
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the run exists and is not locked by another live process
- **THEN** the run-view TUI launches for that run

#### Scenario: --inspect with unknown run ID
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the run does not exist
- **THEN** agent-runner prints an error message naming the missing run ID and exits with a non-zero status

#### Scenario: --inspect requires full run ID
- **WHEN** `agent-runner --inspect <prefix>` is invoked with a prefix that is not a complete run ID
- **THEN** agent-runner treats it as "not found" and exits non-zero

#### Scenario: --inspect is mutually exclusive with --list and --resume
- **WHEN** `agent-runner --inspect <run-id>` is invoked together with `--list` or `--resume`
- **THEN** agent-runner prints an error indicating the flags are mutually exclusive and exits non-zero

#### Scenario: --inspect rejects a run locked by another process
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the target run's run-lock belongs to another live process
- **THEN** agent-runner prints an error to stderr identifying the run as active in another process and exits non-zero; no TUI is launched

#### Scenario: --inspect proceeds past a stale lock
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the target run's run-lock PID is dead
- **THEN** the lock is treated as stale and the run-view TUI launches normally

### Requirement: Step list rendering
The run view SHALL render the current level's steps as a vertical list on the left. Each row SHALL display, in order: a status indicator, the step name, and the type glyph to the right of the name. Loop step rows SHALL additionally display an iteration counter in the form `(N/M)` after the name.

If a step name exceeds 20 visual characters, the sidebar row SHALL truncate the displayed name to the first 17 characters followed by an ellipsis (`…`). Log block separators are unaffected by this truncation and continue to render the full name.

Step types distinguished by glyph SHALL be: shell, headless agent, interactive agent, sub-workflow, loop, and iteration. Every step type SHALL have a type glyph — including iteration rows, which render their own glyph so expansion rows under a selected loop line up with other typed rows rather than appearing offset from them.

Step statuses SHALL be: `pending`, `in-progress`, `success`, `failed`, `skipped`. The `in-progress` indicator SHALL blink only when a run is currently active; otherwise it renders static (covering steps that were aborted mid-execution and will resume on the next run). Loop "exhausted" outcomes SHALL render as `success`.

When a selected container step (sub-workflow, loop, or iteration) has inline expansion rows rendered beneath it AND at least one of those expansion rows is itself `in-progress`, the selected parent's status indicator SHALL be suppressed (rendered as blank whitespace preserving column alignment). This avoids presenting two simultaneous in-progress indicators for what is conceptually one active execution frontier; the expansion row for the actively running child carries the sole indicator. The step name, type glyph, and any counter on the parent row SHALL continue to render normally.

Status glyphs SHALL be: `●` running, `○` pending, `✓` success, `✗` failed, `⇥` skipped. Type glyphs SHALL be: `$` shell, ⚙️ headless agent, 💬 interactive agent, ↳ sub-workflow, and distinct glyphs for loop and iteration (exact symbols are design choices, but loop and iteration MAY share a color token to signal their relationship). Pulse cadence for the running indicator SHALL match the list TUI's 50 ms tick, lerping between the running and dim-running color tokens. Color palette additions (failed red) and reuses are specified in the design document.

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

#### Scenario: Selected container with active child suppresses its own indicator
- **WHEN** the selected step is a container (sub-workflow, loop, or iteration) that is itself `in-progress` and its inline expansion rows include at least one child whose status is also `in-progress`
- **THEN** the parent row's status indicator is rendered as blank whitespace of the same column width as a status glyph, and only the active expansion child's row displays an `in-progress` indicator

#### Scenario: Selected container with no active child keeps its indicator
- **WHEN** the selected step is an `in-progress` container whose expansion rows contain no `in-progress` child (e.g. a loop between iterations, or one whose current iteration is pending)
- **THEN** the parent row's `in-progress` status indicator renders normally (blinking during an active run)

#### Scenario: Pending steps from workflow file before execution
- **WHEN** the run has no audit entries yet but the workflow file is known
- **THEN** the step list is populated from the workflow file with every row in `pending`

#### Scenario: Long step name truncated in sidebar
- **WHEN** a step name longer than 20 visual characters is rendered in the step list
- **THEN** the sidebar row displays the first 17 characters of the name followed by `…`

#### Scenario: Long step name not truncated in log separator
- **WHEN** a step with a name longer than 20 characters has a block rendered in the log
- **THEN** the log block's separator displays the full untruncated step name

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

### Requirement: Sub-workflow header inside drill-in
When the user is drilled inside a sub-workflow, a header SHALL be displayed above the step list showing the resolved sub-workflow path and the interpolated params that were (or will be) passed to it.

#### Scenario: Header shown inside sub-workflow
- **WHEN** the user has drilled into a sub-workflow step
- **THEN** a header above the step list shows the resolved workflow path and the interpolated params

#### Scenario: Header shown for pending sub-workflow
- **WHEN** the user drills into a sub-workflow that has not yet executed
- **THEN** the header shows the resolved path (as a canonical runnable name when under `workflows/`, else a repo-relative path) and each param as its raw template string (e.g., `task_file = {{task_file}}`)

### Requirement: Auto-flatten loop iteration with single sub-workflow child
When a loop iteration's body is exactly one step and that step is a sub-workflow, Enter on the iteration row SHALL skip that degenerate level and drill directly into the sub-workflow's children. The breadcrumb SHALL display only the iteration entry (not the skipped sub-workflow step); the sub-workflow's path and params SHALL appear in the sub-workflow header above the step list.

#### Scenario: Loop iteration with single sub-workflow child auto-flattens
- **WHEN** a loop iteration's body contains exactly one step and that step has a `workflow:` field, and the user presses Enter on the iteration row
- **THEN** the view drills past the single sub-workflow step and shows the sub-workflow's children directly

#### Scenario: Breadcrumb hides the skipped step
- **WHEN** auto-flatten has drilled past a single sub-workflow step inside a loop iteration
- **THEN** the breadcrumb shows the iteration entry as the deepest crumb; the sub-workflow's path and params appear in the header

#### Scenario: Single-step iteration that is not a sub-workflow is not flattened
- **WHEN** a loop iteration's body contains exactly one step and that step is not a sub-workflow (e.g., a shell step)
- **THEN** Enter on the iteration row drills into the normal iteration-children view (showing the single step)

### Requirement: Detail pane per step type
The run view SHALL render a single continuous, scrollable log pane that stacks a detail block for every started step at the current drill-in level, in execution order. Sub-workflow and loop blocks SHALL contain their started children's blocks inline beneath the parent header, recursively at arbitrary depth. Selection SHALL NOT swap the pane's content; it scrolls the pane so the selected step's block is visible.

Each block SHALL open with a header containing the step name and its type glyph, and SHALL contain the same content contract previously rendered on selection:

- **Shell**: interpolated command, exit code, duration, captured-variable name if `capture:` is set, full stdout and stderr (distinguishable).
- **Headless agent**: agent profile, CLI, model, resolved session ID, interpolated prompt, exit code, duration, full stdout and stderr, resume action. The header lines SHALL render in the order: profile, CLI, model, session strategy, session ID. The `model:` line SHALL always be present on a started agent step; when no model can be resolved (no step-level override and no profile default available), the value SHALL render as `(unknown)`.
- **Interactive agent**: agent profile, CLI, model, session ID, interpolated prompt, outcome, duration, resume action. The header-line ordering and always-shown `model:` rule (including the `(unknown)` fallback) match the headless-agent block.
- **Sub-workflow**: resolved workflow path, interpolated params, outcome, duration. Children's blocks render inline beneath this header; no "drill in" hint appears because the content is already inline.
- **Loop**: loop type (counted or for-each), iteration counter `(N/M)`, iterations completed, break_triggered, outcome, duration. Each started iteration renders as a block inline beneath this header, with that iteration's children inline beneath the iteration block; no "drill in" hint appears.

#### Scenario: Shell step block
- **WHEN** a shell step has started and is rendered in the log
- **THEN** the log contains a block with the shell step's header (name, `$` glyph), interpolated command, exit code, duration, captured-variable name if any, and stdout/stderr

#### Scenario: Headless agent block
- **WHEN** a headless agent step has started
- **THEN** the log contains a block with profile, CLI, model, session ID, interpolated prompt, exit code, duration, stdout/stderr, and a resume action

#### Scenario: Interactive agent block
- **WHEN** an interactive agent step has started
- **THEN** the log contains a block with profile, CLI, model, session ID, interpolated prompt, outcome, duration, and a resume action

#### Scenario: Agent block header order places model under CLI
- **WHEN** a headless or interactive agent block is rendered
- **THEN** the `model:` line appears immediately below the `cli:` line in the block header (and the `cli:` line itself appears immediately below the `agent:` profile line when a profile is present)

#### Scenario: Agent block shows model for steps without an inline override
- **WHEN** an agent step relies on its profile's default model (no `model:` set on the step)
- **THEN** the block's `model:` line shows the profile's default model value, not an empty or missing line

#### Scenario: Agent block shows model for a resumed or inherited session
- **WHEN** an agent step uses `session: resume` or `session: inherit`, reusing the CLI session of an earlier step
- **THEN** the block's `model:` line shows the model that was used to launch the CLI (sourced from the profile of the session-originating step, with any step-level override applied)

#### Scenario: Agent block shows unknown model as explicit fallback
- **WHEN** an agent step has started but no model can be resolved (no step-level override and no profile default available)
- **THEN** the block's `model:` line renders with the value `(unknown)` rather than being omitted

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

### Requirement: Large output lazy loading
Shell stdout or stderr exceeding 2000 lines or 256 KB (whichever comes first) SHALL be rendered with the tail portion only, together with a persistent banner stating the total and current shown line counts and indicating that the `g` key loads the full output on demand.

#### Scenario: Large output shows tail with load hint
- **WHEN** a shell step's captured output exceeds the threshold
- **THEN** the detail pane shows the tail of the output and a visible hint describing the key to load the full output

#### Scenario: Load-full key expands output
- **WHEN** the user presses the load-full key while viewing a truncated output
- **THEN** the detail pane loads and displays the full output

### Requirement: Non-UTF8 output handling
Non-UTF8 bytes in shell stdout or stderr SHALL be rendered by replacing invalid byte sequences with the Unicode replacement character (U+FFFD) before display.

#### Scenario: Invalid bytes replaced
- **WHEN** a shell step's captured output contains non-UTF8 byte sequences
- **THEN** the detail pane renders the output with invalid sequences replaced by U+FFFD, leaving valid text intact

### Requirement: Resume action from run view
Selecting the resume action on an agent step (headless or interactive) SHALL spawn the step's agent CLI with `--resume <session-id>` as a subprocess and hand the terminal to it. This is NOT the same as agent-runner's `--resume <run-id>` flag: the runview resume action targets the individual Claude/Codex/etc. session captured on the step, identified by the CLI's own session ID (e.g. `claude --resume <uuid>`), not an agent-runner workflow run.

When the spawned CLI exits (for any reason, including the user typing `/exit` or `/quit`), agent-runner SHALL re-enter the run view for the same run, re-reading audit and state files so events produced by the resumed session appear. Re-entry preserves the original entry path so back-navigation (e.g. esc to the run list) still works. This behavior applies regardless of how the run view was reached (live-run completion, `--list`, or `--inspect`).

#### Scenario: Resume from headless agent step
- **WHEN** the user triggers the resume action on a headless agent step with a known session ID
- **THEN** the step's agent CLI is spawned as a subprocess with `--resume <session-id>` (e.g. `claude --resume <uuid>`) and the terminal is handed to it
- **AND WHEN** that CLI process exits
- **THEN** agent-runner re-enters the run view for the same run, with audit and state re-read so any new events from the resumed session appear

#### Scenario: Resume from interactive agent step
- **WHEN** the user triggers the resume action on an interactive agent step with a known session ID
- **THEN** the step's agent CLI is spawned as a subprocess with `--resume <session-id>` and the terminal is handed to it
- **AND WHEN** that CLI process exits
- **THEN** agent-runner re-enters the run view for the same run

#### Scenario: User exits resumed CLI with /exit or /quit
- **WHEN** the user has resumed an agent CLI session from the run view and types `/exit` or `/quit` inside that CLI
- **THEN** the CLI process exits and agent-runner returns to the run view rather than exiting the agent-runner process

#### Scenario: Resume unavailable without session ID
- **WHEN** an agent step has no resolved session ID (never started, or crashed before session creation)
- **THEN** the resume action is not available for that step

#### Scenario: Resume unavailable while run is active
- **WHEN** the viewed run is active (either the live-run TUI is still executing the workflow or the run lock is active)
- **AND** the selected agent step already has a resolved session ID
- **THEN** the resume action is not available, and pressing Enter on that agent step does nothing

#### Scenario: Spawn failure
- **WHEN** the user triggers the resume action and the agent CLI cannot be spawned (e.g. binary not found on PATH)
- **THEN** agent-runner does not exit; it returns to the run view and surfaces the spawn error to the user

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

### Requirement: Legend overlay
The run view SHALL provide a `?` key that toggles a modal legend overlay showing status glyph meanings and type glyph meanings. The overlay SHALL be dismissible with `?` or Escape.

#### Scenario: Toggle legend overlay on
- **WHEN** the user presses `?` and the legend is not visible
- **THEN** a modal overlay appears showing status glyphs (`●` running, `○` pending, `✓` success, `✗` failed, `⇥` skipped) and type glyphs (`$` shell, ⚙️ headless agent, 💬 interactive agent, ↳ sub-workflow, the loop glyph, and the iteration glyph)

#### Scenario: Toggle legend overlay off
- **WHEN** the user presses `?` or Escape while the legend overlay is visible
- **THEN** the overlay is dismissed and the normal view is restored

### Requirement: Exit behavior
The run view SHALL support two exit mechanisms. Escape SHALL navigate up one breadcrumb level; at the top level Escape SHALL return to the list TUI (if that's how the view was entered) or exit the program (if entered via `--inspect`). The `q` key SHALL unconditionally exit the program regardless of depth.

#### Scenario: Escape drills out one level
- **WHEN** the user presses Escape while drilled inside a sub-workflow, loop, or iteration
- **THEN** the view returns to the parent level and the breadcrumb drops its last entry

#### Scenario: Escape at top level returns to list
- **WHEN** the user presses Escape at the top level of a run view entered from the list TUI
- **THEN** the run view exits and the list TUI is shown

#### Scenario: Escape at top level exits program when launched via --inspect
- **WHEN** the user presses Escape at the top level of a run view launched via `--inspect`
- **THEN** the program exits

#### Scenario: q or Ctrl+C exits program
- **WHEN** the user presses `q` or `Ctrl+C` at any depth
- **THEN** the program exits immediately

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

### Requirement: In-progress agent progress indicator in the log
A headless or interactive agent block SHALL display a visible progress indicator while its step status is `in-progress`, regardless of whether the step has produced output yet. The indicator SHALL appear in the block's body (below any already-rendered output), so that a user viewing the block always has a motion cue that the agent is still working.

When no output has been produced, the indicator SHALL occupy its own multi-line region at the position the `agent:` body would otherwise begin. Once output has started streaming, the indicator SHALL render as a single-character animated glyph on a line positioned below the streamed output. The exact glyph set and color token are design decisions, but the indicator MUST be visually distinct from static output text.

When the step transitions out of `in-progress` (to `success`, `failed`, `skipped`, or becomes aborted with no active run), the indicator SHALL be removed from the block.

#### Scenario: Spinner shown while agent has not produced output
- **WHEN** an agent step is in progress and has produced no stdout or stderr yet
- **THEN** the log block shows an animated progress indicator in place of the `agent:` output region

#### Scenario: Spinner shown below streaming output
- **WHEN** an agent step is in progress and has produced at least one line of output
- **THEN** the log block shows the streamed output followed by a single-character animated progress indicator on a line below it

#### Scenario: Spinner removed on step completion
- **WHEN** an in-progress agent step transitions to `success`, `failed`, or `skipped`
- **THEN** the log block no longer shows a progress indicator

#### Scenario: Spinner absent for aborted step without active run
- **WHEN** an agent step was interrupted by an earlier run and no run is currently active
- **THEN** the log block shows no animated progress indicator (matching the step-list rule that `in-progress` does not blink outside an active run)
