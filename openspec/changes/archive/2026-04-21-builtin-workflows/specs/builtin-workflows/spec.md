## ADDED Requirements

### Requirement: Builtin workflow set embedded at build time
The `agent-runner` binary SHALL include a set of builtin workflows embedded at build time from the repository's top-level `workflows/` directory. Each top-level subdirectory of `workflows/` SHALL define a builtin namespace whose name equals the subdirectory name; the YAML files within it SHALL be the workflows of that namespace. The embedded set SHALL be available without any files present on the end user's filesystem.

#### Scenario: Builtin workflow runnable without local files
- **WHEN** a user invokes `agent-runner run core:finalize-pr` in a directory that contains no `workflows/` or `.agent-runner/workflows/` directory
- **THEN** the workflow loads from the embedded builtin set and executes

#### Scenario: Subdirectory names define namespaces
- **WHEN** the repository contains `workflows/spec-driven/plan-change.yaml` at build time
- **THEN** the built binary resolves `spec-driven:plan-change` to that embedded workflow

#### Scenario: Top-level files not exposed as builtins
- **WHEN** the repository's `workflows/` directory contains a YAML file that is not inside a subdirectory
- **THEN** that file is not exposed as a builtin workflow under any namespace

### Requirement: Builtin sub-workflow references
A builtin workflow that references another workflow via a relative path (e.g., `workflow: plan-change.yaml`) SHALL have that reference resolved within the same embedded namespace. The reference SHALL NOT fall back to the user's `.agent-runner/workflows/` directory.

#### Scenario: Relative reference resolves within embedded namespace
- **WHEN** the embedded workflow `spec-driven:change` contains `workflow: plan-change.yaml`
- **AND** the user invokes `agent-runner run spec-driven:change`
- **THEN** the sub-workflow loads from the embedded `spec-driven/plan-change.yaml`

#### Scenario: Embedded reference does not fall back to user directory
- **WHEN** the embedded workflow `spec-driven:change` references `plan-change.yaml`
- **AND** the user has a file `.agent-runner/workflows/plan-change.yaml`
- **THEN** the embedded `spec-driven/plan-change.yaml` is used, not the user's file

### Requirement: Core namespace for general-purpose builtins
The builtin set SHALL include a `core` namespace containing general-purpose workflows that are not tied to any particular planning methodology. The `core` namespace SHALL at minimum contain `finalize-pr`, `implement-task`, and `run-validator`.

#### Scenario: Core workflows invoked by namespace
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **THEN** the finalize-pr workflow loads from the embedded `core` namespace

#### Scenario: Core workflows not invoked by bare name
- **WHEN** the user runs `agent-runner run finalize-pr` with no `.agent-runner/workflows/finalize-pr.yaml` file
- **THEN** the command fails with a workflow-not-found error; the builtin `core:finalize-pr` is not resolved
