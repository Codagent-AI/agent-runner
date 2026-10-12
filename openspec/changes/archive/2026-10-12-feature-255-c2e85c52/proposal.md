## Why

The and-scene eval compared Agent Runner's baseline workflows with a variant that adds the
task-compliance validator review ("Eval: baseline plus task-compliance validator review, 3
repetitions"). On the rescored runs (rubric 15.0.0, three rescores each) the baseline averaged
58.0/70 and the task-compliance variant 56.4/70. The review showed no quality benefit, yet it adds
an extra model review to every per-task validator run in the built-in workflows, costing tokens,
wall-clock time and repair loops for users who run Agent Runner directly.

The Agent Factory still relies on task-compliance: its own post-verify gate
(`task-compliance-gate.py`) and its feature workflow's `implement` step, which calls
`builtin:core/implement-task-v1.0.yaml` with the change's `tasks.md`. The change must drop the
review from the workflows people run directly without removing it from the factory, and without
needing any change in the agent-factory repository.

Evaluation verdict: **go with caveats**. The change is small and backed by eval evidence. The
caveat is that `core/implement-task` is shared by direct users and the factory, so activation has
to become an explicit choice made by the caller instead of following implicitly from `task_file`.

## What Changes

- `core/run-validator` no longer enables task-compliance just because `task_file` is non-empty.
  Activation becomes an explicit opt-in on the sub-workflow call. Callers that don't opt in run
  `agent-validator run --report` (plus the metrics correlation flags) with no
  `--enable-review task-compliance` or `--context-file`.
- `core/implement-task` gets an optional boolean param that controls task-compliance. It defaults
  to enabled, so the factory's direct `builtin:core/implement-task-v1.0.yaml` call keeps today's
  behavior unchanged.
- Every non-factory embedded caller of `implement-task`, in every shipped version, passes the
  switch as disabled:
  - `core/implement-change`, which the v2.0 `spec-driven`/`openspec` `change` and
    `implement-change` workflows and `core/plan-and-implement-change` reach;
  - `core/complete-simple-change`, which the v2.0 `simple-change` workflows reach;
  - the v1.0 `spec-driven/implement-change` and `openspec/implement-change` workflows, which call
    `implement-task` directly. They stay loadable as pinned children and recorded resume targets
    under `workflow-versioning`, and the v1.0 `change` workflows reach them.

  The v1.0 `spec-driven/simple-change` and `openspec/simple-change` workflows call
  `run-validator` directly and stop requesting activation.
- Prompt and report text that promises a task-compliance review in non-factory runs is reworded
  to match. This covers `core/implement-change`'s `complete-task-index` wording and the
  external-delivery note in `verify-task-commit.sh`, which becomes a statement that holds whether
  or not the review ran.
- Not removed:
  - the `fix-violations` instruction about task-compliance violations for work delivered in
    another repository, which the factory path still needs;
  - `agent-runner internal validator-init`'s `--enable-builtin task-compliance` scaffolding;
  - the disabled `task-compliance` entry in this repository's `.validator/config.yml`.

  The factory's gate needs the target repository to declare the review, and a declared review
  stays inert unless a caller activates it.

There is no **BREAKING** change to the public interface or persisted data. Workflow param
additions are optional. The only change users see is that direct runs no longer perform the
review.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `task-compliance-activation`: activation in `core/run-validator` becomes an explicit opt-in
  instead of following from `task_file`. The "Per-task workflows propagate task_file"
  requirement is replaced: only `core/implement-task` activates by default, through a
  caller-controllable param, and `spec-driven/simple-change`, `openspec/simple-change`, and the
  non-factory callers of `implement-task` no longer activate. The validator-init scaffolding and
  repo config requirements stay.
- `task-delivery-gate`: the external-delivery report no longer claims that a task-compliance
  review ran. Its wording has to be true both when the review ran and when it was skipped.
- `validator-metrics-delivery`: no requirement change. Correlation flags stay independent of
  task-compliance arguments on both the opted-in and the default path. Listed only so the design
  confirms the existing scenario still holds.

## Technical Approach

The change stays inside the workflow layer: the YAML in `workflows/core/`, `workflows/spec-driven/`
and `workflows/openspec/`, plus the `run-validator.sh` script. Runner code is unchanged.

- Decouple activation from `task_file` in `run-validator.sh` and `core/run-validator` with an
  explicit boolean input, validated with the existing `validate-boolean-param.sh` pattern. Keeping
  `task_file` as the context source while adding a separate switch avoids overloading one param
  with two meanings.
- `core/implement-task` defaults the switch to on and forwards it, together with `task_file`, to
  `run-validator`. Defaulting on is the decision that preserves the factory's behavior without an
  agent-factory change. `implement-task` is `hidden: true`, so the callers that matter are the
  embedded ones. Every embedded caller across all shipped workflow versions passes the switch
  explicitly. The only caller left on the default is the external factory workflow.
- The factory's own `task-compliance-gate.py` calls `agent-validator review --gate
  task-compliance --enable-review task-compliance` directly and only needs the review to be
  declared in the target's config. Keeping validator-init scaffolding and the repo config entry
  preserves that.

Risk: a future built-in caller of `implement-task` that forgets to pass the switch would silently
get task-compliance. The existing workflow tests reduce this risk by inventorying every embedded
caller of `implement-task` and pinning the disabled setting for each one.

## Out of Scope

- Any change in the agent-factory repository, including its `factory-feature-v1.0.yaml`,
  `task-compliance-gate.py`, or how the factory consumes task-compliance results.
- Removing the task-compliance builtin from agent-validator, or removing its scaffolding and
  config declarations.
- Removing or changing the factory-oriented `fix-violations` guidance for task-compliance
  violations about work delivered in another repository.
- Rewriting archived OpenSpec changes.
- Other validator reviews and the eval harness itself.

## Impact

- Code: `workflows/core/run-validator.sh`, `workflows/core/run-validator-v1.0.yaml`,
  `workflows/core/implement-task-v1.0.yaml`, `workflows/core/implement-change-v1.0.yaml`,
  `workflows/core/complete-simple-change-v1.0.yaml`, `workflows/spec-driven/implement-change-v1.0.yaml`,
  `workflows/openspec/implement-change-v1.0.yaml`, `workflows/spec-driven/simple-change-v1.0.yaml`,
  `workflows/openspec/simple-change-v1.0.yaml` and `workflows/core/verify-task-commit.sh`, plus
  their tests (`internal/exec/implement_task_workflow_test.go`, `workflows/verify_task_commit_test.go`
  and related workflow tests).
- `cmd/agent-runner/internal_cmd.go` is unchanged: validator-init keeps `--enable-builtin
  task-compliance`.
- Specs: `task-compliance-activation` and `task-delivery-gate`. `docs/built-in-workflows.md` gets a
  check for wording that implies direct runs perform task-compliance.
- Users: direct `spec-driven`/`openspec` implement-change and simple-change runs get faster and
  cheaper per-task validation with no task-compliance repair loops. Factory runs are unchanged.
