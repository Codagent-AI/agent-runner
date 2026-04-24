## ADDED Requirements

### Requirement: Global config file location

The runner SHALL load a global agent config from `~/.agent-runner/config.yaml` (where `~` is the invoking user's home directory) when that file exists, in addition to the project-local `.agent-runner/config.yaml`. The global file is optional; the runner SHALL NOT fail if it is absent.

#### Scenario: Global config absent
- **WHEN** the runner starts and `~/.agent-runner/config.yaml` does not exist
- **THEN** the runner proceeds using only the project-local config (auto-generating it if also absent, per existing behavior)

#### Scenario: Global config present, project config present
- **WHEN** both `~/.agent-runner/config.yaml` and `.agent-runner/config.yaml` exist
- **THEN** the runner loads both files and proceeds with the merged profile set

#### Scenario: Global config present, project config absent
- **WHEN** `~/.agent-runner/config.yaml` exists and `.agent-runner/config.yaml` does not exist
- **THEN** the runner generates the project-local config (per existing auto-generation behavior), loads the global file, and proceeds with the merged profile set

#### Scenario: Global config invalid YAML
- **WHEN** `~/.agent-runner/config.yaml` exists but contains invalid YAML
- **THEN** config loading fails with an error indicating the global file path and the parse error

### Requirement: Global config is not auto-generated

The runner SHALL NOT create `~/.agent-runner/config.yaml` automatically. Users who want a global config create the file manually. (A future change will add an opt-in setup flow that may populate this file.)

#### Scenario: Global file missing on startup
- **WHEN** the runner starts and `~/.agent-runner/config.yaml` does not exist
- **THEN** the runner SHALL NOT create that file or its parent directory

### Requirement: Profile merge precedence

When both a global and a project config are loaded, the runner SHALL produce a single merged profile set as follows:
- Profiles whose names appear in only one file are included as-is.
- For a profile name that appears in both files, the project profile entirely replaces the global profile of the same name. Field-level merging across files SHALL NOT occur; the project profile's full body (including its `extends`, or absence thereof) is what survives the merge.

Validation (base-profile completeness, allowed values, cycle detection) SHALL run against the merged set.

#### Scenario: Disjoint profile names
- **WHEN** the global file defines `headless_base` and the project file defines `implementor`
- **THEN** the merged set contains both profiles

#### Scenario: Same profile name in both files
- **WHEN** both files define a profile named `implementor`, the global one with `extends: headless_base` and `model: opus`, and the project one with `extends: headless_base` and `cli: copilot` (no `model`)
- **THEN** the merged `implementor` profile is exactly the project version (`extends: headless_base`, `cli: copilot`, no `model`); the global `model: opus` SHALL NOT be inherited

#### Scenario: Project profile drops a field present in global
- **WHEN** the global `implementor` sets `effort: high` and the project `implementor` omits `effort`
- **THEN** the merged `implementor` has no `effort` (no field-level fallback to the global file)

### Requirement: Cross-file extends resolution

A profile in either file MAY specify `extends: <name>` where `<name>` is a profile defined in the other file. The runner SHALL resolve `extends` against the merged profile set. Cycle detection and missing-parent detection SHALL operate on the merged set.

#### Scenario: Project profile extends global profile
- **WHEN** the global file defines `headless_base` and the project file defines `implementor` with `extends: headless_base`
- **THEN** resolving `implementor` succeeds and inherits `default_mode`, `cli`, `model`, etc. from the global `headless_base`

#### Scenario: Global profile extends project profile
- **WHEN** the project file defines a base profile `team_base` (with `default_mode` and `cli`) and the global file defines `summarizer` with `extends: team_base`
- **THEN** resolving `summarizer` succeeds and inherits from the project's `team_base`

#### Scenario: Cross-file extends references unknown profile
- **WHEN** a profile specifies `extends: missing` and no profile named `missing` exists in either file
- **THEN** config loading fails with an error indicating the parent profile does not exist

#### Scenario: Cross-file inheritance cycle
- **WHEN** the global file defines `a` with `extends: b` and the project file defines `b` with `extends: a`
- **THEN** config loading fails with an error indicating a cycle in the extends chain

#### Scenario: Project profile shadows then extends the original global name
- **WHEN** the global file defines `headless_base` and the project file defines `headless_base` with `extends: headless_base`
- **THEN** config loading fails with a cycle error (the project profile's `extends` resolves to itself in the merged set)
