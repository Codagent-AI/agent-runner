## ADDED Requirements

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
- **WHEN** `~/.agent-runner/config.yaml` contains `profiles: { headless_base: { default_mode: headless, cli: claude } }`
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
When the project config does not specify `active_profile`, the runner SHALL use the profile set literally named `default`. If no profile set named `default` exists in the merged config, config loading SHALL fail with an error instructing the user to either define a `default` profile set or set `active_profile` explicitly.

#### Scenario: No active_profile, default exists
- **WHEN** the project config does not set `active_profile` and a `default` profile set is defined (via global or project)
- **THEN** the runner uses the `default` profile set for agent resolution

#### Scenario: No active_profile, no default
- **WHEN** the project config does not set `active_profile` and no profile set named `default` exists
- **THEN** config loading fails with an error indicating either `default` must be defined or `active_profile` must be set

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
