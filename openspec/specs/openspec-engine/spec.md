## ADDED Requirements

### Requirement: State directory resolution

The openspec engine SHALL implement `getStateDir(params)` to return the openspec change directory as the state file location. The change name SHALL be read from the param specified by `engine.change_param` in the workflow config.

#### Scenario: State dir resolves to change directory
- **WHEN** the workflow has `engine.change_param: change_name` and params has `change_name: "my-change"`
- **THEN** `getStateDir` returns `openspec/changes/my-change/` and baton writes `baton-state.json` there

#### Scenario: Change param missing from params
- **WHEN** `getStateDir` is called but the param specified by `change_param` is not in params
- **THEN** the engine fails with a descriptive error naming the missing param

### Requirement: Workflow validation via schema matching

The openspec engine SHALL implement `validateWorkflow` to verify that every artifact ID in the openspec schema has a step with a matching ID in the workflow. The match SHALL be exact by name.

#### Scenario: All artifacts have matching steps
- **WHEN** the openspec schema has artifacts `proposal`, `specs`, `design`, `tasks`, `review` and the workflow has steps with those same IDs
- **THEN** validation passes

#### Scenario: Artifact missing a matching step
- **WHEN** the openspec schema has artifact `proposal` but no workflow step has ID `proposal`
- **THEN** validation fails with an error listing the unmatched artifact IDs

#### Scenario: Extra steps without matching artifacts
- **WHEN** the workflow has steps `create`, `implement`, `verify`, `finalize` that don't match any artifact ID
- **THEN** validation passes — extra non-artifact steps are allowed

### Requirement: Prompt enrichment via openspec instructions

The openspec engine SHALL implement `enrichPrompt` by calling `openspec instructions <step-id> --change "<name>" --json` (using the step ID as the artifact ID) and prepending template, output path, and dependencies to the step's prompt. The `instruction` field from the openspec output SHALL be excluded since the prompt already invokes the appropriate skill.

#### Scenario: Enrichment prepends artifact context
- **WHEN** a step with ID `proposal` is executed and the engine calls `enrichPrompt`
- **THEN** the engine calls `openspec instructions proposal --change "<name>" --json`, and prepends an `<artifact_context>` block containing `<output_path>` (absolute, joined from changeDir + outputPath), `<dependencies>` (absolute paths with descriptions), and `<template>` (full template content)

#### Scenario: Openspec CLI call fails
- **WHEN** `openspec instructions` returns a non-zero exit code
- **THEN** the engine fails with the CLI's error message

#### Scenario: Dependency paths are absolute
- **WHEN** the openspec output includes dependencies with relative paths
- **THEN** the engine joins each dependency path with `changeDir` to produce absolute paths in the enrichment block

### Requirement: Step validation via openspec status

The openspec engine SHALL implement `validateStep` by calling `openspec status --change "<name>" --json` and checking whether the artifact matching the step ID has status `done`.

#### Scenario: Artifact status is done
- **WHEN** after a step completes, `openspec status` reports the step's artifact as `done`
- **THEN** validation passes

#### Scenario: Artifact status is not done
- **WHEN** after a step completes, `openspec status` reports the step's artifact as `ready` or `blocked`
- **THEN** validation fails (triggering baton's resume-or-exit prompt)

#### Scenario: Openspec CLI call fails during validation
- **WHEN** `openspec status` returns a non-zero exit code
- **THEN** validation fails with the CLI's error message

### Requirement: Engine configuration

The openspec engine SHALL require only `change_param` in its engine config block, specifying which workflow param holds the openspec change name.

#### Scenario: Minimal engine config
- **WHEN** a workflow has `engine: { type: openspec, change_param: change_name }`
- **THEN** the engine initializes successfully using `change_name` to resolve the change

#### Scenario: Missing change_param config
- **WHEN** a workflow has `engine: { type: openspec }` without `change_param`
- **THEN** engine initialization fails with a descriptive error
