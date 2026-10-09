# openspec-engine Specification

## Purpose
Define how the OpenSpec workflow engine resolves changes, validates workflows, enriches prompts, and tracks artifact state.
## Requirements
### Requirement: Change parameter resolution

The openspec engine SHALL read the change name from the param specified by `engine.change_param` in the workflow config. The engine SHALL use that change name when invoking `openspec status` and `openspec instructions`.

#### Scenario: Change param resolves
- **WHEN** the workflow has `engine.change_param: change_name` and params has `change_name: "my-change"`
- **THEN** the engine uses `my-change` as the change name for OpenSpec CLI calls

#### Scenario: Change param missing from params
- **WHEN** an OpenSpec engine hook is called but the param specified by `change_param` is not in params
- **THEN** the engine fails with a descriptive error naming the missing param

### Requirement: Workflow validation via schema matching

The openspec engine SHALL implement `ValidateWorkflow` to verify that every artifact ID in the openspec schema has a step with a matching ID in the workflow. The match SHALL be exact by name.

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

The openspec engine SHALL implement `EnrichPrompt` by calling `openspec instructions <step-id> --change "<name>" --json` (using the step ID as the artifact ID) and prepending template, output path, and dependencies to the step's prompt. The `instruction` field from the openspec output SHALL be excluded since the prompt already invokes the appropriate skill.

#### Scenario: Enrichment prepends artifact context
- **WHEN** a step with ID `proposal` is executed and the engine calls `EnrichPrompt`
- **THEN** the engine calls `openspec instructions proposal --change "<name>" --json`, and prepends an `<artifact_context>` block containing `<output_path>` (absolute, joined from changeDir + outputPath), `<dependencies>` (absolute paths with descriptions), and `<template>` (full template content)

#### Scenario: Openspec CLI call fails
- **WHEN** `openspec instructions` returns a non-zero exit code
- **THEN** the engine fails with the CLI's error message

#### Scenario: Dependency paths are absolute
- **WHEN** the openspec output includes dependencies with relative paths
- **THEN** the engine joins each dependency path with `changeDir` to produce absolute paths in the enrichment block

### Requirement: Step validation via openspec status

The openspec engine SHALL implement `ValidateStep` by calling `openspec status --change "<name>" --json` and checking whether the artifact matching the step ID has status `done`.

#### Scenario: Artifact status is done
- **WHEN** after a step completes, `openspec status` reports the step's artifact as `done`
- **THEN** validation passes

#### Scenario: Artifact status is not done
- **WHEN** after a step completes, `openspec status` reports the step's artifact as `ready` or `blocked`
- **THEN** validation fails (triggering Agent Runner's resume-or-exit prompt)

#### Scenario: Openspec CLI call fails during validation
- **WHEN** `openspec status` returns a non-zero exit code
- **THEN** validation fails with the CLI's error message

### Requirement: Engine configuration

The openspec engine SHALL require `change_param` in its engine config block, specifying which workflow param holds the openspec change name. The engine SHALL also accept an optional `root_param` that names the workflow param holding the absolute OpenSpec spec root. When `root_param` is absent, the engine SHALL use the process working directory as it does today.

#### Scenario: Minimal engine config
- **WHEN** a workflow has `engine: { type: openspec, change_param: change_name }`
- **THEN** the engine initializes successfully using `change_name` to resolve the change

#### Scenario: Missing change_param config
- **WHEN** a workflow has `engine: { type: openspec }` without `change_param`
- **THEN** engine initialization fails with a descriptive error

#### Scenario: Root param configured
- **WHEN** a workflow has `engine: { type: openspec, change_param: change_name, root_param: spec_root }`
- **THEN** the engine initializes successfully and uses the `spec_root` param as its OpenSpec working directory

### Requirement: OpenSpec CLI working directory

When `root_param` is configured, every `openspec` CLI call the engine makes, including the calls behind `ValidateWorkflow`, `EnrichPrompt`, and `ValidateStep`, SHALL run with the named param's value as its working directory, both at startup and on resume. The engine SHALL bind the directory once, from the params of the workflow that declares it, when that workflow starts or resumes and before any of its steps run. Engines inherited by nested sub-workflows SHALL keep that binding. If `root_param` is configured but that param is missing or empty, or names a directory that does not exist, the declaring workflow SHALL fail to start with a descriptive error naming the param, before any `openspec` call and before any agent step spawns. It SHALL NOT fall back to the process working directory, and it SHALL NOT silently skip enrichment. Absolute output and dependency paths in the enrichment block SHALL resolve under that directory.

#### Scenario: Startup validation runs in the spec root
- **WHEN** a workflow configures `root_param: spec_root` with `spec_root=/work/specs` and the runner calls `ValidateWorkflow` before the first step
- **THEN** the engine's `openspec status` call runs with `/work/specs` as its working directory

#### Scenario: Enrichment paths resolve under the spec root
- **WHEN** `EnrichPrompt` runs for step `proposal` with `spec_root=/work/specs` and change `foo`
- **THEN** the `<output_path>` in the enrichment block is under `/work/specs/openspec/changes/foo/`

#### Scenario: Root param missing from params
- **WHEN** a workflow configures `root_param: spec_root` and params contain no `spec_root`
- **THEN** the workflow fails to start with an error naming `spec_root`, runs no `openspec` command, and spawns no agent

#### Scenario: Nested workflow inherits the bound root
- **WHEN** an engine-bearing workflow bound to `spec_root=/work/specs` invokes a nested sub-workflow whose params do not include `spec_root`, and that nested workflow runs an agent step with an artifact ID
- **THEN** the engine's enrichment call for that step runs in `/work/specs`

