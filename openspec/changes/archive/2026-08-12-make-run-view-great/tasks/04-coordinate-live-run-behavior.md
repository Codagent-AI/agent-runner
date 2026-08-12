# Task: Coordinate live follow, streaming, calls, and completion

## Goal

Adapt the live run view to the selectable tree and selected-execution document. Separate active-step follow from selected-response tail follow, stream and persist output on the correct owning execution, represent calls as independent active leaves, preserve metrics and refresh behavior, and keep the TUI open with correct success/failure terminal selection.

This task covers non-UI live behavior end to end through coordinator messages and run-view model state. Embedded workflow-defined UI key ownership is out of scope here, but the generic active-leaf and selected-detail contracts must support it without special-case drilling or selection.

## Background

You MUST read:

- `openspec/changes/make-run-view-great/proposal.md`, especially “Live Run View”.
- `openspec/changes/make-run-view-great/design.md`, especially section 7 (“Give active follow and tail follow independent state”), the output-persistence portion of section 5, and the live/historical split in section 9.
- `openspec/changes/make-run-view-great/specs/live-run-view/spec.md`, especially `TUI stays open after workflow completion`, `Real-time step output`, `Cursor auto-follows the active step`, `Detail-pane tail-follow`, `Metrics update during an active run`, and `Live agent-call visibility`.
- `openspec/changes/make-run-view-great/specs/view-run/spec.md`, especially `Live refresh for active runs` and the active/inactive progress and selected-output contracts.
- `openspec/changes/make-run-view-great/test-plan.md` for the deterministic local coordinator/subprocess strategy. The named full live integration obligation is completed only when its UI journey is also executable; do not claim it here.

Relevant production paths:

- `internal/liverun/coordinator.go`, `messages.go`, `process_runner.go`, `chunk_writer.go`, and `ansi.go`.
- `internal/runview/model.go`: refresh/pulse commands, `StepStateMsg`, `OutputChunkMsg`, active selection, scrolling, resume/terminal transitions, summary/detail toggle, and call-output refresh.
- `internal/runview/audit.go`, `tree.go`, `output.go`, `detail.go`, `view.go`, `summary.go`, and `failure.go`.
- Tests in `internal/liverun/coordinator_test.go`, `liverun_test.go`, and `internal/runview/model_test.go`, `agent_call_view_test.go`, `start_run_escape_test.go`, `tui_metrics_test.go`, and `failure_test.go`.

Implementation constraints:

- Use TDD around explicit state transitions. Replace the overloaded auto-follow flag with separate `followActive` and `followTail` state.
- At live entry both modes begin engaged. Active progress expands inline ancestry in root/manual scope and selects the active leaf itself; it never appends to or truncates manual drill `path`.
- Active leaves include ordinary steps, execution-frontier iterations, UI steps, and calls.
- Up/Down and explicit drill-in/out pause active follow. Scrolling upward with `k` or mouse pauses both modes. Incoming output and later execution cannot steal paused selection or scroll offset.
- `t` moves to the tail of the same selected execution and enables only tail follow. It must continue following later bytes for that execution. It cannot select the active leaf.
- `l` returns to root manual scope, expands/selects the current active leaf, moves its response to the tail, and re-enables both modes without drilling.
- `End` and uppercase `G` remain unbound. `g` remains the selected full-output action.
- Output belongs to the producing node even when it is not selected. Selection only controls which accumulated bytes render.
- Continue raw persistence for autonomous shell, script, headless agent, and call stdout/stderr. Preserve escaped prefixes (`/` to `__`, `:` to `_`), separately persisted call paths, audit-log independence, and the 100 ms deterministic first-byte target.
- Display sanitization must remove ANSI control sequences, including sequences split across chunks, without changing persisted bytes.
- Interactive agent/shell traffic remains excluded because Agent Runner does not read it. Do not fabricate transcript files or responses.
- Calls remain distinct by invocation identity, including repeated targets and launch failures. Parent and child status, output, metrics, and running indicators cannot bleed into one another.
- When an interactive parent owns the terminal, never interrupt it to show calls; reconstruct recorded calls when terminal ownership returns.
- Poll active external views every two seconds, re-check `run.lock`, and tail only newly appended complete audit lines. Inactive runs do not poll.
- Successful terminal completion stays open on summary; failure stays in detail at root scope with deepest failed leaf selected. Terminal detail then behaves like inactive inspection, including manual navigation and resume.
- Completed executions expose latest-attempt metrics immediately while run-so-far totals include all attempts.
- Keep coordinator behavior deterministic and outside execution semantics. Do not change workflow YAML, audit/state formats, adapter protocols, or interactive terminal ownership.

## Spec

The following source requirements and scenarios are controlling excerpts from `specs/live-run-view/spec.md` and `specs/view-run/spec.md`.

### Requirement: TUI stays open after workflow completion

When the workflow reaches a terminal state, the run-view TUI SHALL remain active until explicit user exit.

On successful completion, the TUI SHALL display the run summary screen per `run-complete-screen`; `s` SHALL toggle to the detailed run view. On failure, the TUI SHALL remain in detail, expand the failed leaf's ancestry in the root tree, and select the failed leaf without creating a manual drill scope. The summary SHALL remain available through `s`.

Once execution is terminal, detailed behavior SHALL match an inactive historical run opened through `--inspect`, including manual tree navigation, optional drill scope, selected-detail scrolling, resume, and the legend.

#### Scenario: Successful completion shows summary
- **WHEN** the final workflow execution completes successfully
- **THEN** the TUI remains open on the summary screen with completed status

#### Scenario: Failure selects failed leaf without drilling
- **WHEN** an execution fails and the workflow halts
- **THEN** the TUI remains in detail, expands the failed ancestry, selects the failed leaf, and retains root manual scope

#### Scenario: Post-completion detail matches inspect
- **WHEN** the workflow is terminal and the user opens or remains in detail
- **THEN** navigation and selected-detail behavior match an inactive `view-run`

### Requirement: Real-time step output

Autonomous shell, script, headless-agent, and agent-call stdout and stderr SHALL continue to stream and persist as produced. When the producing execution is selected, bytes SHALL appear in that row's `Current output` or `Current response` rail without waiting for completion, with the existing first-byte visibility target of 100 ms. ANSI escape sequences, including sequences split across chunks, SHALL be removed from display without altering the raw persisted bytes.

Raw output SHALL continue to be written to `<sessionDir>/output/<step-prefix>.out` and `.err` for post-run inspection, independently of audit-log truncation, where the audit prefix is escaped by replacing `/` with `__` and `:` with `_`. Agent-call output SHALL remain attributed to the call rather than its parent and SHALL use its existing persisted call-output path. Interactive agent and interactive shell executions SHALL remain excluded because Agent Runner does not read or persist their terminal traffic.

If the user manually selects another row, incoming output SHALL continue accumulating and persisting without changing selection. Selecting the producing row again SHALL show all output accumulated so far, subject to the existing large-output loading threshold.

#### Scenario: Selected call response streams separately
- **WHEN** a selected active agent call produces output
- **THEN** its `Current response` updates and the parent agent's response remains unchanged

#### Scenario: Manual selection away is not stolen
- **WHEN** the user selects another row while an execution continues producing output
- **THEN** output accumulates for the producing execution without changing tree selection

#### Scenario: Split ANSI sequence is sanitized
- **WHEN** one ANSI sequence arrives across multiple output chunks
- **THEN** the selected detail removes the complete sequence without exposing partial escape bytes

#### Scenario: First byte remains responsive
- **WHEN** a selected autonomous execution produces its first output byte
- **THEN** the run view targets displaying it within 100 ms

#### Scenario: Output persists past completion
- **WHEN** an autonomous shell, script, headless agent, or call completes
- **THEN** its full raw stdout and stderr remain available through the established escaped-prefix output path

#### Scenario: Interactive execution has no transcript
- **WHEN** an interactive agent or interactive shell exits
- **THEN** Agent Runner creates no transcript output files and selected detail fabricates no response

### Requirement: Cursor auto-follows the active step

While the workflow is running, active-step auto-follow SHALL begin engaged. It SHALL expand the active ancestry in the root workflow tree, select the active leaf itself, and keep that row visible. Active leaves include ordinary steps, iterations when they are the execution frontier, UI steps, and agent calls.

Auto-follow SHALL NEVER drill into or out of a sub-workflow, loop, iteration, group, or agent parent. Entering and leaving nested execution SHALL change inline expansion and selection without changing the manual breadcrumb scope.

Up/Down tree navigation, manual drill-in or drill-out, and scrolling upward within the selected response SHALL pause active-step auto-follow. While paused, execution progress SHALL NOT change selection. When the active ancestry remains inside the current manual scope, it SHALL remain expanded while the user inspects another row.

Pressing `l` SHALL return to root manual scope when needed, expand the current active ancestry, select the active leaf, scroll its response to the tail, and re-engage both active-step follow and response tail-follow.

#### Scenario: Active step enters sub-workflow without drilling
- **WHEN** auto-follow is engaged and execution enters a nested sub-workflow
- **THEN** the sub-workflow ancestry expands inline, the active leaf is selected, and the breadcrumb scope does not change

#### Scenario: Active call becomes selected leaf
- **WHEN** an accepted agent call becomes the active execution frontier
- **THEN** its parent expands inline and the call row becomes selected without drill-in

#### Scenario: Arrow navigation pauses auto-follow
- **WHEN** the user moves tree selection with Up or Down
- **THEN** auto-follow pauses and execution progress does not steal selection

#### Scenario: Response scroll-up pauses auto-follow
- **WHEN** the user presses `k` or scrolls upward with the mouse within the selected response
- **THEN** active-step follow and response tail-follow both pause

#### Scenario: Paused active ancestry remains visible in scope
- **WHEN** auto-follow is paused, the user selects another row, and the active leaf remains inside the current scope
- **THEN** the active ancestry remains expanded while selection stays where the user placed it

#### Scenario: Jump-to-live returns to root active leaf
- **WHEN** the user presses `l` while follow is paused
- **THEN** the view returns to root scope if needed, expands and selects the active leaf, moves its response to the tail, and resumes both follow modes without drilling

### Requirement: Detail-pane tail-follow

While response/output is streaming into the selected execution and response tail-follow is engaged, the detail viewport SHALL remain pinned to its tail. Active-step follow does not need to be engaged as long as that producing execution remains selected.

Pressing `k` or scrolling upward with the mouse SHALL expose earlier visual rows within that selected execution and pause both response tail-follow and active-step auto-follow. Later bytes and later workflow steps SHALL NOT change the selected row or its chosen scroll position while follow is paused.

Pressing `t` SHALL move to the tail of the same selected execution and re-engage response tail-follow only. It SHALL NOT change tree selection or re-engage active-step auto-follow. Pressing `l` SHALL perform the jump-to-live behavior and re-engage both. `End` and uppercase `G` SHALL remain unbound.

#### Scenario: Streaming response stays at tail
- **WHEN** new bytes arrive for the selected producing execution while response tail-follow is engaged
- **THEN** the newest response/output remains visible

#### Scenario: Output arrives while paused
- **WHEN** new bytes arrive after response tail-follow was paused
- **THEN** the user's selected row and scroll position do not change

#### Scenario: t follows selected execution only
- **WHEN** the user presses `t` while follow is paused
- **THEN** the viewport moves to the selected execution's tail, re-engages continuous tail-follow for that selected execution, and does not select the workflow's active leaf

#### Scenario: l follows current active execution
- **WHEN** the user presses `l` while either follow mode is paused
- **THEN** the current active leaf is selected at its response tail and both follow modes resume

### Requirement: Metrics update during an active run

While the workflow runs, completed executions SHALL expose collected usage and cost immediately through their selected detail. Run-so-far metrics SHALL remain available through the summary. A re-executed step SHALL show its latest attempt in selected detail while run totals include every attempt.

#### Scenario: Completed execution exposes metrics mid-run
- **WHEN** an agent step or call completes while later work remains active
- **THEN** selecting it shows its collected usage and cost without waiting for run completion

#### Scenario: Re-executed step uses latest metrics
- **WHEN** a logical step completes another attempt during the run
- **THEN** selected detail shows the latest attempt while run-so-far totals include all attempts

### Requirement: Live agent-call visibility

Accepted autonomous-headless agent calls SHALL appear as independently selectable child executions beneath their parent. Each call SHALL update status and output independently and SHALL remain visible after success, failure, or child CLI launch failure.

An active call SHALL participate in active-leaf expansion and selection. Its response SHALL stream separately into its own `Current response` rail. When an interactive parent owns the terminal, Agent Runner SHALL NOT interrupt terminal ownership; calls recorded during that interval SHALL appear when the TUI returns.

#### Scenario: CLI launch failure updates live row
- **WHEN** the child CLI fails to launch
- **THEN** the call row transitions to failed and remains selectable

#### Scenario: Auto-follow enters and leaves call
- **WHEN** active-step follow is engaged as an agent call starts and later finishes
- **THEN** selection moves to the active call without drilling and then advances to the next active execution point

#### Scenario: Active call carries running indicator
- **WHEN** a parent and its active call both have in-progress status
- **THEN** the call displays the running indicator and the parent suppresses its duplicate indicator

#### Scenario: Call completion is independent of parent
- **WHEN** a called child succeeds or fails while its parent remains active
- **THEN** the call row displays its terminal status without assigning that status to the parent

#### Scenario: Interactive parent retains terminal ownership
- **WHEN** an interactive parent records calls while it owns the terminal
- **THEN** the TUI does not interrupt it

#### Scenario: Interactive-parent calls appear after return
- **WHEN** an interactive parent returns terminal ownership after recording calls
- **THEN** the resumed run view reconstructs those calls beneath the parent from persisted evidence

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

## Test Plan

No named `INT-*` or `E2E-*` obligation is completed here. Add deterministic production-boundary tests for non-UI coordinator and run-view behavior using local subprocesses/fake CLIs:

- nested peer/sub-workflow/loop/call frontier transitions without automatic drilling;
- all active/tail follow transitions and viewport ownership;
- delayed stdout/stderr, accumulated off-selection output, and later reselection;
- split ANSI sequences, unmodified raw persistence, exact escaped paths, and first-byte timing;
- repeated/failing calls and independent parent/call output/status/metrics;
- interactive-parent terminal ownership and reconstruction after return;
- active polling versus inactive static rendering; and
- success/failure terminal states, summary/detail toggle, failed leaf selection, and post-terminal resume.

## Done When

- Live selection follows the active leaf without mutating manual scope and cannot steal paused exploration.
- `t` and `l` have distinct continuous behavior across later output and execution transitions.
- Raw and displayed output satisfy every assigned streaming, ANSI, persistence, attribution, and interactive-exclusion scenario.
- Calls behave as independent dynamic executions through start, output, completion, launch failure, interactive-parent return, and terminal inspection.
- Active metrics, latest attempts, summary totals, refresh cadence, and run-lock transitions remain correct.
- Successful and failed runs remain open in the specified terminal view and behave like historical detail after completion.
- Focused tests in `internal/liverun` and `internal/runview` pass.
