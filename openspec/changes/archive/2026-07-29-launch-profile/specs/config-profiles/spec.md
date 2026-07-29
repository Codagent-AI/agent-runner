## MODIFIED Requirements

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

## ADDED Requirements

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
