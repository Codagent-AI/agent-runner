## MODIFIED Requirements

### Requirement: Profile resolution
The runner SHALL resolve an agent name to a fully-merged agent definition by walking the `extends` chain within the active profile set's effective `agents:` map (that is, the map produced after profile-set `extends` resolution — see the `config-profiles` capability) and merging fields (child overrides parent). The resolved agent provides default_mode, cli, and optionally model, effort, and system_prompt to the executor. Agent-level `extends` references SHALL be resolved against that same effective agents map, which may include agents inherited from a parent profile set. Agent-level `extends` SHALL NOT cross into unrelated profile sets — only agents visible in the containing set's effective map (whether defined locally or inherited via profile-set `extends`) are reachable.

#### Scenario: Effort unset after full merge
- **WHEN** an agent is resolved and `effort` is unset in both the child and all ancestors
- **THEN** the runner does not pass an effort parameter to the CLI adapter

#### Scenario: System prompt set in resolved agent
- **WHEN** an agent is resolved and `system_prompt` is set
- **THEN** the runner prepends it to the fullPrompt string (before the step prompt and engine enrichment), which is then routed through the existing delivery mechanism unchanged

#### Scenario: System prompt combined with engine enrichment
- **WHEN** an agent has `system_prompt` set and the engine provides enrichment for the step
- **THEN** the full prompt is ordered as: [agent system_prompt] [step prompt] [engine enrichment]

#### Scenario: Agent lookup failure on resume
- **WHEN** a resume or inherit step attempts to resolve its inherited agent and no session-originating agent is found (e.g., no prior agentic step in the session chain)
- **THEN** the runner SHALL treat the step as failed with an error indicating no agent could be resolved

#### Scenario: Multi-level inheritance within active profile
- **WHEN** in the active profile set, agent C extends B which extends A, and C sets effort, B sets model, A sets default_mode and cli
- **THEN** the resolved agent has A's default_mode and cli, B's model, and C's effort

#### Scenario: Agent-level extends reaches an inherited agent
- **WHEN** the active profile set `copilot` declares `extends: team_base`, `team_base` defines `headless_base`, and `copilot` defines `implementor` with `extends: headless_base`
- **THEN** resolving `implementor` succeeds and inherits fields from `team_base`'s `headless_base`

#### Scenario: Agent-level extends cannot reach an unrelated profile set
- **WHEN** the active profile set `copilot` does not declare `extends`, and an agent in `copilot` specifies `extends: planner` where `planner` is defined only in an unrelated profile set
- **THEN** config loading fails with an error indicating the parent agent does not exist in the active profile's effective agents map
