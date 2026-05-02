## ADDED Requirements

### Requirement: Onboarding namespace embedded

The builtin set SHALL include an `onboarding` namespace alongside the existing `core`, `openspec`, and `spec-driven` namespaces. The `onboarding` namespace SHALL contain at minimum `welcome` (the top-level workflow invoked by the first-run dispatcher and by direct invocation) and `setup-agent-profile` (the sub-workflow used by Phase 2). Additional sub-workflows or bundled assets MAY be present.

#### Scenario: Onboarding workflows invoked by namespace
- **WHEN** the user runs `agent-runner run onboarding:welcome`
- **THEN** the workflow loads from the embedded `onboarding` namespace and executes

#### Scenario: Setup sub-workflow exists
- **WHEN** the embedded `onboarding:welcome` references `workflow: setup-agent-profile.yaml`
- **THEN** the sub-workflow loads from the embedded `onboarding/setup-agent-profile.yaml`

### Requirement: Non-YAML files embedded as bundled assets

Files in a namespace subdirectory whose names do not end in `.yaml` (e.g., `.sh` scripts and other data files referenced by workflows) SHALL be embedded as bundled assets and accessible at runtime via the relative paths declared by `script:` step fields. The embed mechanism SHALL preserve file mode bits relevant to execution (such as the executable bit) where the host filesystem records them. Asset path resolution SHALL stay within the namespace; the runner SHALL NOT fall back to user-authored workflows under `.agent-runner/workflows/` when an embedded workflow references a bundled asset.

#### Scenario: Embedded script asset accessible
- **WHEN** the embedded workflow `onboarding:setup-agent-profile` declares `script: detect-adapters.sh` and the file `onboarding/detect-adapters.sh` exists in the embedded set
- **THEN** the runner resolves and executes that bundled asset at runtime

#### Scenario: Embedded asset does not fall back to user directory
- **WHEN** the embedded workflow `onboarding:setup-agent-profile` declares `script: detect-adapters.sh` and a file `.agent-runner/workflows/onboarding/detect-adapters.sh` also exists on the user's disk
- **THEN** the embedded asset is used; the user file is not consulted

#### Scenario: Bundled JSON data file embedded
- **WHEN** a namespace subdirectory contains a non-YAML data file (e.g., `models.json`) referenced by a bundled script
- **THEN** the file is embedded and accessible at runtime via its relative path within the namespace

#### Scenario: Top-level non-YAML files not exposed
- **WHEN** the repository's `workflows/` directory contains a non-YAML file at the top level (not inside a namespace subdirectory)
- **THEN** that file is not exposed as a bundled asset under any namespace
