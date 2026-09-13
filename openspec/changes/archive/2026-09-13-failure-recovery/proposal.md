## Why

Built-in workflows guard agent-driven and open-world mutations with deterministic checks, which is the right contract: a script, not an agent, decides whether the workflow may continue. The weakness is what happens when the check fails. Today a failed check is terminal control flow. The run aborts with the check's one-line stderr, the diagnosis the preceding agent produced is discarded, and `--resume` re-enters at the check, which fails again because the mutation it guards never happened.

GitHub issue 65 documents two runs with this shape:

- `change-2026-09-01T16-54-43-258275Z`: the OpenSpec archive commit was rejected by repository-inherited commit hooks (subject format and length). OpenSpec had already moved the change directory, so the first resume passed a now-missing path to `git add`. The commit convention is an open-world property of the target repository that a shell step cannot interpret.
- `change-2026-09-11T03-21-57-838353Z`: `open-draft-pr` could not push because the `gh` token lacked `workflow` scope. The agent diagnosed the exact cause and two remedies, declined to signal completion, and the step still recorded `success`. `verify-draft-pr` then failed the run with "found 0 pull requests". Nothing an agent could do would fix it, and after the user granted the scope, resume would land on the pure check rather than the push.

A third recurring case is a check that fails for a reason a human must fix first (a defect, a model that needs switching): after the fix, the only way to re-run the guarded step is to hand-edit run state to rewind to it. A fourth, from run `change` on 2026-09-11 against `agent-factory.fix-bugs`: `verify-task-commit` failed with "did not produce a commit" because the task was delivered in a sibling repository; the implementor's response four steps earlier said exactly that, and the run could only report the one-line stderr.

In every case the failure was correct, agent-actionable or human-actionable feedback that the Runner could only express as an opaque abort. Repair loops exist (`core:run-validator`, `core:implement-task`'s leftover-commit sequence) but must be assembled by hand from loops, captures, `previous_success`, and `continue_on_failure`, and none of them survives resume well.

## What Changes

- **Recoverable checks.** A shell or script step may declare a `repair` block. When the check fails, the Runner runs the repair, reruns the same check, and only the check's result decides whether the workflow continues. Two repair forms:
  - an inline repair agent (session or agent plus prompt) for mechanical or repository-sensitive fixes such as commits, hook responses, and incomplete artifacts;
  - `rerun` of an earlier step in the same sequential scope, re-executing forward through the check, for cases where the correct repair is to redo the mutation (draft PR push).
- **Bounded, resumable attempts.** Default one repair attempt; `max` is configurable. The Runner persists a repair frame (owning check, form, target, phase, attempt count) so an interruption anywhere in the cycle resumes inside it. When the budget is exhausted the run fails as today, but `--resume` re-enters the repair cycle at the rewind target (or the check, for inline repair) with a fresh budget, on the premise that a human changed something.
- **Failure evidence carried forward.** On a failed check the Runner records a failure reason built from the check's stdout and stderr and the final response of the agent execution it guards: the most recent agent step in the same scope, identified by execution identity (step prefix and attempt), however many shell, script, sub-workflow, or skipped steps sit between them. Child agent reports made through `call_agent` are included when the guarded step recorded them. The reason is shown in the run view, run list, and resume hint, and is what the repair agent receives, explicitly marked as untrusted input.
- **Blocked declaration.** A reserved terminal marker in an agent's final response declares that the failure cannot be repaired by an agent (credentials, permissions, decisions only a human can make). Both the guarded action's agent and a repair agent may emit it. The first failed check inspects the guarded action's response before scheduling any repair, so the motivating credential failure pauses without replaying the push. The marker only stops repair; it can never mark the check passed.
- **Repair agents are named, not implicit.** An inline repair block must name an existing `session` or `agent`; there is no built-in repair role. The migrated archive commit uses `implementor`. A repair agent never declares its own success; the check does.
- **Migrate built-in checks.** Sites that verify an agent's mechanical output or perform a repository-sensitive commit gain `repair`:
  - OpenSpec archive (three callers) split into an idempotent transition, a deterministic commit verification with inline repair by `implementor`, and a separate idempotent Validator baseline advance;
  - `core:plan-change` `check-plan` and `commit-plan`, inline repair by the planning session; `commit-change-plan.sh` becomes replay-safe so it succeeds when the plan is already committed;
  - `core:implement-task` `verify-task-commit`, inline repair on the implementor's session (`session: resume`), which commits the deliverable when it belongs in this repository and otherwise explains where the work went and declares blocked;
  - `core:implement-change` `verify-task-index`, `verify-assumptions-handoff`, and `verify-clean-for-pr`, inline repair by the lead session; `verify-draft-pr`, rerun of `open-draft-pr`, whose prompt gains the blocked-declaration instruction;
  - `openspec:simple-change` `validate-openspec`, inline repair by the lead session.
  The ticket-derived commit subject in the shell scripts is kept as a compatibility fallback, not extended. Preconditions (`validate-feature-branch`, clean-tree guards, input validation), the `check-definition` loop, `run-validator`, and `verify-acceptance-handoff` keep plain failure.

No breaking changes. Existing `continue_on_failure`, `warn_on_failure`, `skip_if`, and `break_if` keep their semantics; `repair` composes with them under the precedence rules below.

## Capabilities

### New Capabilities
- `step-repair`: the `repair` block on shell and script steps: inline-agent and rerun forms, attempt budget, evidence injection, check reruns as the sole success authority, blocked declaration, repair-frame persistence, and resume re-entry.
- `run-failure-evidence`: structured failure reason and evidence for failed checks, including the guarded agent execution's final response, persisted with the run and surfaced in run views and the resume hint.

### Modified Capabilities
- `step-flow-control`: precedence between `repair` and existing controls. Internal repair attempts do not update `previous_success`, do not create warnings, and do not trigger the check's own `break_if`, `continue_on_failure`, or `warn_on_failure`; only the check's terminal result does. Replay ranges containing `break_if` or outcome-relative `skip_if` are rejected at load time in this change. An intermediate blocking failure or abort inside a replay range counts as a failed repair attempt owned by the check.
- `recursive-state`: persisted repair frame (owning check, scope path and loop iteration, target, phase, attempt count) across nested scopes; resume restores the frame before ordinary step resolution and re-enters at the rewind target.
- `builtin-workflows`: archive flow split and the migrated checks listed above. Workflow YAML and script edits are captured in design and tasks, not as spec deltas.
- `view-run`, `list-runs`, `live-run-view`: failure surfaces display the classified reason and evidence; live view renders repair attempts beneath the owning check.
- `audit-log-entries`: repair attempt start/end and blocked-declaration events.

## Technical Approach

- **Verifier stays authoritative.** `repair` is a field on the check step, not a new step type. The check step's own outcome is the step outcome; repair activity is recorded as attempts beneath it, the way loop iterations are today. Whatever repairs, the same script runs again.
- **Replay is a rewind signal handled by each sequencer.** Sibling sequencing lives in the top-level runner, the loop executor, the sub-workflow executor, and the group executor, not in the shell or script executor. The check executor owns the cycle and, for the rerun form, sets a pending rewind that the enclosing sequencer consumes by moving its index back to the target: run check; on non-zero exit, consult the guarded execution's response for a blocked marker; otherwise run an inline agent step synthesized from the repair block (reusing the agent executor, session resolution, and named sessions) or rewind the scope's step index to the `rerun` target and continue forward; rerun the check; stop on success, on budget exhaustion, or on a blocked declaration. `rerun` targets must be earlier steps in the same sequential scope and are validated at load time.
- **Evidence.** The failure reason is assembled by the Runner from the check's captured output and the guarded execution's final response already captured in audit, keyed by execution identity so nesting and child calls are unambiguous. It is written into run state and passed to the repair prompt as built-in variables under an untrusted-input wrapper, matching the pattern `run-validator` already uses.
- **Blocked declaration.** One exact reserved marker parsed only from the adapter-normalized final response, consistent with the `CI_*` marker convention in `finalize-pr`, rather than a new control-channel request. When a replay range contains several agents, only the `rerun` target's response and the inline repair agent's response are consulted.
- **State and resume.** `NestedStepState` gains a repair frame. `ResolveResumeStep` restores an open frame first and returns the rewind target (or the check) rather than the recorded sibling; budgets reset on human resume.
- **Archive split.** The transition step captures and persists the exact archive path, starting `HEAD`, and staged baseline. Verification asserts: active path absent; exactly that archive present; expected archive and canonical-spec delta committed; no unrelated path committed; prior staged state preserved; `HEAD` advanced when a commit was required. Validator baseline advancement runs after verification as its own idempotent step.
- **Failure policy is workflow-authored.** Not every failure should trigger an agent. Precondition guards (dirty worktree), invalid input, and internal invariants keep plain failure. The construct is opt-in per check, and the migrated built-ins follow the classification from issue 65: agent-incomplete output → repair; repository-specific rejection → inline agent repair; external credential precondition → blocked; transient failure → out of scope here.
- **Risk.** Rewind re-executes steps that may have side effects. Mitigation: `rerun` is explicit and load-validated, migrated targets are replay-safe (archive transition is idempotent; PR open is query-then-act), and the check bounds the loop. The archive split moves mutation boundaries and is covered by real-Git tests per the issue's acceptance criteria.

## Out of Scope

- Deterministic retry or backoff for transient network and tool failures.
- A control-channel "decline completion" signal for autonomous agent steps. The blocked marker plus carried evidence covers the observed harm; agent prose remains data, not control.
- A distinct `paused` run status. Failed runs are already resumable; this change makes the failure reason and evidence useful instead of adding a status.
- Replay ranges containing `break_if` or outcome-relative `skip_if`; rerun targets in a different scope.
- Rewriting `core:run-validator` on the construct. Its hand-built loop works and is spec-covered; migration is a later cleanup.
- Auditing and migrating every remaining built-in shell and script step. That follow-up uses this construct once it exists.
- Generalizing commit-message policy in shell. The agent-driven archive commit replaces that direction.
- A built-in repair or operator agent role. Workflows name an existing agent or session; users define cheaper repair profiles in config if they want them.
- Changes to Agent Validator or the OpenSpec CLI.

## Impact

- `internal/model/`: `Step.Repair`, validation, repair frame in `NestedStepState`, `ResolveResumeStep`.
- `internal/exec/` and `internal/runner/`: shared scope controller for the repair cycle in the runner, loop, and sub-workflow sequencers; evidence assembly; failure reason persistence and resume hint.
- `internal/runview/`, `internal/listview/`, `internal/liverun/`: failure surfaces and attempt rendering.
- `internal/audit/`: repair attempt and blocked events.
- `workflows/core/`, `workflows/openspec/`, `workflows/spec-driven/`: migrated checks and archive split.
- `openspec/specs/`: new and modified capabilities above; `docs/` workflow authoring guidance for the failure policy.
- Users: failed runs explain themselves and resume from the right place; workflow authors get one construct instead of hand-built loops.
