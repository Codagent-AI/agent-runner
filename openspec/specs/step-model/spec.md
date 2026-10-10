# Capability: step-model

## Purpose

Defines per-step model and CLI overrides for agent steps.
## Requirements
### Requirement: Per-step model override
A step MAY include a `model` field specifying which model the agent should use. When present, the runner SHALL pass the model to the CLI adapter, overriding the model from the resolved agent profile. When absent, the profile's model is used (which may itself be unset, in which case no model is passed to the CLI). The `model` field is only valid on agent steps, not shell steps.

#### Scenario: Model specified overrides profile
- **WHEN** an agent step has `agent: autonomous_base` (profile model=opus) and `model: sonnet`
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
A step MAY include a `cli` field specifying which CLI backend to use. When present, it SHALL override the cli from the resolved agent profile. When absent, the profile's cli is used. If both the step and the resolved profile omit `cli`, the runner SHALL fall back to `claude`. The `cli` field is only valid on agent steps, not shell steps.

#### Scenario: CLI specified overrides profile
- **WHEN** an agent step has `agent: autonomous_base` (profile cli=claude) and `cli: codex`
- **THEN** the runner uses the Codex adapter for that step

#### Scenario: CLI not specified, uses profile
- **WHEN** an agent step has no `cli` field and the resolved profile has cli=claude
- **THEN** the runner uses the Claude adapter

#### Scenario: CLI on shell step
- **WHEN** a shell step has a `cli` field
- **THEN** the runner fails with a validation error

### Requirement: `model` field rejected on UI steps

The `model` field SHALL NOT be valid on `mode: ui` steps. UI steps are not agent steps; they have no model concept. Validation SHALL fail at workflow-load time when a UI step sets `model`.

#### Scenario: UI step with model field
- **WHEN** a step has `mode: ui` and sets `model: opus`
- **THEN** validation fails with an error indicating that `model` is not valid on UI steps

### Requirement: `cli` field rejected on UI steps

The `cli` field SHALL NOT be valid on `mode: ui` steps. UI steps are not agent steps; they have no CLI adapter. Validation SHALL fail at workflow-load time when a UI step sets `cli`.

#### Scenario: UI step with cli field
- **WHEN** a step has `mode: ui` and sets `cli: claude`
- **THEN** validation fails with an error indicating that `cli` is not valid on UI steps

### Requirement: `model` field rejected on script steps

The `model` field SHALL NOT be valid on `script:` steps. Script steps are not agent steps; the model concept does not apply to a bundled script. Validation SHALL fail at workflow-load time when a script step sets `model`.

#### Scenario: Script step with model field
- **WHEN** a step declares `script: detect.sh` and sets `model: opus`
- **THEN** validation fails with an error indicating that `model` is not valid on script steps

### Requirement: `cli` field rejected on script steps

The `cli` field SHALL NOT be valid on `script:` steps. Script steps are not agent steps; they do not invoke a CLI adapter. Validation SHALL fail at workflow-load time when a script step sets `cli`.

#### Scenario: Script step with cli field
- **WHEN** a step declares `script: detect.sh` and sets `cli: codex`
- **THEN** validation fails with an error indicating that `cli` is not valid on script steps

### Requirement: Static Runner-owned tools on agent steps

An agent step MAY declare a static YAML sequence named `tools`. The supported entries SHALL be the
exact strings `call_agent` and `submit_route`. An omitted `tools` field or an explicitly empty
sequence on an agent step SHALL enable no Runner-owned tools. Tool entries MUST NOT be interpolated.
Unknown names, duplicate names, and scalar declarations MUST fail workflow loading with an error that
identifies the invalid `tools` declaration.

`submit_route` SHALL be reserved for the single `plan` step of the built-in `core:intake` workflow
loaded at top level. A workflow file whose top-level steps declare `submit_route` MUST fail loading
when it is any other workflow, when it is loaded as a sub-workflow, or when it is not exactly that
one-step intake shape.

#### Scenario: Agent step declares call_agent
- **WHEN** an agent step declares `tools: [call_agent]`
- **THEN** the loaded step records `call_agent` as its enabled Runner-owned tool

#### Scenario: Agent step omits tools
- **WHEN** an agent step has no `tools` field
- **THEN** the loaded step has no enabled Runner-owned tools

#### Scenario: Agent step declares an empty sequence
- **WHEN** an agent step declares `tools: []`
- **THEN** the loaded step has no enabled Runner-owned tools

#### Scenario: Unknown tool is rejected
- **WHEN** an agent step declares a tool name other than the exact supported values `call_agent` and `submit_route`
- **THEN** workflow loading fails with an error identifying the unknown tool

#### Scenario: Duplicate tool is rejected
- **WHEN** an agent step declares `tools: [call_agent, call_agent]`
- **THEN** workflow loading fails with an error identifying the duplicate tool

#### Scenario: Scalar tool declaration is rejected
- **WHEN** an agent step declares `tools: call_agent`
- **THEN** workflow loading fails because `tools` must be a sequence

#### Scenario: Tool declaration is not interpolated
- **WHEN** an agent step declares a placeholder or other non-literal value in `tools`
- **THEN** workflow loading rejects it as an unknown tool rather than resolving workflow parameters

#### Scenario: Route submission outside intake is rejected
- **WHEN** a workflow file other than the built-in `core:intake` declares `tools: [submit_route]` on a top-level agent step
- **THEN** workflow loading fails with an error stating that `submit_route` is reserved for the top-level built-in `core:intake` plan step

### Requirement: Tools field is agent-only

The `tools` field SHALL be valid only on an agent step, meaning a step with a `prompt` or an `agent`
profile. Any explicit `tools` field on a shell, script, UI, loop, group, or sub-workflow step MUST
fail workflow loading, including when its sequence is empty. The same constraint SHALL apply to steps
nested inside loops and groups.

#### Scenario: Non-agent step declares call_agent
- **WHEN** a non-agent step declares `tools: [call_agent]`
- **THEN** workflow loading fails with an error stating that `tools` is only allowed on agent steps

#### Scenario: Non-agent step declares an empty sequence
- **WHEN** a non-agent step explicitly declares `tools: []`
- **THEN** workflow loading fails with an error stating that `tools` is only allowed on agent steps

#### Scenario: Nested non-agent declaration is rejected
- **WHEN** a loop or group contains a non-agent child with an explicit `tools` field
- **THEN** recursive step validation rejects that child declaration

