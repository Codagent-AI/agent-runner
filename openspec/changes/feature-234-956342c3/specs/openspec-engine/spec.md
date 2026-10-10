## ADDED Requirements

### Requirement: Managed step classification

The openspec engine SHALL decide whether it manages a step using only the step ID and its own engine configuration. Classification MUST NOT read workflow params and MUST NOT invoke the `openspec` CLI. The engine manages a step when either of these holds:
- the step ID is a key in the engine's `artifact_steps` configuration, or
- the step ID is one of the default `spec-driven` artifact IDs (`proposal`, `specs`, `design`, `tasks`) and is not overridden by an `artifact_steps` entry.

Every other step is unmanaged.

#### Scenario: Default artifact step is managed
- **WHEN** a step with ID `proposal` runs under an openspec engine with no `artifact_steps` configuration
- **THEN** the engine classifies the step as managed for artifact `proposal`

#### Scenario: Mapped step is managed
- **WHEN** a step with ID `plan` runs under an openspec engine whose `artifact_steps` maps `plan` to planning artifacts
- **THEN** the engine classifies the step as managed for the mapped artifacts

#### Scenario: Unrelated step is unmanaged
- **WHEN** a step with ID `push-pr` runs under an inherited openspec engine
- **THEN** the engine classifies the step as unmanaged

#### Scenario: Classification needs no change param
- **WHEN** an unmanaged step runs in a sub-workflow whose params do not include the engine's change param
- **THEN** classification succeeds without error, and no `openspec` command is run

#### Scenario: Classification after the change is archived
- **WHEN** an unmanaged step runs after the archive step has moved the change out of `openspec/changes/`
- **THEN** classification succeeds without error, and no `openspec` command is run

### Requirement: Multi-artifact step mapping

The openspec engine config SHALL accept an optional `artifact_steps` mapping. It maps a step ID to a set of artifact IDs and marks each artifact as either required or optional. A mapped step's enrichment SHALL deliver context for all of its mapped artifacts. Only the required artifacts SHALL be validated after the step. The engine MUST NOT tell the agent to create an optional artifact. Engine initialization SHALL fail with a descriptive error when `artifact_steps` is malformed, when a mapped step lists no artifacts, or when it lists no required artifact.

Each `artifact_steps` entry SHALL be a mapping with a non-empty `required` list and an optional `optional` list of artifact IDs. Any other key, any non-string or empty artifact ID, and any artifact ID listed twice in one entry SHALL fail engine initialization.

#### Scenario: Optional artifact omitted
- **WHEN** a mapped step lists `design` as optional and the agent finishes without writing `design.md`
- **THEN** step validation passes as long as every required artifact is `done`

#### Scenario: Optional artifact written
- **WHEN** a mapped step lists `design` as optional
- **THEN** the step's enrichment includes the `design` output path and rules, clearly labeled optional, so they apply if the agent chooses to write a design

#### Scenario: Required artifact missing
- **WHEN** a mapped step lists `tasks` as required and the agent finishes without `openspec status` reporting `tasks` as `done`
- **THEN** step validation fails and names `tasks` as the artifact that is not done

#### Scenario: Malformed mapping
- **WHEN** a workflow's openspec engine config has an `artifact_steps` entry with no required artifact
- **THEN** engine initialization fails with a descriptive error before any step runs

## MODIFIED Requirements

### Requirement: Change parameter resolution

The openspec engine SHALL read the change name from the param specified by `engine.change_param` in the workflow config. The engine SHALL use that change name when invoking `openspec status` and `openspec instructions`. The change name SHALL be resolved only for managed steps and for workflow validation, never for unmanaged steps.

#### Scenario: Change param resolves
- **WHEN** the workflow has `engine.change_param: change_name` and params has `change_name: "my-change"`
- **THEN** the engine uses `my-change` as the change name for OpenSpec CLI calls

#### Scenario: Change param missing from params
- **WHEN** an OpenSpec engine hook is called for a managed step but the param specified by `change_param` is not in params
- **THEN** the engine fails with a descriptive error naming the missing param

#### Scenario: Change param missing for an unmanaged step
- **WHEN** an unmanaged step runs and the param specified by `change_param` is not in params
- **THEN** the engine does not fail, because it never resolves the change name for that step

### Requirement: Workflow validation via schema matching

The openspec engine SHALL implement `ValidateWorkflow` to verify that every artifact ID in the openspec schema is covered by the workflow. An artifact is covered when a step in the workflow effectively covers it. A step with an `artifact_steps` entry covers exactly the artifacts that entry lists (required or optional), even when the step ID is itself an artifact ID. A step with no `artifact_steps` entry covers the artifact whose ID exactly matches the step ID. When the workflow delegates to sub-workflows, unmatched artifacts SHALL NOT be reported, because they may be covered by a sub-workflow.

#### Scenario: All artifacts have matching steps
- **WHEN** the openspec schema has artifacts `proposal`, `specs`, `design`, `tasks`, `review` and the workflow has steps with those same IDs
- **THEN** validation passes

#### Scenario: Artifact missing a matching step
- **WHEN** the openspec schema has artifact `proposal`, no workflow step has ID `proposal`, no `artifact_steps` entry covers it, and the workflow has no sub-workflow steps
- **THEN** validation fails with an error listing the unmatched artifact IDs

#### Scenario: Artifacts covered by a mapped step
- **WHEN** the openspec schema has artifacts `proposal`, `specs`, `design`, `tasks` and `artifact_steps` maps the workflow's `plan` step to all four
- **THEN** validation passes

#### Scenario: Overridden step does not cover its own name
- **WHEN** the workflow has a step `proposal` whose `artifact_steps` entry lists only `tasks`, no other step covers `proposal`, and the workflow has no sub-workflow steps
- **THEN** validation fails and lists `proposal` as unmatched

#### Scenario: Extra steps without matching artifacts
- **WHEN** the workflow has steps `create`, `implement`, `verify`, `finalize` that don't match any artifact ID
- **THEN** validation passes — extra non-artifact steps are allowed

### Requirement: Prompt enrichment via openspec instructions

For a managed step, the openspec engine SHALL implement `EnrichPrompt` by calling `openspec instructions <artifact-id> --change "<name>" --json` for each artifact the step covers. It SHALL return an enrichment block containing:
- the artifact's resolved output path;
- its dependencies as absolute paths with descriptions, except when the step resumes or inherits a session, and except dependencies on artifacts that the same step covers;
- the project `context`, when present;
- the artifact's `rules`, when present.

The OpenSpec `instruction` and `template` fields SHALL be excluded, because the step prompt's skill owns document structure. For a step that covers several artifacts, the project context SHALL appear once and each artifact's output path and rules SHALL be listed under that artifact, with optional artifacts labeled optional. Dependencies of a multi-artifact step on artifacts outside its mapping SHALL be listed once each, with absolute paths and descriptions.

If any `openspec instructions` call fails or returns output that cannot be parsed, `EnrichPrompt` SHALL return an error that names the artifact and includes the CLI or parse error. It MUST NOT return empty enrichment in that case.

#### Scenario: Enrichment delivers project context and rules
- **WHEN** a step with ID `proposal` is executed, and `openspec/config.yaml` defines `context` and a `rules.proposal` list
- **THEN** the enrichment block includes the absolute output path for `proposal.md`, the project context, and every `proposal` rule

#### Scenario: OpenSpec authoring instruction and template excluded
- **WHEN** a step with ID `tasks` is enriched
- **THEN** the enrichment block contains neither OpenSpec's `tasks` instruction text nor its checkbox template

#### Scenario: No context or rules configured
- **WHEN** a managed step is enriched and the project defines no `context` and no rules for the artifact
- **THEN** the enrichment block contains the output path and dependencies, with no empty context or rules sections

#### Scenario: Openspec CLI call fails
- **WHEN** `openspec instructions` returns a non-zero exit code for a managed step
- **THEN** `EnrichPrompt` returns an error naming the artifact and including the CLI's error message

#### Scenario: Unparseable instructions output
- **WHEN** `openspec instructions` succeeds but prints output that is not valid instructions JSON
- **THEN** `EnrichPrompt` returns an error naming the artifact

#### Scenario: One artifact of a mapped step fails
- **WHEN** a step mapped to `proposal`, `specs`, `design`, and `tasks` is enriched and `openspec instructions` fails only for `specs`
- **THEN** `EnrichPrompt` returns an error naming `specs`, and no partial enrichment is returned

#### Scenario: Mapped step keeps outside dependencies
- **WHEN** a fresh-session step is mapped to `specs` and `design`, and both depend on `proposal`
- **THEN** the enrichment lists the absolute `proposal.md` path once as a dependency, and lists neither `specs` nor `design` as a dependency

#### Scenario: Dependency paths are absolute
- **WHEN** the openspec output includes dependencies with relative paths
- **THEN** the engine joins each dependency path with `changeDir` to produce absolute paths in the enrichment block

### Requirement: Step validation via openspec status

For a managed step, the openspec engine SHALL implement `ValidateStep` by calling `openspec status --change "<name>" --json` and checking that every required artifact covered by the step has status `done`. For a step with no `artifact_steps` entry whose ID is a default artifact ID, that artifact is required. For a step with an `artifact_steps` entry, only that entry's required artifacts are checked, even when the step ID is itself an artifact ID. When validation fails, the result SHALL name each required artifact that is not done.

#### Scenario: Artifact status is done
- **WHEN** after a step completes, `openspec status` reports the step's artifact as `done`
- **THEN** validation passes

#### Scenario: Artifact status is not done
- **WHEN** after a step completes, `openspec status` reports the step's artifact as `ready` or `blocked`
- **THEN** validation fails and names that artifact

#### Scenario: Override replaces the step's own artifact
- **WHEN** a step `proposal` has an `artifact_steps` entry requiring only `tasks`, and after it completes `tasks` is `done` but `proposal` is not
- **THEN** validation passes

#### Scenario: Openspec CLI call fails during validation
- **WHEN** `openspec status` returns a non-zero exit code
- **THEN** validation fails with the CLI's error message
