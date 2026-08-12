## Context

The run view currently derives both panes from the current manual drill level:

- `internal/runview/view.go` renders direct children in the left pane and adds a
  read-only expansion beneath only the selected container.
- `internal/runview/logview.go` recursively renders every started descendant as
  one continuous right-pane log.
- `internal/runview/model.go` stores selection as a direct-child cursor and
  synchronizes that cursor with cross-step log ranges and offsets.
- Live auto-follow selects an ancestor at the current drill level, except for
  agent calls and live UI steps, which can invoke navigation that changes drill
  scope.

That structure produces the two product problems this change addresses: the
active leaf is often not visible without drilling, while the detail pane
contains too much cross-step history. The approved specifications replace those
behaviors with a selectable flattened tree and a detail document for exactly
one selected execution.

Existing mechanisms that remain useful:

- `StepNode` and `NodeKey()` provide a stable in-memory projection of static and
  dynamic execution nodes.
- `path []*StepNode` remains the manual drill scope used by breadcrumbs and
  level-scoped summaries. Resume fallback instead resolves the nearest
  workflow-bearing ancestor of the selected node so inline selection and
  manual drill produce the same target.
- Audit replay and incremental tailing already apply events to one stable tree.
- Autonomous output already accumulates separately on its owning node.
- `loadedFull`, `truncateOutput`, and the persisted agent-call output loader
  provide the existing large-output safeguard.
- The embedded `uistep.Model` already supports rendering a live form inside the
  run-view chrome.
- Lip Gloss and the shared adaptive color palette can render labeled left rails
  without adding a dependency.

No persisted run format needs to change. The design adds only in-memory
projection state derived while existing audit events are replayed.

## Goals / Non-Goals

**Goals:**

- Always expose the active execution leaf in the current root or manual drill
  scope without automatically changing that scope.
- Allow every visible real nested row to be selected with Up and Down.
- Keep inline expansion bounded while preserving both active and selected
  context.
- Size the sidebar from the complete visible tree and keep its width stable
  while the user reads.
- Render one compact selected-step document containing a prior-execution recap,
  bounded current input, full current output, and useful primary metadata.
- Preserve response streaming, live UI forms, copy, resume, summary, and
  large-output behavior.
- Give live active-step following and selected-response tail following
  independent state.

**Non-Goals:**

- Changing workflow execution, audit event schemas, state files, or output
  capture.
- Producing AI-generated recaps.
- Capturing interactive terminal transcripts that Agent Runner does not
  currently own.
- Replacing the summary screen, list view, or workflow-definition view.
- Introducing a general terminal layout or document-rendering framework.

## Approach

### 1. Keep manual scope, selection, and active execution independent

The run-view model will represent three separate concepts:

1. **Manual scope** — the existing `path`, changed only by explicit drill-in or
   drill-out.
2. **Selected node** — the real `StepNode` whose detail is shown.
3. **Active node** — the current execution frontier resolved from live state or
   the deepest in-progress node.

Replace the direct-child meaning of `cursor` with node-based selection. The
model may retain a cached flattened-row index for rendering, but `*StepNode`
or its stable `NodeKey()` is authoritative. This prevents insertion of dynamic
agent calls or expansion changes from silently moving selection to another
execution.

Auto-follow updates the active node, selected node, and derived expansion. It
never appends to or truncates `path`. Pressing `l` is the explicit exception
that returns to root manual scope before selecting the active leaf; it still
does not drill into that leaf.

Manual drill-in retains its existing purpose. Enter on a drillable selected
container changes `path` and initially selects the first real direct child in
workflow order. The direct children of the drilled container are all projected,
subject only to the ordinary vertical viewport. The existing iteration with one
sub-workflow child auto-flatten special case remains: both manual drill and
inline projection skip that degenerate display level while retaining the
iteration breadcrumb and sub-workflow header.

For completed-run `r` fallback, walk from the selected node to its nearest
workflow-bearing ancestor and search that workflow for its last resumable agent
execution. A directly resumable selected agent or call wins first; root workflow
is the final fallback. This avoids using `currentContainer()` when selection
came from inline expansion outside the manual drill path.

### 2. Project one flattened visible tree

Introduce a focused tree-projection helper in `internal/runview/`, conceptually:

```go
type treeRowKind int

const (
    treeRowNode treeRowKind = iota
    treeRowOmission
)

type treeRow struct {
    kind      treeRowKind
    node      *StepNode
    depth     int
    omitted   int
    placement omissionPlacement // earlier, between, or later
}

func projectTree(scope, selected, active *StepNode) []treeRow
```

The projector starts with every direct child of the manual scope. It recursively
expands containers on the selected ancestry plus the active ancestry whenever
the active node lies inside the manual scope. All node rows are selectable;
omission rows are not.

Pending sub-workflows that must expose statically knowable descendants are
resolved before projection through the existing lazy loader. Projection itself
should remain deterministic for a given tree and focus state.

#### Single-focus child window

At a parent scope, an expanded container exposes at most five real direct
children. With one focal direct child, show two siblings before and two after,
filling unused capacity from the available side. With no focal descendant,
show the final five children of a completed container or first five of an
entirely pending container.

#### Dual-focus child window

The active and selected ancestries can pass through different direct children
of the same container. Both focal children must remain visible without
increasing the five-real-child bound.

The window algorithm:

1. Include both focal children.
2. Rank every remaining sibling by distance to its nearest focus.
3. Break equal-distance ties by workflow order.
4. Add the first three candidates, or fewer when fewer exist.
5. Sort the selected real children back into workflow order.
6. Insert one unselectable omission row for every contiguous omitted region.

This keeps the result deterministic and stable for a fixed pair of foci while
always preserving both selected and active rows. Workflow-order tie-breaking
avoids arbitrary changes when the foci are equidistant. Omission labels identify
position relative to the visible regions:

```text
  ✓ step 1
▶ ✓ step 2
  ○ step 3
  … 10 between
  ✓ step 14
  ● step 15
  … 5 later
```

The five-child limit counts only real node rows. `earlier`, `between`, and
`later` markers do not consume that budget and are never selectable.

Up and Down find the selected node in the projected selectable rows and move to
the adjacent selectable row. A selection change rebuilds the projection, so
the new selected ancestry expands and an inactive branch left behind collapses.
The left viewport minimally scrolls to the active selected row while
`followActive` is engaged; while it is paused, the viewport instead minimally
scrolls to the manually selected row even if the active row is elsewhere.

### 3. Measure complete rows before truncating names

Split row construction into logical parts rather than truncating the name in
`stepRowParts`:

```go
type treeRowParts struct {
    cursor string
    indent string
    status string
    name   string
    suffix string
    kind   string
}
```

Measure each complete, unstyled row with its indentation, status, suffix, and
type glyph. Layout then computes:

- preferred sidebar width from the widest visible row;
- a small whitespace gap between panes, with no full-height divider;
- a hard detail-pane minimum of 20 columns whenever the terminal can fit that
  minimum plus essential tree chrome;
- the sidebar cap from the remaining terminal width rather than a fixed
  percentage.

Only the name is truncated when the complete row exceeds the settled sidebar
width. Cursor, indentation, status, loop or call-count suffix, and type glyph
remain intact.

There is no 50% or other proportional sidebar ceiling. If the terminal cannot
fit essential tree chrome, the whitespace gap, and 20 detail columns, the name
has already collapsed to an ellipsis; retain fixed tree chrome while it fits,
then allow detail width to fall below 20 as the unavoidable final degradation.
If even fixed elements do not fit, clamp and clip at the terminal boundary.
Layout calculations must never produce negative dimensions or panic.

Add a remembered sidebar width to `Model`. It grows when a wider visible row
fits, but never shrinks during the same view entry. A terminal resize clears
that remembered measurement and recomputes both panes from the new width.
Entering a new run-view model naturally starts with no remembered width.

### 4. Build a semantic document for one selected node

Replace the recursive continuous-log assembly with a selected-detail builder:

```go
type detailDocument struct {
    header   []detailLine
    sections []detailSection
}

type detailSection struct {
    label   string
    tone    railTone
    display []string
    copy    []string
}
```

The builder reads one selected node and emits:

1. Compact identity and primary metadata.
2. An optional `Previous: <step-name>` section.
3. The applicable current input section.
4. The applicable current response, output, outcome, or status section.

The compact header retains the established model and metrics semantics rather
than merely omitting unknown values. Model follows CLI, comes from the profile
default or session-originating step for resumed/inherited sessions, and falls
back to `(unknown)`. Structured unavailable usage renders `?` with its reason
when known, absent cost renders `?` rather than `$0.00`, and legacy events with
no metrics fields omit those lines. Agent-call session and workdir remain stored
but appear only for resume or error context; `(N calls)`, target, filtering,
raw evidence, and direct-resume behavior remain intact.

The renderer wraps those semantic values to the current detail width and adds
Lip Gloss rail styling. The copy path renders the same document as plain text
but substitutes each section's `copy` body. That lets the on-screen input stay
collapsed while copied detail contains its complete untruncated source.

Store current-input expansion by `NodeKey()` so a resize or temporary selection
change does not discard the user's choice. If wrapping at the new width makes
the input fit within three visual rows, the toggle disappears; the stored choice
can remain dormant in case another resize makes expansion applicable again.

The existing large-output state also remains keyed by `NodeKey()`. Full current
response/output uses the node's currently loaded data and existing truncation
banner. The `g` action continues to load the selected execution's complete
output.

### 5. Derive previous execution from audit start order

Add an in-memory monotonically increasing start ordinal to `Tree` and the latest
start ordinal to `StepNode`. Assign it when applying:

- `step_start`;
- `iteration_start`;
- `sub_workflow_start`;
- `agent_call_start`;
- any equivalent container start needed to order a selectable execution.

Full audit replay and incremental tailing therefore produce the same order
without relying on timestamps being unique. Re-executing a logical node updates
its ordinal, which intentionally makes the latest attempt authoritative.

For selected detail, scan the tree for terminal leaves whose latest ordinal is
nonzero and lower than the selected execution's ordinal. The greatest such
ordinal is the previous execution. Containers are excluded as candidates;
agent calls and skipped leaves are included. A pending selected node with no
established start ordinal has no previous rail.

Previous excerpts use the same source rules as selected output:

- headless agent and agent call: filtered response;
- successful shell or script: stdout;
- failed shell or script: stderr, falling back to stdout;
- interactive agent or shell: metadata only because no transcript is captured;
- UI: recorded outcome only, never submitted values;
- skipped leaf: status plus the triggering `skip_if` expression, with no output
  excerpt.

The excerpt pipeline sanitizes, removes blank trailing rows, wraps at the
current detail width, and keeps the final two visual rows. It prepends an
ellipsis when earlier rendered content was omitted. Resize simply rebuilds the
document, so the bound remains two visual rows.

Agent-call output is not reconstructed from audit data. Generalize the existing
bounded persisted call-output loader so it can ensure output for either the
selected call or a previous-call excerpt, while retaining current live
duplication guards and adapter filtering.

Ordinary autonomous output continues through the existing coordinator message
path and raw output files. Preserve the 100 ms first-byte display target, the
`<sessionDir>/output/<step-prefix>.out` and `.err` paths, `/` to `__` and `:` to
`_` prefix escaping, stateful removal of ANSI sequences split across chunks,
and unmodified raw persisted bytes. This change only changes which selected
document receives the sanitized stream.

### 6. Render labeled rails instead of blocks and separators

Define run-view-local rail styles with a left-only Lip Gloss border. Use one
blank row between sections and no enclosing box:

- previous execution — muted amber;
- current prompt, command, script, or form — cyan;
- current response, output, outcome, or status — green;
- errors — red.

The selected detail header remains above the rails and contains compact metadata
rather than a full-width separator.

#### Previous interactive-agent execution

```text
  ✓ proposal  ❯                         crosscheck  ↗                         ● running
▶ ● crosscheck  ↗                       agent call · codex · gpt-5.6
  ○ specs  ❯                            6.8k tokens · $0.08

                                      ▎ Previous: proposal
                                      ▎ interactive agent · ✓ success · 1m 42s
                                      ▎ planner · claude · sonnet
                                      ▎
                                      ▎ No transcript captured

                                      ▎ Current prompt                         i expand
                                      ▎ Review the OpenSpec proposal under
                                      ▎ openspec/changes/make-run-view-great…
                                      ▎ …

                                      ▎ Current response
                                      ▎ I’m reviewing the proposal against the
                                      ▎ repository’s current run-view behavior.
                                      ▎ ◐
```

`No transcript captured` is fixed explanatory presentation copy, not a
generated summary.

#### Previous script execution

```text
  ✓ define  ❯                           implement  ❯                           ● running
  ✓ plan  ❯                             workflow · 3/7 children complete
▶ ● implement  ❯

                                      ▎ Previous: validate-specs
                                      ▎ script · ✓ success · exit 0 · 4.2s
                                      ▎ …All OpenSpec artifacts passed strict
                                      ▎ validation.

                                      ▎ Current status
                                      ▎ running · 3 success · 1 running
                                      ▎ 3 pending · 0 failed
```

#### Failed script recap

```text
                                      ▎ Previous: run-tests
                                      ▎ script · ✗ failed · exit 1 · 18.7s
                                      ▎ …expected active leaf to remain visible
                                      ▎ FAIL internal/runview
```

### 7. Give active follow and tail follow independent state

Replace the overloaded `autoFollow` behavior with two explicit flags:

- `followActive` — execution may move tree selection to the active leaf;
- `followTail` — selected streaming detail stays at its tail.

Maintain one selected-detail scroll offset rather than cross-step log ranges.
Selection changes reset detail to its top unless selection was produced by
active follow, in which case the active response opens at its tail.

State transitions:

- Up/Down or manual drill navigation disables `followActive`.
- Scrolling upward with `k` or mouse wheel disables both flags.
- `j` scrolls only the selected detail and never changes tree selection.
- `t` moves the same selected detail to its tail and enables only
  `followTail`.
- `l` returns to root manual scope, selects the current active leaf, moves to
  its tail, and enables both flags.
- New output updates only its owning node. It changes selection only when
  `followActive` is engaged and that node becomes the execution frontier.
- A selected streaming response remains at its tail only while `followTail` is
  engaged.

Remove `stepRanges`, cross-step log anchors, selection-from-log synchronization,
and recursive block ranges after the selected-detail renderer covers all step
types.

### 8. Integrate live UI as selected-node detail

Keep `uistep.Model` alive independently of tree selection. When its active UI
node is selected, render the form inside `Current form` and route a key to the
child only when it is applicable to the focused form control. Up/Down, Escape,
`j`, `k`, `l`, `q`, and Ctrl+C remain owned by the run view as defined by the
UI-step specification. Remove the current `d` drill branch; Enter drills only
when selection is on a non-UI drillable container, including while another UI
step remains pending.

When selection moves elsewhere, stop routing form keys and render ordinary
detail for the newly selected node. The UI model and reply channel remain
untouched, so the workflow stays pending. Pressing `l` restores root scope and
selects the UI leaf without drilling or resolving it.

After the form resolves, retained UI outcome data renders through
`Current outcome`; submitted values are never included in a previous-execution
recap.

### 9. Keep live and historical views on one rendering path

The tree projection, pane layout, detail-document builder, copy path, lazy
output, and manual drill behavior are shared.

Live-only behavior consists of:

- incremental audit and output updates;
- active and tail follow;
- progress animation;
- live UI input;
- `l` and `t` follow actions.

Historical views do not create those transitions or animations. Failed
historical entry selects the failed leaf with root manual scope. Completed runs
with structured metrics still open on the existing summary screen before the
user switches to detail. Non-live saved runs retain the recorded workflow
version (or `unversioned`) in the breadcrumb at every drill depth; live and
definition-preview breadcrumbs remain version-free.

## Alternatives Considered

### Filter the existing continuous log down to the selected block

Rejected. The current log builder recursively emits descendants and its range
model exists to synchronize cross-step scrolling with a direct-child cursor.
Filtering its output would preserve the wrong ownership boundaries, complicate
copying full collapsed input, and leave active nested selection coupled to
drill depth.

### Automatically mutate drill scope to follow the active leaf

Rejected. This is the current behavior that makes the left side disorienting.
Manual scope is useful for breadcrumbs and zooming, but execution progress must
not take it away from the user.

### Store an arbitrary set of expanded containers

Rejected for the initial implementation. Expansion has two deterministic
causes—selected ancestry and the active ancestry when it lies inside manual
scope—so a mutable expansion set would add stale-state reconciliation without
enabling approved behavior. Manual drill already supplies the explicit
all-children view.

### Increase the inline window beyond five when there are two foci

Rejected. A dual-focus five-child projection plus exact `between` omissions
keeps the tree bounded and still guarantees that both important rows remain
visible.

### Persist a new execution-order artifact

Rejected. Existing ordered audit replay supplies everything required. An
in-memory ordinal avoids timestamp ties without introducing a format migration
or duplicating durable evidence.

### Generate natural-language previous-step summaries

Rejected. Interactive output frequently does not exist, generation would add
latency and cost, and a generated recap would be less deterministic than a
bounded tail of recorded evidence.

## Risks / Trade-offs

- **Tree projection complexity** — Selected and active ancestries can overlap,
  diverge, or pass through lazy and dynamic nodes. Keep the projector pure after
  lazy loading and cover single-focus, dual-focus, nested, and dynamic-call
  cases with table-driven tests.
- **Selection migration touches many helpers** — Current resume, copy, summary,
  UI, and help code assumes a direct-child cursor. Introduce node-based
  selection helpers first and migrate consumers before deleting cursor-era
  synchronization code.
- **Audit ordinal replay drift** — Ordinals must be assigned only for genuine
  start events and in exactly replay order. Tests should cover full replay,
  incremental tailing, retry, call, and container events.
- **Previous call output adds I/O** — Loading a previous agent-call excerpt can
  touch persisted output even when the call is not selected. Reuse the bounded
  loader, cache its result on the node, and never read unbounded files.
- **Width changes rewrap several sections** — Keep semantic content separate
  from rendered rows so resize can rebuild input and recap bounds without
  mutating stored evidence.
- **Extremely narrow terminals cannot preserve every preferred width** — Apply
  the documented degradation order, clamp every dimension, and cover widths
  below the normal tree-plus-20-column threshold.
- **UI input ownership is easy to regress** — Route child form keys only when
  the exact UI leaf and applicable control are selected, remove the `d` drill
  branch, and test navigation away and back while the form remains pending.
- **Removing the continuous log is a broad behavioral replacement** — Land the
  selected-detail builder with focused per-type coverage before deleting old
  range and recursive rendering paths.

## Implementation and Verification

Use test-driven development for the behavioral changes:

1. Add pure projection tests for selectable flattened rows, single-focus and
   dual-focus windows, exact omission markers, collapse behavior, manual drill,
   and agent calls.
2. Add layout tests for full-name measurement, name-only truncation, detail
   minimum, no proportional cap, extremely narrow degradation, whitespace
   separation, grow-only width, independent tree viewport, and resize reset.
3. Add audit-projection tests for start ordinals, latest attempts, previous
   terminal-leaf selection, and skipped-leaf reason handling.
4. Add detail-document tests for every step type, metadata filtering, visual-row
   recap and input bounds, copy substitution, lazy output, error rails, and
   container aggregation.
5. Add model tests for node-based Up/Down traversal, first-child drill and
   auto-flatten behavior, nested-selection resume fallback, `i`, `t`, `l`,
   independent follow flags, continuous tailing after `t`, streaming without
   selection theft, failure initialization, saved-run versions, and inactive
   historical behavior.
6. Extend live UI and agent-call tests for selectable leaves, applicable-only
   form key routing, Enter drill ownership, absence of `d`, call-count and
   resume preservation, persisted call excerpts, output attribution, output
   path escaping, split ANSI sequences, and first-byte latency.
7. Run targeted `go test ./internal/runview` iterations, then `make fmt`,
   `make test`, and `make lint`.
8. Run `openspec validate make-run-view-great --strict`.

No external dependency, workflow YAML, audit schema, state schema, or migration
step is required.
