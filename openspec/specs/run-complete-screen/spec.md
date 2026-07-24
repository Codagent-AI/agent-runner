# run-complete-screen Specification

## Purpose
Define the completed-run summary experience, including when it appears, how users navigate metric scopes, and how duration, token, and cost values are aligned and labeled.
## Requirements
### Requirement: Summary is the post-completion view on success

When a live workflow run reaches the `completed` (successful) terminal state, the run-view TUI SHALL display the run summary screen as the post-completion view, replacing the step-list/log layout. When a run reaches the `failed` terminal state, the TUI SHALL keep today's behavior — the detailed view with the cursor on the failed step — and SHALL NOT auto-show the summary.

#### Scenario: Successful completion shows summary
- **WHEN** the last step of a live run completes successfully
- **THEN** the TUI displays the run summary screen

#### Scenario: Failure keeps the detailed view
- **WHEN** a step fails and the workflow halts
- **THEN** the TUI shows the detailed run view with the cursor on the failed step; the summary is not auto-shown

### Requirement: Contextual keys switch between run and summary views

The detailed run view SHALL bind `s` to open the summary screen, and the summary screen SHALL bind `v` to return to the detailed run view. These view switches SHALL work in every run state — while a run is executing (showing metrics collected so far), and for completed, failed, and interrupted runs — and at any drill depth. The detailed help bar SHALL advertise `s summary`; the summary help bar SHALL advertise `v view run` instead of calling the current screen a summary action.

#### Scenario: Toggle to summary mid-run
- **WHEN** a workflow is running and the user presses `s`
- **THEN** the summary screen is shown with the metrics of steps completed so far; steps not yet run appear without time or cost values

#### Scenario: Toggle back to detail
- **WHEN** the summary screen is shown and the user presses `v`
- **THEN** the detailed run view is restored in the state it was left

#### Scenario: Summary for a failed run on demand
- **WHEN** a run has failed and the user presses `s`
- **THEN** the summary screen is shown for the failed run

### Requirement: Default view for inspected completed runs

When a run whose status is `completed` is opened via `--inspect` or from the run list and its audit stream contains structured run metrics, the run view SHALL open showing the summary screen. Runs in any other status (failed, interrupted/inactive, active), and legacy completed runs whose audit stream predates structured metrics, SHALL open in the detailed view as before.

#### Scenario: Inspect completed run opens summary
- **WHEN** `agent-runner --inspect <run-id>` opens a run with status `completed` whose audit stream contains structured run metrics
- **THEN** the summary screen is displayed first; `v` switches to the detailed run view

#### Scenario: Legacy completed run opens the original detail view
- **WHEN** a completed run whose audit events contain no structured usage, cost, identity, or run-total fields is opened via `--inspect` or the run list
- **THEN** the detailed run view is displayed first rather than an empty metrics summary

#### Scenario: Inspect failed run opens detail
- **WHEN** a run with status `failed` is opened via `--inspect` or the run list
- **THEN** the detailed run view is displayed first

### Requirement: Level-scoped summary with token rollups

The summary screen SHALL render a full-width table for the current drill-in level, reusing the detailed run view's breadcrumb, cursor, and container navigation. The table SHALL show one row per direct child with right-aligned columns for duration, input, cache read, cache write, output, reasoning, and reported API cost. Up/down SHALL select rows, Enter SHALL drill into container rows, and Escape SHALL drill out. Nested descendants SHALL not be flattened into the same table.

When a logical step executed more than once, its row SHALL aggregate every attempt. A container row SHALL recursively roll up all descendant attempts. The aligned final Total row SHALL describe the current scope: the authoritative whole-run totals at the root and the selected container's descendant aggregate below the root. Every reported token category SHALL be summed vertically; absent categories SHALL render an em dash, reported zeros SHALL render `0`, and unavailable usage SHALL render `?`. Canonical processed input/output/overall totals and their coverage SHALL be shown beneath the raw-category Total row for eval-oriented consumption.

#### Scenario: Flat workflow rows
- **WHEN** the summary is shown for a run of top-level steps only
- **THEN** each step has a row with aligned duration, token-category, and cost columns, followed by the aligned Total row

#### Scenario: Loop rolls up its iterations
- **WHEN** a loop ran 3 iterations that each consumed time and cost
- **THEN** the root table shows one loop row with all descendant metrics rolled up, and Enter on that row opens a table of its 3 iterations

#### Scenario: Sub-workflow rolls up its children
- **WHEN** a sub-workflow step ran child steps
- **THEN** the parent table shows one sub-workflow row with child metrics rolled up, and Enter opens a table of its direct children

#### Scenario: Group rolls up its members
- **WHEN** a group step ran member steps that consumed time and cost
- **THEN** the parent table shows one group row with member metrics rolled up, and Enter opens a table of its direct members

#### Scenario: Repeated step aggregates its attempts
- **WHEN** a logical step executed twice in the run (a failed attempt followed by a successful one)
- **THEN** the step's summary row shows the summed duration and cost of both attempts

#### Scenario: Run totals line
- **WHEN** the summary is shown
- **THEN** the aligned Total row shows duration, every token category, and cost for the current scope, and the root additionally uses the authoritative active run duration

#### Scenario: Canonical processed-token totals
- **WHEN** the current scope contains steps with canonical processed-token totals
- **THEN** the summary shows canonical input, output, and overall tokens beneath the aligned raw-category Total row, with a partial marker when coverage is incomplete

#### Scenario: Run totals with steps missing usage
- **WHEN** the summary is shown for a run where some agent steps have unavailable usage
- **THEN** the totals line sums only the reported usage and indicates that usage coverage is partial

### Requirement: Unavailable cost display in summary

Steps without a reported cost SHALL display `?` in the cost column, never `$0.00`. A container row whose descendants are only partially priced SHALL mark its rolled-up cost as partial.

#### Scenario: Unpriced step shows marker
- **WHEN** a step whose CLI reported no cost appears in the summary
- **THEN** its cost column shows `?` rather than a zero amount

#### Scenario: Partially priced container flagged
- **WHEN** a sub-workflow contains one step with reported cost and one without
- **THEN** the sub-workflow row shows the sum of the priced steps with a partial indicator

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

