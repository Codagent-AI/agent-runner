- [x] Implement the change described by these files using TDD, as `CLAUDE.md` requires: write each failing test before the code that makes it pass. Satisfy every spec scenario and every `INT-*` obligation in the test plan.

  **`core/run-validator` (`workflows/core/run-validator-v1.0.yaml`, `workflows/core/run-validator.sh`)**
  - Add the optional param `task_compliance` with `default: "false"`.
  - Add a top-level `check-task-compliance` step before `validator-retry`, with no `continue_on_failure`. It runs a new script, `workflows/core/check-task-compliance.sh`, with `script_inputs` `task_compliance` and `task_file`. The script reads the JSON payload the way `validate-boolean-param.sh` does (jq, with a sed fallback). It exits 1 with `task_compliance must be true or false, got: <value>` for a non-boolean value, and with `task_compliance is true but task_file is empty` when the value is `true` and `task_file` is empty. Otherwise it exits 0 silently.
  - Forward `task_compliance: "{{task_compliance}}"` to both `run-validator.sh` steps: `validator-retry/run-validator` and `verify-final-validator`.
  - In `run-validator.sh`, read `task_compliance` with `"$AGENT_RUNNER_EXECUTABLE" internal json-value task_compliance`. Add `--enable-review task-compliance --context-file "$task_file"` only when `task_compliance` is exactly `true` and `task_file` is non-empty. Leave the metrics-correlation and `result_file` handling unchanged.
  - Keep the `fix-violations` guidance about task-compliance violations for work delivered in another repository.

  **`core/implement-task` (`workflows/core/implement-task-v1.0.yaml`)**
  - Add the optional param `task_compliance` with `default: "true"`. The Agent Factory calls this workflow without the param and must keep the review.
  - Add a `validate-task-compliance` step right after `validate-skip-validator`, before `check-task-file` and `generate-code`. It reuses `validate-boolean-param.sh` with `{name: task_compliance, value: "{{task_compliance}}"}`.
  - Pass `task_compliance: "{{task_compliance}}"` next to `task_file` in the `run-validator` sub-workflow call. Keep the `skip_validator` `skip_if` unchanged.
  - Reword `description:` so it doesn't promise task-compliance unconditionally.

  **Non-factory callers (edit the v1.0 files in place)**
  - Add `task_compliance: "false"` to the `implement-task` params in:
    - `workflows/core/implement-change-v1.0.yaml`
    - `workflows/core/complete-simple-change-v1.0.yaml`
    - `workflows/spec-driven/implement-change-v1.0.yaml`
    - `workflows/openspec/implement-change-v1.0.yaml`
  - Remove the `task_file` param from the `run-validator` calls in `workflows/spec-driven/simple-change-v1.0.yaml` and `workflows/openspec/simple-change-v1.0.yaml`.
  - In `core/implement-change`'s `complete-task-index` prompt, replace ", and, unless `skip_validator` is `true`, task-compliance validation." with ", and, unless `skip_validator` is `true`, a validator run." The new prompt must not claim that validation passed: final verification is non-blocking (`warn_on_failure`).

  **External-delivery note**
  - In `workflows/core/verify-task-commit.sh`, change the note to `note: this run's validator did not cover the external work`.
  - Update the assertions on that string in `internal/exec/implement_task_workflow_test.go` and `workflows/verify_task_commit_test.go`.

  **Unchanged**
  - `cmd/agent-runner/internal_cmd.go` (`--enable-builtin task-compliance`) and its tests.
  - The disabled `task-compliance` entry in `.validator/config.yml`.
  - Everything under `.agent-runner/workflows/`, which is the factory's.
  - Runner Go code outside tests.

  **Docs**
  - Update `docs/built-in-workflows.md` wherever it implies that the per-task workflows run task-compliance. Keep the `onboarding:validator` scaffolding sentence.

  **Tests**
  - Add the tests listed under "Testing Strategy" in `design.md`:
    - `run-validator.sh` argument table cases and the updated injection test in `workflows/onboarding_scripts_test.go`;
    - `check-task-compliance.sh` script tests;
    - `embed_test.go` structure checks for `run-validator` and `implement-task`;
    - the catalog-walk pin: every embedded `implement-task` call passes `task_compliance: "false"`, and no embedded `run-validator` call opts in;
    - the v1.0 simple-change calls carry no `task_file`;
    - the `complete-task-index` prompt neither mentions task-compliance nor claims validation passed.
  - Add the integration tests:
    - INT-001 and INT-003 in `internal/exec`, against the real built-in `implement-task` and `run-validator` YAML. `run-validator.sh` and `check-task-compliance.sh` run for real against an argv-recording fake `agent-validator`, and `AGENT_RUNNER_EXECUTABLE` must implement `internal json-value`.
    - INT-002 in `internal/runner`, using `newPlanAndImplementFixture`, including the variant where the validator fails every attempt so final verification only warns.

  Finish with `make fmt`, `make test` and `make lint` passing.

  Source files:
  - [proposal.md](proposal.md)
  - [specs/task-compliance-activation/spec.md](specs/task-compliance-activation/spec.md)
  - [specs/task-delivery-gate/spec.md](specs/task-delivery-gate/spec.md)
  - [design.md](design.md)
  - [test-plan.md](test-plan.md)
  - [decisions.md](decisions.md)
