## MODIFIED Requirements

### Requirement: Embedded vs on-disk script resolution

When the containing workflow is part of the embedded builtin set, the runner SHALL resolve `script:` references only against the embedded namespace and SHALL NOT fall back to user-authored workflows under `.agent-runner/workflows/`. When the containing workflow is loaded from disk, the runner SHALL read the script from disk relative to the workflow file's directory.

#### Scenario: Embedded script resolves within embedded namespace
- **WHEN** an embedded workflow in the `onboarding` namespace declares `script: helper.sh`
- **THEN** the runner reads the script from the embedded `onboarding/helper.sh` and executes it

#### Scenario: Embedded script does not fall back to user directory
- **WHEN** an embedded workflow in the `onboarding` namespace declares `script: helper.sh` and a file `.agent-runner/workflows/onboarding/helper.sh` exists on the user's disk
- **THEN** the runner uses the embedded script, not the user file

#### Scenario: On-disk workflow reads script from disk
- **WHEN** a workflow loaded from `.agent-runner/workflows/foo/main.yaml` declares `script: helper.sh`
- **THEN** the runner executes `.agent-runner/workflows/foo/helper.sh`
