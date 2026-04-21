## MODIFIED Requirements

### Requirement: Agent step-specific data

Agent step entries SHALL include the interpolated prompt, mode, session strategy, resolved session ID, model, and engine enrichment on `step_start`. The `step_end` SHALL include exit code and discovered session ID.

The `model` field on `step_start` SHALL be the **resolved** model — that is, the model that agent-runner launched the CLI with, computed by composing the step-level `model:` override (if any) over the resolved agent profile's default model. For `session: resume` and `session: inherit` steps, the profile used SHALL be the profile of the session-originating step (i.e. the profile already recorded in the execution context under that step's ID), so the `model` value matches the model the CLI was originally invoked with when the session was created.

If no model can be resolved (the step has no `model:` override and no profile store / profile default is available to fall back on), the `model` field SHALL be emitted as an empty string.

#### Scenario: Agent step start
- **WHEN** a headless agent step starts with session strategy `resume` and resolved session ID `abc-123`
- **THEN** the `step_start` entry includes prompt, mode, session strategy, resolved session ID, model, and enrichment

#### Scenario: Agent step end
- **WHEN** an agent step completes and baton discovers session ID `def-456`
- **THEN** the `step_end` entry includes exit code and discovered session ID `def-456`

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
