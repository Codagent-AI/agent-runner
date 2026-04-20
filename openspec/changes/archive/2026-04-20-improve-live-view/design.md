## Context

The run view TUI at `internal/runview/` renders workflow executions in a two-column layout: a step list on the left, a selection-driven detail pane on the right. The current detail pane swaps content based on the selected step — users see one step at a time, losing the narrative of what just happened once a new step starts. Nested sub-workflows and loop iterations are hidden behind Enter-to-drill-in navigation. Live mode's auto-follow auto-drills the left pane into deeply nested workflows, which yanks the user out of context.

The proposal replaces the swap-in detail pane with a continuous scrolling log that shows every started step as a stacked block, with nested sub-workflow and loop content rendered inline. The step list gets an inline recursive expansion under the selected step so users can see the active descendant path without drilling in.

Existing tree, audit, and output-capture mechanics are sound and stay. The refactor is purely at the rendering and interaction layer inside `runview/`, plus one added default agent profile.

## Goals / Non-Goals

**Goals:**
- Single continuous log rendering of all started steps at the current drill-in level.
- Inline nested rendering of sub-workflow and loop content.
- Bidirectional scroll/cursor sync between step list and log.
- Unconditional auto-scroll while the run is active.
- Read-only recursive expansion under the selected step in the left pane.
- Live auto-follow that stays at the current drill-in level (no auto-drill).
- Built-in `summarizer` agent profile so authors can add summary steps.
- Spec-text alignment with the post-`live-run-view`/`resume-run` base.

**Non-Goals:**
- New output-capture or streaming mechanics — `liverun` messages and per-step buffers are unchanged.
- Changes to audit log schema, engine interface, or workflow execution.
- A built-in completion summary rendered by the view. Authors who want a summary add an explicit `summarize` step using the `summarizer` profile.
- Workflow-level summary enable/disable flags.
- Jump-to-top/bottom keybindings, scroll indicators, inline resume hints in the log. Deferred.
- Per-step caching of rendered blocks. Start simple; optimize if profiling shows a need.

## Approach

### Files changed

```
internal/runview/
  model.go        MOD  — scroll state, auto-follow logic, stepRanges cache
  view.go         MOD  — two-column layout, viewport slice, left expansion
  detail.go       MOD  — rename per-step renderers to *Block, add indent/width args
  logview.go      NEW  — buildLogLines() + stepLineRange + block traversal
  tree.go         —    — unchanged
  output.go       —    — unchanged (tail cap preserved)
  breadcrumb.go   —    — unchanged (possibly minor help-bar text update)
internal/config/
  config.go       MOD  — add `summarizer` to defaultConfig()
```

### Core data

New in `logview.go`:

```go
type stepLineRange struct {
    node      *StepNode
    startLine int  // inclusive, 0-based into the log's line slice
    endLine   int  // exclusive
}

// Builds the continuous log for the current drill-in level.
// pendingSelected is non-nil when the cursor is on a pending step, triggering
// a ghost block for it at its execution-order position.
func buildLogLines(
    children []*StepNode,
    pendingSelected *StepNode,
    bodyWidth int,
    loadedFull map[string]bool,
) (lines []string, ranges []stepLineRange)
```

`buildLogLines` walks `children` in execution order. For each started child, it calls the appropriate `render<Type>Block(node, indent, width, loadedFull)` from `detail.go`, which returns `[]string`. For container children (sub-workflow, loop, iteration), after emitting the header it recurses into the container's children at `indent+1`. Pending children are skipped unless the child is `pendingSelected`, in which case a ghost block is emitted.

`stepLineRange` is accumulated as lines are appended: each node's range spans the first line of its header through the last line of its deepest descendant's block. This gives us step→line and line→step mapping for scroll sync.

### Model state changes

```go
type Model struct {
    // existing fields ...

    // REMOVED:
    // tailFollow bool

    // RENAMED:
    // detailOffset int  →  logOffset int

    // NEW:
    stepRanges    []stepLineRange  // rebuilt in Update() on any state change
    logAnchor     stepLineAnchor   // for resize stability
}

type stepLineAnchor struct {
    stepKey            string
    lineOffsetInBlock  int
}
```

`stepRanges` is recomputed in `Update()` whenever:
- An `OutputChunkMsg`, `StepStateMsg`, `ExecDoneMsg`, or `RefreshMsg` mutates the tree
- The user drills in or out (path changes)
- The selection moves onto or off a pending step (affects ghost block presence)
- The terminal resizes (line wrap changes)

`stepRanges` is **not** rebuilt on `↑`/`↓` alone (the tree and content haven't changed), but those key handlers use the existing `stepRanges` to compute the new `logOffset`.

View() reads `stepRanges` and `logOffset`, slices `lines[logOffset:logOffset+bodyHeight]`, and renders.

### Rendering pipeline

`view.go` orchestrates per frame:

1. Compute left-pane width (same 45%-cap rule as today).
2. Build left rows:
   - `buildStepRows()` over `currentChildren()` — one row per sibling, with status/type glyphs unchanged.
   - For the selected sibling: append `buildExpansionRows(selected)` — recursive walk down the active-descendant chain, emitting one indented row per level.
3. Build log:
   - Call `buildLogLines(currentChildren(), pendingSelected, rightWidth, loadedFull)`.
   - Slice to viewport: `lines[clamp(logOffset, 0, max) : clamp(logOffset+bodyHeight, 0, len(lines))]`.
4. Assemble final two-column frame.

### Block rendering (detail.go)

The existing per-step renderers — `renderShellDetail`, `renderHeadlessDetail`, `renderInteractiveDetail`, `renderSubWorkflowDetail`, `renderLoopDetail`, `renderIterationDetail` — are renamed to `*Block` and their signatures change:

```go
// BEFORE: func renderShellDetail(node *StepNode, width int) []string
// AFTER:
func renderShellBlock(node *StepNode, indent int, width int, loadedFull bool) []string
```

The `indent` parameter controls:
- Header separator glyphs (see below).
- Leading indent applied to every line in the block (2 spaces × indent).
- Effective content width (`width - 2*indent`).

Sub-workflow and loop blocks return *only their own header lines*; their children's lines are emitted by `buildLogLines` recursively. This separation keeps each block renderer focused on one node's content.

### Separator glyphs per depth

```
Depth 0:   ═══ name ═══════════════ glyph ═
Depth 1:   ─── name ───────────────── glyph ─
Depth 2:   ─ name ──────────────────── glyph
Depth 3+:  · name ·························· glyph
```

Inside a block, sub-separators between subsections (e.g., command → stdout → stderr) use `····`-style lines regardless of block depth; they're relative to the block, not the tree.

Iteration blocks use a distinct header form: `─── iter 2/5 ─────────────── ci.yaml ─` for auto-flattened iterations (binding target shown in place of type glyph) or `─── iter 2/5 · task_file=foo.md ───` for for-each iterations.

### Indentation

2 spaces per depth level applied to the whole block (header, metadata, output). Keeps content visually bounded. At depth 4 on an 80-col terminal, content width is ~70 cols — still readable.

### Scroll sync

**Left → Log.** After `↑`/`↓` moves the cursor:
```
selected := currentChildren()[cursor]
r := find(stepRanges, selected)
m.logOffset = clamp(r.startLine, 0, len(lines)-bodyHeight)
m.logAnchor = stepLineAnchor{stepKey: selected.Key, lineOffsetInBlock: 0}
```

**Log → Left.** After `j`/`k`/wheel updates `logOffset`:
```
viewport := [logOffset, logOffset+bodyHeight)
var winner *StepNode
for _, r := range stepRanges {
    if overlaps(r, viewport) && (winner == nil || execOrder(r.node) > execOrder(winner)) {
        winner = r.node
    }
}
// map winner to ancestor at current drill-in level
target := ancestorAtLevel(winner, currentLevel)
m.cursor = indexOf(currentChildren(), target)
m.logAnchor = stepLineAnchor{stepKey: topRangeInViewport.node.Key, lineOffsetInBlock: logOffset - topRangeInViewport.startLine}
```

`stepRanges` includes **every node in the subtree** rooted at the current drill-in level — not just direct children — so nested nodes can be "winners" and get mapped up to their current-level ancestor.

Execution order: prefer audit-event start timestamp; fall back to tree position (YAML order) for pending/uninstrumented nodes. Tie-break by tree position.

### Auto-scroll

`tailFollow bool` is removed. In `Update()`, any `OutputChunkMsg`, `StepStateMsg`, `ExecDoneMsg`, or `RefreshMsg` that mutated the tree sets:
```
if m.active || m.running {
    m.logOffset = math.MaxInt32  // clamped to max during View()
}
```
Inactive runs preserve `logOffset` across state changes. The `t` keybinding is removed.

### Auto-follow behavior

Auto-follow is a flag `m.autoFollow` that's engaged by default on view entry. When engaged and `StepStateMsg` arrives:

```
active := activeStepFromTree(root)
target := ancestorAtLevel(active, currentLevel)  // top-level-at-current-level
m.cursor = indexOf(currentChildren(), target)
// do NOT mutate m.path (no drill-in)
```

The left expansion under the newly selected step naturally reveals the deeper active descendant path through its existing recursive-expansion mechanism.

User actions that move the cursor (arrows, `j`/`k`, wheel) disengage auto-follow by setting `m.autoFollow = false`. Pressing `l` re-engages it.

### Keyboard

```
↑/↓      navigate step list + scroll log to selected block
j/k      scroll log 1 line + update cursor to latest-in-viewport
wheel    scroll log 3 lines + update cursor
Enter    drill into container; resume agent CLI session on agent step
Esc      drill out one level; exit at top
r        resume the workflow run (unchanged from resume-run capability)
g        load full output for cursor-selected step (no-op on containers)
l        re-engage auto-follow
?        legend overlay
q / ^C   exit
```

Removed: `t` (tail-follow toggle, replaced by unconditional auto-scroll while active).

Help-bar text: `↑↓ step · j/k scroll · Enter drill · Esc back · r resume run · g full output · l follow · ? legend · q quit`.

### Resize behavior

On `tea.WindowSizeMsg`:
1. Recompute `rightWidth` and `bodyHeight`.
2. Rebuild `stepRanges` with the new width (word-wrap changes line counts).
3. Re-resolve `m.logAnchor`: find the anchor's step's new `startLine` in the new ranges, set `m.logOffset = newStart + lineOffsetInBlock`.

The anchor ensures the step the user was reading stays in roughly the same position across resize.

### Pending ghost block

When `selectedNode().Status == pending`:
- `buildLogLines` receives `pendingSelected = selectedNode()`.
- During traversal, when the algorithm reaches `pendingSelected`'s execution-order position among its siblings, it emits a ghost block using a dashed separator (`- - -`) and dimmed color (via `tuistyle` palette).
- Ghost block content: step name, type glyph, configured command/prompt/sub-workflow path, params as raw template strings.

Spec-visible difference from a real block: no runtime fields (exit code, duration, output). The visual distinction is design-layer only (dim color, dashed separator).

### `summarizer` profile addition

One edit to `internal/config/config.go::defaultConfig()`:

```go
cfg.Profiles["summarizer"] = Profile{
    DefaultMode: "headless",
    CLI:         "claude",
    Model:       "haiku",
    Effort:      "low",
}
```

Extends-based alternative (`extends: headless_base` with `model: haiku` override) considered but rejected: the summarizer should not track the `headless_base` model's default (opus) — it deliberately uses haiku for cost. A standalone definition makes that independence explicit.

### Testing strategy

- **Unit tests for `buildLogLines`**: synthetic `StepNode` trees covering: flat, sub-workflow children, loop iterations, deep nesting (4+ levels), pending mix, pending selected, large output (verify tail cap applied), ANSI stripping. Assertions: exact line count, `stepLineRange` bounds, known header strings present.
- **Unit tests for scroll-sync math**: given `stepRanges` + `logOffset`, correct `latestInViewport` selection; given `cursor`, correct `logOffset` mapping.
- **Unit tests for auto-follow**: simulate `StepStateMsg` with active step at various nesting depths, assert cursor lands on correct ancestor-at-current-level and `m.path` is unchanged.
- **Two integration golden files**: one mid-run snapshot with nested content, one completed-run snapshot. Avoid golden tests for live scroll states.
- **Existing golden tests** in `runview/` will all need replacement — content fundamentally changed. Delete and rewrite during implementation.

## Decisions

**1. In-package `logview.go` over new package.** A new file inside `runview/` avoids import cycles (tree, output, tuistyle are all there) while keeping the log-building code separate from model/view plumbing. A new package would require exposing `StepNode` more widely with no benefit.

**2. `buildLogLines` is called in both `Update` and `View`.** `Update` needs ranges for scroll-sync math; `View` needs lines for rendering. Calling twice is wasted work but negligible (pure computation, bounded by tail-capped output). Considered caching the result on the model — decided against: invalidation rules get subtle (tree changed? width changed? selection changed? pending changed?), and the re-compute is cheap enough.

**3. Per-step tail cap preserved (35 lines with `g` to load full).** `g` operates on the cursor-selected step, same as today. Alternative — unlimited output in log — rejected: a single chatty agent step could blow the log to 10k+ lines.

**4. Ghost block for pending-selected only.** Pending steps are not in the log by default. A ghost block is injected *only* when the cursor is on a pending step, disappearing on cursor move. Keeps the log focused on what has actually happened while preserving visibility when the user asks about a pending step.

**5. Auto-scroll is unconditional while active.** Rejected classic tail-follow with pause-on-user-scrollup. Per-the-user direction, simpler and less state. Tradeoff: users who want to read earlier output must pause the run (there is no pause) or re-scroll repeatedly. Acceptable for the watch-it-run use case.

**6. Read-only expansion in left pane.** Cursor moves only among current-level siblings. Navigation into expansion sub-rows would require their steps to be selectable, which duplicates what drill-in already provides. Drill-in (Enter) is the interactive path.

**7. Scroll-sync maps deep → top-level ancestor.** When the log shows nested content, the left cursor resolves to the ancestor-at-current-level. This keeps the left pane a consistent height and meaning. Alternative (b) — looking only at top-level children's ranges — rejected because nested blocks naturally extend a top-level child's range anyway, so (a) and (b) would produce similar results; (a) is more precise.

**8. `summarizer` as a code-defined default rather than documentation-only.** Users can still override it in their config.yaml. Making it a default means workflow authors who write `agent: summarizer` on a fresh repo get it working immediately.

## Risks / Trade-offs

- **[Rendering cost at scale]** → 100+ steps with deep nesting could produce 3000+ log lines rebuilt on every Update. With tail-capped output and modern terminals, this should be under 10ms. If profiling later reveals an issue, cache rendered block lines keyed on `(nodeKey, outputLen, status, indent, width)`.
- **[Auto-scroll fights the user]** → user scrolls up to read; new output snaps them back. Deliberate per direction; mitigation is disabling the workflow run via Ctrl+C if the user really needs to read during a live run. `r` resumes later.
- **[Test churn]** → every existing `view.go`/`detail.go` golden test fails. Planned: delete and rewrite during implementation; unit tests for `buildLogLines` cover behavioral regressions.
- **[Separator weight exhaustion]** → at depth 4+, all deeper levels share the `·` separator. Acceptable since real workflows rarely nest beyond 3–4 levels (workflow → loop → iteration → sub-workflow).
- **[Resize wrap drift]** → anchor-based resize stability is best-effort; if the anchored step is past the new log's bottom, we clamp. User perceives a single jump.
- **[`summarizer` profile cost visibility]** → users running a summary step invoke haiku which has a non-zero cost. Documentation (separate concern) should surface this. No in-code mitigation.

## Migration Plan

This change depends on the already-archived `live-run-view`, `resume-run`, and `change-exit-behavior` changes (all merged pre-implementation). No further openspec dependencies.

Implementation order:
1. `summarizer` profile addition — small, isolated.
2. `logview.go` + block renderer rename — new file and signature changes; existing tests keep passing because `view.go` still calls old renderers.
3. `model.go` scroll-state changes, `view.go` rewrite — cut over to new log pane.
4. Left-pane expansion + auto-follow behavior change.
5. Golden-test rewrites.

No data migration; audit logs and on-disk state unchanged.

Rollback: revert the change. On-disk state format is unchanged, so reverting is safe even mid-run.

## Open Questions

- Inline resume-action hint in each agent block's header (deferred; users discover `r` via legend for now).
- Jump-to-top/bottom keybindings (`gg`/`G` or `Home`/`End`) — deferred. Current `j`/`k` + wheel is sufficient for now.
- Scroll position indicator (mini-map or percentage) — deferred.
