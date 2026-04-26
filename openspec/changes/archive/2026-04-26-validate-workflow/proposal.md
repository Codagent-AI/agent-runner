## Why

Today, only schema and same-file constraint validation runs when a workflow starts. Sub-workflow files, layered config, profile chains, and external CLI/model acceptance are not checked until the relevant step actually dispatches — which can be hours into a long-running workflow. A typo in a sub-workflow's session declaration, an unknown `effort` value in a global config override, or an unsupported model on a particular CLI all surface late, after wasted compute and human time.

The existing `--validate` CLI flag invokes `loader.ValidateComposition`, which walks reachable sub-workflows, but is **not** invoked by normal `agent-runner <workflow>` runs and does not exercise layered config, profile resolution, or external CLI/model acceptance.

This change adds a unified pre-validation pipeline that runs before any step executes (or any TUI launches), reusing the existing `--validate` surface so explicit and implicit paths cannot drift.

## What Changes

- New pre-validation pipeline that runs before fresh `agent-runner <workflow>` runs and is reused by `agent-runner --validate <workflow-or-path> [params...]`.
- The pipeline performs a **full static graph analysis**: schema and constraint validation per file (existing), composition walk with bound parameters resolved (extending existing `ValidateComposition`), `{{var}}` reference checks (params, builtins, captured-by-prior-step), engine-config creation, session-aware agent-profile resolution, loop body well-formedness, and layered-config load.
- Effective agent resolution walks every agent referenced by an agent step through its `extends` chain, mirroring runtime session semantics: `session: new` (or default) resolves from the step; `session: resume` inherits from the most recent session-originating step in the same file; `session: inherit` inherits from the parent workflow's session-originating step; named-session references resolve via the sessions-block declaration. Per-step `cli` / `model` overrides are applied on top.
- The validator collects every unique `(cli, model, effort)` triple referenced across the composition and performs **one minimal external probe per triple**, plus one `exec.LookPath` per unique CLI binary.
- Each adapter's `ProbeModel` returns a probe-strength tag (`Verified`, `SyntaxOnly`, or `BinaryOnly`) so success and failure messages reflect how strongly the underlying CLI was actually exercised.
- **Skip rule for fresh runs: builtins only.** When the resolved workflow is an embedded builtin (matched by `builtinworkflows.IsRef`), pre-validation is skipped because the builtin is validated at the agent-runner repo's build time. Every other path — including project workflows under `<cwd>/.agent-runner/workflows/`, global user workflows under `~/.agent-runner/workflows/`, and any other resolution outcome — pre-validates on every fresh run. Downstream projects MAY (and are encouraged to) configure their own agent-validator check on their workflows directory as an additional author-time gate, but the runner does not skip run-time validation on the assumption that they did.
- `--validate` accepts either a workflow name (resolved through `resolveWorkflowArg`) or a `.yaml`/`.yml` file path that exists. It accepts optional `key=value` params. Missing required params are non-fatal at validate time; sub-workflow paths whose params are unbound at validate time become deferred warnings (`would be checked at run time`) rather than errors. The captured-vars-in-workflow-paths rule still fails hard. `--validate` ignores the skip rule.
- Sub-workflow `workflow:` paths that interpolate workflow params SHALL be resolved with bound values and validated. Paths that interpolate captured variables (only known at runtime) SHALL fail pre-validation with an explicit error.
- Errors include the file path for per-file failures (parse, schema, constraints). Errors involving config / profiles include the profile set, agent name, field, invalid value, and (where the schema knows them) allowed values; the originating layer file is best-effort (list of layers loaded) because the existing config loader does not retain per-field origin metadata.
- Validation failure exits non-zero before launching the TUI or recording any audit entries.
- The agent-runner repo gains a "validate all builtins" mechanism scoped to this project — implemented as a Go test (preferred) or an internal dev-only command — that walks every embedded builtin and runs the pre-validation pipeline on each. This is what the builtin-skip rule rests on. The mechanism does NOT need to ship in the released binary; it only needs to run in the agent-runner repo's own CI / test suite. (If wired into the binary as a hidden flag like `--validate-builtins`, that's acceptable but not required.)

## Capabilities

### New Capabilities

- `workflow-pre-validation`: the pre-validation pipeline (what it checks, when it runs, the builtin-only skip rule, the bound-param resolution rule, the captured-vars-in-workflow-paths fail rule, session-aware agent resolution, the minimal-probe algorithm with per-adapter probe strength, the structured error format, and exit behavior).

### Modified Capabilities

- `sub-workflows`: a new requirement clarifies that broken sub-workflows surface at pre-validation for non-builtin workflows. Builtins continue to surface at dispatch (since they skip pre-validation).
- `config-profiles`: a new requirement clarifies that pre-validation surfaces layered-config and profile-resolution errors with the structured `(profile, agent, field, value, allowed)` format, plus a best-effort layer-file list pending future origin tracking.

## Out of Scope

- Re-validating workflows on `--resume` (resume continues to load only the top-level file via `loader.LoadWorkflow`).
- Runtime-resolved sub-workflow paths (e.g., `workflow: "{{capturedVar}}"`). These now hard-fail pre-validation.
- Deeper external CLI/model compatibility checks beyond the per-adapter probe (e.g., capability matrices, version negotiation, account/quota awareness).
- Per-field origin tracking inside the layered config loader. Config errors include `(profile, agent, field, value, allowed)` plus a best-effort layer-file list; full file-of-record tracking is a separate change.
- Skipping pre-validation for downstream-project workflows. Downstream projects who want author-time gating MAY configure their own agent-validator check; the runner does not act on whether they did.

## Impact

- `cmd/agent-runner/main.go` — `handleRun` invokes the pre-validation pipeline after `matchParams` and before `runner.PrepareRun` / `runner.RunWorkflow` for non-builtin workflows. `handleValidate` invokes the same pipeline (always, regardless of skip rule), accepts a name or `.yaml`/`.yml` path, and accepts optional params. The flag's argument-parsing logic gains a path-or-name detector and a key=value collector.
- `internal/loader/composition.go` — `ValidateComposition` extended to accept bound params, fail on captured-var workflow paths in strict mode, emit deferred warnings in lenient mode, and produce structured errors.
- `internal/validate/` — new files for variable-reference checking, session-aware agent-profile resolution, engine-config validation, loop body well-formedness, and the unique-triple collector with per-adapter probe strength.
- `internal/cli/` adapter package — new `ProbeModel(model, effort string) (ProbeStrength, error)` method on each adapter. Each adapter declares its own probe strength based on the underlying CLI's surface; adapters with no probe surface return `BinaryOnly` after a `LookPath` check.
- `internal/config/` — pre-validation forces the layered-config load even when no agent step would otherwise trigger it.
- A new "validate all builtins" mechanism in the agent-runner repo, scoped to this project and not required to ship in the released binary. Preferred form: a Go test that iterates `builtinworkflows.List()` and runs the pre-validation pipeline against each embedded builtin, failing the test on any error. Alternative form: a hidden / dev-only CLI flag like `--validate-builtins`. Either way, it is wired into the agent-runner repo's own CI (`make test` or equivalent) — this is the build-time guarantee underlying the runtime builtin-skip rule. Downstream projects MAY adopt a similar pattern (running `agent-runner --validate` on each of their workflow YAMLs in CI) if they want author-time gating, but nothing in the runner depends on whether they do.
