## ADDED Requirements

### Requirement: Adapter system prompt capability

Each adapter SHALL declare whether it supports native system prompt delivery. The Claude adapter SHALL declare support. The Codex adapter SHALL declare no support.

Adapters declare support via a `SupportsSystemPrompt() bool` method on the `Adapter` interface. The caller queries this before constructing `BuildArgsInput`, setting the `SystemPrompt` field only when the adapter supports it.

#### Scenario: Claude adapter declares support
- **WHEN** the runner queries the Claude adapter for system prompt capability
- **THEN** the adapter indicates it supports native system prompts

#### Scenario: Codex adapter declares no support
- **WHEN** the runner queries the Codex adapter for system prompt capability
- **THEN** the adapter indicates it does not support native system prompts

## MODIFIED Requirements

### Requirement: Adapter arg construction

Each adapter SHALL construct the CLI invocation args for both headless and interactive modes. The adapter receives the prompt, system prompt content (if applicable), session ID (if resuming), and model override (if specified), and returns the full command and args. When system prompt content is provided and the adapter supports it, the adapter SHALL include the appropriate CLI flags to deliver it as a system prompt (e.g., `--append-system-prompt` for Claude).

#### Scenario: Headless invocation with model override
- **WHEN** the runner executes a headless step with `model: sonnet` and a session ID from state
- **THEN** the adapter returns args that include the prompt, model flag, session resume flag, and headless flag appropriate to that CLI

#### Scenario: Interactive invocation with no session
- **WHEN** the runner executes an interactive step with session strategy `new`
- **THEN** the adapter returns args for a fresh interactive session (no resume flag)

#### Scenario: Interactive invocation with system prompt (Claude)
- **WHEN** the runner provides system prompt content to the Claude adapter for an interactive step
- **THEN** the adapter includes `--append-system-prompt` with the content in the args

#### Scenario: System prompt content provided to unsupporting adapter
- **WHEN** the runner provides system prompt content to an adapter that does not support it
- **THEN** the adapter ignores the system prompt field (the runner handles fallback wrapping)
