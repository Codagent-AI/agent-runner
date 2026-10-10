- [ ] Implement the change described by `proposal.md`, `specs/`, `design.md`, and `test-plan.md` using TDD, as `CLAUDE.md` requires. Satisfy every spec scenario and every `INT-*` / `E2E-*` obligation in the test plan.

  **Engine interface** (`internal/engine/engine.go`)
  - Add `ManagesStep(stepID string) bool`.
  - Change `EnrichPrompt` to return `(string, error)` and `ValidateStep` to return `error`.
  - Change `Constructor` to return `(Engine, error)`, and make `engine.Create` propagate the error.
  - Update the engine creation sites (`cmd/agent-runner/main.go`, `internal/runner/resume.go`, `internal/prevalidate/pipeline.go`, `internal/exec/subworkflow.go`), the stub in `internal/engine/engine_test.go`, and any test fixtures that assign `EngineRef`.

  **OpenSpec engine** (`internal/engine/openspec/openspec.go`)
  - Parse and validate `artifact_steps` as `<step>: {required: [...], optional: [...]}`. Reject:
    - a value that is not a mapping;
    - unknown keys;
    - a missing or empty `required` list;
    - empty or non-string IDs;
    - an ID repeated within one entry.
  - `ManagesStep`: true for `artifact_steps` keys, otherwise for `proposal`, `specs`, `design`, and `tasks`. It must not read params or call the CLI.
  - Add an `artifactsFor` helper that returns the effective required and optional sets; an override replaces the default meaning. Add a `coveredBy` helper that returns the entry's artifacts, or `[stepID]` when there is no entry.
  - `EnrichPrompt`:
    - Resolve the change name only for managed steps.
    - Call `openspec instructions` per artifact (required first, then optional). Parse `resolvedOutputPath`, `context`, `rules`, and the dependency `id`.
    - Return an error naming the artifact on any CLI or JSON failure, with no partial output.
    - Render the output path (`resolvedOutputPath`, else `changeDir/outputPath`), the dependencies, the project context, and the rules. Omit empty sections, and omit dependencies when the session strategy is resume or inherit.
    - For multi-artifact steps: print the context once and give each artifact its own section labeled `(required)` or `(optional — only if you decide to write it)`. Drop dependencies inside the mapping, and list outside dependencies once.
    - Never render `instruction` or `template`. Delete the template-path probe.
  - `ValidateStep`: run a fresh `openspec status`. Return an error naming each required artifact that is not `done` (or is absent), and wrap CLI or parse errors. Ignore optional artifacts.
  - `ValidateWorkflow`: compute coverage as the union of `coveredBy` over the steps. Keep the skip when the change does not exist and the skip for workflows with sub-workflows.

  **Executor** (`internal/exec`)
  - In `buildAgentPrompt`, call `EnrichPrompt` only when `ManagesStep` is true. Return its error so the existing failure path fails the step before launch.
  - Add `failAgentStepForEngineValidation`. It runs only after a successful invocation of a managed step and calls `ValidateStep`. On error it:
    - sets `OutcomeFailed`;
    - appends the message to stderr;
    - prints `agent-runner: step "<id>": engine validation failed: <explanation>` through `log.Errorf`;
    - leaves `Crashed` false.
  - Call the helper in `ExecuteAgentStep` after the uncollected-calls check, and in `finishExternalUserStep` after its uncollected-calls check. Gate capture and session bookkeeping on the step still succeeding.
  - Add tests pinning sub-workflow engine inheritance and override (`NewSubWorkflowContext` already implements both).

  **Built-in workflows**
  - Add `engine: {type: openspec, change_param: change_name}` to `workflows/openspec/change-v2.0.yaml`, `plan-change-v2.0.yaml`, and `implement-change-v2.0.yaml`.
  - Add `artifact_steps` for `plan` to the existing engine block:
    - `simple-change-v1.0.yaml`: required `[proposal, tasks]`, optional `[specs, design]`;
    - `simple-change-v2.0.yaml`: required `[proposal, specs, tasks]`, optional `[design]`.
  - Leave `spec-driven/*` and `core/*` unchanged.
  - If PR #228 has merged, carry the same declarations into its v2.1 workflows and the `simple-change-plan` child, as design decision 7 describes.

  **Tests and CI**
  - Unit tests from design.md's "Testing Strategy".
  - INT-001 to INT-004 in `internal/exec`, using:
    - a stub `openspec` on `PATH`;
    - a fake `ProcessRunner`;
    - an `interactiveRunnerFn` recorder;
    - engine-call attribution by phase.
  - INT-005 against the real `openspec` CLI. Add `npm install -g @fission-ai/openspec@1.6.0` to the existing CI `test` job's setup; do not add a new job.
  - E2E-001 as a testscript next to `engine_registration.txtar`, with an exact `openspec` call sequence and the distinctive validation explanation visible in stderr.

  **Done when**
  - `make fmt`, `make lint`, and `make test` pass.
  - `openspec validate --type change feature-234-956342c3` passes.
  - Every scenario in `specs/` and every `INT-*` / `E2E-*` obligation in `test-plan.md` is covered by a passing test.
  - HT-001 is left for human acceptance.
