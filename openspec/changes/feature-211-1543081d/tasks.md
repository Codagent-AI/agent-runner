- [x] Implement the change described by these files using TDD, as `CLAUDE.md` requires. Satisfy every spec scenario and every `INT-*` / `E2E-*` obligation in the test plan.

  **Classification**
  - Add `Crashed` and the crash error to `AgentInvocationResult`, and set them in `runAgentProcess` / `InvokeAgent` (`internal/exec/agent.go`, `internal/exec/invocation.go`) following the design's classification table.
  - Add a `recordAgentCrash` helper and call it on every post-start crash path in `ExecuteAgentStep`. This includes the control-channel and agent-call runtime setup failures.
  - Pre-start failures, disallowed `AskUserQuestion`, uncollected calls, run-stop cancellation, and `call_agent` children stay non-crash.

  **Failure kind**
  - Add `model.FailureKind`, `model.StepFailure`, and `ctx.StepFailure`.
  - Wrap dispatch in `runClassified`: reset before dispatch, and default to `step` afterwards. Use it from `DispatchStep` and the runner's direct `ExecuteLoopStep` call.
  - In the group, sub-workflow, and loop executors, copy the blocking child's value. Clear it on an absorbed failure, and set `step` on orchestration-error paths.
  - At every top-level stop path that is not a dispatched step's failure, set `StepFailure{Kind: step}` with no origin.

  **Repair**
  - Add `LastAttemptCrashed` and `LastCrashPrefix` to `model.RepairFrame`.
  - Set them from crashed inline repair agents and from crashed rerun-replay agents.
  - Apply them in `terminalExhausted`. `terminalBlocked` sets `step`.

  **Crash ledger**
  - Add a run-scoped `CrashLedger` of `CrashRecord`, shared by reference like `WarningOrigins`, with `ObservedUnder` (structured path matching) and `PruneReexecuted`.
  - Apply the `boundStderr` excerpt rule: at most 4096 bytes in total including the marker, split head and tail, cut on UTF-8 boundaries.
  - Persist the ledger as `RunState.Crashes` on every state write, and restore it on `--resume`.
  - Prune it only when a leaf agent or check step starts, and only records from other execution sessions.

  **Preceding-step tracker**
  - Replace `LastStepOutcome` with a per-scope `PreviousStep{Outcome, FailureKind, CrashObserved}` tracker, read by both `skip_if: previous_success` and the built-ins.
  - Keep each scope's existing skip behavior: groups and loop bodies record the skip, while the top level and sub-workflows keep the last executed step.
  - Persist the tracker in `NestedStepState.PreviousStep` (root, sub-workflow, and iteration entries), and restore it when resume re-enters each scope.

  **Built-ins**
  - In `BuiltinVarsForStep`, always set `last_step_failure_kind` and `last_step_crash_observed`.

  **Audit and state**
  - Add `failure_kind`, `crash_observed`, and `failure_origin` to `step_end` (`emitStepEnd`), `sub_workflow_end`, `iteration_end`, and `run_end` (`runEndData`). Skipped steps omit them.
  - Add `FailureKind`, `CrashObserved`, and `Crashes` to `model.RunState`, with `omitempty`. Write them next to `FailureReason` and `Completed`.

  **Failure reason**
  - When the run's kind is `infrastructure`, `classifyRunFailure` uses a new `exec.ClassifyCrash` and produces `<step> failed (infrastructure): <first line>`. For an inline repair agent the step is `<check> repair`. Apply the exit-code and `agent session did not finish` fallbacks, and append ` after N repair attempts` when a crash ended a repair cycle.
  - Otherwise keep today's check-derived reason.

  **Tests**
  - Add the unit tests listed under "Testing Strategy" in `design.md`.
  - Add the integration tests: INT-001 in `cmd/agent-runner` and `internal/liverun`, INT-002 and INT-003 in `internal/exec` against the real builtin `implement-task` and `finalize-pr` YAML, and INT-004 and INT-005 in `internal/runner`.
  - Add the end-to-end tests E2E-001 and E2E-002 to `cmd/agent-runner/*_e2e_test.go`, using the existing fake-CLI harness.

  Do not change outcome values, exit codes, existing audit fields, the meaning of `continue_on_failure`, or the builtin workflow YAML. Finish with `make fmt`, `make test`, and `make lint` passing.

  Source files:
  - [proposal.md](proposal.md)
  - [specs/infrastructure-failure-classification/spec.md](specs/infrastructure-failure-classification/spec.md)
  - [specs/step-flow-control/spec.md](specs/step-flow-control/spec.md)
  - [specs/sub-workflows/spec.md](specs/sub-workflows/spec.md)
  - [specs/builtin-vars/spec.md](specs/builtin-vars/spec.md)
  - [specs/step-repair/spec.md](specs/step-repair/spec.md)
  - [specs/audit-log-entries/spec.md](specs/audit-log-entries/spec.md)
  - [specs/run-failure-evidence/spec.md](specs/run-failure-evidence/spec.md)
  - [design.md](design.md)
  - [test-plan.md](test-plan.md)
  - [decisions.md](decisions.md)
