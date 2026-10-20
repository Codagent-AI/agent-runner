# Decisions

## propose: verdict
- **Decision**: Go. It is a small, additive change to built-in workflow YAML that follows the existing caller-set param pattern. It fixes a costly real failure (a 2h10m run whose push was rejected), and its default leaves every existing caller unchanged.
- **Alternatives considered**: No-go, and have the factory fork `core:verify-change` instead. Rejected: a fork would drift from the built-in workflow, and the issue asks for the param in Agent Runner.
- **Decision-bearing**: yes

## propose: not a direction-level stop
- **Decision**: Proceed without stopping. The issue states the wanted shape (an optional param whose default keeps today's wording), the change is additive with no change to any public interface or persisted format, and the factory-side change is explicitly a separate change.
- **Alternatives considered**: Stop, treating the free-text vs. enum question as a materially different reading. Rejected: both readings deliver what the issue asks for, and the issue's "for example" wording plus "Agent Factory will pass the narrower scope" point to caller-supplied text.
- **Decision-bearing**: yes

## propose: param shape
- **Decision**: A free-text instruction param `acceptance_fix_scope` that replaces the "Resolve every high-confidence finding…" sentence, with that exact sentence as its default. This mirrors `artifact_validation_instruction`.
- **Alternatives considered**: An enum (`all|change`) with both wordings in the workflow. A boolean that appends a fixed restriction. Rejected: both put the factory's policy in Agent Runner, and the boolean leaves the prompt with two conflicting sentences.
- **Decision-bearing**: yes

## propose: param name
- **Decision**: `acceptance_fix_scope`.
- **Alternatives considered**: `acceptance_fix_instruction` and `fix_scope`. The chosen name says it is the scope of the `acceptance-fix` step, matching the issue title.
- **Decision-bearing**: no

## propose: what becomes caller-controlled
- **Decision**: Only the scope sentence. The triage rules, the bar against dismissing reproduced defects, preserving approved behavior, regression tests, test limits, commits, the retest reason, and the human-decision guidance stay fixed.
- **Alternatives considered**: Make the whole prompt overridable. Rejected: callers would lose shared safety guidance, and the change would be much larger than asked.
- **Decision-bearing**: no

## propose: wrapper workflows
- **Decision**: Do not thread the param through `core:implement-change`, `core:audited-implement-change`, or `core:orchestrated-implement-change`.
- **Alternatives considered**: Thread it through every wrapper. Rejected: the factory calls `builtin:core/verify-change-v1.0.yaml` directly (`.agent-runner/workflows/factory-feature-v1.0.yaml`), no wrapper caller needs it, and adding it later is additive.
- **Decision-bearing**: no

## propose: factory workflow files
- **Decision**: Do not edit `.agent-runner/workflows/factory-*.yaml` in this change.
- **Alternatives considered**: Pass the narrow scope from the factory's feature workflow now. Rejected: the issue says that is a separate change.
- **Decision-bearing**: no

## proposal-review: findings
- **Decision**: The proposal review reported no findings (`proposal-review-findings.json` is an empty array), so nothing was applied or rejected and the proposal is unchanged.
- **Alternatives considered**: None.
- **Decision-bearing**: no

## spec: empty acceptance_fix_scope
- **Decision**: An empty or whitespace-only `acceptance_fix_scope` fails the workflow before any agent step runs, with an error naming the param. The runner passes an explicitly supplied empty value through instead of applying the default (`validateSubWorkflowParams` in `internal/exec/subworkflow.go`), so without this check the fix prompt would silently lose its scope sentence. The proposal's What Changes now says so.
- **Alternatives considered**: Fall back to the default wording. Rejected: a caller that meant to narrow the scope would silently get the broadest scope. Accept the empty value. Rejected: the lead would get no scope guidance at all.
- **Decision-bearing**: yes

## spec: requirement placement
- **Decision**: Modify the existing "Verify-change workflow" requirement in `builtin-workflows` (the param list, the validation list, and a new paragraph on the fix scope) and add four scenarios: the default is unchanged, a caller narrows the scope, the scope affects only the fix step, and an empty value is rejected.
- **Alternatives considered**: A separate ADDED requirement. Rejected: the requirement already lists every verify-change param and its validation, so splitting the param out would leave that list incomplete.
- **Decision-bearing**: no

## spec: scope isolation
- **Decision**: The value replaces only the scope sentence in the acceptance-fix prompt. No other step's prompt changes, including the acceptance-push repair prompt.
- **Alternatives considered**: Also apply the scope to the acceptance-push repair. Rejected: the proposal puts that out of scope, and that prompt already says to stop on missing credentials or token scope.
- **Decision-bearing**: no

## spec: default wording is normative
- **Decision**: The spec states the exact default sentence, so tests can check that existing callers' prompts are unchanged.
- **Alternatives considered**: Describe the default only loosely. Rejected: "existing callers behave exactly as they do now" is the issue's main compatibility promise and needs to be testable.
- **Decision-bearing**: no

## design: non-empty validation mechanism
- **Decision**: Add a reusable bundled script, `workflows/core/validate-nonempty-param.sh`, which reads `{name, value}` as JSON on stdin with `jq` and falls back to `sed`. Run it from a new `validate-acceptance-fix-scope` step.
- **Alternatives considered**: An inline `command` step that tests the value. Rejected: it would interpolate arbitrary caller text into shell. Adding a mode flag to `validate-boolean-param.sh`. Rejected: that script's name and contract are boolean-specific.
- **Decision-bearing**: no

## design: validation step placement
- **Decision**: Place `validate-acceptance-fix-scope` after `validate-stop-unchanged-acceptance` and before `validate-skip-implementation-review`. All validation still runs before any agent step, and the existing run-level test slice `validate-skip-implementation-review..simplify` is untouched.
- **Alternatives considered**: Directly before `review-assumptions`. Rejected: it would fall inside that test's slice and change its expected events.
- **Decision-bearing**: no

## design: test harness default resolution
- **Decision**: `runVerifyChangeAcceptance` in `internal/exec/verify_change_workflow_test.go` will fill any declared param missing from its params map with the workflow's default. It dispatches the tail without the runner's default resolution, so the new placeholder would otherwise fail to interpolate.
- **Alternatives considered**: Hard-code `acceptance_fix_scope` in the harness map. Rejected: the next optional param would break the harness again.
- **Decision-bearing**: no

## design: value is not re-interpolated
- **Decision**: Rely on the existing one-pass interpolation. A caller value containing `{{…}}` reaches the lead literally, and callers interpolate in their own workflow first.
- **Alternatives considered**: None needed; this is existing engine behavior.
- **Decision-bearing**: no

## test-plan: integration obligations
- **Decision**: Five integration obligations:
  - INT-001: a sentinel scope reaches only the `acceptance-fix` agent invocation.
  - INT-002: omitting the param delivers the declared default.
  - INT-003: a blank value fails at the real validation script before any agent step.
  - INT-004: a blank value passed through a sub-workflow call is rejected, not defaulted.
  - INT-005: the validation script agrees with and without `jq`.

  All go in the existing `internal/exec` and `workflows` suites.
- **Alternatives considered**: Assert prompt wording, such as the anti-dismissal sentence, in the plan's obligations. Rejected: the test-plan rules forbid prompt-content tests. The obligations use opaque sentinels and the default read from the loaded workflow, so they test delivery rather than copy. The design's unit-level shape checks stay a repo-convention matter for implementation.
- **Decision-bearing**: no

## test-plan: no end-to-end tests
- **Decision**: No E2E obligation. A real verify-change run needs agents, a GitHub remote, and a PR, and what an agent does with a narrower instruction has no stable assertion. The integration tests already exercise the real loader, param resolution, interpolation, scripts, and dispatch.
- **Alternatives considered**: A real-agent run through `./dev.sh`. Rejected: costly, it needs remote effects, and it adds no stable signal.
- **Decision-bearing**: no

## test-plan: acceptance envelope
- **Decision**:
  - Allowed: local `./dev.sh` runs in scratch with an isolated `HOME`, and at most a few US dollars of real-agent use.
  - Off limits: remote pushes, PR changes and any `gh` mutation; edits to `.github/workflows` or the factory workflows; real factory runs.
  - Fake runner and fake `gh` substitutes are permitted.
- **Alternatives considered**: Allow a real draft-PR run. Rejected: it has outward-facing effects and is not needed to observe param wiring.
- **Decision-bearing**: no

## approach-review: AR-001 (no-jq blank check misses \f and \u000b)
- **Decision**: Applied. Confirmed that Go 1.26 `json.Marshal` encodes form feed as `\f` and vertical tab as `\u000b`, so the drafted fallback would have accepted values the `jq` path rejects. The fix:
  - The spec now defines blank as empty or only ASCII whitespace (space, tab, LF, VT, FF, CR), with or without `jq`. Non-ASCII whitespace and literal backslash text count as content.
  - The `jq` branch uses an explicit `LC_ALL=C tr -d ' \t\n\v\f\r'` set.
  - The no-`jq` branch first neutralizes escaped backslash pairs, then strips spaces, the short escapes `\t \n \r \f`, and the `\u0009`–`\u000d`/`\u0020` forms.
  - INT-005 and the design's script test add blank cases (form feed only, vertical tab only, mixed whitespace) and non-blank cases (literal escape text, lone backslash, U+00A0, U+2028), with per-row expected results under both `PATH`s.
  - INT-003 adds dispatched form-feed and vertical-tab rows under both `PATH`s, proving rejection before any agent invocation.
- **Alternatives considered**: Use the locale-dependent `[:space:]` class. Rejected: the result could differ by host locale. Treat Unicode whitespace as blank. Rejected: the no-`jq` path would need Unicode decoding. Require `jq` or `python3` and drop the `sed` fallback. Rejected: it departs from the existing validation-script convention.
- **Decision-bearing**: no

## tasks: single implementation task
- **Decision**: One task covers the whole change: the workflow YAML, the new validation script, the unit and shape tests, INT-001 to INT-005, and the docs row. It links every artifact and states the done criteria (`make fmt`, `make lint`, `make test`, `openspec validate --strict`, and no diff under `.github/workflows` or `.agent-runner/workflows`).
- **Alternatives considered**: Split the script and the workflow into separate tasks. Rejected: the step instructions require exactly one task, and the parts are tightly coupled.
- **Decision-bearing**: no
