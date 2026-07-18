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

### Requirement: Summary toggle key in any state

The run view SHALL bind the `s` key to toggle between the summary screen and the detailed run view. The toggle SHALL work in every run state — while a run is executing (showing metrics collected so far), and for completed, failed, and interrupted runs — and at any drill depth. The help bar SHALL advertise the `s` binding.

#### Scenario: Toggle to summary mid-run
- **WHEN** a workflow is running and the user presses `s`
- **THEN** the summary screen is shown with the metrics of steps completed so far; steps not yet run appear without time or cost values

#### Scenario: Toggle back to detail
- **WHEN** the summary screen is shown and the user presses `s`
- **THEN** the detailed run view is restored in the state it was left

#### Scenario: Summary for a failed run on demand
- **WHEN** a run has failed and the user presses `s`
- **THEN** the summary screen is shown for the failed run

### Requirement: Default view for inspected completed runs

When a run whose status is `completed` is opened via `--inspect` or from the run list, the run view SHALL open showing the summary screen. Runs in any other status (failed, interrupted/inactive, active) SHALL open in the detailed view as today.

#### Scenario: Inspect completed run opens summary
- **WHEN** `agent-runner --inspect <run-id>` opens a run with status `completed`
- **THEN** the summary screen is displayed first; `s` switches to the detailed view

#### Scenario: Inspect failed run opens detail
- **WHEN** a run with status `failed` is opened via `--inspect` or the run list
- **THEN** the detailed run view is displayed first

### Requirement: Level-scoped summary with token rollups

The summary screen SHALL render a full-width table for the current drill-in level, reusing the detailed run view's breadcrumb, cursor, and container navigation. The table SHALL show one row per direct child with right-aligned columns for duration, input, cache read, cache write, output, reasoning, and reported API cost. Up/down SHALL select rows, Enter SHALL drill into container rows, and Escape SHALL drill out. Nested descendants SHALL not be flattened into the same table.

When a logical step executed more than once, its row SHALL aggregate every attempt. A container row SHALL recursively roll up all descendant attempts. The aligned final Total row SHALL describe the current scope: the authoritative whole-run totals at the root and the selected container's descendant aggregate below the root. Every reported token category SHALL be summed vertically; absent categories SHALL render an em dash, reported zeros SHALL render `0`, and unavailable usage SHALL render an explicit unavailable marker. Canonical processed input/output/overall totals and their coverage SHALL be shown beneath the raw-category Total row for eval-oriented consumption.

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

Steps without a reported cost SHALL display an explicit unavailable marker in the cost column, never `$0.00`. A container row whose descendants are only partially priced SHALL mark its rolled-up cost as partial.

#### Scenario: Unpriced step shows marker
- **WHEN** a step whose CLI reported no cost appears in the summary
- **THEN** its cost column shows an unavailable marker rather than a zero amount

#### Scenario: Partially priced container flagged
- **WHEN** a sub-workflow contains one step with reported cost and one without
- **THEN** the sub-workflow row shows the sum of the priced steps with a partial indicator
