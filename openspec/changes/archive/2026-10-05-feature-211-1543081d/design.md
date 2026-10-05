## Context

Agent Runner collapses every agent-step failure into `OutcomeFailed` (`internal/exec/interfaces.go`). The
evidence to tell a crash apart already exists at the point of failure:

- `runAgentProcess` (`internal/exec/agent.go`) distinguishes these headless failures:
  - a process error (`runErr`), which can be a launch failure, a context cancellation, or `exec.ErrWaitDelay`;
  - a non-zero exit after `HeadlessResultFilter`, where a signal kill reports exit code `-1`;
  - an exit-0 session judged failed for a disallowed `AskUserQuestion`.
- For interactive sessions, `runAgentProcess` distinguishes three cases:
  - suspend-hook and direct-run errors, and durability failures;
  - completion;
  - an exit without a continue trigger, which returns `aborted`.
- `InvokeAgent` (`internal/exec/invocation.go`) adds a Cursor result-stall cancellation (`cli.ErrCursorResultStall`).
- `ExecuteAgentStep` fails some steps before the invocation starts: profile resolution, prompt building, mode and argument checks.
  - It fails others after the agent start event: control-channel binding and agent-call runtime setup.
  - It also turns a finished session that still has uncollected agent calls into a failure.

Outcomes flow through `(StepOutcome, error)` returns. The callers are `DispatchStep` (`dispatch.go`), the
group executor (`executeGroupStep`), the loop executor (`loop.go`), the sub-workflow executor
(`subworkflow.go`, `finishChildStep`), and the top-level runner (`internal/runner/runner.go`,
`runAndPersistTopLevelStep` / `handleFailedTopLevelStep`). There is an existing side-channel pattern for
failure evidence: `ctx.LastFailure` (a `model.FailureRecord`, written only by shell and script checks) is
copied up by `PropagateFailure` on blocking failures, cleared by `ClearInheritedFailure` when absorbed, and
classified by `classifyRunFailure` → `exec.ClassifyFailure` into `RunState.FailureReason` and `run_end`.

Groups share the parent `ExecutionContext`. Sub-workflows, loop iterations, and repair attempts get child
contexts through `NewSubWorkflowContext`, `NewLoopIterationContext`, and `NewRepairAttemptContext`. Run-scoped registries
such as `WarningOrigins` are shared by reference across the tree. Audit prefixes come from
`audit.BuildPrefix`, for example `[impl, sub:implement-task, generate-code]`, `[loop:2, step]`, and
`[check, attempt:1, repair]`.

The behavior is fixed by the specs in this change: `infrastructure-failure-classification`,
`step-flow-control`, `sub-workflows`, `builtin-vars`, `step-repair`, `audit-log-entries`, and
`run-failure-evidence`.

## Goals / Non-Goals

**Goals:**
- Classify crashed agent sessions at the source, without parsing output text.
- Carry a terminating **failure kind** (`infrastructure` | `step`) and a sticky **crash-observed** signal
  through groups, loops, sub-workflows, repair, and the top-level run.
- Expose both signals to the next step via built-ins, on end events, in `state.json`, and in the classified
  failure reason.
- Keep every existing outcome value and flow-control rule, the exit status, and the existing audit fields
  unchanged.

**Non-Goals:**
- No new `StepOutcome` value, no new exit codes, and no change to `continue_on_failure`.
- No retry or recovery of crashes inside Agent Runner.
- No classification of `call_agent` child crashes, or of shell or script failures, as infrastructure.
- No TUI changes beyond the reason string that already renders.

## Approach

### 1. Classify at the invocation boundary

Add a `Crashed bool` field to `AgentInvocationResult`, plus a `CrashError string` holding the runner-side
error message. `runAgentProcess` returns a crash flag with its outcome:

| Path | Crashed |
|---|---|
| headless `runErr` with context cause `ErrCursorResultStall` | yes |
| headless `runErr` that is `context.Canceled` / `DeadlineExceeded` with no stall cause (run being stopped) | no |
| headless `runErr` otherwise (start failure, `ErrWaitDelay` without completed output, wait error) | yes |
| headless `ExitCode != 0` after `HeadlessResultFilter` (includes `-1` signal kills) | yes |
| headless exit 0 + disallowed `AskUserQuestion` | no |
| interactive suspend-hook error, direct-run error (not context cancel), durability failure | yes |
| interactive completed / exited without trigger (`aborted`) | no |

`InvokeAgent` applies the stall override that it already performs. `agent_call.go` uses the same core and
ignores the new fields, so child crashes are never recorded.

`ExecuteAgentStep` applies the step-level rules:

- **Not a crash, kind `step`:** failures before `emitAgentStart`, which go through `emitAgentFailure` and
  `emitAgentPreStartFailure`.
- **Crash:** the post-start `ensureRunnerControl` and `prepareAgentCallRuntime` failures.
- **Not a crash:** `failAgentStepForUncollectedCalls`, because the session finished.

A single helper, `recordAgentCrash(ctx, step, prefix, attempt, invocation, runErr)`, runs on every crash
path. It does three things:

- builds a `model.CrashRecord`;
- adds the record to the run's crash ledger (§3);
- sets the step's failure (§2) to `{Kind: infrastructure, Origin: record}`.

`emitAgentEnd` then writes the failure fields onto the step's `step_end`. Inline repair agents and rerun
replays already execute through `ExecuteAgentStep`, so they are covered without extra wiring.

### 2. Terminating failure kind: a reset-per-dispatch side channel

Add these types to `internal/model`:

```go
type FailureKind string // "", "infrastructure", "step"

type StepFailure struct {
    Kind   FailureKind
    Origin *CrashRecord // non-nil only when Kind == infrastructure
}
```

The context gains three fields:

- `StepFailure StepFailure`: the result of the step that just finished in this context.
- `PreviousStep`: a `{Outcome, FailureKind, CrashObserved}` tracker that replaces the bare
  `LastStepOutcome` pointer and feeds both `skip_if: previous_success` and the built-ins. See the
  "Previous-step built-ins" rule below.

Rules:

- **Reset and normalize.** `DispatchStep`, and the runner's direct `ExecuteLoopStep` call, wrap execution
  in `runClassified`. It resets `ctx.StepFailure` before dispatch. Afterwards it normalizes:
  - when the outcome is `failed` or `exhausted` and `Kind` is empty, `Kind` becomes `step`;
  - for any other outcome, `StepFailure` is cleared.

  This makes `step` the safe default for every non-agent leaf and for every container error path.
- **Groups** share the context. When a child's blocking failure ends the group, `ctx.StepFailure` already
  holds that child's value. Every other failure return in the group explicitly sets `StepFailure{Kind: step}`:
  - a `PrimeReplayResume` error;
  - a `skip_if` evaluation error;
  - a child dispatch error that left `StepFailure` empty.

  When a child's failure is absorbed, the group clears `ctx.StepFailure`, so a later non-child failure
  cannot inherit it.
- **Sub-workflows**: when `executeChildSteps` returns failed, `ExecuteSubWorkflowStep` copies
  `childCtx.StepFailure` to `parentCtx.StepFailure`. The copy is made only when the failure came from a
  blocking child, which `finishChildStep`, `rw.Stopped`, and the dispatch-error branch mark. Resume
  resolution errors and preparation errors set `step`. `finishChildStep` clears `childCtx.StepFailure` when it
  absorbs a failure.
- **Loops**: `executeIterationBody` keeps the iteration context's `StepFailure` from the blocking body step.
  The loop records the last iteration's value:
  - when the loop returns `failed`, it copies that iteration's value;
  - when the loop returns `exhausted`, it copies the last iteration's terminating value if there was one,
    and uses `step` otherwise.

  An iteration whose body step failure was absorbed clears the value.
- **Repair**: add `LastAttemptCrashed bool` to `model.RepairFrame` (it is persisted, so it survives resume).
  - An inline attempt whose repair agent crashed sets it.
  - A replay attempt that ended because a replayed agent crashed sets it. `rewind.go` sees the replayed
    step's `StepFailure` when it records the failed attempt.
  - An attempt that reaches a check rerun clears it.

  `terminalExhausted` sets `StepFailure{Kind: infrastructure, Origin: frame's last crash}` when the flag is
  set, and `step` otherwise. `terminalBlocked` sets `step`. The origin is referenced by its ledger key, so
  the frame stores only `LastAttemptCrashed` and `LastCrashPrefix`.
- **Previous-step built-ins: one tracker per scope.** The outcome, failure kind, and crash-observed value
  move together into a single `PreviousStep{Outcome, FailureKind, CrashObserved}` tracker, which replaces
  the bare `LastStepOutcome` pointer. `ShouldSkipStep` and the built-ins read the same tracker.
  `recordLastStepOutcome(ctx, step, outcome)` gains the step, so it can write the kind from
  `ctx.StepFailure.Kind` and crash-observed from the ledger (§3). The runner's top-level writes at
  `runner.go` lines 698 and 733 go through the same helper. Skip handling keeps each scope's existing
  `LastStepOutcome` behavior, with no flow-control change:
  - **Groups and loop bodies** already record `OutcomeSkipped` for a skipped step, so the tracker becomes
    `{skipped, "", false}`.
  - **Top-level and sub-workflow sequencers** leave `LastStepOutcome` unchanged on a skip, so the tracker
    (and therefore the built-ins) keeps describing the last executed step.
- **Previous-step tracker survives resume.**
  - **Persistence:** `NestedStepState` gains `PreviousStep *PreviousStepRecord`, alongside the existing
    `LastAgent`. Every state write that persists a scope persists that scope's tracker: the root entry in
    `writeStepState`, sub-workflow entries in `recordChildProgress`, and iteration bodies in
    `newIterationBodyEntry`.
  - **Restore:** when resume re-enters a scope, the tracker is restored from that entry before the first
    step runs. This applies to the root context in `initRunState`, sub-workflow contexts in
    `applyResumeState`, and iteration contexts in `applyIterationBodyResume`.
  - **Effect on `previous_success`:** `skip_if: previous_success` after a resume now sees the true
    preceding outcome, where today it starts nil. This closes a pre-existing gap against the
    `step-flow-control` spec.
  - **Old runs:** runs without the field restore nil, which is today's behavior.
- **Top level**: after the step that ends the run, `rs.ctx.StepFailure` is the run's failure. The
  `err != nil` path uses `StepFailure` if it is set, and `step` otherwise. Every top-level stop path that is
  not the end of a dispatched step sets `rs.ctx.StepFailure = {Kind: step}`, with no origin, before
  classifying the run. These paths are a `skip_if` evaluation error, a `PrimeReplayResume` or
  resume-resolution error in `executeSteps` / `executeTopLevelStep`, and any other orchestration error.
  Without this, a value left by an earlier absorbed crash could be reported as the run's failure.

### 3. Crash-observed: a run-scoped crash ledger, queried by path

A `CrashLedger` is shared by reference across all contexts, like `WarningOrigins`, and guarded by a mutex.
It holds `[]CrashRecord`:

```go
type CrashRecord struct {
    StepID             string           `json:"stepId"`
    Prefix             string           `json:"prefix"`             // full audit prefix
    Path               []NestingSegment `json:"path"`               // nesting path + leaf segment
    Attempt            int              `json:"attempt"`
    ExitCode           *int             `json:"exitCode,omitempty"`
    Error              string           `json:"error,omitempty"`
    Stderr             string           `json:"stderr,omitempty"`   // ≤4096 bytes total, see excerpt rule
    ExecutionSessionID string           `json:"executionSessionId"`
}
```

- **Excerpt rule.** `boundStderr(s)` returns `s` unchanged when `len(s) <= 4096`. Otherwise it returns
  head + `marker` + tail, where `marker` is the fixed string `"\n[... stderr truncated ...]\n"`. The
  remaining `4096 - len(marker)` bytes are split with `head = floor(remaining/2)` and `tail` taking the
  rest. Each cut moves inward to a UTF-8 rune boundary, so the result is always valid UTF-8 and never
  exceeds 4096 bytes.
- **Query.** `ObservedUnder(path []NestingSegment) bool` returns true when any record's `Path` starts with
  `path`. Segments are compared by `StepID`. `Iteration`, `SubWorkflowName`, and `RepairAttempt` are compared
  only when the query segment sets them. So the loop step `[loop]` matches records under every iteration,
  while `iteration_end` queries `[loop:i]`. Structured paths are used instead of string-prefix matching,
  because `loop:2` does not prefix-match `loop`.
- **Crash-observed for any step** is computed at the moment its end event is emitted and when
  `recordLastStepOutcome` runs. It is true when `ObservedUnder` matches the step's own path. No container has
  to OR anything upward: a descendant's record is automatically under the ancestor's path. This holds for
  successful, failed, warning, and aborted endings, and it gives "sticky" for free. In-run rerun replays
  reuse the same path and execution session, so nothing is ever removed during an execution.
- **Run level**: `crash_observed` is true when the ledger is non-empty.
- **Resume**:
  - The ledger is persisted as `RunState.Crashes` and rewritten on every state flush (`writeStepState` and
    nested `FlushState` callbacks). `initRunState` restores it on `--resume`.
  - When a **leaf** step starts executing (agent step, check step), it calls
    `ledger.PruneReexecuted(path, ctx.ExecutionSessionID)`. That removes records at or under its path from
    other execution sessions, so a re-executed step's signals reflect its new execution.
  - Containers never prune, so a re-entered container keeps the persisted records of children that
    completed earlier and gains those of re-executed children.
  - A check's pruning also covers its earlier repair-attempt records, which matches "resume re-enters the
    cycle with a fresh budget".
  - Pruned origins stay inspectable on the earlier attempt's `step_end` in the audit log.

### 4. Built-in variables

`BuiltinVarsForStep` always sets `last_step_failure_kind` (`string(ctx.PreviousStep.FailureKind)`) and
`last_step_crash_observed` (`"true"`/`"false"`). It follows the always-present pattern used for
`intake_handoff`. Both read the scope's `PreviousStep` tracker. A scope with no recorded step yet, whether
fresh or restored from an old run, yields `""` and `false`. Normal
precedence (params and captures shadow built-ins) is unchanged. The values are plain identifiers, so `sh:`
shell quoting is a no-op.

### 5. Audit and state

- **`emitStepEnd`** adds:
  - `failure_kind` when the outcome is `failed` or `exhausted`, read from `ctx.StepFailure` and defaulting
    to `step`;
  - `crash_observed` for every non-skipped outcome, read from the ledger by the step's path;
  - `failure_origin` (the `CrashRecord`) when the kind is `infrastructure`.

  The skipped-step emitters omit all three.
- **`sub_workflow_end`** (in `ExecuteSubWorkflowStep`) and **`iteration_end`** (in
  `executeIterationWithAudit`) add the same fields. The first queries the sub-workflow's child path, the
  second the iteration path.
- **`run_end`** (`runEndData`) adds `failure_kind` and `failure_origin` from `rs.ctx.StepFailure` when the
  run failed, and `crash_observed` from the ledger.
- **`RunState`** gains `FailureKind`, `CrashObserved`, and `Crashes`, all with `omitempty`. They are written
  next to `FailureReason` and `Completed` by `writeStateFailureReason` and `markStateCompleted`, so consumers
  such as the factory can classify a finished run from `state.json` without parsing the audit log.

### 6. Failure reason

`classifyRunFailure` checks `rs.ctx.StepFailure.Kind` first:

- **`infrastructure` with an origin**: it returns `exec.ClassifyCrash(origin, repairAttempts)`. The format is
  `<stepID> failed (infrastructure): <first non-empty line of Error, else of Stderr>`. When neither has a
  line, it falls back to `exit code N`, and then to `agent session did not finish`.
  - When the crash ended a repair cycle, ` after N repair attempts` is appended, with N taken from the frame.
  - The step ID is the crashed agent's own ID, which is the repair agent's synthesized `repair` ID for an
    inline repair. In that case the reason uses the owning check's ID followed by `repair`, for example
    `check-plan repair failed (infrastructure): …`. That way the reason names something the user can find.
- **Otherwise** it keeps today's `LastFailure` path.

The run view, run list, debug workflow, and resume hint already render `RunState.FailureReason` and the
`run_end` reason, so they need no changes.

### Data flow (crash inside a builtin sub-workflow)

```
generate-code CLI exits 1 ──► runAgentProcess: Crashed
  └─ recordAgentCrash: ledger += {path [impl, sub:implement-task, generate-code]}, ctx.StepFailure = infra
       step_end{failed, infrastructure, crash_observed:true, failure_origin}
  └─ finishChildStep: blocking ─► return failed
ExecuteSubWorkflowStep: parent.StepFailure = child.StepFailure
       sub_workflow_end / step_end{failed, infrastructure, crash_observed:true}
caller group (continue_on_failure): absorbs; PreviousStep={failed, infrastructure, true}
next step: skip_if 'sh: test "{{last_step_crash_observed}}" != true'  ──► runs, e.g. exits 1 "technical"
run_end{failed, failure_kind: step|infrastructure per what ended it, crash_observed:true}
```

## Decisions

1. **Classification is a separate failure kind, not a new outcome value.** This was settled in the proposal.
   Every `== OutcomeFailed` site stays correct.
2. **The terminating kind uses a reset-per-dispatch side channel on `ExecutionContext`, not a changed
   executor return type.**
   - A `StepResult` return type would touch every executor and its tests: `DispatchStep`, the group, loop,
     sub-workflow, check, agent, and UI executors, the runner, and repair.
   - The side channel mirrors the existing `LastStepOutcome` and `LastFailure` patterns.
   - The reset in `runClassified` plus the default to `step` removes the stale-value risk on paths that
     forget to set it.
3. **Crash-observed is derived from a path-queried ledger, not propagated per container.**
   - Explicit OR-propagation would need code in each container and per-container accumulators persisted in
     `NestedStepState` for resume.
   - The ledger makes the signal a pure function of the step's path. It is naturally sticky and survives
     resume through a single persisted list.
4. **Resume pruning is leaf-only and keyed on execution session.** This gives "re-executed steps reflect their new
   execution" without erasing crashes from in-run replays or from completed children of re-entered
   containers.
5. **The stderr excerpt is at most 4096 bytes in total, split head and tail around a fixed marker** (§3,
   excerpt rule). The head carries the first-line reason. The tail carries the final provider error, which
   CLIs often print last. The marker's bytes come out of the 4096-byte budget, so the cap is exact.
8. **The preceding-step tracker is persisted per scope and kept in step with each scope's existing skip
   behavior.** The built-ins must describe the same step that `skip_if: previous_success` evaluates, both
   across resume and around skipped steps.
   - Alternatives considered: persist only the new signals (rejected: they would drift from the outcome
     that `skip_if` reads); make skip handling uniform across scopes (rejected: that would change existing
     flow control).
   - Restoring the outcome on resume closes a pre-existing gap. Today `previous_success` sees nil after a
     resume.
6. **Run-stop cancellation is not a crash. Stalls and `ErrWaitDelay` without completed output are crashes.**
   The spec was amended to say this explicitly.
7. **`state.json` mirrors the run-level fields.** The factory reads run state, and `run_end` alone is not
   always easy to locate.

## Risks / Trade-offs

- **A non-zero exit that is not a crash**, such as a CLI's own turn limit, is classified `infrastructure`.
  - This is accepted by the proposal.
  - It is mitigated by the adapter's existing `HeadlessResultFilter`, which can map known benign exits.
  - Tests pin the current mapping per adapter.
- **A side-channel setter on a container path could be missed.** `runClassified` defaults that case to
  `step`, which fails safe because crashes are still visible through `crash_observed`. Each container
  executor gets table tests for blocking, absorbed, and error paths.
- **Stdout-only provider errors.** Claude in stream-JSON mode can report the error in its stdout result
  event and leave stderr empty. The reason then degrades to `exit code 1`, which is still classified
  correctly. Extracting a provider message from stdout would need a new adapter hook and is left out.
- **Path-matching correctness depends on `NestingSegment` fidelity** for repair attempts and loop
  iterations. Tests cover crashes under `loop:i`, `sub:x`, and `attempt:n` segments.
- **Ledger growth** is bounded by the number of crashes in a run, which is small, and each record's stderr is
  capped at 4096 bytes.
- **Restoring the preceding outcome on resume changes `skip_if: previous_success` for resumed runs**, from
  "nil" to the true preceding outcome. This matches the existing spec. Runs without the persisted field
  keep today's behavior.

## Testing Strategy

Tests use local fakes: the existing fake `ProcessRunner` in `internal/exec` tests and the testdata
workflows. They go into the existing packages and the existing `go test ./...` job.

- **`internal/exec` invocation table**: one test per row of the classification table, using a fake runner
  returning a launch error, exit 1, exit -1, `ErrWaitDelay`, context cancel, stall cause, and the
  AskUserQuestion stderr. Plus pre-start failures, control-channel failure, and uncollected calls.
- **`internal/exec` containers**:
  - group, sub-workflow, and loop, each with a blocking crash, an absorbed crash followed by an ordinary
    failure, an absorbed crash followed by success, and a container error path;
  - nested sub-workflow inside a group inside a loop.

  Each asserts `StepFailure`, the `PreviousStep` tracker, and the end-event fields.
- **Skip synchronization**: a crashed step with `continue_on_failure`, then a skipped step, then a step
  that reads the built-ins. This runs at top level, in a group, in a loop body, and in a sub-workflow.
  - At top level and in a sub-workflow, the reader sees `infrastructure` / `true`.
  - In a group and a loop body, it sees `""` / `false`.
  - In every case `skip_if: previous_success` agrees with the tracker.
- **Excerpt bound**: `boundStderr` with stderr lengths of 4096 bytes, 4097 bytes, and much longer. The
  boundary inputs include multibyte runes. The test asserts a total of at most 4096 bytes, valid UTF-8, and
  the exact head, marker, and tail.
- **`internal/exec` repair**:
  - an inline repair agent crashing on the last attempt;
  - a crash then recovery;
  - a finished repair agent with the check still red;
  - a rerun-target crash during the last replay.
- **`internal/model`**: `CrashLedger.ObservedUnder` and `PruneReexecuted` path semantics; the built-in
  variable defaults and shadowing; JSON round-trip of the new `RunState` and `RepairFrame` fields; and
  decoding of a pre-change `state.json` with the fields absent.
- **`internal/runner`**:
  - `run_end` and `state.json` fields for a run that stops on a crash, a run that completes after an absorbed
    crash, and a clean run;
  - `classifyRunFailure` for a direct crash, a crash inside a sub-workflow after an earlier absorbed check
    failure, a crash that exhausts repair, and an ordinary check;
  - resume after a crash, which prunes and yields a false crash-observed, and resume inside a sub-workflow
    after an absorbed crash in a completed child, which keeps it;
  - resume after an interruption that lands just after a completed, absorbed crash, at top level and inside
    a sub-workflow. The next step's built-ins and its `skip_if: previous_success` see the restored
    `{failed, infrastructure, true}` before that step runs;
  - an absorbed crash followed by an invalid top-level `skip_if` expression. `run_end` and `state.json`
    show `failure_kind: step` and `crash_observed: true`, with no `failure_origin`, and the reason is not
    an infrastructure reason.

  These follow `resume_repair_test.go` patterns.
- **Builtin workflow smoke**: a `workflows` package test that runs `implement-task` with a fake agent CLI that
  exits 1 and asserts the sub-workflow step's failure kind and crash-observed value.

## Migration Plan

All additions are optional JSON fields. Runs written earlier decode with an empty kind, which readers treat
as `step` for failed steps, a nil ledger, and `crash_observed` false. That matches the spec's historical-run
scenario. Older binaries ignore the new fields because none of the state or audit decoders reject unknown
fields. No rollout gating is needed. The factory change in agent-factory#92 can adopt
`{{last_step_crash_observed}}` and the `state.json` / `run_end` fields once this ships.

## Open Questions

None.
