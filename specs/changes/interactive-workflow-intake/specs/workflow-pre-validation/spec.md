## MODIFIED Requirements

### Requirement: Pre-validation pipeline scope

When pre-validation runs against a workflow root, it SHALL perform a full static graph analysis of all reachable workflow files, including:

1. Per-file schema and struct validation, applying defaults and known-CLI checks (existing behavior of `loader.LoadWorkflow`).
2. Per-file constraint validation: `skip_if` not on the first step in scope, `break_if` only inside a loop body, sessions block well-formed, named-session references resolve in scope, and no workflow parameter or capture uses a reserved built-in name.
3. Composition walk over every reachable sub-workflow, including cross-file named-session compatibility.
4. `{{var}}` reference checks: every interpolated reference in step prompts, commands, sub-workflow paths, and parameter values MUST refer to a workflow parameter visible at that scope, a known built-in variable, or a capture variable produced by an earlier step on a control-flow path that reaches the reference. The known built-in set SHALL include every variable the runner exposes at runtime, including those whose value may be empty.
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

#### Scenario: Reference to the intake handoff built-in passes
- **WHEN** a workflow prompt references `{{intake_handoff}}` and the workflow declares no parameter of that name
- **THEN** pre-validation accepts the reference as a known built-in rather than reporting an undefined variable

#### Scenario: Reserved built-in name used as a parameter fails
- **WHEN** a workflow declares a parameter named `intake_handoff`
- **THEN** pre-validation fails with an error stating the name is reserved
