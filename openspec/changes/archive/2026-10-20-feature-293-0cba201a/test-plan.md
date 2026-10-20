## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

The risk is in wiring, not logic:

- a caller-supplied param must survive loading, param resolution (including sub-workflow resolution, which keeps an explicit `""`), and one-pass interpolation, and arrive in the right agent invocation and no other;
- a blank value must be stopped by a real subprocess script before any agent starts;
- the script's no-`jq` fallback must agree with its `jq` path on every JSON encoding of ASCII whitespace (including `\f` and `\u000b`) and must not treat literal backslash text as whitespace.

These obligations use opaque sentinel values and the workflow's own declared default, read from the loaded workflow, as markers. They assert where a value is delivered, never what the product prompt says. Unit-level checks of the workflow shape (param list, step order, validation-step inputs) follow from the specs and design and are not inventoried here.

All tests go in existing suites run by the `test` job's `go test ./...`. No new suites or CI jobs, and nothing under `.github/workflows` changes.

## Integration Tests

### INT-001: Caller scope value reaches only the acceptance-fix agent invocation
- **Covers:** builtin-workflows "Verify-change workflow". Scenarios: "Caller narrows the acceptance-fix scope" and "Scope affects only the acceptance-fix step".
- **Boundary:** the real embedded `core:verify-change` loaded with `loader.LoadWorkflow`, run through `DispatchStep` with real interpolation and real bundled scripts (`acceptance-gate.sh`). The process runner fakes only the agents.
- **Setup:** the existing `runVerifyChangeAcceptance` harness in `internal/exec/verify_change_workflow_test.go`, extended to fill omitted params from the loaded workflow's declared defaults and to accept an `acceptance_fix_scope` value. Use a throwaway Git repo. The fake tester reports not-ready in round 1 and ready in round 2, so exactly one `acceptance-fix` runs. The fake runner records each agent invocation's step prefix and args.
- **Action:** run the acceptance tail with `acceptance_fix_scope` set to a unique sentinel such as `SCOPE-SENTINEL-7f3a fix only what this change broke`.
- **Assertions:**
  - The `acceptance-fix` invocation's args contain the sentinel and do not contain the loaded workflow's declared default value for the param.
  - No other agent invocation (the acceptance tester, any repair) contains the sentinel.
  - The run reaches `ACCEPTANCE_COMPLETE`.
- **Execution:** `go test ./internal/exec -run TestBuiltinVerifyChange` (the `test` job's `go test ./...`).

### INT-002: Omitted scope delivers the declared default
- **Covers:** builtin-workflows "Verify-change workflow". Scenario: "Default acceptance-fix scope is unchanged".
- **Boundary:** same as INT-001.
- **Setup:** same harness. The params map omits `acceptance_fix_scope`, as existing callers do.
- **Action:** run the acceptance tail through one fix round.
- **Assertions:**
  - The `acceptance-fix` invocation's args contain the declared default value read from the loaded workflow's `Params`.
  - Interpolation raises no unresolved-placeholder error.
  - The existing acceptance-round cases (`TestBuiltinVerifyChangeAcceptanceRounds`) still produce their current event sequences unchanged.
- **Execution:** `go test ./internal/exec -run TestBuiltinVerifyChange`.

### INT-003: Blank scope fails at validation before any agent step
- **Covers:** builtin-workflows "Verify-change workflow". Scenario: "Empty acceptance-fix scope is rejected".
- **Boundary:** the real `validate-nonempty-param.sh` run as a subprocess by the script executor, with JSON stdin built by `buildScriptInput`, inside the dispatched `core:verify-change` head.
- **Setup:** the loaded workflow's steps from `validate-change-name` through `review-assumptions`, dispatched with a fake runner that executes scripts for real and fails the test on any agent invocation. Table: `""`, `"   "`, `"\n\t "`, a form-feed-only value, a vertical-tab-only value, and a valid value as the control. Run the blank rows under the host `PATH` and under the no-`jq` `PATH` from INT-005.
- **Action:** dispatch the head group with each value.
- **Assertions:**
  - For each blank value, the outcome is failed, no agent invocation happened, and the failing script's stderr names `acceptance_fix_scope`.
  - For the valid value, validation passes and `review-assumptions` is invoked.
- **Execution:** `go test ./internal/exec -run TestBuiltinVerifyChange`.

### INT-004: Blank scope passed through a sub-workflow call is rejected, not defaulted
- **Covers:** builtin-workflows "Verify-change workflow". Scenario: "Empty acceptance-fix scope is rejected", on the composition path Agent Factory uses (`workflow: builtin:core/verify-change-v1.0.yaml` with `params:`).
- **Boundary:** sub-workflow param resolution (`validateSubWorkflowParams` keeps an explicit `""`), then the verify-change validation steps.
- **Setup:** a minimal parent workflow in the test's temp dir with one step calling `builtin:core/verify-change-v1.0.yaml`. It passes the required params plus `acceptance_fix_scope: ""`. The fake runner executes scripts for real and fails the test on any agent invocation.
- **Action:** dispatch the parent workflow's sub-workflow step.
- **Assertions:** the sub-workflow fails at `validate-acceptance-fix-scope`, no agent invocation happens, and the error output names `acceptance_fix_scope`.
- **Execution:** `go test ./internal/exec` (the `test` job).

### INT-005: Validation script agrees with and without `jq`
- **Covers:** builtin-workflows "Verify-change workflow": the param is accepted only when non-blank, on hosts with and without `jq`.
- **Boundary:** the shipped `validate-nonempty-param.sh` run as a subprocess under two `PATH`s: the host `PATH`, and a no-`jq` `PATH` built from symlinks to the required tools, following the existing `no-jq` pattern in `workflows/ci_status_gate_test.go`.
- **Setup:** JSON payloads produced with `json.Marshal`, matching the executor. Values:
  - `""`, spaces only, newline/tab/CR only;
  - plain text;
  - text with embedded `"` and `\`;
  - text starting with an escaped quote;
  - multi-line text;
  - a value containing the literal substring `"value":""`;
  - blank: form feed only (`\f`), vertical tab only (`\u000b`), and all six ASCII whitespace characters mixed;
  - non-blank literal escape text typed as characters: `\f`, `\u000b`, `\\`, and a lone backslash;
  - non-ASCII whitespace alone: U+00A0 and U+2028 (non-blank under the spec's predicate).
- **Action:** run the script with each payload under each `PATH`.
- **Assertions:**
  - The exit code is 1 for every blank value and 0 for every non-blank one, identically under both `PATH`s. The table records the expected result for each row, so a branch that disagrees with the spec fails even if both branches agree with each other.
  - On failure, stderr names the param.
- **Execution:** `go test ./workflows` (the `test` job). Skip only the `jq` half when `jq` is absent on the host.

## End-to-End Tests

None. An end-to-end run of `core:verify-change` needs real agent sessions, a GitHub remote, and a draft PR. The only new behavior beyond INT-001 to INT-005 is what an agent does with a narrower instruction, which no stable assertion can check. The loader, param resolution, interpolation, script subprocesses, and dispatch are all real in the integration tests above. Agent Factory's follow-up change exercises the real caller journey.

## Acceptance Testing Envelope

- **Environments and sandboxes:**
  - The local checkout run through `./dev.sh`. `./dev.sh debug --show-workflow core:verify-change` inspects the embedded workflow.
  - Throwaway Git repositories and parent workflow files under `{{session_dir}}/scratch/acceptance-test`.
  - An isolated `HOME` for any run.
- **Credentials and secrets:**
  - Real agent CLIs (Claude, Codex) may be authenticated on the host. The pass may use them for at most a small headless run of a scratch parent workflow that calls `core:verify-change` with a blank or sentinel scope. The blank case fails before any agent starts.
  - No GitHub credential may be used to create, push, or modify remote branches or pull requests.
- **Authorized effects:**
  - Local files under scratch and the isolated `HOME`, removed after the pass.
  - At most a few US dollars in model cost.
- **Off limits:**
  - Pushing to any remote, creating or editing pull requests, and any `gh` mutation.
  - Editing anything under `.github/workflows`.
  - Editing Agent Factory workflows (`.agent-runner/workflows/*`).
  - Running or replaying real factory runs in `~/.agent-factory`.
- **Permitted substitutes:**
  - The fake process runner and fake `gh` used by the existing verify-change tests, when a real-agent run is impractical. Stopping a scratch run right after validation, or inspecting the resolved step through `debug`, is enough to observe the param wiring.
- **Known risk areas:**
  - Test harnesses that dispatch the workflow tail with hand-built params bypass default resolution. A new placeholder fails to interpolate there unless defaults are filled.
  - Explicit `""` from a sub-workflow caller is kept, not defaulted, which is why the blank check exists.
  - The `sed` fallback in validation scripts when `jq` is missing, especially escapes beyond `\n`, `\t`, and `\r` (`\f`, `\u000b`) and literal backslash text (approach review AR-001).
  - Accepted limitations:
    - A caller value containing `{{…}}` is inserted literally, not re-interpolated.
    - Rolling back the change makes a caller-passed scope silently fall back to the default, because undeclared params are not rejected.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| builtin-workflows: Verify-change workflow — Default acceptance-fix scope is unchanged | INT-002 | — | — |
| builtin-workflows: Verify-change workflow — Caller narrows the acceptance-fix scope | INT-001 | — | — |
| builtin-workflows: Verify-change workflow — Scope affects only the acceptance-fix step | INT-001 | — | — |
| builtin-workflows: Verify-change workflow — Empty acceptance-fix scope is rejected | INT-003, INT-004, INT-005 | — | — |
