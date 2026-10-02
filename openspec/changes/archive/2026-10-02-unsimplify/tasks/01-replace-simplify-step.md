# Task: Replace the verify-change simplify step with a diff-scoped implementor pass

## Goal

Replace the `simplify` step in `core:verify-change` so it no longer runs Claude Code's built-in `/simplify` (four reviewer subagents on the lead profile) in the `lead-agent` session. Instead it runs a Codagent-owned, single-pass, diff-scoped, behavior-preserving maintainability prompt in a fresh `implementor` session. Factory-run analysis showed the old step cost about $3.70 per run, and 29% of runs introduced a defect, all from its efficiency and simplification reviewers. The new pass keeps only the valuable part: duplication and wrong-layer logic that static tools cannot see.

## Background

Files:

- `workflows/core/verify-change-v1.0.yaml`: the `simplify` step (currently around line 62) sits between `verify-assumptions-handoff` and `run-validator`. It currently has `session: lead-agent`, `mode: autonomous`, and a prompt that invokes `/simplify`.
- `workflows/verify_change_test.go`: `TestCoreVerifyChangeSimplifyFixesDefects` asserts the current prompt phrases and also asserts `open-draft-pr` prompt phrases.
- Reference pattern: `core:implement-task`'s `generate-code` step already uses `agent: implementor` with `session: new`, so no Runner (Go) code change is needed or allowed.

Decisions (from the approved design):

- Replace the step with exactly the following. The prompt text is user-approved and MUST be used verbatim:

```yaml
  - id: simplify
    agent: implementor
    session: new
    mode: autonomous
    prompt: |
      Do a maintainability pass over {{change_label}} "{{change_name}}" (artifacts in `{{change_dir}}/`), covering the branch's changes since its merge base with the default branch. Review it yourself in one pass; do not use a /simplify skill or reviewer subagents.

      Focus on what static analysis tools cannot catch within this change's diff:
      - Duplication: logic repeated across the change with edits, or different code doing the same job.
      - Altitude: logic at the wrong layer or abstraction level.

      Cleanup must preserve behavior exactly and must not grow the code. When unsure whether a change preserves behavior, leave it.

      If you happen to find a clear-cut defect in this change's own code, fix it, with a regression test where applicable. Record anything else, including defects in code this change did not touch and anything needing a product, scope, or design decision, in `{{session_dir}}/output/acceptance-assumptions.md`, replacing any `No unresolved assumptions or context gaps.` statement.

      Commit each cleanup and each defect fix separately, following the project's commit message conventions and prepending [{{step_id}}]. Do not run Agent Validator or push.
```

- The step keeps its id `simplify`, its position immediately before `run-validator`, and the `[simplify]` commit prefix (via `[{{step_id}}]`).
- The workflow's `sessions:` block is unchanged; `lead-agent` still serves the other lead steps.
- Do NOT change the legacy `workflows/openspec/implement-change-v1.0.yaml` or `workflows/spec-driven/implement-change-v1.0.yaml` `simplify` steps or their `require-simplify-decisions-resolved` gate. Leave `TestLegacyImplementChangeSimplifyStopsForDecisions` and the verify-change step-order test unchanged.
- Update the live spec `openspec/specs/builtin-workflows/spec.md` only through the normal archive flow; do not hand-edit it in this task. The change's spec delta is `openspec/changes/unsimplify/specs/builtin-workflows/spec.md`.

Test changes in `workflows/verify_change_test.go` (write the test first, see it fail, then change the YAML):

- Rename `TestCoreVerifyChangeSimplifyFixesDefects` to describe the new contract (for example `TestCoreVerifyChangeSimplifyPass`).
- Assert `step.Agent == "implementor"`, `step.Session == "new"`, `step.Mode == "autonomous"`.
- Assert the prompt contains: `do not use a /simplify skill or reviewer subagents`, `{{session_dir}}/output/acceptance-assumptions.md`, `No unresolved assumptions or context gaps.`, `[{{step_id}}]`, and `Do not run Agent Validator or push`.
- Keep the existing `open-draft-pr` assertions unchanged.

## Spec

From `openspec/changes/unsimplify/specs/builtin-workflows/spec.md`, Requirement: Verify-change workflow (this task's portion):

> The simplification step SHALL run in a new session of the `implementor` agent profile rather than the `lead-agent` session, SHALL review only the change's own diff, and SHALL NOT run Agent Validator or push. It SHALL commit behavior-preserving cleanups and any defect fixes separately with the `[simplify]` prefix, and SHALL record unfixed defects and findings needing a product, scope, or design decision in `acceptance-assumptions.md`, replacing the `No unresolved assumptions or context gaps.` statement when it adds the first entry.

> No step prompt SHALL run Agent Validator, directly or through a skill; validation SHALL run only as the `core:run-validator` workflow in its own step.

#### Scenario: Simplification runs in a fresh implementor session
- **WHEN** `core:verify-change` reaches its simplification step
- **THEN** the step starts a new session with the user's `implementor` profile, and the `lead-agent` session's history does not include it

#### Scenario: Simplification records an out-of-scope defect
- **WHEN** the simplification pass finds a defect in code the change did not touch
- **THEN** it leaves that code unchanged and `acceptance-assumptions.md` lists the defect in place of the no-unresolved-assumptions statement

The second scenario depends on real agent behavior; it is covered by the prompt-contract assertions here and exercised in the acceptance pass, not by an automated agent run.

## Test Plan

`openspec/changes/unsimplify/test-plan.md` assigns no `INT-*` or `E2E-*` obligations. The step contract is covered by the rewritten unit test in `workflows/verify_change_test.go`.

## Done When

- `workflows/core/verify-change-v1.0.yaml`'s `simplify` step matches the YAML above exactly (verbatim prompt, `agent: implementor`, `session: new`, `mode: autonomous`), still immediately before `run-validator`.
- The renamed test asserts the new contract and passes; `go test ./workflows/...` passes, including the unchanged legacy and step-order tests.
- No Go code outside tests changed; legacy v1 workflows are untouched.
- `make test` and `make lint` pass.
