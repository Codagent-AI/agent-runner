# Task: Continuous log pane

## Goal

Replace the swap-in per-step detail pane with a continuous scrolling log that stacks every started step as a block, renders nested sub-workflow and loop content inline, and keeps bidirectional scroll sync between the step list and the log. While a run is active the log auto-scrolls unconditionally. The `t` keybinding is removed; `l` re-engages auto-follow without drilling in.

## Background

### Context

The run view TUI lives in `internal/runview/`. It renders a two-column layout: step list on the left, detail pane on the right. Currently the right pane swaps content whenever the cursor moves — users lose visibility into earlier steps the moment a new one starts. This task replaces that swap-in pane with a continuous log.

**Files to read before starting:**
- `internal/runview/model.go` — current Model struct, Update(), key handlers, navigateToNode()
- `internal/runview/view.go` — renderTwoColumn(), buildStepRows(), renderHelpBar()
- `internal/runview/detail.go` — all renderXxxDetail() functions; these become renderXxxBlock()
- `internal/runview/tree.go` — StepNode fields, NodeType constants, NodeStatus constants, IsContainer()
- `internal/runview/output.go` — truncateOutput(), tailOutputCap(), maxOutputLines, maxOutputBytes
- `internal/runview/model_test.go` — newTestModel() helper; fixtures for test setup
- `internal/runview/fixtures_test.go` — fixture tree builders; reuse these in new tests
- `internal/runview/view_test.go` — existing tests; keep them passing or delete+rewrite golden ones

### New file: `internal/runview/logview.go`

```go
type stepLineRange struct {
    node      *StepNode
    startLine int  // inclusive, 0-based into the log's line slice
    endLine   int  // exclusive
}

func buildLogLines(
    children []*StepNode,
    pendingSelected *StepNode,
    bodyWidth int,
    loadedFull map[string]bool,
) (lines []string, ranges []stepLineRange)
```

`buildLogLines` walks `children` in execution order. For each **started** child (status ≠ pending, OR child == pendingSelected), it calls the appropriate `render<Type>Block(node, indent, width, loadedFull)` from `detail.go` to get `[]string` lines. For container children (sub-workflow, loop, iteration), after appending the header lines it recurses into the container's children at `indent+1`. Pending children are skipped unless the child is `pendingSelected`, in which case a ghost block is emitted (see Ghost block section below).

`stepLineRange` accumulates as lines are appended. Each node's range spans from the first line of its own header through the last line of its deepest descendant's block (inclusive of all recursive content). The ranges slice includes **every node visited**, not just direct children — nested nodes are included so scroll-sync can map a log offset to any node and then walk up to its current-level ancestor.

`loadedFull` is keyed by `node.ID` (string), not by pointer, so it survives across rebuilds. (The model's existing `loadedFull map[*StepNode]bool` must be changed to `map[string]bool` and keyed by `node.ID`.)

### Block renderers: `detail.go` refactoring

The six existing per-step renderers are renamed and their signatures change:

```go
// BEFORE: func (m *Model) renderShellDetail(b *strings.Builder, n *StepNode)
// AFTER:
func renderShellBlock(node *StepNode, indent int, width int, loadedFull bool) []string
```

The model receiver is removed — block renderers become package-level functions. All styling that previously referenced `m.detailWidth` or `m.loadedFull[n]` now uses the passed-in `width` and `loadedFull` arguments. The `pulsePhase`-based spinner is retained; pass it as an extra argument `pulsePhase float64` to any renderer that shows a spinner (headless agent in-progress). Keep the existing `wrapLine`, `fitDetailLine`, `renderCommonModifiers`, helper functions.

**Sub-workflow and loop blocks return only their own header lines.** Their children's lines are emitted by `buildLogLines` recursively. The "press enter to drill in →" hint is removed from both (content is now inline). The `renderIterationDetail` → `renderIterationBlock` handles iteration header.

The existing `renderDetail()` dispatch function (which drives the old per-step swap-in pane) is deleted.

**Separator glyphs per depth:**
```
Depth 0:   ══ name ══════════════ glyph ═
Depth 1:   ── name ─────────────── glyph ─
Depth 2:   ─ name ──────────────────── glyph
Depth 3+:  · name ·························· glyph
```
Each block opens with a full-width separator line using these glyphs, filling to `width - 2*indent`. Depth is the `indent` parameter.

**Indentation:** 2 spaces per depth level prepended to every line in the block (including the separator). Effective content width = `width - 2*indent`.

**Ghost block (pending-selected):** When `pendingSelected` is non-nil and `buildLogLines` reaches that node's execution-order position, emit a ghost block using a dashed separator (`- - - - -` repeating to fill width) rendered in `tuistyle.DimStyle`. Ghost block content: step name, type glyph, configured command/prompt/sub-workflow path/params as raw template strings. No runtime fields (no exit code, no output, no duration). The visual distinction is the dashed separator and dim color.

### Model state changes (`model.go`)

**Remove:** `tailFollow bool`

**Rename:** `detailOffset int` → `logOffset int`

**Add:**
```go
stepRanges []stepLineRange  // rebuilt whenever tree or selection changes
logAnchor  stepLineAnchor   // for resize stability
```

```go
type stepLineAnchor struct {
    stepKey           string
    lineOffsetInBlock int
}
```

**`stepRanges` rebuild triggers** (call `m.rebuildRanges()` at end of each handler):
- `OutputChunkMsg`, `StepStateMsg`, `ExecDoneMsg`, `RefreshMsg` — any message that mutates the tree or selection
- `tea.WindowSizeMsg` — line wrap changes with width
- Arrow key moves cursor (selection changes → pending ghost block may appear/disappear)

`stepRanges` is NOT rebuilt on `j`/`k` alone (scroll doesn't change content).

**`rebuildRanges()`** calls `buildLogLines(m.currentChildren(), m.pendingSelected(), m.rightWidth(), m.loadedFull)` and stores the result on `m.stepRanges`. `pendingSelected()` returns `m.selectedNode()` if its status is pending, else nil.

**Auto-scroll:** Remove `tailFollow`. Replace the existing tail-follow logic with:
```go
if m.active || m.running {
    m.logOffset = math.MaxInt32  // clamped to max during View()
}
```
This fires in every `OutputChunkMsg`, `StepStateMsg`, `ExecDoneMsg`, and `RefreshMsg` handler when the run is active. Inactive runs preserve `logOffset`.

**Auto-follow change:** The existing `navigateToNode()` drills the path all the way to the target. Auto-follow must NOT drill in. Replace the `StepStateMsg` handler's `m.navigateToNode(...)` call with:
```go
if m.autoFollow {
    active := m.tree.FindByPrefix(msg.ActiveStepPrefix)
    if active != nil {
        target := m.ancestorAtCurrentLevel(active)
        if target != nil {
            idx := m.indexOfChild(target)
            if idx >= 0 {
                m.cursor = idx
            }
        }
    }
}
```
`ancestorAtCurrentLevel(node)` walks `node.Parent` until it finds a node whose parent is `m.currentContainer()`, returning that node. `indexOfChild(node)` returns the index of `node` in `m.currentChildren()`, or -1.

**Key handler changes:**
- Remove `case "t"` entirely.
- `case "l"`: set `m.autoFollow = true`, then apply the ancestor-at-current-level cursor update (same logic as StepStateMsg). Do NOT call `navigateToNode`.
- `case "up"`, `case "down"`: after `moveCursor`, set `m.autoFollow = false`, call `m.syncLogToSelection()`.
- `case "j"`: increment `m.logOffset` by 1, call `m.syncSelectionToLog()`. Set `m.autoFollow = false`.
- `case "k"`: decrement `m.logOffset` by 1 (floor 0), call `m.syncSelectionToLog()`. Set `m.autoFollow = false`.
- Mouse wheel up/down: same as `k`/`j` but ±3 lines.

**Scroll sync helpers:**

`syncLogToSelection()` — after cursor moves via arrow keys:
```go
sel := m.currentChildren()[m.cursor]
for _, r := range m.stepRanges {
    if r.node == sel {
        m.logOffset = r.startLine
        m.logAnchor = stepLineAnchor{stepKey: sel.ID, lineOffsetInBlock: 0}
        return
    }
}
```

`syncSelectionToLog()` — after log scrolls via j/k/wheel:
```go
bodyH := m.bodyHeight()
viewport := [m.logOffset, m.logOffset+bodyH)
var winner *StepNode
for _, r := range m.stepRanges {
    if r.startLine < m.logOffset+bodyH && r.endLine > m.logOffset {
        // overlaps viewport — prefer the one with the latest startLine
        if winner == nil || r.startLine > winnerStart {
            winner = r.node
            winnerStart = r.startLine
        }
    }
}
if winner != nil {
    target := m.ancestorAtCurrentLevel(winner)
    if idx := m.indexOfChild(target); idx >= 0 {
        m.cursor = idx
    }
    m.logAnchor = stepLineAnchor{stepKey: winner.ID, lineOffsetInBlock: m.logOffset - winnerRangeStart}
}
```

**Resize handler** (`tea.WindowSizeMsg`):
1. Update `m.termWidth`, `m.termHeight`.
2. Call `m.rebuildRanges()` (width changed, line wrapping changes).
3. Re-resolve anchor: find the range for `m.logAnchor.stepKey`, set `m.logOffset = newStart + m.logAnchor.lineOffsetInBlock`. Clamp to `[0, max]`.

### View changes (`view.go`)

**`renderTwoColumn`** replaces the right-pane logic:

```go
// Build log lines (also stored in m.stepRanges via rebuildRanges — call again
// here purely for the line content; stepRanges are already current).
lines, _ := buildLogLines(children, m.pendingSelected(), rightWidth, m.loadedFull)

maxOffset := max(0, len(lines)-bodyHeight)
offset := m.logOffset
if offset > maxOffset { offset = maxOffset }
visibleLines := lines[offset : min(offset+bodyHeight, len(lines))]
```

Then render the two-column frame using `visibleLines` on the right side.

`detailWidth` field rename: the field used to be `m.detailWidth` (set during renderTwoColumn, read by detail renderers via the model receiver). Since block renderers are now package-level functions that receive width directly, remove `m.detailWidth` from Model. Compute `rightWidth` locally in `renderTwoColumn` and pass it to `buildLogLines`.

**Help bar** (`renderHelpBar`): update to:
`↑↓ step · j/k scroll · enter drill · esc back · r resume run · g full output · l follow · ? legend · q quit`

Remove the `t tail` hint. Add `l follow` only when `!m.autoFollow`. Keep `g load full` conditional on truncated output. Keep `r resume` conditional on `canResumeRun()`.

**Legend** (`renderLegend`): remove the `t` entry; update `l` entry to "jump to active step and resume auto-follow".

### Testing

**Unit tests for `buildLogLines`** (new file `internal/runview/logview_test.go`):
- Flat children: verify line count and range bounds for 2 shell steps
- Sub-workflow with started children: verify child lines are indented under parent header; verify ranges include both parent and child nodes
- Loop with 2 started iterations: verify iteration blocks appear inline under loop header
- Deep nesting (4 levels): verify separator weight decreases and ranges are non-overlapping
- Pending step (not selected): verify no block emitted for it
- Pending step selected (ghost block): verify ghost block appears at correct execution-order position with dashed separator
- Large output (> maxOutputLines): verify tail cap applied (output truncated)

**Unit tests for scroll sync** (in `model_test.go`):
- Given `stepRanges` + `logOffset`, `syncSelectionToLog` picks the latest node with content in viewport
- Nested winner maps to ancestor-at-current-level
- Given cursor, `syncLogToSelection` sets `logOffset` to node's `startLine`

**Unit tests for auto-follow** (in `model_test.go`):
- `StepStateMsg` with active step nested under top-level step → cursor lands on top-level ancestor, `m.path` unchanged (no drill-in)
- Pressing `l` while auto-follow is disengaged re-engages and snaps cursor

**Golden tests:** The existing golden tests in `view_test.go`/`model_test.go` that assert rendered output will fail because content has fundamentally changed. Delete the golden assertions and replace with two snapshot tests:
1. Mid-run: a tree with a completed shell step, an in-progress headless agent step with output, and a pending step — verify the log contains both started blocks stacked, the pending step has no block.
2. Completed run: same tree with all steps done — verify correct block count and no auto-scroll.

## Spec

### Requirement: Detail pane per step type (MODIFIED)
The run view SHALL render a single continuous, scrollable log pane that stacks a detail block for every started step at the current drill-in level, in execution order. Sub-workflow and loop blocks SHALL contain their started children's blocks inline beneath the parent header, recursively at arbitrary depth. Selection SHALL NOT swap the pane's content; it scrolls the pane so the selected step's block is visible.

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
- **THEN** the log does NOT contain a block for it

#### Scenario: Selecting a step scrolls log to its block
- **WHEN** the user selects a step in the step list whose block is not currently in the viewport
- **THEN** the log scrolls so that step's block is in view; the log's content is not replaced

### Requirement: Keyboard focus and scrolling (MODIFIED)
The step list SHALL always own the up/down arrow keys for step navigation. The log pane SHALL scroll via `j` (down) and `k` (up) and via the mouse wheel.

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
- **WHEN** the log viewport shows content belonging to a step that is nested below the current drill-in level
- **THEN** the step-list cursor highlights the top-level ancestor of that step

#### Scenario: Cursor follows latest step in viewport
- **WHEN** multiple started steps' blocks are visible in the log viewport simultaneously
- **THEN** the step-list cursor highlights the step whose execution order is the latest (furthest down) among those whose content is visible

#### Scenario: PgUp and PgDown are not bound
- **WHEN** the user presses `PgUp` or `PgDown`
- **THEN** nothing happens

### Requirement: Pending steps hidden from log (ADDED)
Steps with status `pending` SHALL NOT appear as blocks in the log except when selected by the cursor.

#### Scenario: Pending top-level step absent from log
- **WHEN** a top-level step has status `pending` and is not currently selected by the cursor
- **THEN** the log contains no block for that step, and the step list shows the step with its pending status indicator

#### Scenario: Pending iteration absent from log
- **WHEN** a loop has started but some iterations are still pending and are not selected by the cursor
- **THEN** the loop's block in the log contains only iteration sub-blocks for started iterations; pending iterations have no sub-block

#### Scenario: Pending child of started sub-workflow absent from log
- **WHEN** a sub-workflow has started and some of its child steps are still pending and not selected
- **THEN** the sub-workflow block contains inline sub-blocks only for started children; pending children have no sub-block

### Requirement: Temporary detail for selected pending step (ADDED)
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

### Requirement: Cross-step auto-scroll while run is active (ADDED)
While the viewed run is active, any new content appended to the log SHALL auto-scroll the log so the newly appended content is visible. Auto-scroll SHALL apply unconditionally regardless of the user's current scroll position.

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

### Requirement: Live refresh for active runs (MODIFIED)
While active and auto-follow is engaged, the step-list cursor SHALL be set to the top-level ancestor of the currently active step at the current drill-in level. Auto-follow SHALL NOT drill into sub-workflows, loops, or iterations.

#### Scenario: Auto-follow tracks active step at top level
- **WHEN** the run is active, auto-follow is engaged, and the active step advances into a nested sub-workflow or loop iteration
- **THEN** the step-list cursor is set to the top-level ancestor of the active step at the current drill-in level; no drill-in occurs

#### Scenario: Manual navigation disengages auto-follow
- **WHEN** the user presses an arrow key, `j`, `k`, or scrolls the mouse wheel while auto-follow is engaged
- **THEN** auto-follow disengages; the cursor stops tracking the active step

#### Scenario: Pressing l re-engages auto-follow
- **WHEN** the user presses `l` while auto-follow is disengaged
- **THEN** auto-follow re-engages and the cursor snaps back to the ancestor of the active step at the current drill-in level

### Requirement: Recursive log nesting with progressive separators (ADDED)
Log blocks for sub-workflows and loops SHALL contain their children's blocks inline beneath their header, recursively at arbitrary depth. Nesting SHALL be visually conveyed by indentation and by using progressively lighter separator characters as depth increases.

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

## Done When

- `buildLogLines` passes unit tests covering flat, nested, loop, pending, ghost-block, and large-output cases.
- Scroll-sync unit tests pass: `syncSelectionToLog` picks the correct cursor position; `syncLogToSelection` sets the correct `logOffset`.
- Auto-follow unit tests pass: `StepStateMsg` lands cursor on ancestor-at-current-level without modifying `m.path`.
- Two new golden snapshot tests (mid-run and completed-run) verify the rendered output structure.
- All non-golden existing tests continue to pass (`TestFitDetailLine_*`, `TestRenderTwoColumn_PromptWraps`, tree/audit/resolve tests).
- `t` keybinding is gone; help bar text matches the new spec.
- `autoFollow` flag engages by default in `FromLiveRun` mode; `tailFollow` field is removed.
- `m.loadedFull` is changed from `map[*StepNode]bool` to `map[string]bool` (keyed by `node.ID`) so block renderers can look up full-load state without holding model pointers.
