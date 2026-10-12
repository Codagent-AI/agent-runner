## Context

Today, task-compliance is activated implicitly. `workflows/core/run-validator.sh` adds
`--enable-review task-compliance --context-file <task_file>` whenever its `task_file` input is
non-empty. `core/run-validator` forwards `task_file` to that script from both of its validator steps:
`validator-retry/run-validator`, which sits inside a three-iteration loop with
`continue_on_failure: true` and a `fix-violations` repair agent, and `verify-final-validator`.

Every per-task built-in workflow passes `task_file` into `run-validator`, so every per-task
validation runs the review:

| Caller | Reaches `run-validator` via | Reached from |
| --- | --- | --- |
| `core/implement-change-v1.0` | `implement-task` (loop `implement-single-task`) | v2.0 `spec-driven`/`openspec` `change` and `implement-change`, `core/plan-and-implement-change` |
| `core/complete-simple-change-v1.0` | `implement-task` (step `implement`) | v2.0 `spec-driven`/`openspec` `simple-change` |
| `spec-driven/implement-change-v1.0` | `implement-task` (loop step) | v1.0 `spec-driven/change`, pinned children, resume |
| `openspec/implement-change-v1.0` | `implement-task` (loop step) | v1.0 `openspec/change`, pinned children, resume |
| `spec-driven/simple-change-v1.0` | `run-validator` directly, `task_file: "{{task_file}}"` | direct launch |
| `openspec/simple-change-v1.0` | `run-validator` directly, `task_file: openspec/changes/<name>/tasks.md` | direct launch |
| Agent Factory `factory-feature-v1.0` (external) | `builtin:core/implement-task-v1.0.yaml` | factory |

The Agent Factory workflow lives in another repository and must not change. It omits any
activation param. Its separate post-verify gate, `task-compliance-gate.py`, calls `agent-validator
review --gate task-compliance --enable-review task-compliance` directly. That gate only needs the
target repository's validator config to declare the review, which the `validator-init` scaffolding
and the `.validator/config.yml` entry provide. Both stay unchanged.

Sub-workflow params are resolved from the calling step's `params` map each time the child launches
(`internal/exec/subworkflow.go` `resolveParams`). Undeclared params fall back to their declared
defaults.

## Goals / Non-Goals

**Goals:**
- Make task-compliance activation an explicit, validated workflow param that is separate from
  `task_file`.
- Keep the factory's direct `implement-task` call behaving exactly as it does today.
- Disable the review in every embedded built-in caller, across every shipped version, and stop
  them from depending on the `implement-task` default.
- Make the external-delivery note accurate whether or not the review ran.

**Non-Goals:**
- No changes to Runner Go code, the loader, or the validator-init command.
- No changes to the `fix-violations` task-compliance guidance, which the factory path still uses.
- No new workflow versions. Edits go into the existing `v1.0` definitions, as previous fixes did.

## Approach

### `core/run-validator` (v1.0)

- Add param `task_compliance` with `required: false` and `default: "false"`.
- Add a new top-level step `check-task-compliance` before `validator-retry`. It runs a new script,
  `workflows/core/check-task-compliance.sh`, with
  `script_inputs: {task_compliance: "{{task_compliance}}", task_file: "{{task_file}}"}`. The step
  has no `continue_on_failure`, so a failure stops the workflow before any validator command or
  repair agent runs. The script reads the JSON payload the same way `validate-boolean-param.sh`
  does (jq, with a sed fallback) and:
  - exits 1 with `task_compliance must be true or false, got: <value>` unless the value is `true`
    or `false`;
  - exits 1 with `task_compliance is true but task_file is empty` when the value is `true` and
    `task_file` is empty;
  - otherwise exits 0 with no output.
- Pass `task_compliance: "{{task_compliance}}"` as an additional `script_inputs` entry to both
  `run-validator.sh` steps.

### `run-validator.sh`

- Read `task_compliance` with `"$AGENT_RUNNER_EXECUTABLE" internal json-value task_compliance`,
  as it already does for `task_file`.
- Change the activation condition from `[ -n "$task_file" ]` to
  `[ "$task_compliance" = true ] && [ -n "$task_file" ]`. Any other value adds no flags. The guard
  step catches invalid values earlier, but the script must never activate on anything except an
  exact `true`.
- Leave the metrics-correlation block and the `result_file` handling unchanged.

### `core/implement-task` (v1.0)

- Add param `task_compliance` with `required: false` and `default: "true"`.
- Add step `validate-task-compliance` right after `validate-skip-validator`, which means before
  `check-task-file` and `generate-code`. It reuses `validate-boolean-param.sh` with
  `{name: task_compliance, value: "{{task_compliance}}"}`.
- In the `run-validator` sub-workflow call, add `task_compliance: "{{task_compliance}}"` next to
  `task_file`. The `skip_if` on `skip_validator` is unchanged, so a skipped validator runs no
  command whatever the `task_compliance` value.
- Update `description:` so it no longer mentions task-compliance retries unconditionally. For
  example: "Implement a single task, require committed progress, and run validation (optionally
  with task-compliance)."

### Non-factory callers

- `core/implement-change-v1.0`, `core/complete-simple-change-v1.0`,
  `spec-driven/implement-change-v1.0` and `openspec/implement-change-v1.0` add
  `task_compliance: "false"` to their `implement-task` params.
- `spec-driven/simple-change-v1.0` and `openspec/simple-change-v1.0` drop the `task_file` param
  from their `run-validator` call. `task_compliance` then defaults to `false`, and the call matches
  every other plain caller.
- `core/implement-change`'s `complete-task-index` prompt replaces ", and, unless `skip_validator`
  is `true`, task-compliance validation." with ", and, unless `skip_validator` is `true`, a
  validator run." The new wording is outcome-neutral on purpose. `verify-final-validator` uses
  `warn_on_failure`, and `validator-retry-verification` ("Unresolved final verification is
  non-blocking and visible") lets the parent continue after final verification fails. So
  `complete-task-index` can be reached with unresolved validator failures, and the prompt must not
  tell the lead agent that validation passed. The non-blocking warning policy itself is unchanged.

### `verify-task-commit.sh`

- Change the note to `note: this run's validator did not cover the external work`.
- Update both tests that assert the old string: `internal/exec/implement_task_workflow_test.go:422`
  and `workflows/verify_task_commit_test.go:338`.

### Documentation

- Update `docs/built-in-workflows.md` if any text says the per-task workflows run task-compliance.
  Keep the `onboarding:validator` sentence about scaffolding.

## Decisions

- **Guard step before the loop, not inside `run-validator.sh`.** The script runs inside a loop with
  `continue_on_failure: true`, so a non-zero exit there would hand an invalid-param error to the
  `fix-violations` agent as if it were a validator failure. A top-level guard fails fast and
  deterministically. The script still keeps an exact-`true` condition as defense in depth.
- **A dedicated `check-task-compliance.sh` instead of two steps.** One script checks both rules
  (boolean, and non-empty `task_file` when enabled) from a JSON payload. That avoids interpolating
  `task_file` into a `command:` string, where quoting is fragile.
- **Drop `task_file` from the v1.0 simple-change `run-validator` calls instead of passing `false`.**
  `task_file` is only meaningful together with an opt-in. Removing it leaves one plain-validation
  shape for callers that don't use task-compliance. An explicit `task_compliance: "false"` would
  also satisfy the spec, but it would leave a dead param in the call.
- **`implement-task` defaults to `true`.** The factory calls it without the param and must not
  change. Every embedded caller states its setting explicitly, so the default only reaches
  external callers.
- **Edit the v1.0 definitions in place.** This matches how the repository has fixed built-in
  workflows before. `workflow-versioning` loads recorded versions by reference. A resumed run picks
  up the new content, and with these edits that content disables the review, which is the intended
  outcome.

## Risks / Trade-offs

- **A future built-in caller forgets the param** and silently gets task-compliance. To mitigate
  this, a catalog-walk test in `workflows/embed_test.go` follows the
  `TestEveryImplementTaskPromptProhibitsAgentRunValidator` pattern: it fails for any embedded step
  whose `workflow:` ends in `core/implement-task-v1.0.yaml` or `implement-task-v1.0.yaml` without
  `task_compliance: "false"`. A sibling assertion fails for any embedded call to
  `run-validator-v1.0.yaml` that sets `task_compliance: "true"`.
- **External callers of `core/run-validator` that pass `task_file` to get the review** lose it
  silently. `run-validator` is `hidden: true` and the factory never calls it with `task_file`, so
  no known caller is affected. The REMOVED requirement's migration note documents the opt-in.
- **Resumed in-flight runs.** A run that resumes inside `implement-task` after upgrading
  re-resolves its params from the parent step. Non-factory runs switch to plain validation on
  their next validator step, which is harmless and intended.

## Testing Strategy

- `workflows/onboarding_scripts_test.go`, which already runs `run-validator.sh` against fake
  `agent-runner`/`agent-validator` binaries:
  - a table test covering `task_compliance` true with `task_file` set (flags present), omitted
    with `task_file` set (flags absent), false with `task_file` set (flags absent), and
    true/false combined with the metrics environment (correlation flags present either way);
  - the existing literal-task-file injection test, updated to set `task_compliance: true` so it
    still exercises `--context-file`.
- A script test for `check-task-compliance.sh` covering valid `true`/`false`, invalid values, and
  `true` with an empty `task_file`.
- `workflows/embed_test.go`:
  - the `run-validator` structure: param defaults, the guard step placed before
    `validator-retry`, and `task_compliance` forwarded to both script steps;
  - the `implement-task` structure: param default `true`, the validation step placed before
    `generate-code`, and `task_compliance` forwarded to `run-validator`;
  - the catalog-walk pin for every `implement-task` caller, and the `run-validator` opt-in check;
  - the v1.0 simple-change `run-validator` calls carrying no `task_file`;
  - the `complete-task-index` prompt not containing `task-compliance`, and not claiming that
    validation passed (for example, no "passed validation" or "passed the validator");
  - an executor-level check in `internal/runner` using the plan-and-implement fixture. A fake
    validator fails every attempt, so final verification only warns. The run must still reach
    `complete-task-index`, and the prompt dispatched there must not say that validation passed.
- `internal/exec/implement_task_workflow_test.go`: an existing executor-level test drives
  `implement-task` with fake processes, with a stub validator that emits `REVIEW
  task-compliance`. Extend it to assert the issued validator command contains the activation flags
  when the param is omitted and omits them for `task_compliance: "false"`.
- Update the two assertions on the external-delivery note string.
- Run the targeted packages (`go test ./workflows ./internal/exec`), then `make test` and
  `make lint`.

## Migration Plan

No data migration is needed. The change ships with the embedded workflows. To roll back, revert the
commit. `validateSubWorkflowParams` fills in declared defaults and ignores undeclared extra params,
so a caller that still passes `task_compliance` to an older `implement-task` keeps working there.
