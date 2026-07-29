# Task: Render Call Executions

## Goal

Project persisted and live agent-call evidence into Agent Runner's run views. Show calls as dynamic child executions beneath their parent, stream autonomous-child output independently, support inactive-session resume, and roll metrics into the completed summary exactly once.

## Background

You MUST read these approved artifacts before starting:

- `proposal.md`, especially the `view-run`, `live-run-view`, and `run-complete-screen` capability impacts.
- `design.md`, especially **Decision 8: Add dynamic call nodes to run views**, the interactive visibility and metrics compatibility risks, and migration step 5.
- `specs/view-run/spec.md`, `specs/live-run-view/spec.md`, and `specs/run-complete-screen/spec.md` for the acceptance criteria copied below.

Relevant implementation and documentation paths:

- `internal/runview/tree.go` defines static/dynamic node types, runtime fields, attempts, containment, drill-down, and stable node keys.
- `internal/runview/audit.go` parses prefixes and events, reconstructs missing workflow structure, creates dynamic nodes, and applies runtime/metric fields.
- `internal/runview/logview.go`, `internal/runview/detail.go`, `internal/runview/view.go`, and `internal/runview/model.go` render rows, detail panes, selection, expansion, navigation, active-run refresh, and direct session resume.
- `internal/runview/summary.go` aggregates logical-step attempts, container scopes, duration, tokens, cost, and coverage.
- `internal/runview/output.go` and the model's output loading resolve persisted stdout/stderr without treating audit metadata as full output.
- `internal/liverun/messages.go`, `internal/liverun/process_runner.go`, `internal/liverun/chunk_writer.go`, and `internal/liverun/coordinator.go` route active prefixes, output chunks, auto-follow, terminal suspension, and restoration.
- Existing focused fixtures and tests live in `internal/runview/*_test.go` and `internal/liverun/*_test.go`.
- User-facing behavior belongs in a new focused `docs/agent-calls.md` page and updates to `docs/README.md`, `docs/sessions-and-modes.md`, `docs/run-state-and-audit.md`, and `docs/usage-and-cost-tracking.md`. Follow `docs/AGENTS.md`, including page frontmatter and relative links.

Add a dynamic agent-call node beneath the exact parent attempt identified by persisted evidence. It is an execution node, never a workflow-definition step and never part of workflow sequencing. Repeated targets remain distinct and ordered by invocation time. Use the design's `↗` type glyph and explicit labels `call session: <name>` and `call agent: <profile>`.

A parent with accepted calls displays `(<n> calls)` and becomes expandable through the existing inline nested-row interaction. Insert a call node when the call becomes accepted, before CLI launch. A CLI launch failure leaves that node visible with failed status and its failed metric record. The call node owns its status, output, error, resolved target/agent/session data, timing, usage, and cost. Load stdout/stderr from the call-specific output files; do not reconstruct a child response from `audit.log`.

For autonomous-headless parents, live events create the call node immediately and call-specific output chunks update its detail pane. Existing auto-follow moves into the active call and returns to the next active execution afterward; manual navigation disables auto-follow. Apply active-child status suppression so only the expanded running child blinks. For interactive parents, preserve terminal ownership and rebuild accumulated calls when the TUI resumes instead of drawing over the CLI.

In the completed summary, treat a parent with calls as a container. Its enclosing row rolls up parent attempts plus calls for usage and cost, but duration remains the parent step's wall-clock attempt duration. Entering it yields a synthetic `parent turn` row followed by chronological call rows. Steps without calls remain leaves. Only an inactive completed call with a known CLI session ID can expose the existing direct-resume action.

Complete user documentation in this final user-visible delivery unit so it can describe the finished behavior without creating overlapping doc edits elsewhere. Document prompt gating, supported fields, serial synchronous execution, named-session reuse, approval and timeout behavior, cancellation, non-recursion, evidence/output locations, metrics aggregation, live/completed views, and troubleshooting limits.

## Spec

### From `specs/view-run/spec.md`

### Requirement: Agent-call hierarchy rendering

A parent agent row with accepted agent calls SHALL display its call count and become expandable through the existing nested-row interaction. When expanded, each call SHALL appear as a dynamic child execution row rather than a workflow step, ordered by invocation time. A named-session target SHALL be labeled `call session: <name>` and a profile target SHALL be labeled `call agent: <profile>`. Each call row SHALL display its own status and an agent-call type glyph; the exact glyph is a design choice. An accepted call whose child CLI fails to launch SHALL remain visible as a failed call row.

#### Scenario: Parent displays call count
- **WHEN** a parent agent attempt has two accepted calls
- **THEN** its row displays `(2 calls)`

#### Scenario: Expanded parent shows chronological calls
- **WHEN** the user expands a parent with multiple calls
- **THEN** the run view shows one child execution row per call in invocation order

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

Selecting an agent-call row SHALL show the target kind and name; resolved profile, CLI, model, session metadata, and working directory; prompt, outcome, duration, usage, cost, and error; and stdout and stderr retained through ordinary headless-agent output behavior. The detail view MUST NOT reconstruct full output from `audit.log`.

When the run is inactive, a completed called-agent execution with a known CLI session ID SHALL offer the existing direct session-resume action. The action MUST NOT be available while the run is active or when no session ID is known.

#### Scenario: Selected call shows execution detail
- **WHEN** the user selects a running or completed agent-call row
- **THEN** the detail pane shows the call's target, resolved agent metadata, prompt, status, timing, metrics, and error information available for that execution

#### Scenario: Persisted call output is displayed
- **WHEN** ordinary headless-agent output persistence created stdout or stderr files for the selected call
- **THEN** the detail pane displays that persisted output

#### Scenario: Audit metadata is not treated as full output
- **WHEN** no persisted output exists for a selected call
- **THEN** the run view does not reconstruct or display a full child response from `audit.log`

#### Scenario: Inactive call session can be resumed
- **WHEN** the run is inactive and the selected completed call has a known CLI session ID
- **THEN** the existing direct resume action is available for that called-agent session

#### Scenario: Resume unavailable during active run
- **WHEN** the run is active or the selected call has no known CLI session ID
- **THEN** the direct resume action is unavailable for that call

### From `specs/live-run-view/spec.md`

### Requirement: Live agent-call visibility

When an autonomous-headless parent invokes `call_agent`, the live run view SHALL insert the call beneath its parent when it becomes accepted, before child CLI launch, update the call's status independently, and stream its stdout and stderr through the call's detail pane using ordinary headless-agent behavior. Parent and called-child output MUST remain separate. If the CLI fails to launch, the inserted call SHALL transition to failed and remain visible.

When auto-follow is engaged, the cursor SHALL move to an active call and SHALL return to the next active execution point after the call finishes. Existing manual navigation SHALL pause auto-follow. When an expanded parent has an active call child, the child SHALL carry the sole running indicator using the existing active-child status-suppression behavior.

When an interactive parent owns the terminal, Agent Runner MUST NOT interrupt it to display the run view. Calls completed during that interval SHALL appear when the parent returns terminal ownership and the TUI resumes.

#### Scenario: Autonomous call appears live
- **WHEN** an autonomous-headless parent starts an accepted agent call
- **THEN** the live run view inserts an in-progress call row beneath that parent

#### Scenario: CLI launch failure updates live row
- **WHEN** an accepted autonomous call fails while launching its child CLI
- **THEN** its inserted live row transitions to failed without disappearing

#### Scenario: Called-child output streams separately
- **WHEN** an active called child produces stdout or stderr
- **THEN** the bytes stream into the call's detail pane without being attributed to the parent row

#### Scenario: Auto-follow enters and leaves call
- **WHEN** auto-follow is engaged as an agent call starts and later finishes
- **THEN** the cursor moves to the active call and then returns to the next active execution point

#### Scenario: Manual navigation pauses call auto-follow
- **WHEN** the user navigates manually before or during an active call
- **THEN** the cursor remains where the user placed it instead of moving automatically to the call

#### Scenario: Active call carries running indicator
- **WHEN** an expanded parent has an in-progress call child
- **THEN** the child displays the running indicator and the parent suppresses its duplicate running indicator

#### Scenario: Call completion is independent of parent
- **WHEN** a called child succeeds or fails while its parent remains active
- **THEN** the call row displays its terminal status without assigning that status to the parent row

#### Scenario: Interactive parent retains terminal ownership
- **WHEN** an interactive parent invokes `call_agent` while its CLI owns the terminal
- **THEN** Agent Runner does not display or resume the run-view TUI during the call

#### Scenario: Interactive-parent calls appear after return
- **WHEN** an interactive parent returns terminal ownership after completing one or more calls
- **THEN** the resumed run view displays those calls beneath the parent from persisted run evidence

### From `specs/run-complete-screen/spec.md`

### Requirement: Agent-call summary rollup and drill-down

An agent step with accepted agent calls SHALL be a drillable container in the run summary. In its enclosing scope, the parent row SHALL roll up the parent turn's own usage and cost together with every called-agent execution exactly once. Its duration SHALL use the parent step's wall-clock attempt duration, including all repeated attempts, and MUST NOT add called-agent durations because they overlap time spent waiting within the parent.

Entering the parent row SHALL show a `parent turn` row followed by one row per accepted call in invocation order. The `parent turn` row SHALL aggregate only the parent step's own attempts. Each call row SHALL show its independent status and metrics and SHALL use `call session: <name>` or `call agent: <profile>` to identify its target. An accepted call whose child CLI failed to launch SHALL appear as a failed call row with its failed metric record. The scope Total SHALL sum usage and cost from the `parent turn` and call rows while retaining the parent step's wall-clock duration. An agent step without accepted calls SHALL remain an ordinary leaf row.

#### Scenario: Parent row rolls up own and call metrics
- **WHEN** a parent agent step and two called agents report usage or cost
- **THEN** the parent's enclosing summary row includes the parent turn and both calls exactly once

#### Scenario: Parent duration does not double-count calls
- **WHEN** a parent step runs for 60 seconds while synchronously waiting 30 seconds for a called agent
- **THEN** the parent row and its drilled scope report 60 seconds rather than 90 seconds

#### Scenario: Drill-down separates parent turn and calls
- **WHEN** the user enters a summary row whose agent step made two accepted calls
- **THEN** the scope shows `parent turn` followed by two call rows in invocation order

#### Scenario: Call targets are explicit
- **WHEN** the drilled scope contains a named-session call and a profile call
- **THEN** their rows are labeled `call session: <name>` and `call agent: <profile>` respectively

#### Scenario: Failed call remains independently visible
- **WHEN** a parent recovers from a failed call and completes successfully
- **THEN** the drilled summary retains the failed call row beneath the successful parent scope

#### Scenario: CLI launch failure appears in summary
- **WHEN** an accepted call fails while launching its child CLI
- **THEN** the drilled summary contains its failed call row and failed metric record

#### Scenario: Repeated parent attempts are aggregated
- **WHEN** a logical parent agent step runs more than one attempt and those attempts make accepted calls
- **THEN** `parent turn` aggregates the parent attempts while each accepted call remains a separate chronological row

#### Scenario: Agent without calls remains a leaf
- **WHEN** an agent step has no accepted agent calls
- **THEN** its summary row retains ordinary leaf behavior

## Done When

- Audit replay creates a distinct dynamic node for every accepted call beneath the correct parent attempt, including completed-run inspection when the workflow file is missing or moved.
- Expanded rows display the parent count, chronological call children, independent statuses, `↗` glyph, and exact target labels; repeated calls to one target do not collapse.
- The detail pane displays all approved request/resolution/result/metric fields, reads call-specific stdout/stderr files lazily, handles absent/non-UTF8/large output through existing behavior, and never treats audit metadata as the full response.
- Direct session resume is available only for inactive completed calls with known CLI session IDs and routes the selected call's CLI/session through the existing resume path.
- Live autonomous calls insert and update independently, stream stdout and stderr only to the child, transfer auto-follow into and out of the call, respect manual navigation, and suppress the parent's duplicate running indicator.
- Interactive parents retain terminal ownership throughout calls; all accumulated rows, details, and output become visible when the run TUI resumes.
- Completed summary tests prove parent-turn/call drill-down ordering, retry aggregation, independent failed-call visibility, exact-once usage/cost rollup, unchanged coverage semantics, non-additive child duration, and leaf behavior for agents without calls.
- Launch-failure view tests prove an accepted call appears before CLI launch, transitions to failed if launch fails, remains visible in live and reconstructed views, and contributes its failed metric record to the completed drill-down without usage coverage.
- `docs/agent-calls.md` and the linked existing docs accurately describe the shipped contract and artifact/UI behavior, with no internal MCP subcommand presented as a user API.
- Tests for every scenario copied into this task pass. Run `make fmt`, targeted `internal/runview` and `internal/liverun` tests, `go test ./...`, and `make lint`.
