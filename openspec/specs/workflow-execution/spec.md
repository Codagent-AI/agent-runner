# Capability: workflow-execution

## Purpose

Defines how the runner dispatches agent step execution to CLI adapters via PTY or direct process execution.
## Requirements
### Requirement: Agent step execution dispatch
The runner's agent step executor SHALL resolve the agent profile before delegating CLI invocation. For `session: new` steps, the profile is resolved from the step's `agent` field. For `session: resume` or `session: inherit` steps, the profile is inherited from the session-originating step. The step's optional `mode` override is applied on top of the resolved profile's `default_mode`. Per-step `model` and `cli` overrides, if present, take precedence over the profile's values. Interactive steps SHALL execute via the PTY layer. Headless steps SHALL execute via direct process execution. Both paths use the adapter for arg construction.

#### Scenario: New session step dispatched
- **WHEN** the runner executes an agent step with `session: new` and `agent: interactive_base`
- **THEN** the runner resolves the `interactive_base` profile, determines mode from the profile's `default_mode` (or the step's `mode` override), and dispatches via PTY for interactive or direct exec for headless

#### Scenario: Resume step with mode override
- **WHEN** the runner executes an agent step with `session: resume` and `mode: headless`, and the inherited profile has `default_mode: interactive`
- **THEN** the runner inherits the profile from the session-originating step, overrides mode to headless, and dispatches via direct exec

#### Scenario: Resume step with no overrides
- **WHEN** the runner executes an agent step with `session: resume` and no `mode`, `model`, or `cli` overrides
- **THEN** the runner inherits the profile from the session-originating step and uses all profile values as-is

#### Scenario: Resume step with per-step model override
- **WHEN** the runner executes an agent step with `session: resume` and `model: sonnet`, and the inherited profile has model=opus
- **THEN** the runner uses sonnet for that step's CLI invocation, not the profile's opus

#### Scenario: Inherit step resolves profile from session origin
- **WHEN** the runner executes an agent step with `session: inherit` and no overrides
- **THEN** the runner inherits the profile from the session-originating step and uses all profile values as-is

