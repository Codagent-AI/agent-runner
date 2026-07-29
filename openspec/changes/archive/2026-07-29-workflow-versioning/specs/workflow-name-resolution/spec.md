## MODIFIED Requirements

### Requirement: Workflow name validation

The `run` command SHALL validate the workflow argument against the pattern `^[a-z0-9_-]+(:[a-z0-9_-]+|(/[a-z0-9_-]+)+)?$`. The argument is a version-free logical name in one of these forms:
- a bare name (e.g., `my-workflow`),
- a bare name with one or more path segments separated by `/` (e.g., `team/deploy`), or
- a namespaced name (e.g., `core:finalize-pr`) where the portion before the colon names a builtin namespace.

A name MUST NOT combine `/` and `:` in the same argument. A name MUST NOT contain uppercase letters, `.`, or any other character outside this set. Version-bearing names, file extensions, and filesystem paths MUST NOT be accepted for execution, even when they identify an existing workflow file. An invalid version-bearing argument SHALL produce guidance to use the version-free logical name so Agent Runner can select the latest version. Validation mode SHALL remain permitted to accept a versioned YAML file path without making that path executable as a new run.

#### Scenario: Argument contains a file extension
- **WHEN** the user runs `agent-runner run my-workflow-v1.0.yaml`
- **THEN** the command fails with an error that the workflow name is not valid for execution

#### Scenario: Existing versioned file path rejected for execution
- **WHEN** `./workflows/my-workflow-v1.0.yaml` exists and the user passes that path to start a run
- **THEN** the command rejects the path before workflow execution and instructs the user to launch the version-free logical name

#### Scenario: Version-bearing logical name rejected
- **WHEN** the user runs `agent-runner run my-workflow-v2.0`
- **THEN** the command rejects the argument and instructs the user to run `my-workflow`

#### Scenario: Uppercase logical name rejected
- **WHEN** the user runs `agent-runner run My-Workflow`
- **THEN** the command fails with an error that logical workflow names must be lowercase

#### Scenario: Bare name accepted
- **WHEN** the user runs `agent-runner run my-workflow`
- **THEN** the argument passes validation

#### Scenario: Bare name with subdirectory path accepted
- **WHEN** the user runs `agent-runner run team/deploy`
- **THEN** the argument passes validation

#### Scenario: Namespaced name accepted
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **THEN** the argument passes validation

#### Scenario: Name mixing path and namespace rejected
- **WHEN** the user runs `agent-runner run core:team/deploy`
- **THEN** the command fails with an error that the workflow name is not valid

#### Scenario: Leading slash rejected
- **WHEN** the user runs `agent-runner run /team/deploy`
- **THEN** the command fails with an error that the workflow name is not valid

#### Scenario: Validation accepts versioned file path
- **WHEN** the user passes an existing `my-workflow-v1.0.yaml` path through validation mode
- **THEN** Agent Runner validates that file without treating the path as permission to execute a new run

### Requirement: Workflow file resolution

The `run` command SHALL resolve version-free workflow arguments against three disjoint sources, in this order:

1. **Namespaced names** (`<ns>:<name>`) SHALL resolve only against the embedded builtin workflow set under namespace `<ns>`. They SHALL NOT fall back to any on-disk project or user location.
2. **Bare names** (with or without `/` path segments) SHALL resolve first against the user's project-local `.agent-runner/workflows/` directory in the current working directory.
3. **Bare names not represented in the project source** SHALL resolve against the user's global `~/.agent-runner/workflows/` directory. A bare name SHALL NOT resolve against any builtin.

Within the winning source, Agent Runner SHALL resolve the logical name to the valid candidate with the numerically highest major/minor version. Project-over-user precedence SHALL apply before version comparison, so a lower project version MUST shadow a higher user version. If the higher-precedence source contains an invalid definition belonging to the requested logical workflow group, resolution SHALL return that validation error and MUST NOT fall back to a lower-precedence source. An invalid definition for an unrelated logical workflow group MUST NOT block resolution.

Both `.yaml` and `.yml` versioned definitions SHALL be eligible. Duplicate logical-name/version pairs SHALL fail as defined by the `workflow-versioning` capability. If no matching valid or invalid logical group exists in any permitted source, the command SHALL fail with a workflow-not-found error.

When a syntactically valid logical-name lookup fails and the final name segment ends in a terminal `-v<digits>` attempt, the workflow-not-found error SHALL additionally suggest the version-free logical name. This hint MUST NOT prevent a real logical workflow whose name ends in `-v<digits>` from resolving normally when that group exists.

#### Scenario: Resolve bare name to user YAML file
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `.agent-runner/workflows/my-workflow-v1.0.yaml` and `.agent-runner/workflows/my-workflow-v1.2.yaml` exist
- **THEN** the workflow is loaded from `.agent-runner/workflows/my-workflow-v1.2.yaml`

#### Scenario: Resolve bare name to user YML file
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `.agent-runner/workflows/my-workflow-v1.0.yml` exists
- **THEN** the workflow is loaded from `.agent-runner/workflows/my-workflow-v1.0.yml`

#### Scenario: Resolve path-style name to nested user file
- **WHEN** the user runs `agent-runner run team/deploy`
- **AND** `.agent-runner/workflows/team/deploy-v2.9.yaml` and `.agent-runner/workflows/team/deploy-v2.10.yaml` exist
- **THEN** the workflow is loaded from `.agent-runner/workflows/team/deploy-v2.10.yaml`

#### Scenario: Resolve namespaced name to embedded builtin
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **AND** the embedded set contains `core/finalize-pr-v1.0.yaml` and `core/finalize-pr-v2.0.yaml`
- **THEN** the workflow is loaded from the embedded `core/finalize-pr-v2.0.yaml`

#### Scenario: Namespaced name does not fall back to disk
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **AND** no embedded `core:finalize-pr` logical workflow group exists
- **AND** `.agent-runner/workflows/core/finalize-pr-v1.0.yaml` exists
- **THEN** the command fails with a workflow-not-found error; the project file is not used

#### Scenario: Namespaced name does not fall back to global directory
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **AND** no embedded `core:finalize-pr` logical workflow group exists
- **AND** `~/.agent-runner/workflows/core/finalize-pr-v1.0.yaml` exists
- **THEN** the command fails with a workflow-not-found error; the global file is not used

#### Scenario: Bare name does not fall back to builtins
- **WHEN** the user runs `agent-runner run finalize-pr`
- **AND** no project or user `finalize-pr` logical workflow group exists
- **AND** the binary contains an embedded `core:finalize-pr` workflow
- **THEN** the command fails with a workflow-not-found error; the builtin is not used

#### Scenario: Bare name falls back to global YAML file
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** no project-local `my-workflow` group exists
- **AND** `~/.agent-runner/workflows/my-workflow-v1.0.yaml` and `~/.agent-runner/workflows/my-workflow-v1.3.yaml` exist
- **THEN** the workflow is loaded from `~/.agent-runner/workflows/my-workflow-v1.3.yaml`

#### Scenario: Bare name falls back to global YML file
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** no project-local `my-workflow` group exists
- **AND** `~/.agent-runner/workflows/my-workflow-v1.0.yml` exists
- **THEN** the workflow is loaded from `~/.agent-runner/workflows/my-workflow-v1.0.yml`

#### Scenario: Project workflow shadows global workflow with same name
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `.agent-runner/workflows/my-workflow-v1.0.yaml` exists
- **AND** `~/.agent-runner/workflows/my-workflow-v3.0.yaml` exists
- **THEN** the workflow is loaded from `.agent-runner/workflows/my-workflow-v1.0.yaml`

#### Scenario: Project path-style workflow shadows global workflow with same path
- **WHEN** the user runs `agent-runner run team/deploy`
- **AND** `.agent-runner/workflows/team/deploy-v1.0.yaml` exists
- **AND** `~/.agent-runner/workflows/team/deploy-v2.0.yaml` exists
- **THEN** the workflow is loaded from `.agent-runner/workflows/team/deploy-v1.0.yaml`

#### Scenario: Resolve path-style name to nested global file
- **WHEN** the user runs `agent-runner run team/deploy`
- **AND** no project-local `team/deploy` group exists
- **AND** `~/.agent-runner/workflows/team/deploy-v1.0.yaml` exists
- **THEN** the workflow is loaded from `~/.agent-runner/workflows/team/deploy-v1.0.yaml`

#### Scenario: Invalid project workflow blocks global fallback
- **WHEN** the user runs `agent-runner run deploy`
- **AND** `.agent-runner/workflows/deploy.yaml` exists
- **AND** `~/.agent-runner/workflows/deploy-v3.0.yaml` exists
- **THEN** the command fails with the actionable versioned-filename error for the project file and does not load the global workflow

#### Scenario: Unrelated invalid project workflow does not block global resolution
- **WHEN** the project source contains invalid `verify.yaml` but no `deploy` group
- **AND** the user source contains valid `deploy-v1.0.yaml`
- **AND** the user runs `agent-runner run deploy`
- **THEN** the workflow is loaded from the user source while the unrelated `verify` group remains invalid

#### Scenario: Top-level workflows directory ignored
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `workflows/my-workflow-v1.0.yaml` exists in the current working directory
- **AND** no project-local or global `my-workflow` group exists
- **THEN** the command fails with a workflow-not-found error

#### Scenario: Workflow not found in any source
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** no project-local or global `my-workflow` group exists
- **THEN** the command fails with an error identifying logical workflow `my-workflow` and the permitted sources that were searched

#### Scenario: Dotless version attempt receives logical-name hint
- **WHEN** the user runs `agent-runner run deploy-v1`
- **AND** no logical workflow group named `deploy-v1` exists
- **THEN** the workflow-not-found error additionally suggests running logical workflow `deploy`

#### Scenario: Logical name ending in dotless version text remains valid
- **WHEN** a workflow group named `deploy-v1` exists and the user runs `agent-runner run deploy-v1`
- **THEN** Agent Runner resolves and launches that logical group normally without rewriting the name to `deploy`
