## MODIFIED Requirements

### Requirement: Prompt enrichment

For steps whose ID matches an engine-managed artifact, baton SHALL call the engine's `enrichPrompt` (if implemented) to obtain engine-provided context. The enrichment SHALL be kept separate from the step prompt and delivered according to the system prompt routing rules: via native system prompt for supporting adapters in interactive mode, wrapped in `<system>` XML tags for non-supporting adapters in interactive mode, or concatenated into the positional argument for headless mode. The engine determines which step IDs it manages.

#### Scenario: Step ID matches an engine artifact
- **WHEN** a step's ID matches an engine-managed artifact and the engine implements `enrichPrompt`
- **THEN** baton calls `enrichPrompt` and delivers the result separately from the step prompt via system prompt routing

#### Scenario: Step ID does not match any engine artifact
- **WHEN** a step's ID does not match any engine-managed artifact
- **THEN** baton uses the step's prompt as-is, without calling `enrichPrompt`

#### Scenario: Engine does not implement enrichPrompt
- **WHEN** the engine does not implement `enrichPrompt`
- **THEN** baton uses the step's prompt as-is
