## Context

The OpenSpec engine (`internal/engine/openspec/openspec.go`) implements `engine.Engine` (`internal/engine/engine.go`):

```go
type Engine interface {
    ValidateWorkflow(workflow *model.Workflow, params map[string]string, workflowFile string) error
    NeedsDeferredValidation() bool
    EnrichPrompt(stepID string, params map[string]string, opts EnrichOptions) string
    ValidateStep(stepID string, params map[string]string) (bool, error)
}
type Constructor func(config map[string]any) Engine
```

Current state, verified in the code:

- **Engine creation.** Engines are created from `model.EngineConfig{Type, Extras}` (`internal/model/step.go`, where `Extras` is the YAML-inline `map[string]any`). Creation happens in four places: `cmd/agent-runner/main.go` (top-level run), `internal/runner/resume.go` (resume), `internal/prevalidate/pipeline.go:createEngine` (pre-validation; built-in refs skip it), and `internal/exec/subworkflow.go:prepareSubWorkflow` (child workflows that declare their own engine).
- **Inheritance already works.** `model.NewSubWorkflowContext` copies `parent.EngineRef` unless the child declared an engine (`internal/model/context.go:629`). Children get only their call-local params.
- **Enrichment.** `exec.buildAgentPrompt` (`internal/exec/agent.go:1207`) calls `EnrichPrompt` for every agent step whenever an engine is in effect. A `buildAgentPrompt` error already fails the step before launch through `emitAgentFailure` → `OutcomeFailed`.
- **`ValidateStep` has no production caller.** `ValidateWorkflow` is called only for the top-level workflow (`internal/runner/runner.go:336`). `NeedsDeferredValidation` has no caller.
- **Current engine behavior:**
  - `EnrichPrompt` returns `""` on any error.
  - It includes OpenSpec's `instruction` text.
  - It looks for a template at `changeDir/../../schemas/<schema>/templates/<id>.md`, which never exists for built-in schemas.
  - It ignores `context` and `rules`.
  - `ValidateStep` resolves `change_name` and may run `openspec status` before it checks whether the step is managed.
- **Agent step outcomes are decided in two places:**
  - the subprocess path in `ExecuteAgentStep`, right after `InvokeAgent` (next to `failAgentStepForUncollectedCalls`);
  - the external-user (direct terminal handoff) path in `finishExternalUserStep` (`internal/exec/external_user.go:314`).

  Both end in `finishAgentStep`, which emits `step_end`.
- **`openspec` 1.6.0 JSON shapes.** `openspec instructions <id> --change <name> --json` returns these keys: `changeDir`, `outputPath`, `resolvedOutputPath`, `instruction`, `context` (string, absent when unset), `rules` (string array, absent when unset), `template`, and `dependencies` (`[{id, done, path, description}]`). `openspec status --change <name> --json` returns `artifacts: [{id, outputPath, status}]`, and status is based on whether the file exists, so `tasks` reports `done` without `design.md`.
- **Overlap with PR #228.** PR #228 (open) adds `root_param` to the engine and v2.1 `openspec:*` workflows. It touches `openspec.go`, `subworkflow.go`, and the same YAML files.

## Goals / Non-Goals

**Goals:**
- The engine enriches and validates the `proposal`, `specs`, `design`, and `tasks` steps of `openspec:change` / `plan-change` v2.0 runs, through inheritance into `core/define-change-v1.0.yaml` and `core/plan-change-v1.0.yaml`.
- Enrichment carries the output path, dependencies, the project `context`, and per-artifact `rules`, and never OpenSpec's `instruction` or `template`.
- Enrichment and validation failures on managed steps become ordinary step failures.
- Unmanaged steps never touch params or the `openspec` CLI, even in children without `change_name` or after archive.
- `openspec:simple-change` v1.0 and v2.0 get combined enrichment and required-only validation on their `plan` step.

**Non-Goals:**
- An `engine:` field on sub-workflow steps; per-call inheritance opt-out.
- Recursive `ValidateWorkflow`, or wiring `NeedsDeferredValidation`.
- Custom OpenSpec schemas beyond what the CLI reports; changes to Codagent skills; `spec-driven:*` workflows.
- External OpenSpec roots (`root_param`, PR #228).

## Approach

### 1. Engine interface (`internal/engine/engine.go`)

```go
type Engine interface {
    ValidateWorkflow(workflow *model.Workflow, params map[string]string, workflowFile string) error
    NeedsDeferredValidation() bool
    // ManagesStep reports whether the engine owns stepID. It must depend only on
    // stepID and engine config: no params, no I/O, no error.
    ManagesStep(stepID string) bool
    // EnrichPrompt is called only for managed steps. An error fails the step.
    EnrichPrompt(stepID string, params map[string]string, opts EnrichOptions) (string, error)
    // ValidateStep is called only after a managed step succeeds. A nil result
    // means the step passes; any error fails the step with that message.
    ValidateStep(stepID string, params map[string]string) error
}
type Constructor func(config map[string]any) (Engine, error)
```

- `engine.Create` propagates the constructor error. The four creation sites already wrap `Create` errors (`create engine: …`), so a malformed `artifact_steps` fails at run start, during pre-validation for project workflows, and when a child workflow loads.
- `ValidateStep` collapses "not done" and "CLI error" into one `error`, because the spec handles both the same way: the step fails with the engine's explanation.
- The test stub in `internal/engine/engine_test.go` and the fixtures in `internal/exec` that assign `EngineRef` are updated to the new signatures.

### 2. OpenSpec engine (`internal/engine/openspec/openspec.go`)

**Config.** The existing `change_param` key stays as it is, and a new `artifact_steps` key is added:

```yaml
engine:
  type: openspec
  change_param: change_name
  artifact_steps:
    plan:
      required: [proposal, specs, tasks]
      optional: [design]
```

`NewEngineWithRunner` parses `artifact_steps` from `map[string]any`, as decoded from YAML. It returns an error when:
- `artifact_steps` is not a mapping;
- a value is not a mapping;
- a value has keys other than `required` and `optional`;
- `required` is missing or empty;
- an entry is not a non-empty string;
- an artifact ID appears twice in one step, in either list.

Artifact IDs are not checked against a schema, because the CLI is not consulted at construction time. An unknown ID surfaces as an enrichment error that names it.

The parsed form:

```go
type stepArtifacts struct {
    required []string // ordered as written
    optional []string
}
// artifactSteps map[string]stepArtifacts
```

**Classification.** `ManagesStep(id)` returns true when `artifactSteps[id]` exists. Otherwise it returns true when `id` is one of `proposal`, `specs`, `design`, or `tasks` (the `spec-driven` defaults). In that case the step is treated as `{required: [id]}`. An `artifact_steps` entry keyed by a default ID replaces the default meaning everywhere: classification, enrichment, validation, and workflow coverage. A helper `artifactsFor(id) (stepArtifacts, bool)` returns the effective set and serves all three hooks. A separate `coveredBy(id) []string` returns the entry's required and optional artifacts when an entry exists, and otherwise `[id]`; `ValidateWorkflow` uses it.

**`EnrichPrompt`.**
1. Resolve the change name, which errors for a managed step that lacks the param.
2. For each artifact (required first, then optional), run `openspec instructions <id> --change <name> --json` and parse it into `instructionsOutput`. That type gains `ResolvedOutputPath string`, `Context string`, and `Rules []string`. Any CLI or parse failure returns `fmt.Errorf("openspec instructions for artifact %q: %w", id, err)`, and no partial block is returned.
3. Render the block:
   - **Single-artifact step** (a default ID, or a mapped step with exactly one artifact and no optional ones):
     ```
     **Output path:** <resolvedOutputPath, else changeDir/outputPath>

     **Dependencies:**            (omitted when the session strategy is resume or inherit, or when the list is empty)
     - <changeDir/dep.path> — <description>

     **Project context:**         (omitted when empty)
     <context>

     **Rules for <id>:**          (omitted when empty)
     - <rule>

     Write your output to the output path.
     ```
   - **Multi-artifact step:**
     ```
     **Project context:**         (once, from the first artifact; omitted when empty)
     <context>

     ### <id> (required)
     **Output path:** …
     **Rules:**                   (omitted when empty)
     - …

     ### <id> (optional — only if you decide to write it)
     **Output path:** …
     **Rules:** …
     ```
     For multi-artifact steps, dependencies on artifacts inside the same mapping are dropped, because they are produced in that step. Dependencies outside the mapping are kept and listed once each in a `**Dependencies:**` block after the project context (deduplicated by artifact ID). The resume and inherit omission still applies. `dependency` gains an `ID string` field (`id` in the CLI JSON) so the engine can tell which dependencies are inside the mapping.
   - `instruction` and `template` are never rendered. The template-path probe and its `os.Stat` are deleted.

**`ValidateStep`.**
1. Resolve the change name.
2. Run `openspec status --change <name> --json` once, fresh, without using the cache.
3. Collect the required artifacts whose status is not `done`, including those absent from the status output.
4. If any were collected, return `fmt.Errorf("openspec artifact(s) not done for change %q: %s", name, "tasks (ready), …")`. A CLI or parse error is wrapped and returned the same way.

Optional artifacts are never checked.

**`ValidateWorkflow`.** As today, it loads artifact IDs from `openspec status`, skipping the check when the change does not exist. It then marks as covered the union of `coveredBy(step.ID)` over the workflow's steps. A step with an override therefore covers only its mapped artifacts, not its own name. The skip for workflows that have sub-workflows is kept.

**`ensureArtifactIDs` / `artifactIDs` cache.** These are no longer used by `EnrichPrompt` or `ValidateStep`. The cache is kept only as `ValidateWorkflow`'s result, for `NeedsDeferredValidation`, so that method's behavior is unchanged.

### 3. Executor wiring (`internal/exec`)

- **`buildAgentPrompt`:**
  ```go
  if eng, ok := ctx.EngineRef.(engine.Engine); ok && eng != nil && eng.ManagesStep(step.ID) {
      enrichment, err = eng.EnrichPrompt(step.ID, ctx.Params, engine.EnrichOptions{SessionStrategy: string(step.Session)})
      if err != nil { return "", "", fmt.Errorf("engine enrichment for step %q: %w", step.ID, err) }
  }
  ```
  The existing error path then fails the step before any adapter is resolved or any process is spawned.
- **New helper** `failAgentStepForEngineValidation(step, ctx, invocation *AgentInvocationResult)`:
  - It runs only when `invocation.Outcome == OutcomeSuccess` and the engine manages the step.
  - It calls `ValidateStep`. On error, it sets `invocation.Outcome = OutcomeFailed` and appends the message to `invocation.Stderr`, the same way `failAgentStepForUncollectedCalls` does.
  - It also prints `agent-runner: step "<id>": engine validation failed: <explanation>` through the executor's `Logger.Errorf`. This uses the same channel as `emitAgentFailure`, which already surfaces enrichment errors. Without it, the explanation would sit only in the audit stderr, because the runner prints just a generic `step "<id>" failed. Stopping.`
  - It leaves `invocation.Crashed` false, so the failure is classified as an ordinary step failure, not an infrastructure crash.
  - It is called in `ExecuteAgentStep` immediately after the uncollected-calls check, before capture and session bookkeeping.
  - It is called in `finishExternalUserStep` right after that function's uncollected-calls check. The capture block must then re-check `result.Outcome == OutcomeSuccess`.

  Because the step ends as `OutcomeFailed` with stderr, it flows through `finishAgentStep` → `step_end`, the run's failure handling, repair, and resume with no new UI.
- **Call-agent children** (`agent_call.go`) keep ignoring `EngineRef`. They are not workflow steps.
- **Sub-workflows** need no code change: `prepareSubWorkflow` already creates a child engine only when the child declares one, and `NewSubWorkflowContext` already falls back to the parent's engine. The sub-workflows spec requirement for this behavior is new, so it is pinned with tests.

### 4. Built-in workflows

- `workflows/openspec/change-v2.0.yaml`, `plan-change-v2.0.yaml`, and `implement-change-v2.0.yaml` each add:
  ```yaml
  engine:
    type: openspec
    change_param: change_name
  ```
- `workflows/openspec/simple-change-v1.0.yaml` adds:
  ```yaml
    artifact_steps:
      plan:
        required: [proposal, tasks]
        optional: [specs, design]
  ```
  to its existing engine block. `simple-change-v2.0.yaml` adds the same, with `required: [proposal, specs, tasks]` and `optional: [design]`.
- `openspec/plan-change-v1.0.yaml` is unchanged. Its per-artifact steps are managed through the default IDs and now get validated too.
- `spec-driven/*` and `core/*` are unchanged.

Data flow for `openspec:change`:

```
openspec:change (engine E, change_name=X)
 ├─ create             shell  → no hooks
 ├─ define → core/define-change (inherits E, params change_name=X)
 │    ├─ proposal      ManagesStep ✓ → EnrichPrompt → agent → ValidateStep
 │    ├─ specs / design  same
 │    └─ test-plan, approach-review   ManagesStep ✗ → no hooks
 ├─ plan → core/plan-change (inherits E)
 │    └─ tasks         ManagesStep ✓ → EnrichPrompt → agent → ValidateStep
 ├─ implement / accept / archive / finalize (inherit E)
 │    └─ every agent step   ManagesStep ✗ → no params read, no CLI
```

## Decisions

1. **Use an explicit `ManagesStep` rather than inferring management from non-empty enrichment.** This is required so that unmanaged steps need no params and make no CLI call (PR-002). It also makes the "empty enrichment is a valid no-op" case unambiguous.
2. **Classify against static defaults plus `artifact_steps`, not against `openspec status`.** Classification must work before the change exists, after archive, and in children without `change_name`. The trade-off is that a custom schema's extra artifacts are not auto-managed. They can still be mapped explicitly with `artifact_steps`.
3. **Use a single `error` return from `ValidateStep`.** Both outcomes produce the same step failure, and the error carries the artifact names the spec requires.
4. **`Constructor` returns `(Engine, error)`.** This fails a malformed `artifact_steps` early at every creation site, and fits PR #228's `root_param` validation.
5. **Validate inside the executor, next to the uncollected-calls check, not in the runner loop.** The runner loop does not know about engines per step, and sub-workflow scopes carry their own `EngineRef`. The executor already owns enrichment, so both hooks live in one package.
6. **In multi-artifact enrichment, drop only dependencies inside the mapping.** Artifacts produced in the same step are not inputs. Artifacts outside the mapping, such as `proposal` for a `specs`+`design` step, still are, so they are listed.
7. **Edit the v1.0 and v2.0 YAML in place.** The project is pre-release, and no params or step IDs change. If PR #228 merges first, the same `engine` and `artifact_steps` lines go into its v2.1 files: the `simple-change-plan` child gets `artifact_steps` for its `plan` step, and the v2.1 `change`, `plan-change`, and `implement-change` declare the engine with `root_param`.

## Risks / Trade-offs

- **Interactive define steps can now fail after the session ends.** If the user leaves the `proposal` session without the file being written, the step fails instead of moving on to `specs`. This is intended. Resume reruns the step.
- **A mapped artifact the CLI doesn't know about** (a typo, or a schema without `design`) fails enrichment for that step at runtime, not at load time. The error names the artifact.
- **Dropping the template changes `openspec:plan-change` v1.0 prompts.** In practice they had no template, because the probed path never exists for built-in schemas.
- **Conflicts with PR #228.** Expect textual conflicts in `openspec.go` (config parsing, `getChangeName`), `engine.go`, and the workflow YAML. They are mechanical. Decision 7 records how to carry the change forward.
- **`openspec` CLI field drift.** `context`, `rules`, and `resolvedOutputPath` are all optional in the parser. A missing `resolvedOutputPath` falls back to `changeDir/outputPath`.

## Testing Strategy

- **`internal/engine/openspec`** (fake `CmdRunner` that records calls):
  - `ManagesStep` for defaults, for mapped steps, for an override of a default ID, and for unrelated IDs, with zero CLI calls.
  - Config errors for each malformed `artifact_steps` shape.
  - Enrichment includes the output path, dependencies, context, and rules.
  - Enrichment never includes `instruction` or `template`.
  - Empty sections are omitted, and dependencies are omitted for resumed sessions.
  - Multi-artifact rendering labels optional artifacts and prints context once.
  - A fresh `specs`+`design` mapped step keeps its `proposal` dependency, listed once, and drops dependencies inside the mapping.
  - An instructions CLI or JSON failure, including a failure on one artifact of a mapped step, returns an error naming that artifact.
  - `ValidateStep`: passes when the required artifacts are done; a missing optional artifact is ignored; failures name the artifacts that are not done; status CLI errors propagate.
  - `ValidateWorkflow` counts mapped artifacts as covered, and a `proposal` step overridden to `[tasks]` leaves `proposal` uncovered.
  - `ValidateStep` on an overridden default-ID step checks only the override's required set.
- **`internal/engine`:** `Create` propagates constructor errors.
- **`internal/exec`** (stub engine plus fake process runner):
  - An unmanaged step calls no hooks, including when params lack `change_name`.
  - An enrichment error fails the step without spawning.
  - A validation error turns a successful invocation into `OutcomeFailed` with the message in stderr, on both the subprocess and the external-user path.
  - The validation error is printed through the logger with the step ID, and the step is not marked crashed.
  - A failed step does not call `ValidateStep`.
  - Sub-workflow inheritance: a child without an engine uses the parent's engine with the child's params; a child with its own engine overrides it.
- **Builtin-workflow integration harness** (`internal/exec`):
  - Built-in define-change, plan-change, and simple-change steps are interactive. The tests replace the package's `interactiveRunnerFn` seam with a recorder that captures args and returns a completed `interactive.DirectResult`, and restore it with `defer`, as `crash_test.go` already does.
  - The fake `ProcessRunner` keeps handling headless agents and scripts.
  - This preserves the real loader, the inherited engine, interactive prompt routing, and the managed-step hooks without a TTY.
- **`workflows`** (embed tests):
  - `openspec/{change,plan-change,implement-change}-v2.0.yaml` declare the openspec engine; `spec-driven/*` declare none.
  - Every embedded engine block constructs successfully.
  - simple-change v1.0 and v2.0 have the required and optional sets above.
- **`cmd/agent-runner` testscript:**
  - Extend `engine_registration.txtar`, or add a sibling, to put a fake `openspec` script on `PATH` with a parent-declared engine and a child workflow.
  - Assert that the child's managed step is enriched, and that it fails with the engine's distinctive explanation visible in stderr when the artifact is not done.
  - Assert that unmanaged child steps without `change_name` pass.
  - Assert the stub `openspec` call log as an exact sequence. Run preparation legitimately makes one startup `status` call (`ValidateWorkflow`).
  - This exercises the real loader, inheritance, and executor wiring end to end without a live agent.

## Migration Plan

No data migration. Run state and the workflow schema are unchanged, and `engine` extras were already opaque. Runs that resume across the upgrade pick up the new hooks on the next managed step. To roll back, revert the commit; the workflows would then have engine declarations that the old code treats as before.

## Open Questions

None.
