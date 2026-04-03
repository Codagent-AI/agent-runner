## MODIFIED Requirements

### Requirement: Per-step model override

A step MAY include a `model` field specifying which model the agent should use. When present, the runner SHALL pass the model to the CLI adapter for inclusion in the invocation args. When absent, the CLI uses its default model. The `model` field is only valid on agent steps (headless or interactive), not shell steps.

#### Scenario: Model specified on agent step
- **WHEN** a headless step has `model: sonnet`
- **THEN** the runner passes the model to the CLI adapter for invocation

#### Scenario: No model specified
- **WHEN** a step does not have a `model` field
- **THEN** the runner invokes the CLI adapter without a model override, using the CLI's default

#### Scenario: Model on shell step
- **WHEN** a shell step has a `model` field
- **THEN** the runner fails at load time with a validation error

## ADDED Requirements

### Requirement: Per-step CLI override

A step MAY include a `cli` field specifying which CLI backend to use (e.g., `claude`, `codex`). When absent, the runner SHALL default to `claude`. The `cli` field is only valid on agent steps, not shell steps.

#### Scenario: CLI specified on agent step
- **WHEN** an agent step has `cli: codex`
- **THEN** the runner uses the Codex adapter for that step

#### Scenario: CLI not specified
- **WHEN** an agent step has no `cli` field
- **THEN** the runner uses the Claude adapter (hard-coded default)

#### Scenario: CLI on shell step
- **WHEN** a shell step has a `cli` field
- **THEN** the runner fails with a validation error
