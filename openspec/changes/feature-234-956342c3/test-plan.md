## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

Unit tests carry most of the logic, using a fake `CmdRunner` in `internal/engine/openspec`, a stub
engine plus a fake `ProcessRunner` in `internal/exec`, and the embed tests in `workflows`. See
"Testing Strategy" in `design.md`. That logic is:

- `ManagesStep` classification;
- `artifact_steps` config validation;
- enrichment rendering and omissions;
- enrichment and validation error propagation;
- required-only validation;
- `ValidateWorkflow` coverage;
- executor gating on both agent paths;
- sub-workflow engine inheritance and override.

The obligations below cover what those unit tests cannot prove:

- that the real built-in YAML, wired through the loader and the sub-workflow executors, enriches and validates exactly the intended steps and no others across the whole `openspec:change` lifecycle;
- that the engine's parser agrees with the real `openspec` CLI's JSON;
- that the built binary turns an engine failure into an ordinary failed run that resume can complete.

In the integration tests the engine is created from YAML, so it shells out to `openspec` with
`os/exec`. They therefore put a stub `openspec` on `PATH`. The stub serves canned
`instructions`/`status` JSON from files in the test's temp dir and appends every invocation to a
log. Workflow scripts and headless agent CLIs go through the fake `ProcessRunner`, so the stub log
records engine calls only.

The built-in define, plan, and simple-change agent steps are interactive. In production they bypass
`ProcessRunner` and run through `interactiveRunnerFn` (`internal/exec/agent.go`). The integration
tests therefore live in `internal/exec`. They replace `interactiveRunnerFn` with a recorder that
captures the args and returns a completed `interactive.DirectResult`, and restore it with `defer`, as
`crash_test.go` does. The tests do not switch steps to autonomous mode. This keeps the real loader,
the inherited engine, interactive prompt routing, and the managed-step hooks.

Engine calls are attributed by phase. When a harness goes through run preparation
(`runner.PrepareRun` / `PrepareResume`), `ValidateWorkflow` legitimately makes one startup `status`
call. Tests assert that call explicitly, then mark the stub log, and only then apply the per-step
assertions to the calls made after the mark. Harnesses that execute steps directly through the
sub-workflow executor have no startup call.

## Integration Tests

### INT-001: openspec:change definition and planning steps are enriched and validated through inheritance
- Covers: `builtin-workflows`, the requirement "OpenSpec engine on built-in openspec workflows" (the enrichment and tasks-validation scenarios); `sub-workflows`, the requirement "Engine inheritance"; `engine-interface`, the requirements "Prompt enrichment", "Step validation", and "Engine step applicability"
- Boundary: the embedded `openspec/change-v2.0.yaml` → `core/define-change-v1.0.yaml` → `core/plan-change-v1.0.yaml` runs through the real loader, runner, sub-workflow executor, and agent executor, with the engine created from the YAML `engine` block.
- Setup:
  - A temp git repo on a feature branch, with `openspec/changes/<name>/`.
  - A stub `openspec` on `PATH`. Its `instructions` responses include a `context` string and a non-empty `rules` list, plus `instruction` and `template` text containing unique sentinel strings.
  - A fake `ProcessRunner` that succeeds every script step and every headless agent.
  - A recording `interactiveRunnerFn` that captures each interactive agent's args, including the system-prompt or enrichment routing, and returns success.
  - The status responses report the artifacts as `done` after each agent step.
- Action: run `openspec:change` with `change_name` through the `plan` step (`--until plan` semantics, or the equivalent runner option).
- Assertions:
  - The `proposal`, `specs`, `design`, and `tasks` agent invocations each receive enrichment that contains the context, the rules, and the absolute output path.
  - No invocation contains either sentinel.
  - `test-plan`, `approach-review`, `repair-definition`, and the review-tasks agents receive no enrichment.
  - After the startup mark, the stub log shows `instructions` and `status` calls only for the four managed artifacts.
  - The enrichment reaches the interactive steps through the interactive routing (native system prompt or `<system>` tags), not the headless positional argument.
  - **Variant:** `status` reports `tasks` as `ready` after the `tasks` agent. The `tasks` step ends failed, and the run does not reach `review-tasks`. The logger's error output contains the step ID and the engine's distinctive explanation naming `tasks`, the `step_end` stderr carries the same text, and the step is not marked crashed.
  - **Variant:** `status` exits non-zero with a distinctive message after the `tasks` agent. That message appears in the logger's error output.
  - **Variant:** `instructions` exits non-zero for `specs`. The `specs` step fails before any agent invocation for it, and the message names `specs`.
- Execution: in `internal/exec`, following the existing builtin-workflow harnesses (`verify_change_workflow_test.go`, `implement_task_workflow_test.go`), with the `interactiveRunnerFn` recorder described above. Skipped on Windows like the other fake-CLI tests. Runs in `go test ./...` in the CI `test` job.

### INT-002: Inherited engine is inert for the rest of the openspec:change lifecycle
- Covers: `builtin-workflows`, the scenario "openspec:change lifecycle steps are unaffected"; `openspec-engine`, the requirement "Managed step classification" (no change param, after archive); `sub-workflows`, the scenario "Inherited engine in a child without the change param"
- Boundary: the embedded `openspec/change-v2.0.yaml` from `implement` through `finalize` (the core implement, accept, archive, and finalize-pr sub-workflows) runs with the engine inherited from the top level.
- Setup:
  - Same stub `openspec` as INT-001. It answers the startup `status` call, if the harness goes through run preparation, and exits non-zero on every call after the log mark, so any step-time engine call is a visible failure.
  - A fake `ProcessRunner` that scripts the headless agent, script, git, and `gh` steps to succeed along the happy path, as in the existing finalize-pr and verify-change harnesses.
  - The `interactiveRunnerFn` recorder for any interactive steps (for example in accept).
  - A temp repo with a completed task plan, so implement has one task.
  - `finalize-pr` receives no `change_name` param. The archive step's fake moves the change directory away before later steps run.
- Action: resume or run the workflow from `implement` to completion.
- Assertions:
  - The run completes successfully.
  - The stub `openspec` log has no entries after the mark. Before the mark there is at most the single expected startup `status` call.
  - No agent invocation carries enrichment.
  - No `step_end` carries an engine error.
- Execution: same location and CI phase as INT-001.

### INT-003: simple-change plan step enforces only its required artifacts
- Covers: `builtin-workflows`, the simple-change scenarios (plan without design, v1.0 plan without specs, plan missing tasks, plan receives planning context); `openspec-engine`, the requirement "Multi-artifact step mapping"
- Boundary: the embedded `openspec/simple-change-v1.0.yaml` and `simple-change-v2.0.yaml` run their top-level engine `artifact_steps` mapping for the `plan` step, through the real loader and agent executor.
- Setup:
  - The stub `openspec` returns status for `proposal`, `specs`, `design`, and `tasks` from a per-case table.
  - A fake `ProcessRunner` succeeds the `create` script.
  - The `interactiveRunnerFn` recorder captures the interactive `plan` agent's enrichment. The v1.0 `plan` step uses the lead agent's default mode; whichever path it takes is recorded.
- Action: run each workflow version through `plan` (`--until plan` semantics) for these cases:
  - v2.0 with `design` not done;
  - v1.0 with `specs` and `design` not done;
  - v2.0 with `tasks` not done.
- Assertions:
  - The first two cases pass `plan`.
  - The third fails, with a message naming `tasks`.
  - In every case, the enrichment contains the project context once, lists all four artifacts, labels `design` (and `specs` for v1.0) as optional, and contains no `instruction` or `template` sentinel.
- Execution: same location and CI phase as INT-001.

### INT-004: spec-driven:change shares the core steps without the engine
- Covers: `builtin-workflows`, the scenario "spec-driven:change runs without the engine"
- Boundary: the embedded `spec-driven/change-v2.0.yaml` runs the same core define and plan sub-workflows with no engine in effect.
- Setup: a stub `openspec` that fails on any call (this workflow has no engine, so there is no startup call either), a fake `ProcessRunner`, and the `interactiveRunnerFn` recorder.
- Action: run through the `plan` step.
- Assertions:
  - The `proposal`, `specs`, `design`, and `tasks` agents receive no enrichment.
  - The stub log is empty.
  - Steps pass or fail exactly as they did before this change.
- Execution: same location and CI phase as INT-001.

### INT-005: Engine parser contract with the real openspec CLI
- Covers: `openspec-engine`, the requirements "Prompt enrichment via openspec instructions" and "Step validation via openspec status" (real JSON shapes: `context`, `rules`, `resolvedOutputPath`, `dependencies`, file-existence status)
- Boundary: the real `openspec` CLI is invoked by the production `realCmdRunner`.
- Setup:
  - A temp project initialized with `openspec init --tools none`.
  - An `openspec/config.yaml` that sets `context` and `rules.proposal` / `rules.tasks`.
  - `openspec new change demo`, then `proposal.md` and `tasks.md` written, with no `design.md` and no specs.
- Action:
  - Construct the engine with `NewEngine` and an `artifact_steps` mapping `plan → required [proposal, tasks], optional [specs, design]`.
  - Call `EnrichPrompt` for `proposal` and for `plan`, and `ValidateStep` for `plan` and for `design`.
  - Delete `tasks.md` and call `ValidateStep("plan")` again.
- Assertions:
  - The enrichment contains the configured context and rules and the absolute `proposal.md` path.
  - It does not contain OpenSpec's instruction text, for example the phrase "Create the proposal document".
  - `ValidateStep("plan")` passes while `design.md` is absent, and fails after `tasks.md` is removed, with `tasks` named.
  - `ValidateStep("design")` fails, naming `design`.
- Execution:
  - `internal/engine/openspec`, in `go test ./...`.
  - The CI `test` job installs the CLI in its existing setup (`npm install -g @fission-ai/openspec@1.6.0`, after the existing `setup-node`). No new job is added.
  - The test fails when `CI` is set and `openspec` is missing; it is skipped locally only when the CLI is absent.

## End-to-End Tests

### E2E-001: Built binary enriches, fails, and resumes a managed child step
- Covers: `engine-interface`, the requirements "Step validation" (validation fails, and then the run is resumed) and "Prompt enrichment"; `sub-workflows`, the requirement "Engine inheritance"; `workflow-engine-config`, the requirement "Engine-aware step matching"
- Surface: the `agent-runner` binary, through testscript (`cmd/agent-runner/testdata/scripts`), extending or sitting next to `engine_registration.txtar`.
- Setup:
  - An isolated `HOME`.
  - A stub `claude` on `PATH` that records its args to a log, as in `multi_profile_active.txtar`.
  - A stub `openspec` on `PATH` that serves `instructions` JSON with a context sentinel, and `status` JSON that reports `proposal` as `done` only when `openspec/changes/demo/proposal.md` exists.
  - A project workflow whose parent declares `engine: {type: openspec, change_param: change_name}` and calls:
    - a child with no engine, which has an autonomous agent step `proposal`;
    - a grandchild with no params, which has an agent step `push-pr`.
- Journey:
  1. Run the workflow headless with `change_name=demo`. The `proposal` stub does not write the file.
  2. Create `proposal.md`.
  3. Resume the run by its run ID.
- Assertions:
  - The first run exits non-zero. Its stderr contains the step ID `proposal` and the engine's distinctive explanation naming the `proposal` artifact. Matching only the step label does not satisfy this.
  - The `claude` log shows the context sentinel in the `proposal` invocation.
  - The resumed run completes. The `push-pr` invocation has no sentinel and no engine error.
  - The stub `openspec` log matches an exact sequence. The first run makes the startup `status` call (`ValidateWorkflow`), then `instructions proposal`, then `status` (`ValidateStep`). The resume makes the startup `status` call, then `instructions proposal` and `status` for the retried step, and nothing after, so `push-pr` made no call.
- Execution: testscript in `cmd/agent-runner`, in `go test ./...`. Skipped on Windows like the other stub-CLI scripts.

## Acceptance Testing Envelope

- Environments and sandboxes:
  - A local checkout run through `./dev.sh`.
  - Throwaway temp project directories, each with `git init`, `openspec init --tools none`, and an `openspec/config.yaml` with `context` and `rules`.
  - An isolated `HOME`.
  - Headless runs, and synthetic-PTY runs per `CLAUDE.md` for navigation only. Interactive define steps that need a real conversation are out of reach; see HT-001.
- Credentials and secrets:
  - No new credentials.
  - The locally installed agent CLIs may use their existing logins, but only for the authorized effects below. Do not copy credentials into the temp `HOME`.
- Authorized effects:
  - Unlimited stub agent CLIs and stub or real `openspec` invocations in temp projects.
  - Running `openspec:change`, `openspec:plan-change`, `openspec:simple-change` (v1.0 behavior via exact refs where reachable), and `spec-driven:change` against temp repos with stub agents.
  - Inspecting audit logs (the `step_start` enrichment field) and `state.json`.
  - Clean up the temp dirs afterwards.
- Off limits:
  - Paul's real `~/.agent-runner` history and settings.
  - Any GitHub remote, `gh pr`, or real `finalize-pr` push. Use a local bare remote or a stub `gh`.
  - The live Agent Factory service.
  - Successful real-model agent sessions, which have real cost.
  - Editing this repository's own `openspec/config.yaml`.
- Permitted substitutes:
  - Stub agent CLIs instead of real ones.
  - A stub `openspec` when a specific failure (bad JSON, non-zero exit) must be induced.
  - A local bare repo instead of GitHub.
- Known risk areas:
  - The external-user (direct terminal handoff) path must validate exactly like the subprocess path.
  - Resume after an engine-validation failure, including inside nested sub-workflows.
  - Archive moving the change before later steps run.
  - Children without `change_name`.
  - A mapped artifact unknown to the schema fails only at enrichment time (accepted limitation).
  - Enrichment no longer includes OpenSpec templates (accepted).
  - Overlap with PR #228. If it has merged, re-check the v2.1 workflows and the `simple-change-plan` child carry the same declarations.

## Human-Only Testing

### HT-001: Real interactive openspec:change definition with project rules
- Reason: the define steps are interactive conversations with a real agent. `CLAUDE.md` states that flows needing a real conversation require a human at a real terminal. Judging whether the agent follows both the Codagent skill and the project's OpenSpec rules without conflict is also a subjective check of agent behavior.
- Prerequisites: INT-001 through INT-005 and E2E-001 pass. The acceptance pass has confirmed, through the audit logs, that the enrichment contains the context and rules.
- Instructions:
  1. In a scratch repo with an `openspec/config.yaml` that sets a distinctive `context` and a distinctive `rules.proposal` entry (for example "Include a Non-goals section"), launch `openspec:change` through `./dev.sh` with change name `ht-demo`.
  2. Complete the proposal step normally.
  3. In the specs step, exit the session without writing specs, then resume the run.
- Required decision or observation:
  - Whether the written proposal honors the distinctive rule while keeping the `codagent:propose` structure.
  - Whether the specs step's failure message is clear and resume returns you to a usable session.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| OpenSpec engine on built-in openspec workflows (openspec:change enrichment and tasks validation) | INT-001 | — | HT-001 |
| openspec:change lifecycle steps unaffected (implement → finalize, after archive, no `change_name`) | INT-002 | E2E-001 | — |
| simple-change required versus optional planning artifacts | INT-003 | — | — |
| spec-driven:change runs without the engine | INT-004 | — | — |
| Engine inheritance (sub-workflows) | INT-001, INT-002 | E2E-001 | — |
| Engine step applicability / engine-aware step matching | INT-001, INT-002 | E2E-001 | — |
| Prompt enrichment via openspec instructions (context, rules, exclusions, errors) | INT-001, INT-005 | E2E-001 | HT-001 |
| Step validation via openspec status / step validation as a step failure | INT-001, INT-003, INT-005 | E2E-001 | HT-001 |
| Multi-artifact step mapping | INT-003, INT-005 | — | — |
| Managed step classification without params or CLI | INT-002 | E2E-001 | — |
