# workflow-pre-validation Specification

## Purpose
TBD - created by archiving change validate-workflow. Update Purpose after archive.
## Requirements
### Requirement: Pre-validation pipeline scope

When pre-validation runs against a workflow root, it SHALL perform a full static graph analysis of all reachable workflow files, including:

1. Per-file schema and struct validation, applying defaults and known-CLI checks (existing behavior of `loader.LoadWorkflow`).
2. Per-file constraint validation: `skip_if` not on the first step in scope, `break_if` only inside a loop body, sessions block well-formed, named-session references resolve in scope.
3. Composition walk over every reachable sub-workflow, including cross-file named-session compatibility.
4. `{{var}}` reference checks: every interpolated reference in step prompts, commands, sub-workflow paths, and parameter values MUST refer to a workflow parameter visible at that scope, a known built-in variable, or a capture variable produced by an earlier step on a control-flow path that reaches the reference.
5. Loop body well-formedness: every loop step has at least one body step; if `over:` is set, the value SHALL be a non-empty glob pattern that parses syntactically; if `max:` is set, the value SHALL be a positive integer; if `as:` is set, the binding name SHALL be a valid identifier.
6. Engine creation: every workflow with an `engine:` block resolves through `engine.Create()` without error.
7. Layered-config load: built-in defaults merged with `~/.agent-runner/config.yaml` and the project's `.agent-runner/config.yaml` SHALL be loaded and validated through the same loader the runtime uses.
8. Session-aware effective agent resolution (see "Session-aware agent resolution" below): every agent step in the composition contributes a `(cli, model, effort)` triple.
9. CLI and model acceptance probe: each unique `(cli, model, effort)` triple collected in step 8 SHALL be probed once via the adapter's `ProbeModel` method, and each unique CLI binary SHALL be resolved once via `exec.LookPath`.

#### Scenario: Pipeline reports first failure with structured context
- **WHEN** any of the steps above fails
- **THEN** pre-validation halts at the first failure and reports an error that names the offending file path (where applicable), and (where applicable) the profile set, agent, field, invalid value, and allowed values

#### Scenario: Pipeline succeeds for a fully valid graph
- **WHEN** every step above succeeds
- **THEN** pre-validation returns success and the run is allowed to proceed

### Requirement: When pre-validation runs

Pre-validation SHALL run before any step executes and before any TUI is launched, in two contexts:

1. **Fresh runs**: every `agent-runner <workflow> [params...]` invocation, after parameter matching and before `runner.PrepareRun` or `runner.RunWorkflow` is called, for workflows that are not skipped by the path-based skip rule below. Fresh runs use **strict mode**: any unresolved `{{paramName}}` reference in a sub-workflow path is an error (since `matchParams` has bound all required params before pre-validation runs).
2. **Explicit validate**: every `agent-runner --validate <workflow-or-path> [key=value...]` invocation, **unconditionally** (the skip rule does not apply). The argument MAY be a workflow name (resolved via `resolveWorkflowArg`) or a `.yaml` / `.yml` file path that exists on disk. Params are optional; missing required params are non-fatal at validate time. `--validate` uses **lenient mode**: a sub-workflow path whose `{{paramName}}` reference cannot be resolved with the supplied params (and has no parameter default) emits a deferred warning ("target depends on unbound param X; checked at run time") rather than an error. Captured-variable references in sub-workflow paths still fail hard.

Pre-validation SHALL NOT run on `agent-runner --resume` invocations; resume continues to load only the top-level file via `loader.LoadWorkflow` (unchanged behavior).

#### Scenario: Fresh run pre-validates before TUI launch
- **WHEN** `agent-runner my-workflow` is invoked and `my-workflow` is not skipped by the skip rule
- **THEN** pre-validation runs to completion before the live TUI is created and before any audit-log entries are written

#### Scenario: Pre-validation failure prevents run start
- **WHEN** pre-validation fails for a fresh run
- **THEN** the runner exits non-zero, prints the structured error to stderr, does not launch the TUI, and does not write any audit entries

#### Scenario: --validate accepts a workflow name
- **WHEN** `agent-runner --validate my-workflow` is invoked
- **THEN** the argument is resolved through `resolveWorkflowArg` and the same pre-validation pipeline runs in lenient mode, printing `workflow is valid` on success or the structured error on failure

#### Scenario: --validate accepts a YAML file path
- **WHEN** `agent-runner --validate workflows/core/finalize-pr.yaml` is invoked and that file exists
- **THEN** the argument is treated as a literal file path (because it ends in `.yaml` or `.yml` and exists) and the pre-validation pipeline runs against that file in lenient mode

#### Scenario: --validate ignores the skip rule
- **WHEN** `agent-runner --validate core:finalize-pr` is invoked (a builtin that fresh runs would skip)
- **THEN** the full pre-validation pipeline still runs

#### Scenario: --validate with optional params binds them
- **WHEN** `agent-runner --validate my-workflow flavor=green` is invoked and `my-workflow` has a sub-workflow path `workflow: "workflows/{{flavor}}.yaml"`
- **THEN** the pre-validation pipeline resolves the path to `workflows/green.yaml` and validates it as in fresh-run mode

#### Scenario: --validate without supplied params produces deferred warnings
- **WHEN** `agent-runner --validate my-workflow` is invoked with no params and `my-workflow` has a sub-workflow path `workflow: "workflows/{{flavor}}.yaml"` where `flavor` is a required workflow param with no default
- **THEN** pre-validation emits a deferred warning naming the step and the unbound param, validates everything else, and exits zero if no other failures exist

#### Scenario: --validate still fails on captured-variable workflow paths
- **WHEN** `agent-runner --validate my-workflow` is invoked and `my-workflow` has `workflow: "workflows/{{captured_target}}.yaml"` where `captured_target` is captured at runtime
- **THEN** pre-validation fails with the captured-variable error regardless of mode

#### Scenario: Resume does not pre-validate
- **WHEN** `agent-runner --resume <session-id>` is invoked
- **THEN** the runner loads only the top-level workflow file via the existing loader and does not run the pre-validation pipeline

### Requirement: Skip rule for fresh runs (builtins only)

For fresh runs only, pre-validation SHALL be skipped when the resolved workflow path is an embedded builtin workflow (any path matched by `builtinworkflows.IsRef`).

Every other resolved workflow path — including workflows under `<cwd>/.agent-runner/workflows/`, workflows under `~/.agent-runner/workflows/`, and any other resolution outcome — SHALL pre-validate on every fresh run.

The skip rule rests on the assumption that builtins are validated at the agent-runner repo's build time by a "validate all builtins" mechanism scoped to that project — preferably a Go test that iterates the embedded builtins and runs the pre-validation pipeline on each, alternatively a hidden / dev-only CLI flag. The mechanism does not need to ship in the released binary. Downstream projects MAY configure an analogous author-time check on their own `.agent-runner/workflows/` (e.g., wiring `agent-runner --validate <relpath>` into CI), but the runner does NOT skip pre-validation on the assumption that they did. `--validate` does not honor the skip rule.

#### Scenario: Builtin invocation skips pre-validation
- **WHEN** `agent-runner core:finalize-pr` is invoked
- **THEN** pre-validation is skipped and the runner proceeds directly to `runner.PrepareRun`

#### Scenario: Project workflow invocation pre-validates
- **WHEN** the cwd is a project root containing `.agent-runner/workflows/deploy.yaml` and `agent-runner deploy` is invoked
- **THEN** pre-validation runs the full pipeline before the run starts

#### Scenario: Global user workflow invocation pre-validates
- **WHEN** the cwd contains no `.agent-runner/workflows/scratch.yaml` but `~/.agent-runner/workflows/scratch.yaml` exists, and `agent-runner scratch` is invoked
- **THEN** pre-validation runs the full pipeline before the run starts

### Requirement: Bound-parameter sub-workflow resolution

Sub-workflow `workflow:` fields that contain `{{paramName}}` interpolation SHALL be resolved during pre-validation using the values bound at the start of validation, and the resolved target SHALL be validated as part of the composition walk.

A `workflow:` field that interpolates any name not in the bound params and not a built-in variable SHALL be treated as a capture-variable reference. Such references SHALL fail pre-validation with an error stating that sub-workflow targets cannot depend on captured variables. (At run start, captured variables do not exist yet, so any non-param non-builtin reference in a `workflow:` field is by definition a captured variable.)

In strict mode (fresh runs), an unresolved `{{paramName}}` (param exists but no bound value and no default) is an error. In lenient mode (`--validate` without all required params supplied), the same condition produces a deferred warning instead.

#### Scenario: Param-bound sub-workflow path resolves and validates
- **WHEN** a step has `workflow: "workflows/{{flavor}}.yaml"` and `flavor` is a workflow parameter set to `"green"` at run start
- **THEN** pre-validation resolves the path to `workflows/green.yaml`, loads it, and includes it in the composition walk

#### Scenario: Captured-variable sub-workflow path is rejected
- **WHEN** a step has `workflow: "workflows/{{detected_target}}.yaml"` and `detected_target` is captured from a prior step's stdout
- **THEN** pre-validation fails with an error naming the step and stating that sub-workflow targets cannot depend on captured variables

#### Scenario: Param-bound path that does not exist fails
- **WHEN** a `workflow: "{{name}}"` resolves to a path that does not exist on disk
- **THEN** pre-validation fails with an error naming the step, the unresolved file path, and the parameter that produced it

### Requirement: Session-aware effective agent resolution

For each agent step in the composition graph, pre-validation SHALL determine the effective `(cli, model, effort)` triple by mirroring runtime session semantics:

- If the step has `session: new` or no session field: resolve `step.Agent` against the active profile set's agents map; this step becomes the current "session-origin step" for its workflow file.
- If the step has a named session reference (`session: <name>` where `<name>` is declared in the sessions block of some workflow visible in the composition): use the agent declared by that sessions block.
- If the step has `session: resume`: inherit the agent from the most recent session-origin step in the same workflow file. The step does NOT become a new session-origin.
- If the step has `session: inherit` (only valid in sub-workflows): inherit the agent from the parent workflow's most recent session-origin step.

After agent resolution, per-step `cli` and `model` overrides SHALL be applied on top of the resolved agent's values. The resulting `(cli, model, effort)` triple is the step's contribution to the probe set.

The validator SHALL collect every step's effective triple and deduplicate the set before probing.

#### Scenario: Resume step inherits triple from origin
- **WHEN** step A has `session: new` resolving to `(claude, opus-4-7, high)` and a later step B has `session: resume` with no overrides
- **THEN** both steps contribute the same triple `(claude, opus-4-7, high)` and pre-validation probes it exactly once

#### Scenario: Resume step with model override produces a distinct triple
- **WHEN** step A resolves to `(claude, opus-4-7, high)` and step B has `session: resume` with `model: sonnet-4-6`
- **THEN** the collected triples are `(claude, opus-4-7, high)` and `(claude, sonnet-4-6, high)`, and pre-validation probes both

#### Scenario: Inherit reuses the parent's session-origin triple
- **WHEN** a sub-workflow's step has `session: inherit` and the parent's most recent session-origin step resolved to `(codex, gpt-5.5, xhigh)`
- **THEN** the inherit step contributes `(codex, gpt-5.5, xhigh)` to the probe set

#### Scenario: Named-session reference resolves via the declaration
- **WHEN** a workflow's `sessions:` block declares `{name: planner, agent: planner_profile}` and a step has `session: planner`
- **THEN** the step's agent is `planner_profile` (regardless of any `step.Agent` field) and the resulting triple is collected from the resolved profile

#### Scenario: Inherit in a top-level workflow contributes nothing
- **WHEN** a top-level workflow's step has `session: inherit` (already a misuse rejected by existing validation)
- **THEN** session-aware resolution does not synthesize a triple for that step (the per-file constraint validation has already failed)

### Requirement: Acceptance probing with per-adapter probe strength

To validate that the underlying CLIs accept the referenced models and effort levels, pre-validation SHALL:

- Deduplicate the collected triples and CLI binaries.
- Resolve each unique CLI binary via `exec.LookPath` exactly once per pre-validation run.
- Invoke `Adapter.ProbeModel(model, effort)` exactly once per unique `(cli, model, effort)` triple per pre-validation run, where `Adapter` is the registered adapter for that CLI.

`ProbeModel` SHALL return a probe strength of `Verified` (the underlying CLI was asked and confirmed acceptance), `SyntaxOnly` (only the adapter's own schema was checked; the underlying CLI was not consulted), or `BinaryOnly` (only the CLI binary's presence on PATH was confirmed). Implementations MUST NOT spawn a real agent invocation for probing.

The total number of external CLI interactions performed by pre-validation SHALL equal the number of unique triples (probe invocations) plus the number of unique CLI binaries (path lookups), regardless of how many agent steps reference each.

When a probe returns successfully with a strength weaker than `Verified`, pre-validation SHALL surface that strength in any human-facing success summary so the user knows how strong the guarantee is. When a probe returns an error, pre-validation SHALL fail with a structured error that includes the strength tag the adapter was attempting (e.g., "model rejected at ProbeStrength=Verified" vs. "binary not found at ProbeStrength=BinaryOnly"), so the user can distinguish a definitive rejection from a missing binary.

#### Scenario: Two agents share a triple — one probe
- **WHEN** the composition references agents `planner` and `reviewer`, both resolved to `(claude, opus-4-7, high)`
- **THEN** pre-validation invokes `claudeAdapter.ProbeModel("opus-4-7", "high")` exactly once

#### Scenario: Three triples — three probes
- **WHEN** the composition resolves to triples `(claude, opus-4-7, high)`, `(claude, sonnet-4-6, medium)`, and `(codex, gpt-5.5, xhigh)`
- **THEN** pre-validation invokes `ProbeModel` exactly three times — twice on the Claude adapter and once on the Codex adapter

#### Scenario: CLI binary lookup deduped across many references
- **WHEN** the composition references the `claude` CLI in 20 agent steps spread across 5 sub-workflows
- **THEN** pre-validation calls `exec.LookPath("claude")` exactly once

#### Scenario: Verified probe failure surfaces a definitive rejection
- **WHEN** the Claude adapter's `ProbeModel("opus-4-7", "extreme")` reaches the underlying CLI and the CLI rejects `extreme`
- **THEN** pre-validation fails with an error that includes `ProbeStrength=Verified`, the agent definitions that resolved to that triple, the CLI, the model, the rejected effort value, and (when the adapter exposes them) the allowed values

#### Scenario: BinaryOnly probe success degrades the success message
- **WHEN** an adapter has no model-acceptance surface and `ProbeModel` returns `BinaryOnly` after `LookPath` succeeds
- **THEN** pre-validation succeeds for that triple but the success summary indicates the probe strength was `BinaryOnly` (the underlying CLI was not consulted)

#### Scenario: Adapter without a probe surface does not spawn the CLI
- **WHEN** an adapter has no cheaper acceptance surface than a full CLI invocation
- **THEN** the adapter's `ProbeModel` SHALL only verify the CLI binary is on PATH and SHALL return `BinaryOnly` without spawning the CLI for model verification

### Requirement: Structured error format

Pre-validation errors SHALL be structured to make the offending location actionable. Errors SHALL include:

- The file path that owns the failure, when the failure is attributable to a specific file (parse, schema, per-file constraint, composition walk). For post-merge config errors, the file path is best-effort: the error includes the list of layer files that were loaded (built-in defaults, `~/.agent-runner/config.yaml`, `<cwd>/.agent-runner/config.yaml` when present), since the existing config loader does not retain per-field origin metadata.
- For workflow-constraint failures: the offending step ID and the violated rule.
- For config / profile failures: the profile set name, the agent name, the field name, the invalid value, and (where the schema knows them) the allowed values.
- For probe failures: the agent definitions that resolved to the triple, the CLI, the model, the rejected value, the probe strength reached, and (where exposed) the allowed values.

The CLI SHALL print the populated fields to stderr, omitting fields that are not relevant to a given failure.

#### Scenario: Config validation error includes profile / agent / field context
- **WHEN** `~/.agent-runner/config.yaml` sets `profiles.default.agents.implementor.effort: extreme` and the schema does not accept `extreme`
- **THEN** the pre-validation error names the profile set (`default`), the agent (`implementor`), the field (`effort`), the invalid value (`extreme`), the allowed values (`low, medium, high, xhigh`), and a best-effort layer list including `~/.agent-runner/config.yaml`

#### Scenario: Workflow constraint error names the file and step
- **WHEN** a sub-workflow has `break_if` outside a loop body
- **THEN** the pre-validation error names the sub-workflow file path, the step ID, and the violated rule

#### Scenario: Parse error names the file and line
- **WHEN** a sub-workflow contains malformed YAML
- **THEN** the pre-validation error names the sub-workflow file path and surfaces the parser's line/column information

