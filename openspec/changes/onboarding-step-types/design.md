## Context

The current embedded onboarding flow is `onboarding:welcome` plus the `setup-agent-profile.yaml` sub-workflow. The welcome workflow writes `onboarding.completed_at` immediately after setup succeeds, so Phase 3 must insert a new sub-workflow before that existing completion write. Onboarding already runs through standard workflow execution, resume, audit, and run-state machinery; the new demo must preserve that shape.

Builtin workflows are embedded from `workflows/` through `workflows/embed.go`. For builtin runs, non-YAML assets in the namespace are materialized into `{{session_dir}}/bundled/<namespace>/` before execution. Script assets are executable; other assets are readable. This is enough to package onboarding documentation alongside workflow YAML without adding a separate documentation delivery system.

The behavioral contract is in `specs/onboarding-workflow/spec.md`: Phase 3 teaches UI, interactive agent, headless agent, shell, and capture; the interactive Q&A uses packaged docs; the final summary can launch one additional Q&A segment; and the demo is non-destructive apart from normal run artifacts.

## Goals / Non-Goals

**Goals:**
- Add `onboarding:step-types-demo` as a normal embedded onboarding workflow, directly invokable and usable as a sub-workflow from `onboarding:welcome`.
- Teach step primitives using existing workflow features only: `mode: ui`, agent steps, shell capture, interpolation, `skip_if`, and sub-workflow execution.
- Package the onboarding docs needed by the Q&A agent as builtin onboarding assets and reference them through `{{session_dir}}/bundled/onboarding/...`.
- Keep interruption, failure, resume, audit, and completion behavior on the existing runner paths.

**Non-Goals:**
- No new workflow step type, executor behavior, dispatcher state, or onboarding-specific resume path.
- No new external dependency or public CLI/config schema.
- No generalized docs registry. The docs packaging here is a narrow builtin asset convention for onboarding.

## Approach

Add a new `workflows/onboarding/step-types-demo.yaml` sub-workflow. Its structure should be mostly linear, with a one-shot Learn More branch from the final summary:

1. `intro-ui`: `mode: ui`, explains that the first demonstrated step is UI and that this workflow is made of multiple step types.
2. `explain-interactive`: `mode: ui`, frames the interactive agent segment.
3. `interactive-qa`: `agent: planner`, `mode: interactive`, prompt explains how to advance to the next step and invites light Agent Runner Q&A. The prompt points at packaged docs under `{{session_dir}}/bundled/onboarding/docs/`.
4. `explain-headless`: `mode: ui`, frames autonomous execution.
5. `headless-demo`: `agent: implementor`, `mode: headless`, prompt performs a harmless explanation task and does not write files.
6. `explain-shell`: `mode: ui`, frames deterministic commands and capture.
7. `shell-capture`: shell `command` emits a small deterministic string and `capture`s it.
8. `summary`: `mode: ui`, displays `{{shell_capture}}` and offers `continue` / `learn_more`, with `outcome_capture`.
9. `learn-more-qa`: interactive `planner` step skipped unless summary outcome is `learn_more`; when it completes, the demo completes successfully.

Do not use workflow loops for this branch. Existing counted loops are retry-oriented and fail on exhaustion, which is a poor fit for onboarding navigation. The final Learn More action is intentionally one-shot: users get an extra Q&A session, then the workflow completes.

Update `workflows/onboarding/welcome.yaml` by inserting a `step-types-demo` sub-workflow step after `setup` and before `set-completed`, gated by the same `user_action == continue` condition. Leave `set-completed` as the final success-path write so completion remains tied to the selected path reaching the end.

Package docs by adding a small curated doc asset set under `workflows/onboarding/docs/`, for example a concise `agent-runner-basics.md` derived from the user guide and onboarding design notes. Do not point the agent at repo-root `docs/`, because those files are not currently embedded. The Q&A prompt should reference the bundled path explicitly.

## Decisions

- **Use YAML-only workflow composition.** Prefer a new builtin YAML sub-workflow plus existing primitives over new Go orchestration code. Alternative: special-case Phase 3 in the dispatcher or runner. Rejected because onboarding already requires no bespoke runtime path, and YAML keeps the demo inspectable and testable.
- **Use existing builtin asset materialization for docs.** Put onboarding docs under `workflows/onboarding/docs/` so they are embedded and materialized with the namespace. Alternative: embed repo-root `docs/` separately. Rejected because that creates a broader packaging surface and a new access API that this change does not need.
- **Use `planner` and `implementor` profiles.** These are created by setup and match the intended interactive/headless roles. Direct `onboarding:step-types-demo` runs before setup can fail through normal profile validation. Alternative: use `interactive_base` / `headless_base` or add fallback demo profiles. Rejected to keep the demo aligned with the user-facing role profiles and avoid hidden config.
- **Use shell capture for data flow.** The shell step emits deterministic text and the summary interpolates it. Alternative: capture headless agent output. Rejected because agent output shape is less deterministic and makes the demo harder to test.
- **Make Learn More one-shot.** Implement the final Q&A action with a simple skipped interactive step rather than a loop. Alternative: use a bounded counted loop to return to the summary repeatedly. Rejected because counted loops fail on exhaustion and are designed for retry semantics, not UI navigation.

## Risks / Trade-offs

- **Packaged docs drift from repo docs** -> Keep the onboarding doc asset short, purpose-built, and covered by embed tests that assert it is present and non-empty.
- **Direct demo invocation before setup fails** -> This is acceptable normal profile validation. Make the failure clear through existing config/profile errors; do not add fallback config.
- **Learn More is not repeatable from the final summary** -> Keep the main interactive Q&A segment earlier in the demo and make the final Learn More action an extra chance to ask questions, not an ongoing help mode.
- **Interactive agent may not complete automatically** -> The prompt must explain both user-driven continuation and agent-driven completion; the existing interactive completion instruction remains appended by the runner.
- **Non-destructive guarantee can regress through prompts** -> Keep prompts explicit that agents must not write files, mutate config, run external services, or change project state; add tests that inspect the workflow YAML for absence of file-writing shell commands in the demo.

## Migration Plan

1. Add `workflows/onboarding/step-types-demo.yaml` and curated docs under `workflows/onboarding/docs/`.
2. Update `workflows/onboarding/welcome.yaml` to run `step-types-demo.yaml` after setup and before `set-completed`.
3. Extend builtin workflow tests to assert `onboarding:step-types-demo` resolves, onboarding docs are listed as assets, and the new workflow validates as part of the embedded set.
4. Add focused workflow/YAML tests for welcome step order, final completion placement, final summary branch behavior, shell capture interpolation, and non-destructive command shape.
5. Run `openspec validate --type change onboarding-step-types`, targeted Go tests for `workflows`, `internal/loader`, and onboarding sub-workflow behavior, then broader `go test ./...`.

Rollback is straightforward before release: remove the new sub-workflow/docs assets and remove the inserted `step-types-demo` step from `welcome.yaml`. No persisted settings or state migration is required.

## Open Questions

None for implementation. The remaining copy choices for UI bodies and prompts are implementation details, constrained by the behavior in the spec and this design.
