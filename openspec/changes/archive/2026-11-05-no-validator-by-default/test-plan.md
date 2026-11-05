## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only the additional integration and end-to-end obligations, the envelope for acceptance testing, and any exceptional human-only obligations.

The unit layer carries most of the coverage:
- parameter alias binding;
- `ResolveRelativeWorkflowPath` with absolute paths;
- the pre-validation walk rules;
- the bookkeeping gate's decision table;
- `check-draft-pr.sh`'s `accept_ready` mode;
- the composition-graph scan in `workflows/embed_test.go`. That scan is a code test of workflow structure and step commands, not of prompt wording.

The integration layer runs the real embedded workflow YAML through `internal/exec` against temporary git repositories, using the stub runners established in `internal/exec/verify_change_workflow_test.go`. Agents, `gh`, and `agent-validator` are deterministic stubs on `PATH`. The only exception is INT-010, which uses the real Validator build CI already provides.

There is no full-lifecycle automated E2E. Running a whole `change` needs live agents, so the exploratory acceptance pass covers that with real CLIs.

## Integration Tests

### INT-001: Built-in validation workflows behave per selection
- Covers: Built-in validation workflows; Unresolved validation warns and continues; repair session routing.
- Boundary:
  - `core/validation-auto-v1.0.yaml`, `validation-agent-validator-v1.0.yaml`, and `validation-none-v1.0.yaml` executed as sub-workflows by a small test parent that declares `implementor-agent` and `lead-agent`;
  - the real `run-validator.sh`, against a stub `agent-validator` executable.
- Setup: a temporary git repo, with and without `.validator/config.yml`, and with the stub on `PATH` or absent. The stub passes, fails once then passes, or always fails, per test case.
- Action: run the parent with each selection, and with `repair_session` set to `implementor-agent`, `lead-agent`, and an invalid value.
- Assertions:
  - `auto` without config invokes no `agent-validator`, needs no executable, and writes `SKIPPED`.
  - `auto` with config created by an earlier parent step runs the validator.
  - Explicit `agent-validator` without config or without the executable fails, naming what is missing.
  - `none` invokes nothing.
  - Repairs run in the named session: the stub runner records a resume of the parent's existing session ID, not a new one and not the most recent session.
  - An invalid `repair_session` fails before any validator command.
  - Exhausted repairs end with warning outcome and `FAIL` in `result_file`, and the parent continues.
  - A custom absolute-path validation workflow is called with `warn_on_failure: true`, as every caller does. When it fails outright, the parent continues with a warning. When it succeeds without writing `result_file`, the parent completes without a warning, and `validation-handoff.sh` reports "recorded no result" rather than a failure.
- Execution: `internal/exec`, `go test ./...` in the CI `test` job.

### INT-002: verify-change v2.0 validates once after acceptance and pushes
- Covers: Validation points in change workflows (point 1); Validation fixes reach the pull request; Tested and validated revisions tracked separately.
- Boundary: `core/verify-change-v2.0.yaml` with its real scripts (`acceptance-gate.sh`, `acceptance-push.sh`, `check-draft-pr.sh`, `validation-handoff.sh`), a stub `gh`, a bare remote, and a stub validator that commits a fix.
- Setup: extend `acceptanceRoundRunner`, covering:
  - a tester ready in round 2 after one lead fix;
  - a tester ready in round 1;
  - rounds exhausted.
- Action: run the workflow with `validation_workflow` set to the agent-validator built-in.
- Assertions:
  - No validator runs before the draft PR or between rounds.
  - Validation runs exactly once after the rounds, with repairs resuming `implementor-agent`.
  - Commits are pushed, and the head check passes for both a draft and a ready PR.
  - `acceptance-preparation-status.txt` stays `ACCEPTANCE_COMPLETE` for the tested revision.
  - The handoff's `## Validation` section lists the commits between the tested revision and HEAD, and states when validation is unresolved.
  - A red validation still reaches the handoff, with a warning outcome.
- Execution: `internal/exec`, CI `test` job.

### INT-003: accept-change v2.0 validates after review settles and retests behavior changes
- Covers: Validation points in change workflows (point 2 and the retest); Tested and validated revisions tracked separately (the re-acceptance recommendation and a declined re-acceptance).
- Boundary: `core/accept-change-v2.0.yaml` with stubbed interactive lead and tester sessions, real gate scripts, and a stub validator.
- Setup, three cases:
  - validation makes no commits;
  - validation commits a fix, and the lead judges it behavior-changing;
  - re-acceptance is declined, leaving untested commits.
- Action: run the workflow from `resolve-assumptions` through to the end.
- Assertions:
  - Validation runs after the re-acceptance gate, never before, with `lead-agent` repairs.
  - The push and the head check follow validation.
  - `retest-validation-fixes` is skipped when HEAD is unchanged, and resumes `acceptance-tester` when HEAD moved.
  - The handoff records the untested commits as not exploratory-tested on decline, and records nothing when the tested revision equals HEAD.
  - A red validation continues with a warning.
- Execution: `internal/exec`, CI `test` job.

### INT-004: finalize-pr v2.0 validates each CI fix before pushing
- Covers: Validation points in change workflows (point 3); Unresolved validation warns and continues (CI fix).
- Boundary: `core/finalize-pr-v2.0.yaml` with the real `ci-status-gate.sh` and `ci-fix-needed-gate.sh`, stub CI reports, a stub `gh`, and a bare remote.
- Setup: CI fails once and then passes. A second case has the validator always fail.
- Action: run the workflow.
- Assertions:
  - The order of steps after a CI failure is: fix commit, validation, push, head check accepting a ready PR, CI wait.
  - The fix step's commit is not on the remote before validation ends.
  - After a successful `fix-pr`, validation still runs. This is the regression case for skip conditions reading the fixer's outcome.
  - Red validation still pushes and leaves a warning.
  - No validation runs when CI is green on the first wait, and the whole `ci-fix` group is skipped.
- Execution: `internal/exec`, CI `test` job.

### INT-005: complete-simple-change v2.0 validates twice with the right fixers
- Covers: Validation points in simple-change workflows; repair session routing.
- Boundary: `core/complete-simple-change-v2.0.yaml` composing `core/implement-task-v2.0.yaml`, with stubbed sessions and a stub validator.
- Setup: a `tasks.md` with multiple tasks. The user's review requests one fix.
- Action: run the workflow.
- Assertions:
  - No validation runs during implementation.
  - Validation runs after flow testing, with repairs resuming the session the implement step created (the same session ID).
  - Validation runs once after review, with repairs in `lead-agent`.
  - Nothing is pushed.
- Execution: `internal/exec`, CI `test` job.

### INT-006: Validator bookkeeping follows the selection
- Covers: Validator bookkeeping follows the selected validation workflow.
- Boundary:
  - the real `create-change.sh` (OpenSpec and spec-driven), `commit-change-plan.sh`, and the archive baseline step;
  - the spec-driven simple-change detect step;
  - a stub `agent-validator` that logs its invocations, or none on `PATH`.
- Setup: temporary repos with and without config, and with the selection empty, `auto`, `agent-validator`, `none`, or a custom path.
- Action: run each script or step.
- Assertions:
  - `detect` and `skip` run only for `agent-validator`, or for `auto` and empty with config present.
  - `none` with config and unvalidated changes proceeds without invoking the validator, and archive does not advance the baseline.
  - Creation succeeds with no executable installed under `auto` and no config.
  - A configured `auto` project with unvalidated changes fails creation with the existing message.
- Execution: `workflows` package script tests, CI `test` job.

### INT-007: Older helpers keep behavior apart from config gating
- Covers: Implement-change validator skipping (preserved versions); Verify-change workflow (v1.0 config gating); `implement-task-v1.0` and `complete-simple-change-v1.0` per task-compliance-activation; direct bookkeeping commands in preserved workflows.
- Boundary: `core/run-validator-v1.0.yaml` via `core/implement-task-v1.0.yaml` and the existing `verify_change_workflow_test.go` cases on `core/verify-change-v1.0.yaml`.
- Setup: the existing tests, plus a no-config variant.
- Action: run the existing scenarios with and without `.validator/config.yml`.
- Assertions:
  - With config, the validator commands and task-compliance flags are unchanged from today.
  - Without config, no `agent-validator` command runs, and steps record skipped or `SKIPPED` outcomes.
  - `validator-pr-gate.sh` passes on `SKIPPED`.
  - Without config, the direct `agent-validator detect` and `skip` commands are no-ops and need no executable on `PATH`. They appear in `spec-driven/simple-change-v1.0`, `spec-driven/plan-change-v1.0`, `openspec/plan-change-v1.0`, and `openspec/implement-change-v1.0`. With config, they run as today.
  - The Agent Factory call shape (`implement-task-v1.0` with no `task_compliance`) still activates task compliance.
- Execution: `internal/exec` and `workflows`, CI `test` job.

### INT-008: Selection binding, pre-validation, and absolute-path loading through the runner
- Covers: Validation workflow selection; Sub-workflow invocation (absolute paths); Skip rule for fresh runs.
- Boundary: `internal/runner` run entry with real pre-validation and the loader, on embedded `openspec:change-v3.0` and `openspec:simple-change-v2.0`, executed up to and including the first sub-workflow that resolves `{{validation_workflow}}`. A small builtin-shaped fixture parent stands in where the full chain would need agents.
- Setup: a temp project containing a valid custom validation workflow file and an invalid one.
- Action:
  - Start fresh runs with `validation_workflow` unset, `none`, `agent-validator`, `foo`, the existing but incompatible `builtin:core/verify-change-v1.0.yaml`, an absolute valid path, an absolute missing path, and an absolute invalid file.
  - Start `core:implement-change` directly with `validation_workflow=none`, as agent-evals does.
  - Resume a run that bound `none`.
- Assertions:
  - Bad values fail pre-validation with an error naming the target, and no step starts (no audit `step_start`).
  - The incompatible builtin target fails on its missing required params. Validation reaches it through the nested helpers `implement-change-v3.0` → `verify-change-v2.0`.
  - The direct `core:implement-change` invocation binds `none` to the built-in reference.
  - Unset binds the `auto` built-in reference.
  - The absolute path is loaded from disk under a builtin parent at both pre-validation and execution.
  - Builtin default targets are not re-validated: no load of their internals is recorded.
  - The resumed run keeps the bound selection.
- Execution: `internal/runner`, CI `test` job.

### INT-009: Optional validator setup paths
- Covers: Optional validator setup (scaffold and onboarding).
- Boundary: `openspec/scaffold-v1.0.yaml`, `spec-driven/scaffold-v1.0.yaml`, and `onboarding/validator-v1.0.yaml`, with stubbed UI outcomes, stubbed agents, and a stub `validator-init`.
- Setup: decline and accept choices for each. One accept case has a validator that fails once.
- Action: run the workflows.
- Assertions:
  - On decline, no `validator-init` or setup step runs, no config is created, and the guidance step is shown.
  - Scaffold completes, with its validation step passing as `SKIPPED` under `auto`.
  - Onboarding skips the stash, restore, and demonstration steps and continues.
  - On accept, the existing step sequence runs unchanged.
  - The scaffold's validation repair resumes the session ID that the scaffold step created (`implementor-agent`).
- Execution: `internal/exec` and `workflows`, CI `test` job.

### INT-010: Real Agent Validator through the new built-in
- Covers: Built-in validation workflows (`agent-validator` with a real Validator).
- Boundary: the real Validator build from CI, with deterministic local provider fakes (the existing `TestLocalValidatorDeliveryIntegration` harness), invoked through `validation-agent-validator-v1.0.yaml` instead of `run-validator-v1.0.yaml`.
- Setup: the existing scenarios, plus a repair run in a named session.
- Action: run the existing scenarios through the new built-in.
- Assertions: the same delivery, report, and metrics expectations as today, with the result file written.
- Execution: `internal/exec`, CI `test` job (it already builds agent-validator from main).

### INT-011: Unresolved validation warnings survive resume
- Covers: Unresolved validation warns and continues (the run's final status).
- Boundary: `internal/runner` resume with real `state.json` persistence, running a small builtin-shaped parent around the agent-validator built-in, with a stub validator that always fails.
- Setup: the run warns at its validation step and is then interrupted at the next step (a failing push stub).
- Action: resume the run, with the push stub now succeeding, to completion.
- Assertions:
  - `state.json` records the warning before the interruption.
  - After resume, `run_end` has `completed_with_warnings: true` and a warning count that includes the earlier attempt's validation warning.
  - A run without warnings still reports none after resume.
- Execution: `internal/runner`, CI `test` job.

## End-to-End Tests

### E2E-001: CLI rejects an unusable validation selection before any step
- Covers: Validation workflow selection (unresolvable selection); pre-validation exception.
- Surface: the `agent-runner` CLI, built from source in the test.
- Setup: a temporary git repo on a feature branch with no agent configuration needed.
- Journey: run `agent-runner run openspec:change change_name=x validation_workflow=foo`, then the same with a nonexistent absolute path.
- Assertions: a non-zero exit, an error naming the target, no run directory step activity, and no change directory created.
- Execution: the existing CLI test package under `cmd/agent-runner`, CI `test` job.

The full-lifecycle journeys need live agents. The acceptance pass covers them.

## Acceptance Testing Envelope

**Environments and sandboxes**
- Paul's Mac, using `./dev.sh` from this worktree.
- Disposable git repositories created under the run's `scratch_dir`, including small sample projects with and without `.validator/config.yml`.
- An isolated `HOME`, for onboarding and native setup runs only.

**Credentials and secrets**
- The locally installed and authenticated agent CLIs (Claude, Codex, and others in the user's profile).
- The locally installed `agent-validator`.
- `gh`, authenticated as the user.

**Authorized effects**
- Real paid agent and Validator calls, up to about $15 in total for the pass.
- Creating one private scratch GitHub repository under the user's account, named `agent-runner-acceptance-<date>`, plus draft and ready PRs in it, to exercise push and head checks. Do not delete it; list it in the handoff for the user to remove.
- Local branches and commits in scratch repos.

**Off limits**
- Any Codagent-AI repository's remote, branches, or PRs.
- This worktree's branch history, except normal workflow commits.
- The user's real `~/.agent-runner` settings and profiles. Onboarding uses the isolated `HOME`.
- The live Agent Factory install and its runs.
- The agent-evals data.

**Permitted substitutes**
- If GitHub writes fail, a local bare remote plus a stub `gh` for push and head-check behavior. Record the substitution.
- A PTY with `pyte` for the onboarding and scaffold UI choice, per CLAUDE.md. The run-launching parts that do not work under a synthetic PTY may use `./dev.sh run` directly.

**Known risk areas**
- Named-session merging across sibling helpers: `implementor-agent` resumed from `verify-change-v2.0` and `complete-simple-change-v2.0`.
- Resuming an in-flight `change-v3.0` run started before these in-place edits. Accepted as a pre-release limitation, but note the observed behavior.
- `auto` detecting config created mid-run in scaffold.
- The handoff showing commits made after the tested revision and unresolved validation.
- The CI-fix push ordering when the installed `fix-pr` skill is still the old version, which pushes by itself.
- The accepted narrow gap: no re-validation after the targeted retest at point 2.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| Validation workflow selection | INT-008 | E2E-001 | — |
| Built-in validation workflows | INT-001, INT-010 | — | — |
| Validation repairs run in an implementation-capable session | INT-001, INT-002, INT-003, INT-005 | — | — |
| Unresolved validation warns and continues | INT-001, INT-002, INT-003, INT-004, INT-011 | — | — |
| Validation points in change workflows | INT-002, INT-003, INT-004 | — | — |
| Validation points in simple-change workflows | INT-005 | — | — |
| Validation fixes reach the pull request | INT-002, INT-003, INT-004 | — | — |
| Tested and validated revisions tracked separately | INT-002, INT-003 | — | — |
| Validator bookkeeping follows the selected validation workflow | INT-006 | — | — |
| Optional validator setup | INT-009 | — | — |
| Sub-workflow invocation (absolute paths) | INT-008 | — | — |
| Workflow pre-validation skip rule (caller-supplied targets) | INT-008 | E2E-001 | — |
| Preserved helpers' config gating (builtin-workflows, task-compliance-activation) | INT-007 | — | — |
