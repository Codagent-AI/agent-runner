## ADDED Requirements

### Requirement: Bare name validation
The `run` command SHALL validate the workflow argument against the pattern `^[a-zA-Z0-9_-]+$`. If the argument contains any character outside this set (including `/` and `.`), the command SHALL reject it with an error indicating the workflow name is not valid.

#### Scenario: Argument contains a slash
- **WHEN** the user runs `baton run workflows/flokay.yaml`
- **THEN** the command fails with an error that the workflow name is not valid

#### Scenario: Argument contains a dot
- **WHEN** the user runs `baton run flokay.yaml`
- **THEN** the command fails with an error that the workflow name is not valid

#### Scenario: Bare name accepted
- **WHEN** the user runs `baton run flokay`
- **THEN** the argument passes validation

#### Scenario: Name with hyphens and underscores accepted
- **WHEN** the user runs `baton run plan-change`
- **THEN** the argument passes validation

### Requirement: Workflow file resolution
The `run` command SHALL resolve a bare workflow name to a file path by looking in the `workflows/` directory, trying both `.yaml` and `.yml` extensions.

#### Scenario: Resolve bare name to YAML file
- **WHEN** the user runs `baton run flokay`
- **AND** `workflows/flokay.yaml` exists
- **THEN** the workflow is loaded from `workflows/flokay.yaml`

#### Scenario: Resolve bare name to YML file
- **WHEN** the user runs `baton run flokay`
- **AND** `workflows/flokay.yaml` does not exist
- **AND** `workflows/flokay.yml` exists
- **THEN** the workflow is loaded from `workflows/flokay.yml`

#### Scenario: Workflow not found
- **WHEN** the user runs `baton run flokay`
- **AND** neither `workflows/flokay.yaml` nor `workflows/flokay.yml` exists
- **THEN** the command fails with an error like "Workflow 'flokay' not found"
