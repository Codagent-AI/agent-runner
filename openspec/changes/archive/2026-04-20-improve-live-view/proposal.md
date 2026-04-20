## Why

The live run view shows one step's detail at a time, so users lose visibility into completed steps the moment a new step starts — forcing manual scrolling back to read earlier output, and obscuring the narrative of what just happened. Nested workflows and loop iterations are hidden behind drill-in navigation, so from the top level the user can't tell what iteration or sub-workflow step is actually running. Live auto-follow currently auto-drills the left pane into deeply nested workflows, yanking the user out of context.

## What Changes

- Right pane becomes a **continuous scrolling log** spanning every started step in the current view, with nested sub-workflow and loop content rendered inline beneath their parent block and clear visual separators between steps. Same per-step content that's rendered today (command, params, streamed output, errors), stacked instead of swapped.
- **Left/right scroll sync**: scrolling the log updates the left-side cursor to highlight the latest started step with content in the viewport. Selecting a step on the left scrolls the log to that step.
- **Cross-step auto-scroll**: while the run is active, any new output appended to the log auto-scrolls the log to the newly appended content, unconditionally.
- **Top-level summary of nested execution**: only the currently selected step in the left pane expands inline, recursively showing each nested level down to the deepest active descendant. Drill-in navigation (Enter to scope into a container, Esc to go back) is preserved; entering a container now also scopes the log to that container's subtree.
- **Pending steps hidden from log**: pending steps remain in the step list; a temporary static-detail block is rendered in the log only when the cursor is on a pending step.
- **Live auto-follow stays at top level**: the cursor follows the top-level ancestor of the active step instead of auto-drilling into sub-workflows and iterations.
- **`summarizer` agent profile**: new built-in profile (claude + haiku, headless) so workflow authors can add summary steps as a pattern without re-declaring the profile.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `view-run`: replaces the per-step swap-in detail pane with a continuous scrolling log; nests sub-workflow and loop content inline; adds left/right scroll synchronization; adds cross-step auto-scroll while active; changes live auto-follow to stay at the current drill level; hides pending steps from the log; adds recursive left-pane expansion under the selected step.
- `agent-profiles`: adds `summarizer` to the auto-generated default profile set.

## Out of Scope

- Changes to `list-runs`, post-run-only inspection entry points, or the workflow-run resume behavior (`r` binding).
- Changes to per-step output capture, ANSI stripping, or memory/file caps.
- Changes to the audit log schema or tree construction logic.
- Restructuring how loops or sub-workflows execute — this is a display change only.
- Reworking the left-pane step list visuals (status/type glyphs, indentation) beyond what scroll-sync and expansion require.
- A completion summary rendered by the view. Workflow authors write an explicit `summarize` step using the new `summarizer` profile if they want one.

## Impact

- **Affected packages**: `internal/runview` (new `logview.go` file; modifications to `model.go`, `view.go`, `detail.go`); `internal/config` (add `summarizer` profile to `defaultConfig()`).
- **Affected specs**: `view-run`, `agent-profiles`.
- **Unaffected**: audit log format, engine interface, workflow execution, session resolution, CLI surface, liverun message types.
- **Dependencies**: none new; continues to use bubbletea + lipgloss.
- **Risk areas**: scroll-sync UX (mapping scroll offset → active step cursor without fighting the user); performance of rendering a long concatenated log for large runs; existing view/detail tests all need new golden files.
