## MODIFIED Requirements

### Requirement: Step list rendering

The run view SHALL render the current manual drill scope as a selectable workflow tree on the left and the selected row's detail on the right. The panes SHALL be separated by whitespace; the run view SHALL NOT render a full-height vertical separator between them.

Each real tree row SHALL display, in order: indentation for its tree depth, a status indicator, the step name, and the type glyph. Loop rows SHALL additionally display an iteration counter in the form `(N/M)`. Iteration rows SHALL NOT display binding values, per-iteration parameters, or arguments.

Step statuses SHALL remain `pending`, `in-progress`, `success`, `failed`, and `skipped`. The deepest visible active row's `in-progress` indicator SHALL blink only while the run is active. Visible in-progress ancestor containers SHALL retain a static in-progress indicator while the active leaf blinks. An interrupted step in an inactive run SHALL render a static in-progress indicator. Loop exhaustion SHALL render as success.

The sidebar SHALL measure complete visible rows, including indentation, before truncating names. A name SHALL remain untruncated whenever the preferred sidebar width and a usable detail pane fit within the terminal. When they do not fit, the sidebar SHALL truncate names with an ellipsis while preserving indentation, status, suffixes, and type glyphs. The detail pane SHALL retain at least 20 visual columns whenever the terminal can accommodate the minimum tree chrome, whitespace gap, and that width. The sidebar SHALL NOT have a proportional-width ceiling.

When the terminal is too narrow to fit minimum tree chrome, the whitespace gap, and 20 detail columns, layout SHALL first reduce the name to an ellipsis, then preserve fixed tree chrome for as long as it physically fits, and only then allow the detail pane to fall below 20 columns. If the terminal cannot fit even those fixed elements, rendering SHALL clip safely at the terminal boundary without negative dimensions or a panic.

Within one run-view entry, sidebar width MAY grow when a newly visible row needs more space but SHALL NOT shrink until the terminal is resized or the user exits and re-enters the run view. A terminal resize SHALL recompute the pane widths from the new available width.

Status glyphs SHALL remain `●` running, `○` pending, `✓` success, `✗` failed, and `⇥` skipped. Every supported row type, including shell, script, UI, headless agent, interactive agent, agent call, sub-workflow, loop, iteration, and group, SHALL have a type glyph. Exact type glyphs, rail glyphs, colors, padding, and whitespace are design decisions.

#### Scenario: Shell step row
- **WHEN** a shell step is rendered in the workflow tree
- **THEN** its row shows indentation, a status indicator, the step name, and the shell type glyph

#### Scenario: Loop row shows iteration counter
- **WHEN** a counted or for-each loop has completed 3 of 5 iterations
- **THEN** its row shows the loop type glyph and `(3/5)` after the name

#### Scenario: Active leaf carries the running indicator
- **WHEN** an expanded active ancestry contains an in-progress leaf
- **THEN** the leaf displays the blinking running indicator and every visible in-progress ancestor displays a static running indicator

#### Scenario: Stopped active ancestry retains status indicators
- **WHEN** a run stops while an expanded container and its visible descendant retain `in-progress` status
- **THEN** both rows display static running indicators

#### Scenario: Aborted step does not blink
- **WHEN** a step retained `in-progress` status from an interrupted run and the run is no longer active
- **THEN** its indicator is static

#### Scenario: Pending steps render before execution
- **WHEN** the workflow definition is known but the run has no audit entries
- **THEN** the tree shows every defined top-level row with pending status

#### Scenario: Executed steps recover without workflow file
- **WHEN** a saved run's workflow file is unavailable but audit history identifies executed top-level steps
- **THEN** the tree reconstructs those executed rows and does not report a missing-workflow error

#### Scenario: Missing workflow and history reports an error
- **WHEN** neither the workflow definition nor recoverable audit history is available
- **THEN** the run view reports the missing-workflow error

#### Scenario: Long name remains visible when space permits
- **WHEN** a row name exceeds 20 visual characters and the terminal can fit the complete measured tree row plus the minimum detail width
- **THEN** the complete name is shown without truncation

#### Scenario: Constrained name truncates at pane cap
- **WHEN** the preferred sidebar and minimum detail pane do not fit together
- **THEN** the sidebar truncates the name with an ellipsis while retaining the row's indentation, status, and type glyph

#### Scenario: Panes have no full-height separator
- **WHEN** the two-pane run view is rendered
- **THEN** whitespace separates the tree and detail pane and no full-height vertical rule appears between them

#### Scenario: Sidebar grows for wider visible row
- **WHEN** a newly visible row needs more width and the terminal can provide it without violating the detail minimum
- **THEN** the sidebar grows to fit the row

#### Scenario: Sidebar does not shrink during same entry
- **WHEN** a wider row disappears after the sidebar has grown and the terminal has not been resized
- **THEN** the sidebar retains its established width

#### Scenario: Resize recomputes pane widths
- **WHEN** the terminal width changes
- **THEN** the tree and detail widths are recomputed for the new terminal width

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

#### Scenario: Top-level breadcrumb rendering
- **WHEN** the run view has no manual drill scope
- **THEN** the breadcrumb shows the workflow's canonical runnable name, start time, and run status

#### Scenario: Non-live saved run shows recorded version
- **WHEN** a saved run recorded `deploy-v2.0.yaml` and is opened from the run list or through `--inspect`
- **THEN** the top-level breadcrumb shows canonical name `deploy` with version label `v2.0`

#### Scenario: Saved-run status does not suppress version
- **WHEN** a saved run is opened for non-live inspection with status inactive, failed, or completed
- **THEN** the breadcrumb shows its recorded version alongside the status

#### Scenario: Version remains visible after drill-in
- **WHEN** the user drills into a sub-workflow, loop, iteration, group, or agent parent while inspecting a saved versioned run
- **THEN** the breadcrumb retains the top-level workflow's recorded version while appending the entered container

#### Scenario: Live run omits version
- **WHEN** a workflow is executing in the live run view
- **THEN** the breadcrumb shows the version-free canonical workflow name without a version label

#### Scenario: Definition preview omits version
- **WHEN** the user opens a workflow's pre-run definition preview
- **THEN** the breadcrumb shows the version-free canonical workflow name without a version label

#### Scenario: Legacy saved run displays unversioned
- **WHEN** a saved run recorded an unversioned workflow file and is opened for non-live inspection
- **THEN** the breadcrumb displays `unversioned` and inspection continues without a filename-version error

#### Scenario: Enter on sub-workflow drills in
- **WHEN** the user presses Enter on a sub-workflow row
- **THEN** the tree shows all direct children of that workflow, the breadcrumb appends the workflow, and the first real direct child is selected rather than rendering a recursive container log

#### Scenario: Enter on loop shows every iteration
- **WHEN** the user presses Enter on a loop row
- **THEN** the tree shows all known iteration rows without applying the five-child inline window

#### Scenario: Enter on iteration shows every child
- **WHEN** the user presses Enter on an iteration row
- **THEN** the tree shows every direct child step and the breadcrumb appends the iteration identifier

#### Scenario: Enter on agent parent shows every call
- **WHEN** an agent row has accepted calls and the user drills into it
- **THEN** the tree shows every call execution in invocation order

#### Scenario: Auto-flatten remains available
- **WHEN** an iteration has exactly one sub-workflow child and the user presses Enter on the iteration
- **THEN** the view selects the first real child of that sub-workflow, keeps the iteration as the deepest crumb, and shows the sub-workflow path and params in the header

#### Scenario: Drill into pending sub-workflow
- **WHEN** the user drills into a statically resolvable pending sub-workflow
- **THEN** all direct children appear with pending status and selected pending detail contains only statically knowable data

#### Scenario: Selecting nested row does not drill
- **WHEN** the user selects an inline nested row with the arrow keys
- **THEN** the selected detail changes without changing the breadcrumb or manual drill scope

#### Scenario: Enter on non-container leaf is a no-op
- **WHEN** the user presses Enter on a shell, script, UI, or agent leaf with no applicable resume action
- **THEN** the manual drill scope does not change

### Requirement: Detail pane per step type

The right pane SHALL render detail for exactly one selected real tree row. Changing selection SHALL replace the pane's content; the pane SHALL NOT stack blocks for other steps or recursively embed descendant detail.

The selected detail SHALL begin with compact primary metadata. Identity, type, status, outcome, and duration SHALL appear when available. Agent steps SHALL show profile, CLI, and model; agent calls SHALL additionally show their target. Completed agent and call executions SHALL show concise usage and reported cost when available. Shell and script steps SHALL show exit status. `capture` SHALL appear when configured, and a `skip_if` or `break_if` expression SHALL appear when it explains the recorded outcome.

Session IDs, workdirs, request IDs, session strategy, and inactive modifiers SHALL be omitted from the primary presentation unless a value is needed for an available resume action or to explain an error. Omitting primary metadata MUST NOT remove it from persisted run evidence.

Current-step content SHALL be grouped into visually distinct, labeled rail sections:

- **Headless agent and agent call**: `Current prompt` and `Current response`. The response SHALL use the resolved adapter's ordinary filtered output.
- **Interactive agent**: `Current prompt`; no response transcript SHALL be fabricated when terminal output was not captured.
- **Shell**: `Current command` and `Current output`, with stdout and stderr distinguishable.
- **Script**: `Current script` and `Current output`, with stdout and stderr distinguishable.
- **UI**: `Current form` and `Current outcome`, integrated with the existing live UI behavior. When durable audit evidence identifies a historical UI execution but its workflow definition is unavailable, `Current form` SHALL explicitly state `definition unavailable` while `Current outcome` continues to show the recorded outcome.
- **Sub-workflow, loop, iteration, and group**: `Current status`, containing identity, workflow params or loop counters, outcome, duration, and aggregate direct-child counts by status. Container detail SHALL NOT list child rows or render descendant detail.

The exact rail glyph, rail color, padding, and label styling are design decisions, but every section SHALL have its semantic label.

The latest attempt of a re-executed logical step SHALL provide its selected-detail outcome, duration, usage, and cost, with the attempt number shown when greater than one. Earlier attempts SHALL remain in run-level aggregates.

Agent metadata SHALL retain the existing model rules: the model line follows the CLI line, uses the profile default when there is no inline override, uses the session-originating step's effective model for resumed or inherited sessions, and displays `(unknown)` when no model can be resolved.

Completed agent and call detail SHALL retain explicit metrics semantics. Collected usage and reported cost appear beside duration. Unavailable usage displays `?` and its reason when known; absent cost displays `?`, never `$0.00`. Legacy audit entries with no structured metric fields omit usage and cost lines instead of inventing unavailable values.

#### Scenario: Selection replaces detail
- **WHEN** the user moves from one real tree row to another
- **THEN** the right pane replaces the prior content with detail for the newly selected row

#### Scenario: Headless agent detail
- **WHEN** a headless agent row is selected
- **THEN** the pane shows profile, CLI, model, status and timing, metrics when available, `Current prompt`, and its full recorded filtered response

#### Scenario: Interactive agent detail has no fabricated transcript
- **WHEN** a completed interactive agent row is selected and no terminal transcript was captured
- **THEN** the pane shows known agent metadata, prompt, outcome, and duration without a `Current response` transcript

#### Scenario: Agent-call detail
- **WHEN** an agent-call row is selected
- **THEN** the pane shows target, profile, CLI, model, status and timing, metrics when available, prompt, error context when present, and its separately recorded filtered response

#### Scenario: Agent-call resume exposes required session
- **WHEN** an inactive selected call has a known resumable CLI session
- **THEN** the resume action is available and the session information needed for that action may appear

#### Scenario: Shell detail
- **WHEN** a shell row is selected
- **THEN** the pane shows `Current command`, exit and duration when available, capture metadata when present, and full recorded stdout and stderr under `Current output`

#### Scenario: Script detail
- **WHEN** a script row is selected
- **THEN** the pane shows `Current script`, exit and duration when available, and full recorded stdout and stderr under `Current output`

#### Scenario: UI detail
- **WHEN** a UI row is selected
- **THEN** the pane groups its form and recorded outcome under `Current form` and `Current outcome`

#### Scenario: Historical UI definition is unavailable
- **WHEN** audit-only recovery identifies a historical UI execution but the workflow definition containing its form is unavailable
- **THEN** `Current form` states `definition unavailable` and `Current outcome` shows the recorded outcome without inventing form content

#### Scenario: Container detail is aggregate only
- **WHEN** a sub-workflow, loop, iteration, or group row is selected
- **THEN** `Current status` shows the container's own metadata and aggregate direct-child status counts without listing child detail rows

#### Scenario: Triggered condition is explanatory metadata
- **WHEN** `skip_if` caused a selected step to skip or `break_if` caused a selected loop to stop
- **THEN** the expression responsible for that recorded outcome appears in the primary detail

#### Scenario: Inactive diagnostic metadata stays hidden
- **WHEN** session strategy, workdir, request ID, or an untriggered modifier is recorded but is not needed for resume or an error
- **THEN** it is absent from the primary detail presentation

#### Scenario: Re-executed step uses latest attempt
- **WHEN** a logical step has multiple attempts
- **THEN** selected detail shows the latest attempt and its attempt number while earlier attempts remain in run aggregates

#### Scenario: Agent block shows collected usage and cost
- **WHEN** a completed selected agent or call has collected usage and reported cost
- **THEN** the primary detail shows both values adjacent to duration

#### Scenario: Agent block shows unavailable usage marker
- **WHEN** a completed selected agent or call has an unavailable usage record
- **THEN** usage and cost show `?`, the usage reason appears when available, and neither zero token counts nor `$0.00` is fabricated

#### Scenario: Legacy agent block omits metrics lines
- **WHEN** selected detail is reconstructed from an audit event with no structured metrics fields
- **THEN** usage and cost lines are omitted

#### Scenario: Agent block header order places model under CLI
- **WHEN** a headless agent, interactive agent, or agent-call detail header is rendered
- **THEN** the model line appears immediately below the CLI line

#### Scenario: Agent block shows model for steps without an inline override
- **WHEN** a selected agent relies on its profile's default model
- **THEN** the model line shows that profile default

#### Scenario: Agent block shows model for a resumed or inherited session
- **WHEN** a selected agent uses `session: resume` or `session: inherit`
- **THEN** the model line shows the effective model used to launch the originating CLI session

#### Scenario: Agent block shows unknown model as explicit fallback
- **WHEN** no model can be resolved for a selected agent or call
- **THEN** the model value displays `(unknown)`

### Requirement: Copy selected step detail

The run view SHALL provide a `c` keybinding that copies the current directory, current breadcrumb, and the selected row's detail to the system clipboard as plain text with terminal styling removed. The copied content SHALL include the complete untruncated current prompt, command, or script even when the on-screen input preview is collapsed.

Copied response/output SHALL reflect the selected output's current large-output loading state. A previous-execution rail that is visible for the selected row SHALL be included. Existing success notice, two-second clearing behavior, and clipboard failure notice SHALL remain.

#### Scenario: Copy includes full collapsed input
- **WHEN** the selected input exceeds three visual rows, its preview is collapsed, and the user presses `c`
- **THEN** the copied text contains the complete input rather than only the three-row preview

#### Scenario: Copy includes selected context
- **WHEN** a previous-execution rail is visible and the user presses `c`
- **THEN** the copied text includes directory, breadcrumb, previous execution, primary metadata, current input, and current response/output

#### Scenario: Copy preserves lazy-output state
- **WHEN** exceptionally large output has not been fully loaded and the user copies the selected detail
- **THEN** the copied response/output matches the currently loaded output state

#### Scenario: Copy success notice clears
- **WHEN** a copy succeeds and its notice is still visible two seconds later
- **THEN** the notice clears

#### Scenario: Copy failure remains non-destructive
- **WHEN** the clipboard write fails
- **THEN** the run view remains open and shows a transient failure notice

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

#### Scenario: j and k scroll only selected detail
- **WHEN** the user presses `j` or `k`
- **THEN** the right pane scrolls and tree selection remains unchanged

#### Scenario: Mouse wheel scrolls only selected detail
- **WHEN** the user uses the mouse wheel over the run view
- **THEN** the right pane scrolls and tree selection remains unchanged

#### Scenario: Manual selection starts at detail top
- **WHEN** the user selects a different row with Up or Down
- **THEN** the new row's detail opens at its top

#### Scenario: Active-follow viewport keeps active row visible
- **WHEN** active-step follow is engaged and execution advances beyond the current tree viewport
- **THEN** the tree scrolls only enough to reveal the newly active selected row

#### Scenario: Paused viewport keeps manual selection visible
- **WHEN** active-step follow is paused and the user selects a row outside the current tree viewport
- **THEN** the tree scrolls only enough to reveal the selected row without jumping to the active row

#### Scenario: Help reflects new navigation
- **WHEN** the detailed run view renders its legend
- **THEN** it describes the applicable Enter, input, output, tail, live-follow, agent-call, and group actions without a `d` drill shortcut

#### Scenario: PgUp and PgDown remain unbound
- **WHEN** the user presses `PgUp` or `PgDown`
- **THEN** neither pane reacts

### Requirement: Live refresh for active runs

While the viewed run is active, the run view SHALL poll run state every two seconds, re-check `run.lock`, and tail only newly appended complete audit lines. Inactive runs SHALL render once and remain static until user input. Active selection and tail-follow behavior are defined by the `live-run-view` capability rather than by cross-step log synchronization.

#### Scenario: Active run refreshes on interval
- **WHEN** the viewed run is active
- **THEN** the run view polls on the list TUI cadence and re-renders changed state

#### Scenario: Inactive run does not poll
- **WHEN** the viewed run is inactive
- **THEN** the run view does not poll for new state

#### Scenario: Missing or empty audit log
- **WHEN** the audit log is missing or empty but the workflow definition is available
- **THEN** the tree shows the defined pending rows without an error

### Requirement: Temporary detail for selected pending step

Selecting a pending row SHALL replace the right pane with visually pending detail containing only statically knowable fields. The pane SHALL show identity, type, and the configured command, script, prompt, form, workflow path, or raw template params as applicable. It SHALL NOT show runtime outcome, duration, metrics, response, output, or a previous execution that cannot be established from recorded execution order.

Pending input SHALL use the same labeled rail and three-row preview/expansion behavior as executed input.

#### Scenario: Pending leaf shows static input
- **WHEN** the user selects a pending shell, script, agent, or UI row
- **THEN** the pane shows its statically configured input and no runtime result fields

#### Scenario: Pending container shows raw params
- **WHEN** the user selects a pending sub-workflow
- **THEN** the pane shows its resolved workflow path and raw template params

#### Scenario: Pending detail is visibly pending
- **WHEN** selected detail belongs to an unexecuted row
- **THEN** its treatment clearly distinguishes configured data from recorded execution data

#### Scenario: Pending input can expand
- **WHEN** a pending configured input exceeds three visual rows
- **THEN** `i` expands and collapses it using the same behavior as executed input

### Requirement: Step list inline expansion of direct children under selected step

The tree SHALL expand containers on the selected ancestry and, during live execution, on the active ancestry whenever that ancestry lies inside the current manual scope. At the parent scope, each expanded container SHALL show a window containing no more than five real direct children.

When an active or selected descendant determines a focal direct child, the window SHALL contain up to two real children before the focal child, the focal child, and up to two real children after it. Near the beginning or end, unused capacity SHALL be filled from the other side so that up to five real children remain visible. If no descendant supplies a focal child, a completed container SHALL show its final five children and an entirely pending container SHALL show its first five children.

When the selected and active ancestries pass through different direct children of the same container, both focal children SHALL appear within the same five-real-child window. The remaining capacity SHALL be filled deterministically with siblings nearest to either focal child, with equal-distance ties resolved in workflow order, and the resulting real children SHALL render in workflow order.

If children are omitted before, between, or after the visible real children, the tree SHALL render unselectable `… N earlier`, `… N between`, and `… N later` indicators with the exact omitted counts for each contiguous omitted region. An indicator SHALL NOT replace a real child that fits in the five-row window and SHALL NOT count toward the five-real-child limit. Containers with five or fewer direct children SHALL show every child and no overflow indicator.

Every real expansion row SHALL be selectable. Selecting a nested real row SHALL change selected detail without changing manual drill scope. When selection leaves a non-active expanded branch, that branch SHALL collapse; an active ancestry inside the current manual scope SHALL remain expanded while the user explores another row in that scope.

#### Scenario: Five or fewer children all appear
- **WHEN** an expanded container has five or fewer direct children
- **THEN** every child appears as a selectable row and no overflow indicator appears

#### Scenario: Active child is centered
- **WHEN** an expanded container has at least two children before and after the direct child containing the active leaf
- **THEN** the window shows those two earlier children, the active-path child, and the two later children

#### Scenario: Distant active and selected children both remain visible
- **WHEN** the active and selected ancestries pass through different direct children whose combined centered windows would exceed five real children
- **THEN** the window contains both focal children, fills its remaining real-child capacity with the nearest siblings, and does not exceed five real children

#### Scenario: Dual-focus omission reports the middle gap
- **WHEN** one or more direct children are omitted between the visible selected-focus and active-focus regions
- **THEN** an unselectable `… N between` indicator reports the exact number of children in that contiguous omitted region

#### Scenario: Edge window fills from available side
- **WHEN** the focal child has only one earlier child and enough later children exist
- **THEN** the window shows that one earlier child, the focal child, and up to three later children

#### Scenario: Overflow counts only omitted children
- **WHEN** three earlier children and four later children lie outside and inside the five-child window
- **THEN** the indicators report exactly the children omitted before and after the real rows

#### Scenario: Overflow indicators are not selectable
- **WHEN** an earlier, between, or later overflow indicator is visible
- **THEN** it cannot receive tree selection or selected detail

#### Scenario: Nested real row is selectable
- **WHEN** the user moves selection onto an inline child
- **THEN** the right pane shows that child's detail without drilling in

#### Scenario: Inactive branch collapses after selection leaves
- **WHEN** selection leaves an expanded branch that does not contain the active leaf
- **THEN** the branch collapses to its container row

#### Scenario: Active ancestry remains expanded during exploration
- **WHEN** auto-follow is paused and the user selects another row in a scope that still contains the active leaf
- **THEN** the active ancestry remains visibly expanded

#### Scenario: Drilled scope is not windowed
- **WHEN** the user manually drills into a container with more than five direct children
- **THEN** all direct children are available through the tree viewport

#### Scenario: Iteration rows hide binding data
- **WHEN** an iteration appears inline or directly inside a drilled loop
- **THEN** its row omits binding values, params, and arguments

### Requirement: In-progress agent progress indicator in the log

A selected in-progress headless agent or agent call SHALL display an animated progress indicator inside its `Current response` rail. Before response text exists, the indicator SHALL occupy the response body. After response text begins streaming, a compact animated indicator SHALL appear below the visible response.

The indicator SHALL disappear when the execution reaches a terminal status or when an interrupted in-progress execution is viewed without an active run. Exact animation frames and colors are design decisions.

#### Scenario: Spinner shown before response
- **WHEN** a selected headless agent or call is running and has no response text
- **THEN** `Current response` contains an animated progress indicator

#### Scenario: Spinner follows streaming response
- **WHEN** a selected headless agent or call is running and response text is visible
- **THEN** an animated indicator appears below the response

#### Scenario: Spinner removed on completion
- **WHEN** the selected execution reaches success, failure, or skipped status
- **THEN** its response rail no longer shows the progress indicator

#### Scenario: Spinner absent for inactive interrupted run
- **WHEN** an interrupted step retains in-progress status but the run is inactive
- **THEN** selected detail shows no animation

### Requirement: Agent-call hierarchy rendering

A parent agent row with accepted agent calls SHALL retain its `(N calls)` count and participate in the same selectable inline-tree projection as other containers. When its selected or in-scope active ancestry is expanded, each call SHALL appear as a dynamic child execution row in invocation order. Enter on the parent SHALL manually scope the tree to all call executions and select the first real call.

A named-session target SHALL be labeled `call session: <name>` and a profile target SHALL be labeled `call agent: <profile>`. Each call SHALL retain its independent status and agent-call type glyph. Accepted calls that fail to launch, repeated calls to the same target, and calls reconstructed from persisted evidence SHALL remain distinct visible rows.

#### Scenario: Parent displays call count
- **WHEN** a parent agent attempt has two accepted calls
- **THEN** its row displays `(2 calls)`

#### Scenario: Expanded parent shows chronological calls
- **WHEN** a parent with multiple calls is expanded inline or manually drilled
- **THEN** the run view shows one selectable child execution row per call in invocation order

#### Scenario: Target form is explicit
- **WHEN** one call targets a named session and another targets an agent profile
- **THEN** their rows use `call session: <name>` and `call agent: <profile>` labels respectively

#### Scenario: Call status is independent
- **WHEN** a parent recovers from a failed call and later succeeds
- **THEN** the failed call remains visible with failed status beneath the successful parent

#### Scenario: CLI launch failure remains visible
- **WHEN** an accepted call fails while launching its child CLI
- **THEN** the run view displays that call as a failed child row beneath its parent

#### Scenario: Repeated target calls remain distinct
- **WHEN** a parent calls the same target multiple times
- **THEN** each invocation appears as a separate child row

#### Scenario: Inspect reconstructs call hierarchy
- **WHEN** a completed run containing agent calls is opened for inspection
- **THEN** the run view reconstructs the parent call count and child rows from persisted run evidence

### Requirement: Agent-call detail and resume

Selecting an agent-call row SHALL show target kind and name; resolved profile, CLI, and model; prompt, outcome, duration, metrics, error context, and retained stdout and stderr through the ordinary selected-step rails. Session metadata and working directory SHALL remain in persisted evidence but SHALL appear in primary detail only when needed for an available resume action or to explain an error.

Called-agent stdout and stderr SHALL use the resolved CLI adapter's ordinary headless output, result, and diagnostic filtering for successful and failed calls. Filtering display output SHALL NOT alter raw persisted evidence, and full output SHALL NOT be reconstructed from `audit.log`.

When the run is inactive, a completed called-agent execution with a known CLI session ID SHALL offer the existing direct session-resume action. The action SHALL be unavailable while the run is active or when no session ID is known.

#### Scenario: Selected call shows execution detail
- **WHEN** the user selects a running or completed agent-call row
- **THEN** the detail pane shows the call's target, compact resolved agent metadata, prompt, status, timing, metrics, and error information available for that execution

#### Scenario: Persisted call output is displayed
- **WHEN** ordinary headless-agent output persistence created stdout or stderr files for the selected call
- **THEN** `Current response` displays the output after applying the resolved CLI adapter's ordinary filtering

#### Scenario: Failed call output uses ordinary filtering
- **WHEN** a failed called agent produced raw protocol output or diagnostics
- **THEN** selected detail shows the same filtered response, error, and relevant diagnostics as an ordinary failed headless agent

#### Scenario: Raw persisted evidence remains unchanged
- **WHEN** the detail pane filters called-agent output for display
- **THEN** the raw persisted stdout and stderr files remain unchanged for evidence and debugging

#### Scenario: Audit metadata is not treated as full output
- **WHEN** no persisted output exists for a selected call
- **THEN** the run view does not reconstruct or display a full child response from `audit.log`

#### Scenario: Inactive call session can be resumed
- **WHEN** the run is inactive and the selected completed call has a known CLI session ID
- **THEN** the existing direct resume action is available and the required session context may be shown

#### Scenario: Resume unavailable during active run
- **WHEN** the run is active or the selected call has no known CLI session ID
- **THEN** the direct resume action is unavailable for that call

## ADDED Requirements

### Requirement: Previous execution context

When a terminal leaf execution precedes the selected execution in audit start order, selected detail SHALL begin with a visually distinct rail labeled `Previous: <step-name>`. Containers SHALL NOT qualify as previous executions; their terminal leaf descendants MAY qualify. Agent calls and skipped leaf executions SHALL qualify.

The previous rail SHALL show known type, status, outcome or exit status, and duration compactly. When captured output exists, the rail SHALL additionally show at most the final two nonblank visual rows after filtering, sanitization, and wrapping to the current detail width. If earlier rendered content was omitted, the excerpt SHALL begin with an ellipsis. A resize SHALL recompute the two visual rows.

For headless agents and agent calls, the excerpt SHALL come from the ordinary filtered response. For successful shell and script executions it SHALL come from stdout. For failed shell and script executions it SHALL prefer stderr and fall back to stdout. Interactive agent and interactive shell executions SHALL show only known metadata and SHALL NOT fabricate a transcript. UI executions SHALL show their recorded outcome without submitted form values. A skipped execution SHALL show `skip_if` and the triggering expression and SHALL NOT show an output excerpt.

The rail SHALL be omitted when no earlier terminal leaf exists. For a re-executed logical step, ordering and recap metadata SHALL use its latest attempt.

#### Scenario: Headless agent recap uses response tail
- **WHEN** a completed headless agent precedes the selected execution and has more than two wrapped response rows
- **THEN** its previous rail shows the final two rows with a leading ellipsis

#### Scenario: Successful script recap uses stdout tail
- **WHEN** a successful script precedes the selected execution
- **THEN** its previous rail shows up to the final two nonblank visual rows of stdout

#### Scenario: Failed script recap prefers stderr
- **WHEN** a failed script has both stdout and stderr
- **THEN** its previous rail shows the final two nonblank visual rows of stderr

#### Scenario: Interactive agent recap has no transcript
- **WHEN** a completed interactive agent precedes the selected execution
- **THEN** its previous rail shows known interactive-agent metadata, outcome, and duration without response text

#### Scenario: Agent call qualifies as previous execution
- **WHEN** a completed agent call is the latest completed leaf before the selected execution
- **THEN** the rail label identifies that call and its excerpt uses the call's separately filtered response

#### Scenario: Skipped execution shows reason without output
- **WHEN** a skipped leaf is the latest terminal execution before the selected execution
- **THEN** its previous rail shows skipped status and the triggering `skip_if` expression without a fabricated output excerpt

#### Scenario: Container resolves to completed leaf
- **WHEN** a completed sub-workflow precedes the selected step and its last completed child leaf is the latest prior execution
- **THEN** the previous rail identifies the child leaf rather than the sub-workflow container

#### Scenario: First execution has no previous rail
- **WHEN** no terminal leaf precedes the selected execution
- **THEN** selected detail omits the previous rail

#### Scenario: Resize preserves two-row bound
- **WHEN** the detail pane width changes
- **THEN** the previous excerpt is rewrapped and still occupies at most two visual rows

### Requirement: Current input preview and expansion

A current prompt, command, or script that wraps beyond three visual rows SHALL initially show only its first three rows in the appropriate labeled rail, with an ellipsis indicating omitted content and an `i expand` hint. Pressing `i` SHALL expand the complete input inline and change the hint to `i collapse`. Pressing `i` again SHALL restore the three-row preview.

When the complete input fits within three visual rows, no expansion hint SHALL appear and `i` SHALL have no effect. A terminal resize SHALL recompute whether the input exceeds three rows and SHALL preserve the expanded/collapsed choice whenever expansion remains applicable.

#### Scenario: Long prompt starts collapsed
- **WHEN** a selected agent prompt wraps to more than three visual rows
- **THEN** `Current prompt` shows the first three rows, an ellipsis, and `i expand`

#### Scenario: i expands complete input
- **WHEN** a long current input is collapsed and the user presses `i`
- **THEN** the complete input appears inline and the hint changes to `i collapse`

#### Scenario: i collapses expanded input
- **WHEN** a long current input is expanded and the user presses `i`
- **THEN** the rail returns to its three-row preview

#### Scenario: Short input has no toggle
- **WHEN** the complete current input fits within three visual rows
- **THEN** no input-toggle hint appears and pressing `i` does nothing

#### Scenario: Resize recomputes visual rows
- **WHEN** a terminal resize changes the wrapped input height
- **THEN** the preview and toggle availability update for the new detail width

### Requirement: Nested-selection resume fallback

For a completed inactive run, direct resume of the selected resumable agent or agent call SHALL take precedence. When the selected row is not directly resumable, the `r` fallback SHALL search the nearest workflow-bearing ancestor of the selected row, including an ancestor reached only through inline expansion, and SHALL resume the last resumable agent execution inside that workflow. If no such ancestor exists, it SHALL use the root workflow. Manual drill scope SHALL NOT override the nearer selected ancestry.

#### Scenario: Inline nested selection scopes resume
- **WHEN** a non-resumable row selected through inline expansion lies inside a sub-workflow with an earlier resumable agent execution
- **THEN** `r` resumes the last resumable execution in that nearest sub-workflow

#### Scenario: Selected direct resume takes precedence
- **WHEN** the selected row is a resumable agent or agent call
- **THEN** `r` resumes that exact selected execution

#### Scenario: Root fallback remains available
- **WHEN** no workflow-bearing ancestor below the root contains a resumable execution
- **THEN** `r` falls back to the last resumable execution in the root workflow

### Requirement: Historical selected-step initialization

An inactive historical detail view SHALL use the same selectable tree and selected-step detail without live auto-follow, jump-to-live, response streaming, tail-follow, or animated progress.

When a failed run opens directly in detailed view, the tree SHALL expand the failed leaf's ancestry and select that leaf. If multiple failed leaves share the greatest depth, the view SHALL select the leaf whose failure was recorded most recently. When durable failure ordering is unavailable, workflow order SHALL be the deterministic fallback. Other historical detail entries SHALL expand the ancestry of the selection established by their existing entry behavior. Completed runs with structured metrics SHALL continue to open on the summary screen before the user switches to detail.

#### Scenario: Failed historical run selects failed leaf
- **WHEN** a failed run is opened for inspection
- **THEN** the detailed view expands the failed ancestry and selects the failed leaf

#### Scenario: Equally deep historical failures select the latest failure
- **WHEN** a failed historical run contains multiple failed leaves at the greatest depth
- **THEN** the view selects the most recently recorded failure, or the first in workflow order when durable failure ordering is unavailable

#### Scenario: Completed metrics run still opens summary
- **WHEN** a completed run with structured metrics is opened
- **THEN** the summary screen appears first and the selected ancestry is expanded when the user switches to detail

#### Scenario: Historical navigation remains manual
- **WHEN** the user navigates an inactive historical detail view
- **THEN** selection and drill scope change only in response to user input

## REMOVED Requirements

### Requirement: Pending steps hidden from log

**Reason**: The continuous cross-step log is removed, so there is no unselected pending block to suppress.

**Migration**: Pending rows remain in the tree and show statically knowable selected detail when chosen.

#### Scenario: Pending top-level step absent from log
- **WHEN** a top-level step has status `pending` and is not currently selected by the cursor
- **THEN** the log contains no block for that step, and the step list shows the step with its pending status indicator

#### Scenario: Pending iteration absent from log
- **WHEN** a loop has started but some iterations are still pending and are not selected by the cursor
- **THEN** the loop's block in the log contains only iteration sub-blocks for started iterations; pending iterations have no sub-block

#### Scenario: Pending child of started sub-workflow absent from log
- **WHEN** a sub-workflow has started and some of its child steps are still pending and not selected
- **THEN** the sub-workflow block contains inline sub-blocks only for started children; pending children have no sub-block

### Requirement: Cross-step auto-scroll while run is active

**Reason**: The detail pane now belongs to one selected execution and no longer receives blocks from other steps.

**Migration**: Active-step selection and selected-response tail-follow are defined by `live-run-view`.

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

**Reason**: Descendant execution is represented by the selectable workflow tree rather than recursively nested detail blocks.

**Migration**: Inline ancestry expansion provides bounded context, and manual drill-in exposes all direct children.

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
