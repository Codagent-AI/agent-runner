# Capability: workflow-name-resolution

## Purpose

Validates and resolves bare workflow names passed to the `run` command, rejecting file paths and extensions in favor of simple names that are resolved to files in the `workflows/` directory.

## Requirements

### Requirement: Workflow name validation
The `run` command SHALL validate the workflow argument against the pattern `^[a-zA-Z0-9_-]+(:[a-zA-Z0-9_-]+)?$`. The argument is either a bare name (e.g., `my-workflow`) or a namespaced name (e.g., `openspec:plan-change`) where the portion before the colon names a subdirectory of `workflows/`. If the argument contains any character outside this set (including `/` and `.`), the command SHALL reject it with an error indicating the workflow name is not valid.

#### Scenario: Argument contains a slash
- **WHEN** the user runs `baton run workflows/my-workflow.yaml`
- **THEN** the command fails with an error that the workflow name is not valid

#### Scenario: Argument contains a dot
- **WHEN** the user runs `baton run my-workflow.yaml`
- **THEN** the command fails with an error that the workflow name is not valid

#### Scenario: Bare name accepted
- **WHEN** the user runs `baton run my-workflow`
- **THEN** the argument passes validation

#### Scenario: Name with hyphens and underscores accepted
- **WHEN** the user runs `baton run plan-change`
- **THEN** the argument passes validation

#### Scenario: Namespaced name accepted
- **WHEN** the user runs `baton run openspec:plan-change`
- **THEN** the argument passes validation

### Requirement: Workflow file resolution
The `run` command SHALL resolve a workflow name to a file path by looking in the `workflows/` directory, trying both `.yaml` and `.yml` extensions. A namespaced name `<ns>:<name>` SHALL resolve under `workflows/<ns>/`.

#### Scenario: Resolve bare name to YAML file
- **WHEN** the user runs `baton run my-workflow`
- **AND** `workflows/my-workflow.yaml` exists
- **THEN** the workflow is loaded from `workflows/my-workflow.yaml`

#### Scenario: Resolve bare name to YML file
- **WHEN** the user runs `baton run my-workflow`
- **AND** `workflows/my-workflow.yaml` does not exist
- **AND** `workflows/my-workflow.yml` exists
- **THEN** the workflow is loaded from `workflows/my-workflow.yml`

#### Scenario: Resolve namespaced name to YAML file
- **WHEN** the user runs `baton run openspec:plan-change`
- **AND** `workflows/openspec/plan-change.yaml` exists
- **THEN** the workflow is loaded from `workflows/openspec/plan-change.yaml`

#### Scenario: Workflow not found
- **WHEN** the user runs `baton run my-workflow`
- **AND** neither `workflows/my-workflow.yaml` nor `workflows/my-workflow.yml` exists
- **THEN** the command fails with an error like "Workflow 'my-workflow' not found"
