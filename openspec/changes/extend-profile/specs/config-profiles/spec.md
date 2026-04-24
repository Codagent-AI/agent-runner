## ADDED Requirements

### Requirement: Profile set extends field
A profile set MAY include an optional top-level `extends: <parent_profile_set_name>` field that names another profile set to inherit agents from. `extends` SHALL be a single string naming exactly one parent. The parent name SHALL reference a profile set that exists in the merged (global + project + defaults) profile set map.

The `extends` field MAY appear in either the global config (`~/.agent-runner/config.yaml`) or the project config (`.agent-runner/config.yaml`). When both files declare `extends` on a profile set of the same name, the project file's value SHALL win (matching the existing project-over-global precedence rule).

#### Scenario: Child profile set inherits parent's agents
- **WHEN** the global file defines profile set `team_base` with agents `{ headless_base, planner }` and the project file defines `copilot` with `extends: team_base` and agents `{ implementor }`
- **THEN** the effective `copilot` profile set contains `headless_base`, `planner`, and `implementor`

#### Scenario: Child profile set overrides a parent agent by name
- **WHEN** the parent profile set defines `implementor` with `cli: claude` and `model: opus`, and the child profile set (via `extends`) defines `implementor` with `extends: headless_base` and `cli: copilot`
- **THEN** the effective child `implementor` is exactly the child's version (`extends: headless_base`, `cli: copilot`); no fields inherit from the parent set's `implementor`

#### Scenario: Extends references a profile set that does not exist
- **WHEN** a profile set declares `extends: missing` and no profile set named `missing` exists in the merged config
- **THEN** config loading fails with an error identifying the profile set and the missing parent name

#### Scenario: Extends value is not a string
- **WHEN** a profile set declares `extends: [a, b]` or any non-string value
- **THEN** config loading fails with an error indicating `extends` must be a single profile set name

#### Scenario: Both files declare extends on the same profile set
- **WHEN** the global file declares `profiles.copilot.extends: base_a` and the project file declares `profiles.copilot.extends: base_b`, and both `base_a` and `base_b` exist
- **THEN** the effective `copilot` profile set uses `base_b` (the project value) as its parent

### Requirement: Profile set extends resolution order
The runner SHALL resolve profile set `extends` after merging profile sets across files by name, and before resolving agent-level `extends`. The resolution order for each profile set name is:

1. Merge agents maps across files for profile sets of that name (existing rule: same-name agents are replaced wholesale by the project file).
2. Merge the `extends` field for that profile set name (project value wins if set, else global, else defaults, else absent).
3. Walk the `extends` chain from the profile set toward its ancestors, composing effective agents maps such that each child's agents override the parent's agents by name (wholesale; no field-level merge).
4. Run agent-level validation (base completeness, allowed field values, agent-level cycle detection) against each profile set's effective agents map.

#### Scenario: Merge-then-extend across global and project
- **WHEN** the global file contributes `team_base.agents: { headless_base }` and `copilot.agents: { planner }`, and the project file contributes `copilot.extends: team_base` and `copilot.agents: { implementor }`
- **THEN** the effective `copilot` profile set contains `headless_base` (from `team_base`), `planner` (from global `copilot`), and `implementor` (from project `copilot`)

#### Scenario: Multi-level profile set chain
- **WHEN** profile set `a` extends `b`, and profile set `b` extends `c`; `c` defines `{ headless_base }`, `b` defines `{ planner }`, `a` defines `{ implementor }`
- **THEN** the effective `a` contains `headless_base`, `planner`, and `implementor`

#### Scenario: Validation runs against effective agents
- **WHEN** a non-active profile set inherits (via `extends`) an agent whose `effort` is invalid
- **THEN** config loading fails with a validation error, even though the invalid agent was pulled in from a parent set and the containing set is not active (validation runs over every profile set's effective agents map)

### Requirement: Profile set extends cycle detection
The runner SHALL detect cycles in the profile set `extends` chain and SHALL reject configurations in which a profile set's ancestry, including self-references, forms a cycle.

#### Scenario: Direct cycle
- **WHEN** profile set `a` declares `extends: b` and profile set `b` declares `extends: a`
- **THEN** config loading fails with an error indicating a cycle in the profile set extends chain and naming both sets

#### Scenario: Self-reference
- **WHEN** profile set `a` declares `extends: a`
- **THEN** config loading fails with an error indicating a cycle in the profile set extends chain

#### Scenario: Indirect cycle through chain
- **WHEN** profile set `a` extends `b`, `b` extends `c`, and `c` extends `a`
- **THEN** config loading fails with an error indicating a cycle in the profile set extends chain

### Requirement: Agent-level extends spans inherited agents
After profile set `extends` resolution, an agent's `extends` field SHALL resolve against the containing profile set's effective agents map. Inherited agents (pulled in from a parent profile set via profile-set `extends`) are eligible parents for agent-level `extends`.

#### Scenario: Child agent extends an agent inherited from a parent set
- **WHEN** the project's `copilot` profile set declares `extends: team_base`, and `team_base` defines `headless_base`, and `copilot` defines `implementor` with `extends: headless_base`
- **THEN** resolving `implementor` in the active `copilot` profile succeeds and inherits fields from `team_base`'s `headless_base`

#### Scenario: Agent-level extends still cannot cross unrelated profile sets
- **WHEN** the active profile set is `copilot` (with no `extends`), and an agent in `copilot` declares `extends: planner`, where `planner` is defined only in a different, unrelated profile set
- **THEN** config loading fails with an error indicating the parent agent does not exist in the containing profile set's effective agents map
