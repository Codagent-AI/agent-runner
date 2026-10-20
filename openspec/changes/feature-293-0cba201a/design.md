## Context

`core:verify-change` (`workflows/core/verify-change-v1.0.yaml`) runs bounded acceptance rounds. After each round that does not converge, the `acceptance-fix` step resumes the `lead-agent` session with a fixed prompt. One sentence of that prompt sets the fix scope:

> Resolve every high-confidence finding you can, including implementation, CI, PR-state, tooling, setup, and environmental problems.

Agent Factory calls `builtin:core/verify-change-v1.0.yaml` directly (`.agent-runner/workflows/factory-feature-v1.0.yaml`, step `verify`). It needs a narrower scope so the lead stops repairing unrelated CI, such as `.github/workflows` edits its token cannot push. The workflow already exposes caller-set tuning params (`skip_implementation_review`, `stop_unchanged_acceptance`), and each is validated at the top of the workflow by a bundled script.

Relevant runtime facts:

- **Param defaults.** `validateSubWorkflowParams` (`internal/exec/subworkflow.go`) and `validateParams` (`internal/runner/runner.go`) apply a param's `default` only when the caller omits the key. An explicitly passed empty string is kept as `""`.
- **Interpolation.** `textfmt.InterpolateTyped` substitutes placeholders in one pass (`ReplaceAllStringFunc`). A param value that itself contains `{{...}}` is inserted literally and not interpolated again.
- **Script inputs.** These are interpolated, then sent to the script as one JSON object on stdin (`buildScriptInput`, `json.Marshal`). The payload is a single line, and control characters and quotes in a value are JSON-escaped. Under the repository's Go (1.26), a value of space, tab, LF, VT, FF, and CR encodes as a literal space then `\t`, `\n`, `\u000b`, `\f`, and `\r`. A literal backslash encodes as `\\`, and non-ASCII characters such as U+00A0 are emitted raw or as `\uXXXX` (U+2028). Bundled validation scripts read it with `jq` and fall back to `sed` when `jq` is missing (`validate-boolean-param.sh`).

## Goals / Non-Goals

**Goals:**

- Callers can replace the acceptance-fix scope sentence with their own wording through `acceptance_fix_scope`.
- Callers that omit the param get a fix prompt byte-identical to today's.
- An empty or whitespace-only value fails before any agent step.
- Tests in the existing suites cover all of the above.

**Non-Goals:**

- Threading the param through `core:implement-change`, `core:audited-implement-change`, or `core:orchestrated-implement-change`.
- Changing the factory's `.agent-runner/workflows/*` files or anything under `.github/workflows`.
- Scoping any other prompt, such as the `acceptance-push` repair or `core:accept-change`.
- Enforcing the scope mechanically, for example by blocking edits to certain paths.

## Approach

### 1. Param declaration (`verify-change-v1.0.yaml`)

Append after `stop_unchanged_acceptance`, with a short comment in the style of the existing param comments:

```yaml
  # Sets which findings the lead resolves in each acceptance fix. A caller can
  # narrow it, for example to failures the change caused, so unrelated CI or
  # environment problems go to the assumptions ledger instead.
  - name: acceptance_fix_scope
    required: false
    default: "Resolve every high-confidence finding you can, including implementation, CI, PR-state, tooling, setup, and environmental problems."
```

### 2. Prompt substitution

In the `acceptance-fix` prompt, replace that exact sentence with `{{acceptance_fix_scope}}`. The surrounding text stays as it is. A single space separates the placeholder from the sentence before and the sentence after, so with the default the rendered paragraph is unchanged:

```
… with your reasoning. {{acceptance_fix_scope}} Preserve approved behavior, scope, and design; …
```

No other step references the param.

### 3. Non-empty validation

Add a reusable bundled script `workflows/core/validate-nonempty-param.sh`, modeled on `validate-boolean-param.sh`. It reads `{"name": …, "value": …}` from stdin. Both branches share one predicate. A value is **blank** when it is empty or every character is one of the six ASCII whitespace characters: space, `\t`, `\n`, `\v`, `\f`, `\r`. Anything else makes it non-blank, including non-ASCII whitespace. A blank value prints `<name> must not be empty` to stderr and exits 1; a non-blank value exits 0.

- **With `jq`:** `value=$(jq -r '.value // ""')`, then reject when `printf '%s' "$value" | LC_ALL=C tr -d ' \t\n\v\f\r'` is empty. The set is spelled out rather than `[:space:]` so the result does not depend on locale.
- **Without `jq`:** work on the raw JSON string body.
  1. Extract the text after `"value":"` up to the first `"`, using the same `sed` shape as the boolean script.
  2. Replace every escaped backslash pair (`\\`) with a non-whitespace placeholder such as `x`, so literal backslash text like `\\f` cannot be mistaken for an escape.
  3. Delete literal spaces, the short escapes `\t`, `\n`, `\r`, and `\f`, and the `\u` escapes `\u0009`, `\u000a`, `\u000b`, `\u000c`, `\u000d`, and `\u0020` (hex case-insensitive). The `\u` set also covers encoders that emit `\u` forms.
  4. Reject if nothing remains.

  After step 2, every remaining backslash starts a real escape, so step 3 removes exactly the encodings of the six whitespace characters. If step 1 stops early at an escaped quote, the value contains `"` and the cut-off text ends in a backslash, so the value is correctly non-blank. Every other character leaves something behind, so both branches agree.

Add a step `validate-acceptance-fix-scope` that runs this script with `name: acceptance_fix_scope` and `value: "{{acceptance_fix_scope}}"`. Place it **after `validate-stop-unchanged-acceptance` and before `validate-skip-implementation-review`**, so it runs before `review-assumptions` and every other agent step. The new step order starts:

```
validate-change-name
validate-skip-validator
validate-stop-unchanged-acceptance
validate-acceptance-fix-scope        # new
validate-skip-implementation-review
review-assumptions
…
```

This placement keeps the `validate-skip-implementation-review`…`simplify` range that `TestBuiltinVerifyChangeSkipImplementationReview` slices free of the new step.

### 4. Docs and spec

- **Docs:** extend the `core:verify-change` row in `docs/built-in-workflows.md` with one sentence on `acceptance_fix_scope`: its default, that it replaces only the scope sentence of the acceptance fix, and that an empty value is rejected.
- **Spec:** the delta spec in this change is already written. Archiving merges it into `openspec/specs/builtin-workflows/spec.md`.

### Data flow

```
caller params ──► param resolution (default if omitted; "" kept)
                    │
                    ├─► validate-acceptance-fix-scope (JSON stdin) ──► fail if blank
                    │
                    └─► acceptance-fix prompt: one-pass interpolation of {{acceptance_fix_scope}}
                                               ──► lead-agent session
```

## Decisions

- **New generic `validate-nonempty-param.sh` over an inline `command` step.** A shell `command` would interpolate arbitrary caller text into a shell string, a quoting and injection hazard. The script path receives the value as JSON on stdin, and other params can reuse the script. Extending `validate-boolean-param.sh` with a mode flag was rejected because its name and contract are boolean-specific.
- **Step placement before `validate-skip-implementation-review`.** All validations still run before any agent step, and existing run-level test slices stay valid.
- **The scope sentence is the whole substitution unit.** The placeholder replaces exactly one sentence, so the default reproduces today's text and the rest of the safety guidance stays fixed (spec scenario "Caller narrows the acceptance-fix scope").
- **No re-interpolation of the value.** This comes from the existing one-pass interpolation. A caller value containing `{{session_dir}}` reaches the lead literally. Callers that need paths interpolate them in their own workflow before passing the param. Documenting this in the param comment is not needed.

## Testing Strategy

All tests go in existing suites: the `test` job's `go test ./...`.

`workflows/verify_change_test.go`:

- **`TestCoreVerifyChangeShape`:** add `{Name: "acceptance_fix_scope", Default: "<exact sentence>"}` to `wantParams`, and add `validate-acceptance-fix-scope` to `wantSteps` in its position.
- **New `TestCoreVerifyChangeAcceptanceFixScope`:**
  - The validation step uses `validate-nonempty-param.sh` with `name: acceptance_fix_scope` and `value: "{{acceptance_fix_scope}}"`, and comes before `review-assumptions`.
  - The `acceptance-fix` prompt contains `{{acceptance_fix_scope}}` exactly once and no longer contains the literal default sentence.
  - Interpolating the prompt with the default gives the sentence between "…with your reasoning. " and " Preserve approved behavior", proving the default rendering matches today's.
  - Interpolating with a custom value contains that value and still contains the anti-dismissal and "Preserve approved behavior" guidance.
  - Walking all steps, including nested loop steps and repair prompts, `{{acceptance_fix_scope}}` appears only in `validate-acceptance-fix-scope` and `acceptance-fix` (scope isolation).
- **New script test for `validate-nonempty-param.sh`:** payloads built with `json.Marshal`. Blank cases: empty, spaces, `\n\t\r` only, form feed only, vertical tab only, and all six whitespace characters mixed. Non-blank cases: normal text, text with quotes and embedded newlines, literal backslash escape text (`\f`, `\u000b`, and `\\` typed as characters), a lone backslash, U+00A0 alone, and U+2028 alone. Run each case both with `jq` on `PATH` and with a no-`jq` `PATH` built from symlinks, following the `no-jq` pattern in `workflows/ci_status_gate_test.go`. Check the exit code and that stderr names the param.

`internal/exec/verify_change_workflow_test.go`:

- **`runVerifyChangeAcceptance`:** this harness dispatches the workflow tail directly with a hand-built params map. It bypasses default resolution, so `{{acceptance_fix_scope}}` would fail to interpolate. Make the harness fill any declared param missing from its map with the workflow's `Default`, and let it accept an optional scope.
- **`acceptanceRoundRunner.RunAgent`:** record the `acceptance-fix` prompt text from `options.Args`. Add cases asserting:
  - the default run's fix prompt contains the default sentence;
  - a run with a custom `acceptance_fix_scope` has that text in its fix prompt and not the default sentence.
- **Run-level empty check:** dispatch the steps from `validate-acceptance-fix-scope` through `review-assumptions` with `acceptance_fix_scope: "  "`. Assert the outcome is failed and only the validation script ran. The fake runner's `RunScript` must execute `validate-nonempty-param.sh` for real, as `implementationReviewRunner` does.

## Risks / Trade-offs

- **Free text gives callers enough room to weaken the lead.** Only the scope sentence is replaceable, and the anti-dismissal and preservation guidance stays fixed, which bounds the risk. The factory owns its wording.
- **The `sed` fallback is weaker than `jq`.** It only has to tell blank from non-blank against one explicit ASCII-whitespace predicate. It neutralizes escaped backslashes before stripping whitespace escapes, so it agrees with `jq` on single-line JSON from `json.Marshal`, including `\f` and `\u000b`. The no-`jq` parity cases guard it. An earlier draft stripped only space, `\n`, `\t`, and `\r`, and would have accepted a form-feed-only or vertical-tab-only value without `jq` (approach review AR-001).
- **Non-ASCII whitespace counts as content.** A value of only U+00A0 or U+2028 passes validation. This keeps both branches locale-independent; such a value is not a plausible misconfiguration the way an empty interpolation is.
- **Prompt drift.** A future edit to the surrounding paragraph could break the default-rendering test. That is intended: the test pins the compatibility promise.

## Migration Plan

None needed. The change is additive and the default preserves behavior. It ships in the next binary because built-in workflows are embedded. Agent Factory adopts it in its own change by adding `acceptance_fix_scope: "<narrow wording>"` to its `verify` step's params. Rollback is reverting the commit. Sub-workflow param resolution does not reject undeclared params, so a caller that already passes `acceptance_fix_scope` keeps running, silently back on the default scope.
