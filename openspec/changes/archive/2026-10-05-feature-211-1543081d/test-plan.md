## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

Most of the logic is covered by unit tests, using the fake `ProcessRunner` and the testdata workflows
listed under "Testing Strategy" in `design.md`. That logic is:

- crash classification per process-result path;
- the failure kind carried by each container;
- crash-ledger path queries and pruning on resume;
- the built-in variables;
- end-event fields;
- failure-reason formatting.

The obligations below cover what those unit tests cannot prove:

- that real subprocess terminations reach the classifier as the design assumes;
- that the real builtin workflows carry the signals to a caller;
- that the built binary's public surfaces agree with each other: the `--headless` CLI, the audit log,
  `state.json`, the console resume hint, and resume.

## Integration Tests

### INT-001: Real subprocess terminations classify correctly
- Covers: `infrastructure-failure-classification`, the requirements "Agent crash classification" and "Ordinary agent failures"
- Boundary: the production headless process runners execute real child processes and report the result to `runAgentProcess` / `InvokeAgent` classification. These are the `cmd/agent-runner` process runner and the `internal/liverun` TUI process runner.
- Setup: create POSIX shell stubs in a temp dir for each case:
  - exits 1 with `Selected model is at capacity` on stderr;
  - kills itself with `kill -9 $$`;
  - is a missing binary path;
  - exits 0 but leaves a background child holding stdout open past the wait delay;
  - exits 0 with a disallowed-`AskUserQuestion` line on stderr;
  - exits 0 normally.

  A context-cancellation case cancels the invocation context while a stub sleeps.
- Action: invoke each stub through both process runners as a headless agent invocation.
- Assertions:
  - The crash flag is set for exit 1, the signal kill, the missing binary, and the wait-delay case.
  - The crash flag is not set for AskUserQuestion, a clean exit, or context cancellation.
  - The captured stderr for the exit-1 case contains the capacity line.
- Execution: in the packages that own each runner (`cmd/agent-runner`, `internal/liverun`), skipped on Windows like the existing fake-CLI tests. Runs in `make test` / the CI `test` job.

### INT-002: Crash inside builtin `implement-task` reaches the caller
- Covers: `sub-workflows`, the requirement "Sub-workflow failure signals" (crash inside a builtin sub-workflow); `step-flow-control`, the requirement "Previous step failure signals"
- Boundary: the embedded `builtin:core/implement-task-v1.0.yaml` is loaded and run by the real loader and the sub-workflow, group, and agent executors. It is called from a parent workflow step with `continue_on_failure: true`, followed by a shell step that records `{{last_step_failure_kind}}` and `{{last_step_crash_observed}}`.
- Setup: follow the existing `internal/exec/implement_task_workflow_test.go` harness. Use a temp git repo and task file, and a fake `ProcessRunner` that succeeds on shell and script steps up to `generate-code`. For the `generate-code` agent invocation, it returns exit 1 with capacity stderr.
- Action: execute the parent workflow.
- Assertions:
  - The sub-workflow step's `step_end` has `failure_kind: infrastructure` and `crash_observed: true`.
  - Its `failure_origin` carries the nested prefix ending in `generate-code`.
  - The recording step captured `infrastructure` and `true`.
  - A control variant, in which `generate-code` succeeds and a later check fails, records `step` and `false`.
- Execution: `internal/exec`, in `go test ./...`.

### INT-003: Absorbed crash inside builtin `finalize-pr` is still reported on success
- Covers: `infrastructure-failure-classification`, the requirement "Crash-observed signal"; `sub-workflows`, the scenario "Absorbed crash in finalize-pr"
- Boundary: the embedded `builtin:core/finalize-pr-v1.0.yaml` runs its real `ci-fix-loop` flow control (`continue_on_failure` on the failing gates, `break_if` on the status gate) under the real loop and sub-workflow executors.
- Setup: use a fake `ProcessRunner` that scripts `push-pr` success and lets `ci-wait.sh` succeed. The first `ci-status-gate.sh` and `ci-fix-needed-gate.sh` runs fail, so `fix-pr` runs and its first agent invocation crashes. Later status gates pass. Nothing contacts GitHub, because every script and shell step is faked.
- Action: execute a parent workflow whose single step calls `finalize-pr`.
- Assertions:
  - The sub-workflow step's outcome is `success`, with `crash_observed: true` and no `failure_kind`.
  - The first iteration's `iteration_end` has `crash_observed: true`.
  - The `fix-pr` `step_end` for that iteration has `failure_kind: infrastructure`.
- Execution: `internal/exec`, in `go test ./...`.

### INT-004: Crashed inline repair agent exhausts a check as an infrastructure failure
- Covers: `step-repair`, the requirement "Crashed repair agents"; `run-failure-evidence`, the requirement "Classified failure reason" (repair-agent reason)
- Boundary: the real repair cycle, repair-attempt context, agent executor, and top-level runner failure classification.
- Setup: a testdata workflow containing a check with `repair: {max: 1}` that always fails. A fake `ProcessRunner` makes the repair agent exit 1 with capacity stderr.
- Action: run the workflow through `runner` and read the failure reason, `state.json`, and `run_end`.
- Assertions:
  - The check's `step_end` has `failure_kind: infrastructure`.
  - `RunState.FailureReason` and the `run_end` `failure_reason` both equal `check-plan repair failed (infrastructure): Selected model is at capacity after 1 repair attempts`.
  - A variant where the repair agent finishes and the recheck fails yields `failure_kind: step` and today's check-derived reason.
- Execution: `internal/runner`, in `go test ./...`.

### INT-005: Preceding-step record survives resume
- Covers: `step-flow-control`, the requirement "Preceding-step tracking survives resume"; `infrastructure-failure-classification`, the scenario "Resume right after an absorbed crash"
- Boundary: the runner's state persistence (`state.json` write and read) and resume re-entry of the top-level scope and a sub-workflow scope, plus `skip_if` and built-in interpolation on the restored context.
- Setup: a testdata workflow following the `internal/runner/resume_repair_test.go` patterns. At top level, and inside a sub-workflow, an agent step with `continue_on_failure: true` crashes through a fake `ProcessRunner`. The run is then interrupted, simulated by stopping after that step's state write. The next step records the two built-ins and carries `skip_if: previous_success`. A second variant has a succeeding step before the interruption.
- Action: resume from the persisted state.
- Assertions:
  - Before the next step runs, its built-ins resolve to `infrastructure` and `true`, and `previous_success` treats the preceding step as failed.
  - In the success variant, the next step is skipped.
  - A state file without the record resumes with empty and false values.
- Execution: `internal/runner`, in `go test ./...`.

## End-to-End Tests

### E2E-001: Headless run stops on a crash inside a sub-workflow
- Covers: the main journey in the issue: crash → sub-workflow → caller branch → run outcome and evidence. Specifically `infrastructure-failure-classification` (classification, container failure kind), `builtin-vars`, `audit-log-entries`, and `run-failure-evidence`.
- Surface: the built `agent-runner` binary with `--headless`.
- Setup:
  - Follow the `cmd/agent-runner/repair_inline_e2e_test.go` pattern: a temp HOME and project, the smoke profile config, and a fake `claude` stub on PATH that exits 1 with `Selected model is at capacity` on stderr.
  - The project workflow calls a project sub-workflow containing a headless agent step, with `continue_on_failure: true` on the parent step.
  - The next parent step writes `{{last_step_failure_kind}}` and `{{last_step_crash_observed}}` to a file.
  - A final parent step, a second sub-workflow call without `continue_on_failure`, crashes again.
- Journey: run the workflow headless until it fails.
- Assertions:
  - The exit status is non-zero and unchanged from today's failed-run status.
  - The file contains `infrastructure` and `true`.
  - The audit log has `failure_kind`, `crash_observed`, and `failure_origin` on the agent `step_end`, the `sub_workflow_end`, and the parent `step_end`.
  - `run_end` has `outcome: failed`, `failure_kind: infrastructure`, `crash_observed: true`, and a `failure_origin` whose prefix names the nested agent step.
  - `state.json` has `failureKind: infrastructure`, `crashObserved: true`, a non-empty crash list, and `failureReason` = `<agent-step> failed (infrastructure): Selected model is at capacity`.
  - The console output prints that reason above the `to resume:` hint.
- Execution: `cmd/agent-runner/*_e2e_test.go`, skipped on Windows. Runs in the CI `test` job.

### E2E-002: Resume after a crash and recovery in a later loop iteration
- Covers: `infrastructure-failure-classification`, the requirements "Classification survives resume" and "Crash-observed signal" (loop recovery); `run-failure-evidence`, the scenario "Origin survives resume"
- Surface: the built binary with `--headless`, then `--resume`.
- Setup:
  - The temp HOME, project, and smoke profile follow E2E-001.
  - The fake `claude` stub reads a counter file. It exits 1 on the first call and exits 0 with a normal result afterwards.
  - Run A has one top-level agent step without `continue_on_failure`.
  - Run B has a counted loop with `break_if: success` on a shell gate. Its body agent step has `continue_on_failure: true` and crashes only on the first call.
- Journey:
  - Run A until it fails, then resume it with `--resume` until it completes.
  - Run B once until it completes.
- Assertions:
  - Run A:
    - The first attempt's `step_end` keeps its `failure_origin`.
    - After resume, the run completes with exit 0.
    - The final `run_end` and `state.json` show `crash_observed: false`, with no `failure_kind` and no crash records remaining.
  - Run B:
    - The run completes with exit 0.
    - The loop step's `step_end` has `outcome: success` and `crash_observed: true`.
    - `run_end` and `state.json` show `crash_observed: true`.
- Execution: `cmd/agent-runner/*_e2e_test.go`, in the CI `test` job.

## Acceptance Testing Envelope

- Environments and sandboxes:
  - Local checkout run through `./dev.sh` (compiles from source).
  - Throwaway temp project directories, each with `git init`.
  - An isolated `HOME`, so run history goes to a temp `~/.agent-runner` and not Paul's.
  - Headless runs only. Interactive-mode crash paths may be exercised under a synthetic PTY per `CLAUDE.md`, but a flow that needs a real interactive conversation is out of reach.
- Credentials and secrets:
  - No new credentials.
  - The locally installed agent CLIs (claude, codex, cursor, copilot) may use whatever login already exists on the machine.
  - Do not copy, print, or move credentials into the temp HOME. Point the CLIs at their existing config only if a real-CLI probe is needed.
- Authorized effects:
  - Unlimited fake-CLI stubs on PATH. This is the primary way to induce crashes: non-zero exits, signal kills, missing binaries, and stalls.
  - At most a few real agent CLI invocations that fail before doing model work, such as an invalid `--model` or a deliberately wrong CLI flag, to observe a real CLI's non-zero exit and its stderr/stdout shape. This costs effectively nothing.
  - Running builtin `implement-task`, `verify-change`, or `archive-change` against a temp git repo with fake CLIs is allowed.
  - Clean up the temp dirs afterwards.
- Off limits:
  - Paul's real `~/.agent-runner` run history and settings.
  - Any GitHub remote or pull request: no `push`, `gh pr`, or `finalize-pr` against a real remote. `finalize-pr` may only run with every git and gh call faked or against a local bare remote.
  - The live Agent Factory service and its claims.
  - Successful real-model agent sessions, which have real cost.
  - Killing processes other than the ones the pass started.
- Permitted substitutes:
  - A local bare git repo instead of a GitHub remote.
  - Fake CLI stubs instead of real agent CLIs for all crash induction.
  - A stub `gh` on PATH for `finalize-pr` flows.
- Known risk areas:
  - Claude in stream-JSON mode may report a provider error only on stdout. The design accepts that the reason then degrades to `exit code 1` while the classification stays correct.
  - Non-zero exits that are not crashes, such as a CLI turn limit, are classified `infrastructure`. This is accepted.
  - Crash-ledger path matching under `loop:i`, `sub:<name>`, and `attempt:n` nesting.
  - Pruning on resume: a re-entered container must keep earlier children's crashes, while a re-executed leaf drops its own.
  - A stale `StepFailure` leaking from an absorbed failure into a later container-error failure.
  - The repair and rerun replay interplay, which is a prior defect cluster in `step-repair`.
  - The Cursor stall path and the `ErrWaitDelay` path, which only some adapters reach.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| Agent crash classification (real subprocess terminations) | INT-001 | E2E-001 | — |
| Ordinary agent failures (AskUserQuestion, clean exit, cancellation) | INT-001 | — | — |
| Container failure kind through sub-workflows | INT-002 | E2E-001 | — |
| Crash-observed signal on successful containers | INT-003 | E2E-002 | — |
| Classification survives resume | INT-005 | E2E-002 | — |
| Preceding-step tracking survives resume | INT-005 | — | — |
| Sub-workflow failure signals (builtin workflows) | INT-002, INT-003 | — | — |
| Previous step failure signals / built-in variables | INT-002 | E2E-001 | — |
| Crashed repair agents | INT-004 | — | — |
| Failure classification on end events (`run_end`, `sub_workflow_end`, `iteration_end`) | INT-003 | E2E-001, E2E-002 | — |
| Agent crash failure origin and classified failure reason | INT-004 | E2E-001, E2E-002 | — |
