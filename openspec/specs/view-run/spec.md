# view-run Specification

## Purpose
TBD - created by archiving change view-run. Update Purpose after archive.
## Requirements
### Requirement: Run-view entry points
The CLI SHALL provide two entry points to the run view: a new `--inspect <run-id>` flag for direct entry, and an Enter action from the list TUI (covered by the `list-runs` delta). Direct entry SHALL require a full run ID (no prefix matching).

#### Scenario: --inspect launches run view
- **WHEN** `agent-runner --inspect <run-id>` is invoked and the run exists
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

### Requirement: Step list rendering
The run view SHALL render the current level's steps as a vertical list on the left. Each row SHALL display, in order: a status indicator, the step name, and (for non-loop steps) the type glyph to the right of the name. Loop steps SHALL display an iteration counter in the form `(N/M)` after the name instead of a type glyph.

Step types distinguished by glyph SHALL be: shell, headless agent, interactive agent, sub-workflow, and loop.

Step statuses SHALL be: `pending`, `in-progress`, `success`, `failed`, `skipped`. The `in-progress` indicator SHALL blink only when a run is currently active; otherwise it renders static (covering steps that were aborted mid-execution and will resume on the next run). Loop "exhausted" outcomes SHALL render as `success`.

Status glyphs SHALL be: `●` running, `○` pending, `✓` success, `✗` failed, `⇥` skipped. Type glyphs SHALL be: `$` shell, ⚙️ headless agent, 💬 interactive agent, ↳ sub-workflow. Loop steps SHALL NOT have a type glyph; the `(N/M)` counter is sufficient. Pulse cadence for the running indicator SHALL match the list TUI's 50 ms tick, lerping between the running and dim-running color tokens. Color palette additions (failed red) and reuses are specified in the design document.

#### Scenario: Shell step row
- **WHEN** a shell step is rendered in the step list
- **THEN** the row shows a status indicator, the step name, and the shell type glyph to the right of the name

#### Scenario: Loop step row shows iteration counter
- **WHEN** a loop step with `max: 5` has completed 3 iterations
- **THEN** the row shows a status indicator, the step name, and `(3/5)` after the name with no type glyph

#### Scenario: For-each loop row shows iteration counter
- **WHEN** a for-each loop has resolved to 4 matches and completed 2 iterations
- **THEN** the row shows a status indicator, the step name, and `(2/4)` after the name with no type glyph

#### Scenario: Active step blinks
- **WHEN** a step is currently executing and the run is active
- **THEN** the step's status indicator blinks

#### Scenario: Aborted step does not blink when no run is active
- **WHEN** a step was interrupted by an earlier run and no run is currently active
- **THEN** the step's status indicator shows `in-progress` without blinking

#### Scenario: Pending steps from workflow file before execution
- **WHEN** the run has no audit entries yet but the workflow file is known
- **THEN** the step list is populated from the workflow file with every row in `pending`

### Requirement: Drill-in navigation with breadcrumbs
The run view SHALL support drilling into sub-workflows and loops via a drill-in model. Enter on a drillable row SHALL replace the step list with that container's children. A breadcrumb line at the top SHALL show the current depth path (run name, then each entered container in order).

Drillable rows SHALL be: sub-workflow steps and loop steps. Drill-in SHALL be available on `pending` containers (children read from the workflow file or resolved statically) as well as executed ones.

#### Scenario: Top-level breadcrumb rendering
- **WHEN** the run view is at the top level (no drill-in)
- **THEN** the breadcrumb shows the workflow's canonical runnable name, the start time, and the run status (active/failed/completed/inactive)

#### Scenario: Enter on sub-workflow drills in
- **WHEN** the user presses Enter on a sub-workflow step row
- **THEN** the step list is replaced by the sub-workflow's children, and the breadcrumb appends the sub-workflow entry

#### Scenario: Enter on loop drills into iteration list
- **WHEN** the user presses Enter on a loop step row
- **THEN** the step list is replaced by a list of iterations, one row per iteration, each showing its own status; the breadcrumb appends the loop entry

#### Scenario: Enter on iteration drills into iteration children
- **WHEN** the user presses Enter on an iteration row in the iteration list
- **THEN** the step list is replaced by that iteration's child steps, and the breadcrumb appends the iteration identifier

#### Scenario: Drill in to pending sub-workflow
- **WHEN** the user presses Enter on a sub-workflow step that has not yet executed
- **THEN** the sub-workflow file is read and its children are displayed with status `pending`

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
Selecting a step in the step list SHALL populate the main content pane with step-specific detail. Detail content SHALL be:

- **Shell**: interpolated command, exit code, duration, captured-variable name if `capture:` is set, full stdout and stderr (distinguishable), scrollable.
- **Headless agent**: agent profile, model, CLI, resolved session ID, interpolated prompt, exit code, duration, full stdout and stderr, resume action.
- **Interactive agent**: agent profile, model, CLI, session ID, interpolated prompt, outcome, duration, resume action.
- **Sub-workflow**: resolved workflow path, interpolated params, outcome, duration, "Enter to drill in" hint.
- **Loop**: loop type (counted or for-each), max or resolved glob matches, iterations completed, break_triggered, outcome, duration, "Enter to drill in" hint.

#### Scenario: Shell step detail
- **WHEN** a shell step is selected
- **THEN** the detail pane shows interpolated command, exit code, duration, captured-variable name (if any), and scrollable stdout/stderr

#### Scenario: Headless agent detail
- **WHEN** a headless agent step is selected
- **THEN** the detail pane shows profile, model, CLI, session ID, interpolated prompt, exit code, duration, stdout/stderr, and a resume action

#### Scenario: Interactive agent detail
- **WHEN** an interactive agent step is selected
- **THEN** the detail pane shows profile, model, CLI, session ID, interpolated prompt, outcome, duration, and a resume action

#### Scenario: Sub-workflow detail
- **WHEN** a sub-workflow step is selected
- **THEN** the detail pane shows resolved path, interpolated params, outcome, duration, and a "Enter to drill in" hint

#### Scenario: Loop detail
- **WHEN** a loop step is selected
- **THEN** the detail pane shows loop type, max or resolved matches, iterations completed, break_triggered, outcome, duration, and a "Enter to drill in" hint

#### Scenario: Pending step detail
- **WHEN** a step with status `pending` is selected
- **THEN** the detail pane shows whatever is statically knowable (step name, type, configured command/prompt/path, expected behavior) without runtime fields

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

#### Scenario: Spawn failure
- **WHEN** the user triggers the resume action and the agent CLI cannot be spawned (e.g. binary not found on PATH)
- **THEN** agent-runner does not exit; it returns to the run view and surfaces the spawn error to the user

### Requirement: Keyboard focus and scrolling
The step list SHALL always own up/down keys for step navigation. The detail pane SHALL scroll via PageUp/PageDown and mouse wheel. Focus SHALL not need to be switched between panes.

#### Scenario: Up/down navigates step list
- **WHEN** the user presses Up or Down
- **THEN** the step list selection moves one row in that direction and the detail pane updates for the newly selected step

#### Scenario: PageUp/PageDown scrolls detail pane
- **WHEN** the user presses PageUp or PageDown
- **THEN** the detail pane scrolls one page in that direction; the step list selection does not change

#### Scenario: Mouse wheel scrolls detail pane
- **WHEN** the user scrolls the mouse wheel while the pointer is over the detail pane
- **THEN** the detail pane scrolls

#### Scenario: Mouse wheel outside detail pane is ignored
- **WHEN** the user scrolls the mouse wheel while the pointer is over the step list
- **THEN** nothing happens (up/down keys are the only way to navigate the step list)

### Requirement: Legend overlay
The run view SHALL provide a `?` key that toggles a modal legend overlay showing status glyph meanings and type glyph meanings. The overlay SHALL be dismissible with `?` or Escape.

#### Scenario: Toggle legend overlay on
- **WHEN** the user presses `?` and the legend is not visible
- **THEN** a modal overlay appears showing status glyphs (`●` running, `○` pending, `✓` success, `✗` failed, `⇥` skipped) and type glyphs (`$` shell, ⚙️ headless agent, 💬 interactive agent, ↳ sub-workflow)

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

#### Scenario: Active run refreshes on interval
- **WHEN** the viewed run is active
- **THEN** the run view polls state on the same interval as the list TUI and re-renders on any change

#### Scenario: Inactive run does not poll
- **WHEN** the viewed run is not active
- **THEN** the run view renders once and does not poll

#### Scenario: Missing or empty audit log
- **WHEN** the run's `audit.log` file is missing or empty
- **THEN** the step list is populated from the workflow file with every row in `pending` (identical to the "Pending steps from workflow file before execution" scenario); no error is shown
