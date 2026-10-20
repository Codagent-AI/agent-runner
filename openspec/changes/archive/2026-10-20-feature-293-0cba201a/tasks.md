- [x] Implement the change described by these files using TDD, as `CLAUDE.md` requires: write each failing test before the code that makes it pass. Satisfy every spec scenario in `specs/` and every `INT-*` obligation in `test-plan.md`. `design.md` is the technical source of truth; the bullets below summarize it.
  - [proposal.md](proposal.md), [design.md](design.md), [test-plan.md](test-plan.md), [decisions.md](decisions.md)
  - [specs/builtin-workflows/spec.md](specs/builtin-workflows/spec.md)

  **Workflow** (`workflows/core/verify-change-v1.0.yaml`)
  - Add the param `acceptance_fix_scope` after `stop_unchanged_acceptance`, with `required: false`. Its `default` is exactly `Resolve every high-confidence finding you can, including implementation, CI, PR-state, tooling, setup, and environmental problems.` Give it a short comment in the style of the neighboring param comments.
  - In the `acceptance-fix` prompt, replace that exact sentence with `{{acceptance_fix_scope}}`, keeping one space on each side. The rendered default must be byte-identical to today's paragraph. Change nothing else in the prompt, and reference the param from no other step, including the `acceptance-push` repair.
  - Add the step `validate-acceptance-fix-scope` between `validate-stop-unchanged-acceptance` and `validate-skip-implementation-review`. It uses `script: validate-nonempty-param.sh` with `script_inputs: {name: acceptance_fix_scope, value: "{{acceptance_fix_scope}}"}`.
  - Do not touch the wrapper workflows (`implement-change`, `audited-implement-change`, `orchestrated-implement-change`), `.agent-runner/workflows/*`, or anything under `.github/workflows`.

  **Validation script** (`workflows/core/validate-nonempty-param.sh`, new, executable, modeled on `validate-boolean-param.sh`)
  - Read `{"name","value"}` JSON from stdin.
  - **Blank** means empty, or made only of the six ASCII whitespace characters: space, `\t`, `\n`, `\v`, `\f`, `\r`. A blank value prints `<name> must not be empty` to stderr and exits 1; anything else exits 0. Non-ASCII whitespace (U+00A0, U+2028) and literal backslash text count as content.
  - **With `jq`:** `jq -r '.value // ""'`, then `LC_ALL=C tr -d ' \t\n\v\f\r'`. Reject if nothing is left.
  - **Without `jq`:**
    1. Extract the raw string body after `"value":"` up to the first `"`.
    2. Replace every escaped backslash pair (`\\`) with a non-whitespace placeholder.
    3. Delete literal spaces, the escapes `\t`, `\n`, `\r`, and `\f`, and `\u0009`, `\u000a`, `\u000b`, `\u000c`, `\u000d`, and ` ` (hex case-insensitive).
    4. Reject if nothing remains.
  - The new file is under `workflows/core/`, so it is embedded with the namespace. Confirm that any embed or asset-list test (for example `workflows/embed_test.go`) picks it up or is updated.

  **Unit and shape tests** (`workflows/verify_change_test.go`)
  - `TestCoreVerifyChangeShape`: add `{Name: "acceptance_fix_scope", Default: <exact sentence>}` to `wantParams`, and add `validate-acceptance-fix-scope` to `wantSteps` in its position.
  - Add a test that the validation step uses `validate-nonempty-param.sh` with the right `name` and `value` inputs and comes before `review-assumptions`.
  - Add a test that `{{acceptance_fix_scope}}` appears exactly once in the `acceptance-fix` prompt and in no other step's prompt, command, or repair (walk nested steps). Rendering the prompt with the declared default must reproduce today's paragraph, so the default output is unchanged.

  **Tests from `test-plan.md`**
  - INT-001 and INT-002 (`internal/exec/verify_change_workflow_test.go`):
    - Make `runVerifyChangeAcceptance` fill every declared param missing from its map with the loaded workflow's `Default`, and accept an optional scope.
    - Have `acceptanceRoundRunner.RunAgent` record each agent invocation's prefix and args.
    - INT-001: a sentinel scope reaches only the `acceptance-fix` invocation and not the declared default; no other agent invocation contains the sentinel.
    - INT-002: with the param omitted, the `acceptance-fix` invocation receives the declared default.
    - Existing `TestBuiltinVerifyChangeAcceptanceRounds` event sequences stay unchanged.
  - INT-003 (`internal/exec`): dispatch `validate-change-name` through `review-assumptions` with a runner that executes scripts for real and fails the test on any agent call.
    - Rows: `""`, spaces, `"\n\t "`, form feed only, vertical tab only, and a valid control.
    - Run the blank rows under both the host `PATH` and a no-`jq` `PATH`.
    - Assert: a failed outcome, no agent call, and stderr naming `acceptance_fix_scope`. The control reaches `review-assumptions`.
  - INT-004 (`internal/exec`): a minimal parent workflow in a temp dir calls `builtin:core/verify-change-v1.0.yaml` with the required params and `acceptance_fix_scope: ""`. It fails at `validate-acceptance-fix-scope` with no agent call and an error naming the param.
  - INT-005 (`workflows`): run the shipped script with `json.Marshal` payloads under the host `PATH` and under a no-`jq` symlink `PATH`, following `workflows/ci_status_gate_test.go`. Use every row listed in `test-plan.md` INT-005.
    - Each row has its own expected exit code, identical under both `PATH`s.
    - Failures name the param on stderr.
    - Skip only the `jq` half when `jq` is absent.

  **Docs**
  - `docs/built-in-workflows.md`: extend the `core:verify-change` row with the optional `acceptance_fix_scope`. Cover its default (today's scope sentence), that it replaces only that sentence of the acceptance fix, and that an empty or whitespace-only value is rejected.

  **Done when**
  - `make fmt`, `make lint`, and `make test` pass.
  - `openspec validate feature-293-0cba201a --strict` passes.
  - `git diff --stat` shows no changes under `.github/workflows` or `.agent-runner/workflows`.
