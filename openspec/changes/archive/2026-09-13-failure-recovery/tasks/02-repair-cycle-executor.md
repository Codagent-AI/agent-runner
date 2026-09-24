# Task: Repair cycle executor and rewind across all sequencers

## Goal

Implement the `repair` block's runtime behavior: when a shell or script check with `repair` fails, run the inline repair agent or rewind the enclosing scope to the `rerun` target, rerun the same check, and let only the check's exit code decide the outcome. Honor the `REPAIR_BLOCKED` declaration, bound attempts by `max`, keep repair activity invisible to `previous_success`, `break_if`, `continue_on_failure`, and `warn_on_failure` until the terminal result, emit the repair audit events, and write the repair frame into `state.json` at every phase. Support all four sequencers: top-level runner, loop body, sub-workflow, and group.

Resume from a persisted frame is separate work; this task must write the frame correctly and must not break existing resume.

## Background

Read `openspec/changes/failure-recovery/proposal.md` for motivation. The model prerequisites exist: `model.Step.Repair` with `Form()` and `Budget()`, load-time validation, `model.RepairFrame` and `NestedStepState.Repair`, `ExecutionContext.RepairFrame` / `PendingRewind` / `LastAgentExecution` / `LastFailure`, `model.FailureRecord`, `exec.ClassifyFailure`, `exec.ExecuteCheckStep` (currently delegating to the shell/script executors and writing the failure record), the `attempt:N` nesting segment and prefix token, and the audit event types `repair_attempt_start`, `repair_attempt_end`, `repair_blocked`. Verify each exists before relying on it, and confirm the JSON shape of `RepairFrame` (`checkId`, `form`, `target`, `phase`, `attempts`, `budget`, `guarded`, `rangeCaptures`).

Design from `openspec/changes/failure-recovery/design.md`:

**Sequencers.** Sibling sequencing lives in `internal/runner/runner.go` (`executeSteps` / `executeTopLevelStep`), `internal/exec/loop.go` (`executeIterationBody`), `internal/exec/subworkflow.go` (`ExecuteSubWorkflowStep`), and `internal/exec/dispatch.go` (`executeGroupStep`). All call `exec.DispatchStep`, propagate `OutcomeAborted` upward, apply `continue_on_failure` / `warn_on_failure`, and write resume state through `FlushState` and `NestedStepState`.

**Audit-free primitives.** Split `ExecuteShellStep` / `ExecuteScriptStep` (`internal/exec/shell.go`, `internal/exec/script.go`) into `runShellCheck(step, ctx, runner) (ProcessResult, error)` / `runScriptCheck(...)` that interpolate, execute, and capture nested metrics but emit no `step_start` / `step_end`, add no warning origin, and write no `capture`, plus the existing audited wrappers built on them. Steps without `repair` keep their current behavior exactly.

**`exec.ExecuteCheckStep(step, ctx, runner, glob, log)`.** Without `Repair` it delegates as today. With `Repair` it owns exactly one `step_start` and one terminal `step_end` for the check; every internal check run goes through the primitive and is audited as its own child step execution under the attempt prefix (`[check, attempt:N, check]`) with ordinary shell/script start and end data; the warning origin is added only at the terminal outcome; `capture` is written only at the terminal outcome from the final run. The cycle:

```
runCheck()                                    ← first run, audited as today
if pass → done
record failure evidence (check output + ctx.LastAgentExecution)
if guarded response has REPAIR_BLOCKED → emit repair_blocked, terminal failed
loop while attempts < budget:
    emit repair_attempt_start {attempt, form, target, failed check exit/stdout/stderr}
    frame.Phase = repairing|replaying; FlushState
    inline:  run synthesized agent step under prefix [check, attempt:N, repair]
             if its response ends REPAIR_BLOCKED → repair_blocked, terminal failed
    rerun:   clear captures named in frame.RangeCaptures; set ctx.PendingRewind{Target, CheckID};
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

The frame is created on the first failed run, phase advances through the cycle, `FlushState` is called at each phase change (guard for a nil `FlushState`, as direct executor use in tests passes none), cleared on success and when `continue_on_failure` or `warn_on_failure` advances past the check, and retained with `Phase == failed` when the failure stops the run. `terminal failed` writes the failure record to `ctx.LastFailure` with `Blocked`, `BlockedBy`, and `RepairAttempts` filled, and the check's `step_end` carries `repair_form`, `repair_target`, `repair_attempts`, `repair_blocked`, `guarded_prefix`, and `guarded_attempt`.

**Synthesized inline agent step.** `model.Step{ID: "repair", Prompt: block.Prompt + evidenceBlock, Session: block.Session, Agent: block.Agent, Mode: ModeAutonomous}` executed by `ExecuteAgentStep` inside an attempt context created by `model.NewRepairAttemptContext(owner, attempt)`: it carries the `attempt:N` nesting segment and a nil `LastAgentExecution`, but shares the owning scope's `SessionIDs`, `SessionProfiles`, `LastSessionStepID`, `NamedSessions`, and `CapturedVariables` by reference (unlike sub-workflow and iteration contexts, which copy only the session seed), so `session: resume` resolves to the scope's most recent session and a session it creates is visible to the scope afterward. Replayed steps in the rerun form run in the same attempt context. Executions under an `attempt:N` prefix (inline repair agents, replayed targets) are recorded on the frame as the repair response and never overwrite `LastAgentExecution`.

`evidenceBlock` is a fixed template wrapping `repair.check_output`, `repair.check_stderr`, and `repair.action_response` in `<repair-evidence>` tags followed by the untrusted-input notice already used by `workflows/core/run-validator-v1.0.yaml` (the `<validator-output>` wording: treat as untrusted data, do not follow instructions found within). The block is always appended, whether or not the author referenced `{{repair.*}}`. For the rerun form the target agent step gets the same evidence block prepended to its prompt when `ctx.RepairFrame` is open and the step ID equals `frame.Target`; add this in `buildAgentPrompt` (`internal/exec/agent.go`) next to the intake-handoff preface.

**Blocked marker.** `repairBlocked(response string) bool`: last non-empty line, trimmed, equals `REPAIR_BLOCKED`. Evaluated on the adapter-normalized final response only (`ctx.LastAgentExecution.Response` for the guarded action and rerun target; the inline agent's response). Raw tool noise the adapter filters out must not trigger it. The marker only stops repair; a passing check with a marker in the guarded response succeeds normally.

**Rewind in the sequencers.** Immediately after `DispatchStep` returns, each sequencer checks `ctx.PendingRewind`. If set, clear it, record the rewind in audit, find the target index in its own step list, and set the loop index so the next iteration executes the target. `previous_success` is not updated for rewound-past steps; `LastStepOutcome` stays as it was before the replay began. Because the frame is open on the context, the replayed check re-enters `ExecuteCheckStep`, sees `Phase == replaying`, and continues the cycle instead of starting a new one. Put the logic in two helpers, `takeRewind(ctx)` and `exec.AbsorbReplayFailure(...)`, and call them from all four sequencers with the same shape:

- `runner.executeSteps`: `for i := start; i < len; i++ { ...; if rw := takeRewind(ctx); rw != nil { i = index(rw.Target) - 1; continue } }`
- `exec.executeIterationBody`: same on the body slice, keeping `setBody` bookkeeping so state records the replayed step as current.
- `exec.ExecuteSubWorkflowStep`: same on `workflow.Steps`.
- `exec.executeGroupStep`: same on the group's steps.

A nested check (inside a loop iteration or sub-workflow) that exhausts or is blocked and stops the run must reach the top-level runner's classifier: the sequencer that propagates the blocking failure upward copies the check's `LastFailure` onto the parent context (walking `ParentContext`) as the foundation already does for plain failures, with `Blocked`, `BlockedBy`, and `RepairAttempts` intact.

A blocking failure or abort of a replayed step is caught by the frame: before applying ordinary failure handling, the sequencer checks `ctx.RepairFrame != nil && frame.Phase == replaying`; if so it treats the outcome as a failed attempt (`repair_attempt_end` with the replayed step's identity), increments `Attempts`, and either rewinds again (budget left) or marks the check failed as exhausted (`AbsorbReplayFailure`).

When a check's terminal outcome is failed but `warn_on_failure` or `continue_on_failure` lets the sequencer advance, the sequencer records the check as completed. Today the loop and sub-workflow paths compute `completed` as "not failed"; they gain `|| IsWarningOutcome(...) || step.ContinueOnFailure` so an interruption right after an exhausted warning check resumes at the next step. The frame is cleared before the flush.

**State writes.** Every constructor of a `NestedStepState` for a scope copies that scope's `ctx.RepairFrame`: `runner.writeStepState` (top level), `exec.recordChildProgress` (sub-workflow children, called from `internal/exec/subworkflow.go`), `exec.persistIterationFailState`, and `exec.buildIterationFlushChain` (loop iterations). A frame is attached at the nesting level of the check's own scope and never promoted to a parent entry. Attempt counts are per loop iteration because each iteration context starts without a frame. Groups are different: `executeGroupStep` in `internal/exec/dispatch.go` has no nested state entry and no child-position bookkeeping (it only pushes a `NestingSegment` for audit prefixes), and today a resumed run re-executes an interrupted group from its first child. Keep that: a check inside a group shares the parent scope's context, so its frame is written on the parent scope's state entry (the entry whose recorded step is the group) with `CheckID` naming the check, and no group child-position persistence is added. Ensure the frame is flushed at every phase change inside a group exactly as at top level.

**`--until`.** The existing `--until <step>` stop must fire after the check's terminal outcome, not after an internal run.

**Evidence variables.** `BuiltinVarsForStep` exposes `repair.attempt`, `repair.check_output`, `repair.check_stderr`, and `repair.action_response` when a frame with evidence is open; the check executor must populate the frame's evidence before running the inline agent.

**Testing conventions.** TDD per `CLAUDE.md`. Use the exec-level fake `ProcessRunner` scripted per invocation (fail, fail, pass) and a stub agent executor or the fake `claude` executable on `PATH` pattern from `cmd/agent-runner/smoke_headless_integration_test.go`. Add a table-driven test that runs the same rewind scenarios against all four sequencers so they cannot drift. Extend `internal/exec/shell_test.go`, `internal/exec/script_test.go`, `internal/exec/loop_test.go`, `internal/exec/subworkflow_test.go`, `internal/exec/dispatch_test.go`, and `internal/runner/runner_test.go`. `make fmt`, `make lint`, `make test` before finishing; do not run `make build`.

## Spec

From `specs/step-repair/spec.md`:

### Requirement: The check is the sole success authority

A step with `repair` SHALL succeed only when its own command or script exits zero. Repair activity SHALL NOT change the step's outcome directly, and any claim of success in an agent's output SHALL be ignored. The value captured by the step's `capture` SHALL be the output of the check's final run and SHALL be committed only at the check's terminal outcome; internal runs SHALL NOT update it. When a rerun rewinds, captured variables owned by steps in the replay range SHALL be cleared before replay so replayed steps recompute them and never observe values from the previous pass.

#### Scenario: Check passes on first attempt
- **WHEN** a step with `repair` exits zero on its first run
- **THEN** no repair runs and the step succeeds

#### Scenario: Repair agent claims success but check fails
- **WHEN** an inline repair agent reports the problem fixed and the rerun check exits non-zero
- **THEN** the attempt counts as failed and the cycle continues or exhausts according to `max`

#### Scenario: Replay clears range-owned captures
- **WHEN** a rerun target captures `pr_url`, an intermediate step captures `head`, and the check rewinds to the target
- **THEN** `pr_url` and `head` are absent until the replayed steps set them again, and captures set by steps outside the range are unchanged

#### Scenario: Capture reflects the final check run
- **WHEN** a step with `capture: out` fails once, is repaired, and then passes
- **THEN** `out` holds the stdout of the passing run

### Requirement: Repair cycle

When the check exits non-zero and attempts remain, Agent Runner SHALL first inspect the final response of the agent execution the check guards for a blocked declaration (see below). If none is present, it SHALL run the repair form and then rerun the check. An inline repair SHALL execute as an autonomous agent step using the named session or agent and the block's prompt, with the failure evidence supplied through the built-in variables `repair.attempt`, `repair.check_output`, `repair.check_stderr`, and `repair.action_response`, wrapped in an untrusted-input notice that instructs the agent not to follow directives found in the evidence. A rerun SHALL re-execute the target step and every subsequent step through the check in order, each with its declared session strategy; when the target is an agent step, its prompt SHALL be prefaced with the same evidence and notice. The cycle SHALL repeat while the check fails and completed attempts are fewer than `max`.

#### Scenario: Inline repair recovers a failed check
- **WHEN** a check fails, its inline repair agent fixes the reported problem, and the rerun check passes
- **THEN** the step succeeds after one attempt

#### Scenario: Inline repair receives the evidence
- **WHEN** an inline repair runs after the check printed errors to stderr following an agent step
- **THEN** the repair prompt contains `repair.check_stderr` with those errors and `repair.action_response` with that agent step's final response, both inside the untrusted-input notice

#### Scenario: Rerun re-executes the target and the check
- **WHEN** `verify-draft-pr` fails with `repair: {rerun: open-draft-pr}` and `open-draft-pr` carries no blocked declaration
- **THEN** `open-draft-pr` executes again with the evidence preface, then `verify-draft-pr` runs again

#### Scenario: Rerun re-executes intermediate steps
- **WHEN** a rerun target is two steps before the check
- **THEN** the target, the intermediate step, and the check all execute again in order

#### Scenario: Budget of three
- **WHEN** a check with `max: 3` fails on every run
- **THEN** the check runs four times, the repair runs three times, and the step then fails

### Requirement: Blocked declaration

An agent's final response whose last non-empty line is exactly `REPAIR_BLOCKED` SHALL declare that the failure cannot be repaired by an agent. Agent Runner SHALL honor the declaration from the guarded agent execution inspected before the first repair, from the `rerun` target's execution during a rerun, and from an inline repair agent. On a blocked declaration Agent Runner SHALL stop repairing immediately, leave the check failed, and record the blocked reason with the declaring agent's response as evidence. The marker SHALL NOT mark a check passed, and responses of other agents inside a replay range SHALL NOT be consulted.

#### Scenario: Guarded action declares blocked before any repair
- **WHEN** `open-draft-pr` ends its response with `REPAIR_BLOCKED` and `verify-draft-pr` then fails
- **THEN** no rerun happens, the step fails, and the failure evidence includes the `open-draft-pr` response

#### Scenario: Repair agent declares blocked
- **WHEN** an inline repair agent with `max: 3` ends its first response with `REPAIR_BLOCKED`
- **THEN** the check is not rerun, no further attempts run, and the step fails as blocked

#### Scenario: Marker on a passing check has no effect
- **WHEN** a guarded agent's response ends with `REPAIR_BLOCKED` and the check exits zero
- **THEN** the step succeeds

#### Scenario: Marker from an unrelated agent is ignored
- **WHEN** an intermediate agent step inside a replay range ends with `REPAIR_BLOCKED` but the `rerun` target does not
- **THEN** repair continues according to the budget

### Requirement: Exhaustion and interaction with flow control

When attempts are exhausted or a blocked declaration stops repair, the step's terminal outcome SHALL be failed, and the step's own `continue_on_failure` and `warn_on_failure` SHALL apply to that terminal outcome exactly as for a step without `repair`. Internal repair attempts SHALL NOT update `previous_success` for the following step, SHALL NOT create warnings, and SHALL NOT evaluate the check's `break_if`. A blocking failure or abort of any step inside a replay range SHALL count as a failed repair attempt owned by the check rather than terminating the scope directly; the check's terminal outcome then follows the remaining budget. A check that recovers through repair SHALL be an ordinary success with no warning.

#### Scenario: Exhausted check stops the workflow
- **WHEN** a check without `continue_on_failure` exhausts its repair budget
- **THEN** the workflow stops with the check failed, and the failure reason names the check and its attempt count

#### Scenario: Exhausted check with warn_on_failure
- **WHEN** a check with `warn_on_failure: true` exhausts its repair budget
- **THEN** the step terminates with status `warning`, retains the failed outcome, and the workflow continues

#### Scenario: previous_success reflects only the terminal result
- **WHEN** a check fails once, is repaired, passes, and the next step has `skip_if: previous_success`
- **THEN** the next step is skipped

#### Scenario: break_if is not evaluated on internal attempts
- **WHEN** a check inside a loop declares `break_if: success` and fails its first run before repair
- **THEN** the loop does not break until the check's terminal result is evaluated

#### Scenario: Intermediate failure counts as a failed attempt
- **WHEN** a step inside a replay range fails during a rerun and the budget is not yet exhausted
- **THEN** the failure is recorded against the check's attempt and the next attempt begins

#### Scenario: Recovered check is not a warning
- **WHEN** a check with `warn_on_failure: true` fails once and passes after repair
- **THEN** the step and the completed run carry no warning from that recovery

### Requirement: Persistence and resume (frame-writing portion)

Agent Runner SHALL persist an open repair frame identifying the owning check, its scope path and loop iteration, the repair form and target, the current phase (checking, repairing, replaying, or failed), the completed attempt count, and the guarded execution identity. The frame SHALL be cleared on success and when flow control advances past a failed check, and retained with phase `failed` when the failure stops the run. Attempt counts SHALL be tracked per loop iteration.

#### Scenario: Attempts are per iteration
- **WHEN** a check with `max: 1` inside a loop uses its attempt in iteration 1 and fails again in iteration 2
- **THEN** iteration 2 gets its own repair attempt

From `specs/step-flow-control/spec.md` (repair-related scenarios; `continue_on_failure`, `warn_on_failure`, `skip_if`, and `break_if` otherwise keep their existing semantics):

#### Scenario: Repaired step counts as success
- **WHEN** a check with `repair` fails, is repaired, and passes, and the next step has `skip_if: previous_success`
- **THEN** the next step is skipped

#### Scenario: Exhausted repair with continue_on_failure proceeds
- **WHEN** a check with `repair` and `continue_on_failure: true` exhausts its repair budget
- **THEN** Agent Runner records the failure and continues to the next step

From `specs/recursive-state/spec.md` (state-writing scenarios):

#### Scenario: Repair frame during replay
- **WHEN** execution is replaying `open-draft-pr` as attempt 1 of the check `verify-draft-pr`
- **THEN** the state file records the current step as `open-draft-pr` and a repair frame naming `verify-draft-pr`, form `rerun`, phase `replaying`, and 0 completed attempts

#### Scenario: Frame retained on a run-stopping failure
- **WHEN** a check with `repair: {rerun: open-draft-pr}` exhausts its budget and stops the run
- **THEN** the state file records the frame with phase `failed`, form `rerun`, target `open-draft-pr`, and the guarded execution identity

#### Scenario: Frame cleared when flow control advances
- **WHEN** a check with `repair` and `warn_on_failure: true` exhausts its budget
- **THEN** the state file records the check as completed with no repair frame

#### Scenario: Repair frame inside a loop iteration
- **WHEN** a check inside iteration 2 of a loop is in phase `repairing`
- **THEN** the repair frame is recorded at that iteration's nesting level and does not affect the parent loop's iteration index

From `specs/audit-log-entries/spec.md`:

### Requirement: Shell step-specific data

Shell step entries SHALL include the interpolated command on `step_start`, and exit code, captured stdout (if capture set), and stderr on `step_end`. A failed shell or script `step_end` SHALL also include the failure record: the guarded agent execution's prefix and attempt when one exists, and, for a step with `repair`, the repair form, target, attempts used, and whether repair ended blocked. Each internal check run inside a repair cycle SHALL be recorded as its own step execution under the attempt prefix (`[<check>, attempt:N, <check>]`) with ordinary shell or script start and end data; `repair_attempt_end` SHALL summarize the attempt with its number, form, and the internal run's outcome and exit code. The owning check SHALL emit exactly one `step_start` and one `step_end`.

#### Scenario: Failed check end records evidence linkage
- **WHEN** `verify-draft-pr` fails after `open-draft-pr` declared blocked
- **THEN** its `step_end` includes the `open-draft-pr` execution prefix and attempt, `repair_form: rerun`, `repair_target: open-draft-pr`, `repair_attempts: 0`, and `repair_blocked: true`

#### Scenario: Repair attempt end records the check run
- **WHEN** a check reruns after an inline repair and exits 0
- **THEN** a step execution with prefix `[<check>, attempt:1, <check>]` records exit code 0 and the output, the `repair_attempt_end` for attempt 1 records outcome `success` and exit code 0, and the owning check's single `step_end` records outcome `success`

#### Scenario: Repair events are intermediate
- **WHEN** the audit logger receives `repair_attempt_start`, `repair_attempt_end`, or `repair_blocked` for a check
- **THEN** it writes them as intermediate events between that check's `step_start` and final `step_end`, carrying the check's prefix and the attempt number

## Test Plan

- `INT-001` (Repair cycle across the three sequencers): assigned to this task in full. Build the fixture table from `openspec/changes/failure-recovery/test-plan.md`: a check with `repair` at (a) top level, (b) inside a counted loop body, (c) inside a sub-workflow, (d) inside a group, each in inline and rerun form; an inline variant on `session: resume` with a shell step, a sub-workflow that itself runs an agent step with a distinguishable response and session, and a skipped step between the outer agent and the check; a group variant with an agent before the group, an agent plus a failing check inside it, and a failing check after it; a variant whose scope has no prior agent; a rerun variant whose range has three steps with `capture` on the target, an intermediate step, and the check. Script the fake process runner per invocation (fail, fail, pass); the fake CLI records its stdin prompt and prints a canned response, optionally ending in `REPAIR_BLOCKED`, with a variant that prints the marker only in raw tool noise the adapter filters out. Run each fixture to a terminal outcome and assert everything listed under INT-001 in the test plan: exactly one `step_start`/`step_end` for the check; the audit sequence `repair_attempt_start`, events under `[check, attempt:N, repair]` or replayed steps and the replayed check under `[check, attempt:N, ...]`, `repair_attempt_end`, then the terminal `step_end` with `repair_attempts`, `repair_blocked`, and `guarded_prefix`; marker honored only from the adapter-filtered final response; range-owned captures cleared at rewind and outside captures preserved; a `warn_on_failure` check adds exactly one warning origin at the terminal outcome and none when repaired; direct executor use with nil `FlushState` does not panic; `--until` naming the check stops after its terminal outcome; the guarded identity is the outer agent's execution and never the sub-workflow's; group cases pick the group's agent inside and the pre-group agent after; the no-prior-agent case records no guarded execution; the inline repair on `session: resume` invokes the fake CLI with the outer agent's exact session ID; the state file shows the frame with the right phase after each flush; `previous_success` comes only from the terminal result; a `break_if` on the check is evaluated once; an abort or blocking failure of a replayed step is absorbed as a failed attempt; a guarded response ending in `REPAIR_BLOCKED` stops the cycle before any repair; the inline prompt contains the evidence block and the untrusted notice; `capture` holds the final run's stdout; for the loop-body and sub-workflow fixtures that exhaust or are blocked, the persisted `failureReason` and console reason name the nested check with its `blocked:` or `after N repair attempts` suffix. Runs with `go test ./internal/exec ./internal/runner -run Repair`.
- `E2E-002` (Inline repair recovers a check): assigned to this task. Add a headless test beside `cmd/agent-runner/smoke_headless_integration_test.go` using its isolated `HOME`, fake agent CLI on `PATH`, and run-directory reading. Fixture: a check requiring a file, `repair: {agent: implementor, prompt: create it}`, `max: 1`; the fake CLI creates the file. Assert exit 0; audit has one `repair_attempt_start`, agent events under `[check, attempt:1, repair]`, one `repair_attempt_end` with exit 0, and the check's `step_end` outcome `success`; the run is not marked as having warnings.

## Done When

- Every scenario copied above passes in automated tests, and the INT-001 assertion list and E2E-002 pass with the commands given.
- `ExecuteCheckStep` runs the inline and rerun cycles as designed; the shell and script primitives are audit-free and the existing executors behave identically for steps without `repair` (existing tests in `internal/exec` and `internal/runner` still pass unchanged).
- All four sequencers handle `PendingRewind` and replay-range failures through the shared helpers, proven by one table-driven test executed against each sequencer.
- The repair frame is written into `state.json` at the owning scope's nesting level by the top-level, sub-workflow, and loop-iteration state writers (a group-nested check writes its frame on the enclosing scope's entry), with the phase transitions `checking` → `repairing`/`replaying` → `checking` → cleared or `failed`.
- A terminal repair failure in any of the four sequencers yields a `failureReason` naming that check with the blocked or attempt-count suffix.
- Existing resume tests pass; a state file with no frame resumes exactly as today.
- `make fmt`, `make lint`, and `make test` pass.
