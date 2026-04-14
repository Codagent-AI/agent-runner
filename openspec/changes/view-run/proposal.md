## Why

The list TUI from the previous change surfaces which runs exist and their status, but offers no way to inspect what actually happened inside a run — users still have to dig through state files and scrollback to understand step outcomes or grab a CLI session ID. A dedicated run view turns the list into a navigable starting point and makes step-level execution state (output, agent identity, loop progress, live activity) legible without leaving the terminal.

## What Changes

- New run-view TUI screen showing a vertical list of workflow steps on the left with per-step status indicators, type glyphs, and loop iteration counters (`N/M`), and a main content pane on the right with step-specific detail.
- Active-step status indicator blinks while the workflow is executing; other statuses (pending, running, completed, failed, skipped) render as static indicators.
- Up/down keyboard navigation between steps; selecting a step populates the detail pane with context appropriate to the step type:
  - Command / headless steps: full captured output, scrollable.
  - Agentic (interactive) steps: agent profile, CLI session ID, and a resume action.
  - Nested sub-workflow steps: summary that can itself be entered as a nested run view (exact depth behavior deferred to specs).
- **BREAKING**: In the list TUI, pressing Enter on a run now opens the run view instead of resuming. Resume becomes an explicit action inside the run view (e.g., on an agentic step, or via a dedicated key).
- New CLI flag to jump directly to the run view for a given run ID, bypassing the list.

## Capabilities

### New Capabilities

- `view-run`: The single-run detail TUI — step list rendering (status indicator, type glyph, loop counter), keyboard navigation, per-step detail panes (command output, agent session info, resume action), live refresh for active runs, and the CLI entry flag for direct navigation to a run by ID.

### Modified Capabilities

- `list-runs`: The "Resume from TUI" requirement changes. Enter on a run SHALL open the run view rather than exit-and-resume. Resume moves into `view-run` as an explicit action. The "only inactive runs are selectable" constraint is removed — active and completed runs are also openable in the view (read-only where appropriate).

## Out of Scope

- Editing, re-running, or deleting individual steps from the view.
- Diffing or comparing runs.
- Exporting step output to a file or clipboard (may come later; not required for v1).
- Remote / multi-machine run inspection — local filesystem only, same as list-runs.
- Full nested-workflow drill-down semantics (depth, back-navigation stack) — flagged here, fleshed out in specs/design.

## Impact

- **CLI entry point** (`cmd/agent-runner/main.go`): New flag for direct run-view entry by run ID.
- **tui package**: New run-view screen/model; list-runs Enter handler redirected to it.
- **runs package** (or equivalent): Reader extended to surface per-step execution state (status, captured output, agent/session metadata, loop iteration) — not just the top-level fields the list view needs.
- **Output capture / state file consumers**: Run view depends on complete per-step output and session metadata being available on disk; any gaps surface here first.
- **Existing list-runs users**: Breaking change — Enter no longer resumes directly. Resume is one extra keystroke inside the run view.
