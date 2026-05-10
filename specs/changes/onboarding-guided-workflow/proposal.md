## Why

Onboarding currently ends after the step-types demo, so a new user finishes the flow without having actually used Agent Runner to do real work. That breaks the core onboarding pitch — "use Agent Runner to teach Agent Runner" — because the user only sees primitives in isolation, never a real workflow on a real task in their own project. Phase 3 closes that gap by walking the user through a guided real-task workflow (plan → tutor Q&A → implement) using the same primitives they just saw demoed.

## What Changes

- Add a new `guided-workflow` sub-workflow under `workflows/onboarding/` that runs after the step-types demo on first-run onboarding.
- Update the top-level `onboarding/onboarding.yaml` so the guided-workflow runs after `step-types-demo` and before the completion-marker step.
- The new sub-workflow chains:
  - An intro UI screen describing what this phase will do.
  - A directory-confirmation UI screen showing the current working directory, captured from `pwd`, with a Continue action. The user can press Esc (TUI cancel) to abort the workflow if the directory is wrong.
  - A soft git-cleanliness guard: a shell step captures `git status --porcelain`, and a UI warning screen renders only when the working tree is dirty (or the directory is not a git repo). The warning offers Continue; the user can press Esc to abort.
  - An interactive **planning** step using the `planner` agent that elicits a small real task from the user and produces a brief plan.
  - A headless capture step (`session: resume` to continue the planner session) that emits a one-line `plan_summary` for handoff. Functionally analogous to `locate-proposal` in `plan-change.yaml`, which uses a named session rather than resume.
  - An interactive **tutorial** step that runs in a *separate session* with a tutor-oriented prompt, fielding questions about what just happened and what is next. Reuses the existing `planner` agent profile — the tutor is a separate *session*, not a new agent profile.
  - A headless **implementation** step using the `implementor` agent that performs the change end-to-end, receiving `{{plan_summary}}` via interpolation.
  - A summary UI screen pointing the user at `git diff` to inspect the change and commit it themselves.
- Use the existing skill set: `mode: ui`, interactive agent step, headless agent step, shell+capture, `skip_if`, sub-workflow composition. No new step primitive, no new runtime path, no new agent profile.
- The headless implementor **modifies the user's real repo** — that is the point of "learn by doing." The summary screen makes this explicit and instructs the user to review with `git diff` and commit themselves.

## Capabilities

### New Capabilities

- `onboarding-guided-workflow`: The Phase 3 guided-real-workflow sub-workflow. Owns the screen sequence, soft guards (directory confirmation, git cleanliness), planning + tutor + implementation step composition, and handoff between sessions.

### Modified Capabilities

- `onboarding-workflow`: The top-level `onboarding/onboarding.yaml` now runs `guided-workflow` after `step-types-demo` and before recording `onboarding.completed_at`. Onboarding completion now requires the guided-workflow to complete (or abort via the TUI cancel gesture).

## Technical Approach

The change is shaped almost entirely as workflow YAML composition over primitives that already exist. The runtime, executor, model, and CLI layers are not touched.

```
┌──────────────────────────────────────────────────────────────────┐
│             workflows/onboarding/onboarding.yaml                  │
│   - step-types-demo            (existing)                         │
│   - guided-workflow            NEW — Phase 3                      │
│   - set-completed              (existing)                         │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│         guided-workflow.yaml  (new sub-workflow, Phase 3)        │
│                                                                  │
│   intro-ui          mode: ui          informational               │
│   capture-cwd       command: pwd      capture → cwd               │
│   confirm-cwd       mode: ui          shows {{cwd}}, [Continue]    │
│   check-clean       command: git status --porcelain               │
│                                       capture → git_status        │
│   warn-dirty        mode: ui          skip_if git_status empty    │
│                                       [Continue anyway]           │
│                                                                  │
│   explain-plan      mode: ui          informational               │
│   plan              agent: planner    interactive                 │
│                     session: new                                  │
│   capture-plan      session: resume   headless, capture → plan_summary │
│                                                                  │
│   explain-tutor     mode: ui          informational               │
│   tutor             agent: planner    interactive                 │
│                     session: new      (separate session)          │
│                                                                  │
│   explain-impl      mode: ui          informational               │
│   implement         agent: implementor headless                   │
│                     session: new      prompt uses {{plan_summary}}│
│                                                                  │
│   summary           mode: ui          "run `git diff`, commit"    │
└──────────────────────────────────────────────────────────────────┘
```

**Shape-level decisions:**

- **Tutor is a session boundary, not a new agent profile.** The spec emphasizes "tutorial agent ≠ execution agent," and the user-confirmed model is a *sequential* step between planning and implementation. We satisfy that by using `session: new` on the tutor step with a tutor-oriented prompt, reusing the existing `planner` agent profile. No agent-profile schema change.
- **Plan → implement handoff via captured one-liner.** After the interactive planner step (`session: new`), a `session: resume`, `mode: headless` step continues that same session and emits a single-line summary for capture. This is functionally analogous to `locate-proposal` in `plan-change.yaml` (which uses a named session rather than resume). The implementor reads the summary via `{{plan_summary}}`. This avoids touching session inheritance semantics or introducing artifact files.
- **Soft guards via capture + `skip_if`.** Directory and git-clean checks are shell steps that capture output; the warning UI step uses `skip_if` so it only renders when the tree is dirty. Abort at any UI step is via the TUI cancel gesture (Esc), which triggers `OutcomeAborted` and halts the workflow — no special "Stop" action semantics are needed.
- **No new builtin variables.** `cwd` is captured from `pwd` rather than added as a runner-provided builtin, keeping the change scope-bounded to workflows.
- **Phase 4 owns validation.** The summary step points at `git diff` and asks the user to commit; no validator integration. This preserves the spec's progressive-disclosure principle (delay validator until later).

**Risks:**

- *Headless implementor modifies the user's real repo.* This is by design — onboarding's whole point is real work — but it is the highest-impact action in onboarding so far. Mitigation: prompt the implementor with bounded instructions (e.g., "keep the change small and limited to the task"), and make the summary screen explicit about reviewing the diff before committing.
- *Free-form task elicitation can drift.* The planner step has no hard stop other than the agent deciding it has enough to proceed. Mitigation: a tight prompt with concrete small-task examples (typo fix, log line, rename).
- *No project = no useful run.* In an empty directory the implementor has nothing to do. Soft guard surfaces this; the user can still choose to proceed.

## Out of Scope

- **Tutorial agent as a persistent companion session.** This change uses a sequential tutor step only. A parallel/toggle-able session-companion model would require new TUI affordance and runtime support and is explicitly deferred.
- **A dedicated `tutor` agent profile.** The tutor is realized as a new session, not a new agent profile. Adding a tutor profile (different model, different system prompt, etc.) is deferred.
- **Hard project gating / refusing to run in non-project dirs.** Soft warn + allow override is the chosen behavior; a strict gate is out of scope.
- **Validator integration.** Phase 4 introduces Agent Validator. Phase 3 does not run any checks against the implementor's output beyond pointing the user at `git diff`.
- **Auto-commit / auto-push of the implementor's change.** The user reviews and commits themselves.
- **Free-text input via `mode: ui`.** Task elicitation happens inside the agent conversation, not via a UI text-input control. `mode: ui` text input remains deferred.
- **Failure / partial-completion recovery beyond existing resume semantics.** The workflow relies on the standard workflow state and resume behavior. Phase-3-specific recovery flows (e.g., "retry implementor on previous plan") are deferred.
- **Listview / main-menu entry for Phase 3.** Re-entry continues to use the existing `agent-runner run onboarding:onboarding` invocation.
- **New builtin interpolation variables.** `cwd` is captured from `pwd`; no runner-provided builtin is added.

## Impact

- `workflows/onboarding/guided-workflow.yaml` — **new** sub-workflow file.
- `workflows/onboarding/onboarding.yaml` — add `guided-workflow` step before `set-completed`.
- `workflows/embed_test.go` and any onboarding workflow tests — extend to cover that the new sub-workflow is embedded and resolves under the `onboarding` namespace, and that the top-level workflow chain includes it in the correct order.
- No changes to `internal/model/`, `internal/exec/`, `internal/runner/`, `internal/cli/`, or `cmd/agent-runner/`.
- No changes to `internal/usersettings/` — onboarding completion tracking already exists from prior phases.
- No new external dependencies.
- Docs: a short pointer in `docs/agent_runner_onboarding_workflow_spec.md` may be useful (note Phase 3 is implemented, including the "Step 4.x" numbering correction). Not a hard requirement of this change.
