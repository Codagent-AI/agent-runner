# Decisions

## propose

### Verdict: go with caveats
- Decision: Proceed. Eval evidence (58.0 vs 56.4 of 70) shows no benefit from per-task task-compliance in direct runs, and the change is confined to workflow YAML and scripts.
- Alternatives considered: no-go (keep the review everywhere), which contradicts the issue.
- Decision-bearing: yes

### Preserve the factory path by defaulting `core/implement-task` to task-compliance on
- Decision: Separate activation from `task_file` with an explicit switch. `core/implement-task` defaults it to on, so the factory's direct `builtin:core/implement-task-v1.0.yaml` call keeps its behavior, and every non-factory built-in caller (`core/implement-change`, `core/complete-simple-change`, `spec-driven/simple-change`, `openspec/simple-change`) turns it off.
- Alternatives considered: (a) remove activation from `implement-task` entirely, which drops the factory's implement-phase review against the issue's scope; (b) default off and have the factory opt in, which needs an agent-factory change outside the target repository; (c) infer factory context from the environment, which is implicit and fragile.
- Decision-bearing: yes

### Keep validator-init scaffolding and the repo config declaration
- Decision: Keep `--enable-builtin task-compliance` in `internal validator-init` and the disabled `task-compliance` entry in `.validator/config.yml`.
- Alternatives considered: remove both as part of "removing task-compliance". Rejected because the factory's `task-compliance-gate.py` reports `not-declared` when the target repository doesn't declare the review, and a declared-but-disabled entry does nothing unless a caller activates it.
- Decision-bearing: yes

### Keep the `fix-violations` external-delivery guidance in `core/run-validator`
- Decision: Keep it. The factory path still produces task-compliance violations that this guidance handles.
- Alternatives considered: remove it with the activation, which would regress the factory.
- Decision-bearing: no

### Reword the external-delivery note in `verify-task-commit.sh`
- Decision: Reword the note so it no longer claims a task-compliance review ran. The new text must be accurate whether or not the review ran.
- Alternatives considered: make the note depend on activation, which means threading the switch into the delivery script; this can be decided in design.
- Decision-bearing: no

## proposal-review

### PR-001: the non-factory caller inventory missed the v1.0 implement-change workflows (applied)
- Decision: Applied. Verified that `workflows/spec-driven/implement-change-v1.0.yaml:26` and `workflows/openspec/implement-change-v1.0.yaml:21` call `core/implement-task-v1.0.yaml` directly. They are still reachable through the v1.0 `change` workflows, as pinned children, and as recorded resume targets. The proposal now lists every embedded caller across versions and passes the switch as disabled in both, keeps the enabled default for the factory's unchanged call, and pins each caller's setting in the existing workflow tests.
- Alternatives considered: rely on the v2.0 entry points delegating to `core/implement-change`. Rejected because the v1.0 definitions still ship and would keep running task-compliance, which contradicts the scope.
- Decision-bearing: no (closes a scope gap within the existing direction)

## spec

### Name the switch `task_compliance`, a `true`/`false` workflow param
- Decision: Add an optional `task_compliance` param to `core/run-validator` (default `false`) and to `core/implement-task` (default `true`). Values outside `true`/`false` fail before any validator command or implementation step runs, matching the existing `skip_validator` validation.
- Alternatives considered: keep a non-empty `task_file` as the activation signal, which is the ambiguity this change removes; use an enum or a string such as `reviews`, which is broader than needed.
- Decision-bearing: yes

### `task_compliance: true` with an empty `task_file` fails in run-validator
- Decision: Fail with an error naming `task_file` instead of silently running without task-compliance.
- Alternatives considered: silently skip activation, which hides a caller error; activate without a context file, which the validator review cannot use.
- Decision-bearing: no

### Replace the implicit-activation requirements instead of editing them in place
- Decision: Remove "Task-compliance flags activated when task_file is set" and "Per-task workflows propagate task_file", with migration notes, and add three requirements: explicit opt-in in run-validator, implement-task defaulting on, and built-in workflows running without task-compliance. Modify the optional-param requirement to cover the new param.
- Alternatives considered: modify the existing requirements in place, which keeps names that no longer describe the behavior.
- Decision-bearing: no

### Keep the validator-init and repo-config requirements unchanged
- Decision: Leave "validator-init requests task-compliance scaffolding" and "agent-runner repo config carries the entry" untouched, as the proposal decided.
- Alternatives considered: none beyond the proposal's.
- Decision-bearing: no

### No delta for validator-metrics-delivery
- Decision: Write no spec delta. Its scenario "including when task-compliance arguments are present" still holds on the factory path, and the correlation-independence scenarios now live in task-compliance-activation.
- Alternatives considered: write a no-op delta, which OpenSpec does not need.
- Decision-bearing: no

### External-delivery note in task-delivery-gate
- Decision: Modify "Reporting external delivery" so the output says only that the validator did not cover the external work and never claims a particular review ran. This makes the note independent of activation, so the switch doesn't need threading into `verify-task-commit.sh`.
- Alternatives considered: make the wording depend on activation, which adds plumbing for no user benefit.
- Decision-bearing: no

## design

### Validate run-validator params in a top-level guard step
- Decision: Add a `check-task-compliance` step that runs a new `check-task-compliance.sh` before `validator-retry`. It rejects a non-boolean `task_compliance`, and `true` with an empty `task_file`. `run-validator.sh` activates the review only on an exact `true` with a non-empty `task_file`.
- Alternatives considered: validate inside `run-validator.sh`, which runs inside a `continue_on_failure` loop and would hand param errors to the `fix-violations` agent; two separate steps with a `command:` that interpolates `task_file`, which has fragile quoting.
- Decision-bearing: no

### Drop `task_file` from the v1.0 simple-change run-validator calls
- Decision: Remove the param instead of passing `task_compliance: "false"`, so every caller that doesn't use task-compliance has the same plain-validation call.
- Alternatives considered: pass an explicit `false`, which also satisfies the spec but leaves a dead `task_file`.
- Decision-bearing: no

### Edit the v1.0 workflow definitions in place
- Decision: Edit the existing v1.0 YAML instead of adding new versions. This matches how the repository has fixed built-in workflows before. Resumed runs re-resolve sub-workflow params from the parent step.
- Alternatives considered: publish v1.1 or v2.1 variants, which is extra surface for a behavior removal.
- Decision-bearing: no

### Enforce the caller pin with a catalog-walk test
- Decision: Add an `embed_test.go` walk that fails when any embedded `implement-task` call lacks `task_compliance: "false"`, or when any embedded `run-validator` call opts in.
- Alternatives considered: per-workflow assertions only, which miss future callers.
- Decision-bearing: no

## test-plan

### Three integration obligations at the YAML-to-process seam, no E2E
- Decision: INT-001 covers the factory-shaped `implement-task` call, which activates the review on both validator launch sites. INT-002 covers a non-factory composed run through the real runner with no task-compliance flags. INT-003 covers the fail-fast guard, with no validator call and no repair agent. No automated E2E, because the public entry points need real agents and INT-002 already executes the composed workflows for real.
- Alternatives considered: an E2E through `agent-runner run` with real agents, which is costly and nondeterministic; structure tests only, which miss broken `script_inputs` and param forwarding.
- Decision-bearing: no

### Acceptance envelope permits one small real task-compliance review
- Decision: Allow running the real `agent-validator` locally, including one opted-in task-compliance review on a tiny temp repository. Keep the agent-factory repository, the live factory, GitHub and user configuration off limits.
- Alternatives considered: fakes only, which can't confirm that the opted-in argv still drives a working review.
- Decision-bearing: no

### No human-only testing
- Decision: None. Every behavior can be observed through validator argv and workflow outcomes.
- Alternatives considered: none.
- Decision-bearing: no

## approach-review

### AR-001: the "passed validation" wording overstates the outcome (applied)
- Decision: Applied. Verified that `verify-final-validator` uses `warn_on_failure`, and that `validator-retry-verification` lets the parent continue after final verification fails. The design now uses outcome-neutral wording (", and, unless `skip_validator` is `true`, a validator run."). The spec adds a requirement sentence and the scenario "Task index prompt does not claim validation passed". The testing strategy now rejects pass claims in the prompt and adds a runner-level check that an exhausted, warning-only validation still reaches `complete-task-index` without a pass claim. The non-blocking warning policy is unchanged.
- Alternatives considered: make exhausted validation block `complete-task-index`, which changes `validator-retry-verification` and exceeds this feature.
- Decision-bearing: no

### AR-002: the acceptance envelope excluded the checkout and evidence directory under `~/.agent-factory` (applied)
- Decision: Applied. The envelope now authorizes reading and executing source in the designated checkout, and writing only to the current attempt's evidence directory and temp scratch directories. The factory service, the agent-factory repository, production factory operations, and every other clone, run or artifact directory stay excluded.
- Alternatives considered: designate a separate checkout outside `~/.agent-factory`, which needs extra copying and revision identification for no benefit.
- Decision-bearing: no

## tasks

### One implementation task for the whole change
- Decision: Write a single checklist task in `tasks.md` that covers the run-validator switch and guard, the implement-task default, the non-factory caller pins, the delivery-note rewording, the docs, and every unit, structure and INT test. It links every source artifact.
- Alternatives considered: split by component, which the step instructions rule out.
- Decision-bearing: no

## archive-repair

### Restore the current scenario header in the MODIFIED optional-param requirement
- Decision: Rename the delta scenario "Params default when caller omits them" back to the current spec's "Param defaults to empty when caller omits it", keeping its body, which also covers `task_compliance`. Both MODIFIED blocks now carry every current scenario header.
- Alternatives considered: add the old scenario alongside the renamed one, which would duplicate the same behavior.
- Decision-bearing: no
