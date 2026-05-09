# builtin-workflows Specification

## Purpose
TBD - created by archiving change builtin-workflows. Update Purpose after archive.
## Requirements
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

Files in a namespace subdirectory whose names do not end in `.yaml` SHALL be embedded as bundled assets and accessible at runtime via the relative paths declared by supported builtin workflow references. The embed mechanism SHALL preserve file mode bits relevant to execution where the host filesystem records them. Asset path resolution SHALL stay within the namespace; the runner SHALL NOT fall back to user-authored workflows under `.agent-runner/workflows/` when an embedded workflow references a bundled asset.

#### Scenario: Embedded onboarding docs accessible
- **WHEN** the embedded onboarding demo references packaged documentation for Q&A
- **THEN** the documentation files are embedded and accessible at runtime

#### Scenario: Embedded asset does not fall back to user directory
- **WHEN** an embedded onboarding workflow references a bundled asset and a user-authored file with the same relative path exists
- **THEN** the embedded asset is used and the user file is not consulted

#### Scenario: Bundled JSON data file embedded
- **WHEN** a namespace subdirectory contains a non-YAML data file referenced by a bundled workflow or asset
- **THEN** the file is embedded and accessible at runtime via its relative path within the namespace

#### Scenario: Top-level non-YAML files not exposed
- **WHEN** the repository's `workflows/` directory contains a non-YAML file at the top level
- **THEN** that file is not exposed as a bundled asset under any namespace

