# config-profiles Specification

## Purpose
Define profile-set schema, active-profile selection, profile-set inheritance, and layered config merge behavior.
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

Config loading SHALL additionally accept an optional caller-supplied profile-set override, consisting of
a profile set name and a label describing where the caller obtained it. Callers supply the override from
the `--profile` flag (see the `launch-profile-flag` capability) or from a profile set recorded in a
resumed run's state file (see the `recursive-state` capability). Selection precedence SHALL be, highest
first:

1. the caller-supplied override, when supplied,
2. the project config's `active_profile`, when set,
3. the profile set literally named `default`.

The override SHALL be able to name any profile set present in the merged config, including one defined
only in the global config file. The override SHALL affect only the current invocation and SHALL NOT be
written to any config file.

When the override names a profile set that does not exist in the merged config, config loading SHALL
fail with an error that names the requested profile set, attributes it to the caller-supplied origin
label rather than to `active_profile`, and lists the available profile set names in sorted order.

#### Scenario: Project selects an existing profile
- **WHEN** the project config contains `active_profile: copilot` and a `copilot` profile set is defined (in either the project or global file)
- **THEN** the runner uses the `copilot` profile set's agents for all agent resolution in the workflow

#### Scenario: Project selects a nonexistent profile
- **WHEN** the project config contains `active_profile: missing` and no profile set named `missing` exists after merging global and project
- **THEN** config loading fails with an error naming the missing profile

#### Scenario: Override wins over active_profile
- **WHEN** the project config contains `active_profile: work`, a `copilot` profile set exists, and config
  loading is given the override `copilot`
- **THEN** the runner uses the `copilot` profile set's agents for all agent resolution

#### Scenario: Override wins over the default fallback
- **WHEN** the project config sets no `active_profile`, a `copilot` profile set exists, and config loading
  is given the override `copilot`
- **THEN** the runner uses the `copilot` profile set's agents rather than the `default` set

#### Scenario: Override names a globally-defined profile set
- **WHEN** the global config defines profile set `work`, the project config defines no profile sets and no
  `active_profile`, and config loading is given the override `work`
- **THEN** config loading succeeds and the runner uses the `work` profile set's agents

#### Scenario: Override names a nonexistent profile set
- **WHEN** config loading is given the override `missing` and the merged config defines only `default` and
  `copilot`
- **THEN** config loading fails with an error that attributes `missing` to the caller-supplied origin and
  lists `copilot, default` as available

#### Scenario: Error text reflects the origin label
- **WHEN** config loading is given the nonexistent override `missing` with an origin label identifying a
  resumed run's state file
- **THEN** the error attributes `missing` to that state file rather than to the `--profile` flag or to
  `active_profile`

#### Scenario: No override supplied
- **WHEN** config loading is given no override
- **THEN** selection behaves exactly as it did before this change: `active_profile` when set, otherwise
  `default`

### Requirement: Active profile fallback to default
When the project config does not specify `active_profile` and no caller-supplied override is supplied, the runner SHALL use the profile set literally named `default`. The built-in defaults always provide a `default` profile set as a base layer beneath the global and project configs, so the runner SHALL always find a `default` set (either from defaults alone or merged with user-supplied overrides).

#### Scenario: No active_profile, default exists
- **WHEN** the project config does not set `active_profile` and a `default` profile set is defined (via global or project)
- **THEN** the runner uses the `default` profile set for agent resolution

#### Scenario: No active_profile, no user-defined default
- **WHEN** the project config does not set `active_profile` and neither the project nor global file defines a `default` profile set
- **THEN** the runner uses the built-in defaults' `default` profile set for agent resolution

#### Scenario: No active_profile but an override is supplied
- **WHEN** the project config does not set `active_profile` and a caller-supplied override names an existing
  profile set
- **THEN** the runner uses the overridden profile set and does not fall back to `default`

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
The runner SHALL load and validate all profile sets in the merged config (for error detection and future use), but SHALL only expose agents from the profile set selected by the precedence rule in "Active profile selection" (the caller-supplied override, then `active_profile`, then `default`) for workflow agent resolution. Agents defined in non-selected profile sets SHALL NOT be reachable via `agent: <name>` references in workflow steps.

Validation of every profile set is independent of selection: a validation error in a non-selected set
SHALL still block config loading, and the error SHALL identify the profile set that contains the invalid
definition rather than the selected set.

#### Scenario: Agent referenced from inactive profile set
- **WHEN** `active_profile: default` is set, the `default` profile set contains `planner`, the `copilot` profile set contains `cloud_reviewer`, and a workflow step says `agent: cloud_reviewer`
- **THEN** agent resolution fails with an error indicating `cloud_reviewer` is not defined in the active profile

#### Scenario: Invalid agent in non-active profile set still blocks load
- **WHEN** the `copilot` profile set (not the active one) contains an agent with an invalid `effort` value
- **THEN** config loading fails with a validation error, even though `copilot` is not active

#### Scenario: Agent only in the config-selected set is unreachable under an override
- **WHEN** the project config sets `active_profile: work`, the `work` set defines agent `auditor`, the
  `copilot` set does not, an override selects `copilot`, and a workflow step says `agent: auditor`
- **THEN** agent resolution fails with an error indicating `auditor` is not defined in the active profile

#### Scenario: Invalid agent in a non-selected set still blocks load under an override
- **WHEN** an override selects `copilot` and the non-selected `work` profile set contains an agent with an
  invalid `effort` value
- **THEN** config loading fails with a validation error naming `work` as the profile set containing the
  invalid definition, even though `work` is not the selected set

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

Built-in agents are fallback entries only when global and project configuration do not explicitly select a parent for that profile set. When a user layer declares `extends`, the selected parent's agents replace built-in agents as the fallback; agents declared in global and project layers for the child still merge normally and override the parent.

#### Scenario: Merge-then-extend across global and project
- **WHEN** the global file contributes `team_base.agents: { autonomous_base }` and `copilot.agents: { planner }`, and the project file contributes `copilot.extends: team_base` and `copilot.agents: { implementor }`
- **THEN** the effective `copilot` profile set contains `autonomous_base` (from `team_base`), `planner` (from global `copilot`), and `implementor` (from project `copilot`)

#### Scenario: User-selected parent replaces built-in fallback agents
- **WHEN** global configuration defines a `codex` profile set, declares `default.extends: codex`, and does not declare `default.agents.implementor`
- **THEN** the effective `default` profile inherits `implementor` from `codex`; the built-in `default.agents.implementor` does not shadow the user-selected parent

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

### Requirement: Resolved profile set is reportable
Config loading SHALL expose the name of the profile set it selected and the source of that selection: the
caller-supplied override (carrying the caller's origin label), the project config's `active_profile`, or
the `default` fallback. The reported name SHALL be the set actually used for agent resolution. Callers
that record or display the source (see the `audit-log-entries` capability) SHALL derive their own source
value from this report and the origin label they supplied, so config loading itself does not need to know
about flags, state files, or run history.

#### Scenario: Resolved name and source for an override
- **WHEN** config loading selects profile set `copilot` because of a caller-supplied override
- **THEN** the loaded config reports the resolved name `copilot`, the override as its source, and the
  origin label the caller supplied

#### Scenario: Resolved name and source for active_profile
- **WHEN** config loading selects profile set `work` because the project config sets `active_profile: work`
- **THEN** the loaded config reports the resolved name `work` with the project config as its source

#### Scenario: Resolved name and source for the fallback
- **WHEN** config loading selects `default` because no override and no `active_profile` were supplied
- **THEN** the loaded config reports the resolved name `default` with the fallback as its source

### Requirement: Profile set is always resolved
Config loading SHALL resolve a profile set for every run, regardless of whether the workflow contains agent
steps. The runner SHALL therefore load layered config for every run rather than only for workflows that
reference agents, so that a caller-supplied override is always validated and the resolved name is always
available to record and display.

#### Scenario: Shell-only workflow resolves a profile set
- **WHEN** a workflow containing only shell steps runs with no `--profile` flag in a project that sets no
  `active_profile`
- **THEN** config loading resolves `default` as the run's profile set

#### Scenario: Override validated on a shell-only workflow
- **WHEN** a workflow containing only shell steps runs with `--profile missing` and no `missing` profile set
  exists
- **THEN** config loading fails with the unknown-profile error and the run does not start

#### Scenario: Malformed config fails a shell-only run
- **WHEN** a workflow containing only shell steps runs in a project whose config file is malformed
- **THEN** the run fails with the config error, rather than succeeding because no agent step needed profiles

