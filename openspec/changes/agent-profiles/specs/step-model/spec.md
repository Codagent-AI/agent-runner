## MODIFIED Requirements

### Requirement: Per-step model override
A step MAY include a `model` field specifying which model the agent should use. When present, the runner SHALL pass the model to the CLI adapter, overriding the model from the resolved agent profile. When absent, the profile's model is used (which may itself be unset, in which case no model is passed to the CLI). The `model` field is only valid on agent steps, not shell steps.

#### Scenario: Model specified overrides profile
- **WHEN** an agent step has `agent: headless_base` (profile model=opus) and `model: sonnet`
- **THEN** the runner passes sonnet to the CLI adapter, not the profile's model

#### Scenario: No model on step, profile has model
- **WHEN** an agent step does not have a `model` field and the resolved profile has model=opus
- **THEN** the runner passes opus to the CLI adapter

#### Scenario: No model on step, profile has no model
- **WHEN** an agent step does not have a `model` field and the resolved profile has no model set
- **THEN** the runner invokes the CLI adapter without a model override

#### Scenario: Model on shell step
- **WHEN** a shell step has a `model` field
- **THEN** the runner fails with a validation error

### Requirement: Per-step CLI override
A step MAY include a `cli` field specifying which CLI backend to use. When present, it SHALL override the cli from the resolved agent profile. When absent, the profile's cli is used. The `cli` field is only valid on agent steps, not shell steps.

#### Scenario: CLI specified overrides profile
- **WHEN** an agent step has `agent: headless_base` (profile cli=claude) and `cli: codex`
- **THEN** the runner uses the Codex adapter for that step

#### Scenario: CLI not specified, uses profile
- **WHEN** an agent step has no `cli` field and the resolved profile has cli=claude
- **THEN** the runner uses the Claude adapter

#### Scenario: CLI on shell step
- **WHEN** a shell step has a `cli` field
- **THEN** the runner fails with a validation error

## REMOVED Requirements

### Requirement: Step mode field
**Reason**: The `mode` field as a step-type discriminator is removed. Shell steps are identified by the `command` field. Agent steps are identified by the `prompt` and/or `agent` field. The execution mode (interactive/headless) is determined by the resolved agent profile's `default_mode`, with an optional per-step `mode` override.
**Migration**: Replace `mode: interactive` or `mode: headless` with an `agent` profile reference on the first agentic step. For subsequent steps that resume the session, remove the `mode` field entirely or use the optional `mode` override to switch between interactive and headless.
