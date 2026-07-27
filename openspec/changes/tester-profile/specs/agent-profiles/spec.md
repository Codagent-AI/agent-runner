## MODIFIED Requirements

### Requirement: Built-in default profile set

The runner SHALL provide an in-memory default profile set named `default` as the bottom layer of config resolution. The default set SHALL contain seven agents:

- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `autonomous_base`: default_mode=autonomous, cli=claude, model=opus, effort=high
- `planner`: extends interactive_base with no overrides
- `reviewer`: extends autonomous_base with no overrides
- `implementor`: extends autonomous_base with no overrides
- `tester`: extends autonomous_base and overrides model=sonnet
- `summarizer`: default_mode=autonomous, cli=claude, model=haiku, effort=low

The runner SHALL NOT create `.agent-runner/config.yaml` or any other config file automatically. The defaults SHALL exist only as an in-memory layer beneath global and project configuration that the user has chosen to create.

#### Scenario: Project config missing on startup
- **WHEN** the runner starts and `.agent-runner/config.yaml` does not exist
- **THEN** the runner uses all seven built-in agents in memory and SHALL NOT create the file or its parent directory

#### Scenario: Project config already exists
- **WHEN** the runner starts and `.agent-runner/config.yaml` exists
- **THEN** the runner loads and uses it without modifying it

#### Scenario: Reviewer resolves to autonomous flagship fallback
- **WHEN** a workflow step references `agent: reviewer` with no global or project override and no step mode override
- **THEN** the resolved reviewer has default_mode=autonomous, cli=claude, model=opus, and effort=high

#### Scenario: Explicit reviewer mode override still wins
- **WHEN** a workflow step references the built-in reviewer and specifies `mode: interactive`
- **THEN** the runner executes that step interactively while retaining the reviewer's resolved CLI, model, and effort

#### Scenario: Tester resolves to autonomous balanced fallback
- **WHEN** a workflow step or agent call references `agent: tester` with no global or project override
- **THEN** the resolved tester has default_mode=autonomous, cli=claude, model=sonnet, and effort=high

#### Scenario: Summarizer agent resolves to Claude Haiku
- **WHEN** a workflow step references `agent: summarizer` with no global or project override
- **THEN** the resolved summarizer has default_mode=autonomous, cli=claude, model=haiku, and effort=low
