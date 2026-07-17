# audit-log-entries Specification (delta)

## MODIFIED Requirements

### Requirement: Agent step-specific data

Agent step entries SHALL include the interpolated prompt, mode, session strategy, resolved session ID, model, and engine enrichment on `step_start`. The `step_end` SHALL include exit code, discovered session ID, the step's token-usage record, and `estimated_api_cost_usd`.

The `model` field on `step_start` SHALL be the **resolved** model — that is, the model that agent-runner launched the CLI with, computed by composing the step-level `model:` override (if any) over the resolved agent profile's default model. For `session: resume` and `session: inherit` steps, the profile used SHALL be the profile of the session-originating step (i.e. the profile already recorded in the execution context under that step's ID), so the `model` value matches the model the CLI was originally invoked with when the session was created.

If no model can be resolved (the step has no `model:` override and no profile store / profile default is available to fall back on), the `model` field SHALL be emitted as an empty string.

The usage record on `step_end` SHALL follow the `agent-usage-collection` capability: distinct token categories with provenance and completeness, or an explicit unavailable state. `estimated_api_cost_usd` SHALL follow the `cost-capture` capability: the CLI-reported USD value, or null. Unavailable usage and absent cost SHALL be emitted as explicit null/unavailable values, never as zeros.

#### Scenario: Agent step start
- **WHEN** a headless agent step starts with session strategy `resume` and resolved session ID `abc-123`
- **THEN** the `step_start` entry includes prompt, mode, session strategy, resolved session ID, model, and enrichment

#### Scenario: Agent step end
- **WHEN** an agent step completes and Agent Runner discovers session ID `def-456`
- **THEN** the `step_end` entry includes exit code and discovered session ID `def-456`

#### Scenario: Agent step end includes usage and cost
- **WHEN** an autonomous-headless agent step completes with collected usage and a CLI-reported cost
- **THEN** the `step_end` entry includes the token-usage record (categories, provenance, completeness) and `estimated_api_cost_usd`

#### Scenario: Agent step end with unavailable usage
- **WHEN** an agent step completes but usage could not be collected (PTY-backed context or parse failure)
- **THEN** the `step_end` entry carries an explicit unavailable usage state and a null `estimated_api_cost_usd`; no zero counts are emitted

#### Scenario: Resolved model populated from profile default
- **WHEN** an agent step has no step-level `model:` override and its resolved profile specifies model `sonnet`
- **THEN** the `step_start` entry's `model` field is `sonnet`

#### Scenario: Resolved model populated from step-level override
- **WHEN** an agent step sets `model: opus` inline and its resolved profile's default model is `sonnet`
- **THEN** the `step_start` entry's `model` field is `opus`

#### Scenario: Resolved model for resumed session uses originating profile
- **WHEN** an agent step uses `session: resume` or `session: inherit` to reuse the CLI session of an earlier step whose profile had default model `opus`, and this step has no step-level override
- **THEN** the `step_start` entry's `model` field is `opus`

#### Scenario: Resolved model empty when nothing can be resolved
- **WHEN** an agent step has no step-level override and no profile store is available to supply a default
- **THEN** the `step_start` entry's `model` field is an empty string

## ADDED Requirements

### Requirement: Run end usage and cost totals

The `run_end` entry SHALL include the run's aggregated metrics: per-category token totals across all steps of the run (cumulative across resume sessions) with the usage-coverage indicator, and the run-level cost total with its coverage indicator, per the `cost-capture` capability. Token totals SHALL follow the aggregation semantics of the `run-metrics-artifact` capability: only reported values are summed, steps with unavailable usage contribute nothing, and unreported categories are absent rather than zero. When no step reported cost, the cost total SHALL be null with coverage `none`.

#### Scenario: Run end carries aggregated totals
- **WHEN** a run ends after agent steps consumed tokens and some reported cost
- **THEN** the `run_end` entry includes per-category token totals and the cost total with its coverage indicator

#### Scenario: Run end with no cost data
- **WHEN** a run ends and no step reported a USD cost
- **THEN** the `run_end` entry's cost total is null and its coverage is `none`

#### Scenario: Run end with mixed usage availability
- **WHEN** a run ends containing one agent step with a full usage record and one whose usage is unavailable
- **THEN** the `run_end` entry's token totals equal the reporting step's values with usage coverage `partial`; no zeros are substituted
