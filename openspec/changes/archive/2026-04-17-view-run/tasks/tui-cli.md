# Task: Run-view TUI, --inspect CLI, and list-screen Enter handoff

## Goal

Build the user-facing single-run view: the bubbletea Model/View/Update for `internal/runview`, a top-level switcher Model that routes between the list and run views, a new `--inspect <run-id>` CLI flag for direct entry, and a change to the list screen's Enter handler so it opens the run view instead of resuming.

## Background

You MUST read these files before starting:

- `openspec/changes/view-run/proposal.md` — the "why" and scope.
- `openspec/changes/view-run/design.md` — the full design. Pay particular attention to **Top-level switcher Model**, **Rendering** (layout zones, colors, glyphs), **Mockups** (all 5), **Keybindings**, **CLI**, and **Decisions**.
- `openspec/changes/view-run/specs/view-run/spec.md` — behavioral requirements.
- `openspec/changes/view-run/specs/list-runs/spec.md` — the MODIFIED "Open run from TUI" requirement (Enter behavior change).
- `internal/tui/model.go`, `internal/tui/view.go`, `internal/tui/worktree.go` — the existing list TUI you're integrating with.
- `internal/tuistyle/` (created previously in this change) — shared styling, formatting, tickers. Use this package for colors, `FitCell`, `AdjustOffset`, `DoPulse`, `DoRefresh`, `PulseMsg`, `RefreshMsg`.
- `internal/runview/tree.go`, `internal/runview/audit.go`, `internal/runview/names.go` — the data layer you're rendering on top of. Call into these; do not duplicate tree/event logic here.
- `cmd/agent-runner/main.go` — existing CLI and the current `handleList()` entry point. `--inspect` is added here, the switcher Model is wired here (or in a small helper package if main gets crowded; the existing pattern keeps things in `main.go` so follow suit unless the file becomes unreadable), and `handleResume(sessionID)` is re-invoked after `Program.Run()` returns when the runview has signalled a resume.
- `internal/runs/runs.go` — study `RunInfo`, `ListForDir`, and the session-dir layout so the switcher can hand the runview a `SessionDir` and `ProjectDir`.
- `internal/audit/logger.go` — `EncodePath` is used to find the current project directory (same function the list TUI uses).
- `internal/runlock/runlock.go` — `Check(sessionDir)` returns the live/stale/missing lock state; drive blink-gating from this.

### Architecture recap

**Switcher Model** (top-level bubbletea Model in `cmd/agent-runner/main.go` or a small helper). Holds either a list sub-Model or a runview sub-Model and routes messages:

```
list.ViewRunMsg{SessionDir, ProjectDir}    → switcher swaps in a runview Model for that run
runview.BackMsg                            → switcher swaps back to list (list state preserved)
runview.ResumeMsg{SessionID}               → switcher records session ID, tea.Quit;
                                              main.go then calls handleResume(id)
runview.ExitMsg                            → switcher tea.Quit; main.go returns 0
```

The switcher owns both sub-Models across the whole program lifetime. The list sub-Model stays alive while the runview is active so its cursor, tab, and drilled-in state survive the round-trip without serialization.

**List screen change**: flip the current `handleEnter` behavior that sets `m.selected = &r; m.quitting = true; return m, tea.Quit` (for inactive runs only) to instead emit a `ViewRunMsg{SessionDir: r.SessionDir, ProjectDir: <project dir for this run>}` that the switcher handles. You will need to add a helper method on the list Model that derives the project directory for a given run (the current-dir tab uses `m.projectDir`, the worktree/all tabs use the selected entry's encoded path under `m.projectsRoot`). All runs — active, inactive, completed — are now openable; no selectability filtering.

**Runview Model files** in `internal/runview/`:

- `model.go` — the `Model` struct (holds the tree from `tree.go`, current drill path, cursor position within current level, scroll offset for the detail pane, loaded-output overrides per step, tail state, `SessionDir`, `ProjectDir`, an `Entered` enum {fromList, fromInspect} so `Esc`-at-top knows whether to emit `BackMsg` or `ExitMsg`, and viewport dimensions).
- `update.go` (or inside `model.go`) — the `Update` method handling key presses per the keybinding table below, refresh/pulse ticks (tail the audit log on refresh, update the blink phase on pulse).
- `view.go` — the `View` method rendering header, breadcrumb (with `·  active|failed|completed|inactive` status suffix per design), sub-workflow header when drilled inside one, step list (left column, width = longest row at current level; see format rules in mockups), detail pane (right column, step-type-specific via `detail.go`), help bar. Use `tuistyle` styles; never define colors inline.
- `detail.go` — per-step-type detail rendering (shell, headless agent, interactive agent, sub-workflow, loop, plus the iteration-selected-in-loop case which shows loop metadata).
- `output.go` — large-output helpers: truncation threshold 2000 lines or 256 KB, banner string formatter, U+FFFD replacement for invalid UTF-8, `g`-key load-full handling.
- `breadcrumb.go` — breadcrumb path stack, drill-in/drill-out transitions, auto-flatten check (call into `tree.StepNode.FlattenTarget`).

**Entry points**:

1. **From list TUI**: user Enter on a run row → list emits `ViewRunMsg` → switcher constructs `runview.NewFromList(SessionDir, ProjectDir)` and swaps in.
2. **Via `--inspect <run-id>`**: new CLI flag. Resolve the session the same way `--resume` does (cwd's project dir only; see `resolveResumeStatePath` in `cmd/agent-runner/main.go`). If not found, print "session not found: <id>" to stderr and exit non-zero. If found, start a program whose root Model is the switcher pre-loaded with a runview sub-Model (`runview.NewFromInspect(SessionDir, ProjectDir)`) — no list Model is constructed in this path.

**Mode enum**: `Entered fromList` makes `Esc` at the top breadcrumb emit `BackMsg`; `Entered fromInspect` makes it emit `ExitMsg`.

**Keybindings** (definitive table; `?` toggles a legend overlay):

| Key | Action |
|---|---|
| ↑ / k | Move step cursor up in current level |
| ↓ / j | Move step cursor down in current level |
| PgUp | Scroll detail pane up one page |
| PgDn | Scroll detail pane down one page |
| Enter | On loop/sub-workflow row: drill in. On agent step: emit `ResumeMsg` (resume). On shell step: no-op. |
| Esc | Drill out one breadcrumb level; at the top level, emit `BackMsg` (if `fromList`) or `ExitMsg` (if `fromInspect`). |
| `g` | Load full output (only when the large-output banner is visible on the current detail pane). |
| `?` | Toggle the legend overlay modal. |
| `q` / Ctrl+C | Unconditionally emit `ExitMsg` (program quits regardless of breadcrumb depth). |
| Mouse wheel | Scroll the detail pane when the pointer is over it. |

**Layout & color rules**:

- Step-list column width = max visible row width at current level, no padding-to-column. Rows format: `<status-glyph>  <step-name>[ (N/M)][ <type-glyph>]`. Iterations use `iter N  <binding-value>` (1-based `N`).
- Loops have no type glyph.
- Breadcrumb: `← <canonical-top-name> [/ <crumb> ...]  ·  started <when>  ·  <run-status>`. Run-status colors: active pulses green; failed red; completed gray; inactive amber.
- Selected row uses `selectedText` token; unselected uses `bodyText`. Failed step's name is tinted red.
- Glyphs: `●` running, `○` pending, `✓` success, `✗` failed, `⇥` skipped; `$` shell, ⚙️ headless, 💬 interactive, ↳ sub-workflow. Loops have none.
- Only the running step of a currently-active run blinks. When run is inactive (`runlock.LockActive` is false), no blinking anywhere — the `inProgress` status (including aborted steps) renders static.

**Large-output banner string** (exactly): `[<total> lines total · showing last <shown> — press g to load all]`. Position: above the scrollable region; stays visible while the user scrolls. After `g`, the banner disappears and the full buffer becomes scrollable.

**Polling**: use `tuistyle.DoRefresh()` (2 s) and `tuistyle.DoPulse()` (50 ms). On `RefreshMsg`, if the run is active, tail the audit log and recheck `run.lock`. If the run is inactive, skip the tail (idempotent). On `PulseMsg`, advance the blink phase only when a run is active — no blink when inactive.

**Visual consistency with list**: share the palette via `tuistyle` so active-pulse phase lines up when the user bounces between screens. Do not introduce new color tokens in this task beyond what the data-model task already added (`failedRed` / `statusFailed`).

**Integration tests**: use `github.com/charmbracelet/x/exp/teatest` if available (confirm via `go doc` before committing to the dependency); otherwise, drive the Model directly with synthetic `Msg` values and assert on rendered string output from `Model.View()`. At minimum, cover: opening a fixture run from `--inspect`, navigating up/down, drilling into a loop iteration with auto-flatten, triggering resume on an agent step (assert `ResumeMsg` is emitted with the correct session ID), pressing `q` (assert `ExitMsg`), pressing Esc at top-level after entering from list (assert `BackMsg`).

Look for a pre-existing script test pattern in `cmd/agent-runner/script_test.go` and follow its conventions for any end-to-end invocation of the binary.

## Spec

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

### Requirement: Drill-in navigation with breadcrumbs
The run view SHALL support drilling into sub-workflows and loops via a drill-in model. Enter on a drillable row SHALL replace the step list with that container's children. A breadcrumb line at the top SHALL show the current depth path (run name, then each entered container in order).

Drillable rows SHALL be: sub-workflow steps and loop steps. Drill-in SHALL be available on `pending` containers (children read from the workflow file or resolved statically) as well as executed ones.

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

#### Scenario: Top-level breadcrumb rendering
- **WHEN** the run view is at the top level (no drill-in)
- **THEN** the breadcrumb shows the workflow's canonical runnable name, the start time, and the run status (active/failed/completed/inactive)

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
Selecting the resume action on an agent step (headless or interactive) SHALL exit the TUI and exec the step's agent CLI with `--resume <session-id>`, resuming that agent's conversation directly. This is NOT the same as agent-runner's `--resume <run-id>` flag: the runview resume action targets the individual Claude/Codex/etc. session captured on the step, identified by the CLI's own session ID (e.g. `claude --resume <uuid>`), not an agent-runner workflow run.

#### Scenario: Resume from headless agent step
- **WHEN** the user triggers the resume action on a headless agent step with a known session ID
- **THEN** the TUI exits and the step's agent CLI is exec'd with `--resume <session-id>` (e.g. `claude --resume <uuid>`)

#### Scenario: Resume from interactive agent step
- **WHEN** the user triggers the resume action on an interactive agent step with a known session ID
- **THEN** the TUI exits and the step's agent CLI is exec'd with `--resume <session-id>` (e.g. `claude --resume <uuid>`)

#### Scenario: Resume unavailable without session ID
- **WHEN** an agent step has no resolved session ID (never started, or crashed before session creation)
- **THEN** the resume action is not available for that step

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

### Requirement: Open run from TUI (list-runs delta)
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

## Done When

- `internal/runview/model.go`, `view.go`, `detail.go`, `output.go`, `breadcrumb.go` exist; the runview package uses `tuistyle` for all colors/formatting/tickers.
- `cmd/agent-runner/main.go` accepts `--inspect <run-id>`, resolves the session using the same rules as `--resume`, and errors out cleanly on unknown IDs.
- A top-level switcher Model wires list ↔ runview, preserving list state across round-trips.
- List-screen Enter handler now emits `ViewRunMsg` for any status row (active, inactive, completed); the old "selected for resume" path inside the list is removed.
- Every scenario above is covered by a test — unit tests for rendering and Update-logic (teatest or direct Model-driving) plus integration tests for the `--inspect` invocation path and the list→view→list round-trip.
- `go build ./...` and `go test ./...` succeed.
- Manual smoke test against a real run (e.g., create a short `implement-change`-style run, inspect it live, drill through iterations, exit to list, resume an agent step) works end-to-end.
