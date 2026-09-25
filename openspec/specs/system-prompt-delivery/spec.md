# system-prompt-delivery Specification

## Purpose
Define how agent prompts and engine enrichment are routed through native and fallback system-prompt mechanisms.

## Requirements

### Requirement: System prompt routing

For interactive mode with enrichment, a fresh session on an adapter with native system-prompt support SHALL receive the full step prompt and enrichment through the adapter's system-prompt mechanism, with a short positional prompt. On every resumed session, including adapters with native system-prompt support, the runner SHALL pass the full current step prompt and completion instruction in the positional user message and leave the adapter system prompt empty. When enrichment or a profile system prompt is present on a resumed session or an adapter without native system-prompt support, the runner SHALL wrap the full prompt in `<system>` XML tags. Headless mode SHALL continue concatenating prompt and enrichment into the positional argument without wrapping.

#### Scenario: Fresh session with native system-prompt support
- **WHEN** executing an interactive step in a fresh session and the adapter declares system-prompt support
- **THEN** the runner passes the full step instructions through the adapter's system-prompt mechanism and uses a short positional prompt

#### Scenario: Resumed session with native system-prompt support
- **WHEN** executing an interactive step in an existing session, including a named session or resumed workflow
- **THEN** the runner leaves the adapter system prompt empty and passes the full current step instructions and completion command in the positional user message

#### Scenario: Adapter does not support system prompt (interactive with enrichment)
- **WHEN** executing an interactive step with enrichment and the adapter does not support system prompts
- **THEN** the runner wraps enrichment in `<system>` XML tags, prepends it to the step prompt, and passes the combined text as the positional argument

#### Scenario: Headless mode bypasses routing
- **WHEN** executing a headless step regardless of adapter support
- **THEN** prompt and enrichment are concatenated and passed as the positional argument without wrapping (current behavior)

#### Scenario: No enrichment on an adapter without native system-prompt support
- **WHEN** no engine enrichment or profile system prompt is present for an interactive step on an adapter without native system-prompt support
- **THEN** the full step prompt is passed as the positional argument without XML wrapping

#### Scenario: No step prompt
- **WHEN** a step has no prompt
- **THEN** the runner returns a failed outcome (current behavior, unchanged)
