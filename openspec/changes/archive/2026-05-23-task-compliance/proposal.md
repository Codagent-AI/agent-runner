## Why

Agent Validator's `task-compliance` built-in review never runs during agent-runner's implementation workflows (issue #34). Two compounding regressions cause this:

- **Runtime:** `workflows/core/run-validator.yaml` invokes `agent-validator run --report` with no opt-in flags. The `--enable-review task-compliance` flag that used to be present was removed in commit `b9131a6` as collateral damage in an unrelated fix; `--context-file <task_file>` was never wired up in the first place.
- **Install time:** Per agent-validator's docs, `--enable-review <name>` only activates a review that is already defined in `.validator/config.yml`. The entry for `task-compliance` was also removed in `b9131a6`, and nothing in agent-runner's onboarding or `validator-init` ensures it exists in consumer projects.

The agent-validator side of the install-time fix has already landed (commit `fc01607` upstream): `agent-validator init --enable-builtin task-compliance` now scaffolds the entry. This change wires the agent-runner side end-to-end.

## What Changes

- `workflows/core/run-validator.yaml` gains an optional `task_file` param (default `""`). When set, the validator command is `agent-validator run --report --enable-review task-compliance --context-file "{{task_file}}"`; when unset, the command stays `agent-validator run --report`. The conditional lives inline in the YAML command as a shell `if` ("Option α" from planning).
- `workflows/core/implement-task.yaml` propagates its `task_file` param to the `run-validator` sub-workflow call.
- `cmd/agent-runner/internal_cmd.go` `validatorInitArgs` returns `["init", "--enable-builtin", "task-compliance"]` so any project running agent-runner onboarding gets the config entry scaffolded.
- `.validator/config.yml` (this repo) restores the `task-compliance` review entry under the root entry point with `enabled: false` and the activation comment that was present before `b9131a6`.
- `docs/LOOPS-AND-SUBWORKFLOWS.md` restores the example showing `--enable-review task-compliance` (also removed in `b9131a6`).
- Existing callers of `run-validator` that don't have a task file (`spec-driven/implement-change`, `openspec/implement-change`, `spec-driven/simple-change`, `openspec/simple-change`, `onboarding/validator`) are unchanged and continue to work — the new param is optional and defaults to skipping task-compliance.

## Capabilities

### New Capabilities

- `task-compliance-activation`: how agent-runner activates agent-validator's opt-in `task-compliance` review through workflow params and the install-time `--enable-builtin` flag.

## Out of Scope

- The agent-validator-side change (already landed upstream as `fc01607`).
- Wiring agent-validator's other opt-in built-in `test-integrity`. The `--enable-builtin` flag is variadic and supports it, but no agent-runner workflow currently consumes it; that's a future change if a workflow needs it.
- A migration path for existing projects whose `.validator/` was scaffolded before `agent-validator` had the `--enable-builtin` flag. Per the agent-validator change, init prints a "paste this into your config" warning in that case; no further automation is planned.
- Pinning a minimum agent-validator version in code. If an older binary is installed, `agent-validator init --enable-builtin task-compliance` will fail with an unknown-option error from the binary itself. Documented at release time, not enforced in code.
- Any other changes to validator activation semantics. `--enable-review` and `--context-file` already behave as documented; this change only ensures the flags reach the binary.

## Impact

- **Code:**
  - `cmd/agent-runner/internal_cmd.go` — `validatorInitArgs` returns the new flag list.
  - `cmd/agent-runner/internal_cmd_test.go` — update the test for `validatorInitArgs`.
- **Workflows (embedded into the binary):**
  - `workflows/core/run-validator.yaml` — `task_file` param + conditional command.
  - `workflows/core/implement-task.yaml` — propagate `task_file` to sub-workflow.
- **Repo config:**
  - `.validator/config.yml` — restore `task-compliance` entry under the root entry point's `reviews:`.
- **Docs:**
  - `docs/LOOPS-AND-SUBWORKFLOWS.md` — restore the `--enable-review task-compliance` example in the run-validator sub-workflow snippet.
- **Tests:** TDD per project conventions for the Go change. Workflow YAML changes are config-only and follow the project's no-tests-required-for-YAML rule.
- **No new dependencies.** Cross-repo dependency on agent-validator with the `--enable-builtin` flag (landed upstream in `fc01607`).
