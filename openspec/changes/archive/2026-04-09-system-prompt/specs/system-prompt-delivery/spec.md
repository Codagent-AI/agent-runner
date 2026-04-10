## ADDED Requirements

### Requirement: System prompt routing

For interactive mode with enrichment, the runner SHALL deliver only the enrichment as a system prompt, keeping the step prompt as a positional argument. When the adapter supports native system prompts, the runner SHALL pass enrichment via the adapter's system prompt mechanism and the step prompt as the positional argument. When the adapter does not support native system prompts, the runner SHALL wrap enrichment in `<system>` XML tags, prepend it to the step prompt, and pass the combined text as the positional argument. Headless mode SHALL continue concatenating prompt and enrichment into the positional argument with no wrapping (current behavior).

#### Scenario: Adapter supports system prompt (interactive with enrichment)
- **WHEN** executing an interactive step with enrichment and the adapter declares system prompt support
- **THEN** the runner passes enrichment via the adapter's system prompt mechanism and the step prompt as the positional argument

#### Scenario: Adapter does not support system prompt (interactive with enrichment)
- **WHEN** executing an interactive step with enrichment and the adapter does not support system prompts
- **THEN** the runner wraps enrichment in `<system>` XML tags, prepends it to the step prompt, and passes the combined text as the positional argument

#### Scenario: Headless mode bypasses routing
- **WHEN** executing a headless step regardless of adapter support
- **THEN** prompt and enrichment are concatenated and passed as the positional argument without wrapping (current behavior)

#### Scenario: No enrichment (interactive)
- **WHEN** no engine enrichment is returned for an interactive step
- **THEN** the step prompt is passed as the positional argument with no system prompt routing

#### Scenario: No step prompt
- **WHEN** a step has no prompt
- **THEN** the runner returns a failed outcome (current behavior, unchanged)
