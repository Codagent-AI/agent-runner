## Context

Agent Runner executes steps through three forward-only sequencers: the top-level loop in `internal/runner/runner.go` (`executeSteps` / `executeTopLevelStep`), the loop-body executor in `internal/exec/loop.go` (`executeIterationBody`), the sub-workflow executor in `internal/exec/subworkflow.go`, and the group executor `executeGroupStep` in `internal/exec/dispatch.go`. Groups are a fourth forward-only sequencer. All three call `exec.DispatchStep`, propagate `OutcomeAborted` upward, apply `continue_on_failure` / `warn_on_failure`, and write resume state through `FlushState` and `NestedStepState`. Shell and script steps run in `ExecuteShellStep` / `ExecuteScriptStep`; agent steps in `ExecuteAgentStep`, which builds the prompt, resolves the profile and session, and emits `step_start` / `step_end` audit events with the adapter's filtered output in `stdout`.

Resume is resolved by `model.ResolveResumeStep`, which knows only a recorded step ID and a completed bit at each nesting level. Failure reason strings are derived after the fact by `runview.failureReason` from the audit tree. The only backward movement today is a manual edit of `state.json` before `--resume`.

The specs for this change (`step-repair`, `run-failure-evidence`, and deltas to `step-flow-control`, `recursive-state`, `view-run`, `list-runs`, `live-run-view`, `audit-log-entries`) define the behavior. This document defines how it is built and how the built-in workflows are migrated.

## Goals / Non-Goals

**Goals:**
- One repair executor that wraps a shell or script check and owns the inline repair cycle end to end.
- A rewind signal that lets the rerun form move any of the four sequencers back to an earlier step without restructuring them.
- A persisted repair frame so interruption anywhere in the cycle resumes correctly, and failed rerun checks resume at their target.
- Failure records and a classified failure reason computed once by the runner and consumed by the run view, run list, headless output, and debug workflow.
- Migration of the eight built-in sites named in the proposal, including the archive split, with real-Git tests.

**Non-Goals:**
- A shared sequence controller or any refactor of iteration and child-progress resume bookkeeping.
- A new agent role, a new run status, transient-failure retry, or a control-channel decline signal.
- Migrating `run-validator`, the `check-definition` loop, or `verify-acceptance-handoff`.

## Approach

### Model

`model.Step` gains `Repair *Repair`:

```go
type Repair struct {
    Prompt  string          `yaml:"prompt,omitempty"`
    Session SessionStrategy `yaml:"session,omitempty"`
    Agent   string          `yaml:"agent,omitempty"`
    Rerun   string          `yaml:"rerun,omitempty"`
    Max     *int            `yaml:"max,omitempty"`
}
func (r *Repair) Form() RepairForm   // RepairInline | RepairRerun
func (r *Repair) Budget() int        // Max or 1
```

`Step.Validate` enforces the field-level rules from the `step-repair` spec (shell/script only, exactly one form, inline names exactly one of session/agent, `max >= 1`). Scope-level rules (target is an earlier sibling, replay range free of `break_if` and `skip_if: previous_success`) are enforced by a new `validateRepairTargets(steps []Step)` called from `Workflow.Validate` for the top-level list and recursively for every `steps:` body, since only the containing list knows sibling order.

`model.NestingSegment` gains `RepairAttempt *int`. `audit.BuildPrefix` renders it as a distinct token `attempt:N` placed after the owning check's token, so a replayed step's prefix is `[verify-draft-pr, attempt:1, open-draft-pr]`, the inline repair agent's is `[check-plan, attempt:1, repair]`, and a check rerun is recorded on the attempt event rather than as a nested step. `runview.parsePrefix` learns the `attempt:` token alongside `sub:` and `call:`.

`model.NestedStepState` gains `Repair *RepairFrame`:

```go
type RepairFrame struct {
    CheckID   string `json:"checkId"`
    Form      string `json:"form"`             // inline | rerun
    Target    string `json:"target,omitempty"` // rerun target
    Phase     string `json:"phase"`            // checking | repairing | replaying | failed
    Attempts  int    `json:"attempts"`         // completed attempts
    Budget    int    `json:"budget"`
    Guarded   *ExecutionRef `json:"guarded,omitempty"` // prefix + attempt of the guarded agent execution
    RangeCaptures []string  `json:"rangeCaptures,omitempty"` // capture names owned by replay-range steps
}
```

Lifecycle: created on the first failed check run; `Phase` advances through the cycle; cleared on success and when `continue_on_failure` or `warn_on_failure` advances past the check (the check is then recorded as completed in every sequencer, including the loop and sub-workflow child-progress paths that today mark any failed outcome incomplete); retained with `Phase == failed` only when the failure stops the run.

`model.ExecutionContext` gains `RepairFrame *RepairFrame` (the open frame for the current scope), `PendingRewind *RewindRequest`, and `LastAgentExecution *AgentExecutionRecord` (`Ref ExecutionRef{Prefix, Attempt}`, `Response`, `CallResponses []CallResponse`). It is written after every completed ordinary agent step in the scope and by the agent-call completion path, and left in place across later shell, script, sub-workflow, and skipped steps, so a check finds the most recent agent execution in its own scope (the `verify-task-commit` case, four steps after `generate-code`). Executions under an `attempt:N` prefix (inline repair agents, replayed targets) are recorded on the frame as `RepairResponse` and never overwrite it.

Scope isolation: sub-workflow and loop-iteration child contexts start with `LastAgentExecution == nil`, so an agent inside `run-validator` is never the guarded execution for a check in `implement-task`. Groups reuse the parent context, so `executeGroupStep` saves the record before the body and restores it after, and a check inside a group sees only agents that ran earlier in that group.

Persistence: `NestedStepState` gains `LastAgent *ExecutionRef`, written by every state-chain constructor (the same list as the repair frame) after each completed agent step, independent of any frame, so an interruption between the guarded step and the check does not lose the identity. On resume the reference is restored and, when the check needs the response, `exec.LoadAgentExecution(sessionDir, ref)` rebuilds it from the audit log by exact prefix and attempt, the same lookup the run view performs. The attempt number comes from the metrics collector's per-prefix counter (`metrics.Collector.AttemptFor(identity)`), which is the counter that stamps `identity.attempt` on the audit `step_end`; `emitAgentEnd` reads it back after emission so context, state, and audit carry the same number. `BuiltinVarsForStep` exposes `repair.attempt`, `repair.check_output`, `repair.check_stderr`, and `repair.action_response` when a frame with evidence is open.

### Repair executor

The shell and script executors are split into an audit-free primitive and the audited wrapper: `runShellCheck(step, ctx, runner) (ProcessResult, error)` and `runScriptCheck(...)` perform interpolation, process execution, and nested-metrics capture but emit no `step_start` / `step_end`, add no warning origin, and do not write `capture`. `ExecuteShellStep` / `ExecuteScriptStep` keep their current behavior by calling the primitive and then doing the audit, warning, and capture work, so steps without `repair` are unchanged apart from the failure record.

`exec.ExecuteCheckStep(step, ctx, runner, glob, log)` replaces the direct calls in `DispatchStep` for shell and script steps. Without `Repair` it delegates to the existing executor and writes a failure record on non-zero exit. With `Repair` it owns exactly one `step_start` and one terminal `step_end` for the check; every internal check run is executed through the primitive and audited as its own child execution under the attempt prefix (`[check, attempt:N, check]`); the warning origin is added only at the terminal outcome; `capture` is written only at the terminal outcome from the final run:

```
runCheck()                                    ← ordinary shell/script execution, audit as today
if pass → done
record failure evidence (check output + ctx.LastAgentExecution)
if guarded response has REPAIR_BLOCKED → emit repair_blocked, terminal failed
loop while attempts < budget:
    emit repair_attempt_start {attempt, form, target, failed check exit/stdout/stderr}
    frame.Phase = repairing|replaying; FlushState
    inline:  run synthesized agent step under prefix [check, attempt:N, repair]
             if its response ends REPAIR_BLOCKED → repair_blocked, terminal failed
    rerun:   clear captures named in frame.RangeCaptures (computed at load time
             from the replay range's capture fields); set ctx.PendingRewind{Target, CheckID};
             return OutcomeFailed
             (the sequencer replays target..check under the attempt prefix; the
              replayed check re-enters ExecuteCheckStep with the open frame and
              continues here as the attempt's check run)
    frame.Phase = checking; runCheck() as child execution [check, attempt:N, check]
    emit repair_attempt_end {attempt, form, outcome, exit_code}
    attempts++
    if pass → clear frame, terminal success
terminal failed (exhausted)
```

The synthesized inline agent step is a `model.Step{ID: "repair", Prompt: block.Prompt + evidenceBlock, Session: block.Session, Agent: block.Agent, Mode: ModeAutonomous}` executed by `ExecuteAgentStep` inside an attempt context created by `model.NewRepairAttemptContext(owner, attempt)`: it carries the `attempt:N` nesting segment and a nil `LastAgentExecution`, but shares the owning scope's `SessionIDs`, `SessionProfiles`, `LastSessionStepID`, `NamedSessions`, and `CapturedVariables` by reference (unlike sub-workflow and iteration contexts, which copy only the session seed), so `session: resume` resolves to the scope's most recent session (`generate-code` in `implement-task`) and a session it creates is visible to the scope afterward. Replayed steps in the rerun form run in the same attempt context. `evidenceBlock` is a fixed template that wraps `repair.check_output`, `repair.check_stderr`, and `repair.action_response` in `<repair-evidence>` tags followed by the untrusted-input notice already used by `run-validator`. Named sessions resolve through the shared `NamedSessions` map, so `session: planning-agent` resumes the caller's declared session.

For the rerun form the target agent step gets the same evidence block prepended to its prompt when `ctx.RepairFrame` is open and the step ID equals `frame.Target`; this is a small addition to `buildAgentPrompt` next to the intake-handoff preface.

The blocked marker is detected by `repairBlocked(response string) bool`: last non-empty line, trimmed, equals `REPAIR_BLOCKED`. It is evaluated on `ctx.LastAgentExecution.Response` for the guarded action and rerun target, and on the inline agent's response.

`terminal failed` writes the failure record to `ctx` (`ctx.LastFailure *FailureRecord`) and the check's `step_end` carries `repair_form`, `repair_target`, `repair_attempts`, `repair_blocked`, and `guarded_prefix` / `guarded_attempt`.

### Rewind signal in the sequencers

Each sequencer, immediately after `DispatchStep` returns, checks `ctx.PendingRewind`. If set, it clears it, records the rewind in audit, finds the target index in its own step list, and sets the loop index so the next iteration executes the target. `previous_success` is not updated for the rewound-past steps; `LastStepOutcome` is left as it was before the replay began. Because the frame is open on the context, the replayed check re-enters `ExecuteCheckStep`, which sees `Phase == replaying` and continues the cycle instead of starting a new one.

Four edits of the same shape, each two helper calls:

- `runner.executeSteps`: `for i := start; i < len; i++ { ...; if rw := takeRewind(rs.ctx); rw != nil { i = index(rw.Target) - 1; continue } }`
- `exec.executeIterationBody`: same, on the body slice, keeping `setBody` bookkeeping so state records the replayed step as current.
- `exec.ExecuteSubWorkflowStep`: same, on `workflow.Steps`.
- `exec.executeGroupStep`: same, on the group's `steps`.

A blocking failure or abort of a replayed step is caught by the frame: the sequencer would normally stop, so before applying its ordinary failure handling it checks `ctx.RepairFrame != nil && frame.Phase == replaying`; if so it treats the outcome as a failed attempt (`repair_attempt_end` with the replayed step's identity), increments `Attempts`, and either rewinds again (budget left) or marks the check failed as exhausted. This keeps "the check owns the terminal outcome" without the sequencer knowing repair semantics beyond one helper call, `exec.AbsorbReplayFailure`.

When a check's terminal outcome is failed but `warn_on_failure` or `continue_on_failure` lets the sequencer advance, the sequencer records the check as completed (today the loop and sub-workflow paths compute `completed` as "not failed"; they gain `|| IsWarningOutcome(...) || step.ContinueOnFailure` so an interruption right after an exhausted warning check resumes at the next step, not inside the repair cycle) and the frame is cleared before the flush.

### State and resume

Every constructor of a `NestedStepState` for a scope copies that scope's `ctx.RepairFrame`: `runner.writeStepState` (top level), `exec.recordChildProgress` (sub-workflow and group children), `exec.persistIterationFailState` and `exec.buildIterationFlushChain` (loop iterations, including the mid-iteration flush chain). A frame is attached at the nesting level of the check's own scope and never promoted to a parent entry. On resume, `restoreResumeContext` and the loop / sub-workflow resume paths restore the frame onto the context of the scope that owns it. `ResolveResumeStep` gains a third input, the frame:

- frame open with `Phase == replaying` → resume at the recorded current step (a replayed step), frame kept.
- frame open with `Phase == repairing` → resume at the check; `ExecuteCheckStep` sees the phase and resumes the inline agent session for the same attempt before rerunning the check.
- frame `Phase == failed` with `Form == rerun` → resume at `frame.Target` with `Attempts = 0`.
- frame `Phase == failed` with `Form == inline` → resume at the check with `Attempts = 0`.
- no frame → today's behavior.

The stale-target error reuses the existing "step no longer exists" path with the target named.

### Failure record and reason

`model.FailureRecord{StepID, Prefix, Attempt, ExitCode, Stdout, Stderr, Guarded *AgentExecutionRecord, Blocked bool, BlockedBy string, RepairAttempts int}` is produced by `ExecuteCheckStep` for every non-zero exit. The sequencer that decides the failure is terminal calls `exec.ClassifyFailure(record) string`, which renders the reason exactly as the `run-failure-evidence` spec states. The runner writes it to `RunState.FailureReason` (new field, `failureReason` in JSON) in the final state write, prints it above the `to resume:` hint, and adds `failure_reason` to `run_end`.

Consumers:
- The failure record is durable in the failed `step_end` (and its guarded response in the guarded execution's own events). Audit is append-only, so a later re-execution of the same check on resume does not erase it: the run view's existing previous-execution mechanism (`Tree.PreviousExecution`) exposes the earlier attempt's node, and its detail renders the same `Failure evidence` section. No new audit representation is needed.
- `runview.failureReason` returns `state.FailureReason` when present and falls back to the current derivation for older runs.
- `runs.RunInfo` gains `FailureReason`, read from state; `listview` appends it after the step column for inactive, uncompleted runs, truncated by `fitCell`.
- The selected-detail renderer adds a `Failure evidence` rail section for a failed shell/script node whose `step_end` carries `guarded_prefix`; the guarded response is looked up by prefix in the tree rather than duplicated into the check's event.
- `core:debug`'s prompt already reads the run-view failure reason through `FailureReasonForSession`; it picks up the richer string for free.

### Run view and live view

`runview` tree: a check node with `Repair` gets children built from audit at ingest time: `repair_attempt_start` creates an `attempt N` node (type `NodeRepairAttempt`, glyph `⟳`) carrying the failed run's output; events whose prefix contains `attempt:N` attach under the same attempt node: the inline `repair` agent node, or a `rerun N` container holding the replayed steps and the replayed check (`[check, attempt:N, check]`). `repair_attempt_end` closes the attempt node and, when it passed, marks the owning check `success` with `(repaired N/M)`. This is the single authoritative shape for audit, the tree, and the fixtures in INT-005. `stepRowLabel` produces the suffixes from `repair_attempts`, `repair_blocked`, and the live frame. Auto-follow needs no new rule: attempt children are ordinary descendants, and the frontier is whichever leaf is in progress.

### Workflow migrations

All edits are to YAML and scripts under `workflows/`; the Runner does not special-case them.

1. **Archive split.** New hidden `workflows/openspec/archive-change-v1.0.yaml`:
   - `archive-transition` (script `archive-transition.sh`, `capture: archive_state`): before touching anything, snapshot the index and worktree for the three allowed path sets (`git diff --cached --name-status`, `git diff --name-status`, untracked list) and record `start_head`; if the active dir exists, `openspec validate` + `openspec archive --yes`; resolve exactly one archive dir, fail on zero or many; compute the transition-owned delta as the worktree change under the allowed paths minus the pre-existing snapshot; write JSON `{archive_dir, change_dir, start_head, owned_delta, prior_index}` to stdout. Idempotent: a second run with the active dir gone and one archive present recomputes the same JSON from the same snapshot file kept under the run's output directory.
   - `verify-archive-commit` (script `verify-archive-commit.sh`, inputs `archive_state`, `repair: {agent: implementor, prompt: ...}`): asserts the active dir is absent from worktree and index; exactly that archive dir is present; `start_head` is an ancestor of `HEAD`; the committed delta `start_head..HEAD` restricted to the allowed paths equals `owned_delta` exactly (no more, no less); the commit range touches no path outside the allowed paths; the index outside the owned delta equals `prior_index` (pre-existing staged edits, including ones inside `openspec/specs`, are neither committed nor unstaged); and the worktree under the allowed paths is clean apart from the pre-existing unstaged edits in the snapshot. Passes with no new commit when the owned delta is already in `start_head..HEAD`. The repair prompt names the exact owned paths the agent may stage, forbids staging anything else, and asks it to follow repository commit conventions and respond to hooks.
   - `advance-validator-baseline` (`command: agent-validator skip`).
   `change-v2.0`, `simple-change-v2.0`, and `simple-change-v1.0` call the sub-workflow in place of the script. `archive-change.sh` is deleted; its ticket-subject heuristic moves into the repair prompt as a hint, not a rule.
2. **`commit-change-plan.sh`** becomes replay-safe: when nothing is staged for `change_dir` and `git status --porcelain -- change_dir` is empty and `HEAD` already contains the dir, exit 0 with a "already committed" message. `core:plan-change` `commit-plan` gains `repair: {session: planning-agent, prompt: commit only {{change_dir}} following repository conventions}`.
3. **`core:plan-change` `check-plan`** gains inline repair on `planning-agent` (mechanical fixes only, no re-planning).
4. **`core:implement-task` `verify-task-commit`** gains `repair: {session: resume, max: 1, prompt: ...}` on the implementor's session. The prompt: this repository shows no commit for the task since `{{task_start_head}}`. First inspect `git status --porcelain`, `git diff --cached --stat`, and `git diff --stat`. If the task's deliverable belongs in this repository and is present in the worktree, commit only an explicit list of task-related paths (`git commit -- <paths>`), leaving every unrelated staged or unstaged file exactly as it is, following project conventions with the `[verify-task-commit]` prefix. Do not create an empty or placeholder commit. If the work was legitimately delivered elsewhere, the task conflicts with this repository, or task-related changes cannot be isolated safely from unrelated ones, make no commit; report only facts you have verified (repository, branch, commit, whether pushed), writing `unknown` for anything unverified, explain why, and end with `REPAIR_BLOCKED`. `verify-task-commit.sh` is unchanged.
5. **`core:implement-change`**: `verify-task-index` and `verify-assumptions-handoff` gain inline repair on `lead-agent`; `verify-clean-for-pr` gains inline repair on `lead-agent` with the `commit-leftovers` wording from `implement-task`; `verify-draft-pr` gains `repair: {rerun: open-draft-pr}` and `open-draft-pr`'s prompt gains the `REPAIR_BLOCKED` instruction for credential, permission, and user-decision blocks.
6. **`openspec:simple-change` `validate-openspec`** gains inline repair on `lead-agent`.

`docs/` gains a "Recoverable checks" section in the workflow authoring guide with the failure-policy table from the proposal.

## Decisions

- **Rewind signal over a shared sequencer.** Three localized edits versus a refactor of resume-sensitive code. The signal is patterned on `OutcomeAborted` propagation, which every sequencer already handles.
- **Frame on the context, mirrored into state.** Avoids threading a new parameter through every executor; the existing `FlushState` writers already copy context fields into `NestedStepState`.
- **`attempt:N` prefix token** rather than a synthetic iteration. Keeps `iteration` semantics intact for loops and lets the run view distinguish attempts without workflow definitions.
- **Reason computed once by the runner and persisted.** The run list must not parse audit logs to render a row, and headless output and the debug workflow need the same string.
- **Evidence block always appended.** Simpler contract than detecting whether the author placed `{{repair.*}}`; authors who reference the variables get both, which is harmless.
- **Guarded response looked up by prefix in the view**, not copied into the check's `step_end`. Keeps audit entries bounded and single-sourced.
- **Archive verification tolerates a pre-existing commit.** The verify script passes when the paths are already clean and committed, so a retry after a successful commit is a no-op, as the issue requires.

## Risks / Trade-offs

- **Replayed side effects.** A rerun re-executes every step from the target to the check. Mitigation: load-time restriction to the same scope, rejection of `break_if` and outcome-relative `skip_if` in the range, and the only migrated rerun target (`open-draft-pr`) is query-then-act.
- **Sequencer divergence.** Three copies of the rewind handling can drift. Mitigation: the handling is two helper calls (`takeRewind`, `AbsorbReplayFailure`) with the logic in one place, plus a table-driven test run against all three sequencers.
- **Prefix grammar change.** Older binaries reading new audit logs will treat `attempt:1` as a step ID. Acceptable pre-release; the parser change is backward compatible with old logs.
- **Marker false positives.** An agent could echo `REPAIR_BLOCKED` while quoting instructions. Mitigation: only the final non-empty line counts, and the marker only ever stops repair.
- **Inline agent session reuse on resume.** Resuming an interrupted inline repair relies on the agent session ID having been flushed. `ExecuteAgentStep` already flushes on session discovery, so the frame records the attempt and the session map records the ID.

## Testing

- `internal/model`: table tests for `Repair` validation and `validateRepairTargets`; `ResolveResumeStep` with each frame phase.
- `internal/exec`: `ExecuteCheckStep` with a fake `ProcessRunner` scripted per call (fail, pass) and a stub agent executor: inline recovery, exhaustion, blocked-before-repair, blocked-by-repair-agent, capture reflects final run, budget per loop iteration. Rewind tests for loop body and sub-workflow: replay order, intermediate failure absorbed, `previous_success` untouched.
- `internal/runner`: top-level rewind, state file contents at each phase, resume from each phase, failure reason persisted and printed.
- `internal/audit` and `internal/runview`: prefix round-trip with `attempt:N`; tree ingestion fixtures for repaired, exhausted, blocked, and in-progress rerun; selected-detail `Failure evidence` rendering; list row truncation.
- `workflows/`: real-Git tests (pattern of `verify_task_commit_test.go`) for `archive-transition.sh` first run and replay, `verify-archive-commit.sh` pass, missing-archive, ambiguous-archive, unrelated-path, and already-committed cases, and `commit-change-plan.sh` replay; YAML load tests asserting every migrated site validates.
- Smoke: a fixture workflow with an agent step that emits `REPAIR_BLOCKED` followed by a rerun check, run headless, asserting the printed reason and the resume entry point.

## Migration Plan

Additive. Workflows without `repair` behave exactly as before, apart from richer failure reasons for failed shell and script steps. State files from earlier runs have no frame and resume as today. Rollback is deleting the `repair` blocks; the archive sub-workflow can be reverted to the script independently.
