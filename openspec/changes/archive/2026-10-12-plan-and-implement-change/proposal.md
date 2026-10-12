## Why

The Agent Evals `and-scene` suite is moving its starting line from a fixture with hand-written tasks to one with only definition artifacts (Codagent-AI/agent-evals#87). It needs to run task planning and then implementation as a single Agent Runner run: one run id, one resume target, and one `run-metrics.json`. No built-in does that today. `openspec:change` always creates and interactively defines the change first, and running `core:plan-change` then `core:implement-change` produces two unrelated runs. The eval's `--skip-validator` mode must also skip every workflow-owned Validator path, but `core:plan-change` always runs `agent-validator skip` when it commits the plan.

## What Changes

- Add the hidden built-in `core:plan-and-implement-change` (`workflows/core/plan-and-implement-change-v1.0.yaml`).
  - It has two top-level steps: `plan`, which invokes `plan-change-v1.0.yaml`, then `implement`, which invokes `implement-change-v1.0.yaml`.
  - Its parameters are the union of theirs.
- Add an optional `skip_validator` parameter to `core:plan-change`, defaulting to `false`. Validate it as a boolean and pass it to `commit-change-plan.sh`.
  - When it is `true`, the script commits the plan without running `agent-validator skip`.
  - Existing callers that omit it behave exactly as before.
- Cover both with embed and workflow tests, and list the new workflow in `docs/built-in-workflows.md`.

Two open Runner pull requests rewrite `plan-change-v1.0.yaml`, and whichever lands later must merge with the others:

- #209 moves definition checking into a `check-definition-v1.0.yaml` sub-workflow. `skip_validator` validation must still run before definition checking.
- #228 adds `commit_plan` and gates `commit-plan` with `skip_if: 'sh: test {{change_kind}} = spec-driven || test {{commit_plan}} = false'`. Pass `skip_validator` to the commit step without disturbing that condition.

This change lands first. Agent Evals change `start-from-definition` depends on it reaching Runner `main`. Agent Factory change `eval-crosscheck-role` follows that.

## Capabilities

### New Capabilities
None.

### Modified Capabilities
- `builtin-workflows`: adds the `core:plan-and-implement-change` workflow and Validator skipping for `core:plan-change`.

## Out of Scope

- Any change to the steps of `plan-change`, `review-tasks`, `implement-change`, or `verify-change`, other than passing `skip_validator` through the plan commit.
- An `openspec:` wrapper or a start-at-plan option for `openspec:change`.
- The orchestrated implement-change workflow on `claude/implement-change-workflow-v1-apg8xp`, and a future change-v3 workflow.

## Impact

- New file: `workflows/core/plan-and-implement-change-v1.0.yaml`.
- Edited files:
  - `workflows/core/plan-change-v1.0.yaml`: new parameter, validation step, and `skip_validator` input to the commit step.
  - `workflows/core/commit-change-plan.sh`: skips both `agent-validator skip` calls, the already-committed path and the post-commit path, only when `skip_validator` is `true`. Its other caller, `workflows/core/commit-change-plan-v1.0.yaml`, used by `openspec/simple-change` v1 and v2, passes no value and is unaffected.
- Tests: `workflows/embed_test.go`, plus the workflow and shell tests that cover `commit-change-plan.sh`.
- Docs: `docs/built-in-workflows.md`.
- `core:plan-and-implement-change` uses the `lead`, `crosscheck`, `implementor`, and `tester` agents, so a caller's profile must define all four.
