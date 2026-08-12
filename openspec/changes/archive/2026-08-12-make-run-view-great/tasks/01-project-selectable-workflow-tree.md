# Task: Project the selectable workflow tree and responsive layout

## Goal

Replace direct-child cursor semantics and read-only selected-row expansion with one node-based, flattened workflow-tree projection. Every real visible row must be selectable, manual drill scope must remain independent from selection and active execution, and the sidebar must size from complete rows without destabilizing the detail pane.

This task delivers the reusable tree and layout foundation consumed by live and historical run views. It must be useful and fully tested on its own: navigation, projection, drilling, viewport ownership, responsive sizing, row rendering, help, and agent-call hierarchy all belong here.

## Background

You MUST read:

- `openspec/changes/make-run-view-great/proposal.md`, especially “Shared Run View” and the capability mapping.
- `openspec/changes/make-run-view-great/design.md`, especially sections 1–3 (“Keep manual scope, selection, and active execution independent”, “Project one flattened visible tree”, and “Measure complete rows before truncating names”).
- `openspec/changes/make-run-view-great/specs/view-run/spec.md`, especially `Step list rendering`, `Drill-in navigation with breadcrumbs`, `Keyboard focus and scrolling`, `Step list inline expansion of direct children under selected step`, and `Agent-call hierarchy rendering`.
- `openspec/changes/make-run-view-great/test-plan.md` for the pure tree-projection and layout strategy. Do not execute or claim the `AT-*` flows in this implementation task.

Relevant production paths:

- `internal/runview/tree.go`: `Tree`, `StepNode`, `NodeKey`, lazy sub-workflow loading, iterations, groups, and dynamic agent-call children.
- `internal/runview/model.go`: current direct-child cursor, `path`, key handling, drill behavior, tree and detail offsets, selection restoration across tree rebuilds, help, and summary/resume consumers.
- `internal/runview/view.go`: step-row construction, selected-row expansion, row measurement/truncation, type/status glyphs, two-pane widths, viewport calculations, legend, and whitespace.
- `internal/runview/breadcrumb.go`, `internal/runview/summary.go`, and `internal/runview/resolve.go`: manual scope, saved workflow versions, scoped summaries, pending-tree recovery, and deleted-workflow reconstruction.
- `internal/runview/tree_test.go`, `view_test.go`, `model_test.go`, `agent_call_view_test.go`, `resolve_test.go`, and `tui_metrics_test.go`.

Implementation constraints:

- Use test-driven development. Start with pure projection and layout tests that fail against the current direct-child/read-only expansion behavior.
- Keep `path []*StepNode` as explicit manual drill scope. Store selection by `*StepNode` or stable `NodeKey()`; a cached projected-row index may be derived but cannot be authoritative.
- Project direct children of the current manual scope and recursively expand only selected ancestry plus in-scope active ancestry. Omission rows are derived presentation, never selectable nodes.
- Implement the design’s single-focus and dual-focus five-real-child algorithms exactly. The five-child limit excludes omission rows. Equal-distance dual-focus ties resolve in workflow order, then visible real children render in workflow order.
- Preserve the iteration-with-one-sub-workflow auto-flatten special case for both drilled and inline projections.
- Enter is the only drill key. It applies to sub-workflows, loops, iterations, groups, and agent parents with accepted calls; it selects the first real direct child. Enter on a non-container leaf remains a no-op here.
- A drilled container shows every direct child through the ordinary tree viewport; it is not subject to inline five-child windowing.
- Pending resolvable containers must lazy-load before projection, but projection itself must stay pure and deterministic once the tree is ready.
- Measure complete unstyled rows before truncating names. Preserve cursor, indentation, status, loop/call suffixes, and type glyphs. There is no proportional sidebar ceiling.
- Remember the widest settled sidebar width for one model entry. It may grow, never shrink, until a terminal resize resets the measurement.
- On very narrow terminals, apply the approved degradation order and clamp every dimension; never panic or produce negative widths.
- Keep saved-run version labels, `unversioned`, live/definition-preview version omission, level-scoped summaries, and pending/deleted-workflow reconstruction intact.
- Keep this task focused on tree/layout ownership. Do not redesign selected-execution document content beyond the narrow adapter changes required to make every nested selection identify the correct node for detail consumers.

## Spec

The following source requirements and scenarios are controlling excerpts from `specs/view-run/spec.md`.

### Requirement: Step list rendering

The run view SHALL render the current manual drill scope as a selectable workflow tree on the left and the selected row's detail on the right. The panes SHALL be separated by whitespace; the run view SHALL NOT render a full-height vertical separator between them.

Each real tree row SHALL display, in order: indentation for its tree depth, a status indicator, the step name, and the type glyph. Loop rows SHALL additionally display an iteration counter in the form `(N/M)`. Iteration rows SHALL NOT display binding values, per-iteration parameters, or arguments.

Step statuses SHALL remain `pending`, `in-progress`, `success`, `failed`, and `skipped`. The `in-progress` indicator SHALL blink only while the run is active; an interrupted step in an inactive run SHALL render a static in-progress indicator. Loop exhaustion SHALL render as success. When an expanded container and a visible descendant both have `in-progress` status, only the deepest visible active row SHALL display the running indicator; ancestor rows SHALL preserve alignment with blank indicator space.

The sidebar SHALL measure complete visible rows, including indentation, before truncating names. A name SHALL remain untruncated whenever the preferred sidebar width and a usable detail pane fit within the terminal. When they do not fit, the sidebar SHALL truncate names with an ellipsis while preserving indentation, status, suffixes, and type glyphs. The detail pane SHALL retain at least 20 visual columns whenever the terminal can accommodate the minimum tree chrome, whitespace gap, and that width. The sidebar SHALL NOT have a proportional-width ceiling.

When the terminal is too narrow to fit minimum tree chrome, the whitespace gap, and 20 detail columns, layout SHALL first reduce the name to an ellipsis, then preserve fixed tree chrome for as long as it physically fits, and only then allow the detail pane to fall below 20 columns. If the terminal cannot fit even those fixed elements, rendering SHALL clip safely at the terminal boundary without negative dimensions or a panic.

Within one run-view entry, sidebar width MAY grow when a newly visible row needs more space but SHALL NOT shrink until the terminal is resized or the user exits and re-enters the run view. A terminal resize SHALL recompute the pane widths from the new available width.

Status glyphs SHALL remain `●` running, `○` pending, `✓` success, `✗` failed, and `⇥` skipped. Every supported row type, including shell, script, UI, headless agent, interactive agent, agent call, sub-workflow, loop, iteration, and group, SHALL have a type glyph. Exact type glyphs, rail glyphs, colors, padding, and whitespace are design decisions.

#### Scenario: Active leaf carries the running indicator
- **WHEN** an expanded active ancestry contains an in-progress leaf
- **THEN** the leaf displays the running indicator and its visible in-progress ancestors preserve the indicator column with blank space

#### Scenario: Long name remains visible when space permits
- **WHEN** a row name exceeds 20 visual characters and the terminal can fit the complete measured tree row plus the minimum detail width
- **THEN** the complete name is shown without truncation

#### Scenario: Sidebar does not shrink during same entry
- **WHEN** a wider row disappears after the sidebar has grown and the terminal has not been resized
- **THEN** the sidebar retains its established width

#### Scenario: Sidebar is not capped at half the terminal
- **WHEN** a complete visible tree row needs more than half the terminal and it still fits beside the whitespace gap and a 20-column detail pane
- **THEN** the sidebar may grow beyond half the terminal so the complete row remains visible

#### Scenario: Extremely narrow terminal degrades deterministically
- **WHEN** the terminal cannot fit minimum tree chrome, the whitespace gap, and a 20-column detail pane
- **THEN** the name is already reduced to an ellipsis, fixed tree chrome is preserved while it fits, the detail width takes the unavoidable remaining reduction, and layout remains valid

### Requirement: Drill-in navigation with breadcrumbs

The run view SHALL retain manual drill-in as an optional scope over the selectable workflow tree. Enter on a drillable container row SHALL scope the tree, breadcrumb, sub-workflow header, resume fallback, and level-scoped summary to that container. Manual drill-in SHALL NOT be required to select or inspect a nested row exposed by inline expansion. After drill-in, selection SHALL move to the first real direct child in workflow order; omission markers are never eligible.

A manually drilled scope SHALL list all of the scoped container's direct children, subject only to ordinary vertical viewport scrolling. It SHALL NOT apply the five-child inline expansion window used at the parent scope. A breadcrumb SHALL show the run name followed by each manually entered container.

Drillable rows SHALL include sub-workflows, loops, iterations, groups, and agent parents with accepted agent calls. Drill-in SHALL remain available for statically resolvable pending containers.

When a saved run is opened for non-live inspection from the run list or through `--inspect`, the top-level breadcrumb SHALL retain the recorded workflow version next to the version-free canonical runnable name using a `v<major>.<minor>` label. The version label SHALL remain present at every manual drill depth and for every saved-run status. A saved run whose recorded workflow file is unversioned SHALL remain inspectable and SHALL display `unversioned`. The live run view and the pre-run definition preview SHALL NOT show a workflow version.

The existing auto-flatten special case SHALL remain: when an iteration contains exactly one sub-workflow child, drilling into the iteration SHALL project that sub-workflow's children directly, keep the iteration as the deepest breadcrumb, and retain the sub-workflow path and params in the header. Inline expansion of that same ancestry SHALL use the same projected child structure without creating manual drill scope.

#### Scenario: Enter on sub-workflow drills in
- **WHEN** the user presses Enter on a sub-workflow row
- **THEN** the tree shows all direct children of that workflow, the breadcrumb appends the workflow, and the first real direct child is selected rather than rendering a recursive container log

#### Scenario: Enter on agent parent shows every call
- **WHEN** an agent row has accepted calls and the user drills into it
- **THEN** the tree shows every call execution in invocation order

#### Scenario: Auto-flatten remains available
- **WHEN** an iteration has exactly one sub-workflow child and the user presses Enter on the iteration
- **THEN** the view selects the first real child of that sub-workflow, keeps the iteration as the deepest crumb, and shows the sub-workflow path and params in the header

#### Scenario: Selecting nested row does not drill
- **WHEN** the user selects an inline nested row with the arrow keys
- **THEN** the selected detail changes without changing the breadcrumb or manual drill scope

### Requirement: Keyboard focus and scrolling

The workflow tree SHALL always own Up and Down. Those keys SHALL traverse every real visible tree row in display order, including nested rows, and SHALL skip overflow indicators. The selected detail SHALL scroll with `j`, `k`, and the mouse wheel without moving tree selection or changing the manual drill scope. Focus SHALL not need to be switched between panes.

The tree SHALL have an independent vertical viewport. While active-step follow is engaged, it SHALL minimally scroll to keep the active selected row visible. While active-step follow is paused, it SHALL minimally scroll to keep the manually selected row visible even when a different active row exists elsewhere in the projected tree.

Selecting a different row manually SHALL show the new detail from its top. `i` SHALL control current-input expansion as defined by the input-preview requirement. `PgUp` and `PgDown` SHALL remain unbound. The legend/help SHALL advertise the actions applicable to the current state, including selectable nested-row navigation, Enter drill-in, `i` input expansion, `g` full-output loading, `t` selected-response tail-follow, `l` jump-to-live, agent-call selection, and group/container rows. It SHALL NOT advertise `d` as a drill action.

#### Scenario: Up and Down traverse nested rows
- **WHEN** inline expansion exposes selectable descendants
- **THEN** Up and Down traverse parent and descendant rows in their visible order

#### Scenario: Overflow indicators are skipped
- **WHEN** `… N earlier` or `… N later` appears in an inline expansion
- **THEN** arrow navigation never places selection on that indicator

#### Scenario: Paused viewport keeps manual selection visible
- **WHEN** active-step follow is paused and the user selects a row outside the current tree viewport
- **THEN** the tree scrolls only enough to reveal the selected row without jumping to the active row

#### Scenario: Help reflects new navigation
- **WHEN** the detailed run view renders its legend
- **THEN** it describes the applicable Enter, input, output, tail, live-follow, agent-call, and group actions without a `d` drill shortcut

### Requirement: Step list inline expansion of direct children under selected step

The tree SHALL expand containers on the selected ancestry and, during live execution, on the active ancestry whenever that ancestry lies inside the current manual scope. At the parent scope, each expanded container SHALL show a window containing no more than five real direct children.

When an active or selected descendant determines a focal direct child, the window SHALL contain up to two real children before the focal child, the focal child, and up to two real children after it. Near the beginning or end, unused capacity SHALL be filled from the other side so that up to five real children remain visible. If no descendant supplies a focal child, a completed container SHALL show its final five children and an entirely pending container SHALL show its first five children.

When the selected and active ancestries pass through different direct children of the same container, both focal children SHALL appear within the same five-real-child window. The remaining capacity SHALL be filled deterministically with siblings nearest to either focal child, with equal-distance ties resolved in workflow order, and the resulting real children SHALL render in workflow order.

If children are omitted before, between, or after the visible real children, the tree SHALL render unselectable `… N earlier`, `… N between`, and `… N later` indicators with the exact omitted counts for each contiguous omitted region. An indicator SHALL NOT replace a real child that fits in the five-row window and SHALL NOT count toward the five-real-child limit. Containers with five or fewer direct children SHALL show every child and no overflow indicator.

Every real expansion row SHALL be selectable. Selecting a nested real row SHALL change selected detail without changing manual drill scope. When selection leaves a non-active expanded branch, that branch SHALL collapse; an active ancestry inside the current manual scope SHALL remain expanded while the user explores another row in that scope.

#### Scenario: Distant active and selected children both remain visible
- **WHEN** the active and selected ancestries pass through different direct children whose combined centered windows would exceed five real children
- **THEN** the window contains both focal children, fills its remaining real-child capacity with the nearest siblings, and does not exceed five real children

#### Scenario: Dual-focus omission reports the middle gap
- **WHEN** one or more direct children are omitted between the visible selected-focus and active-focus regions
- **THEN** an unselectable `… N between` indicator reports the exact number of children in that contiguous omitted region

#### Scenario: Overflow indicators are not selectable
- **WHEN** an earlier, between, or later overflow indicator is visible
- **THEN** it cannot receive tree selection or selected detail

#### Scenario: Active ancestry remains expanded during exploration
- **WHEN** auto-follow is paused and the user selects another row in a scope that still contains the active leaf
- **THEN** the active ancestry remains visibly expanded

#### Scenario: Drilled scope is not windowed
- **WHEN** the user manually drills into a container with more than five direct children
- **THEN** all direct children are available through the tree viewport

### Requirement: Agent-call hierarchy rendering

A parent agent row with accepted agent calls SHALL retain its `(N calls)` count and participate in the same selectable inline-tree projection as other containers. When its selected or in-scope active ancestry is expanded, each call SHALL appear as a dynamic child execution row in invocation order. Enter on the parent SHALL manually scope the tree to all call executions and select the first real call.

A named-session target SHALL be labeled `call session: <name>` and a profile target SHALL be labeled `call agent: <profile>`. Each call SHALL retain its independent status and agent-call type glyph. Accepted calls that fail to launch, repeated calls to the same target, and calls reconstructed from persisted evidence SHALL remain distinct visible rows.

#### Scenario: Expanded parent shows chronological calls
- **WHEN** a parent with multiple calls is expanded inline or manually drilled
- **THEN** the run view shows one selectable child execution row per call in invocation order

#### Scenario: CLI launch failure remains visible
- **WHEN** an accepted call fails while launching its child CLI
- **THEN** the run view displays that call as a failed child row beneath its parent

#### Scenario: Repeated target calls remain distinct
- **WHEN** a parent calls the same target multiple times
- **THEN** each invocation appears as a separate child row

## Test Plan

No named `INT-*` or `E2E-*` obligation is assigned to this foundation. Implement the spec and design with table-driven unit/model tests for:

- root and drilled scopes;
- single-focus, dual-focus, overlapping-focus, edge-fill, omission-count, collapse, and active-ancestry projections;
- dynamic calls, groups, loops, iterations, pending/lazy sub-workflows, auto-flatten, and deleted-workflow reconstruction;
- node-key selection stability when rows are inserted or the tree is rebuilt;
- independent tree viewport behavior;
- complete-row measurement, name-only truncation, grow-only width, resize reset, no proportional cap, and widths below the normal minimum; and
- saved version/unversioned breadcrumbs, scoped summaries, row glyphs, and state-aware help.

## Done When

- Direct-child cursor ownership is replaced by stable node selection across the tree, model, view, drill, and summary consumers.
- The pure projector and its omission rows satisfy every scenario in the assigned `view-run` requirements, including scenarios not repeated above.
- Every real nested row, including agent calls, can be selected by Up/Down without changing manual drill scope.
- Enter drilling, Escape drill-out, pending container resolution, and auto-flatten preserve their approved semantics.
- Tree and detail panes render safely at representative wide, narrow, and extremely narrow widths; the sidebar grows but does not shrink until resize.
- No full-height pane separator remains, and the legend reflects the new tree/navigation model.
- Focused tests in `internal/runview` pass and protect the new projection and layout contracts.
