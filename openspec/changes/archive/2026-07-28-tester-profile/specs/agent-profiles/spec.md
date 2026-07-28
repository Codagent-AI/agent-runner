## ADDED Requirements

### Requirement: Legacy planning-role aliases

The runner SHALL treat `planner` as a deprecated alias for `lead` and `reviewer` as a deprecated alias for `crosscheck` wherever agent profile names are accepted, including profile entries, `extends` targets, workflow agent references, and runtime agent-call targets. Each global or project configuration layer SHALL be canonicalized before normal layer precedence is applied, so a legacy override remains effective for a canonical built-in workflow and a higher-layer alias overrides a lower-layer synonym.

Using either legacy alias in the active profile-set inheritance chain or explicitly referencing the alias SHALL surface a non-fatal, user-visible deprecation warning that names its canonical replacement and SHALL remain supported without a scheduled removal date. An alias declared only in an inactive, uninherited profile set SHALL NOT emit a warning solely because configuration is loaded. Each distinct alias warning SHALL appear at most once per command or workflow run. Loading legacy configuration SHALL NOT rewrite its source file.

A single profile layer that declares both `lead` and `planner`, or both `crosscheck` and `reviewer`, SHALL fail validation with an actionable ambiguity error. Native setup migration after explicit overwrite confirmation is governed by the agent-profile-editor capability.

#### Scenario: Legacy config override serves canonical workflow
- **WHEN** a user config layer defines `planner` with a custom CLI and a workflow references `agent: lead`
- **THEN** the runner resolves Lead from the legacy override
- **AND** emits a deprecation warning directing the user to `lead`

#### Scenario: Legacy workflow reference serves canonical config
- **WHEN** a config layer defines `lead` and a workflow or agent call references `agent: planner`
- **THEN** the runner resolves the canonical Lead profile
- **AND** emits a deprecation warning directing the user to `lead`

#### Scenario: Legacy extends target is canonicalized
- **WHEN** an agent declares `extends: reviewer` and the profile layer provides canonical `crosscheck`
- **THEN** the runner resolves the inheritance through Crosscheck
- **AND** emits a deprecation warning directing the user to `crosscheck`

#### Scenario: Higher-layer synonym wins
- **WHEN** global config defines `planner` and project config defines `lead`
- **THEN** the project Lead replaces the canonicalized global Lead under normal layer precedence

#### Scenario: Alias warning is deduplicated
- **WHEN** one command or workflow run resolves `planner` multiple times
- **THEN** it emits one `planner`-to-`lead` deprecation warning

#### Scenario: Inactive profile alias does not warn
- **WHEN** configuration declares `planner` only in a profile set outside the active profile-set inheritance chain
- **AND** the command or workflow does not explicitly reference `planner`
- **THEN** the runner canonicalizes the inactive declaration without emitting its deprecation warning

#### Scenario: Same-layer alias pair is rejected
- **WHEN** one profile layer defines both `crosscheck` and `reviewer`
- **THEN** config validation fails with an error naming both conflicting aliases and directing the user to keep `crosscheck`

#### Scenario: Legacy file is not rewritten on load
- **WHEN** the runner loads a valid config file containing only legacy planning-role aliases
- **THEN** it leaves the file contents unchanged

## MODIFIED Requirements

### Requirement: Built-in default profile set

The runner SHALL provide an in-memory default profile set named `default` as the bottom layer of config resolution. The default set SHALL contain seven agents:

- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `autonomous_base`: default_mode=autonomous, cli=claude, model=opus, effort=high
- `lead`: extends interactive_base with no overrides
- `crosscheck`: extends autonomous_base with no overrides
- `implementor`: extends autonomous_base with no overrides
- `tester`: default_mode=autonomous, cli=claude, model=sonnet, effort=high
- `summarizer`: default_mode=autonomous, cli=claude, model=haiku, effort=low

The runner SHALL NOT create `.agent-runner/config.yaml` or any other config file automatically. The defaults SHALL exist only as an in-memory layer beneath global and project configuration that the user has chosen to create.

#### Scenario: Project config missing on startup
- **WHEN** the runner starts and `.agent-runner/config.yaml` does not exist
- **THEN** the runner uses all seven built-in agents in memory and SHALL NOT create the file or its parent directory

#### Scenario: Project config already exists
- **WHEN** the runner starts and `.agent-runner/config.yaml` exists
- **THEN** the runner loads and uses it without modifying it

#### Scenario: Crosscheck resolves to autonomous flagship fallback
- **WHEN** a workflow step references `agent: crosscheck` with no global or project override and no step mode override
- **THEN** the resolved crosscheck has default_mode=autonomous, cli=claude, model=opus, and effort=high

#### Scenario: Legacy reviewer resolves to Crosscheck fallback
- **WHEN** a workflow step references `agent: reviewer` with no global or project override and no step mode override
- **THEN** the resolved agent has default_mode=autonomous, cli=claude, model=opus, and effort=high
- **AND** the runner emits the `reviewer`-to-`crosscheck` deprecation warning

#### Scenario: Explicit crosscheck mode override still wins
- **WHEN** a workflow step references the built-in crosscheck and specifies `mode: interactive`
- **THEN** the runner executes that step interactively while retaining the crosscheck's resolved CLI, model, and effort

#### Scenario: Tester resolves to autonomous balanced fallback
- **WHEN** a workflow step or agent call references `agent: tester` with no global or project override
- **THEN** the resolved tester has default_mode=autonomous, cli=claude, model=sonnet, and effort=high

#### Scenario: Summarizer agent resolves to Claude Haiku
- **WHEN** a workflow step references `agent: summarizer` with no global or project override
- **THEN** the resolved summarizer has default_mode=autonomous, cli=claude, model=haiku, and effort=low
