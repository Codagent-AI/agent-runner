## Coverage Strategy

The specifications remain the source of unit-test requirements. This plan records only:

- the additional integration obligations;
- the acceptance testing envelope;
- human-only obligations, of which there are none.

Most of the change sits in two kinds of place: workflow YAML wiring, and two small shell scripts
(`run-validator.sh` and the new `check-task-compliance.sh`). The following checks belong in the
existing unit and structure suites and are not inventoried here:

- the script argument tables;
- the catalog-walk pin in `workflows/embed_test.go`;
- param defaults and step ordering;
- the prompt and note wording assertions.

What those suites can't detect is a break at the seam between workflow YAML and process: params
resolved through sub-workflow calls, serialized into `script_inputs`, read by `run-validator.sh`
through `agent-runner internal json-value`, and finally turned into the `agent-validator` argv. A
typo in a `script_inputs` key or a missing param forward passes every structure test and still
silently enables or disables the review. The integration obligations below cover that seam on the
two paths that matter: the factory-shaped default-on path and the non-factory default-off path.
They also cover the fail-fast guard, whose failure mode is handing a param error to a repair agent.

## Integration Tests

### INT-001: Factory-shaped implement-task call activates task-compliance on every validator launch
- Covers: "implement-task activates task-compliance by default" (caller that omits the param); "Task-compliance flags activated only by explicit opt-in" (opt-in adds the flags; correlation is independent of task-compliance).
- Boundary: The real executor runs `builtin:core/implement-task-v1.0.yaml`, its `core/run-validator` sub-workflow, the real `check-task-compliance.sh` and `run-validator.sh` scripts, and Runner's `internal json-value` helper. A fake `agent-validator` sits at the end of the chain.
- Setup:
  - A temporary git repository with a non-empty task file whose path contains a space.
  - Agents faked, as in `taskDeliveryRunner` in `internal/exec/implement_task_workflow_test.go`, with `run-validator.sh` executed for real instead of short-circuited.
  - A fake `agent-validator`, set via `AGENT_RUNNER_VALIDATOR_EXECUTABLE`, appends its argv to a log and fails the first N calls.
  - `AGENT_RUNNER_EXECUTABLE` points at an executable that implements `internal json-value`: either Runner built for the test, or a shim.
  - The metrics consumer/context environment is set.
- Action: Run `implement-task` with `task_file` set and no `task_compliance` param, which is the shape of the factory's call. Make the fake validator fail every retry, so that both `validator-retry/run-validator` and `verify-final-validator` launch.
- Assertions:
  - Every logged validator invocation contains `run --report --enable-review task-compliance --context-file <task file path, verbatim>` and the metrics flags.
  - Repeat the run with `task_compliance: "false"`: no invocation contains `--enable-review` or `--context-file`, and the metrics flags are still present.
- Execution: `internal/exec` (`go test ./internal/exec`), part of the existing `test` job.

### INT-002: A non-factory change run validates every task without task-compliance
- Covers: "Built-in workflows run without task-compliance" (core implement-change disables task-compliance per task; complete-simple-change disables task-compliance).
- Boundary: The real runner executes the composed built-in workflows end to end, from the public `core/plan-and-implement-change` (or a v2.0 `change` adapter) through `core/implement-change`, then `implement-task`, then `run-validator`, then the real scripts, ending at the fake `agent-validator`.
- Setup: Reuse `newPlanAndImplementFixture` in `internal/runner/plan_and_implement_change_test.go`, which provides fake `openspec`/`agent-validator` executables, a validator call log and faked agents. Use at least two task files and `skip_validator: "false"`. Add the same kind of run through `core/complete-simple-change`, reached via the v2.0 `openspec/simple-change` or `spec-driven/simple-change` adapter, if the fixture supports it at modest cost; otherwise a direct `complete-simple-change` executor run.
- Action: Run the workflow to the point where every per-task validator has launched.
- Assertions:
  - The validator call log has at least one invocation per task.
  - No invocation contains `--enable-review task-compliance` or `--context-file`.
  - The run does not fail on any `task_compliance` validation step.
  - In a variant where the fake validator fails every attempt, so final verification only warns, the run still reaches `complete-task-index`, and the prompt dispatched there neither mentions task-compliance nor claims that validation passed.
- Execution: `internal/runner` (`go test ./internal/runner`), part of the existing `test` job.

### INT-003: Invalid task-compliance params fail fast, before any validator or repair agent
- Covers: "Optional task_file param on run-validator" (invalid value fails fast); "Task-compliance flags activated only by explicit opt-in" (opt-in without a task file fails); "implement-task activates task-compliance by default" (invalid value fails before implementation).
- Boundary: The real executor runs `core/run-validator` and `core/implement-task` with the real guard scripts and a fake `agent-validator`.
- Setup: The same fake validator log and faked agents as INT-001, recording each agent prompt the executor dispatches.
- Action:
  - Run `core/run-validator` with `task_compliance: "yes"`.
  - Run `core/run-validator` with `task_compliance: "true"` and an empty `task_file`.
  - Run `core/implement-task` with `task_compliance: "maybe"`.
- Assertions:
  - Each run fails, and its error output names the offending param (`task_compliance` or `task_file`).
  - The validator log is empty.
  - No `fix-violations` prompt is dispatched.
  - For `implement-task`, no `generate-code` prompt is dispatched.
- Execution: `internal/exec` (`go test ./internal/exec`), part of the existing `test` job.

## End-to-End Tests

No automated end-to-end obligation. The public entry points (`agent-runner run` and the TUI) need
real agent CLIs to reach a per-task validator step, which makes them slow, costly and
nondeterministic. INT-002 already drives the composed built-in workflows through the real runner,
with only agents and Validator replaced. The behavior under test lives entirely in the workflow
definitions and scripts that INT-002 executes for real.

## Acceptance Testing Envelope

- Environments and sandboxes:
  - The designated Agent Runner checkout for this attempt, `/Users/paul/.agent-factory/clones/c2e85c52-34a7-4a2a-bf38-8e53f83f544b/0/repo`, run from source with `./dev.sh`. Reading and executing source there is authorized even though the path is under `~/.agent-factory`.
  - The current attempt's evidence directory, `/Users/paul/.agent-factory/artifacts/c2e85c52-34a7-4a2a-bf38-8e53f83f544b-feature/attempt-1`, and temp scratch directories. These are the only writable locations besides throwaway repositories.
  - Throwaway git repositories under a temp directory, initialized with `agent-validator init` or a copied `.validator/config.yml`, and an OpenSpec change with one or more task files.
  - Direct execution of `workflows/core/*.sh` with JSON on stdin.
  - Both the TUI and headless runs. Under a synthetic PTY, a run started with `r` does not launch (see CLAUDE.md), so launch runs from the command line.
- Credentials and secrets: none are needed. Real agent CLIs may already be logged in on the machine, but the pass must not depend on them.
- Authorized effects:
  - Creating and deleting temp repositories and files.
  - Running the real `agent-validator` locally, including one real `--enable-review task-compliance` review on a tiny repository to confirm that the opted-in path still reaches a working review. That review spends a small amount of model usage.
  - Delete temp repositories afterward.
- Off limits:
  - The agent-factory repository, the factory service and its production operations, and every other clone, run or artifact directory under `~/.agent-factory`. The only exceptions are the designated checkout and the current attempt's evidence directory named above.
  - GitHub: no pushes, pull requests or issue changes.
  - This repository's own `.validator/config.yml` and any user-level validator or agent configuration.
- Permitted substitutes:
  - Point `AGENT_RUNNER_VALIDATOR_EXECUTABLE` at an argv-recording wrapper around, or in place of, `agent-validator` to observe exactly which flags each launch receives.
  - Use stub agent executables when a real agent conversation isn't needed to reach the validator step.
- Known risk areas:
  - The final verification step (`verify-final-validator`) is a second launch site that is easy to miss when forwarding `task_compliance`.
  - The v1.0 `implement-change` and `simple-change` definitions are reachable only through the v1.0 `change` workflows, pinned references, or resume.
  - Runs resumed inside `implement-task` re-resolve params from the parent step after upgrading.
  - Task-file paths with spaces or shell metacharacters, which already have an injection test.
  - Metrics correlation flags must stay present whether or not task-compliance is activated.
  - The external-delivery note now says only that the validator did not cover the external work.
  - Accepted limitation: external callers of `core/run-validator` that relied on `task_file` alone to activate the review now silently lose it.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| Task-compliance flags activated only by explicit opt-in | INT-001, INT-003 | — | — |
| implement-task activates task-compliance by default (factory-shaped call) | INT-001, INT-003 | — | — |
| Built-in workflows run without task-compliance | INT-002 | — | — |
| Optional task_file param on run-validator (invalid value fails fast) | INT-003 | — | — |
