## Why

Agent Runner treats Agent Validator as a hard dependency and runs it far more often than the value justifies.

- **Required:**
  - `create-change.sh` refuses to start a change unless `agent-validator` is installed.
  - The onboarding and scaffold flows always set it up.
  - `docs/setup.md` tells users to set it up before relying on Agent Runner.
  
  A project that verifies itself through its own linters, test suite, and CI cannot opt out except by passing `skip_validator` on every run.
- **Run too often:** the current `change` workflow validates:
  - before the draft PR;
  - after every acceptance fix round;
  - again during human acceptance;
  - after every re-acceptance or recovery fix, and after every CI fix (the last two through prompt and skill instructions the workflow cannot count or control).
  
  On the happy path that is two full validations; a messy change can reach about eight, each a retry loop of up to four validator executions.
- **What the evals show:**
  - The validator stage costs about $1.90 of a $10.67 eval rep, and about 19 minutes of wall time, mostly in its fix steps.
  - Its findings are mostly real hardening: error paths, resource cleanup, edge-case crashes. Those are a different class of problem from the functional and visual defects acceptance testing finds.
  - Reviewing one change as three increments cost 2.3x a single batched review, because every review carries a fixed overhead.
- **The ordering problem:**
  - Validation and acceptance testing each invalidate the other: fixes from either one leave the other's evidence stale.
  - Requiring the full other procedure after every fix is expensive and never converges.
  - The workflow needs a deliberate order, with batched validation at a few points.
- **Hardcoded:** validation cannot be swapped today. Every call site names the validator directly, so alternative validation approaches cannot be compared in evals without editing the workflows.

## What Changes

**The validation workflow is chosen per run.**
- Current workflows that validate take a `validation_workflow` parameter that names the workflow run at each validation point.
- Built-in options:
  - `auto`, the default: runs Agent Validator when the project has `.validator/config.yml`, otherwise does nothing.
  - `agent-validator`: the existing bounded retry loop.
  - `none`.
- Any other built-in reference, or an absolute path to a workflow file, can be passed, for example in evals.
- Every validation workflow receives the session that repairs its findings. Repairs never fall to whichever agent ran last, such as an acceptance tester that is forbidden to fix.
- **BREAKING:** `skip_validator` is removed from these workflows.

**Validation happens only where the workflow calls the chosen validation workflow.**
- No step prompt and no skill instructs an agent to run Agent Validator.
- Between validation points, agents verify fixes with the project's own relevant tests and checks as described in `AGENTS.md`, never with `agent-validator` commands.

**Validation timing in `change` (v3, OpenSpec and spec-driven):**
1. **After the agent's acceptance testing:** the whole change, including acceptance fixes, is validated and repaired until it passes. Acceptance testing catches the functional problems first; validation then hardens the result. The draft PR opens before acceptance testing, as the tester requires.
2. **After human review settles:** once human review, requested refinements, and any re-acceptance or recovery have settled, everything changed since the previous validation is validated in one batch. If its fixes change behavior, the tester retests only that behavior.
3. **After each CI fix:**
   - The workflow owns the order: the fix is made and committed, the validation workflow runs, its outcome is gated, and only then is the fix pushed for CI.
   - The CI fixer no longer pushes on its own inside this workflow.
4. **Validation fixes are pushed at every validation point that has a PR.**
   - A deterministic push follows each one, then a check that the PR head matches local `HEAD`, whether the PR is a draft or ready for review.
   - This closes an existing gap: today, validator fixes made during human acceptance can stay unpushed until finalization.
5. **Tested and validated revisions are tracked separately.**
   - Validator commits after acceptance testing do not invalidate the acceptance result.
   - The human-review handoff shows the validator's fixes since the tested revision.
   - The re-acceptance recommendation covers everything changed since the last tested revision, including those fixes.
   - Declining re-acceptance is recorded as leaving those fixes without exploratory testing.

**Validation timing in `simple-change` (v2, OpenSpec and spec-driven):**
- The same principle applies: validation runs after flow testing and again after the user's review settles, not per task.
- `simple-change` creates no PR, so it does not push.

**Scaffold** keeps its local-only flow: it validates before offering optional publication.

**Validator bookkeeping follows the chosen validation workflow.**
- `detect` and baseline `skip` run only when the chosen validation workflow actually runs Agent Validator: `auto` with `.validator/config.yml` present, or `agent-validator`.
- With `none` or a custom workflow, nothing requires Agent Validator or advances its baseline.
- `create-change.sh` no longer requires `agent-validator` to be installed.

**Setup makes Agent Validator optional.**
- The onboarding validator phase and the scaffold workflows ask whether to set it up.
- Declining tells the user to make sure `AGENTS.md` explains how to verify the project: linters, test suite, and any project-specific checks.
- `docs/setup.md` describes the validator as optional.
- Native setup copy stops presenting Agent Validator as the implementation-review system.

**Older workflow versions keep their behavior.**
- Covered: `change` v1/v2, `simple-change` v1, and the helper workflows they call.
- The current workflows move to new versions of the shared helpers they use. The existing helper versions stay in place for older callers and external ones, such as the Agent Factory's direct use of `implement-task-v1.0`.
- In those older paths, `skip_validator` keeps working, and the only change is that each validator invocation runs only when the validator is configured.

## Capabilities

### New Capabilities
- `pluggable-validation`:
  - per-run selection of the validation workflow;
  - the built-in `auto`, `agent-validator`, and `none` workflows and how `auto` decides;
  - validation happening only at workflow-defined points, with no embedded agent instructions;
  - an explicit repair session;
  - pushes at PR-bearing validation points;
  - separate tracking of tested and validated revisions;
  - validator bookkeeping that follows the chosen validation workflow;
  - optional validator setup with the `AGENTS.md` guidance.

### Modified Capabilities
- `builtin-workflows`:
  - the implement-change, verify-change, and plan-change `skip_validator` requirements now apply only to the preserved older helper versions; the current versions use validation-workflow selection;
  - the `change` and `simple-change` requirements describe the new validation timing.
- `task-compliance-activation`:
  - the current `simple-change` path no longer validates per task, so its per-task timing scenario changes;
  - `implement-task-v1.0` keeps `skip_validator`, `task_file`, and `task_compliance` for older and external callers, and validation workflows can still receive the task file and task-compliance setting;
  - the external-delivery protections in `task-delivery-gate` stay inside the Agent Validator workflow.
- `workflow-pre-validation`: a fresh built-in run still resolves and validates sub-workflow targets that depend on a caller-supplied parameter, so a bad `validation_workflow` fails before any step runs.
- `sub-workflows`: a sub-workflow target given as an absolute file path loads from the filesystem, even beneath a built-in parent. This applies in both pre-validation and execution.
- `native-setup-ux`: the crosscheck copy no longer states that Agent Validator owns implementation-code review.

## Technical Approach

**Swapping mostly reuses existing runtime support.**
- Sub-workflow steps already interpolate their target (`workflow: "{{validation_workflow}}"`), and pre-validation resolves and checks a param-bound target before the run starts.
- Built-in references resolve directly.
- One gap needs a runtime change:
  - Beneath a built-in parent, `ResolveRelativeWorkflowPath` turns every non-`builtin:` target into another embedded reference, so a project workflow file is never found.
  - Absolute paths get loaded from the filesystem instead, in both pre-validation and execution.
  - Project-relative names and lookup by workflow name stay out of scope.
- The parameter is threaded from each current top-level workflow to every validation point. That is the same plumbing `skip_validator` uses today.

**Versioning:**
- The current top-level workflows switch to new versions of the shared helpers they call: implement, verify, accept, finalize, complete-simple-change, and their plan and commit helpers as needed.
- The existing helper versions are left alone apart from config gating, so older top-level versions and external callers keep today's behavior.

**The built-in validation workflows share one contract**:
- **Parameters:** a fixed set, including the session that repairs findings and optional task-file and task-compliance context.
- **Outcome:** a reported result that callers gate on.

The three built-ins:
- `agent-validator` wraps the existing `run-validator` retry loop. Its repair step uses the supplied session instead of inheriting the most recent one.
- `auto` calls `agent-validator` behind a `skip_if` on the absence of `.validator/config.yml`. It replaces a tri-state flag with an ordinary workflow.
- `none` reports success without running anything.

How each validation point reacts to an unrecovered failure is decided in design. Today, pre-PR failure blocks the PR, and later failures warn.

**Moving and removing call sites:**
- **verify-change:**
  - the acceptance-loop validation is removed;
  - the pre-PR validation moves after the acceptance loop;
  - the draft PR opens before acceptance;
  - the acceptance gate and handoff record the tested revision separately from the validated `HEAD`, rather than requiring them to match.
- **accept-change:**
  - the post-review validation moves after re-acceptance and recovery, followed by a conditional targeted retest;
  - the re-acceptance recommendation covers everything since the last tested revision;
  - the prompts that tell agents to invoke the validator are rewritten.
- **finalize-pr:** the CI loop runs fix and commit, then validation, its outcome gate, the push, and the CI wait.
- **The agent-skills `push-pr` and `fix-pr` skills** drop their validator steps. `fix-pr` gains a commit-only mode, so the workflow owns the push; standalone use keeps pushing. This is a coordinated change in the agent-skills repository.
- Step-level wiring belongs in design and tasks.

**Pushes after validation** reuse `acceptance-push.sh` plus a head-alignment check that accepts either a draft or a ready PR. Callers without a PR, such as `simple-change` and scaffold, do not push.

**A regression test** scans the embedded workflows and fails on any validator invocation outside the built-in validation workflows or config-gated bookkeeping, and on prompts that instruct agents to run the validator.

**Constraint from Agent Validator's current behavior:**
- A passing `agent-validator check` marks the commit trusted, so a later `run` skips reviewing it.
- This design never runs `check` between validations, so it does not depend on changing that.

**Main risk: validator fixes made at the first validation point are not exploratory-tested by the agent tester.**
- The handoff shows them for human review, and the re-acceptance recommendation includes them.
- Re-acceptance is optional, though. When the user declines it, those fixes go untested by the agent, and that gap is recorded rather than hidden.
- They are typically small hardening edits.

## Out of Scope

- **Agent Validator changes**, tracked separately:
  - check-only runs marking commits trusted;
  - separate check and review baselines;
  - per-check rerun policy;
  - a leaner reviewer invocation;
  - findings on deleted lines being filtered out.
- A project-level default validation workflow; selection is per run only.
- Supporting projects without CI. Agent Runner assumes CI runs the project's checks.
- Risk-triggered early reviews of individual fixes.
- Eval arms comparing validation workflows. This change makes them possible but does not define them.
- Restructuring older workflow versions, or the existing helper versions they share, beyond gating their validator calls.
- Resolving validation workflows by project-relative path or by workflow name.
- The agent-skills `init` skill's validator requirement.

## Impact

**Workflows:**
- `workflows/openspec/` and `workflows/spec-driven/`:
  - `change-v3.0`, `simple-change-v2.0`, `scaffold-v1.0`;
  - `create-change.sh`;
  - `archive-change-v1.0`.
- `workflows/core/`:
  - new versions of `implement-change`, `verify-change`, `accept-change`, `finalize-pr`, `complete-simple-change`, and the commit helpers the current workflows call;
  - `run-validator-v1.0` and the acceptance and draft-PR gate scripts;
  - new built-in validation workflows.
  - The existing helper versions (`implement-change-v1.0`/`v2.0`, `verify-change-v1.0`, `accept-change-v1.0`, `finalize-pr-v1.0`, `implement-task-v1.0`, `plan-change-v1.0`, `commit-definition-v1.0`, `commit-change-plan-v1.0` and its script) only gain config gating.
- `workflows/onboarding/validator-v1.0` and the older workflow versions that reach validator calls.

**Code and docs:**
- `internal/loader/loader.go` (`ResolveRelativeWorkflowPath`) and `internal/prevalidate/pipeline.go`: load absolute workflow targets from disk.
- `cmd/agent-runner/internal_cmd.go` (`validator-init`), only if the optional setup flow needs it.
- Native setup copy in `internal/onboarding/native/`.
- `docs/setup.md` and the bundled onboarding docs.

**Tests:**
- `workflows/embed_test.go` and `workflows/verify_change_test.go`: replace the `skip_validator` tests and add the invocation-scan regression test.

**Specs:** `builtin-workflows`, `task-compliance-activation`, `workflow-pre-validation`, `sub-workflows`, `native-setup-ux`, and the new `pluggable-validation`.

**Agent-skills repository:** `push-pr`, and `fix-pr` with its new commit-only mode.

**Users:**
- Callers of the current workflows that pass `skip_validator` must switch to `validation_workflow` (for example `none`). Callers of the older versions and of `implement-task-v1.0` are unaffected.
- Projects without `.validator/config.yml` stop running the validator by default.
