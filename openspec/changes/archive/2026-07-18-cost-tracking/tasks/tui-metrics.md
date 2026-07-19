# Task: Run-view metrics and summary screen

## Goal

Surface the collected metrics in the terminal UI: token-usage and cost lines in every agent step's detail block (live and inspect), attempt-aware step nodes, and a new run summary screen with per-step rows, nested rollups, run totals, an `s` toggle, and summary-first behavior for successful and inspected completed runs.

## Background

You MUST read these files before starting:

- `openspec/changes/cost-tracking/design.md` — section 7 (TUI) and the Decisions list govern this task.
- `openspec/changes/cost-tracking/specs/run-complete-screen/spec.md`
- `openspec/changes/cost-tracking/specs/view-run/spec.md`
- `openspec/changes/cost-tracking/specs/live-run-view/spec.md`
- `internal/runview/tree.go` — `StepNode` and `DurationMs`.
- `internal/runview/audit.go` — `Tree.ApplyEvent`; `applyStepEnd` reads `duration_ms`; re-execution of the same logical step currently **replaces** runtime data.
- `internal/runview/detail.go` — detail-block rendering.
- `internal/runview/model.go` — `Model`, `handleExecDoneMsg`, `handleKey`.
- `internal/runview/view.go` — boolean view-mode flags (`showLegend`, `quitConfirming`), `helpBarParts`.
- `cmd/agent-runner/main.go` — the two liverun wirings; `handleExecDoneMsg` is the single choke point both share. Also the `--inspect`/run-list open paths for setting the initial view of completed runs.
- `internal/tuistyle/` — styling conventions.

### Current state you build on

Every terminal audit event is already enriched before it reaches `audit.log`: agent `step_end` entries carry a serialized `model.UsageRecord` (status, reason, cli, provider, model, tokens, raw_cumulative, source, completeness), an `estimated_api_cost_usd` (number or null), and an identity block with `attempt`, `kind`, `session_id`, and `agent_invoked`. `run_end` entries carry run totals (active duration, per-category token totals, usage coverage, cost total, cost coverage). The runview tree is built purely by tailing and parsing `audit.log` JSON, so this task parses those fields back into `model.UsageRecord` (the types in `internal/model` have JSON tags) — live mid-run updates then come for free from the existing tail.

Canonical token category keys are `input`, `cached_input`, `cache_write`, `output`, `reasoning` (constants in `internal/model`), plus preserved `other:<vendor-key>` entries.

### Step nodes and attempts

Replace the single-value metrics idea with attempt history on `StepNode`:

```go
type AttemptMetrics struct {
    Attempt    int
    Usage      *model.UsageRecord
    CostUSD    *float64
    DurationMs *int64
    Outcome    string
}
// StepNode gains: Attempts []AttemptMetrics
```

`applyStepEnd` appends an attempt instead of overwriting; the existing latest-wins replacement (:284) is kept for the other runtime fields. Display rule: **detail blocks show the latest attempt** (annotated `attempt N` when N > 1); **summary rows aggregate every attempt** (summed duration, summed usage, summed cost).

### Detail-block rendering (`detail.go`)

On completed agent steps, render token-usage and cost lines adjacent to the existing duration line. Unavailable usage renders an explicit marker (with its reason where helpful), never zero counts; unreported cost renders an unavailable marker, never `$0.00`. Non-agent steps show no usage/cost lines (their zero usage is implicit).

### Summary screen

- New `showSummary` boolean view flag following the `showLegend` pattern (`view.go:33-38`), with a `renderSummary` view function.
- `s` toggles summary ↔ detailed view in **every** run state (running, completed, failed, interrupted) at any drill depth (`handleKey`), and the help bar advertises it (`helpBarParts`).
- Auto-show on successful completion: set `showSummary` in `handleExecDoneMsg` when the terminal state is `completed`; failed runs keep today's behavior (detailed view, cursor on the failed step). This single choke point covers both liverun wirings in `main.go`.
- Runs with status `completed` opened via `--inspect` or the run list start with `showSummary = true`; all other statuses open in the detailed view as today.
- Content: one row per step showing duration and cost; repeated attempts aggregate into the step's row; loop iterations, sub-workflow children, and group members render indented beneath their parent; every container row shows rolled-up totals **derived from its descendants** (containers' own records carry only their own duration; never double-count). Steps without a reported cost show an explicit unavailable marker; a container with partially priced descendants marks its rolled-up cost as partial. Steps not yet run appear without time or cost values.
- Run-totals line: total active execution duration, per-category token totals, and the cost total with its coverage state, indicating when usage coverage is partial. For finished runs take the totals from the `run_end` event data (they are authoritative, cumulative across resume sessions); mid-run, compute totals from the tree's attempts and show elapsed time so far.

### Conventions

TDD for rendering/behavior changes (assert rendered content and state transitions); pure styling tweaks (colors, spacing) need no tests. Tests next to the package, `google/go-cmp`, `make fmt`, `make test`, `make lint`. Commit style: `type: lowercase description` (e.g. `feat: add run summary screen and step metrics to run views`).

## Spec

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

### Requirement: Detail pane per step type (relevant scenarios)

Token-usage and cost lines on agent blocks SHALL render adjacent to the duration line on completed steps. When usage is unavailable (PTY-backed step, parse failure) the usage line SHALL render an explicit unavailable marker; when no cost was reported the cost line SHALL render an unavailable marker, never `$0.00` (per `cost-capture`). When a logical step executed more than once, the detail block SHALL reflect the latest attempt's metrics, annotated with the attempt number; earlier attempts remain part of run-level aggregates (per `run-metrics-artifact`).

(This requirement in `specs/view-run/spec.md` also covers additional scenarios for the agent detail block: the THEN clauses of the Headless and Interactive agent block scenarios now include usage and cost lines — implement those too. The pre-existing block-content contract for headers, model lines, stdout/stderr, and resume actions must keep passing.)

#### Scenario: Agent block shows collected usage and cost
- **WHEN** a completed autonomous-headless agent step's block is rendered and usage plus cost were collected
- **THEN** the block shows the token usage and the reported cost adjacent to the duration line

#### Scenario: Agent block shows unavailable usage marker
- **WHEN** a completed agent step's block is rendered and its usage record is unavailable
- **THEN** the usage and cost lines render explicit unavailable markers; no zero token counts and no `$0.00` are shown

#### Scenario: Re-executed step block shows latest attempt
- **WHEN** a logical step executed twice and its block is rendered
- **THEN** the block shows the latest attempt's usage, cost, and duration with an attempt annotation; earlier attempts are not shown in the block but still count in run aggregates

### Requirement: TUI stays open after workflow completion (summary-related scenarios)

On a successful (`completed`) terminal state, the TUI SHALL display the run summary screen as the post-completion view, per the `run-complete-screen` capability; the `s` key toggles to the detailed run view. On a `failed` terminal state, the TUI SHALL show the detailed view with the cursor on the failed step, and the summary is available via `s`.

(This requirement in `specs/live-run-view/spec.md` also contains modified scenarios: the post-completion navigation and resume-action scenarios have updated WHEN clauses that include the summary-related state — implement those. The staying-open behavior and pre-existing resume-action flow must keep passing.)

#### Scenario: Successful completion shows summary screen
- **WHEN** the last step in the workflow completes successfully
- **THEN** the TUI remains open displaying the run summary screen, with the breadcrumb status showing `completed`

#### Scenario: Failure keeps TUI open in detailed view
- **WHEN** a step fails and the workflow halts
- **THEN** the TUI remains open in the detailed view with the breadcrumb status showing `failed`; the summary is not auto-shown

### Requirement: Metrics update during an active run

While a workflow run is executing, the live detailed view SHALL show each completed step's token usage and reported cost in its detail block as soon as the step completes, per the `view-run` capability's detail-pane contract. Metrics for the run so far SHALL also be available mid-run through the summary toggle (per `run-complete-screen`).

#### Scenario: Completed step shows metrics mid-run
- **WHEN** an agent step completes while later steps are still running
- **THEN** that step's detail block shows its token usage and reported cost without waiting for the run to finish

#### Scenario: Re-executed step shows latest attempt mid-run
- **WHEN** a logical step completes a second attempt while the run is still active
- **THEN** the step's detail block reflects the latest attempt's metrics (per the `view-run` attempt rule), while run-so-far totals include every attempt

## Done When

Runview tests cover every scenario above (detail lines with collected/unavailable/attempt states, summary rendering with rollups and totals, the `s` toggle in all states, auto-show on success, inspect-completed opening in summary). Inspecting a completed run and watching a live run both show metrics end-to-end. `make test` and `make lint` are clean.
