## MODIFIED Requirements

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
