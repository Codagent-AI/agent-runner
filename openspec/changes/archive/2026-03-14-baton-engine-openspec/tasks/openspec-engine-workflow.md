# Task: OpenSpec engine and workflow update

## Goal

Implement the openspec engine — the first concrete engine for baton — and update the flokay workflow file to use it. The engine shells out to the `openspec` CLI to enrich prompts with templates/dependencies, validate step completion, provide a custom state directory, and validate workflow structure against the openspec schema.

## Background

Baton has a working engine framework: the `Engine` interface in `src/engine.ts`, a hardcoded registry (currently empty) via `createEngine`, a state file module in `src/state.ts`, and runner integration in `src/runner.ts` that calls engine hooks. This task adds the first concrete engine.

**OpenSpec engine** — Implemented in `src/engines/openspec.ts`. The engine is constructed with opaque config from the workflow's `engine` block. It requires one config field: `change_param` — the name of the workflow param that holds the openspec change name. At construction, validate that `change_param` is present (throw if missing) and that the `openspec` CLI is available (run `which openspec`, throw if not found).

At init time, the engine also loads the openspec schema's artifact IDs by running `openspec status --change "<name>" --json` (it needs to defer this until params are available — see enrichPrompt/validateStep). The artifact ID set determines which step IDs the engine manages.

**Implemented hooks:**

`getStateDir(params)` — Returns `openspec/changes/<change_name>/` where `<change_name>` comes from `params[this.changeParam]`. Throws if the param is missing.

`validateWorkflow(workflow)` — Runs `openspec status --change "<name>" --json` to get the schema's artifact IDs, then verifies every artifact ID has a matching step ID in the workflow. Throws with the list of unmatched artifact IDs if any are missing. Extra steps without matching artifacts are allowed.

`enrichPrompt(stepId, params)` — If `stepId` is not in the artifact ID set, return `undefined`. Otherwise run `openspec instructions <stepId> --change "<name>" --json` and build an enrichment block wrapped in `<artifact_context>` tags. The block MUST contain three sections: `<output_path>` with the absolute artifact path (join `changeDir` + `outputPath`), `<dependencies>` listing each dependency as an absolute path with its description, and `<template>` containing the full template content from the openspec output. The `instruction` field from the openspec output SHALL be excluded — the prompt already invokes the skill. All paths MUST be absolute: join `changeDir` (absolute, from openspec output) with `outputPath` (relative) and with each dependency's `path` (relative).

`validateStep(stepId, params)` — If `stepId` is not in the artifact ID set, return `true`. Otherwise run `openspec status --change "<name>" --json`, find the artifact matching `stepId`, and return `true` if its status is `"done"`, `false` otherwise.

**CLI interaction** — All openspec CLI calls use `Bun.spawn` (or equivalent subprocess). Parse stdout as JSON. If the command exits non-zero, throw with stderr content.

**Registry update** — Add `openspec: OpenSpecEngine` to the registry map in `src/engine.ts`.

**Workflow update** — Update `workflows/flokay.yaml`:
- Add `engine` block: `engine: { type: openspec, change_param: change_name }`
- Rename step `propose` to `proposal` (matches the openspec artifact ID)

**Key files:**
- `src/engines/openspec.ts` — new file: OpenSpecEngine class
- `src/engine.ts` — add openspec to registry
- `workflows/flokay.yaml` — add engine block, rename propose→proposal
- `test/engines/openspec.test.ts` — unit tests for all engine hooks

## Spec

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

## Done When

- `src/engines/openspec.ts` implements all four hooks
- openspec is registered in the engine registry
- `workflows/flokay.yaml` has the engine block and step IDs match artifact IDs
- Unit tests cover: construction (valid/missing config, missing CLI), getStateDir, validateWorkflow (matching/unmatched artifacts), enrichPrompt (enrichment format, CLI failure, non-artifact step), validateStep (done/not-done, CLI failure, non-artifact step)
- All tests pass via `bun test`
