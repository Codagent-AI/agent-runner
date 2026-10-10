# Decisions

## proposal

1. **Verdict: go with caveats.**
   - Alternatives: no-go (remove the dead engine declarations instead); go and switch the engine on as-is.
   - Why: the issue says the engine should be used. Switching it on unchanged would inject OpenSpec's `tasks` instruction, which conflicts with `codagent:plan-tasks` and the core planning validation. So the change goes ahead, together with an enrichment fix.
   - Decision-bearing: yes.

2. **Attach the engine by declaring it on the v2 `openspec:*` parents and relying on sub-workflow engine inheritance (approach A, inheritance only as a fallback).**
   - Alternatives: B, an `engine:` field on sub-workflow steps; C, OpenSpec-specific copies of the core workflows; A with opt-in.
   - Why: `model.NewSubWorkflowContext` already inherits the parent's engine when the child declares none, which contradicts the premise in #234 and #230. Only the define and plan children have artifact step IDs, so inheritance into implement, accept, archive, and finalize has no effect. The `spec-driven:*` parents declare no engine. B adds a schema feature, and C re-duplicates what aca9f53 consolidated.
   - Decision-bearing: yes.

3. **Enrichment keeps the output path and dependencies, adds `context` and `rules`, and drops `instruction` and `template`.**
   - Alternatives: keep the current block; keep the template only.
   - Why: the `openspec-engine` spec already excludes `instruction`. The `tasks` instruction and template conflict with the format that Codagent skills and validation require. `context` and `rules` from `openspec/config.yaml` are the project-specific value that agents never see today. Dropping the template also removes a broken template-path guess.
   - Decision-bearing: yes.

4. **Run `ValidateStep` after engine-managed steps. A failure becomes a step failure that the normal failure and resume handling deals with, not a bespoke resume-or-exit prompt.**
   - Alternatives: leave it unwired (out of scope); remove `ValidateStep` from the interface and spec.
   - Why: the issue expects the engine to check artifact completion, but nothing calls `ValidateStep` today. A step failure behaves the same way in autonomous and headless runs.
   - Decision-bearing: yes.

5. **For simple-change, add an optional `artifact_steps` engine config that maps the `plan` step to several artifacts.**
   - Alternatives: leave simple-change out of scope (contradicts "should be used"); restructure it into per-artifact steps; remove its engine declaration; rely on #228's `simple-change-plan` child, which still has no artifact step IDs.
   - Why: engine config is already opaque, so no schema change is needed. It covers simple-change v1.0 and v2.0 without restructuring them.
   - Decision-bearing: yes.

6. **Edit the v1.0 and v2.0 workflow YAML in place, and carry the change into the v2.1 files if PR #228 merges first.**
   - Alternatives: create new workflow versions.
   - Why: the project is pre-release, and the change adds no params or step IDs.
   - Decision-bearing: no.

7. **Keep the `ValidateWorkflow` skip for workflows that delegate to sub-workflows. No recursive validation.**
   - Alternatives: load sub-workflows recursively to check artifact coverage.
   - Why: this is the minimum useful scope. `artifact_steps` lets simple-change pass the check on its own merits.
   - Decision-bearing: no.

8. **Not a direction-level stop.** The issue reading is clear: make the engine take effect. The A/B/C options are design choices the issue left open. There is no public interface or persisted-format break, and nothing has to change outside this repository. The overlap with PR #228 is recorded as a coordination caveat.
   - Decision-bearing: no.

## proposal-review

9. **PR-001: applied.** Enrichment for a managed step must succeed before the agent launches. `EnrichPrompt` gains an error return. CLI and JSON errors become step failures that name the artifact. A mapped step needs every one of its artifacts retrieved successfully. An empty result stays a no-op only for unmanaged steps.
   - Alternatives: keep the silent no-op on failure; log a warning and continue.
   - Why: a silent no-op brings back the "configuration silently ignored" problem that motivates this change. The existing `openspec-engine` spec already requires the CLI error to surface. `engine.Engine` is internal, so this is not a public interface break.
   - Decision-bearing: yes.

10. **PR-002: applied.** The engine gets an explicit applicability check that uses only the step ID and static config, with no parameters and no CLI call. The executor runs the enrichment and validation hooks only for managed steps. A managed step with a missing or invalid binding still errors. Verification scope now covers the full change-to-finalize lifecycle, children without `change_name` (`finalize-pr`), and steps after `archive`.
    - Alternatives: treat non-empty enrichment as proof that a step is managed; reorder `ValidateStep` internally so it checks the cached artifact IDs first; stop inheritance for children that lack `change_name`.
    - Why: inheritance does not carry parameters, so `ValidateStep` as it is would fail `finalize-pr`'s `push-pr`. Static classification is deterministic and needs no change to exist.
    - Decision-bearing: yes.

11. **PR-003: applied.** `artifact_steps` separates artifacts whose context is delivered from artifacts that must be `done`. Simple-change keeps its intentional omissions: v1.0 requires `proposal` and `tasks` (v1 sets `require_specs: false`), and v2.0 requires `proposal`, `specs`, and `tasks`. `design` (and `specs` in v1.0) is labeled optional context, so its rules apply only if the planner writes it. The enrichment never asks the agent to create an optional artifact. A probe of `openspec` 1.6.0 confirmed that `tasks` reports `done` without `design.md`. The config shape is left to design.
    - Alternatives: make every mapped artifact required; leave `design` out of the mapping.
    - Why: `codagent:simple-plan` defaults to no design document, and the review step treats omissions as intentional. Leaving `design` out entirely would lose its rules when one is written.
    - Decision-bearing: yes.

12. **No direction-level findings.** All three refine the approach within the issue's scope. None contradicts the issue or breaks a public interface or persisted format.
    - Decision-bearing: no.

## specs

13. **Add `workflow-engine-config` as a modified capability.**
    - Alternatives: leave its "Engine-aware step matching" requirement unchanged.
    - Why: the requirement said the runner passes every step ID to the hooks and the engine decides. The PR-002 applicability check changes that contract.
    - Decision-bearing: no.

14. **Classification uses `artifact_steps` keys plus the default `spec-driven` artifact IDs (`proposal`, `specs`, `design`, `tasks`).**
    - Alternatives: query `openspec status` for schema artifact IDs; an explicit list of managed steps in config.
    - Why: classification must not use params or the CLI (PR-002). Custom OpenSpec schemas are out of scope in the proposal.
    - Decision-bearing: yes.

15. **`artifact_steps` must contain at least one required artifact per mapped step, and a malformed mapping fails engine initialization. The YAML shape is deferred to design.**
    - Alternatives: allow all-optional mappings.
    - Why: a mapped step with nothing required could never fail validation, so the mapping would be meaningless.
    - Decision-bearing: no.

16. **Validation and enrichment failures become ordinary step failures that go through normal failure, resume, and repair handling. The old resume-or-exit prompt scenarios are replaced.**
    - Alternatives: keep a bespoke prompt in interactive mode.
    - Why: proposal decision 4. The behavior is the same in every mode.
    - Decision-bearing: yes.

17. **Engine hooks run only for agent steps, and `ValidateStep` is not called when the step itself failed.**
    - Alternatives: run the hooks for shell steps too.
    - Why: this matches the existing `EnrichPrompt` call site in the agent executor. Artifact steps are agent steps.
    - Decision-bearing: no.

18. **`openspec:implement-change` v2.0 declares the engine even though it has no managed steps today.**
    - Alternatives: leave it without an engine.
    - Why: the proposal and the issue list it. It keeps the `openspec:*` entry workflows consistent, and specified inheritance makes it harmless.
    - Decision-bearing: no.

19. **For a mapped step, a failure retrieving any one artifact fails the whole enrichment, and no partial block is returned.**
    - Why: PR-001 recommendation.
    - Decision-bearing: no.

## design

20. **Interface changes: add `ManagesStep(stepID) bool`; `EnrichPrompt` returns `(string, error)`; `ValidateStep` returns `error`; `Constructor` returns `(Engine, error)`.**
    - Alternatives: keep `ValidateStep` returning `(bool, error)`; validate config lazily on the first hook call.
    - Why: both validation outcomes produce the same step failure, and a malformed config fails at every engine creation site. The interface is internal.
    - Decision-bearing: yes.

21. **`artifact_steps` shape: `<step>: {required: [..], optional: [..]}`. Unknown keys, empty or duplicate IDs, and an empty `required` list are rejected. IDs are not checked against a schema at construction time.**
    - Alternatives: a flat list with `?`-suffixed optional IDs; checking IDs against `openspec status` at load time.
    - Why: explicit and easy to validate without the CLI. An unknown ID still fails at enrichment time with an error that names it.
    - Decision-bearing: no.

22. **Validation lives in the executor, next to `failAgentStepForUncollectedCalls`, on both the subprocess and the external-user paths.**
    - Alternatives: the runner loop.
    - Why: the runner loop has no per-step engine context, and the executor already owns enrichment.
    - Decision-bearing: no.

23. **Multi-artifact enrichment prints the project context once and omits dependencies.**
    - Why: the artifacts are produced together in one step, so listing each as a dependency of the others would mislead.
    - Decision-bearing: no.

24. **The end-to-end check is a testscript with a fake `openspec` on `PATH`, extending the existing `engine_registration` fixture. No new suite.**
    - Decision-bearing: no.

25. **Resolved the `artifact_steps` deferred-to-design marker in the `openspec-engine` spec with the chosen shape.**
    - Decision-bearing: no.

## test-plan

26. **Integration tests drive the real embedded workflows with a stub `openspec` on `PATH`, and a fake `ProcessRunner` for scripts and agents.**
    - Alternatives: inject a fake `CmdRunner` engine, which would bypass engine creation from YAML.
    - Why: the stub log isolates engine calls and proves that the YAML wiring and inheritance work.
    - Decision-bearing: no.

27. **INT-005 runs against the real `openspec` CLI. The CI `test` job installs `@fission-ai/openspec@1.6.0` in its existing setup, and the test fails in CI if the CLI is missing.**
    - Alternatives: fixture JSON only.
    - Why: CLI JSON drift (`context`, `rules`, `resolvedOutputPath`) is the main external risk. `CLAUDE.md` allows installing tools in the `test` job, and no new job is added.
    - Decision-bearing: yes.

28. **One E2E testscript (stub `claude` and stub `openspec`) covers enrich → fail → resume through the built binary.**
    - Decision-bearing: no.

29. **HT-001 is a real interactive `openspec:change` definition run.**
    - Alternatives: `None.`
    - Why: `CLAUDE.md` says real agent conversations need a human at a real terminal, and checking for conflicts between rules and skills is a subjective judgment.
    - Decision-bearing: no.

## approach-review

30. **AR-001: applied.** The validation helper prints `step "<id>": engine validation failed: <explanation>` through the executor logger's `Errorf`, the same channel `emitAgentFailure` uses. It keeps the audit stderr and does not set `Crashed`. The `engine-interface` spec now requires the explanation in user-facing output, including headless runs and child workflows, and that the failure is not classified as a crash. INT-001 and E2E-001 assert the distinctive explanation and a status CLI error message, not just the step label.
    - Alternatives: record a `LastFailure` check-failure record.
    - Why: verified that the runner prints only a generic `step "<id>" failed. Stopping.`, while enrichment errors already reach the logger.
    - Decision-bearing: no.

31. **AR-002: applied.** The integration tests live in `internal/exec` and replace the `interactiveRunnerFn` seam with a recorder, restored with `defer` as `crash_test.go` does. The fake `ProcessRunner` stays for headless agents and scripts. Steps are not forced to autonomous mode, so interactive prompt routing is still covered.
    - Alternatives: autonomous substitutions plus a separate routing check.
    - Why: verified that interactive steps call `interactiveRunnerFn` directly, bypassing `ProcessRunner`.
    - Decision-bearing: no.

32. **AR-003: applied.** An `artifact_steps` override is authoritative everywhere. Coverage uses `coveredBy(step)`, which returns the entry's artifacts when an entry exists and otherwise the step ID itself. Validation of an overridden default-ID step checks only the override's required set. Spec scenarios were added: an overridden step does not cover its own name, and an override replaces the step's own artifact. The design's testing strategy now includes both cases.
    - Decision-bearing: no.

33. **AR-004: applied.** Multi-artifact enrichment drops only dependencies on artifacts inside the same mapping. Outside dependencies (for example `proposal` for a `specs`+`design` step) are listed once with absolute paths. `dependency` parses the CLI's `id`. The resume and inherit omission is kept. This supersedes design decision 6 / decisions entry 23. The spec and design are aligned, and a test case was added.
    - Decision-bearing: no.

34. **AR-005: applied.** Tests attribute engine calls by phase. A harness that goes through run preparation asserts the single startup `ValidateWorkflow` `status` call, marks the stub log, and applies the per-step assertions after the mark. E2E-001 asserts an exact call sequence for both the first run and the resume. Workflow validation at startup is kept.
    - Decision-bearing: no.

35. **No direction-level findings.** All five refine the design and test plan within the approved scope.
    - Decision-bearing: no.

## tasks

36. **`tasks.md` holds exactly one implementation task covering the whole change, as instructed.** It follows the single-task format of the archived `feature-211-1543081d` change, with no `tasks/*.md` files.
    - Alternatives: split the work into per-layer task files.
    - Why: explicit step instruction.
    - Decision-bearing: no.
