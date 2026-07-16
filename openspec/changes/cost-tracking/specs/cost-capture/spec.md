# cost-capture Specification (delta)

## ADDED Requirements

### Requirement: CLI-reported cost captured verbatim

Agent Runner SHALL record a step's cost as `estimated_api_cost_usd` only when the step's CLI itself reports a cost denominated in USD, and SHALL record the CLI's value verbatim. Agent Runner SHALL NOT compute cost from token counts: it maintains no pricing catalog, performs no token-times-rate math, fetches no prices, and converts no non-USD denominations.

#### Scenario: CLI reports a USD cost
- **WHEN** an autonomous-headless agent step completes and its CLI's structured output reports a USD-denominated cost
- **THEN** the step's `estimated_api_cost_usd` equals the reported value

#### Scenario: CLI reports no cost
- **WHEN** an autonomous-headless agent step completes and its CLI's output contains no cost field
- **THEN** the step's `estimated_api_cost_usd` is null

#### Scenario: Non-USD cost units are not converted
- **WHEN** an autonomous-headless agent step completes and its CLI reports cost only in a non-USD unit (such as a proprietary credit)
- **THEN** the step's `estimated_api_cost_usd` is null and no unit conversion is performed

### Requirement: Missing cost is null, never zero

When a step has no CLI-reported USD cost — because the CLI reports none, the step's usage is unavailable, or the step is a non-agent step — the cost SHALL be represented as null (or an equivalent explicit absent state). A missing cost SHALL never be recorded as `0`.

#### Scenario: Unavailable usage yields null cost
- **WHEN** an agent step's usage record is unavailable (e.g. PTY-backed context or parse failure)
- **THEN** the step's `estimated_api_cost_usd` is null, not zero

#### Scenario: Shell step has null cost
- **WHEN** a shell step completes
- **THEN** its `estimated_api_cost_usd` is null (its token usage is a true zero, but no cost was reported)

### Requirement: Run-level cost aggregation with coverage

Run-level cost SHALL be the sum of `estimated_api_cost_usd` over the agent steps that reported a cost, accompanied by an explicit coverage indicator: `complete` when every completed agent step in the run reported a cost, `partial` when some did and some did not, and `none` when no step reported a cost (including runs with no agent steps). When coverage is `none`, the run-level cost total SHALL be null.

#### Scenario: All agent steps priced
- **WHEN** every completed agent step in a run reported a USD cost
- **THEN** the run-level cost is their sum and coverage is `complete`

#### Scenario: Mixed run is flagged partial
- **WHEN** a run contains one agent step whose CLI reported a cost and one whose CLI did not
- **THEN** the run-level cost is the sum of the reported costs and coverage is `partial`

#### Scenario: No cost reported
- **WHEN** a run's agent steps reported no cost (or the run has no agent steps)
- **THEN** the run-level cost is null and coverage is `none`

### Requirement: Cost separated from raw usage

The captured cost SHALL be stored as a distinct field alongside — not merged into — the raw usage record (token categories and provenance). Consumers SHALL be able to read token counts independently of whether a cost was reported.

#### Scenario: Tokens present, cost absent
- **WHEN** an agent step's CLI reports token usage but no cost
- **THEN** the step's record contains the full token-category counts and a null cost, and both are independently readable
