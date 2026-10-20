## Why

The `acceptance-fix` step of `core:verify-change` tells the lead agent to "Resolve every high-confidence finding you can, including implementation, CI, PR-state, tooling, setup, and environmental problems." That broad mandate suits an ordinary feature run, where the person running it owns the whole repository and wants the branch green.

It does not suit every caller. In an Agent Factory run (agent-runner#227, claim 11f43345), the instruction led the lead to edit `.github/workflows/ci.yml` to fix an unrelated red `security-deps` check. The push was rejected because the factory's token lacks the `workflow` scope, after about 2h10m of work. The factory's runs should fix only what their change broke and hand everything else to a human, but today a caller cannot narrow the instruction without forking the whole workflow.

`core:verify-change` already takes optional caller-set params that tune its behavior (`skip_implementation_review`, `stop_unchanged_acceptance`). A scope param for the fix step follows the same pattern and unblocks the factory's own change.

## What Changes

- Add an optional `acceptance_fix_scope` param to `core:verify-change`. Its value is the sentence in the `acceptance-fix` prompt that says which findings the lead resolves.
- The param's default is today's wording, word for word, so the rendered prompt for existing callers (`core:implement-change`, `core:audited-implement-change`, `core:orchestrated-implement-change`, and the factory's current call) does not change.
- An empty or whitespace-only value fails the workflow before any agent step runs, so a misconfigured caller cannot silently drop the scope sentence.
- A caller can pass a narrower instruction, for example: fix only failures this change caused, and record unrelated CI, tooling, environmental, or `.github/workflows` problems in `acceptance-assumptions.md` instead of fixing them.
- Update the `builtin-workflows` spec requirement for the verify-change workflow and the `core:verify-change` row in `docs/built-in-workflows.md`.
- Add tests to the existing suites (`workflows/verify_change_test.go`, and `internal/exec/verify_change_workflow_test.go` if a run-level check is useful). No new suites or CI jobs.

No breaking changes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `builtin-workflows`: the "Verify-change workflow" requirement gains the optional `acceptance_fix_scope` param, with its default and how the `acceptance-fix` step uses it.

## Technical Approach

This is a YAML change to `workflows/core/verify-change-v1.0.yaml`, with the existing param-interpolation mechanism doing the work:

- Declare `acceptance_fix_scope` with `required: false` and `default` set to the current sentence: "Resolve every high-confidence finding you can, including implementation, CI, PR-state, tooling, setup, and environmental problems."
- Replace that sentence in the `acceptance-fix` prompt with `{{acceptance_fix_scope}}`. The surrounding guidance stays fixed in the workflow: triage, not dismissing reproduced contract violations, preserving approved behavior, regression tests, test limits, commits, the retest reason, and the human-decision escape hatch.

**Key decision: free-text instruction, not an enum.** The param carries the caller's own wording, the same way `artifact_validation_instruction` does. Alternatives considered:

- An enum such as `all|change` with both wordings built into the workflow. Rejected: the issue gives the narrower wording only as an example and says the factory will supply it from its own workflows. An enum would put the factory's policy in Agent Runner and need a new release whenever that policy changes.
- A boolean that appends a fixed restriction. Rejected for the same reason, and because two sentences that conflict ("resolve every…" then "but not…") make a weaker prompt than one sentence that replaces the other.

Only the scope sentence becomes caller-controlled. The rest of the prompt keeps the safety guidance every caller relies on.

Tests check that the param is declared with the exact default, that the `acceptance-fix` prompt interpolates it in place of the hard-coded sentence, and that rendering with the default gives the same text as before. They also check that a caller-supplied value reaches the lead's prompt. Details belong in design and test-plan.

## Out of Scope

- Changing the factory's own workflows (`.agent-runner/workflows/factory-*.yaml`) to pass a narrower scope. The issue assigns this to a separate change.
- Threading the param through `core:implement-change`, `core:audited-implement-change`, or `core:orchestrated-implement-change`. The factory calls `builtin:core/verify-change-v1.0.yaml` directly, and no wrapper caller has asked for it. Adding it later is additive.
- Scoping other steps that carry a similar mandate, such as the `acceptance-push` repair prompt or `core:accept-change`.
- Any change to files under `.github/workflows`, and any new test suite or CI job.
- Enforcing the scope with tooling, for example blocking edits to `.github/workflows`. The param changes guidance only.

## Impact

- `workflows/core/verify-change-v1.0.yaml`: new param, and one prompt sentence becomes an interpolation. This ships in the binary because built-in workflows are embedded.
- `openspec/specs/builtin-workflows/spec.md`: the verify-change requirement changes, and a scenario is added.
- `docs/built-in-workflows.md`: the `core:verify-change` row documents the param.
- `workflows/verify_change_test.go` (and possibly `internal/exec/verify_change_workflow_test.go`): new tests in the existing suites.
- Existing callers see no change in behavior. Agent Factory gains the hook it needs for its follow-up change.
