# run-complete-screen Specification

## Purpose
TBD - created by archiving change cost-tracking. Update Purpose after archive.
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

### Requirement: Summary content with nested rollup

The summary screen SHALL show one row per step of the workflow, displaying the step's wall-clock duration and its reported API cost. When a logical step executed more than once (repeated attempts), its row SHALL aggregate the duration, usage, and cost of every attempt. Nested steps — loop iterations, sub-workflow children, and group members — SHALL render indented beneath their parent, and every container row SHALL show the rolled-up totals (summed duration and summed cost) of its descendants. Top-level rows therefore show whole-subtree totals. A run-totals line SHALL show the run's total active execution duration (the sum of its execution sessions, excluding interrupted time, per `run-metrics-artifact`), per-category token totals, and cost total with its coverage state (per `cost-capture`). Token totals SHALL follow the `run-metrics-artifact` aggregation semantics — only reported values are summed — and the totals line SHALL indicate when usage coverage is partial.

#### Scenario: Flat workflow rows
- **WHEN** the summary is shown for a run of top-level steps only
- **THEN** each step has a row with its duration and cost, followed by the run-totals line

#### Scenario: Loop rolls up its iterations
- **WHEN** a loop ran 3 iterations that each consumed time and cost
- **THEN** the loop's row shows the summed duration and cost of all 3 iterations, and the iteration rows render indented beneath it

#### Scenario: Sub-workflow rolls up its children
- **WHEN** a sub-workflow step ran child steps
- **THEN** the sub-workflow's row shows its children's summed duration and cost, with the child rows indented beneath it

#### Scenario: Group rolls up its members
- **WHEN** a group step ran member steps that consumed time and cost
- **THEN** the group's row shows its members' summed duration and cost, with the member rows indented beneath it

#### Scenario: Repeated step aggregates its attempts
- **WHEN** a logical step executed twice in the run (a failed attempt followed by a successful one)
- **THEN** the step's summary row shows the summed duration and cost of both attempts

#### Scenario: Run totals line
- **WHEN** the summary is shown
- **THEN** a totals line shows the run's total active execution duration, per-category token totals, and the cost total with its coverage indicator

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

