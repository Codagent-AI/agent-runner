## Context

`agent-runner` workflows recursively load sub-workflows, reference layered config (built-in defaults + `~/.agent-runner/config.yaml` + project `.agent-runner/config.yaml`), and resolve agent profiles through `extends` chains. Today most of this graph is not exercised until the relevant step actually runs:

- `loader.LoadWorkflow` validates one file's schema and same-file constraints.
- `loader.ValidateComposition` walks reachable sub-workflows and checks cross-file named-session compatibility, but is **only** invoked by the `--validate` CLI flag — not by `agent-runner <workflow>`.
- Layered config and profile resolution happen lazily inside `internal/exec/agent.go` when an agent step is dispatched.
- External CLI acceptance of a `(cli, model, effort)` triple is never checked — the runtime simply spawns the CLI with those flags and relies on the CLI to fail.

This means a typo in a sub-workflow, an unsupported `effort` value in a global override, or a model the underlying CLI does not accept can blow up hours into a run. The current `--validate` flag covers some of this but not all of it, and is not invoked on normal runs.

## Goals / Non-Goals

**Goals:**
- Move workflow, sub-workflow composition, layered-config, profile-chain, and CLI/model acceptance failures from "lazy at dispatch" to "fail-fast before run start" for non-builtin workflows.
- Reuse a single pipeline so `--validate` and the implicit run-time pre-validation cannot drift.
- Accept either a workflow name or a `.yaml`/`.yml` file path on `--validate` so an agent-validator check can invoke `agent-runner --validate <relpath>` directly without a path-to-name mapping shim.
- Mirror runtime session semantics in agent resolution so `session: resume` and `session: inherit` reuse the originating session's triple, not a fresh resolution from `step.Agent`.
- Minimize external CLI calls: probe each unique `(cli, model, effort)` triple exactly once and look up each unique CLI binary exactly once per run, regardless of how many agent steps reference them.
- Be honest about probe strength: each adapter declares whether its `ProbeModel` is `Verified`, `SyntaxOnly`, or `BinaryOnly`, and pre-validation surfaces that to the user.
- Provide an agent-validator check at the agent-runner repo level so the builtins are validated at build time — the only basis for the runtime skip rule.

**Non-Goals:**
- Re-validating workflows on `--resume`. Resume continues to load only the top-level file via `loader.LoadWorkflow`.
- Validating runtime-resolved sub-workflow paths. `workflow:` fields that interpolate captured variables hard-fail pre-validation; the change does not introduce a runtime fallback.
- Deeper external CLI/model compatibility checks (capability matrices, version negotiation, account/quota awareness).
- Per-field origin tracking inside the layered config loader. Errors include the list of layer files loaded rather than a precise file-of-record; adding origin tracking is a separate change.
- Skipping pre-validation for downstream-project workflows. Downstream projects who want author-time gating MAY add their own agent-validator check; the runner does not act on whether they did.

## Approach

### Integration points

In `cmd/agent-runner/main.go`:

- `handleRun(args)` — after `resolveWorkflowArg`, after `loader.LoadWorkflow`, after `parseParams` and `matchParams`, and before `runner.PrepareRun` / `runner.RunWorkflow`, invoke the pre-validation pipeline (strict mode) with the resolved root path and the bound parameter map. Skip when the resolved path satisfies the builtin-only skip rule.
- `handleValidate(args)` — accept either a workflow name or a YAML file path. Detect a file path by suffix (`.yaml` / `.yml`) plus existence on disk; otherwise treat the arg as a name and resolve via `resolveWorkflowArg`. Parse optional `key=value` params via the same `parseParams` helper. Invoke the pipeline in lenient mode unconditionally (no skip rule). Print `workflow is valid` (with any deferred warnings) on success.

The pipeline lives in `internal/validate/` (extending the existing package) with a single entry point such as:

```go
type Mode int
const (
    Strict Mode = iota  // fresh run; unresolved param refs are errors
    Lenient             // --validate; unresolved param refs are deferred warnings
)

type Result struct {
    DeferredWarnings []DeferredWarning
    ProbeResults     []ProbeResult // one per unique triple, with strength tag
}

func Pipeline(rootPath string, boundParams map[string]string, mode Mode, opts Options) (Result, error)
```

`opts` carries the CLI adapter registry, the layered-config loader, and the per-run probe cache. Both `handleRun` and `handleValidate` construct this with real implementations; tests construct it with fakes.

### Skip rule (builtins only)

In `handleRun`, after `resolveWorkflowArg` returns the resolved path:

```go
skip := builtinworkflows.IsRef(resolvedPath)
```

`handleValidate` does not consult the skip rule.

**Rationale.** Builtins ship with the binary and are validated by an agent-runner repo agent-validator check that runs `agent-runner --validate` on every YAML under `workflows/`. End users running a builtin therefore get the structural guarantee for free. Project workflows under `<cwd>/.agent-runner/workflows/` and global user workflows under `~/.agent-runner/workflows/` have no such guarantee — the runner cannot know whether the downstream project configured a similar check — so they pay the run-time cost. Downstream projects who want author-time gating MAY add their own agent-validator check; nothing in the runner depends on whether they do. `--validate` remains the explicit per-workflow override and ignores the skip rule.

### Composition walk with bound parameters

Extend `loader.ValidateComposition` to accept `boundParams map[string]string` and a `Mode`. During the walk:

- `workflow:` fields with no interpolation are validated as today.
- `workflow:` fields with `{{paramName}}` SHALL be resolved using `boundParams` and then validated.
  - In Strict mode: an unresolved `paramName` (no bound value, no default) is an error.
  - In Lenient mode: the same condition produces a deferred warning ("target depends on unbound param X; checked at run time"), and the sub-workflow is not loaded for further validation.
- `workflow:` fields that reference any name not in `boundParams`, not a workflow param, and not a built-in variable SHALL fail with the captured-variable error in either mode. (At run start, captured variables do not exist yet, so any non-param non-builtin reference in a `workflow:` field is by definition a captured variable.)

**Rationale for the captured-vars-fail rule.** Allowing captured variables in `workflow:` paths means a sub-workflow target is unknowable before the run starts, defeating the entire point of pre-validation for that branch. There are zero workflows in the repo today that use this pattern (verified by `grep -rn 'workflow:.*{{' workflows/ testdata/`), so the strict rule has no breakage cost. If a real use case appears later, it can be re-introduced behind an explicit per-step opt-in.

### New static checks

The pipeline performs these checks in order, halting at the first error (deferred warnings accumulate but do not halt):

1. **Per-file schema and constraints.** Existing `LoadWorkflow` (which calls `Workflow.Validate` and `validate.WorkflowConstraints`).
2. **Composition walk.** Extended `ValidateComposition` with `boundParams` and `Mode`.
3. **`{{var}}` reference checks.** For every interpolated reference in step prompts, commands, sub-workflow paths, and parameter values, confirm the name resolves to (a) a workflow parameter visible at that scope, (b) a known built-in variable, or (c) a capture variable produced by an earlier step that is reachable on a control-flow path leading to the reference. Implementation: walk steps in order, maintaining a "captured-by-now" set per scope. When entering a sub-workflow with explicit `params:`, the captured-by-now set resets to the passed params (matching runtime parameter scoping).
4. **Loop body well-formedness.** For every loop step: at least one body step; if `over:` is set, the value parses syntactically as a glob (e.g., via `filepath.Match("", pattern)`); if `max:` is set, it is a positive integer; if `as:` is set, the binding name is a valid identifier. The model fields are `max` / `over` / `as` (no `times` field exists).
5. **Engine creation.** For every workflow with `engine:`, call `engine.Create(config)` and propagate errors with the file path.
6. **Layered-config load.** Call the same loader the runtime uses (`internal/config`) with `cwd`, so the merged result reflects what the runtime would see.
7. **Session-aware effective agent resolution.** See below.
8. **Acceptance probe.** See below.

### Session-aware effective agent resolution

The validator walks each workflow file's steps in order, tracking the most recent "session-origin step" per file. For each agent step:

- `session: new` (or no session field): step is the new session-origin. Resolve agent from `step.Agent` against the active profile set's agents map. Apply per-step `cli` / `model` overrides. Collect the effective `(cli, model, effort)` triple.
- Named session reference (`session: <name>` where `<name>` is declared in the workflow's sessions block, or merged in via composition): use the agent declared by the sessions block. Per-step overrides apply on top. The step does NOT become a new session-origin.
- `session: resume`: inherit agent from the most recent session-origin step in the same workflow file. Per-step overrides apply on top — so the resulting triple may differ from the originating step's triple.
- `session: inherit` (sub-workflow only): inherit agent from the parent workflow's most recent session-origin step. Per-step overrides apply on top.

This mirrors the runtime resolution in `internal/exec/agent.go` (line ~39 in current source), where `session: resume` / `inherit` use the originating session's profile rather than re-resolving from `step.Agent`. Existing per-file constraint validation already rejects `session: inherit` in top-level workflows, so the validator does not need to synthesize a triple for that case.

The collected per-step triples are deduplicated (`set` keyed by `(cli, model, effort)`) before probing, so the number of probes equals the number of distinct triples, not the number of agent steps.

### Probe minimization algorithm

```go
type probeKey struct{ cli, model, effort string }
type probeResult struct{ Strength ProbeStrength; Err error }

seenTriple := map[probeKey]probeResult{}
seenCLI := map[string]error{}

for each effective agent (collected per session-aware resolution above) {
    if _, ok := seenCLI[cli]; !ok {
        if _, err := exec.LookPath(cli); err != nil {
            seenCLI[cli] = err
            fail with structured error
        }
        seenCLI[cli] = nil
    }
    key := probeKey{cli, model, effort}
    if _, ok := seenTriple[key]; !ok {
        strength, err := registry.Adapter(cli).ProbeModel(model, effort)
        seenTriple[key] = probeResult{strength, err}
        if err != nil { fail with structured error including strength }
    }
}
```

Total external interactions = `|seenCLI|` LookPath calls + `|seenTriple|` ProbeModel calls. Within a single pre-validation run, these caches are not persisted to disk. There is no need for cross-run caching at this point; if probe latency becomes a concern, that's a separate change.

### Adapter `ProbeModel` contract with probe strength

```go
type ProbeStrength int
const (
    BinaryOnly ProbeStrength = iota // only confirmed `LookPath`; underlying CLI not consulted
    SyntaxOnly                      // adapter's own schema validated; underlying CLI not consulted
    Verified                        // underlying CLI was consulted and accepted the (model, effort) combination
)

// ProbeModel performs the lightest available check that the underlying CLI
// will accept the given (model, effort) combination. Implementations MUST NOT
// spawn a real agent invocation. The returned ProbeStrength tells the caller
// how strongly the result is grounded:
//   - Verified: the underlying CLI was asked and confirmed acceptance.
//   - SyntaxOnly: the adapter's own schema accepts (model, effort), but the
//     underlying CLI was not consulted (it may still reject at runtime).
//   - BinaryOnly: only the CLI binary's presence on PATH was confirmed.
//
// Adapters with no probe surface return BinaryOnly after a successful
// LookPath (which the pipeline has already done; ProbeModel may simply
// return BinaryOnly, nil in that case).
ProbeModel(model, effort string) (ProbeStrength, error)
```

Each existing adapter (`claude`, `codex`, `copilot`, `cursor`) implements this against its own surface. The implementer should review each adapter's CLI documentation to determine which strength is achievable; defaulting to `BinaryOnly` is acceptable for adapters where no cheap probe surface has been identified yet — and explicit, since the user will see the strength in the success summary.

This addresses a real risk: official CLI docs (Claude, Copilot) show that model/effort acceptance is dynamic, account- and policy-dependent, and may degrade or fall back rather than fail. A non-spawning probe cannot give a uniform "Verified" guarantee across all CLIs, and pretending otherwise would mislead users. The strength tag makes the guarantee explicit per call.

### Error format

Errors implement an interface that exposes structured fields and also satisfy `error`:

```go
type ValidationError struct {
    File         string   // best-effort for post-merge config; concrete for per-file errors
    LayerFiles   []string // populated for post-merge config errors instead of a single File
    StepID       string   // populated for workflow-constraint errors
    ProfileSet   string   // populated for config / profile errors
    Agent        string   // populated for config / profile errors and probe errors
    Field        string   // populated for config / schema errors
    Value        string   // populated for config / schema / probe errors
    Allowed      []string // populated where the schema or adapter exposes them
    ProbeStrength ProbeStrength // populated for probe errors
    Message      string   // human-readable summary
}
```

The CLI prints populated fields in a single-line form and omits empty fields. For deferred warnings, the same struct is populated with a `Deferred bool` and printed with a "warning:" prefix instead of "error:".

**Why best-effort file-of-record for config errors.** The current `internal/config` loader parses each layer, merges them, and validates the merged result without tracking which layer supplied each effective field. Adding per-field origin tracking is a non-trivial change to the loader (and likely a change to the YAML library or a custom parser). For this change, errors name `(profile, agent, field, value, allowed)` plus the list of layer files that were loaded — already a major usability improvement over today. Adding origin tracking is captured as a future-work item rather than blocking this change.

### Build-time validation of builtins (agent-runner repo only)

The runtime builtin-skip rule rests on a single guarantee: every embedded builtin has been pre-validated at the agent-runner repo's build time. This change adds the mechanism that delivers that guarantee. Scope: agent-runner repo only — it does not need to ship in the released binary.

**Preferred form: a Go test in the agent-runner repo.** The test iterates `builtinworkflows.List()` (or whatever the embedded-workflow enumerator is named), runs the pre-validation pipeline against each builtin in Lenient mode (so workflows with unbound required params don't false-fail), and reports any errors. Lives next to the existing tests for the loader / validate package; runs as part of `make test`. Suggested location: `internal/validate/builtins_test.go`.

```go
func TestAllBuiltinsPreValidate(t *testing.T) {
    for _, name := range builtinworkflows.List() {
        t.Run(name, func(t *testing.T) {
            _, err := validate.Pipeline(builtinworkflows.Ref(name), nil, validate.Lenient, validate.DefaultOpts())
            if err != nil { t.Fatalf("%s: %v", name, err) }
        })
    }
}
```

**Acceptable alternative: a hidden / dev-only CLI flag.** If a flag like `--validate-builtins` is wired into the binary, that's fine — but it's not required and the test should exist either way (so CI fails on `go test ./...` even without invoking the CLI).

**Why a test rather than a shipped feature.** End users of the released binary don't need to revalidate builtins themselves — the build-time guarantee is what justifies the runtime skip. Putting it in `_test.go` keeps it out of the shipped binary, runs it on every CI build automatically, and matches Go conventions for build-time invariants.

**Path-or-name `--validate` is what downstream projects use.** Because `--validate` accepts a `.yaml` / `.yml` file path directly when the file exists, downstream projects who want author-time gating on their own `.agent-runner/workflows/` can wire `agent-runner --validate <relpath>` into their CI without a path-to-name mapping shim. Whether they do this is their choice; the runner does not depend on it.

## Decisions

- **Single pipeline for `--validate` and run-time pre-validation, with a Strict / Lenient mode toggle.** Two pipelines would drift. The differences are localized to (a) the skip rule (only honored by `handleRun`, not `handleValidate`), and (b) Lenient mode allowing deferred warnings instead of errors for unresolved param-bound paths and missing required params.
- **Skip rule is builtins only.** Builtins are validated at the agent-runner repo's build time and end users get the guarantee for free. Project workflows under `<cwd>/.agent-runner/workflows/` are NOT skipped because the runner cannot verify whether the downstream project configured an analogous check. Earlier drafts of this design skipped them too; the safety gradient is wrong (most-edited files getting least validation) and the cost saved is small.
- **Captured variables fail in `workflow:` paths.** Cheap to enforce, no current workflows use the pattern, defers the alternative until there's real demand. Opting in later is a smaller change than retrofitting validation around an existing loophole.
- **One probe per unique `(cli, model, effort)` triple, no persistence.** Per-run caching is sufficient for current workflow sizes. Disk caching is a separable concern.
- **Probe strength is per-adapter and surfaced.** Adapters don't claim more than they can deliver. `BinaryOnly` is a perfectly valid result and is shown to the user as such. Avoids the trap of pretending non-spawning probes uniformly verify acceptance across CLIs whose documentation says otherwise.
- **Session-aware agent resolution mirrors runtime.** `session: resume` / `inherit` reuse the originating session's profile (with per-step overrides on top), matching `internal/exec/agent.go`. The validator does not re-resolve `step.Agent` for these cases — that would inflate the probe set with triples the runtime never actually uses.
- **`--validate` accepts a YAML file path.** Lets the agent-validator check invoke `agent-runner --validate <relpath>` directly without a path-to-name mapping shim. Detection is suffix + existence; ambiguity is unlikely because the regex for workflow names already excludes `.`.
- **Config-error file-of-record is best-effort.** Adding per-field origin tracking to the config loader is a real change with its own risks. For this change, errors name `(profile, agent, field, value, allowed)` + layer list; full origin tracking is captured as future work.
- **Resume is out of scope.** Resume already accepts that the workflow file may have been edited since the run started; pre-validating on resume would be inconsistent with that posture and would surprise users who edited a sub-workflow specifically to fix the problem they're resuming through.

## Risks / Trade-offs

- **Probe strength weaker than `Verified` is the common case at first.** Adapter implementations may default to `BinaryOnly` until somebody researches each CLI's surface. The fail-fast win is still substantial for the rest of the pipeline (layered config, profile chains, sub-workflow composition, references); explicit strength tagging means no false confidence.
- **`{{captured}}` paths now hard-fail.** Verified zero hits in the repo today. If a real future use case appears, it requires an explicit opt-in — accepted.
- **Project workflows now pay the run-time cost.** Probe deduplication keeps it small, but the cost is not zero. Trade-off accepted in exchange for not relying on an unverifiable assumption about downstream CI.
- **Probe latency adds startup time.** Each unique triple incurs one external call before the run starts. For workflows with one or two distinct triples this is negligible; for unusual configurations with many distinct triples, the user-visible delay grows linearly. Mitigation if it becomes a problem: add a per-user cache keyed on `(cli, model, effort, cli-version)`. Out of scope for this change.
- **Best-effort config file-of-record is less actionable than it could be.** Naming the bad field plus the layer list is still much better than today, but power users with multi-layer configs may have to hunt across layers to find the offending file. Adding origin tracking is the proper fix and is captured as future work.
- **Lenient mode in `--validate` may hide bugs.** A deferred warning is not an error, so a CI check using `--validate` will not fail on an unbound-param workflow path. Mitigation: success summaries surface the deferred-warning count; the check could be extended in the future to fail on warnings. Acceptable for now because the alternative (requiring per-workflow param fixtures in CI) creates more friction than it removes.
