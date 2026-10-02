## Why

The `simplify` step in `core:verify-change` runs Claude Code's built-in `/simplify` in the `lead-agent` session: Opus at high effort with four reviewer subagents. Across 24 factory runs (Sep 20 to 28, 2026, analysis linked from #185):

- it cost about $3.70 per run (range $1.56 to $6.44), with a median wall time of 13 minutes; the four subagents were about 57% of that cost;
- no post-run audit rated it above medium value, and 9 of the 14 audits that assessed it called it the costliest step in the run;
- 7 of 24 runs (29%) introduced a defect that a later step caught, such as removed "redundant" index clamps, a public prop deleted as dead code, a shortened screenshot wait, and a refactor that grew the code by 304 lines. All seven came from the efficiency and simplification reviewers. The reuse and altitude reviewers produced the valuable changes.

At the same time, no Codagent repository has duplication detection, and the validator's code-quality review skips design, so `simplify` is currently the only maintainability pass. Removing it outright would leave nothing. The useful part is narrow: deterministic tools can own textual clones, complexity, nesting, and dead code nearly for free, leaving an agent to look only for what tools cannot see.

## What Changes

- **Replace the `simplify` step in `core:verify-change`** with a Codagent-owned, single-pass prompt that runs in a fresh `implementor` session instead of `lead-agent`. It no longer invokes `/simplify` or reviewer subagents. It looks only within the change's diff, for duplication that tools miss (edited copies, different code doing the same job) and for logic at the wrong layer. Cleanup must preserve behavior and must not grow the code. Defects it happens to find in the change's own code are fixed; everything else goes to the acceptance assumptions ledger. The step keeps its id, its position immediately before `run-validator`, and the `[simplify]` commit prefix.
- **Add static maintainability gates to agent-runner's own validator checks and CI**, in two tiers:
  - the existing `golangci-lint` configuration stays as a repository-wide ceiling (gocognit and cyclop at 35, funlen);
  - a new strict lint run applies gocognit and cyclop at 25, `dupl` at 100 tokens, `nestif` at 5, and `maintidx`, reporting only issues on lines the branch changed (against `origin/main` in the validator, and against the pull request's base branch in CI);
  - `deadcode` for unreachable functions in both the default and `dev_audit` builds, failing on any finding, with existing findings cleaned up once in this change;
  - `jscpd` (70-token minimum) over non-Go files (workflow YAML and shell scripts), as a ceiling set just above today's 3.0% (about 3.5%) so duplication there cannot grow. `dupl` already covers Go.
- **File companion issues** for static gates in agent-validator (detailed), and agent-evals, agent-factory, and and-scene (looser). Those repositories' changes are not part of this change.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `builtin-workflows`: the `core:verify-change` simplification step runs a Codagent-owned, diff-scoped, behavior-preserving pass in a fresh `implementor` session rather than the built-in `/simplify` in the `lead-agent` session.

## Technical Approach

**Workflow step.** `core:verify-change` already composes into every current structured-change path (`openspec:change-v2`, `openspec:implement-change-v2`, `spec-driven:change-v2`, `spec-driven:implement-change-v2`) through `core:implement-change`. Workflows already support `agent: implementor` with `session: new` (`core:implement-task` uses it), so no runner change is needed. The model and cost follow the user's `implementor` profile rather than being pinned in the workflow, which also covers Codex-based profiles that the built-in `/simplify` skill never reached. The approved prompt is:

```text
Do a maintainability pass over {{change_label}} "{{change_name}}" (artifacts in `{{change_dir}}/`), covering the branch's changes since its merge base with the default branch. Review it yourself in one pass; do not use a /simplify skill or reviewer subagents.

Focus on what static analysis tools cannot catch within this change's diff:
- Duplication: logic repeated across the change with edits, or different code doing the same job.
- Altitude: logic at the wrong layer or abstraction level.

Cleanup must preserve behavior exactly and must not grow the code. When unsure whether a change preserves behavior, leave it.

If you happen to find a clear-cut defect in this change's own code, fix it, with a regression test where applicable. Record anything else, including defects in code this change did not touch and anything needing a product, scope, or design decision, in `{{session_dir}}/output/acceptance-assumptions.md`, replacing any `No unresolved assumptions or context gaps.` statement.

Commit each cleanup and each defect fix separately, following the project's commit message conventions and prepending [{{step_id}}]. Do not run Agent Validator or push.
```

This keeps the defect-routing behavior that #177 just added (fix clear-cut defects, route decisions to the ledger, never list known follow-ups) while separating it from cleanup, which must not change behavior.

**Two-tier lint.** One `golangci-lint` configuration cannot apply different thresholds to old and new code, so the strict tier is a second configuration file run as its own check. `dupl` reports on the cloned lines, so it catches new clones even inside old functions. The other strict linters report on a line that may not change: gocognit, cyclop, and `maintidx` on the function declaration, and `nestif` on the outermost `if` of a nest. So their strict limits apply to new functions and new nests; edits inside existing functions or nests stay bounded only by the ceiling. Both tiers run as validator checks and in CI. The diff base is supplied by each environment rather than the configuration file: the validator check passes `--new-from-merge-base=origin/main`, and CI uses `golangci-lint-action`'s `only-new-issues`, which takes the pull request's diff against whichever base it targets (`main` or `dev`).

**Non-golangci tools.** `deadcode` and `jscpd` have no diff mode, so `deadcode` starts from a clean baseline after a one-time cleanup. `deadcode` reports findings but exits successfully, so the check wraps it to fail on any finding. It analyzes one build configuration at a time, so the check covers both the default and `dev_audit` builds; whether test code counts as reachable (`-test`) is a design decision. `jscpd` fails only when total duplication exceeds its threshold, so it is a ceiling, not a per-change check: a new clone passes while the total stays under it. Its unique value here is non-Go files, which nothing else checks. Measured with a 70-token minimum, those files are at 3.0% today (workflow YAML 4.5%, shell 1.6%), mostly mirrored openspec and spec-driven workflows and a copied `validate-change-name.sh`, so the ceiling starts just above that rather than at 2%.

## Out of Scope

- The legacy `openspec:implement-change-v1` and `spec-driven:implement-change-v1` workflows. Their `simplify` step and #177's `require-simplify-decisions-resolved` gate are unchanged.
- Searching the whole repository for existing code that new code could reuse. That belongs before implementation, in the design step, and is planned as a separate change.
- A wrapper that holds every function the diff touches to the strict complexity limits. Add it only if complexity creep in existing functions shows up.
- Paying down existing complexity, nesting, or clone findings beyond what `deadcode` needs.
- Skipping the pass for small diffs.
- Pinning a model for the pass, or adding a new agent role.
- Static gates in other repositories, which the companion issues cover, and any change to Agent Validator's built-in reviewers.

## Impact

- **Workflows:** `workflows/core/verify-change-v1.0.yaml`, plus its prompt-contract tests in `workflows/verify_change_test.go`.
- **Spec:** `openspec/specs/builtin-workflows/spec.md`, the verify-change requirement.
- **Lint and checks:** a new strict `golangci-lint` configuration; new `lint-strict`, `deadcode`, and `duplication` checks in `.validator/config.yml`; and matching CI steps in `.github/workflows/ci.yml`. The `deadcode` and `duplication` commands live in small scripts under `.validator/` that both the validator and CI call, so the two cannot drift. These add pinned tool dependencies: `golang.org/x/tools/cmd/deadcode`, pre-installed locally like `gosec` and `golangci-lint` so it works under `.validator/go-offline.sh`, and `jscpd`, which needs Node and a pinned local install because the validator cannot download it at run time. CI installs both pinned versions.
- **Code:** a one-time removal of exported functions that `deadcode` reports as unreachable.
- **Users:** structured-change runs make the simplification pass in their `implementor` profile instead of `lead`. With the built-in default profile (Opus at high effort for both roles), the savings come from dropping the four subagents. With a cheaper implementor profile, they are larger.
- **Other repositories:** companion issues [agent-validator#174](https://github.com/Codagent-AI/agent-validator/issues/174), [agent-evals#53](https://github.com/Codagent-AI/agent-evals/issues/53), [agent-factory#74](https://github.com/Codagent-AI/agent-factory/issues/74), and [and-scene#41](https://github.com/Codagent-AI/and-scene/issues/41).
