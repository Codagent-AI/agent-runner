## MODIFIED Requirements

### Requirement: CLI adapter registry

The runner SHALL maintain a hard-coded registry of known CLI adapters. Each adapter SHALL be identified by a string key (e.g., `claude`, `codex`, `copilot`). The registry is compile-time — adding a new CLI requires a code change.

#### Scenario: Known CLI resolved
- **WHEN** a step specifies `cli: claude`
- **THEN** the runner resolves the Claude adapter from the registry

#### Scenario: Copilot CLI resolved
- **WHEN** a step specifies `cli: copilot`
- **THEN** the runner resolves the Copilot adapter from the registry

#### Scenario: Unknown CLI requested
- **WHEN** a step specifies a `cli` value not in the registry
- **THEN** the runner fails at load time with a validation error indicating the CLI is not supported

### Requirement: Adapter system prompt capability

Each adapter SHALL declare whether it supports native system prompt delivery. The Claude adapter SHALL declare support. The Codex adapter SHALL declare no support. The Copilot adapter SHALL declare no support.

Adapters declare support via a `SupportsSystemPrompt() bool` method on the `Adapter` interface. The caller queries this before constructing `BuildArgsInput`, setting the `SystemPrompt` field only when the adapter supports it.

#### Scenario: Claude adapter declares support
- **WHEN** the runner queries the Claude adapter for system prompt capability
- **THEN** the adapter indicates it supports native system prompts

#### Scenario: Codex adapter declares no support
- **WHEN** the runner queries the Codex adapter for system prompt capability
- **THEN** the adapter indicates it does not support native system prompts

#### Scenario: Copilot adapter declares no support
- **WHEN** the runner queries the Copilot adapter for system prompt capability
- **THEN** the adapter indicates it does not support native system prompts
