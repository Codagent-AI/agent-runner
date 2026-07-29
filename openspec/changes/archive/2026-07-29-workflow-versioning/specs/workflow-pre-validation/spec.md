## MODIFIED Requirements

### Requirement: When pre-validation runs

Pre-validation SHALL run before any step executes and before any TUI is launched, in two contexts:

1. **Fresh runs**: every `agent-runner <logical-workflow> [params...]` invocation, after logical-name resolution and parameter matching and before `runner.PrepareRun` or `runner.RunWorkflow` is called, for workflows that are not skipped by the path-based skip rule below. Fresh runs use **strict mode**: any unresolved `{{paramName}}` reference in a sub-workflow path is an error (since `matchParams` has bound all required params before pre-validation runs).
2. **Explicit validate**: every `agent-runner --validate <workflow-or-path> [key=value...]` invocation, **unconditionally** (the skip rule does not apply). The argument MAY be a version-free workflow name resolved to its latest version or an existing exact versioned `.yaml` / `.yml` file path. Params are optional; missing required params are non-fatal at validate time. `--validate` uses **lenient mode**: a sub-workflow path whose `{{paramName}}` reference cannot be resolved with the supplied params (and has no parameter default) emits a deferred warning ("target depends on unbound param X; checked at run time") rather than an error. Captured-variable references in sub-workflow paths still fail hard.

Pre-validation SHALL NOT run on `agent-runner --resume` invocations; resume continues to load only the exact top-level version recorded in state via `loader.LoadWorkflow`.

#### Scenario: Fresh run pre-validates before TUI launch
- **WHEN** `agent-runner my-workflow` is invoked and `my-workflow` is not skipped by the skip rule
- **THEN** the latest resolved version is pre-validated to completion before the live TUI is created and before any audit-log entries are written

#### Scenario: Pre-validation failure prevents run start
- **WHEN** pre-validation fails for a fresh run
- **THEN** the runner exits non-zero, prints the structured error to stderr, does not launch the TUI, and does not write any audit entries

#### Scenario: --validate accepts a workflow name
- **WHEN** `agent-runner --validate my-workflow` is invoked
- **THEN** the logical argument is resolved to its latest version and the same pre-validation pipeline runs in lenient mode, printing `workflow is valid` on success or the structured error on failure

#### Scenario: --validate accepts a YAML file path
- **WHEN** `agent-runner --validate workflows/core/finalize-pr-v1.0.yaml` is invoked and that file exists
- **THEN** the argument is treated as a literal exact file path and the pre-validation pipeline runs against that version in lenient mode

#### Scenario: --validate ignores the skip rule
- **WHEN** `agent-runner --validate core:finalize-pr` is invoked (a builtin that fresh runs would skip)
- **THEN** the full pre-validation pipeline still runs against the latest embedded version

#### Scenario: --validate with optional params binds them
- **WHEN** `agent-runner --validate my-workflow flavor=green` is invoked and `my-workflow` has a sub-workflow path `workflow: "workflows/{{flavor}}-v1.0.yaml"`
- **THEN** the pre-validation pipeline resolves the path to `workflows/green-v1.0.yaml` and validates it as in fresh-run mode

#### Scenario: --validate without supplied params produces deferred warnings
- **WHEN** `agent-runner --validate my-workflow` is invoked with no params and `my-workflow` has a sub-workflow path `workflow: "workflows/{{flavor}}-v1.0.yaml"` where `flavor` is a required workflow param with no default
- **THEN** pre-validation emits a deferred warning naming the step and the unbound param, validates everything else, and exits zero if no other failures exist

#### Scenario: --validate still fails on captured-variable workflow paths
- **WHEN** `agent-runner --validate my-workflow` is invoked and `my-workflow` has `workflow: "workflows/{{captured_target}}-v1.0.yaml"` where `captured_target` is captured at runtime
- **THEN** pre-validation fails with the captured-variable error regardless of mode

#### Scenario: Resume does not pre-validate
- **WHEN** `agent-runner --resume <session-id>` is invoked
- **THEN** the runner loads only the exact top-level workflow version recorded in state via the existing loader and does not run the pre-validation pipeline

### Requirement: Skip rule for fresh runs (builtins only)

For fresh runs only, pre-validation SHALL be skipped when the resolved latest workflow path is an embedded builtin workflow (any path matched by `builtinworkflows.IsRef`).

Every other resolved workflow path — including versioned workflows under `<cwd>/.agent-runner/workflows/`, versioned workflows under `~/.agent-runner/workflows/`, and any other resolution outcome — SHALL pre-validate on every fresh run.

The skip rule rests on the assumption that builtins are validated at the agent-runner repo's build time by a "validate all builtins" mechanism scoped to that project — preferably a Go test that iterates every embedded workflow version and runs the pre-validation pipeline on each, alternatively a hidden / dev-only CLI flag. The mechanism does not need to ship in the released binary. Downstream projects MAY configure an analogous author-time check on their own `.agent-runner/workflows/` (e.g., wiring `agent-runner --validate <versioned-relpath>` into CI), but the runner does NOT skip pre-validation on the assumption that they did. `--validate` does not honor the skip rule.

#### Scenario: Builtin invocation skips pre-validation
- **WHEN** `agent-runner core:finalize-pr` is invoked
- **THEN** pre-validation is skipped and the runner proceeds directly to `runner.PrepareRun` with the resolved latest embedded version

#### Scenario: Project workflow invocation pre-validates
- **WHEN** the cwd is a project root containing `.agent-runner/workflows/deploy-v1.0.yaml` and `agent-runner deploy` is invoked
- **THEN** pre-validation runs the full pipeline before the run starts

#### Scenario: Global user workflow invocation pre-validates
- **WHEN** the cwd contains no `.agent-runner/workflows/scratch-v1.0.yaml` but `~/.agent-runner/workflows/scratch-v1.0.yaml` exists, and `agent-runner scratch` is invoked
- **THEN** pre-validation runs the full pipeline before the run starts

### Requirement: Bound-parameter sub-workflow resolution

Sub-workflow `workflow:` fields that contain `{{paramName}}` interpolation SHALL be resolved during pre-validation using the values bound at the start of validation, and the resolved target SHALL be validated as part of the composition walk. The resolved target MUST have a valid versioned workflow filename.

A `workflow:` field that interpolates any name not in the bound params and not a built-in variable SHALL be treated as a capture-variable reference. Such references SHALL fail pre-validation with an error stating that sub-workflow targets cannot depend on captured variables. (At run start, captured variables do not exist yet, so any non-param non-builtin reference in a `workflow:` field is by definition a captured variable.)

In strict mode (fresh runs), an unresolved `{{paramName}}` (param exists but no bound value and no default) is an error. In lenient mode (`--validate` without all required params supplied), the same condition produces a deferred warning instead.

#### Scenario: Param-bound sub-workflow path resolves and validates
- **WHEN** a step has `workflow: "workflows/{{flavor}}-v1.0.yaml"` and `flavor` is a workflow parameter set to `"green"` at run start
- **THEN** pre-validation resolves the path to `workflows/green-v1.0.yaml`, loads it, and includes it in the composition walk

#### Scenario: Captured-variable sub-workflow path is rejected
- **WHEN** a step has `workflow: "workflows/{{detected_target}}-v1.0.yaml"` and `detected_target` is captured from a prior step's stdout
- **THEN** pre-validation fails with an error naming the step and stating that sub-workflow targets cannot depend on captured variables

#### Scenario: Param-bound path that does not exist fails
- **WHEN** a `workflow: "{{name}}-v1.0.yaml"` resolves to a versioned path that does not exist on disk
- **THEN** pre-validation fails with an error naming the step, the unresolved file path, and the parameter that produced it
