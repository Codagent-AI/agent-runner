# Task: Enforce and Migrate Versioned Definitions

## Goal

Make exact workflow loading filename-aware and atomically migrate the embedded workflow set to versioned filenames and exact versioned references. After this task, every embedded definition and every exact loader entry point uses one validated filename/YAML-name contract, while historical embedded files remain directly readable.

## Background

`internal/loader/loader.go` currently reads bytes and calls the pathless `ParseWorkflow`. Change the source-aware path so `LoadWorkflow` validates the filename before reading, parses the YAML, and verifies `workflow.Name` against the version-free basename. Keep `ParseWorkflow` content-only for callers that genuinely have no source path. Add a source-aware parse function for `fs.FS` consumers so embedded discovery cannot bypass filename and YAML-name validation.

Filename errors should be structured enough to distinguish on-disk paths from `builtin:` references. Validate before reading so an unversioned recorded on-disk path gets migration guidance even if the old file has already been renamed or removed. Exact valid paths remain exact; this task must not perform latest-version substitution inside `LoadWorkflow`.

Update `workflows/embed.go` to use the shared catalog for builtin enumeration and logical resolution:

- `Resolve("<namespace>:<logical-name>")` returns the latest valid embedded version;
- `ReadFile("builtin:<namespace>/<versioned-file>")` continues to read that exact file;
- `List()` retains every exact embedded workflow version for internal validation and saved-state access;
- top-level files and underscore-prefixed YAML metadata are not exposed as workflows; and
- `.yaml` and `.yml` are definitions, never bundled assets.

Rename the complete builtin set under `workflows/` in the same implementation:

- `openspec/change.yaml` and `openspec/change2.yaml` become `openspec/change-v1.0.yaml` and `openspec/change-v2.0.yaml`;
- `openspec/plan-change.yaml` and `openspec/plan-change2.yaml` become `openspec/plan-change-v1.0.yaml` and `openspec/plan-change-v2.0.yaml`;
- `openspec/implement-change.yaml` and `openspec/implement-change2.yaml` become `openspec/implement-change-v1.0.yaml` and `openspec/implement-change-v2.0.yaml`;
- every other non-metadata builtin becomes `*-v1.0.yaml`;
- YAML `name:` values remain lowercase and version-free; and
- every relative `workflow:` reference points to an exact versioned child, including cross-namespace references.

Update exact embedded refs in Go tests and production code, and mechanically migrate loader/prevalidation/exec/run-view fixtures that are meant to be valid definitions. Keep deliberately malformed and legacy-unversioned fixtures unversioned when a test is asserting the new failure behavior. Update `internal/prevalidate/builtins_test.go` or equivalent integration coverage to enumerate every embedded version, load it, validate its composition, and prove every referenced child exists. Preserve non-YAML assets, their namespace boundaries, and executable modes.

Key paths include `internal/loader/`, `workflows/embed.go`, `workflows/**/*.yaml`, `internal/prevalidate/builtins_test.go`, `testdata/`, and exact `builtin:` literals throughout `cmd/` and `internal/`.

### Intermediate green boundary

This task runs after task 01 and before catalog-backed local CLI resolution and logical discovery. It MUST leave `make test` green without pulling task 03 or task 04 production behavior forward.

At this boundary:

- `LoadWorkflow` rejects unversioned definitions and all valid fixtures/builtins use versioned filenames.
- Namespaced builtin launches remain end-to-end testable because this task makes `builtinworkflows.Resolve("<namespace>:<logical-name>")` catalog-backed.
- Bare/path-style local name resolution still probes the old unversioned filenames until task 03. Mechanically change affected end-to-end command tests to pass an existing explicit versioned file path, which the pre-task-03 resolver still accepts, or use a namespaced builtin logical name. This is temporary test wiring only; do not add a production compatibility branch for unversioned files.
- Discovery still exposes the physical version suffix in canonical names until task 04. Mechanically update affected discovery, listview, runview-definition, and command/message assertions to the intermediate version-bearing canonical names.

Record these temporary assertions clearly in the tests. Task 03 MUST replace temporary direct-path launch coverage with final logical-name launch and path-rejection coverage. Task 04 MUST replace intermediate discovery/listview canonical-name assertions with final version-free logical names.

The embedded migration and logical `builtinworkflows.Resolve` portions of the “Builtin workflow set embedded at build time” requirement finish here. Its final discovery-row clauses and scenarios are completed by task 04 after discovery consumes the catalog.

## Spec

### Requirement: YAML name matches logical basename

Every workflow definition's required YAML `name:` value MUST exactly equal its lowercase, version-free filename basename. Directory segments, the version suffix, and the file extension MUST NOT appear in `name:`. Agent Runner SHALL reject a mismatch with an error containing both the expected and actual values.

#### Scenario: Nested workflow name matches basename
- **WHEN** `team/deploy-v2.0.yaml` contains `name: deploy`
- **THEN** the workflow passes name-alignment validation

#### Scenario: Directory-qualified YAML name rejected
- **WHEN** `team/deploy-v2.0.yaml` contains `name: team/deploy`
- **THEN** Agent Runner rejects the workflow and reports that the expected name is `deploy`

#### Scenario: Version-bearing YAML name rejected
- **WHEN** `deploy-v2.0.yaml` contains `name: deploy-v2.0`
- **THEN** Agent Runner rejects the workflow and reports that the expected name is `deploy`

#### Scenario: Uppercase YAML name rejected
- **WHEN** `deploy-v2.0.yaml` contains `name: Deploy`
- **THEN** Agent Runner rejects the workflow and reports that the expected name is `deploy`

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

#### Scenario: Relative reference resolves exact version within namespace
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

## Done When

- `LoadWorkflow` and the embedded/source-aware parse path enforce filename validity and YAML-name alignment, while pathless `ParseWorkflow` remains content-only.
- All non-metadata files under `workflows/` use the approved version mapping, all YAML names are version-free, and every builtin sub-workflow reference is exact and versioned.
- Exact `builtin:` refs can read older versions, logical builtin refs select the latest version, and `List()` exposes all exact versions for validation without exposing metadata or top-level files.
- Tests cover source-specific filename errors, YAML-name mismatch, `.yml`, invalid siblings, duplicate versions, namespace confinement, bundled assets, and executable scripts.
- A build-time test loads and pre-validates the complete embedded graph and fails on an unversioned definition or missing/unversioned child.
- Focused loader, builtin, and prevalidation tests pass; all mechanically affected fixtures and exact path literals are migrated without changing test intent.
- Temporary command tests launch local definitions by explicit versioned path (or use namespaced builtin logical names), and temporary discovery/listview assertions expect physical version suffixes exactly as described in the intermediate boundary.
- `make fmt` and `make test` pass before task 03 begins.
