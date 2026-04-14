# Task: Extract tuistyle package and build run-view data model

## Goal

Two deliverables in one task: (1) refactor the existing list TUI to move its shared styling, formatting, and tick helpers into a new `internal/tuistyle` package so multiple TUI screens can share them; (2) build a pure data layer for the new single-run view in `internal/runview` — a step tree merging workflow YAML with audit log events, an incremental audit-log tailer, status mapping, auto-flatten detection, and canonical-name resolution. No bubbletea Model/View code in this task.

## Background

You MUST read these files before starting:

- `openspec/changes/view-run/proposal.md` for the "why" and scope
- `openspec/changes/view-run/design.md` — full design; pay particular attention to the **Package layout**, **Step tree model**, **Audit log tailing**, and **Decisions** sections
- `openspec/changes/view-run/specs/view-run/spec.md` for behavioral requirements (primarily the requirements used in the Spec section below)
- `internal/tui/styles.go`, `internal/tui/format.go`, `internal/tui/model.go` — source of the code being extracted
- `internal/audit/types.go` and `internal/audit/logger.go` — audit event types and writer (this task only reads events, but understanding the shape matters)
- `internal/model/step.go`, `internal/model/state.go`, `internal/model/context.go` — step, workflow, nested-state types; `Step.StepType()` is the existing classifier
- `internal/loader/loader.go` — how workflows are loaded; you'll call `loader.LoadWorkflow` to read both the top-level workflow and any sub-workflow files
- `internal/stateio/stateio.go` — reads `state.json`
- `internal/runlock/runlock.go` — PID lock check (used to gate polling)
- `internal/runs/runs.go` — existing per-run reader; study the layout under `~/.agent-runner/projects/<encoded-path>/runs/<session-id>/`
- `workflows/openspec/implement-change.yaml` and `workflows/implement-task.yaml` — these are the canonical real-workflow examples referenced throughout the design and mockups; make sure your tree builder produces sensible output against them
- `openspec/specs/audit-log-entries/spec.md` — authoritative description of the event shapes your tailer must parse

### Part 1 — tuistyle extraction (refactor, no behavior change)

Create `internal/tuistyle/` with these files:

- `styles.go` — move the entire contents of `internal/tui/styles.go` (palette `AdaptiveColor` constants + style instances). Add a new `AdaptiveColor` token for failed red: `failedRed = AdaptiveColor{Dark: "#f87171", Light: "#dc2626"}`, plus a `statusFailed` style that uses it. Export every symbol that the runview package will need (the existing list-TUI consumers in `internal/tui` can be updated to use the exported names too, or keep using unexported aliases — your call, consistency across both packages is the only requirement).
- `format.go` — move every function from `internal/tui/format.go` **except `runSummary`**. `runSummary` is list-specific (references `runs.RunInfo` for the "N runs · M active" summary); leave it in `internal/tui/format.go`. Exported surface includes at minimum: `FitCell`, `FitCellLeft`, `AdjustOffset`, `ShortenPath`, `FormatTime`, `LerpColor`, `ParseHex`, `Sanitize`. You may preserve the lowercase names inside `tuistyle` and re-export as needed; the list TUI currently uses the unexported names.
- `ticker.go` — extract the `refreshMsg`, `pulseMsg`, `doRefresh()`, `doPulse()` definitions from `internal/tui/model.go` (lines ~80–93 in the current file). Export them: `RefreshMsg`, `PulseMsg`, `DoRefresh()`, `DoPulse()`. The 2 s refresh and 50 ms pulse cadences must match the current constants exactly so the list and run views stay phase-synchronized.

Update `internal/tui/` to import from `tuistyle` and delete the migrated code. All existing list-TUI tests (`internal/tui/*_test.go` if any, plus `cmd/agent-runner/script_test.go`) and downstream builds MUST continue to pass.

### Part 2 — runview data layer

Create `internal/runview/` with these files:

- `tree.go` — the `StepNode` type and tree-build functions (from workflow YAML; static-only).
- `audit.go` — audit-log tailer and event-to-mutation apply logic.
- `names.go` — canonical runnable-name resolver (converting workflow file paths to `<ns>:<name>` form; fallback to repo-relative path).
- corresponding `*_test.go` files with thorough coverage.

No bubbletea, no lipgloss, no terminal-rendering code in this task. Data types may expose methods needed by the UI (e.g., `(*StepNode).Drilldown()`), but nothing should import `charmbracelet/bubbletea`.

**`StepNode` shape** — follow the `StepNode` struct in design.md's "Step tree model" section. Key fields: `ID`, `Type` (shell, headlessAgent, interactiveAgent, loop, subWorkflow), `Status` (pending, inProgress, success, failed, skipped), `Children`, static fields (command, prompt, workflow canonical name, loop max/over/as), runtime fields populated from audit events (interpolated command/prompt/params, exit code, duration, stdout, stderr, capture name, agent profile/model/cli, session ID, loop matches, iterations completed, break triggered, error message), iteration-specific fields (index, binding value), and `FlattenTarget` for auto-flatten.

**Tree construction order**:
1. Load the workflow YAML via `loader.LoadWorkflow`. Walk the `Workflow.Steps` tree and create a `StepNode` per entry, setting static fields and classifying `Type` via a function equivalent to `model.Step.StepType()` (you may reuse it, or define your own that distinguishes headless vs interactive based on `Mode`, with the understanding that `Mode` may be empty and resolved via profile default at runtime — in that case default to interactive for static-tree rendering, and let audit events correct it).
2. Do not expand loop iterations or sub-workflow bodies at this point — iteration `Children` start empty; sub-workflow `Children` start empty and a lazy-load flag indicates the body hasn't been read yet.
3. Compute `FlattenTarget` for loop nodes whose body is exactly one `Step` with a `Workflow` field. `FlattenTarget` points to the child sub-workflow's static children, loaded at the time a user drills into an iteration.

**Audit event → tree mutation** — the audit log stores events with a nesting prefix (see `openspec/specs/audit-log-entries/spec.md` requirement "Nesting prefix" for grammar — e.g., `[task-loop:2, implement]`, `[task-loop:0, verify, sub:verify-task, check]`). Parse the prefix to locate a node (creating iteration and sub-workflow children lazily as `iteration_start` and `sub_workflow_start` events arrive). Apply:

- `run_start` — record run-level start time.
- `step_start` — set status to `inProgress`, record `step_type`-specific start data (interpolated command for shells, prompt/profile/model/cli/session-id/enrichment for agents, params/path for sub-workflows, loop type and max/glob+matches for loops).
- `step_end` — set status from outcome (`success` → `success`, `failed` → `failed`, `skipped` → `skipped`, `aborted` → `inProgress`, `exhausted` → `success`); record end data (exit code, duration, captured stdout, stderr). **Preserve an `aborted` flag** on the node so UI can display it; status visual is still in-progress, but if a run is not active the blink is disabled — the UI layer handles that gating.
- `iteration_start` — create or locate the iteration child (1-based display index derived from 0-based internal); bind the loop's `As` variable if present.
- `iteration_end` — set iteration status.
- `sub_workflow_start` — if not already lazy-loaded, load the sub-workflow YAML now and attach its static children to the sub-workflow node. Record `resolvedPath` and `interpolatedParams`.
- `sub_workflow_end` — propagate outcome.
- `error` — attach the message to the currently-executing node (or the run-level node if no step context is active).

**Audit log tailer (`audit.go`)**:

- First read (on run-view entry): open `audit.log`, stream through all bytes, parse every complete line (`\n` terminated), apply to tree, record byte offset at EOF.
- Incremental tail: exposes `Apply(r io.Reader) (newBytesConsumed int, err error)` and a companion helper that, given a session directory and the last-known offset, reads only new bytes since the offset, handles the partial-line buffer, and returns mutated events and updated offset.
- Partial-line handling: buffer any bytes after the last `\n` across invocations. Do not parse a line until you see its terminator. The buffer lives on the tailer state struct, not globally.
- Tailing must be safe to invoke when the file has not changed (no new bytes, no-op return).

**Canonical name resolver (`names.go`)**:

- Input: resolved absolute path to a workflow YAML file and the project's repo root (or workflows-root discovery rule — look up from `cwd` the way `loader.LoadWorkflow` does, if there's an existing helper prefer it over reimplementing).
- Output: canonical runnable name string (e.g., `implement-change`, `openspec:plan-change`) when the file is under `workflows/`; otherwise the repo-relative path as fallback (e.g., `../external/other.yaml`).
- Rule: strip `.yaml`/`.yml` extension; for namespaced files, replace the subdirectory separator between `workflows/` and the file with `:`. For files more than one subdirectory deep under `workflows/` — this is not common in practice, but if encountered, fall back to the repo-relative path.

**Status mapping summary** (for reference; scenarios below are the authoritative contract):

| Audit outcome | StepNode Status | Notes |
|---|---|---|
| `success` | `success` | — |
| `exhausted` | `success` | loop hit max without break |
| `failed` | `failed` | — |
| `skipped` | `skipped` | — |
| `aborted` | `inProgress` | set Aborted=true flag; UI disables blink when run inactive |
| no `step_end` yet | `inProgress` | — |
| no events at all | `pending` | from static workflow only |

## Spec

The data layer built in this task must satisfy the behavioral contract for step-list rendering (tree shape, statuses, iteration counters), drill-in (tree must expose sub-workflow children lazily, iteration children, and flatten targets), sub-workflow headers (resolved name + params must be accessible from the tree), and live refresh (tail must be incremental). Scenarios copied verbatim from `specs/view-run/spec.md`:

### Requirement: Step list rendering
The run view SHALL render the current level's steps as a vertical list on the left. Each row SHALL display, in order: a status indicator, the step name, and (for non-loop steps) the type glyph to the right of the name. Loop steps SHALL display an iteration counter in the form `(N/M)` after the name instead of a type glyph.

Step types distinguished by glyph SHALL be: shell, headless agent, interactive agent, and sub-workflow. Loop steps are identified by their `(N/M)` iteration counter instead of a type glyph.

Step statuses SHALL be: `pending`, `in-progress`, `success`, `failed`, `skipped`. The `in-progress` indicator SHALL blink only when a run is currently active; otherwise it renders static (covering steps that were aborted mid-execution and will resume on the next run). Loop "exhausted" outcomes SHALL render as `success`.

Status glyphs SHALL be: `●` running, `○` pending, `✓` success, `✗` failed, `⇥` skipped. Type glyphs SHALL be: `$` shell, ⚙️ headless agent, 💬 interactive agent, ↳ sub-workflow. Loop steps SHALL NOT have a type glyph; the `(N/M)` counter is sufficient. Pulse cadence for the running indicator SHALL match the list TUI's 50 ms tick, lerping between the running and dim-running color tokens. Color palette additions (failed red) and reuses are specified in the design document.

#### Scenario: Shell step row
- **WHEN** a shell step is rendered in the step list
- **THEN** the row shows a status indicator, the shell type glyph, and the step name

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

Note: The data-layer scenarios are the ones above whose satisfaction depends on the tree shape and status assignments (all six). Visual rendering happens in the UI task; the data layer provides the contract the UI reads.

### Requirement: Drill-in navigation with breadcrumbs
The run view SHALL support drilling into sub-workflows and loops via a drill-in model. Enter on a drillable row SHALL replace the step list with that container's children. A breadcrumb line at the top SHALL show the current depth path (run name, then each entered container in order).

Drillable rows SHALL be: sub-workflow steps and loop steps. Drill-in SHALL be available on `pending` containers (children read from the workflow file or resolved statically) as well as executed ones.

#### Scenario: Drill in to pending sub-workflow
- **WHEN** the user presses Enter on a sub-workflow step that has not yet executed
- **THEN** the sub-workflow file is read and its children are displayed with status `pending`

Note: The data layer must support lazy sub-workflow loading on demand.

### Requirement: Sub-workflow header inside drill-in
When the user is drilled inside a sub-workflow, a header SHALL be displayed above the step list showing the resolved sub-workflow path and the interpolated params that were (or will be) passed to it.

#### Scenario: Header shown inside sub-workflow
- **WHEN** the user has drilled into a sub-workflow step
- **THEN** a header above the step list shows the resolved workflow path and the interpolated params

#### Scenario: Header shown for pending sub-workflow
- **WHEN** the user drills into a sub-workflow that has not yet executed
- **THEN** the header shows the resolved path (as a canonical runnable name when under `workflows/`, else a repo-relative path) and each param as its raw template string (e.g., `task_file = {{task_file}}`)

Note: Canonical-name resolution and raw-template-string preservation happen in the data layer.

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

Note: The data layer detects the auto-flatten condition and exposes `FlattenTarget` on the iteration node.

### Requirement: Detail pane per step type
Selecting a step in the step list SHALL populate the main content pane with step-specific detail. Detail content SHALL be:

- **Shell**: interpolated command, exit code, duration, captured-variable name if `capture:` is set, full stdout and stderr (distinguishable), scrollable.
- **Headless agent**: agent profile, model, CLI, resolved session ID, interpolated prompt, exit code, duration, full stdout and stderr, resume action.
- **Interactive agent**: agent profile, model, CLI, session ID, interpolated prompt, outcome, duration, resume action.
- **Sub-workflow**: resolved workflow path, interpolated params, outcome, duration, "Enter to drill in" hint.
- **Loop**: loop type (counted or for-each), max or resolved glob matches, iterations completed, break_triggered, outcome, duration, "Enter to drill in" hint.

Note: The data layer must populate each of these fields onto the corresponding `StepNode` from audit events. The UI task handles rendering. Scenarios (verbatim from spec) that constrain the data-layer side:

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

### Requirement: Live refresh for active runs
While the viewed run is active, the run view SHALL poll run state every 2 seconds (matching the list TUI cadence) and re-render when state changes. On each poll the run view SHALL re-check `run.lock` (for active status and blink gating) and tail `audit.log` (reading only bytes appended since the last poll, buffering any partial trailing line). Inactive runs SHALL render once and remain static until user input.

#### Scenario: Missing or empty audit log
- **WHEN** the run's `audit.log` file is missing or empty
- **THEN** the step list is populated from the workflow file with every row in `pending` (identical to the "Pending steps from workflow file before execution" scenario); no error is shown

Note: The data-layer contract is the incremental tailer and its partial-line handling. Full-read-on-entry, offset-tracking, and `io.Reader` adapter for new bytes only are required. The tailer must handle missing or empty audit.log gracefully (return an empty event set, no error).

## Done When

- `internal/tuistyle/` exists with `styles.go`, `format.go`, and `ticker.go` containing the moved code plus the new `failedRed` / `statusFailed` tokens.
- `internal/tui/` continues to build and all existing tests pass; list TUI behavior is byte-for-byte unchanged.
- `internal/runview/tree.go`, `audit.go`, and `names.go` exist and compile with no bubbletea/lipgloss imports.
- Unit tests in `internal/runview/*_test.go` exercise: (a) static tree construction against `workflows/openspec/implement-change.yaml` and `workflows/implement-task.yaml`, verifying node types, nesting, and static fields; (b) event-to-mutation apply for every audit event type, including partial prefix resolution and lazy sub-workflow loading; (c) partial-line buffering in the tailer with synthetic writes that split events mid-line; (d) auto-flatten detection on single-sub-workflow iteration bodies and no-flatten on non-sub-workflow bodies; (e) canonical-name resolution including the `openspec:plan-change` case, a bare-name case, and the outside-`workflows/` fallback; (f) status mapping for every outcome including `exhausted→success` and `aborted→inProgress` with the aborted flag set.
- `go build ./...` and `go test ./...` both succeed.
- Every scenario referenced above is covered by at least one unit test.
