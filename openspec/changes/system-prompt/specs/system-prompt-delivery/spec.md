## ADDED Requirements

### Requirement: System prompt routing

For interactive mode, the runner SHALL deliver the full prompt content (step prompt and engine enrichment, concatenated) as a system prompt rather than a user-visible message. When the adapter supports native system prompts, the runner SHALL pass the full content via the adapter's system prompt mechanism. When the adapter does not support native system prompts, the runner SHALL wrap the full content in `<system>` XML tags and pass it as the positional argument. Headless mode SHALL continue using the positional argument with no wrapping (current behavior).

#### Scenario: Adapter supports system prompt (interactive)
- **WHEN** executing an interactive step and the adapter declares system prompt support
- **THEN** the runner passes the full prompt content (step prompt + enrichment) via the adapter's system prompt mechanism, with no positional argument

#### Scenario: Adapter does not support system prompt (interactive)
- **WHEN** executing an interactive step and the adapter does not support system prompts
- **THEN** the runner wraps the full prompt content in `<system>` XML tags and passes it as the positional argument

#### Scenario: Headless mode bypasses routing
- **WHEN** executing a headless step regardless of adapter support
- **THEN** the full prompt content is passed as the positional argument without wrapping (current behavior)

#### Scenario: No enrichment
- **WHEN** no engine enrichment is returned for a step
- **THEN** the step prompt alone is subject to the same routing rules

#### Scenario: No step prompt
- **WHEN** a step has no prompt
- **THEN** the runner skips the step (current behavior, unchanged)
