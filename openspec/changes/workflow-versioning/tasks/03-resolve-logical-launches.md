# Task: Resolve Logical Workflow Launches

## Goal

Route fresh execution, explicit validation, and read-only debug lookup through catalog-backed logical resolution. New runs must accept only version-free logical names, honor source precedence before version selection, and pass the selected exact version into loading and pre-validation.

## Background

The relevant command paths are in `cmd/agent-runner/main.go`: `resolveWorkflowArg`, `resolveValidateWorkflowArg`, primary/`run` argument dispatch, parameter matching, and the strict/lenient pre-validation entry points. Replace extension-probing in `resolveWorkflowArg` with per-source catalog lookup:

- namespaced names query only the embedded namespace;
- bare/path-style names query `<cwd>/.agent-runner/workflows/` first;
- a project group, including an invalid group, prevents user fallback;
- only absence of the requested project group allows lookup in `~/.agent-runner/workflows/`;
- bare names never query builtins; and
- version comparison never crosses source boundaries.

Validate fresh-run arguments against the lowercase logical-name grammar before probing the filesystem. Reject files, paths, uppercase, dots, explicit versions, and mixed namespace/path forms with actionable guidance. Preserve a real logical name ending in dotless `-v<digits>`; add the hint only after that group lookup fails.

`--validate` is the intentional exact-file escape hatch: an existing versioned `.yaml` or `.yml` path goes directly to lenient pre-validation, while a logical name resolves to latest. This permission must not leak into fresh execution.

`internal/prevalidate/pipeline.go` and `internal/loader/composition.go` must keep exact resolved paths during composition walking. Parameter-bound sub-workflow references resolve before loading; strict mode rejects unbound declared params, lenient mode emits deferred warnings, and captured-variable references fail in both modes. Builtin fresh runs retain the existing skip rule, but explicit validation always runs. Keep structured errors with file and step context.

Update `cmd/agent-runner/debug_cmd.go` so version-free builtin refs resolve latest, exact `builtin:` refs and on-disk paths read exact bytes, and namespaced version-bearing shorthand reports the required exact ref. Debug output remains byte-for-byte and read-only.

Use focused tests in `cmd/agent-runner/`, `internal/prevalidate/`, `internal/loader/`, and `workflows/`. Include real directory fixtures for source precedence and invalid-group behavior.

### Intermediate handoff from task 02

This task requires task 02's green intermediate boundary. Replace its temporary command-test wiring with the final launch contract:

- tests that launched local workflows through explicit versioned paths now create versioned project/user candidates and invoke their version-free logical names;
- add the final negative assertions that both the primary form and `run` alias reject exact paths for execution;
- retain exact versioned paths only in `--validate` and read-only debug tests; and
- keep namespaced builtin tests on version-free logical names backed by catalog resolution.

Do not yet convert discovery or New-tab canonical-name assertions from task 02's intermediate version-bearing names; task 04 owns that final presentation change. The full suite MUST still pass with final CLI resolution and intermediate discovery behavior coexisting.

This task owns composition-walk and run-start pre-validation coverage. Exercise `internal/prevalidate/pipeline.go` and `internal/loader/composition.go` for missing, malformed, interpolated, and builtin child references here rather than deferring those tests to the exact-resume/run-view task.

## Spec

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

#### Scenario: Resolve bare name to latest project YAML
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `.agent-runner/workflows/my-workflow-v1.0.yaml` and `.agent-runner/workflows/my-workflow-v1.2.yaml` exist
- **THEN** the workflow is loaded from `.agent-runner/workflows/my-workflow-v1.2.yaml`

#### Scenario: Resolve bare name to project YML
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `.agent-runner/workflows/my-workflow-v1.0.yml` exists
- **THEN** the workflow is loaded from `.agent-runner/workflows/my-workflow-v1.0.yml`

#### Scenario: Resolve path-style name to latest nested project file
- **WHEN** the user runs `agent-runner run team/deploy`
- **AND** `.agent-runner/workflows/team/deploy-v2.9.yaml` and `.agent-runner/workflows/team/deploy-v2.10.yaml` exist
- **THEN** the workflow is loaded from `.agent-runner/workflows/team/deploy-v2.10.yaml`

#### Scenario: Resolve namespaced name to latest embedded builtin
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

#### Scenario: Bare name falls back to latest global YAML
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** no project-local `my-workflow` group exists
- **AND** `~/.agent-runner/workflows/my-workflow-v1.0.yaml` and `~/.agent-runner/workflows/my-workflow-v1.3.yaml` exist
- **THEN** the workflow is loaded from `~/.agent-runner/workflows/my-workflow-v1.3.yaml`

#### Scenario: Bare name falls back to global YML
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** no project-local `my-workflow` group exists
- **AND** `~/.agent-runner/workflows/my-workflow-v1.0.yml` exists
- **THEN** the workflow is loaded from `~/.agent-runner/workflows/my-workflow-v1.0.yml`

#### Scenario: Project workflow shadows newer global workflow
- **WHEN** the user runs `agent-runner run my-workflow`
- **AND** `.agent-runner/workflows/my-workflow-v1.0.yaml` exists
- **AND** `~/.agent-runner/workflows/my-workflow-v3.0.yaml` exists
- **THEN** the workflow is loaded from `.agent-runner/workflows/my-workflow-v1.0.yaml`

#### Scenario: Project path-style workflow shadows global workflow
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

### Requirement: Flatten CLI to single command

The former command framework's `run`, `resume`, and `validate` subcommands SHALL remain removed. For fresh execution, the primary CLI form SHALL accept a version-free logical workflow name as a positional argument: `agent-runner [flags] <workflow-name> [params...]`. The lightweight `agent-runner run <workflow-name> ...` command form SHALL remain an alias for fresh execution. A workflow filename or filesystem path MUST NOT start a fresh run through either form. Global flags in the primary form MUST precede positional arguments. The `--resume` and `--validate` flags replace the former resume and validate subcommands.

The `--validate` flag SHALL accept either a version-free logical workflow name or an existing exact versioned `.yaml` or `.yml` file path. Accepting an exact path for validation MUST NOT make that path executable as a fresh run.

#### Scenario: Run workflow without subcommand
- **WHEN** `agent-runner deploy` is invoked without any subcommand
- **THEN** the runner resolves the logical workflow name to its latest version and executes it

#### Scenario: Run command alias resolves logical workflow
- **WHEN** `agent-runner run deploy` is invoked
- **THEN** the runner resolves the logical workflow name to its latest version and executes it

#### Scenario: Workflow path cannot start fresh run
- **WHEN** `agent-runner deploy-v1.0.yaml` is invoked without `--validate`
- **THEN** the runner rejects the workflow filename and instructs the user to launch logical workflow `deploy`

#### Scenario: Run alias also rejects workflow path
- **WHEN** `agent-runner run deploy-v1.0.yaml` is invoked
- **THEN** the runner rejects the workflow filename and instructs the user to launch logical workflow `deploy`

#### Scenario: Validate logical workflow via flag
- **WHEN** `agent-runner --validate deploy` is invoked
- **THEN** the runner resolves and validates the latest `deploy` workflow without executing it

#### Scenario: Validate exact versioned path via flag
- **WHEN** `agent-runner --validate deploy-v1.0.yaml` is invoked and the file exists
- **THEN** the runner validates that exact workflow file and exits without executing it

#### Scenario: Validate and resume are mutually exclusive
- **WHEN** both `--validate` and `--resume` are passed
- **THEN** the runner exits with an error indicating the flags are mutually exclusive

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

#### Scenario: --validate accepts a versioned YAML file path
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

### Requirement: Pre-validation catches broken sub-workflows before run start

For fresh runs that are not builtin workflows (i.e., not skipped by the pre-validation skip rule defined in `workflow-pre-validation`), every reachable sub-workflow SHALL be loaded and validated before any step in the root workflow executes. Every resolved sub-workflow target MUST use a valid versioned workflow filename. Errors that today would surface lazily at sub-workflow dispatch SHALL surface at run start.

For builtin workflow runs (which the skip rule excludes from pre-validation), broken sub-workflows continue to surface lazily at dispatch — the agent-runner repo's build-time agent-validator check is responsible for ensuring builtins do not ship broken.

#### Scenario: Missing sub-workflow file fails at run start, not at dispatch
- **WHEN** a non-builtin root workflow references `workflow: workflows/missing-v1.0.yaml` and the file does not exist
- **THEN** pre-validation fails before any root step executes, with an error naming the missing file and the referencing step

#### Scenario: Sub-workflow with broken sessions fails at run start
- **WHEN** a non-builtin root reaches a versioned sub-workflow that has a `session: implementor` reference but no workflow in the composition tree declares `implementor`
- **THEN** pre-validation fails before any root step executes, with an error naming the unresolved reference and the file that contains it

#### Scenario: Project workflow with a broken sub-workflow fails at run start
- **WHEN** the cwd contains `.agent-runner/workflows/deploy-v1.0.yaml` referencing a versioned sub-workflow with a syntax error and `agent-runner deploy` is invoked
- **THEN** pre-validation fails before any deploy step executes (project workflows are not skipped)

#### Scenario: Builtin run falls back to lazy dispatch failure
- **WHEN** a builtin root workflow (e.g., `agent-runner core:finalize-pr`) reaches a broken versioned sub-workflow at runtime
- **THEN** the failure surfaces at dispatch, as in pre-existing behavior — pre-validation does not run for builtin roots, since builtins are gated by the agent-runner repo's build-time check

### Requirement: `debug --show-workflow <ref>` prints resolved YAML

The `agent-runner debug --show-workflow <ref>` command SHALL print the YAML for the named workflow ref to stdout and exit 0 on success. A version-free builtin logical ref (`<namespace>:<logical-name>`) SHALL resolve to the highest embedded version. An exact embedded ref (`builtin:<namespace>/<versioned-file>`) SHALL read that exact historical version. A namespaced version-bearing shorthand such as `core:finalize-pr-v1.0` SHALL NOT act as a historical selector; callers SHALL use the exact `builtin:` ref instead.

Non-builtin refs (relative paths, absolute paths, `~`-prefixed paths) SHALL be resolved from disk and MAY identify an exact historical version. The output SHALL be the YAML for the **named ref only** — composed sub-workflow references SHALL NOT be inlined, expanded, or followed; they SHALL appear in the output exactly as they appear in the source. The output SHALL be the bytes as embedded or stored, with no normalization, reformatting, or comment stripping. Read-only acceptance of an exact path or embedded ref MUST NOT make that historical version launchable as a new run.

#### Scenario: Builtin logical ref returns latest embedded YAML
- **WHEN** `agent-runner debug --show-workflow core:finalize-pr` is invoked and versions `v1.0` and `v2.0` are embedded
- **THEN** the bytes for embedded `workflows/core/finalize-pr-v2.0.yaml` are printed to stdout verbatim and the command exits 0

#### Scenario: Exact builtin ref returns historical embedded YAML
- **WHEN** `agent-runner debug --show-workflow builtin:core/finalize-pr-v1.0.yaml` is invoked
- **THEN** the bytes for that exact embedded version are printed to stdout verbatim and the command exits 0

#### Scenario: Namespaced version shorthand is not a historical selector
- **WHEN** `agent-runner debug --show-workflow core:finalize-pr-v1.0` is invoked
- **THEN** the command exits non-zero and instructs the user to provide exact ref `builtin:core/finalize-pr-v1.0.yaml`

#### Scenario: On-disk ref returns file bytes
- **WHEN** `agent-runner debug --show-workflow ./my-workflow-v1.0.yaml` is invoked and the file exists
- **THEN** the file's bytes are printed to stdout verbatim and the command exits 0

#### Scenario: Sub-workflow references preserved
- **WHEN** the requested workflow contains `workflow: plan-change-v2.0.yaml` references in its YAML
- **THEN** those reference lines appear in the output unmodified; no sub-workflow content is inlined

#### Scenario: Unknown ref
- **WHEN** `agent-runner debug --show-workflow` is invoked with a ref that resolves to neither an embedded builtin nor an existing on-disk file
- **THEN** the command exits non-zero and prints an error to stderr naming the missing ref

#### Scenario: Malformed ref string
- **WHEN** `agent-runner debug --show-workflow` is invoked with a ref string that cannot be parsed (e.g. empty, contains illegal characters)
- **THEN** the command exits non-zero and prints a parse error to stderr

#### Scenario: Output is unnormalized
- **WHEN** the resolved YAML contains comments, blank lines, or non-canonical whitespace
- **THEN** those bytes appear in the output unchanged

## Done When

- Primary and `run` alias fresh execution accept only logical names and pass the exact selected latest version to loading and execution.
- Project/user precedence, builtin isolation, invalid-group blocking, nested logical names, `.yml`, duplicates, and arbitrary-size version ordering are covered by command-level tests.
- `--validate` accepts either a logical name or an existing exact versioned path and exercises strict/lenient pre-validation behavior exactly as specified.
- Pre-validation preserves exact versioned sub-workflow targets and structured error context; builtin skip behavior is covered without weakening explicit validation.
- Fresh-run pre-validation catches missing, malformed, and session-invalid versioned sub-workflows before execution, while builtin roots retain lazy dispatch failure.
- Debug lookup distinguishes logical latest refs from exact historical refs and preserves source bytes.
- Task 02's temporary direct-path launch assertions are replaced with final logical-name and execution-path-rejection assertions; task 04's discovery assertions remain explicitly intermediate.
- Focused tests for `cmd/agent-runner`, `internal/prevalidate`, `internal/loader`, and builtin resolution pass.
- `make fmt` and `make test` pass before task 04 begins.
