## MODIFIED Requirements

### Requirement: Adapter arg construction
Each adapter SHALL construct the CLI invocation args for both headless and interactive modes. The adapter receives the prompt, system prompt content (if applicable), session ID (if resuming), model override (if specified), effort level (if specified), and returns the full command and args. When effort is provided, the adapter SHALL include the appropriate CLI flag for the effort level. When effort is empty, no effort flag is emitted. When system prompt content is provided and the adapter supports it, the adapter SHALL include the appropriate CLI flags to deliver it as a system prompt (e.g., `--append-system-prompt` for Claude).

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

#### Scenario: Effort level specified
- **WHEN** the runner provides effort level "high" to an adapter
- **THEN** the adapter includes the CLI-appropriate effort flag in the args

#### Scenario: Effort level not specified
- **WHEN** the runner provides no effort level (empty string)
- **THEN** the adapter does not include any effort flag in the args
