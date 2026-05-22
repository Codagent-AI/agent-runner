# config-profiles Specification

## Purpose
TBD - created by archiving change multi-profile. Update Purpose after archive.
## Requirements
### Requirement: Top-level profile set schema
The config file's top-level `profiles:` key SHALL be a map of named profile sets. Each profile set SHALL be an object with an `agents:` key whose value is a map of agent names to agent definitions (as specified in the `agent-profiles` capability). A profile set MAY be empty (no `agents:` key or an empty map) but MUST be a mapping, not a scalar.

#### Scenario: Profile set with agents
- **WHEN** the config file contains `profiles: { default: { agents: { planner: {...}, implementor: {...} } } }`
- **THEN** config loading succeeds and the `default` profile set exposes `planner` and `implementor` as resolvable agents

#### Scenario: Profile set with empty agents map
- **WHEN** a profile set contains `agents: {}` or omits `agents:` entirely
- **THEN** config loading succeeds and the profile set has no agents (any lookup against it fails with a not-found error at resolve time)

#### Scenario: Profile set value is a scalar
- **WHEN** a profile set's value is a string, number, or list rather than a mapping
- **THEN** config loading fails with an error indicating the profile set must be a mapping

### Requirement: Legacy flat shape rejection
When the runner loads a config file whose top-level `profiles:` map contains entries that look like agent bundles (i.e., values have any of `default_mode`, `cli`, `model`, `effort`, `system_prompt`, or `extends` as direct keys, instead of an `agents:` key), the runner SHALL fail with an error that identifies the file path and instructs the user to restructure the file under `profiles.<name>.agents.<agent_name>`.

#### Scenario: Project config uses legacy flat shape
- **WHEN** `.agent-runner/config.yaml` contains `profiles: { planner: { extends: interactive_base } }` (i.e., agent bundles at the top level)
- **THEN** config loading fails with an error naming the file and instructing the user to move the entries under `profiles.default.agents`

#### Scenario: Global config uses legacy flat shape
- **WHEN** `~/.agent-runner/config.yaml` contains `profiles: { autonomous_base: { default_mode: autonomous, cli: claude } }`
- **THEN** config loading fails with an error naming the global file and instructing the user to restructure

#### Scenario: Mixed shape within a single file
- **WHEN** `profiles:` contains one entry that looks like a profile set (has `agents:`) and another entry that looks like a legacy agent bundle (has `default_mode` or `cli` at its top)
- **THEN** config loading fails with an error identifying the offending legacy entry

### Requirement: Active profile selection
A project config MAY include a top-level `active_profile: <name>` field that selects which profile set is used for agent resolution during workflow execution. When present, the runner SHALL use the selected profile set's `agents:` map as the pool against which all `agent: <name>` step references are resolved.

#### Scenario: Project selects an existing profile
- **WHEN** the project config contains `active_profile: copilot` and a `copilot` profile set is defined (in either the project or global file)
- **THEN** the runner uses the `copilot` profile set's agents for all agent resolution in the workflow

#### Scenario: Project selects a nonexistent profile
- **WHEN** the project config contains `active_profile: missing` and no profile set named `missing` exists after merging global and project
- **THEN** config loading fails with an error naming the missing profile

### Requirement: Active profile fallback to default
When the project config does not specify `active_profile`, the runner SHALL use the profile set literally named `default`. The built-in defaults always provide a `default` profile set as a base layer beneath the global and project configs, so the runner SHALL always find a `default` set (either from defaults alone or merged with user-supplied overrides).

#### Scenario: No active_profile, default exists
- **WHEN** the project config does not set `active_profile` and a `default` profile set is defined (via global or project)
- **THEN** the runner uses the `default` profile set for agent resolution

#### Scenario: No active_profile, no user-defined default
- **WHEN** the project config does not set `active_profile` and neither the project nor global file defines a `default` profile set
- **THEN** the runner uses the built-in defaults' `default` profile set for agent resolution

### Requirement: active_profile is project-only
The `active_profile` field SHALL only be honored in the project-local config file (`.agent-runner/config.yaml`). Setting `active_profile` in the global config file (`~/.agent-runner/config.yaml`) SHALL cause config loading to fail with an error indicating that `active_profile` is not allowed in the global config.

#### Scenario: active_profile present only in global
- **WHEN** `~/.agent-runner/config.yaml` contains `active_profile: something`
- **THEN** config loading fails with an error indicating `active_profile` is not allowed in the global config

#### Scenario: active_profile in project overrides nothing in global
- **WHEN** the project config sets `active_profile: foo` and the global config does not (because it cannot)
- **THEN** the runner uses `foo` without consulting the global file for an active profile

### Requirement: Profile set merging across files
When both the global and project configs define profile sets, the runner SHALL produce a single merged map of profile sets as follows:
- Profile sets whose names appear in only one file are included as-is.
- For a profile set whose name appears in both files, the runner SHALL merge their `agents:` maps. Within that merged agents map, an agent whose name appears in both files is taken entirely from the project file (the existing `agent-profiles` merge rule, applied one level deeper). Agents whose names appear in only one file pass through as-is.

Validation (base-agent completeness, allowed field values, cycle detection) runs against the merged set of agents in the active profile.

#### Scenario: Disjoint profile set names across files
- **WHEN** the global file defines profile set `work` and the project file defines profile set `personal`
- **THEN** the merged config contains both `work` and `personal` as distinct profile sets

#### Scenario: Same profile set name, disjoint agents
- **WHEN** both files define profile set `default`, the global with `agents: { planner: ... }` and the project with `agents: { implementor: ... }`
- **THEN** the merged `default` profile set contains both `planner` and `implementor`

#### Scenario: Same profile set name, overlapping agents
- **WHEN** both files define profile set `default` with an `implementor` agent, the global with `cli: claude, model: opus` and the project with `cli: copilot` (no `model`)
- **THEN** the merged `default.agents.implementor` is exactly the project version (`cli: copilot`, no `model`); no field-level fallback to the global version occurs

### Requirement: Non-active profile sets are loaded but unused
The runner SHALL load and validate all profile sets in the merged config (for error detection and future use), but SHALL only expose agents from the profile set named by `active_profile` (or `default` when unset) for workflow agent resolution. Agents defined in non-active profile sets SHALL NOT be reachable via `agent: <name>` references in workflow steps.

#### Scenario: Agent referenced from inactive profile set
- **WHEN** `active_profile: default` is set, the `default` profile set contains `planner`, the `copilot` profile set contains `cloud_reviewer`, and a workflow step says `agent: cloud_reviewer`
- **THEN** agent resolution fails with an error indicating `cloud_reviewer` is not defined in the active profile

#### Scenario: Invalid agent in non-active profile set still blocks load
- **WHEN** the `copilot` profile set (not the active one) contains an agent with an invalid `effort` value
- **THEN** config loading fails with a validation error, even though `copilot` is not active

### Requirement: Profile set extends field
A profile set MAY include an optional top-level `extends: <parent_profile_set_name>` field that names another profile set to inherit agents from. `extends` SHALL be a single string naming exactly one parent. The parent name SHALL reference a profile set that exists in the merged (global + project + defaults) profile set map.

The `extends` field MAY appear in either the global config (`~/.agent-runner/config.yaml`) or the project config (`.agent-runner/config.yaml`). When both files declare `extends` on a profile set of the same name, the project file's value SHALL win (matching the existing project-over-global precedence rule).

#### Scenario: Child profile set inherits parent's agents
- **WHEN** the global file defines profile set `team_base` with agents `{ autonomous_base, planner }` and the project file defines `copilot` with `extends: team_base` and agents `{ implementor }`
- **THEN** the effective `copilot` profile set contains `autonomous_base`, `planner`, and `implementor`

#### Scenario: Child profile set overrides a parent agent by name
- **WHEN** the parent profile set defines `implementor` with `cli: claude` and `model: opus`, and the child profile set (via `extends`) defines `implementor` with `extends: autonomous_base` and `cli: copilot`
- **THEN** the effective child `implementor` is exactly the child's version (`extends: autonomous_base`, `cli: copilot`); no fields inherit from the parent set's `implementor`

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
- **WHEN** the global file contributes `team_base.agents: { autonomous_base }` and `copilot.agents: { planner }`, and the project file contributes `copilot.extends: team_base` and `copilot.agents: { implementor }`
- **THEN** the effective `copilot` profile set contains `autonomous_base` (from `team_base`), `planner` (from global `copilot`), and `implementor` (from project `copilot`)

#### Scenario: Multi-level profile set chain
- **WHEN** profile set `a` extends `b`, and profile set `b` extends `c`; `c` defines `{ autonomous_base }`, `b` defines `{ planner }`, `a` defines `{ implementor }`
- **THEN** the effective `a` contains `autonomous_base`, `planner`, and `implementor`

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
- **WHEN** the project's `copilot` profile set declares `extends: team_base`, and `team_base` defines `autonomous_base`, and `copilot` defines `implementor` with `extends: autonomous_base`
- **THEN** resolving `implementor` in the active `copilot` profile succeeds and inherits fields from `team_base`'s `autonomous_base`

#### Scenario: Agent-level extends still cannot cross unrelated profile sets
- **WHEN** the active profile set is `copilot` (with no `extends`), and an agent in `copilot` declares `extends: planner`, where `planner` is defined only in a different, unrelated profile set
- **THEN** config loading fails with an error indicating the parent agent does not exist in the containing profile set's effective agents map

### Requirement: Pre-validation surfaces layered-config and profile errors

Pre-validation (see `workflow-pre-validation`) SHALL exercise the same layered config load that runtime agent-step resolution uses, so configuration errors that today only surface at the moment an agent step is dispatched SHALL instead surface before the run begins.

The layered load SHALL combine built-in defaults, `~/.agent-runner/config.yaml`, and the project's `.agent-runner/config.yaml` when present, validate the resulting config through the same loader the runtime uses, and walk every agent referenced by the workflow through its `extends` chain in the active profile set.

Errors from layered-config validation SHALL include the profile set name, agent name, field name, invalid value, and (where the schema knows them) allowed values. The originating layer file is **best-effort**: errors include the list of layer files that were loaded rather than a precise file-of-record, because the current layered-config loader merges and validates without retaining per-field origin metadata. Adding origin tracking is a separate, future change.

#### Scenario: Invalid effort in global config fails before run start
- **WHEN** `~/.agent-runner/config.yaml` sets `profiles.default.agents.implementor.effort: extreme` and a fresh, non-builtin run references the `implementor` agent
- **THEN** pre-validation fails before any step executes with a structured error naming the profile set `default`, the agent `implementor`, the field `effort`, the invalid value `extreme`, the allowed values, and a best-effort layer list that includes `~/.agent-runner/config.yaml`

#### Scenario: Project config overrides resolved in pre-validation
- **WHEN** the project's `.agent-runner/config.yaml` overrides `profiles.default.agents.planner.model` and a fresh, non-builtin run references the `planner` agent
- **THEN** pre-validation resolves the effective `(cli, model, effort)` triple using the merged config and probes that triple, not the unmerged global value

#### Scenario: Profile resolution failure names the chain
- **WHEN** an agent definition extends a parent profile that does not exist in the active profile set
- **THEN** pre-validation fails with an error naming the agent, the missing parent, the active profile set, and the best-effort layer list searched

