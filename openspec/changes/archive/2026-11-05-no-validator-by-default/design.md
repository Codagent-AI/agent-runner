## Context

The current OpenSpec and spec-driven `change` (v3.0) and `simple-change` (v2.0) workflows require Agent Validator and run it at many places:
- before the draft pull request;
- after every acceptance fix round;
- before re-acceptance in `accept-change`;
- inside agent prompts that tell the lead to invoke `validator-run`;
- inside the `codagent:push-pr` and `codagent:fix-pr` skills;
- per task in `simple-change`.

`create-change.sh` refuses to run when `agent-validator` is not installed. The only control is a `skip_validator` boolean.

The approved specs (`pluggable-validation` plus deltas to `builtin-workflows`, `task-compliance-activation`, `sub-workflows`, `workflow-pre-validation`, and `native-setup-ux`) replace that boolean with:
- a per-run `validation_workflow` selection;
- a fixed small set of validation points;
- warn-and-continue failure handling;
- optional validator setup.

Relevant runtime facts this design builds on:
- **Sub-workflow targets are interpolated** (`internal/exec/subworkflow.go` `resolveWorkflowPath`), so `workflow: "{{validation_workflow}}"` already works for `builtin:` references.
- **Absolute paths under a builtin parent are mishandled.** `loader.ResolveRelativeWorkflowPath` turns any non-`builtin:` target under a builtin parent into an embedded reference, which breaks absolute paths.
- **Named sessions persist run-wide.** Child workflows read and write the same `NamedSessions` map, and `MergeSessionDecls` accepts repeated declarations that name the same agent. So `implementor-agent` declared by `core/implement-change-v2.0.yaml` can be resumed by a later sibling helper that declares it identically.
- **The session field is not interpolated.** A step's `session:` value is literal.
- **`model.Param` has `Name`, `Required`, `Default`.** There is no value-mapping support. Parameters are bound in three places: `internal/runner/runner.go` (top level), `internal/exec/subworkflow.go` (`validateSubWorkflowParams`), and `internal/prevalidate/pipeline.go` (walk binding).
- **Callers may pass undeclared parameters to a sub-workflow.** Only missing required parameters are an error.
- **`core/run-validator-v1.0.yaml`** has a 3-iteration repair loop. Its `fix-violations` step uses `session: inherit`, which is the most recent session of the enclosing context. It ends with a warning-only final verification.
- **UI steps (`mode: ui`) can capture a `single_select` input**, and later steps can `skip_if` on it, as `scaffold`'s publish choice already does.

## Goals / Non-Goals

**Goals:**
- **Selection:** make the validation workflow a per-run selection, defaulting to `auto`, with `agent-validator` and `none` as short names. Allow custom workflows by `builtin:` reference or absolute path.
- **Fewer runs:** validate only at the spec's points (two in `change` plus one per CI fix; two in `simple-change`), and never from an agent prompt or skill.
- **Right fixer:** repair findings in the session that made the changes being validated, never a tester and never a fresh session.
- **Warn and continue:** unresolved validation warns everywhere, and the warning is visible in the handoff and final status.
- **No forced install:** projects without `.validator/config.yml` work without installing Agent Validator.
- **Stable older workflows:** keep older workflows and the Agent Factory's direct `core/implement-task-v1.0.yaml` use behaving as before, except for config gating.

**Non-Goals:**
- Changing Agent Validator itself (check trust, deleted-line filtering).
- Validation strategies beyond the three built-ins. Custom workflows are the extension point.
- Changing acceptance, review, or CI-wait behavior other than where validation sits.

## Approach

### 1. Runtime: parameter aliases

Add an optional `aliases` map to `model.Param`:

```yaml
params:
  - name: validation_workflow
    required: false
    default: auto
    aliases:
      auto: builtin:core/validation-auto-v1.0.yaml
      agent-validator: builtin:core/validation-agent-validator-v1.0.yaml
      none: builtin:core/validation-none-v1.0.yaml
```

- `Param.Bind(value string) string` returns `aliases[value]` when present, else `value`.
- It is applied after default substitution at all three binding sites: top-level runner, sub-workflow binding, and pre-validation walk binding.
- Binding is idempotent, because full references are never alias keys. A resumed run therefore gets the same value whether or not state stored the pre- or post-alias value.
- Loader validation rejects an empty alias key or value.
- The parameter form keeps showing `default` (`auto`), so users see and type short names.
- Every built-in workflow that declares `validation_workflow` declares the same default and aliases. That covers the six top-level workflows (OpenSpec and spec-driven `change-v3.0`, `simple-change-v2.0`, `scaffold-v1.0`) and the hidden helpers that accept it.
  - Helpers receive full references from their parents, so aliasing is a no-op there.
  - Callers that start a helper directly still get short names. Agent-evals, for example, runs `core:implement-change`.
- A unit test asserts that every embedded declaration of `validation_workflow` carries the identical default and alias map.

### 2. Runtime: absolute sub-workflow paths

`ResolveRelativeWorkflowPath` returns `workflowField` unchanged when `filepath.IsAbs(workflowField)`, before the builtin-parent branch. The loader already loads non-`builtin:` paths from disk, and pre-validation and execution both call this function, so one change fixes both.

### 3. Runtime: pre-validation of caller-supplied targets on builtin fresh runs

Fresh runs of builtin roots currently skip pre-validation. The new rule: when a builtin root is fresh and the caller supplied at least one param, run a reduced walk.
- **Binding and provenance.** The walk binds params as the full walk does. It also tracks provenance: a param value is *caller-derived* when the caller supplied it, or when a parent step's `params:` entry interpolated a caller-derived value into it. This is how `validation_workflow` flows through `change-v3.0` → `implement-change-v3.0` → `verify-change-v2.0`. Alias binding does not change provenance.
- **Caller-derived targets.** For each sub-workflow step whose `workflow:` field interpolates a caller-derived value, the walk resolves the target with `resolveWorkflowField` in strict mode. It then runs the full pre-validation of that target and its composition, as for a non-builtin root. The checks use the params and the named-session declarations the step would actually pass. This applies to `builtin:` and path targets alike. So `validation_workflow=builtin:core/verify-change-v1.0.yaml` fails on its missing required params, rather than passing an existence check.
- **Everything else.** The walk only traverses fixed builtin helpers and targets resolved from defaults; it does not validate them. Build-time validation of all embedded builtins covers them, as today.

So `validation_workflow=foo` fails before any step runs, with an error naming the unresolved embedded `builtin:core/foo` target. A nonexistent, invalid, or incompatible target fails the same way. A run without caller-supplied params skips pre-validation exactly as today. Resumes keep today's behavior.

### 3a. Runtime: warnings survive resume

`ExecutionContext.WarningOrigins` is created empty on every start (`internal/model/context.go`), and resume does not restore it (`internal/runner/resume.go`). So a run that warned on red validation, stopped, and resumed would finish without `completed_with_warnings`.

- `state.json` gains a `warnings` list: each origin's step path and message, written whenever a warning is recorded.
- Resume seeds `WarningOrigins` from that list before executing.
- The final `run_end` data and run status then count warnings from earlier attempts.

The change is generic and benefits every `warn_on_failure` step, not only validation.

### 4. Built-in validation workflows (`workflows/core/`)

All three accept the validation contract:

| Param | Meaning |
|---|---|
| `repair_session` | Named session that repairs findings: `implementor-agent` or `lead-agent`. Default `lead-agent`. |
| `result_file` | Optional path. When non-empty, the workflow writes a first line of `PASS`, `FAIL`, or `SKIPPED`, followed by details. |
| `task_file`, `task_compliance` | Optional pass-through for task-compliance-aware workflows. Default `""` and `false`. |

**Outcome.** The authoritative outcome is the validation step's own outcome. The result file is supplementary evidence.
- A validation workflow passes by succeeding.
- It reports unresolved validation by ending with a warning or by failing. Either is allowed, so a custom workflow needs no special warning mechanism.
- Every caller sets `warn_on_failure: true` on its validation sub-workflow step. Any failure therefore becomes a warning and the caller continues. That includes a custom workflow failing, and the built-in `agent-validator` failing on a missing executable or config. The step still fails with its error, as the spec requires, and the run completes with warnings.
- The built-ins write `result_file`. A custom workflow may ignore it.
- Handoff scripts read `result_file` only to add detail:
  - `PASS` or `SKIPPED` → reported as such;
  - `FAIL` → reported as unresolved, with its details;
  - absent or malformed → "the validation workflow recorded no result; see the run's warnings for its outcome".

  A missing file is never reported as a failure by itself.

Custom workflows receive the same params and may ignore the ones they don't use. Undeclared params are allowed.

**`validation-agent-validator-v1.0.yaml`**
1. `validate-repair-session`: a script rejects any value other than the two names.
2. `check-agent-validator`: a command fails naming `.validator/config.yml` or the `agent-validator` executable when either is missing.
3. `check-task-compliance` and `validator-retry` loop (max 3): the same as `run-validator-v1.0`, except `fix-violations` becomes two steps:
   - `fix-violations-implementor` with `session: implementor-agent`, skipped unless `repair_session` is `implementor-agent`;
   - `fix-violations-lead` with `session: lead-agent`, skipped unless it is `lead-agent`.
   Both use the existing prompt. The loop's `break_if: success` on the validator run means neither fix runs after a pass.
4. `verify-final-validator` with `warn_on_failure: true`, unchanged.

It declares `sessions: implementor-agent (implementor)` and `lead-agent (lead)`. These merge with the callers' identical declarations, so repairs resume the caller's session.

Today's `fix-violations` prompt also says not to run the validator except `update-review`. That is retained.

**`validation-auto-v1.0.yaml`**
1. `detect-config`: a command prints `configured` or `unconfigured` (captured) depending on whether `.validator/config.yml` exists at run time. This leading non-skippable step exists because a workflow's first step cannot be conditionally skipped.
2. `run-agent-validator`: a sub-workflow of `validation-agent-validator-v1.0.yaml` that passes every contract param, `skip_if` unconfigured.
3. `record-skipped`: writes `SKIPPED` to a non-empty `result_file` when unconfigured.

Because detection is per invocation, a scaffold run that creates config earlier picks it up.

**`validation-none-v1.0.yaml`**: a single command step that writes `SKIPPED` to a non-empty `result_file`.

`core/run-validator-v1.0.yaml` is unchanged except for config gating (below).

### 5. New current helper versions

Each is copied from the current version and then edited. The old versions stay embedded and readable.

**`core/implement-change-v3.0.yaml`** (from v2.0)
- Replace `skip_validator` with `validation_workflow`.
- Pass it to `core/commit-definition-v1.0.yaml` and `core/verify-change-v2.0.yaml`.
- Keep the session declarations: `implementor-agent`, `crosscheck-agent`, `lead-agent`, `acceptance-tester`.
- The "never run Agent Validator" rule in the implementor prompt stays. Add: "verify with the project's own checks as AGENTS.md describes."

**`core/verify-change-v2.0.yaml`** (from v1.0)
- Remove `skip_validator` and its validation step, the pre-PR `run-validator`, `verify-validator-result` (`validator-pr-gate.sh`), and the in-loop `acceptance-validator`.
- The draft PR opens without validation. Each failing round runs fix, push, and head check.
- Declare `implementor-agent (implementor)`. Accept `validation_workflow`.
- After the existing `write-acceptance-status` and `verify-acceptance-handoff`:
  1. `record-validation-base`: capture `git rev-parse HEAD`.
  2. `validate`: `workflow: "{{validation_workflow}}"` with `repair_session: implementor-agent` and `result_file: {{session_dir}}/output/validation-after-acceptance.txt`. The implementor wrote nearly all of the change, and validation reviews the whole change.
  3. `validation-push`: `acceptance-push.sh` with the existing lead repair.
  4. `verify-validation-pr`: the PR head check through `check-draft-pr.sh` in a new `accept_ready: true` mode (below).
  5. `record-validation-commits`: a script (`validation-handoff.sh`) appends a `## Validation` section to `acceptance-handoff.md`:
     - the result-file status, following the outcome contract in section 4;
     - `git log --oneline <tested-revision>..HEAD`, labelled as commits made after the tested revision.

     The script cannot see step outcomes. When the step warned, the warning is visible in the run status, which survives resume (section 3a).
- Every `validate` step in this and the other helpers carries `warn_on_failure: true` (section 4).
- The acceptance status files are written before validation, so commits made by validation do not invalidate `ACCEPTANCE_COMPLETE` for the tested revision.

**`core/accept-change-v2.0.yaml`** (from v1.0)
- Accept `validation_workflow`. Remove the `run-validator` step that ran before re-acceptance.
- Prompt edits:
  - `review-and-refine` (line 97 today): replace "Invoke the validator-run skill, incorporate its fixes" with "verify with the project's relevant checks as AGENTS.md describes; do not run Agent Validator".
  - `recover-reacceptance-testing` (line 136): replace "the full Validator" the same way.
  - `recommend-reacceptance-testing`: name the commits since the last tested revision, including the validation commits listed in the handoff's `## Validation` section.
- After the re-acceptance gate settles:
  1. `record-validation-base`: capture HEAD.
  2. `validate`: `repair_session: lead-agent`, `result_file: validation-after-review.txt`.
  3. `validation-push` and `verify-validation-pr`, as in verify-change.
  4. `retest-validation-fixes`: a `lead-agent` step, autonomous, with `call_agent`.
     - It is skipped when HEAD equals the recorded base.
     - The lead reads the validation commits and decides whether any changes observable behavior.
     - If so, the lead resumes `acceptance-tester` through `codagent:call-agent` for a targeted retest of that behavior, using `codagent:prepare-acceptance` against the diff from the tested revision.
     - The lead fixes reported defects with the project's own checks, then commits and pushes. There is no further validation run. The CI-fix validation covers anything later.
  5. `record-untested-commits`: a script appends to `acceptance-handoff.md` the commits since the latest recorded tested revision.
     - This covers a declined re-acceptance, validation commits not retested, and lead fixes after the last retest.
     - Each is labelled "not exploratory-tested".
     - It writes nothing when the tested revision equals HEAD.

**`core/finalize-pr-v2.0.yaml`** (from v1.0)
- Accept `validation_workflow`. Declare `lead-agent`.
- `push-pr`: unchanged skill use, plus "do not run Agent Validator."
- In `ci-fix-loop`, `fix-pr` and the steps that must follow it move into one group step, `ci-fix`. The group carries the `skip_if: previous_success` that `fix-pr` has today, which reads the outcome of `ci-fix-needed-gate`. The steps inside therefore run together, or are skipped together, on that gate's outcome alone. A `skip_if: previous_success` on the `validate` step would have read `fix-pr`'s outcome and skipped validation after every successful fix. The group contains:
  1. `fix-pr`: "invoke codagent:fix-pr in commit-only mode: commit the fix, do not push, do not run Agent Validator."
  2. `validate`: `repair_session: lead-agent`, `result_file: validation-ci-fix.txt`.
  3. `ci-fix-push`: `acceptance-push.sh`.
  4. `verify-ci-fix-pr`: head check with `accept_ready: true`.

  The loop then waits for CI as today. A red validation leaves a warning and the push still happens.

**`core/complete-simple-change-v2.0.yaml`** (from v1.0)
- Accept `validation_workflow`. Declare `implementor-agent`.
- `implement` calls the new `core/implement-task-v2.0.yaml`.
- After `test`:
  - `validate` with `repair_session: implementor-agent`;
  - the `review` prompt replaces "invoke validator-run" with project checks;
  - after `review`: `validate` with `repair_session: lead-agent`.
- No push.

**`core/implement-task-v2.0.yaml`** (from v1.0)
- Remove `skip_validator`, `task_compliance`, and the validator steps.
- Its implementation step and the resume steps that follow it run in a declared `implementor-agent (implementor)` named session instead of `session: new`/`resume`, so later validation repairs resume the implementing agent.
- Task delivery, commit verification, and the session report are unchanged.

`check-draft-pr.sh` gains an optional `accept_ready` input. When `true`, it accepts a single open PR whose head equals local HEAD in draft or ready state. The default keeps today's draft-only check.

### 6. Top-level workflows

**OpenSpec and spec-driven `change-v3.0.yaml`** (edited in place)
- Declare `validation_workflow` with aliases.
- `create` passes `validation_workflow` as a script input.
- `implement` uses `implement-change-v3.0.yaml`.
- `accept` uses `accept-change-v2.0.yaml`.
- OpenSpec `archive` passes `validation_workflow`.
- `finalize` uses `finalize-pr-v2.0.yaml`.
- Each passes `validation_workflow` through.

**OpenSpec and spec-driven `simple-change-v2.0.yaml`** (edited in place)
- Declare `validation_workflow` with aliases.
- Pass it to `create`, `commit-plan`, `complete-simple-change-v2.0.yaml`, and archive.
- The spec-driven version's inline `agent-validator detect` command moves behind the same bookkeeping gate.

**`scaffold-v1.0.yaml`** (both, edited in place)
- Declare `validation_workflow` with aliases.
- Add the setup question (section 9).
- Rename the `scaffold-agent` session (agent `implementor`) to `implementor-agent`. The agent that wrote the scaffold then repairs validation findings; scaffold has no `lead-agent` session, so naming that one would start a fresh repairer.
- Replace `run-validator` with `workflow: "{{validation_workflow}}"` (`repair_session: implementor-agent`, `warn_on_failure: true`).
- The `publish-choice` copy changes from "Agent Validator has run" to "validation has run".

### 7. Bookkeeping gate

Add `workflows/core/validator-bookkeeping.sh`. Its input is `validation_workflow`, which may be empty. It prints `run` or `skip`, and other scripts source it as a function.

| `validation_workflow` value | Result |
|---|---|
| empty (older callers) | `run` iff `.validator/config.yml` exists |
| the `agent-validator` built-in reference | `run` |
| the `auto` built-in reference | `run` iff config exists |
| anything else (`none`, custom paths) | `skip` |

Callers that use it:
- **`create-change.sh` (both).** It accepts an optional `validation_workflow` input. On `skip`, it does neither the install check nor `detect`. On `run`, it keeps today's install check and `detect`. For older callers this means config gating only.
- **`commit-change-plan.sh`.** It accepts optional `validation_workflow` alongside `skip_validator`. It advances the baseline only when `skip_validator` is not `true` and the gate says `run`. `core/commit-definition-v1.0.yaml` and `core/commit-change-plan-v1.0.yaml` gain an optional `validation_workflow` param and pass it through.
- **`openspec/archive-change-v1.0.yaml`.** It gains optional `validation_workflow`. `advance-validator-baseline` becomes a gated command.
- **Spec-driven `simple-change-v2.0`.** Its inline detect uses the gate.

Bookkeeping helpers get an optional param instead of new versions, because their behavior with the param absent changes only by config gating, which the specs require anyway.

### 8. Config gating in older helpers

- `core/run-validator-v1.0.yaml` gains a leading `check-config` capture step. Its validator steps are skipped when `.validator/config.yml` is absent, writing `SKIPPED` to a non-empty `result_file`.
- `validator-pr-gate.sh` treats `SKIPPED` as pass.
- `core/implement-task-v1.0.yaml`'s validator step is reached only through run-validator, so it inherits the gate.
- Every direct bookkeeping command in a preserved workflow is wrapped in the same config check, so it is a no-op without `.validator/config.yml`:
  - `agent-validator detect` in `spec-driven/simple-change-v1.0.yaml` and `spec-driven/plan-change-v1.0.yaml`;
  - `agent-validator skip` in `openspec/plan-change-v1.0.yaml` and `openspec/implement-change-v1.0.yaml`.

  `create-change.sh`, `commit-change-plan.sh`, and the archive step are covered by section 7.
- Prompts in preserved versions that tell an agent to run the validator skill stay unchanged. Rewriting old prompts would change pinned behavior. In an unconfigured project, the agent sees the skill report no configuration, as it does today.

This keeps the preserved `change` v1.0 and v2.0, the `simple-change` v1.0, `complete-simple-change-v1.0`, and the Agent Factory unchanged in configured projects. In unconfigured ones, none of their deterministic steps fail for lack of Agent Validator.

### 9. Optional setup

**Scaffold.** Before `validator-init`, add a `validator-choice` UI step: a `single_select` with "Set up Agent Validator" (default) or "Skip".
- `validator-init` and `validator-setup` are skipped on "Skip".
- A `validator-skipped-guidance` UI step, shown only on "Skip", says: "Make sure AGENTS.md explains how to verify this project: linters, the test suite, and any project-specific checks."
- The validation point runs `{{validation_workflow}}`. Under `auto` without config it passes as `SKIPPED`.

**Onboarding `validator-v1.0.yaml`.** `intro-ui` becomes the choice, with actions:
- "Set up Agent Validator": outcome `continue`;
- "Skip": outcome captured as `skip`.

Every later step in the phase is `skip_if` skip. That includes stash, init, setup, restore, and the demonstration. On skip, one UI step shows the same `AGENTS.md` guidance. Onboarding then continues to its next phase as today.

Docs and copy:
- `docs/setup.md` says Agent Validator is optional.
- `docs/agent-profiles.md:21` and the native-setup crosscheck copy (the `internal/onboarding` native setup strings matching "Agent Validator owns implementation-code review") say that crosscheck challenges planning artifacts and that implementation is verified by the project's checks and any configured validation.

### 10. Cross-repo skills (agent-skills)

- `codagent:push-pr` drops its validator step.
- `codagent:fix-pr` drops its validator step and gains a commit-only mode ("commit the fix; do not push").
- Runner's prompts state "do not run Agent Validator" and, for fix-pr, "do not push" explicitly. So an older installed skill is overridden by the step prompt, and rollout order between repos does not matter.

### 11. Regression guard

`workflows/embed_test.go` adds `TestCurrentWorkflowsInvokeValidatorOnlyInValidationWorkflows`. It walks the composition graph of the six current top-level workflows through embedded files, following `workflow:` references and resolving `{{validation_workflow}}` to each built-in. It asserts:
- the only files whose steps run `agent-validator` commands or `run-validator.sh` are:
  - `validation-agent-validator-v1.0.yaml`;
  - the scripts it uses;
  - the bookkeeping-gated `create-change.sh`, `commit-change-plan.sh`, and archive steps;
  - the spec-driven simple-change detect step;
  - `validator-init` in scaffold and onboarding;
- no prompt in the graph contains `validator-run`, `agent-validator run`, `agent-validator check`, `agent-validator review`, or "the full Validator", except the repair prompts' `update-review` instructions.

A second test asserts that each validation point passes `repair_session` with one of the two allowed names and never passes a tester session.

## Decisions

- **Aliases in Runner rather than dropping short names.** Short names were approved product behavior. A run-time translation script cannot work, because pre-validation and sub-workflow loading need the resolved target before any step runs. A generic `aliases` map is about 30 lines, and other enumerated params can reuse it.
- **Repair session as an enumerated param with two fixed fix steps, not session interpolation.** Interpolating `session:` would touch session resolution, pre-validation of session names, and resume. Two named sessions cover every validation point:
  - the implementor after acceptance (change) and after flow testing (simple-change);
  - the lead after human review and after CI fixes.

  The spec's "calling workflow names the session" holds. Custom workflows get the name and may declare the same named sessions to resume them.
- **No fresh fixer, ever.** A fresh session fixes blindly from validator output. Named sessions persist run-wide, so `implementor-agent` resumes the agent that wrote the code. In `simple-change` this requires `core/implement-task-v2.0.yaml` to implement in a named session.
- **Point 1 always repairs in `implementor-agent`.** The lead may have made small acceptance fixes later. But validation reviews the whole change, and the implementor owns nearly all of it. Choosing whoever committed last would need run-time session selection.
- **No re-validation after the targeted retest at point 2.** It would cost another loop for a narrow gap. The CI-fix validation covers anything pushed later, and the handoff records untested commits.
- **Version the helpers that change call sites. Gate the helpers that only do bookkeeping.** The Agent Factory and pinned older workflows keep exact behavior apart from config gating.
- **The workflow owns the push after a CI fix.** This lets validation sit between commit and push without depending on the installed skill version.
- **A per-call config check inside `auto`, not once per run.** Scaffold creates config mid-run, and the spec requires `auto` to pick it up.

## Risks / Trade-offs

- **Agent Factory and agent-evals breakage.** The agent-evals harness runs `core:implement-change` directly. It lists `skip_validator` in `REQUIRED_WORKFLOW_PARAMETERS` (`agent-evals/evals/agent-runner/and-scene/lib/workflow.mjs`) and passes `skip_validator=true`. `core:implement-change` now resolves to v3.0, which declares `validation_workflow` with aliases instead.
  - The harness must pass `validation_workflow=none` and replace `skip_validator` with `validation_workflow` in its parameter-contract list.
  - Its real invocation must be re-run once against the new Runner.
  - Agent-factory call sites that pass `skip_validator` to `change` v3 or `core:implement-change` need the same switch.

  Both land right after Runner ships. Factory uses of `core/implement-task-v1.0.yaml` are unaffected.
- **Session-name coupling.** The built-in validation workflow hardcodes `implementor-agent` and `lead-agent`. A caller that declares those names with different agents fails at session merge with a clear error. The regression test covers the built-ins.
- **Fewer validation runs mean later detection.** Acceptance-round fixes are no longer validated until point 1. That is intended per the approved strategy, and the project's own checks between points mitigate it.
- **Older skills during rollout.** The prompt-level overrides handle a skill that still runs the validator or pushes, at the cost of slightly redundant wording until the skills update.
- **Alternative considered:** a single validation sub-workflow with `session: inherit` plus a preceding no-op step in the right session. Rejected: it is fragile and invisible in YAML.

## Migration Plan

1. Runtime changes: aliases, absolute paths, the pre-validation exception, and persisted warnings. Each has unit tests in `internal/model`, `internal/loader`, `internal/exec`, `internal/runner`, and `internal/prevalidate`.
2. Built-in validation workflows and config gating of `run-validator-v1.0`.
3. The bookkeeping gate script and its optional inputs.
4. New helper versions and the in-place edits to the current top-level workflows, plus the regression tests.
5. Optional setup in onboarding and scaffold, and the docs and copy.
6. The agent-skills `push-pr`/`fix-pr` change, independent of Runner merge order.
7. The agent-evals and agent-factory `skip_validator` → `validation_workflow=none` update, immediately after Runner ships. For agent-evals this includes the `core:implement-change` parameter-contract list.

Rollback: revert the Runner PR. The older helper versions are untouched, so pinned runs and resumes keep working.
