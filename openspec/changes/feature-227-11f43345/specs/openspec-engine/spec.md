## MODIFIED Requirements

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

## ADDED Requirements

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
