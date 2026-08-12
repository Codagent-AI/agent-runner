# builtin-workflows Specification

## Purpose
Define embedded built-in workflow namespaces, metadata, name resolution, validation, and launch behavior.
## Requirements
### Requirement: Builtin workflow set embedded at build time

The `agent-runner` binary SHALL include all built-in workflow versions embedded at build time from the repository's top-level `workflows/` directory. Each top-level subdirectory of `workflows/` SHALL define a builtin namespace whose name equals the subdirectory name. YAML files inside a namespace whose basenames do not start with an underscore (`_`) SHALL be treated as workflow definitions and MUST follow the versioned filename contract. YAML files whose basenames begin with an underscore SHALL remain reserved for namespace metadata and SHALL NOT be exposed as workflows.

Discovery SHALL expose exactly one entry per canonical logical workflow name, backed by the highest embedded major/minor version. Older versioned files SHALL remain embedded and addressable by exact internal references and saved versioned run state, but MUST NOT be exposed as separate logical names or accepted as explicit launch names. An unversioned built-in workflow definition SHALL invalidate its logical group even when a valid versioned sibling exists. The embedded set SHALL remain available without any files present on the end user's filesystem.

The built-in migration SHALL map each existing base/`2` pair in the `openspec` namespace to coexisting `v1.0` and `v2.0` generations:
- `openspec/change.yaml` and `openspec/change2.yaml` become `openspec/change-v1.0.yaml` and `openspec/change-v2.0.yaml`;
- `openspec/plan-change.yaml` and `openspec/plan-change2.yaml` become `openspec/plan-change-v1.0.yaml` and `openspec/plan-change-v2.0.yaml`;
- `openspec/implement-change.yaml` and `openspec/implement-change2.yaml` become `openspec/implement-change-v1.0.yaml` and `openspec/implement-change-v2.0.yaml`.

Other built-in workflows present at migration time SHALL start at `v1.0`.

#### Scenario: Builtin workflow runnable without local files
- **WHEN** a user invokes `agent-runner run core:finalize-pr` in a directory that contains no `workflows/` or `.agent-runner/workflows/` directory
- **THEN** the highest embedded `core:finalize-pr` version loads and executes

#### Scenario: Subdirectory names define namespaces
- **WHEN** the repository contains `workflows/spec-driven/plan-change-v1.0.yaml` at build time
- **THEN** the built binary resolves logical name `spec-driven:plan-change` to that embedded workflow

#### Scenario: Latest builtin version exposed
- **WHEN** the embedded set contains `openspec/change-v1.0.yaml` and `openspec/change-v2.0.yaml`
- **THEN** discovery exposes one `openspec:change` entry backed by `change-v2.0.yaml`

#### Scenario: Legacy numeric suffix logical name removed
- **WHEN** the embedded set has migrated `change2.yaml` to `change-v2.0.yaml`
- **THEN** `openspec:change2` returns a workflow-not-found error while `openspec:change` resolves the latest version

#### Scenario: Older version remains embedded but not launchable
- **WHEN** `openspec/change-v1.0.yaml` is older than the selected `change-v2.0.yaml`
- **THEN** exact internal references and saved versioned state can read `change-v1.0.yaml`, but discovery does not expose `openspec:change-v1.0` and that name cannot start a new run

#### Scenario: Paired v2 workflows migrate together
- **WHEN** built-in migration is complete
- **THEN** the `openspec` namespace's `change`, `plan-change`, and `implement-change` workflows each have embedded `v1.0` and `v2.0` files derived from their former base and `2` files

#### Scenario: Same-named spec-driven workflows remain first generation
- **WHEN** built-in migration is complete
- **THEN** the `spec-driven` namespace's `change`, `plan-change`, and `implement-change` workflows each have a `v1.0` file and do not gain a `v2.0` file from the `openspec` namespace's pairs

#### Scenario: Other builtins begin at v1.0
- **WHEN** a built-in workflow had no paired `2` file at migration time
- **THEN** its migrated definition uses version `v1.0`

#### Scenario: Unversioned builtin invalidates logical group
- **WHEN** a namespace contains `deploy.yaml` and `deploy-v1.0.yaml`
- **THEN** built-in discovery reports the actionable versioned-filename error for `deploy.yaml` and does not expose `deploy`

#### Scenario: Top-level files not exposed as builtins
- **WHEN** the repository's `workflows/` directory contains a YAML file that is not inside a subdirectory
- **THEN** that file is not exposed as a builtin workflow under any namespace

#### Scenario: Underscore-prefixed file not exposed as a workflow
- **WHEN** `workflows/core/_group.yaml` exists
- **THEN** discovery does not produce a workflow entry for `core:_group`
- **AND** `agent-runner run core:_group` returns a workflow-not-found error

### Requirement: Builtin sub-workflow references

A builtin workflow that references another workflow via a relative path SHALL reference a valid versioned filename and SHALL load that exact embedded file. A newer version of the referenced logical workflow MUST NOT replace the explicit target. Relative references within one namespace and relative cross-namespace references SHALL remain inside the embedded built-in set and MUST NOT fall back to the user's `.agent-runner/workflows/` directory. An unversioned built-in sub-workflow reference SHALL fail with the actionable versioned-filename error.

#### Scenario: Relative reference resolves within embedded namespace
- **WHEN** embedded `openspec/change-v2.0.yaml` contains `workflow: plan-change-v2.0.yaml`
- **AND** the user invokes `agent-runner run openspec:change`
- **THEN** the sub-workflow loads from embedded `openspec/plan-change-v2.0.yaml`

#### Scenario: Newer child does not replace explicit version
- **WHEN** a parent references `plan-change-v1.0.yaml` and embedded `plan-change-v1.1.yaml` also exists
- **THEN** the parent loads `plan-change-v1.0.yaml`

#### Scenario: Embedded reference does not fall back to user directory
- **WHEN** an embedded workflow references `plan-change-v1.0.yaml`
- **AND** the user has `.agent-runner/workflows/plan-change-v1.0.yaml`
- **THEN** the embedded `plan-change-v1.0.yaml` is used, not the user's file

#### Scenario: Cross-namespace reference resolves exact embedded version
- **WHEN** embedded `openspec/change-v2.0.yaml` references `../core/finalize-pr-v1.0.yaml`
- **THEN** the sub-workflow loads embedded `core/finalize-pr-v1.0.yaml`

#### Scenario: Unversioned builtin reference rejected
- **WHEN** an embedded parent contains `workflow: plan-change.yaml`
- **THEN** validation fails with the actionable versioned-filename error for `plan-change.yaml`

### Requirement: Core namespace for general-purpose builtins

The builtin set SHALL include a `core` namespace containing general-purpose workflows that are not tied to any particular planning methodology. The `core` namespace SHALL at minimum contain logical workflows `finalize-pr`, `implement-task`, `run-validator`, and `debug`, each backed by at least one versioned definition.

#### Scenario: Core workflows invoked by namespace
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **THEN** the latest finalize-pr version loads from the embedded `core` namespace

#### Scenario: Core workflows not invoked by bare name
- **WHEN** the user runs `agent-runner run finalize-pr` with no project or user logical workflow group named `finalize-pr`
- **THEN** the command fails with a workflow-not-found error; the builtin `core:finalize-pr` is not resolved

#### Scenario: Debug workflow available under core
- **WHEN** the user runs `agent-runner run core:debug`
- **THEN** the latest debug version loads from the embedded `core` namespace and executes

### Requirement: Onboarding namespace embedded

The builtin set SHALL include an `onboarding` namespace alongside the existing `core`, `openspec`, and `spec-driven` namespaces. The `onboarding` namespace SHALL contain at minimum `onboarding` as the top-level demo workflow and `step-types-demo` as the workflow step demonstration. The namespace SHALL NOT expose `welcome` or `setup-agent-profile` workflows because first-run setup is native TUI functionality.

#### Scenario: Onboarding demo workflow invoked by namespace
- **WHEN** the user runs `agent-runner run onboarding:onboarding`
- **THEN** the workflow loads from the embedded `onboarding` namespace and executes

#### Scenario: Step types demo workflow exists
- **WHEN** the user runs `agent-runner run onboarding:step-types-demo`
- **THEN** the workflow loads from the embedded `onboarding` namespace and executes

#### Scenario: Welcome workflow not exposed
- **WHEN** the user runs `agent-runner run onboarding:welcome`
- **THEN** the runner fails with a workflow-not-found error

#### Scenario: Setup workflow not exposed
- **WHEN** the user runs `agent-runner run onboarding:setup-agent-profile`
- **THEN** the runner fails with a workflow-not-found error

### Requirement: Non-YAML files embedded as bundled assets

Files in a namespace subdirectory whose names end in neither `.yaml` nor `.yml` SHALL be embedded as bundled assets and accessible at runtime via the relative paths declared by supported builtin workflow references. Files ending in `.yaml` or `.yml` SHALL be treated as workflow definitions or underscore-prefixed workflow metadata, not bundled assets. The embed mechanism SHALL preserve file mode bits relevant to execution where the host filesystem records them. Asset path resolution SHALL stay within the namespace; the runner SHALL NOT fall back to user-authored workflows under `.agent-runner/workflows/` when an embedded workflow references a bundled asset.

#### Scenario: Embedded onboarding docs accessible
- **WHEN** the embedded onboarding demo references packaged documentation for Q&A
- **THEN** the documentation files are embedded and accessible at runtime

#### Scenario: Embedded asset does not fall back to user directory
- **WHEN** an embedded onboarding workflow references a bundled asset and a user-authored file with the same relative path exists
- **THEN** the embedded asset is used and the user file is not consulted

#### Scenario: Bundled JSON data file embedded
- **WHEN** a namespace subdirectory contains a non-YAML data file referenced by a bundled workflow or asset
- **THEN** the file is embedded and accessible at runtime via its relative path within the namespace

#### Scenario: YML definition is not a bundled asset
- **WHEN** a namespace subdirectory contains `deploy-v1.0.yml`
- **THEN** the file is treated as a workflow definition and is not exposed through the bundled-asset API

#### Scenario: Top-level non-YAML files not exposed
- **WHEN** the repository's `workflows/` directory contains a non-YAML file at the top level
- **THEN** that file is not exposed as a bundled asset under any namespace

### Requirement: Per-namespace group metadata

Each builtin namespace directory `workflows/<ns>/` MAY contain a metadata file named `_group.yaml` declaring a human-readable display name and description for the namespace. The file SHALL support two optional top-level fields: `display_name` (string) and `description` (string). When present, the metadata file SHALL be embedded into the binary at build time alongside the namespace's workflow YAMLs and SHALL be accessible to discovery at runtime. When the metadata file is absent or malformed, the namespace SHALL fall back to defaults: the display name equals the namespace name, and the description is empty. A missing or malformed metadata file SHALL NOT prevent the namespace's workflows from being discovered, loaded, or executed.

#### Scenario: Metadata file present is surfaced by discovery
- **WHEN** `workflows/core/_group.yaml` exists and declares `display_name` and `description`
- **THEN** discovery exposes those values to consumers (e.g., the new-tab renderer) for the `core` namespace

#### Scenario: Metadata file absent yields defaults
- **WHEN** a builtin namespace directory contains workflow YAMLs but no `_group.yaml`
- **THEN** discovery reports the namespace with a default display name equal to the namespace name and an empty description
- **AND** the namespace's workflows are still discovered and runnable

#### Scenario: Malformed metadata file does not fail the namespace
- **WHEN** a builtin namespace's `_group.yaml` exists but cannot be parsed
- **THEN** discovery reports the namespace with default display name and empty description
- **AND** the namespace's workflows are still discovered and runnable
