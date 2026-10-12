## RENAMED Requirements

- FROM: `### Requirement: Partial subagent collection affects coverage`
- TO: `### Requirement: Partial Claude transcript collection affects coverage`

## MODIFIED Requirements

### Requirement: Partial Claude transcript collection affects coverage

When a Claude step's subagent collection is partial, or an autonomous-interactive Claude step's main-thread transcript collection is partial, the step SHALL count as partial for run-level and session-level usage coverage and canonical-total coverage. Its known subtotal SHALL still contribute to the totals. As a result, coverage SHALL be `partial`, not `complete`, even if every other agent step is complete. The corresponding native token fields SHALL carry partial availability with the reason. Cost coverage SHALL be determined independently, by whether the step reported an eligible full USD cost.

This rule SHALL apply only to partial subagent collection and partial main-thread transcript collection. Usage records that are partial for other reasons, such as nested Validator projections or a missing expected category, SHALL keep their existing coverage treatment.

#### Scenario: Other partial records keep existing coverage
- **WHEN** a run contains a nested Validator attempt whose usage record is partial, and a Claude step whose subagent collection is complete
- **THEN** usage coverage is `complete`, as it is today

#### Scenario: Missing subagent transcript yields partial coverage
- **WHEN** a run's only agent step is a Claude step whose main thread and one subagent were collected but whose second subagent transcript is missing, and the CLI reported `total_cost_usd`
- **THEN** the run-level token totals equal the known subtotal, usage coverage and canonical-total coverage are `partial`, and cost coverage is `complete`

#### Scenario: Collection reason visible without a subagent allocation
- **WHEN** a Claude step's stdout shows no subagent spawns and its portion of the parent session transcript cannot be established
- **THEN** the step's record in `run-metrics.json` shows subagent collection `partial` together with the specific reason, even though it has no subagent allocations

#### Scenario: Session rollup mirrors partial coverage
- **WHEN** an execution session contains a Claude step with partial subagent collection
- **THEN** that session's rollup reports `partial` usage and canonical-total coverage and includes the known subtotal

#### Scenario: Partial main-thread transcript yields partial coverage
- **WHEN** a run's only agent step is an autonomous-interactive Claude step whose session transcript span contains an unparseable line among valid assistant messages
- **THEN** the run-level token totals equal the known subtotal, and usage coverage and canonical-total coverage are `partial`

#### Scenario: Collected autonomous-interactive step counts as complete
- **WHEN** a run contains one headless Codex step with reported usage and one autonomous-interactive Claude step whose main thread and subagents were all collected from transcripts
- **THEN** usage coverage and canonical-total coverage are `complete`, and the run-level totals include both steps once
