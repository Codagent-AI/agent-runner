## MODIFIED Requirements

### Requirement: CLI-reported cost captured verbatim

Agent Runner SHALL record a step's cost as `estimated_api_cost_usd` only when the step's CLI itself reports a cost denominated in USD, and SHALL record the CLI's value verbatim. The CLI's report MAY come from its headless structured output or, for autonomous-interactive Claude steps, from cost reports Claude Code makes to its status line during the interactive process, as specified by `interactive-claude-usage`. Such a report is process-scoped, so it SHALL be attributed against the process's own first report rather than by the session-cumulative delta. Agent Runner SHALL NOT compute cost from token counts: it maintains no pricing catalog, performs no token-times-rate math, fetches no prices, and converts no non-USD denominations.

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
- **WHEN** an autonomous-headless agent step completes and its CLI's output contains no cost field
- **THEN** the step's `estimated_api_cost_usd` is null

#### Scenario: Non-USD cost units are not converted
- **WHEN** an autonomous-headless agent step completes and its CLI reports cost only in a non-USD unit (such as a proprietary credit)
- **THEN** the step's `estimated_api_cost_usd` is null and no unit conversion is performed

#### Scenario: Autonomous-interactive Claude step reports cost in session
- **WHEN** an autonomous-interactive Claude step completes and Claude Code reported a USD session cost tied to the step's final turn
- **THEN** the step's `estimated_api_cost_usd` is the final report's value minus the value in the process's first report, with no price math

#### Scenario: Autonomous-interactive step without a usable cost report
- **WHEN** an autonomous-interactive agent step completes and no usable CLI-reported USD cost was captured, even though its token usage was collected
- **THEN** the step's `estimated_api_cost_usd` is null and no cost is computed from its tokens
