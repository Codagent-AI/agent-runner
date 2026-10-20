## MODIFIED Requirements

### Requirement: CLI-reported cost captured verbatim

Agent Runner SHALL record a step's CLI-reported cost as `estimated_api_cost_usd` only when the step's CLI itself reports a cost denominated in USD, and SHALL record the CLI's value verbatim. The only other source of `estimated_api_cost_usd` is the Codex estimate defined by `cost-estimation`. The CLI's report MAY come from its headless structured output or, for autonomous-interactive Claude steps, from cost reports Claude Code makes to its status line during the interactive process, as specified by `interactive-claude-usage`. Such a report is process-scoped, so it SHALL be attributed against the process's own first report rather than by the session-cumulative delta. Agent Runner SHALL NOT compute cost from token counts, with one exception: the Codex API-equivalent estimate defined by `cost-estimation`, computed from the embedded price table. Apart from that table, it maintains no pricing catalog and performs no token-times-rate math. It fetches no prices and converts no non-USD denominations. A CLI-reported cost SHALL always take precedence: an estimate SHALL NOT replace or adjust a cost the CLI reported.

#### Scenario: CLI reports a USD cost
- **WHEN** an autonomous-headless agent step completes and its CLI's structured output reports a USD-denominated cost
- **THEN** the step's `estimated_api_cost_usd` equals the reported value

#### Scenario: Session-cumulative cost is attributed by delta
- **WHEN** a CLI's reported USD cost is cumulative for its session (as Claude's `total_cost_usd` is) and a step resumes or inherits that session
- **THEN** the step's `estimated_api_cost_usd` is the reported value minus the prior reported value for the same session, with no price math

#### Scenario: Session-cumulative cost without a trusted baseline is null
- **WHEN** a step resumes a session whose prior cumulative cost was not observed, was cleared by an invocation that reported none, or exceeds the current value
- **THEN** the step's `estimated_api_cost_usd` is null

#### Scenario: CLI reports no cost
- **WHEN** an autonomous-headless agent step on a CLI other than Codex completes and its CLI's output contains no cost field
- **THEN** the step's `estimated_api_cost_usd` is null

#### Scenario: Codex step without a CLI-reported cost
- **WHEN** an autonomous-headless Codex step completes with token usage and no CLI-reported cost
- **THEN** the step's `estimated_api_cost_usd` is the estimate defined by `cost-estimation` when the step is eligible, and null with a reason otherwise

#### Scenario: Non-USD cost units are not converted
- **WHEN** an autonomous-headless agent step completes and its CLI reports cost only in a non-USD unit (such as a proprietary credit)
- **THEN** the step's `estimated_api_cost_usd` is null and no unit conversion is performed

#### Scenario: Autonomous-interactive Claude step reports cost in session
- **WHEN** an autonomous-interactive Claude step completes and Claude Code reported a USD session cost tied to the step's final turn
- **THEN** the step's `estimated_api_cost_usd` is the final report's value minus the value in the process's first report, with no price math

#### Scenario: Autonomous-interactive step without a usable cost report
- **WHEN** an autonomous-interactive agent step on a CLI other than Codex completes and no usable CLI-reported USD cost was captured, even though its token usage was collected
- **THEN** the step's `estimated_api_cost_usd` is null and no cost is computed from its tokens

### Requirement: Missing cost is null, never zero

When a step has neither a CLI-reported USD cost nor an eligible estimate under `cost-estimation`, the cost SHALL be represented as null (or an equivalent explicit absent state). This happens when the CLI reports none and no estimate applies, when the step's usage is unavailable, when a Codex step is ineligible for an estimate, or when the step is a non-agent step. A missing cost SHALL never be recorded as `0`. When a Codex step's estimate is ineligible, the null cost SHALL carry the reason defined by `cost-estimation`.

#### Scenario: Unavailable usage yields null cost
- **WHEN** an agent step's usage record is unavailable (e.g. interactive context or parse failure)
- **THEN** the step's `estimated_api_cost_usd` is null, not zero

#### Scenario: Shell step has null cost
- **WHEN** a shell step completes
- **THEN** its `estimated_api_cost_usd` is null (its token usage is a true zero, but no cost was reported)

#### Scenario: Ineligible Codex estimate yields null with reason
- **WHEN** a Codex step's usage is collected but its model has no price-table entry
- **THEN** the step's `estimated_api_cost_usd` is null with reason `model-unpriced`, not zero

### Requirement: Run-level cost aggregation with coverage

Run-level cost SHALL be the sum of `estimated_api_cost_usd` over the agent steps that have a cost, whether CLI-reported or estimated, regardless of step outcome. It SHALL be accompanied by an explicit coverage indicator: `complete` when every agent step that actually invoked its CLI has a cost, `partial` when some did and some did not, and `none` when no step reported a cost (including runs with no agent steps). Agent steps that never invoked their CLI (skipped, or failed before launch) SHALL NOT count toward the coverage denominator. When coverage is `none`, the run-level cost total SHALL be null. The run-level cost SHALL also carry a cost-basis indicator, `reported`, `estimated`, or `mixed` according to the bases of the summed costs (null when coverage is `none`), together with the subtotal contributed by each basis.

#### Scenario: All agent steps priced
- **WHEN** every completed agent step in a run reported a USD cost
- **THEN** the run-level cost is their sum and coverage is `complete`

#### Scenario: Mixed run is flagged partial
- **WHEN** a run contains one agent step whose CLI reported a cost and one whose CLI did not
- **THEN** the run-level cost is the sum of the reported costs and coverage is `partial`

#### Scenario: No cost reported
- **WHEN** a run's agent steps reported no cost (or the run has no agent steps)
- **THEN** the run-level cost is null and coverage is `none`

#### Scenario: Skipped agent step does not affect coverage
- **WHEN** a run contains one agent step that invoked its CLI and reported a cost, and one agent step that was skipped and never invoked its CLI
- **THEN** the run-level cost coverage is `complete`; the skipped step is excluded from the denominator

#### Scenario: Mixed Claude and Codex run
- **WHEN** a run contains a Claude step with a reported cost of $1.50 and an eligible Codex step with an estimated cost of $0.98
- **THEN** the run-level cost is $2.48, coverage is `complete`, the cost basis is `mixed`, and the subtotals are $1.50 reported and $0.98 estimated

#### Scenario: Ineligible Codex step leaves coverage partial
- **WHEN** a run contains a Claude step with a reported cost and a Codex step whose estimate is ineligible
- **THEN** the run-level cost is the Claude cost, coverage is `partial`, and the cost basis is `reported`

#### Scenario: Estimates only
- **WHEN** every costed step in a run is an eligible Codex estimate
- **THEN** the run-level cost basis is `estimated`

## ADDED Requirements

### Requirement: Every recorded cost carries its basis

Every non-null step cost SHALL carry a cost basis: `reported` when the step's CLI reported the value, or when it comes from a nested producer's own full attempt-level USD cost evidence (such as a Validator model attempt); `estimated` when Agent Runner computed it under `cost-estimation`. A null cost SHALL carry no basis. No non-null cost SHALL be aggregated without a basis: per-basis subtotals SHALL always sum to the cost total. Consumers SHALL be able to tell the two bases apart without inspecting the CLI or model.

#### Scenario: Reported cost basis
- **WHEN** a Claude step records its CLI-reported cost
- **THEN** its cost basis is `reported`

#### Scenario: Estimated cost basis
- **WHEN** a Codex step records an eligible estimate
- **THEN** its cost basis is `estimated`

#### Scenario: Producer-reported nested cost basis
- **WHEN** a nested Validator model attempt's producer evidence carries a full attempt-level USD cost
- **THEN** its projected step record's cost basis is `reported`

#### Scenario: Null cost has no basis
- **WHEN** a step's cost is null
- **THEN** it carries no cost basis
