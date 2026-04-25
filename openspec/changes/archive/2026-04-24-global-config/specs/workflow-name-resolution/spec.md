## MODIFIED Requirements

### Requirement: Workflow file resolution
The `run` command SHALL resolve workflow arguments against three disjoint sources, in this order:

1. **Namespaced names** (`<ns>:<name>`) SHALL resolve only against the embedded builtin workflow set, under the namespace `<ns>`. They SHALL NOT fall back to any on-disk location (project-local or global).
2. **Bare names** (with or without `/` path segments) SHALL resolve first against the user's project-local `.agent-runner/workflows/` directory in the current working directory. A name `a/b/c` SHALL resolve to `.agent-runner/workflows/a/b/c.yaml` (or `.yml`).
3. **Bare names not found project-local** SHALL then resolve against the user's global `~/.agent-runner/workflows/` directory (where `~` is the invoking user's home directory), using the same path-mapping rules. A bare name SHALL NOT resolve against any builtin.

Both `.yaml` and `.yml` extensions SHALL be tried, in that order, at each on-disk location before moving to the next location. If no matching file or embedded entry is found in any source, the command SHALL fail with a workflow-not-found error.

#### Scenario: Resolve bare name to user YAML file
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `.agent-runner/workflows/my-workflow.yaml` exists
- **THEN** the workflow is loaded from `.agent-runner/workflows/my-workflow.yaml`

#### Scenario: Resolve bare name to user YML file
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `.agent-runner/workflows/my-workflow.yaml` does not exist
- **AND** `.agent-runner/workflows/my-workflow.yml` exists
- **THEN** the workflow is loaded from `.agent-runner/workflows/my-workflow.yml`

#### Scenario: Resolve path-style name to nested user file
- **WHEN** the user runs `agent-runner run team/deploy`
- **AND** `.agent-runner/workflows/team/deploy.yaml` exists
- **THEN** the workflow is loaded from `.agent-runner/workflows/team/deploy.yaml`

#### Scenario: Resolve namespaced name to embedded builtin
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **THEN** the workflow is loaded from the embedded `core/finalize-pr` builtin

#### Scenario: Namespaced name does not fall back to disk
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **AND** no such embedded builtin exists
- **AND** a file `.agent-runner/workflows/core/finalize-pr.yaml` exists
- **THEN** the command fails with a workflow-not-found error; the on-disk file is not used

#### Scenario: Namespaced name does not fall back to global directory
- **WHEN** the user runs `agent-runner run core:finalize-pr`
- **AND** no such embedded builtin exists
- **AND** a file `~/.agent-runner/workflows/core/finalize-pr.yaml` exists
- **THEN** the command fails with a workflow-not-found error; the global file is not used

#### Scenario: Bare name does not fall back to builtins
- **WHEN** the user runs `agent-runner run finalize-pr`
- **AND** no `.agent-runner/workflows/finalize-pr.yaml` or `.yml` exists
- **AND** no `~/.agent-runner/workflows/finalize-pr.yaml` or `.yml` exists
- **AND** the binary contains an embedded `core:finalize-pr` builtin
- **THEN** the command fails with a workflow-not-found error; the builtin is not used

#### Scenario: Bare name falls back to global YAML file
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** neither `.agent-runner/workflows/my-workflow.yaml` nor `.agent-runner/workflows/my-workflow.yml` exists
- **AND** `~/.agent-runner/workflows/my-workflow.yaml` exists
- **THEN** the workflow is loaded from `~/.agent-runner/workflows/my-workflow.yaml`

#### Scenario: Bare name falls back to global YML file
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** no project-local match exists
- **AND** `~/.agent-runner/workflows/my-workflow.yaml` does not exist
- **AND** `~/.agent-runner/workflows/my-workflow.yml` exists
- **THEN** the workflow is loaded from `~/.agent-runner/workflows/my-workflow.yml`

#### Scenario: Project workflow shadows global workflow with same name
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** both `.agent-runner/workflows/my-workflow.yaml` and `~/.agent-runner/workflows/my-workflow.yaml` exist
- **THEN** the workflow is loaded from `.agent-runner/workflows/my-workflow.yaml`; the global file is not used

#### Scenario: Project path-style workflow shadows global workflow with same path
- **WHEN** the user runs `agent-runner run team/deploy`
- **AND** both `.agent-runner/workflows/team/deploy.yaml` and `~/.agent-runner/workflows/team/deploy.yaml` exist
- **THEN** the workflow is loaded from `.agent-runner/workflows/team/deploy.yaml`

#### Scenario: Resolve path-style name to nested global file
- **WHEN** the user runs `agent-runner run team/deploy`
- **AND** no project-local match exists
- **AND** `~/.agent-runner/workflows/team/deploy.yaml` exists
- **THEN** the workflow is loaded from `~/.agent-runner/workflows/team/deploy.yaml`

#### Scenario: Top-level workflows directory ignored
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `workflows/my-workflow.yaml` exists in the current working directory
- **AND** no `.agent-runner/workflows/my-workflow.yaml` or global match exists
- **THEN** the command fails with a workflow-not-found error

#### Scenario: Workflow not found in any source
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** neither `.agent-runner/workflows/my-workflow.{yaml,yml}` nor `~/.agent-runner/workflows/my-workflow.{yaml,yml}` exists
- **THEN** the command fails with an error like "Workflow 'my-workflow' not found"
