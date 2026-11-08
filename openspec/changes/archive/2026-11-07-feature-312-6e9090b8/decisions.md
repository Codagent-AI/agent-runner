# Decisions: feature-312-6e9090b8

## propose: verdict
- **Decision:** Go. The issue's decided design is feasible with existing workflow primitives plus a small Go change to how failure reasons are worded.
- **Alternatives considered:** A Claude Stop hook (CLI-specific; the issue rules it out); a larger `complete-evidence` budget (still re-prompts blindly and cannot tell "blocked" from "unfinished"); no-go.
- **Decision-bearing:** yes

## propose: continue-pass budget
- **Decision:** 3 continue passes.
- **Alternatives considered:** 4 or 5 (the issue allows 3 to 5). A pass can take about 1h45m, the manual fix needed one pass, and stall detection already ends unproductive loops, so the lower bound limits cost without losing the observed recovery.
- **Decision-bearing:** yes (within the range the issue allows)

## propose: judge session
- **Decision:** A step-level `agent: tester` with `session: new` for each judgment, not the declared `acceptance-tester` session.
- **Alternatives considered:** Reusing `acceptance-tester` (would compromise the acceptance tester's independence in `verify-change`); a new declared session (named sessions persist across loop iterations, so the judge would not be fresh).
- **Decision-bearing:** no (follows from the issue's "fresh agent")

## propose: fix-record accounting
- **Decision:** A `fix-record` verdict does not use up a continue pass. Consecutive `fix-record` verdicts with no passing check are capped separately (the design sets the cap) so the loop always terminates.
- **Alternatives considered:** Count `fix-record` against the continue budget (would spend expensive implementor budget on record fixes); no cap (risks an endless judge loop).
- **Decision-bearing:** no

## propose: malformed judge verdict
- **Decision:** A script validates the verdict file, and a malformed verdict is treated as a judge defect, never as a pass.
- **Alternatives considered:** Treat a malformed verdict as `continue` (silently spends implementor budget on a judge bug).
- **Decision-bearing:** no

## propose: failure-reason wording
- **Decision:** Label the check's stderr `before repair` when the last recorded check run predates the repair attempt that ended the cycle (a blocked declaration from a repair agent or rerun target, or a repair agent that failed to run). Keep today's wording when a guarded action blocks before any repair, and when the last check run followed the final repair.
- **Alternatives considered:** Omit the stderr entirely (loses a useful diagnostic); rerun the check after a blocked repair to refresh stderr (changes repair semantics). The free-text `failure_reason` keeps its field and format, so this is not a persisted-format break.
- **Decision-bearing:** no (the issue allows either labeling or omitting)

## propose: edit in place
- **Decision:** Edit `core/implement-change-v2.0.yaml` in place rather than add a new version.
- **Alternatives considered:** `implement-change-v3.0.yaml` plus updating both change-v3.0 references. The issue targets v2.0, and editing a versioned file in place is supported and is how recent changes to it landed.
- **Decision-bearing:** yes (revised by proposal-review PR-1: some in-flight runs cannot resume, which is now accepted explicitly)

## proposal-review: PR-1 (in-place edit breaks some in-flight resumes)
- **Finding:** applied. Confirmed in code: `ResolveResumeStep` (`internal/model/state.go`) errors `step "complete-evidence" not found` for a failed rerun frame whose target was removed, and the changed-file warning does not migrate anything.
- **Decision:** Keep editing `core/implement-change-v2.0.yaml` in place. Explicitly accept that runs recorded inside the replaced steps (`check-implementation-not-blocked`, `complete-evidence`, `check-evidence-not-blocked`, `check-evidence` and its repair frame) cannot resume. Their guidance is to restart the change workflow or finish the run on its original binary. I corrected the proposal's claim that such runs resume with only a warning, and added a Rollout paragraph.
- **Alternatives considered:** A new `implement-change-v3.0.yaml` (Change v3's own recorded runs would still resume into a changed child, so seamless resume would also need a `change-v4.0` in both namespaces); a deliberate state migration for the removed steps (code and tests for a handful of in-flight runs in a pre-release project whose CLAUDE.md prefers clear changes over compatibility).
- **Direction-level:** no. The issue targets v2.0. The persisted state format does not change; only the resumability of some in-flight runs against an edited workflow does, which is the same trade-off earlier in-place edits to this file made.
- **Decision-bearing:** yes

## proposal-review: PR-2 (finished work with only a missing completion record)
- **Finding:** applied.
- **Decision:** `fix-record` may create or repair `implementation-complete.md` when the commits, checklist, and saved evidence already show the work is done. It still writes no product code, runs nothing, and creates no evidence. After every `continue` pass, including the last, the blocked check and completion check run before any stall or budget outcome. The stall check applies only when the completion check still fails. The three-pass bound for unfinished work stays.
- **Alternatives considered:** Keep the completion record implementor-only and route its absence to `continue` (a pass that only writes the record would be called stalled); exempt the record from the completion check (drops the issue's required completion record).
- **Direction-level:** no
- **Decision-bearing:** no

## spec: fix-record cap
- **Decision:** At most 2 consecutive `fix-record` verdicts without the completion check passing in between. A third fails the run with "implementation record could not be fixed".
- **Alternatives considered:** Leaving the cap to design (a spec needs a testable bound); 1 (a single wrong fix would end a finished run); counting `fix-record` against the continue budget (rejected in propose).
- **Direction-level:** no
- **Decision-bearing:** no

## spec: final judgment after the last continue pass
- **Decision:** When the completion check still fails after the third `continue` pass, the judge runs once more. `blocked`, `fix-record`, and `complete` take effect as usual, and `continue` fails the run with `implementation incomplete` plus the judge's list of what is left.
- **Alternatives considered:** Failing as incomplete straight after the third pass (no current list of remaining work, and it misses record-only fixes or a legitimate `complete`).
- **Direction-level:** no
- **Decision-bearing:** no

## spec: deterministic guard on `complete`
- **Decision:** A `complete` verdict is honored only when the completion check reports that rows `not verified` with a reason are its only unmet condition. Otherwise it is an invalid verdict and fails the run.
- **Alternatives considered:** Trusting the judge's `complete` (would let a weak judge skip a missing completion record or missing commits, contradicting the proposal's "the deterministic check is the authority").
- **Direction-level:** no
- **Decision-bearing:** no

## spec: invalid verdict handling
- **Decision:** A missing or malformed verdict, a `continue` with no remaining-work list, or an unguarded `complete` fails the run with "completion judge returned no valid verdict". There is no automatic re-judgment during the failed run. On manual resume, the judge and validation group restarts from its first member, discards a rejected verdict file, and re-runs the judge for the still-open ledger entry. A verdict already recorded in the ledger remains authoritative and skips the judge.
- **Alternatives considered:** Automatically re-running the judge once (more surface for a defect case; manual resume provides a retry through the explicit judge and validation group).
- **Direction-level:** no
- **Decision-bearing:** no

## spec: blocked record checked before every completion check
- **Decision:** The blocked-record check runs before every completion check, including after `continue` passes, and stops the run before any stall or budget outcome.
- **Alternatives considered:** Checking only after `implement` (a blocked record written during a `continue` pass would be judged or called stalled).
- **Direction-level:** no
- **Decision-bearing:** no

## spec: before-repair wording
- **Decision:** The label renders as `<step> failed: before repair: <stderr line>`, or `<step> failed with exit code N before repair` when stderr is empty. Whether the output predates the last repair is persisted in the failure record, so historical runs render the same reason, and records written before this change render unlabeled.
- **Alternatives considered:** Omitting the stale stderr (loses a diagnostic); labeling it only in the run view (surfaces would disagree, which the existing spec forbids).
- **Direction-level:** no
- **Decision-bearing:** no

## spec: removed-step resume behavior
- **Decision:** Spec the accepted incompatibility as observable behavior: resuming a run recorded at a removed step or repair target fails with an error naming the missing step, and never silently resumes elsewhere.
- **Alternatives considered:** Leaving it unspecified (the trade-off accepted in PR-1 would go untested).
- **Direction-level:** no
- **Decision-bearing:** no

## design: loop composition and bounds state
- **Decision:** Build the loop from existing primitives (a counted `loop` with `max: 12`, scripts, `skip_if`/`break_if`, and an `agent: tester`/`session: new` judge). The budget lives in a JSON-lines judgment ledger, and the judge writes a JSON verdict that a script validates and captures as a map.
- **Alternatives considered:** A new loop step type (more Runner surface); using loop iterations as the budget (no iteration variable, and the budget is per verdict type); parsing the verdict from the judge's response (free text in control flow).
- **Direction-level:** no
- **Decision-bearing:** no

## design: judge commits excluded from the commit condition
- **Decision:** The judge commits `fix-record` checklist edits with the `[judge-completion]` prefix, and the completion check counts only commits without that prefix. I added this to the spec, with a scenario.
- **Alternatives considered:** Leaving the judge's edits uncommitted (pushes the commit onto the `check-clean` repair); counting them (a record-only commit could satisfy "work was committed").
- **Direction-level:** no
- **Decision-bearing:** no

## design: judge has no call_agent
- **Decision:** The judge cannot call the lead. Rulings that need the user go through `blocked`.
- **Alternatives considered:** Giving it `call_agent` to the lead (weakens independence and adds cost).
- **Direction-level:** no
- **Decision-bearing:** no

## design: before-repair flag on the repair frame
- **Decision:** `RepairFrame.CheckBeforeLastRepair` is set when a repair attempt starts and cleared when the check runs. It is copied to `FailureRecord.OutputBeforeRepair` and emitted as `repair_output_before_repair` on `step_end`.
- **Alternatives considered:** A flag on each `terminalBlocked` caller (misses the exhaustion path when the agent failed to run).
- **Direction-level:** no
- **Decision-bearing:** no

## design: blocked verdict requires the blocked record
- **Decision:** A `blocked` verdict without a non-empty `implementation-blocked.md` is invalid. I added this to the spec's verdict validation.
- **Alternatives considered:** Writing a generic blocked record on the judge's behalf (loses the cause and the decision needed).
- **Direction-level:** no
- **Decision-bearing:** no

## test-plan: test layers
- **Decision:** Four integration tests run against the embedded workflow, the real bundled scripts, and real git, with only the agents stubbed. Three end-to-end tests drive the built binary with a fake agent CLI: the early-stop journey, budget surviving resume, and the labeled reason on the console and in state.
- **Alternatives considered:** Script-only tests (miss the YAML wiring); a full Change v3 E2E through `verify-change` (needs a PR and Validator; `--until audit` covers the changed portion); real-agent E2E in CI (cost and nondeterminism).
- **Direction-level:** no
- **Decision-bearing:** no

## test-plan: acceptance envelope
- **Decision:** Unlimited runs with fake CLIs, and at most two real-agent `core:implement-change` runs on a tiny scratch change, stopped with `--until audit`. No GitHub writes, and no touching existing runs or user settings. A scripted implementor may substitute for paths a real model cannot be made to hit reliably.
- **Alternatives considered:** Real agents for every path (forcing an early stop is unreliable and costly); no real-agent run (the judge's behavior on a real model would go unobserved).
- **Direction-level:** no
- **Decision-bearing:** no

## test-plan: human-only testing
- **Decision:** None. Every behavior is observable by an agent with the product and tools.
- **Alternatives considered:** A human check of judge verdict quality (judge quality is a later eval, outside this change's acceptance).
- **Direction-level:** no
- **Decision-bearing:** no

## approach-review: AR-1 (before-repair label versus infrastructure precedence)
- **Finding:** applied. Confirmed: `runInlineRepairAgent` records a non-zero repair-agent exit as an infrastructure failure, `classifyRunFailure` picks `ClassifyCrash` first, and `TestNestedRepairCrashReasonKeepsAttemptCount` pins the unlabeled infrastructure reason.
- **Decision:** Infrastructure classification keeps precedence. The `before repair` label applies only to reasons rendered from the check's failure record by `ClassifyFailure`. The "failed to run" case now means a repair agent judged failed with failure kind `step` (for example, a disallowed `AskUserQuestion`). I added a scenario for a crashing final repair agent, which keeps the infrastructure reason with no label. I aligned the spec, the design (Failure-reason wording, and the Decision 6 wording), and test-plan INT-004 (c) and the new (g). This supersedes the "repair agent that failed to run" wording in "propose: failure-reason wording".
- **Alternatives considered:** Labeling the check's stderr even on a crash (changes established infrastructure semantics and breaks an existing passing test).
- **Direction-level:** no
- **Decision-bearing:** no

## approach-review: AR-2 (old runs recorded after implement completed)
- **Finding:** applied, by narrowing the accepted rollout policy.
- **Decision:** A run recorded with `implement` completed under the old layout resumes into the loop without `implementor_response`. The judge step fails at strict prompt interpolation with a `step`-kind undefined-variable error naming `implementor_response`, before any judge agent starts. This is now part of the accepted incompatibility, with restart or original-binary guidance. I updated the proposal's Rollout and Impact, the design's Migration Plan, and the builtin-workflows spec, which has a new scenario. Compatibility fixture INT-005 covers this boundary and the removed-step boundary.
- **Alternatives considered:** A bootstrap that recovers the implement response from the audit log before the judge prompt (new resume-time machinery for a narrow, transient case); having the judge read the response from a file written by a new step after `implement` (that step would itself fail on the same undefined capture for these runs).
- **Direction-level:** no. It extends the trade-off already accepted in PR-1 and changes no persisted format.
- **Decision-bearing:** yes

## approach-review: AR-3 (E2E-002 stall variant contradicted the snapshot semantics)
- **Finding:** applied.
- **Decision:** I split E2E-002 into Case A, where pass 2 commits before the crash and is not stalled because the pre-crash commit counts toward the whole pass, and Case B, where pass 2 crashes before any change and the resumed invocation also changes nothing, so the run fails as `implementation stalled`. Both cases assert that the original snapshot survives the resume and that pass 2 is counted once.
- **Alternatives considered:** Refreshing the snapshot on resume (would discard real progress made before the crash, contradicting the spec's whole-pass stall definition).
- **Direction-level:** no
- **Decision-bearing:** no

## tasks: single implementation task
- **Decision:** One task covers the workflow edit, the three bundled scripts, the Go failure-reason change, every INT and E2E obligation, and the docs update to the `core:implement-change` row in `docs/built-in-workflows.md`.
- **Alternatives considered:** Separate tasks for the Go change and the workflow (the instruction requires exactly one task).
- **Direction-level:** no
- **Decision-bearing:** no
