# Task: Failure records, guarded-execution tracking, and the repair model

## Goal

Make every failed shell or script check explain itself: record a structured failure record that names the guarded agent execution and its final response, derive one classified failure reason from it, persist that reason in `state.json`, print it above the resume hint, and put it in `run_end`. At the same time land the `repair` model types, load-time validation, the `attempt:N` prefix token, and the three new audit event types so the repair executor built on top of this has a stable contract.

Nothing in this task executes a repair. Workflows without `repair` behave as before apart from the richer failure reason.

## Background

Read `openspec/changes/failure-recovery/proposal.md` for motivation (GitHub issue 65). Relevant design decisions from `openspec/changes/failure-recovery/design.md`:

**Model (`internal/model/step.go`).** `model.Step` gains `Repair *Repair`:

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

`Step.Validate` enforces the field-level rules (shell/script only, exactly one form, inline names exactly one of `session`/`agent`, `max >= 1`). The inline `session` value must be `resume`, `inherit`, or a named session reference (`model.IsNamedSession`); reject `session: new` with an error saying a fresh session must use `agent` instead. Do not check named sessions against `Workflow.Sessions` at load time: sub-workflows legitimately reference sessions declared by their caller and today's steps resolve names at runtime, so a repair block follows the same rule. Scope-level rules (target is an earlier sibling in the same sequential list, replay range free of `break_if` and `skip_if: previous_success`) live in a new `validateRepairTargets(steps []Step)` called from `Workflow.Validate` for the top-level list and recursively for every `steps:` body (loop bodies, groups), since only the containing list knows sibling order. Check how `Workflow.Validate` currently recurses so the new validation follows the same path; the loader in `internal/loader/` must surface these errors at load time.

**Prefix token.** `model.NestingSegment` gains `RepairAttempt *int`. `audit.BuildPrefix` (in `internal/audit/`) renders it as a token `attempt:N` placed after the owning check's token, so a replayed step's prefix is `[verify-draft-pr, attempt:1, open-draft-pr]` and an inline repair agent's is `[check-plan, attempt:1, repair]`. `runview.parsePrefix` in `internal/runview/audit.go` learns `attempt:` alongside the existing `sub:` and `call:` tokens (it must round-trip and remain backward compatible with old logs).

**Audit event types (`internal/audit/types.go`).** Add `repair_attempt_start`, `repair_attempt_end`, and `repair_blocked`. They are intermediate events between a check's `step_start` and `step_end`, carrying the check's prefix and the attempt number. Only the types and their validation land here; emission is done by the repair executor later.

**Guarded execution tracking (`internal/model/context.go`).** `model.ExecutionContext` gains `LastAgentExecution *AgentExecutionRecord` (`Ref ExecutionRef{Prefix string, Attempt int}`, `Response string`, `CallResponses []CallResponse{CallID, Response}`). It is published once, when an ordinary agent step completes (`internal/exec/agent.go`), and left in place across later shell, script, sub-workflow, and skipped steps. Agent calls complete while the parent agent step is still running, before the record exists, so do not append them to `LastAgentExecution`: keep an in-flight accumulator on the step's `AgentCallHandler` (`internal/exec/agent_call.go`, which already knows the parent prefix and emits `agent_call_end` with the child's filtered response) keyed by the parent's prefix and attempt, and have the agent executor read the accumulated `CallResponses` from the handler when it builds and publishes the record at completion. A call made by agent step B must never attach to the record of the earlier agent step A. Scope isolation: `NewSubWorkflowContext` and `NewLoopIterationContext` start with `LastAgentExecution == nil`. Groups (`executeGroupStep` in `internal/exec/dispatch.go`) reuse the parent context, so save the record before the group body and restore it after; a check inside the group sees only agents that ran earlier in the group.

The attempt number must be the same one stamped on the audit `step_end` `identity.attempt`. That number comes from the metrics collector's per-prefix counter; expose it (e.g. `metrics.Collector.AttemptFor(identity)`) and read it back in the agent end-emission path so context, state, and audit agree.

**Persistence of the guarded identity.** `model.NestedStepState` gains `LastAgent *ExecutionRef` (JSON `lastAgent`), written by every state-chain constructor after each completed agent step: `runner.writeStepState` (top level, `internal/runner/runner.go`), `exec.recordChildProgress` (sub-workflow children; note it is called only from `internal/exec/subworkflow.go` and the loop flush chain, never for groups), and the loop iteration state writers in `internal/exec/loop.go` (`persistIterationFailState`, `buildIterationFlushChain`). On resume, `restoreResumeContext` in `internal/runner/resume.go` and the loop/sub-workflow resume paths restore the reference to the owning scope's context. When a check needs the response and only the reference is present, `exec.LoadAgentExecution(sessionDir, ref)` rebuilds it from the audit log by exact prefix and attempt (the same lookup the run view performs on `step_end` data; the agent's filtered response is in the event's `stdout`, and agent-call responses are in `agent_call_end` events under `call:` prefixes).

**Propagation to the deciding sequencer.** Loop iterations and sub-workflows execute in child contexts (`NewLoopIterationContext`, `NewSubWorkflowContext` in `internal/model/context.go`, each with `ParentContext` set), but the run's terminal failure is decided by the top-level runner. When a nested check's failure stops its scope and propagates upward as a blocking failure, copy that check's `LastFailure` onto each parent context (walk `ParentContext`) before the parent applies its own failure handling, so the top-level runner classifies the actual failing check rather than a container. Clear the parent's inherited record when the nested scope recovers or continues through `continue_on_failure` / `warn_on_failure`, so a stale record is never classified. Groups share the parent context and need no propagation.

**Failure record.** `model.FailureRecord{StepID, Prefix string, Attempt, ExitCode int, Stdout, Stderr string, Guarded *AgentExecutionRecord, Blocked bool, BlockedBy string, RepairAttempts int}`. Introduce `exec.ExecuteCheckStep(step, ctx, runner, glob, log)` and route shell and script steps through it from `DispatchStep` in `internal/exec/dispatch.go`. In this task it delegates to `ExecuteShellStep` / `ExecuteScriptStep` and, on non-zero exit, builds the record from the process result plus `ctx.LastAgentExecution` (loading from audit via the persisted reference when the in-memory record is absent) and stores it on `ctx.LastFailure *FailureRecord`. The failed `step_end` of a shell or script step gains `guarded_prefix` and `guarded_attempt` when a guarded execution exists (see `emitShellEnd`/`emitScriptEnd` paths in `internal/exec/shell.go` and `internal/exec/script.go`). Do not copy the guarded response into the check's event; the view looks it up by prefix.

**Classified reason.** `exec.ClassifyFailure(record) string` renders: step ID, `failed:`, then the first non-empty line of stderr (or `with exit code N` when stderr is empty), then `; blocked: <first line of the declaring response's explanation>` when `Blocked`, then `after N repair attempts` when `RepairAttempts > 0`. Follow the exact strings in the `run-failure-evidence` spec below. The sequencer that decides a failure is terminal (the top-level runner) calls it and writes `RunState.FailureReason` (JSON `failureReason`) in the final state write, prints it above the existing `to resume:` hint, and adds `failure_reason` to the `run_end` audit event. `runview.failureReason` (`internal/runview/failure.go`) returns `state.FailureReason` when present and otherwise falls back to today's derivation, so the debug workflow (`FailureReasonForSession`) and the run view pick up the richer string. Blocked and repair-attempt inputs to `ClassifyFailure` are produced by the repair executor in later work; here they are always false/zero, but the renderer must handle them and be unit-tested for every branch.

**Built-in variables.** `BuiltinVarsForStep` in `internal/model/context.go` exposes `repair.attempt`, `repair.check_output`, `repair.check_stderr`, and `repair.action_response` when the context carries an open repair frame with evidence. Add the `RepairFrame` type to `internal/model/state.go` now so the variables and state shape are defined once:

```go
type RepairFrame struct {
    CheckID       string        `json:"checkId"`
    Form          string        `json:"form"`             // inline | rerun
    Target        string        `json:"target,omitempty"`
    Phase         string        `json:"phase"`            // checking | repairing | replaying | failed
    Attempts      int           `json:"attempts"`
    Budget        int           `json:"budget"`
    Guarded       *ExecutionRef `json:"guarded,omitempty"`
    RangeCaptures []string      `json:"rangeCaptures,omitempty"`
}
```

`NestedStepState` gains `Repair *RepairFrame` (JSON `repair`) and `ExecutionContext` gains `RepairFrame *RepairFrame` and `PendingRewind *RewindRequest{Target, CheckID string}`. Only the types and JSON round-trip are required here; lifecycle is later work. `RangeCaptures` is computed at load time from the `capture` fields of the steps in the replay range (target through the step before the check) and stored where the check executor can reach it (for example on the validated `Repair` value).

**Testing conventions.** TDD per `CLAUDE.md`: failing test first, tests next to the package, `google/go-cmp` for structured comparison, no mocking framework. Extend the nearest existing test file (`internal/model/step_test.go`, `internal/model/state_test.go`, `internal/model/context_test.go`, `internal/audit/logger_test.go`, `internal/runview/audit_test.go`, `internal/exec/shell_test.go`, `internal/exec/dispatch_test.go`, `internal/runner/runner_test.go`). Format with `make fmt`; run `make lint` and `make test` before finishing. Do not run `make build`.

## Spec

From `specs/step-repair/spec.md`:

### Requirement: Repair block shape

A shell or script step MAY declare a `repair` block. The block SHALL contain exactly one repair form: an inline form with a `prompt` and exactly one of `session` (a declared named session, `resume`, or `inherit`) or `agent` (a profile name), or a rerun form with `rerun` naming a step ID. The block MAY set `max`, a positive integer defaulting to 1, bounding repair attempts per execution of the check. Load-time validation SHALL reject: `repair` on a step that is not a shell or script step; a block with both forms or neither; an inline block naming neither `session` nor `agent`, or both; `max` less than 1; a `rerun` target that is not an earlier step in the same sequential scope as the check; and a replay range (the `rerun` target through the step before the check) containing a step with `break_if` or `skip_if: previous_success`.

#### Scenario: Inline repair on a script step validates
- **WHEN** a script step declares `repair` with `session: planning-agent`, a `prompt`, and no `max`
- **THEN** the workflow loads and the step has a repair budget of 1

#### Scenario: Rerun repair on a shell step validates
- **WHEN** a shell step `verify-draft-pr` declares `repair: {rerun: open-draft-pr}` and `open-draft-pr` is an earlier sibling in the same scope
- **THEN** the workflow loads

#### Scenario: Repair on an agent step is rejected
- **WHEN** an agent step declares `repair`
- **THEN** loading fails with an error stating `repair` is only allowed on shell and script steps

#### Scenario: Both forms rejected
- **WHEN** a `repair` block sets both `rerun` and `prompt`
- **THEN** loading fails with an error stating exactly one repair form is allowed

#### Scenario: Inline block without a target is rejected
- **WHEN** a `repair` block has a `prompt` but neither `session` nor `agent`
- **THEN** loading fails with an error stating an inline repair must name `session` or `agent`

#### Scenario: Rerun target outside the scope is rejected
- **WHEN** a check inside a loop body declares `rerun` naming a step outside that loop body, a later step, or the check itself
- **THEN** loading fails with an error identifying the invalid target

#### Scenario: Replay range with break_if is rejected
- **WHEN** a step between the `rerun` target and the check declares `break_if`
- **THEN** loading fails with an error naming that step and stating replay ranges cannot contain `break_if` or outcome-relative `skip_if`

From `specs/run-failure-evidence/spec.md`:

### Requirement: Failure record for every failed check

When a shell or script step exits non-zero, Agent Runner SHALL record a failure record for that execution containing the step ID, exit code, stdout, stderr, and attempt number. When the same sequential scope contains an earlier agent step execution, the record SHALL also identify the most recent such execution (the guarded execution), regardless of how many shell, script, sub-workflow, or skipped steps lie between it and the check, and SHALL contain its identity (audit prefix and attempt), its final response, and the final responses of its recorded agent calls. Skipped steps and steps in other scopes are never the guarded execution. The guarded execution SHALL be identified by execution identity, never by position, and that identity SHALL be persisted with the check's state so the record can be rebuilt from audit evidence after an interruption between the guarded execution and the check. Responses of repair agents and rerun targets SHALL be recorded separately from the guarded execution and SHALL NOT replace it. The record SHALL be written whether or not the step declares `repair`.

#### Scenario: Failed check after an agent step
- **WHEN** `verify-draft-pr` exits 1 immediately after the agent step `open-draft-pr`
- **THEN** the failure record contains the check's stderr and exit code and `open-draft-pr`'s final response identified by its audit prefix

#### Scenario: Failed check several steps after the agent
- **WHEN** `verify-task-commit` exits 1 after `generate-code` (agent), `run-validator` (sub-workflow), `check-clean` (shell), and a skipped `commit-leftovers-if-needed`
- **THEN** the failure record identifies `generate-code` as the guarded execution and contains its final response

#### Scenario: Group boundary
- **WHEN** an agent step runs, then a group containing another agent step and a failing check runs, then a check after the group fails
- **THEN** the check inside the group identifies the group's agent as guarded, and the check after the group identifies the agent that ran before the group

#### Scenario: No agent step in scope
- **WHEN** a check exits non-zero and no agent step executed earlier in the same scope
- **THEN** the failure record contains the check's output and exit code and no guarded agent response

#### Scenario: Interrupted between action and check
- **WHEN** the run is interrupted after `open-draft-pr` completes and before `verify-draft-pr` runs, then resumed, and the check fails
- **THEN** the failure record contains `open-draft-pr`'s final response rebuilt from audit by its persisted identity

#### Scenario: Guarded agent inside a sub-workflow
- **WHEN** a check inside a sub-workflow fails after the preceding sibling agent step in that sub-workflow
- **THEN** the failure record identifies that agent execution by its full nested prefix

#### Scenario: Guarded agent used agent calls
- **WHEN** the preceding agent step made two `call_agent` calls and the check then fails
- **THEN** the failure record contains the agent step's final response and both children's final responses, each labeled with its call identity

### Requirement: Classified failure reason

The run's root failure reason SHALL be derived from the failure record of the failing check: the step ID, then the first non-empty line of stderr (or the exit code when stderr is empty), then `blocked: <first line of the declaring response's explanation>` when a blocked declaration was recorded, then `after N repair attempts` when at least one repair attempt ran. The same reason SHALL appear in the failure surface of the run view, the run list row, the debug workflow's failure reason, and the resume hint printed when a run fails.

#### Scenario: Blocked failure reason
- **WHEN** `verify-draft-pr` fails after `open-draft-pr` declared `REPAIR_BLOCKED` with a first line of `push rejected: token lacks workflow scope`
- **THEN** the failure reason is `verify-draft-pr failed: expected exactly one open pull request for branch 'dev', found 0; blocked: push rejected: token lacks workflow scope`

#### Scenario: Exhausted failure reason
- **WHEN** `check-plan` fails after two repair attempts
- **THEN** the failure reason ends with `after 2 repair attempts`

#### Scenario: Plain failure reason unchanged
- **WHEN** a check without `repair` fails and no agent preceded it
- **THEN** the failure reason is the step ID and its first stderr line, as today

#### Scenario: Resume hint carries the reason
- **WHEN** a headless run fails at a check
- **THEN** the console prints the classified failure reason above the `to resume:` hint

### Requirement: Evidence persistence and reconstruction

Failure records SHALL be persisted in the run directory and in audit evidence so that the run view, run list, and debug workflow can reconstruct the failure reason and evidence for a historical run without the workflow definition. Resume SHALL retain earlier failure records.

#### Scenario: Historical run shows evidence
- **WHEN** a failed run is opened in the run view after the workflow file has changed
- **THEN** the failing check's failure evidence, including the guarded agent's response, is displayed

#### Scenario: Evidence survives resume
- **WHEN** a run fails at a check, is resumed, and later completes
- **THEN** the earlier failure record remains inspectable for that attempt

(This task delivers the persistence side of the last requirement: audit is append-only, so the failed `step_end` with `guarded_prefix`/`guarded_attempt` plus the guarded execution's own events are the durable record, and `state.json` carries `failureReason`. Rendering in the run view is separate work.)

From `specs/audit-log-entries/spec.md`:

### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `agent_call_start`, `agent_call_end`, `error`, `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, `child_continued`, `repair_attempt_start`, `repair_attempt_end`, and `repair_blocked`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Repair events are intermediate
- **WHEN** the audit logger receives `repair_attempt_start`, `repair_attempt_end`, or `repair_blocked` for a check
- **THEN** it writes them as intermediate events between that check's `step_start` and final `step_end`, carrying the check's prefix and the attempt number

### Requirement: Shell step-specific data (guarded-linkage portion)

A failed shell or script `step_end` SHALL also include the failure record: the guarded agent execution's prefix and attempt when one exists, and, for a step with `repair`, the repair form, target, attempts used, and whether repair ended blocked.

## Test Plan

No `INT-*` or `E2E-*` obligation is assigned to this task. The test plan's unit-level expectations that belong here: `Repair` validation and scope rules, `ClassifyFailure` string rendering for every branch, prefix round-trips with the `attempt:N` token, guarded-execution tracking across intervening steps and across sub-workflow and group boundaries, `LastAgent` persisted in `NestedStepState` at every nesting level, and `LoadAgentExecution` rebuilding a record (response and call responses) from an audit log by prefix and attempt. Cover the "interrupted between action and check" scenario at the runner level with a state file that carries `lastAgent` and an audit log containing the agent's `step_end`.

## Done When

- Every scenario above passes in automated tests: `Repair` validation (field-level in `Step.Validate`, scope-level in `validateRepairTargets`, exercised through the loader for top-level, loop-body, and group lists); `ClassifyFailure` for plain, blocked, and repair-attempt forms; `attempt:N` prefix build and parse round-trip with old-format prefixes unchanged; `session: new` in an inline repair block is rejected at load time.
- A check failing inside a loop iteration or a sub-workflow (and a loop → sub-workflow nesting) that stops the run produces a `failureReason` naming that nested check, in `state.json`, on the console, and in `run_end`; a nested failure absorbed by `continue_on_failure` leaves no stale record on the parent.
- The audit logger accepts the three new event types and rejects nothing it accepted before.
- A failed shell or script step, with or without `repair`, writes a `FailureRecord` to the context and its `step_end` carries `guarded_prefix` and `guarded_attempt` when an agent ran earlier in the same scope, including through intervening shell, script, sub-workflow, and skipped steps; sub-workflow and loop-iteration contexts start with no guarded execution; groups save and restore the parent's record.
- The guarded identity's attempt number equals `identity.attempt` on that agent's audit `step_end`, and agent-call responses are attached to the record with their call IDs; a test with agent A, then agent B making two calls, then a failing check proves both call responses attach to B and none to A, and that `LoadAgentExecution` rebuilds both from audit.
- `NestedStepState.LastAgent` is written by the top-level, sub-workflow/group, and loop-iteration state writers and restored on resume; `LoadAgentExecution` rebuilds the response from audit when only the reference is present. Groups (`executeGroupStep`) have no nested state entry today and run inside their parent's context, so `LastAgent` for an agent inside a group is written on the parent scope's entry like any other step in that scope; do not add group child-position persistence.
- A failed run writes `failureReason` to `state.json`, prints the same string above `to resume:` on the console, and records `failure_reason` in `run_end`; `runview.FailureReasonForSession` returns it for that run and falls back to the existing derivation for runs without it.
- `RepairFrame`, `NestedStepState.Repair`, `ExecutionContext.RepairFrame`, `ExecutionContext.PendingRewind`, and the `repair.*` built-in variables exist with JSON round-trip tests, with `RangeCaptures` computed at load time.
- `make fmt`, `make lint`, and `make test` pass.
