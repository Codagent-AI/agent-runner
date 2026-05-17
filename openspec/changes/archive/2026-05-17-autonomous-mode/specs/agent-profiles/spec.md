## MODIFIED Requirements

### Requirement: Profile schema
Each agent profile SHALL have a name (the YAML key) and MAY include: `default_mode` (interactive|autonomous), `cli` (claude|codex), `model` (string), `effort` (low|medium|high), `system_prompt` (string), and `extends` (string referencing another profile name).

#### Scenario: All fields specified
- **WHEN** a profile specifies default_mode, cli, model, effort, system_prompt
- **THEN** the runner loads all values from the profile

#### Scenario: Optional fields omitted
- **WHEN** a profile omits model, effort, or system_prompt
- **THEN** the runner treats those fields as unset and does not pass them to the CLI adapter

#### Scenario: Unrecognized field
- **WHEN** a profile specifies a field not in the schema
- **THEN** the runner ignores it without error

#### Scenario: Invalid effort value
- **WHEN** a profile specifies an effort value not in (low, medium, high)
- **THEN** config loading SHALL fail with a validation error indicating the invalid effort value

#### Scenario: Invalid default_mode value
- **WHEN** a profile specifies a default_mode value not in (interactive, autonomous)
- **THEN** config loading SHALL fail with a validation error indicating the invalid default_mode value

### Requirement: Built-in default profile set
The runner SHALL provide an in-memory default profile set named `default` as the bottom layer of config resolution. The default set contains five agents:
- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `autonomous_base`: default_mode=autonomous, cli=claude, model=opus, effort=high
- `planner`: extends interactive_base (no overrides)
- `implementor`: extends autonomous_base (no overrides)
- `summarizer`: default_mode=autonomous, cli=claude, model=haiku, effort=low

The runner SHALL NOT create `.agent-runner/config.yaml` (or any config file) automatically. The defaults exist only as an in-memory layer beneath any global and project configs the user has chosen to create.

#### Scenario: Project config missing on startup
- **WHEN** the runner starts and `.agent-runner/config.yaml` does not exist
- **THEN** the runner uses the built-in defaults in memory and SHALL NOT create the file or its parent directory

#### Scenario: Project config already exists
- **WHEN** the runner starts and `.agent-runner/config.yaml` exists
- **THEN** the runner loads and uses it as-is without modifying it

#### Scenario: Summarizer agent resolves to claude + haiku
- **WHEN** a workflow step references `agent: summarizer` with no project or global overrides (so the active profile is `default`)
- **THEN** the resolved agent has default_mode=autonomous, cli=claude, model=haiku, effort=low

### Requirement: Step mode override
An agent step MAY include a `mode` field (interactive|autonomous) to override the resolved profile's `default_mode` for that step. When omitted, the runner SHALL use the profile's `default_mode`.

#### Scenario: Mode override on resume step
- **WHEN** an agent step has `session: resume` and `mode: autonomous`, and the inherited profile has `default_mode: interactive`
- **THEN** the runner executes the step in autonomous mode

#### Scenario: No mode override
- **WHEN** an agent step does not specify `mode`
- **THEN** the runner uses the resolved profile's `default_mode`

#### Scenario: Mode override on new session step
- **WHEN** an agent step has `session: new`, `agent: interactive_base`, and `mode: autonomous`
- **THEN** the runner executes the step in autonomous mode, overriding the profile's default
