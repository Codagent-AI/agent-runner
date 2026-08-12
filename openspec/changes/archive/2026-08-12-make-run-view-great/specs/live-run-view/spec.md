## MODIFIED Requirements

### Requirement: TUI stays open after workflow completion

When the workflow reaches a terminal state, the run-view TUI SHALL remain active until explicit user exit.

On successful completion, the TUI SHALL display the run summary screen per `run-complete-screen`; `s` SHALL toggle to the detailed run view. On failure, the TUI SHALL remain in detail, expand the failed leaf's ancestry in the root tree, and select the failed leaf without creating a manual drill scope. If multiple failed leaves share the greatest depth, the TUI SHALL select the leaf whose failure was recorded most recently, falling back to workflow order when durable failure ordering is unavailable. The summary SHALL remain available through `s`.

Once execution is terminal, detailed behavior SHALL match an inactive historical run opened through `--inspect`, including manual tree navigation, optional drill scope, selected-detail scrolling, resume, and the legend.

#### Scenario: Successful completion shows summary screen
- **WHEN** the final workflow execution completes successfully
- **THEN** the TUI remains open on the summary screen with completed status

#### Scenario: Failure keeps TUI open in detailed view
- **WHEN** an execution fails and the workflow halts
- **THEN** the TUI remains in detail, expands the failed ancestry, selects the failed leaf, and retains root manual scope

#### Scenario: Equally deep failures select the latest failure
- **WHEN** terminal failure contains multiple failed leaves at the greatest depth
- **THEN** the TUI selects the most recently recorded failure, or the first in workflow order when durable failure ordering is unavailable

#### Scenario: Post-completion navigation matches inspect mode
- **WHEN** the workflow is terminal and the user opens or remains in detail
- **THEN** navigation and selected-detail behavior match an inactive `view-run`

#### Scenario: Resume action available after completion
- **WHEN** a terminal run has a selected resumable agent execution and the user invokes resume
- **THEN** the existing `view-run` resume behavior applies

### Requirement: Real-time step output

Autonomous shell, script, headless-agent, and agent-call stdout and stderr SHALL continue to stream and persist as produced. When the producing execution is selected, bytes SHALL appear in that row's `Current output` or `Current response` rail without waiting for completion, with the existing first-byte visibility target of 100 ms. ANSI escape sequences, including sequences split across chunks, SHALL be removed from display without altering the raw persisted bytes.

Raw output SHALL continue to be written to `<sessionDir>/output/<step-prefix>.out` and `.err` for post-run inspection, independently of audit-log truncation, where the audit prefix is escaped by replacing `/` with `__` and `:` with `_`. Agent-call output SHALL remain attributed to the call rather than its parent and SHALL use its existing persisted call-output path. Interactive agent and interactive shell executions SHALL remain excluded because Agent Runner does not read or persist their terminal traffic.

If the user manually selects another row, incoming output SHALL continue accumulating and persisting without changing selection. Selecting the producing row again SHALL show all output accumulated so far, subject to the existing large-output loading threshold.

#### Scenario: Long-running shell step output streams
- **WHEN** a selected autonomous shell execution produces stdout or stderr
- **THEN** its `Current output` rail updates without waiting for completion

#### Scenario: Selected script output streams
- **WHEN** a selected script execution produces output
- **THEN** its `Current output` rail updates without waiting for completion

#### Scenario: Headless agent output streams
- **WHEN** a selected headless agent produces filtered response output
- **THEN** its `Current response` rail updates without waiting for completion

#### Scenario: Selected call response streams separately
- **WHEN** a selected active agent call produces output
- **THEN** its `Current response` updates and the parent agent's response remains unchanged

#### Scenario: Manual selection away is not stolen
- **WHEN** the user selects another row while an execution continues producing output
- **THEN** output accumulates for the producing execution without changing tree selection

#### Scenario: Returning shows accumulated output
- **WHEN** the user reselects an execution after output accumulated while it was not selected
- **THEN** its current detail includes all accumulated output subject to lazy loading

#### Scenario: ANSI sequences are stripped in the detail pane
- **WHEN** streamed output contains ANSI color or cursor sequences
- **THEN** the selected detail renders sanitized text without corrupting the TUI

#### Scenario: Split ANSI sequence is sanitized
- **WHEN** one ANSI sequence arrives across multiple output chunks
- **THEN** the selected detail removes the complete sequence without exposing partial escape bytes

#### Scenario: First byte remains responsive
- **WHEN** a selected autonomous execution produces its first output byte
- **THEN** the run view targets displaying it within 100 ms

#### Scenario: Output persists past step completion
- **WHEN** an autonomous shell, script, headless agent, or call completes
- **THEN** its full raw stdout and stderr remain available through the established escaped-prefix output path

#### Scenario: Post-completion detail pane reads from output files
- **WHEN** a terminal run's recorded execution is selected
- **THEN** the pane reads its output files and applies the existing large-output threshold

#### Scenario: Interactive agent step has no output files
- **WHEN** an interactive agent or interactive shell exits
- **THEN** Agent Runner creates no transcript output files and selected detail fabricates no response

### Requirement: Cursor auto-follows the active step

While the workflow is running, active-step auto-follow SHALL begin engaged. It SHALL expand the active ancestry in the root workflow tree, select the active leaf itself, and keep that row visible. Active leaves include ordinary steps, iterations when they are the execution frontier, UI steps, and agent calls.

Auto-follow SHALL NEVER drill into or out of a sub-workflow, loop, iteration, group, or agent parent. Entering and leaving nested execution SHALL change inline expansion and selection without changing the manual breadcrumb scope.

Up/Down tree navigation, manual drill-in or drill-out, and scrolling upward within the selected response SHALL pause active-step auto-follow. While paused, execution progress SHALL NOT change selection. When the active ancestry remains inside the current manual scope, it SHALL remain expanded while the user inspects another row.

Pressing `l` SHALL return to root manual scope when needed, expand the current active ancestry, select the active leaf, scroll its response to the tail, and re-engage both active-step follow and response tail-follow.

#### Scenario: Active step advances to peer
- **WHEN** auto-follow is engaged and execution moves to a peer
- **THEN** selection moves to that peer and its selected detail is shown

#### Scenario: Active step enters a sub-workflow
- **WHEN** auto-follow is engaged and execution enters a nested sub-workflow
- **THEN** the sub-workflow ancestry expands inline, the active leaf is selected, and the breadcrumb scope does not change

#### Scenario: Active step enters a loop iteration
- **WHEN** auto-follow is engaged and execution enters a loop iteration
- **THEN** the loop and iteration ancestry expand inline, the active leaf is selected, and the breadcrumb scope does not change

#### Scenario: Active call becomes selected leaf
- **WHEN** an accepted agent call becomes the active execution frontier
- **THEN** its parent expands inline and the call row becomes selected without drill-in

#### Scenario: Active step leaves a sub-workflow
- **WHEN** execution leaves nested work and advances elsewhere
- **THEN** inline active expansion updates without changing manual drill scope

#### Scenario: Manual navigation pauses auto-follow
- **WHEN** the user moves tree selection with Up or Down
- **THEN** auto-follow pauses and execution progress does not steal selection

#### Scenario: Manual drill pauses auto-follow
- **WHEN** the user drills in or out manually
- **THEN** auto-follow pauses and the chosen scope remains under user control

#### Scenario: Response scroll-up pauses auto-follow
- **WHEN** the user presses `k` or scrolls upward with the mouse within the selected response
- **THEN** active-step follow and response tail-follow both pause

#### Scenario: Paused active ancestry remains visible in scope
- **WHEN** auto-follow is paused, the user selects another row, and the active leaf remains inside the current scope
- **THEN** the active ancestry remains expanded while selection stays where the user placed it

#### Scenario: Jump-to-live re-engages auto-follow
- **WHEN** the user presses `l` while follow is paused
- **THEN** the view returns to root scope if needed, expands and selects the active leaf, moves its response to the tail, and resumes both follow modes without drilling

#### Scenario: Failure jumps cursor to the failed step
- **WHEN** the workflow reaches failed terminal state
- **THEN** the root tree expands and selects the failed leaf without creating a drill scope, regardless of the prior follow state

### Requirement: Detail-pane tail-follow

While response/output is streaming into the selected execution and response tail-follow is engaged, the detail viewport SHALL remain pinned to its tail. Active-step follow does not need to be engaged as long as that producing execution remains selected.

Pressing `k` or scrolling upward with the mouse SHALL expose earlier visual rows within that selected execution and pause both response tail-follow and active-step auto-follow. Later bytes and later workflow steps SHALL NOT change the selected row or its chosen scroll position while follow is paused.

Pressing `t` SHALL move to the tail of the same selected execution and re-engage response tail-follow only. It SHALL NOT change tree selection or re-engage active-step auto-follow. Pressing `l` SHALL perform the jump-to-live behavior and re-engage both. `End` and uppercase `G` SHALL remain unbound.

#### Scenario: Streaming output auto-tails
- **WHEN** new bytes arrive for the selected producing execution while response tail-follow is engaged
- **THEN** the newest response/output remains visible

#### Scenario: User scroll pauses tail-follow
- **WHEN** the user scrolls to earlier rows of the current response
- **THEN** the viewport and selected row remain where the user placed them as output and workflow execution continue

#### Scenario: Output arrives while paused
- **WHEN** new bytes arrive after response tail-follow was paused
- **THEN** the user's selected row and scroll position do not change

#### Scenario: t re-engages tail-follow
- **WHEN** the user presses `t` while follow is paused
- **THEN** the viewport moves to the selected execution's tail, re-engages continuous tail-follow for that selected execution, and does not select the workflow's active leaf

#### Scenario: l follows current active execution
- **WHEN** the user presses `l` while either follow mode is paused
- **THEN** the current active leaf is selected at its response tail and both follow modes resume

#### Scenario: End and G are not bound
- **WHEN** the user presses `End` or uppercase `G`
- **THEN** neither key changes selection or detail scrolling

### Requirement: UI steps render inside the live-run-view chrome

When the active execution is `mode: ui`, the live run view SHALL render its title, body, inputs, and actions inside the ordinary selected-detail chrome. The root tree SHALL expose the UI step through inline active-ancestry expansion without automatic drill-in, and the active UI row SHALL be selected while auto-follow is engaged.

The workflow breadcrumb and tree SHALL remain visible. UI content SHALL remain bounded by the detail width and height. The selected UI detail SHALL group form content under `Current form` and its eventual recorded result under `Current outcome`.

When auto-follow is paused because the user navigated or manually drilled elsewhere, a newly active UI step SHALL NOT force a scope or selection change. The explicit `l` action SHALL return to root scope, expand and select the active UI leaf, and re-engage follow without resolving the UI step.

Existing UI input routing and quit behavior SHALL remain: Left/Right, Tab, Shift-Tab, Enter, and other keys operate the selected form only when applicable to its focused control; Up/Down remain tree navigation; Ctrl+C exits immediately; and keys consumed by the form SHALL NOT trigger chrome actions. Enter SHALL drill only when a non-UI drillable container is selected. The live run view SHALL NOT retain a separate `d` drill shortcut.

#### Scenario: Workflow name and sidebar visible during a UI step
- **WHEN** a UI step is selected and active
- **THEN** the workflow breadcrumb, inline active tree, and UI form are visible together

#### Scenario: UI step body wraps within the content area
- **WHEN** UI content exceeds the available detail width
- **THEN** it wraps within that pane rather than extending beyond the chrome

#### Scenario: Active UI ancestry expands without drilling
- **WHEN** auto-follow is engaged and execution enters a nested UI step
- **THEN** the root tree expands and selects the UI leaf without changing breadcrumb scope

#### Scenario: Sidebar reflects the active UI step
- **WHEN** the active execution is a UI step and auto-follow is engaged
- **THEN** the tree highlights that UI leaf as in-progress and retains final statuses on prior steps

#### Scenario: UI step in sibling sub-workflow leaves stale drill-in
- **WHEN** the user manually navigated or drilled away before a UI step becomes active
- **THEN** the UI step remains pending without forcing a scope or selection change

#### Scenario: Jump-to-live returns to UI without drilling
- **WHEN** a UI step is active and the user presses `l`
- **THEN** the view returns to root scope, expands and selects the UI leaf, and leaves the form unresolved

#### Scenario: UI step input does not trigger chrome quit
- **WHEN** a selected UI control consumes Left, Right, Tab, Shift-Tab, or Enter
- **THEN** the form handles the key without triggering navigation or quit

#### Scenario: Ctrl+C during focused UI step exits immediately
- **WHEN** the user presses Ctrl+C while a UI step is selected
- **THEN** the application exits immediately and the UI step remains unresolved

### Requirement: Metrics update during an active run

While the workflow runs, completed executions SHALL expose collected usage and cost immediately through their selected detail. Run-so-far metrics SHALL remain available through the summary. A re-executed step SHALL show its latest attempt in selected detail while run totals include every attempt.

#### Scenario: Completed step shows metrics mid-run
- **WHEN** an agent step or call completes while later work remains active
- **THEN** selecting it shows its collected usage and cost without waiting for run completion

#### Scenario: Re-executed step shows latest attempt mid-run
- **WHEN** a logical step completes another attempt during the run
- **THEN** selected detail shows the latest attempt while run-so-far totals include all attempts

### Requirement: Live agent-call visibility

Accepted autonomous-headless agent calls SHALL appear as independently selectable child executions beneath their parent. Each call SHALL update status and output independently and SHALL remain visible after success, failure, or child CLI launch failure.

An active call SHALL participate in active-leaf expansion and selection. Its response SHALL stream separately into its own `Current response` rail. When an interactive parent owns the terminal, Agent Runner SHALL NOT interrupt terminal ownership; calls recorded during that interval SHALL appear when the TUI returns.

#### Scenario: Autonomous call appears live
- **WHEN** an autonomous-headless parent starts an accepted call
- **THEN** the tree inserts an in-progress call row beneath that parent

#### Scenario: Called-child output streams separately
- **WHEN** the active call produces output
- **THEN** its selected response updates without attributing bytes to the parent

#### Scenario: CLI launch failure updates live row
- **WHEN** the child CLI fails to launch
- **THEN** the call row transitions to failed and remains selectable

#### Scenario: Auto-follow enters and leaves call
- **WHEN** active-step follow is engaged as an agent call starts and later finishes
- **THEN** selection moves to the active call without drilling and then advances to the next active execution point

#### Scenario: Manual navigation pauses call auto-follow
- **WHEN** the user navigates manually before or during an active call
- **THEN** selection remains where the user placed it while the call stays visible in the in-scope active ancestry

#### Scenario: Active call carries running indicator
- **WHEN** a parent and its active call both have in-progress status
- **THEN** the call displays the blinking running indicator and the parent retains a static running indicator

#### Scenario: Call completion is independent of parent
- **WHEN** a called child succeeds or fails while its parent remains active
- **THEN** the call row displays its terminal status without assigning that status to the parent

#### Scenario: Interactive parent retains terminal ownership
- **WHEN** an interactive parent records calls while it owns the terminal
- **THEN** the TUI does not interrupt it

#### Scenario: Interactive-parent calls appear after return
- **WHEN** an interactive parent returns terminal ownership after recording calls
- **THEN** the resumed run view reconstructs those calls beneath the parent from persisted evidence
