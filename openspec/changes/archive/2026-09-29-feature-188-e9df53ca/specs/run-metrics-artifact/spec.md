## ADDED Requirements

### Requirement: Subagent usage in the artifact

For a Claude agent step that spawned subagents, `run-metrics.json` SHALL expose the following:

- The step's native measurement SHALL contain a main-thread allocation and one or more subagent allocations, each with its observed model identity and token categories.
- Subagent allocations SHALL be distinguishable from the main-thread allocation, so that consumers can report main-thread and subagent usage separately.
- The compatibility step fields `usage.tokens` and `usage.token_totals`, the run-level per-category and canonical totals, and session rollups SHALL include subagent tokens exactly once.
- Each step SHALL still count as a single attempt in every coverage denominator.

#### Scenario: Subagent usage visible per step
- **WHEN** a run's `simplify` step spawns four subagents whose transcripts are all readable
- **THEN** the step's record in `run-metrics.json` shows non-zero subagent usage in subagent allocations with observed models, alongside a main-thread allocation

#### Scenario: Run totals include subagents once
- **WHEN** a run contains one Claude step with 10,000 main-thread tokens and 4,000 subagent tokens and one Codex step with 5,000 tokens
- **THEN** the run-level canonical overall total is 19,000, and the Claude step counts once in the usage and canonical-total coverage denominators

### Requirement: Partial subagent collection affects coverage

When a Claude step's subagent collection is partial, the step SHALL count as partial for run-level and session-level usage coverage and canonical-total coverage. Its known subtotal SHALL still contribute to the totals. As a result, coverage SHALL be `partial`, not `complete`, even if every other agent step is complete. The corresponding native token fields SHALL carry partial availability with the reason. Cost coverage SHALL be determined independently, by whether the step reported an eligible full USD cost.

This rule SHALL apply only to partial subagent collection. Usage records that are partial for other reasons, such as nested Validator projections or a missing expected category, SHALL keep their existing coverage treatment.

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
