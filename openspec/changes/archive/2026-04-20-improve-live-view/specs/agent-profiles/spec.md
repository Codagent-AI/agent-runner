## MODIFIED Requirements

### Requirement: Config file auto-generation
When `.agent-runner/config.yaml` does not exist, the runner SHALL generate it with five default profiles:
- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `headless_base`: default_mode=headless, cli=claude, model=opus, effort=high
- `planner`: extends interactive_base (no overrides)
- `implementor`: extends headless_base (no overrides)
- `summarizer`: default_mode=headless, cli=claude, model=haiku, effort=low

#### Scenario: Config file missing on startup
- **WHEN** the runner starts and `.agent-runner/config.yaml` does not exist
- **THEN** the runner creates the file with the five default profiles and proceeds normally

#### Scenario: Config file already exists
- **WHEN** the runner starts and `.agent-runner/config.yaml` exists
- **THEN** the runner loads and uses it as-is without modifying it

#### Scenario: Summarizer profile resolves to claude + haiku
- **WHEN** a workflow step references `agent: summarizer` and the generated config is unchanged
- **THEN** the resolved profile has default_mode=headless, cli=claude, model=haiku, effort=low
