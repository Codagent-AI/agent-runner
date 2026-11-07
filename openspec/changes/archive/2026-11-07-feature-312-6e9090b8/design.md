## Context

`workflows/core/implement-change-v2.0.yaml` currently runs these steps between the checklist commit and the audit:

```
record-implementation-start → implement → check-implementation-not-blocked
  → complete-evidence (skip_if no evidence-problems.txt) → check-evidence-not-blocked
  → check-evidence (repair: rerun complete-evidence, max 2) → audit → …
```

When the `implement` agent turn exits successfully, nothing checks that the change is finished. The `check-evidence` repair can only tidy the checklist: it runs tier 1 tests within 15 minutes.

Runner primitives this design relies on, all existing:

- **Counted loops** (`loop: {max: N}`) with `break_if: success` on a child. Exhausting the loop fails the workflow, unless the loop step itself sets `continue_on_failure`. A child step without `continue_on_failure` that fails stops the loop and the run, and the run's failure reason is that child's failure record (`<step> failed: <first stderr line>`).
- **Iteration context.** An iteration starts from a copy of the parent's captures and merges its own captures back when it ends (`internal/exec/loop.go`, `mergeIterationCaptures`). A capture made inside the loop therefore overwrites a same-named capture from outside it for later iterations.
- **`skip_if: 'sh: <cmd>'`** skips a step when `<cmd>` exits 0.
- **Script steps.** `script_inputs` is passed as JSON on stdin, so interpolated agent text is never shell-parsed. `capture_format: json` yields a string map whose fields read as `{{cap.field}}`. Bundled scripts are materialized whole under `bundled/<namespace>/`, so one script can call a sibling through `$script_dir`.
- **Sessions.** Named sessions (`implementor-agent`) are shared across loop iterations. A step-level `agent: <profile>` with `session: new` (as in `core/debug-v1.0.yaml`) starts a fresh session.
- **Repair failure records.** `internal/exec/repair.go` builds the failure record for a check with repair. `terminalBlocked` and `terminalExhausted` end the cycle, `ClassifyFailure` (`internal/exec/failure.go`) renders the reason, and the run view rebuilds the record from `step_end` data (`internal/runview/audit.go`, `applyRepairStepEnd`).

## Goals / Non-Goals

**Goals:**
- Implement the `implementation-completion-loop` spec as plain workflow composition plus bundled scripts, with no new step type and no CLI-specific hook.
- Keep every bound (continue passes, consecutive `fix-record` verdicts, stall) enforced by deterministic scripts whose state survives resume.
- Label failure output from before a repair, consistently on every surface.

**Non-Goals:**
- Changing loop, repair, or session semantics in the Runner.
- Migrating in-flight runs recorded inside the replaced steps (accepted in proposal-review PR-1).
- Mechanically enforcing that the judge only inspects. As with the existing audit step, the prompt states it, and the deterministic checks plus the crosscheck audit contain the damage if the judge ignores it.

## Approach

### Workflow layout

Replace `check-implementation-not-blocked`, `complete-evidence`, `check-evidence-not-blocked`, and `check-evidence` with one loop step. `implement` gains `capture: implementor_response`, and its prompt gains the completion-record rules.

```
implement (capture: implementor_response)
complete-implementation (loop: {max: 12})
  1 check-completion-not-blocked    script check-not-blocked.sh                       stops run if blocked
  2 check-completion                script check-implementation-complete.sh           continue_on_failure, break_if: success
  3 check-progress                  script check-implementation-progress.sh           stall check, opens judgment
  4 judge-completion                agent: tester, session: new, autonomous           writes verdict JSON
  5 check-completion-verdict        script check-completion-verdict.sh                validates, counts, capture: completion_verdict (json)
  6 check-judge-not-blocked         script check-not-blocked.sh                       stops run on a blocked verdict
  7 accept-complete                 command: "true", skip_if verdict != complete      break_if: success
  8 continue-implementation         session: implementor-agent, skip_if verdict != continue,
                                    tools: [call_agent], capture: implementor_response
audit …                             unchanged from here on
```

A `fix-record` verdict skips steps 7 and 8, so the next iteration's `check-completion` decides. Every unchanged step after `audit` (`resolve-findings`, `complete-evidence-after-review`, `recheck-evidence`, `check-clean`, `verify-implementation-commits`, …) keeps its ID and behavior.

**Why `max: 12`.** Iterations are a safety net, not the budget: the scripts end the run before the loop can exhaust. Every iteration that does not break makes exactly one judgment. The longest accepted sequence before a terminal judgment is `F F C F F C F F C F F` (two `fix-record` verdicts before each of three `continue` passes, then two more), which is 11 judgments. The 12th judgment is always terminal: a 4th `continue`, a 3rd consecutive `fix-record`, `blocked`, `complete`, or a passing check. A comment in the YAML records this arithmetic. If the loop ever did exhaust, the existing `exhausted after 12 of 12 iterations` reason would apply.

### Files under `{{session_dir}}/output/`

| File | Written by | Purpose |
|---|---|---|
| `implementation-complete.md` | implementor; the judge on `fix-record` | completion record |
| `implementation-blocked.md` | implementor; the judge on `blocked` | blocked record (existing) |
| `completion-problems.txt` | `check-completion` | readable problem list for the judge |
| `completion-check.json` | `check-completion` | `{"passed": bool, "only_unverified_with_reason": bool}` |
| `completion-verdict.json` | judge | `{"verdict": "...", "reason": "...", "remaining": ["..."]}` |
| `completion-loop.jsonl` | `check-progress` and `check-completion-verdict` | judgment ledger: budget state that survives resume |
| `continue-snapshot.json` | `check-completion-verdict` on `continue` | `{"head": sha, "checklist_sha256": sha}` from before the pass |
| `implementation-remaining.md` | `check-completion-verdict` | the judge's latest remaining-work list |

### `check-implementation-complete.sh`

Inputs: `checklist_file`, `evidence_dir`, `plan_file`, `problems_file` (the existing `evidence-problems.txt`), `start_head`, `completion_file`, `report_dir`.

1. Pipe `{checklist_file, evidence_dir, problems_file, plan_file}` to `$script_dir/check-evidence.sh` and keep its exit code and stderr. Its rules are not copied. `evidence-problems.txt` keeps its existing contract (it is removed on success), so `complete-evidence-after-review` behaves as before.
2. Completion record: `test -s completion_file`.
3. Implementation commits: `git log --format=%s start_head..HEAD`, excluding subjects that start with `[judge-completion]`, must be non-empty. This stops the judge's own record-keeping commit from satisfying the condition.
4. Row statuses: parse the same table that `check-evidence.sh` reads (the first Markdown table with Status and Evidence columns, using the same `\|` escaping), and list every ID whose Status is not `verified`.
5. Classification: `only_unverified_with_reason` is true when the evidence check passed, the record exists, commits exist, and every non-`verified` row is `not verified`, not `failed`. The evidence check already guarantees that each such row has a reason.
6. Write `completion-problems.txt` (one `- ` line per unmet condition, row IDs included) and `completion-check.json`. Exit 0 only when every condition holds; otherwise print the problems to stderr and exit 1.

### `check-implementation-progress.sh`

This runs only after `check-completion` failed, because a pass breaks the loop first.

1. **Stall.** If `continue-snapshot.json` exists, compare it with the current `git rev-parse HEAD` and the checklist's sha256. If both are unchanged, print `implementation stalled: continue pass <n> added no commit and changed no checklist row` and exit 1. The step has no `continue_on_failure`, so the run fails with that reason. Otherwise, delete the snapshot.
2. **Open a judgment.** Remove any `completion-verdict.json`. If the ledger's last entry is an open judgment with no verdict, reuse it; this is what a resumed rerun of this step sees. Otherwise, append `{"judgment": n+1, "state": "open"}`.

### `judge-completion`

The judge runs with `agent: tester` and `session: new`, in autonomous mode, with no `call_agent`. It must be fresh and independent of the lead, so it has no escalation path. The prompt:

- names its inputs: `completion-problems.txt`, the checklist, the evidence directory, `git log`/`git diff {{implementation_start_head}}..HEAD`, `acceptance-assumptions.md` and `implementor-decisions.md` when present, and `{{implementor_response}}` inside `<implementor-response>` tags with the standard untrusted-data notice;
- forbids running tests, builds, the product, or any command that executes the change, and forbids creating evidence;
- defines the four verdicts in precedence order, with the issue's definitions and the lean toward `continue`;
- on `blocked`, writes `implementation-blocked.md` (cause and decision needed);
- on `fix-record`, edits only the Code, Evidence, and Status cells and/or writes `implementation-complete.md`, from saved evidence, and commits only the checklist with the prefix `[judge-completion]` when `{{change_dir}}` is inside the repository;
- always writes `completion-verdict.json` last.

### `check-completion-verdict.sh`

Inputs: `verdict_file`, `check_file` (`completion-check.json`), `ledger_file`, `blocked_file`, `remaining_file`, `snapshot_file`, `checklist_file`, `max_continue` (3), `max_fix_record` (2). It requires `jq`, like `check-evidence.sh`.

1. **Validate the verdict.** The file must exist and parse, `verdict` must be one of the four, and `continue` must have a non-empty `remaining` list. `complete` requires `only_unverified_with_reason`, and `blocked` requires a non-empty blocked record. On failure, print `completion judge returned no valid verdict: <why>` and exit 1.
2. **Record it in the ledger.** Record the verdict on the open judgment. If that judgment already has a verdict (a resumed rerun), reuse it rather than appending a second.
3. **Enforce the bounds.** Count `continue` verdicts in the ledger and the trailing run of `fix-record` verdicts.
   - A `continue` beyond `max_continue`: write `implementation-remaining.md`, print `implementation incomplete: <k> items remain after 3 continue passes` and then the list, and exit 1.
   - A `fix-record` beyond `max_fix_record`: print `implementation record could not be fixed after 2 fix-record verdicts` and exit 1.
4. **Prepare a `continue` pass.** On an accepted `continue`, write `implementation-remaining.md` and `continue-snapshot.json`.
5. **Emit the capture.** Print `{"verdict": "...", "remaining": "<list joined as Markdown bullets>"}` for `capture_format: json`.

Steps 7 and 8 interpolate `{{completion_verdict.verdict}}` into `sh:` expressions. That is safe because the script has already restricted it to the four literal words. `continue-implementation` puts `{{completion_verdict.remaining}}` inside `<remaining-work>` tags with the untrusted-data notice, and repeats the stopping rules: a compaction is not a stopping point; end only after writing the completion or blocked record. It keeps the `implement` step's other rules: no Agent Validator, commit with the `[{{step_id}}]` prefix, record each row's evidence as it goes, and `{{artifact_validation_instruction}}`.

### Resume

All loop state lives in files and in the Runner's loop and capture state. The ledger keeps the budget across resumes. Its open-then-record entries make the two counting steps idempotent when a resume reruns an interrupted step. A run interrupted during `continue-implementation` resumes that step, and the snapshot and ledger are already written, so at most the remaining passes run. This satisfies "Budget survives resume".

### Failure-reason wording (Go)

- `model.FailureRecord` gains `OutputBeforeRepair bool \`json:"outputBeforeRepair,omitempty"\``, and `model.RepairFrame` gains `CheckBeforeLastRepair bool`. The frame is already persisted in state, so the flag survives resume.
- `repair.go`: set `frame.CheckBeforeLastRepair = true` when a repair attempt starts its work (before `runInlineRepairAgent`, and in `armRerunReplay`). Clear it whenever the check runs inside the cycle (`settleInternalCheck`). The guarded-action block at the opening of the cycle happens before any attempt, so the flag stays false there. `terminalFailed` copies the flag onto `ctx.LastFailure` and `repairEndData` emits `repair_output_before_repair: true`. This covers all three spec cases (inline agent blocked, rerun target blocked, last repair agent failed with failure kind `step`) without per-caller flags.
- `ClassifyFailure`: when `OutputBeforeRepair` is set, render `<id> failed: before repair: <line>`, or `<id> failed with exit code N before repair` when stderr is empty. The `; blocked: …` and ` after N repair attempts` suffixes are unchanged.
- `runview`: `FailureEvidence` gains `OutputBeforeRepair`. `applyRepairStepEnd` reads `repair_output_before_repair`, and `classifiedFailureReason` passes it through, so the run view, run list, debug workflow (`FailureReasonForSession`), and console all render the same string. Older audit logs have no such field and render unlabeled.
- Infrastructure classification (`ClassifyCrash`) is unaffected and keeps precedence. `classifyRunFailure` already selects `ClassifyCrash` when the run ended with failure kind `infrastructure`, so a crashed final repair agent still renders `<check> repair failed (infrastructure): … after N repair attempts` with no label, even though the flag is set on the underlying record. The label shows only through `ClassifyFailure`. The run view's per-check failure evidence may still show the flagged record, which correctly describes the check output as older than the repair.

## Decisions

1. **Composition over a new step type.** Every behavior maps onto existing primitives. A dedicated "completion loop" step type would add Runner surface for one workflow.
2. **A JSON verdict file validated by a script, not parsing the judge's response.** The file has a strict schema that `jq` can check, and it keeps the free-text response out of control flow. Capturing the verdict as a JSON map gives the following steps typed fields.
3. **A ledger file rather than loop-iteration arithmetic.** There is no built-in iteration variable, and the budget is per verdict type, not per iteration. A ledger makes the counts explicit, testable, and resume-safe.
4. **Exclude `[judge-completion]` commits from the commit condition.** The judge's checklist commit is needed so that `check-clean` stays clean, but it must not count as implementation. The alternative, leaving the judge's edits uncommitted, would push the commit onto the `check-clean` repair and blur authorship.
5. **The stall check runs after the failed completion check, in a separate step.** This follows the spec order (blocked, then completion, then stall) and never calls a completing pass stalled.
6. **A flag on the repair frame for "the check has not run since the last repair".** It sits in one place, is persisted, and covers both repair forms and the non-crash agent-failure path. Threading a flag through each `terminalBlocked` caller would miss the exhaustion case.
7. **A judge with no `call_agent`.** The issue asks for an independent judge, and `blocked` covers rulings that need the user.

## Risks / Trade-offs

- **The judge ignores the inspection-only rule** and runs tests or edits code. A `fix-record` that touches planned cells is caught by `check-evidence.sh`'s plan comparison. Product edits are visible in the diff the crosscheck audits. The budget bounds the cost.
- **The judge picks `complete` too eagerly.** The script accepts it only when the deterministic classification says not-verified rows are the sole gap, and the audit still reviews every not-verified reason.
- **Duplicated table parsing.** The status scan in step 4 of the completion check re-reads the same table as `check-evidence.sh`. Both must follow the codagent:verify-behavior columns. A shared fixture in the script tests runs both against the same checklists to catch drift.
- **Cost:** up to 3 extra long passes plus up to 12 short judgments. Stall detection and the deterministic gates keep the worst case rare.
- **Prompt size:** `implementor_response` is interpolated into the judge prompt. Final responses are short relative to the context window, and the audit already interpolates a whole findings list the same way.

## Migration Plan

- Edit `core/implement-change-v2.0.yaml` in place and add three bundled scripts under `workflows/core/`. No new workflow version.
- Runs recorded before `implement` finished, or at `audit` or later, resume normally. Runs recorded inside the four removed steps fail resume with `step "<id>" not found`, which is existing `ResolveResumeStep` behavior, now specified. A run recorded with `implement` completed under the old layout advances into the loop without `implementor_response`. The first judgment then fails at prompt interpolation (strict interpolation in `buildAgentPrompt`) with a `step`-kind undefined-variable error naming `implementor_response`, before any agent process starts. Their guidance is to restart the change workflow, or finish the run on the binary it started with.
- The failure-reason change is additive: a new optional JSON field in state and audit, read as false when absent.
- Rollback: revert the commit. Runs started on the new layout would then hit the same not-found resume error in reverse.

## Testing Strategy

- **Script tests** in a new `workflows/completion_loop_scripts_test.go`, following the existing `*_scripts_test.go` pattern of running the real script in a temp git repo:
  - each completion-check condition, and the classification;
  - judge commits excluded;
  - stall versus progress, and the first iteration with no snapshot;
  - ledger idempotence on rerun;
  - every verdict-validation failure and both budget failures, with exact first stderr lines;
  - the capture JSON shape.
- **Workflow layout tests** in `workflows/verify_change_test.go` and `embed_test.go`: update the expected step order, assert that `complete-evidence` is absent and the loop's children and sessions are present, and assert the judge uses `agent: tester` and `session: new`.
- **Go unit tests** in `internal/exec`: `ClassifyFailure` with and without the flag and with empty stderr; repair-cycle tests for inline-agent blocked, rerun-target blocked, agent failed to run then exhausted, guarded block before repair (unlabeled), and exhaustion after a recheck (unlabeled); frame-flag persistence across resume.
- **Run view tests** in `internal/runview`: `step_end` with `repair_output_before_repair` renders the labeled reason, and an older log without the field renders unlabeled.
- **Resume test:** in `internal/model`, a failed rerun frame whose target is missing returns the not-found error. That behavior exists already; the test pins the specified outcome.

## Open Questions

None.
