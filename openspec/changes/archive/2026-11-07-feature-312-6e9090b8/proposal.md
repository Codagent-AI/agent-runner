## Why

In `core/implement-change-v2.0.yaml`, the autonomous `implement` turn is trusted to end only when the change is done. Nothing checks that. The agent CLI exits 0 whatever the agent decided, and the only gate before the audit is `check-evidence` with its `complete-evidence` rerun repair. That repair is capped at tier 1 tests within 15 minutes, so it can tidy a checklist but cannot finish a half-built change.

Run `change-2026-10-27T02-40-12-943756Z` showed the failure. The Claude Sonnet implementor auto-compacted at about 324K tokens and then ended its turn on purpose ("The change is not finished, and I'm stopping here for this turn"), with about two thirds of the change unbuilt. `implement` succeeded and no blocked record was written. `complete-evidence` verified 20 of 75 rows and declared `REPAIR_BLOCKED`. The run's `failure_reason` then quoted `check-evidence`'s stderr from *before* the repair, which was no longer accurate. The manual fix was to resume the same implementor session and tell it that it was not finished, that a compaction is not a stopping point, and that it should end only after writing a completion record. It finished in one pass of about 1h45m, through two more compactions, and verified 74 of 75 rows. The one remaining row legitimately needs the owner's ratings at acceptance.

This affects every Change v3 run (`openspec:change` and `spec-driven:change`). Long changes routinely run past one context window, and an early stop now either wastes the run or reaches the audit with unbuilt work. The issue has already decided the design. This proposal bounds it.

**Verdict: go.** The fix that worked by hand turns into a deterministic, CLI-agnostic loop built almost entirely from primitives the Runner already has: loops, `break_if`, `skip_if`, script checks, named sessions, and step-level `agent:`. The only Go change is a small, contained correction to how failure reasons are worded. The alternatives are weaker. A Claude Stop hook works for one CLI only. A bigger `complete-evidence` budget still re-prompts blindly and cannot tell "blocked" from "unfinished". Doing nothing keeps losing long runs.

## What Changes

- **Completion record.** The `implement` prompt tells the implementor to end only after writing `{{session_dir}}/output/implementation-complete.md` (once it believes the change is done) or the existing `implementation-blocked.md` (when it is blocked). It also states that a compaction is not a stopping point.
- **Deterministic completion check.** A new bundled script passes only when all of these hold: `implementation-complete.md` exists, `check-evidence.sh` passes against the planned checklist, at least one commit exists since `implementation_start_head`, and every checklist row is `verified`. Anything else, including any `not verified` row, goes to the judge. An existing `implementation-blocked.md` still stops the workflow before the judge runs.
- **Independent judge.** When the check fails, a fresh session with the `tester` profile judges why the implementor stopped. It is inspection only: no tests, builds, or product runs. It reads the check output, the checklist and evidence directory, the git log and diff since the implementation started, the decision records, and the implementor's final response (treated as untrusted). It returns exactly one verdict, in this order of precedence:
  - `blocked`: a concrete cause only a human can fix. The judge writes the blocked record and the workflow stops.
  - `continue`: work remains. The judge writes a concrete list of what is left. The same implementor session is re-prompted to keep working from what is on disk.
  - `fix-record`: the work is done but the record is wrong. The judge fixes record keeping only, using evidence that already exists: the checklist's Code, Evidence, and Status cells, and a missing or malformed `implementation-complete.md`. The check then runs again.
  - `complete`: the only unmet condition is rows that cannot be verified before acceptance, each with its reason. The run proceeds to the audit.

  The judge leans toward `continue` over `blocked`.
- **Bounds.** At most **3** `continue` passes. After every `continue` pass, including the last, the blocked check and the completion check run again before any stall or budget outcome. A pass that satisfies the completion check, for example by only writing a missing completion record, therefore passes. A pass that leaves the check failing, adds no commit, and changes no checklist row ends the loop early with "implementation stalled". When the budget runs out, the run fails with "implementation incomplete" and the judge's list of what is left.
- **Replaces `complete-evidence`.** The loop replaces `complete-evidence` and the `check-evidence` rerun repair that runs before the audit. `complete-evidence-after-review` and `recheck-evidence` after the audit do not change.
- **Honest failure reason.** When a check's repair cycle ends (blocked or exhausted) and the check's last recorded output predates the last repair attempt, the classified `failure_reason` marks that output as `before repair` and no longer presents it as the current state. This is a wording change to a free-text field. Field names and formats stay the same.

## Capabilities

### New Capabilities
- `implementation-completion-loop`: the completion record, the deterministic completion check, the judge and its four verdicts, the continue-pass budget, stall detection, and the resulting run outcomes in the audited implementation workflow.

### Modified Capabilities
- `builtin-workflows`: `core/implement-change-v2.0.yaml` runs the completion loop between `implement` and `audit` in place of `complete-evidence` and the pre-audit `check-evidence` repair, and declares the judge's fresh `tester` session.
- `run-failure-evidence`: the classified failure reason labels a check's output as `before repair` when that output predates the repair attempt that ended the cycle.

## Technical Approach

**Workflow composition, not a new step type.** The loop is an ordinary counted `loop` in `implement-change-v2.0.yaml`. Each iteration runs the blocked check, then the completion check (`break_if: success`), then the judge, then steps for each verdict, gated by `skip_if: 'sh: …'` on the judge's verdict file. The pieces:

- The completion check is a new bundled script, `workflows/core/check-implementation-complete.sh`. It reuses `check-evidence.sh` instead of copying its rules, and writes a problems file the judge reads.
- The judge is a step with `agent: tester` and `session: new`, so every judgment is fresh. It must not reuse the declared `acceptance-tester` session, because that would compromise the acceptance tester's independence later in `verify-change`. Its verdict goes to a file under `{{session_dir}}/output/`. A small script validates the file's shape, and a malformed verdict is treated as a judge defect, not as a pass.
- The `continue` pass resumes `implementor-agent` with the judge's list wrapped as untrusted data. The prompt again says that a compaction is not a stopping point and that the turn ends only with a completion or blocked record.
- The stall check compares HEAD and the checklist with a snapshot taken before the `continue` pass. It runs only after the next blocked check and completion check have run and the completion check has failed, so a pass that completes the change is never called stalled. It fails with `implementation stalled` when neither HEAD nor the checklist changed.
- `fix-record` runs inside the judge's own turn and is limited to record keeping: the checklist's Code, Evidence, and Status cells, and creating or repairing `implementation-complete.md` when the commits, checklist, and saved evidence already show the work is done. It writes no product code, runs nothing, and creates no evidence. The next loop iteration's completion check then decides. A `fix-record` verdict does not use up a `continue` pass. Consecutive `fix-record` verdicts with no passing check are capped at 2, so the loop always terminates.

The implementor's final response reaches the judge through `capture` on the `implement` and `continue` steps. Because the loop is plain workflow composition, runs that start on the new definition resume, audit, and show in the run view as they do today.

**Rollout: edit in place, and accept that some in-flight runs cannot resume.** `core/implement-change-v2.0.yaml` is edited in place, which the project's pre-release policy allows and recent changes to this file have done. The cost is that a run recorded against the old step layout can fail to resume. In particular, a run whose `check-evidence` repair failed with rerun target `complete-evidence` now fails resume with `step "complete-evidence" not found`, because `ResolveResumeStep` cannot find the removed target. A run recorded inside the steps the loop replaces also resumes in a different scope. The changed-file warning is not a migration, and none is provided. Guidance for affected runs: restart the change workflow, or finish the run with the binary it started on. Runs recorded before `implement` finished (including one interrupted during `implement`), and runs recorded at `audit` or later, keep working, because those steps keep their IDs. A run recorded as having *completed* `implement` under the old layout (for example, one stopped with `--until implement`) resumes into the new loop without the `implementor_response` capture that the judge's prompt needs. The judge step then fails with an undefined-variable error naming `implementor_response`, before any judge agent runs. This is part of the accepted incompatibility, with the same restart or original-binary guidance. No bootstrap that recovers the response from the audit log is provided. A new `implement-change-v3.0.yaml` was rejected: Change v3's own recorded runs would still resume into a changed child, so seamless resume would also need a new `change-v4.0` in both namespaces. That is far more surface than a pre-release project needs for a few in-flight runs.

**Failure-reason wording.** In `internal/exec`, the failure record already knows how many repair attempts ran. It needs to record whether its stdout and stderr came from a check run that preceded the final repair attempt. That happens on a blocked declaration from a repair agent or rerun target, and when a repair agent failed to run. `ClassifyFailure` then renders the stderr as `before repair: …`. A guarded action that blocks before any repair keeps today's wording, because no repair ran.

**Material risks.**
- *Weak judge on smaller models:* the factory's `tester` default is `codex:gpt-6-luna:medium`. The deterministic check, not the judge, is the authority for "pass". The judge only decides how to respond to a failure, and the budget and stall detection bound the cost of a bad verdict. `crosscheck` is the fallback role if this proves weak.
- *Cost:* up to 3 extra long implementor passes. That is the price of not losing the run. The stall check ends unproductive passes early.
- *Judge overreach in `fix-record`:* the prompt forbids code changes, test runs, and new evidence. `check-evidence.sh` still compares the checklist with the planned copy, and the crosscheck audit still follows.

## Out of Scope

- A Claude Stop hook or any CLI-specific completion mechanism.
- Starting a fresh implementor per pass (pure Ralph). That comparison is a possible later eval.
- Changes to `complete-evidence-after-review`, `recheck-evidence`, the crosscheck audit, `verify-change`, or acceptance.
- Changes to `core/implement-change-v1.0.yaml`, the v1.0/v2.0 change workflows, or the `simple-change` workflows.
- Making the continue budget a workflow parameter. It is fixed in the definition.
- Changing `CLAUDE_AUTOCOMPACT_PCT_OVERRIDE` handling or how agent environments are inherited.

## Impact

- **Workflows:** `workflows/core/implement-change-v2.0.yaml` (new loop, judge session, `implement` prompt, `complete-evidence` removed), plus new bundled scripts under `workflows/core/`. Because the file is edited in place, every Change v3 run picks this up. In-flight runs recorded inside the replaced steps (for example, one stopped in the `check-evidence` repair) cannot resume and must be restarted or finished on their original binary. Runs recorded before `implement` finished, or at `audit` or later, resume normally. Runs recorded after `implement` completed fail at the judge step with an undefined `implementor_response`.
- **Go:** `internal/exec/failure.go` and `repair.go` (failure-record provenance and reason wording). The run view, run list, and debug failure surfaces render the new wording through the shared classifier.
- **Tests:** script tests and step-layout tests in `workflows/*_test.go` (for example, the `verify_change_test.go` step order that currently lists `complete-evidence`), plus classifier and repair tests in `internal/exec`.
- **Specs:** a new `implementation-completion-loop` spec, and deltas to `builtin-workflows` and `run-failure-evidence`.
- **Users:** Change v3 runs can take longer but no longer pass an unfinished implementation to the audit. Failed runs report "implementation stalled", "implementation incomplete", or a blocked reason, with a list of what is left.
