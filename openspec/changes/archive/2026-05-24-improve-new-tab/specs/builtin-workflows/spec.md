## ADDED Requirements

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

## MODIFIED Requirements

### Requirement: Builtin workflow set embedded at build time
The `agent-runner` binary SHALL include a set of builtin workflows embedded at build time from the repository's top-level `workflows/` directory. Each top-level subdirectory of `workflows/` SHALL define a builtin namespace whose name equals the subdirectory name; the YAML files within it whose basenames do not start with an underscore (`_`) SHALL be the workflows of that namespace. YAML files in a namespace directory whose basenames begin with an underscore SHALL be reserved for namespace metadata (see "Per-namespace group metadata") and SHALL NOT be exposed as workflows. The embedded set SHALL be available without any files present on the end user's filesystem.

#### Scenario: Builtin workflow runnable without local files
- **WHEN** a user invokes `agent-runner run core:finalize-pr` in a directory that contains no `workflows/` or `.agent-runner/workflows/` directory
- **THEN** the workflow loads from the embedded builtin set and executes

#### Scenario: Subdirectory names define namespaces
- **WHEN** the repository contains `workflows/spec-driven/plan-change.yaml` at build time
- **THEN** the built binary resolves `spec-driven:plan-change` to that embedded workflow

#### Scenario: Top-level files not exposed as builtins
- **WHEN** the repository's `workflows/` directory contains a YAML file that is not inside a subdirectory
- **THEN** that file is not exposed as a builtin workflow under any namespace

#### Scenario: Underscore-prefixed file not exposed as a workflow
- **WHEN** `workflows/core/_group.yaml` exists
- **THEN** discovery does not produce a workflow entry for `core:_group`
- **AND** `agent-runner run core:_group` returns a workflow-not-found error
