## Why

The OpenSpec engine (`internal/engine/openspec`) is meant to tie the built-in `openspec:*` workflows to the project's OpenSpec setup. Today it does nothing in the workflows people actually run:

- `openspec:change`, `openspec:plan-change`, and `openspec:implement-change` v2.0 declare no engine. Their artifact steps (`proposal`, `specs`, `design`, `tasks`) live in the shared `core/define-change-v1.0.yaml` and `core/plan-change-v1.0.yaml`, so no step is ever enriched.
- `openspec:simple-change` v1.0 and v2.0 declare the engine, but they write every artifact in one `plan` step. No step ID matches an artifact ID, so the engine never acts. `ValidateWorkflow` also skips its missing-artifact check because the workflow has sub-workflows.
- `openspec:plan-change` v1.0 is the only workflow where the engine acts.

Looking at the code shows three more problems that matter more than whether the engine is declared:

1. **Engine inheritance already exists but is not specified.** `model.NewSubWorkflowContext` falls back to the parent's engine when the child declares none (`internal/model/context.go:629`). #234 and #230 assumed the opposite. If an `openspec:*` parent declares the engine, the shared core children already pick it up at runtime. No spec or test covers this behavior.
2. **Turning the engine on as it is would make agent output worse.** `EnrichPrompt` passes on OpenSpec's `instruction` text, even though the `openspec-engine` spec says it must be left out. For `tasks`, that text requires a single checkbox `tasks.md`, which contradicts the `tasks/*.md` files plus index that `codagent:plan-tasks` and the core planning validation require. The engine also drops the two fields that would help: the project `context` and the per-artifact `rules` from `openspec/config.yaml`. OpenSpec documents these as "shown to AI when creating artifacts". The v2 workflows never pass them to agents today.
3. **`ValidateStep` is never called.** The `engine-interface` spec requires it, and the engine implements it, but nothing in the runner or executors calls it. The engine therefore cannot catch an artifact step that finishes without writing its artifact.

This matters now because the OpenSpec setup in the v2 lifecycle is configuration that gets silently ignored. PR #228 (#227) is also reworking the same workflows and engine, so the intended engine behavior should be settled before more structure is built on a no-op.

## What Changes

- **Declare the engine on the v2 `openspec:*` entry workflows** (`change`, `plan-change`, `implement-change`) and rely on engine inheritance so the shared core define and plan sub-workflows are enriched. The `spec-driven:*` parents declare no engine, so the same core children stay engine-free there.
- **Make engine inheritance specified behavior.** A sub-workflow that declares no engine runs under its parent's engine. A sub-workflow that declares its own engine uses that engine instead.
- **Change what enrichment contains.** It keeps the resolved output path and the dependency paths. It adds the project `context` and the artifact's `rules` from `openspec instructions --json`. It drops OpenSpec's `instruction` and `template`, because the step prompt's Codagent skill owns the document structure.
- **Fail on enrichment errors for managed steps.** Today, when `openspec instructions` fails or returns bad JSON, the step silently runs without enrichment. Instead, enrichment for a managed step must succeed before the agent launches. A CLI or JSON error fails the step through normal failure handling, and the error names the affected artifact. For a step that covers several artifacts, every one of them must be retrieved successfully. An empty result stays a valid no-op only for steps the engine does not manage.
- **Run `ValidateStep`.** After an engine-managed step succeeds, the runner checks that the artifact's `openspec status` is `done`. If it is not, the step fails with a descriptive error and goes through the run's normal failure and resume handling.
- **Make unmanaged steps safe under an inherited engine.** Deciding whether the engine manages a step must not require the change parameter and must not call OpenSpec. Inherited children such as `finalize-pr`, which receive no `change_name`, and steps after `archive` has moved the change away, both bypass enrichment and validation. A managed step with a missing or invalid binding still fails with an error.
- **Let one step cover several artifacts, with required and optional artifacts kept apart.** An optional `artifact_steps` key in the openspec engine config maps a step ID to artifacts. It separates artifacts whose context is delivered from artifacts that must be `done` for the step to succeed. `openspec:simple-change` maps its `plan` step to all four planning artifacts for context. It keeps its existing intentional omissions:
  - v1.0 requires `proposal` and `tasks`.
  - v2.0 requires `proposal`, `specs`, and `tasks`.
  - `design` (and `specs` in v1.0) is delivered as clearly labeled optional context. Its rules apply only if the planner chooses to write it.
  - The enrichment never asks the agent to create an optional artifact just to satisfy validation.
  - `openspec status` marks `tasks` as `done` even when `design.md` is absent, so this policy matches the CLI.
  - `ValidateWorkflow` counts mapped artifacts as covered.

No **BREAKING** changes. The workflow YAML schema is unchanged because engine config is already opaque and passed through to the engine. Persisted run state is unchanged.

## Capabilities

### New Capabilities
- None.

### Modified Capabilities
- `openspec-engine`: Enrichment contents (adds `context` and `rules`, drops `instruction` and `template`). An optional `artifact_steps` mapping with required and optional artifacts. Enrichment errors surface for managed steps. Unmanaged-step classification needs no parameters and no CLI call. Validation accounts for mapped steps.
- `engine-interface`: The engine reports whether it manages a step. Enrichment can return an error, and for a managed step that error becomes a step failure. `ValidateStep` actually runs after managed steps, and a failure becomes a step failure that the normal resume path handles. Unmanaged steps never call the enrichment or validation hooks.
- `sub-workflows`: A sub-workflow that declares no engine inherits its parent's engine.
- `workflow-engine-config`: Step matching asks the engine whether it manages a step before calling any hook. An engine may map step IDs to entities through its own configuration.
- `builtin-workflows`: The v2 `openspec:*` entry workflows declare the OpenSpec engine, and `openspec:simple-change` maps its `plan` step to the planning artifacts.

## Technical Approach

- **Attachment (approach A from the issue, with inheritance only as the fallback):** the inheritance already exists in `NewSubWorkflowContext`, so this change specifies it and tests it rather than building it. Among the core children, only `define-change` (`proposal`, `specs`, `design`) and `plan-change` (`tasks`) have managed step IDs. Inheritance does not carry engine parameters, though, because children get only their call-local params. Today `ValidateStep` resolves `change_name` and may call `openspec status` before it checks whether the step is managed. Wiring it like the existing unconditional `EnrichPrompt` call would therefore fail unrelated steps, such as `finalize-pr`'s `push-pr` agent, which has no `change_name`. The engine instead gets an explicit applicability check that uses only the step ID and static config (default artifact IDs plus `artifact_steps`). The executor calls the enrichment and validation hooks only for steps that check reports as managed. A non-empty enrichment result is never treated as proof that a step is managed. Rejected alternatives: B (an `engine:` field on sub-workflow steps) adds a schema feature for something inheritance already handles. C (OpenSpec-specific copies of the core workflows) undoes the consolidation from aca9f53.
- **Enrichment:** extend `instructionsOutput` with `context` and `rules`. Prefer `resolvedOutputPath` when the CLI provides it. Build the block from the output path, dependencies, context, and rules only. Removing the template also removes the broken `changeDir/../../schemas/...` path guess. `EnrichPrompt` gains an error return (the `engine.Engine` interface is internal), and the agent executor turns that error into a step failure for managed steps.
- **Step validation:** call `ValidateStep` in the agent step executor after a successful managed step. Return a failure, not an interactive prompt, so autonomous and headless runs behave the same way and the existing resume and repair paths apply. For mapped steps, only the required artifacts are validated. `design.md` settles the exact wiring, the applicability-check signature, the `artifact_steps` config shape, and how resumed and inherited sessions behave.
- **Multi-artifact steps:** `artifact_steps` lives in the openspec engine config, not on steps, so `workflow-engine-config`'s step-ID convention stays the default and no loader schema change is needed.
- **Workflow versions:** edit the v2.0 and v1.0 YAML in place. The project is pre-release, and the change adds no params or step IDs. If PR #228 merges first, carry the same declarations into its v2.1 files and its hidden `simple-change-plan` child instead.

## Out of Scope

- Changes to `spec-driven:*` workflows, or applying OpenSpec behavior to non-OpenSpec runs.
- A new `engine:` field on sub-workflow steps, and per-call opt-in or opt-out of inheritance.
- Recursive `ValidateWorkflow` that loads sub-workflows to verify artifact coverage. Top-level workflows that delegate keep the existing skip.
- Changes to the Codagent skills (`codagent:propose`, `spec`, `design`, `plan-tasks`, `simple-plan`).
- External OpenSpec roots and `root_param` (#227 / PR #228). This change must stay compatible with them.
- Supporting OpenSpec schemas other than the default `spec-driven` beyond what `openspec status` and `openspec instructions` already report.

## Impact

- **Code:**
  - `internal/engine/engine.go`: applicability check, and an error return from `EnrichPrompt`.
  - `internal/engine/openspec/openspec.go`: enrichment, `artifact_steps`, validation, and parameter-free classification.
  - `internal/exec/agent.go`: hooks run only for managed steps; enrichment errors and `ValidateStep` failures become step failures.
  - `internal/exec/subworkflow.go` / `internal/model/context.go`: inheritance tests only, unless design finds a gap.
  - Engine and openspec tests.
- **Verification scope:** the full `openspec:change` lifecycle from `create` through `finalize`. This includes inherited child calls that receive no `change_name` (for example `finalize-pr`) and steps after `archive` has moved the change, as well as both simple-change versions with and without an optional `design.md`.
- **Workflows:** `workflows/openspec/change-v2.0.yaml`, `plan-change-v2.0.yaml`, `implement-change-v2.0.yaml`, `simple-change-v1.0.yaml`, `simple-change-v2.0.yaml`, and possibly `plan-change-v1.0.yaml` if validation timing differs.
- **Agent behavior:** proposal, specs, design, and tasks prompts in `openspec:*` runs get an extra context block with the project's OpenSpec context and rules. An artifact step that does not produce a required artifact now fails right away instead of at the later planning-validation step. An `openspec instructions` failure now fails a managed step before launch instead of silently running without the project's rules. Simple-change plans that intentionally omit optional artifacts still pass.
- **Runtime dependency:** the `openspec` CLI is needed during define and plan steps of `openspec:change` runs. Those runs already require it for `create-change.sh` and `openspec validate`.
- **Coordination:** this overlaps PR #228 in `openspec.go`, `subworkflow.go`, the `openspec:*` workflow files, and the `openspec-engine` spec. Whichever change merges second must rebase onto the other.

## Verdict

**Go with caveats.** The engine is worth using, but only after its enrichment is fixed. Switching it on as-is would inject task-format instructions that conflict with the core planning validation, while still dropping the project context and rules that are its main value. The caveats are the overlap with PR #228 and a visible change in agent prompts for `openspec:*` runs.
