## Context

`core:verify-change` (`workflows/core/verify-change-v1.0.yaml`) runs a `simplify` step in the `lead-agent` session that invokes Claude Code's built-in `/simplify` (four reviewer subagents on the lead profile). PR #182 (issue #177) recently added defect-routing instructions to that prompt. Every current structured-change path reaches this step through `core:implement-change`: `openspec:change-v2`, `openspec:implement-change-v2`, `spec-driven:change-v2`, and `spec-driven:implement-change-v2`. The legacy `openspec:implement-change-v1` and `spec-driven:implement-change-v1` workflows have their own `simplify` step and are out of scope.

Agent-runner's static checks today:

- `.golangci.yml` (golangci-lint v2, local 2.11.4): errcheck, govet, staticcheck, ineffassign, unused, gocognit 35, funlen 100/60, cyclop 35, gocritic, revive, misspell, unconvert, nolintlint (`require-explanation`, `require-specific`). `_test.go` is excluded from cyclop, gocognit, funlen, and errcheck. Build tag `dev_audit`.
- `.validator/config.yml` entry point `"."` runs `build`, `lint`, `test`, and `security-code`, each wrapped by `.validator/go-offline.sh` (sets `GOPROXY=off`, a project-local module cache, and adds `$GOPATH/bin` and `~/go/bin` to `PATH`).
- `.github/workflows/ci.yml` runs on pull requests to `main` and `dev`, with jobs `build`, `lint` (`golangci/golangci-lint-action@v7`), `test`, `security-code` (`go install gosec@latest`), and `security-deps`.
- Developer tool installation is documented in `docs/development.md` (`go install … golangci-lint`, `gosec`, `govulncheck` into `~/go/bin`).

Nothing checks duplication, nesting depth, maintainability index, or unreachable exported functions.

## Goals / Non-Goals

**Goals:**

- Replace the built-in `/simplify` in `core:verify-change` with a single-pass, diff-scoped, behavior-preserving prompt that runs in a fresh `implementor` session.
- Add deterministic maintainability gates that run identically in Agent Validator and CI: a strict lint tier on changed lines, a dead-code check, and a duplication ceiling for non-Go files.

**Non-Goals:**

- Changing the legacy v1 implement-change workflows or the `require-simplify-decisions-resolved` gate.
- Paying down existing complexity, nesting, or clone findings.
- A function-range wrapper that holds edited existing functions to the strict complexity limits.
- Any Runner (Go) behavior change; workflows already support `agent:` with `session: new`.

## Approach

### 1. Workflow step

In `workflows/core/verify-change-v1.0.yaml`, replace the `simplify` step with:

```yaml
  - id: simplify
    agent: implementor
    session: new
    mode: autonomous
    prompt: |
      Do a maintainability pass over {{change_label}} "{{change_name}}" (artifacts in `{{change_dir}}/`), covering the branch's changes since its merge base with the default branch. Review it yourself in one pass; do not use a /simplify skill or reviewer subagents.

      Focus on what static analysis tools cannot catch within this change's diff:
      - Duplication: logic repeated across the change with edits, or different code doing the same job.
      - Altitude: logic at the wrong layer or abstraction level.

      Cleanup must preserve behavior exactly and must not grow the code. When unsure whether a change preserves behavior, leave it.

      If you happen to find a clear-cut defect in this change's own code, fix it, with a regression test where applicable. Record anything else, including defects in code this change did not touch and anything needing a product, scope, or design decision, in `{{session_dir}}/output/acceptance-assumptions.md`, replacing any `No unresolved assumptions or context gaps.` statement.

      Commit each cleanup and each defect fix separately, following the project's commit message conventions and prepending [{{step_id}}]. Do not run Agent Validator or push.
```

The prompt text is user-approved and must be used verbatim. The step keeps its id and its position between `verify-assumptions-handoff` and `run-validator`. This matches `core:implement-task`'s `generate-code` step (`agent: implementor`, `session: new`). The workflow's `sessions:` block is unchanged; `lead-agent` continues to serve the other lead steps, and its history no longer includes the simplification pass.

Tests in `workflows/verify_change_test.go`:

- Rewrite `TestCoreVerifyChangeSimplifyFixesDefects` (rename it to describe the new contract, e.g. `TestCoreVerifyChangeSimplifyPass`) to assert `Agent == "implementor"`, `Session == "new"`, `Mode == "autonomous"`, and that the prompt contains: `do not use a /simplify skill or reviewer subagents`, `{{session_dir}}/output/acceptance-assumptions.md`, `No unresolved assumptions or context gaps.`, `[{{step_id}}]`, and `Do not run Agent Validator or push`. Keep its `open-draft-pr` assertions unchanged.
- Leave the step-order test and the legacy-workflow tests (`require-simplify-decisions-resolved`) unchanged.

### 2. Strict lint tier

New file `.golangci-strict.yml`:

- `version: "2"`, `run.build-tags: [dev_audit]`, `linters.default: none`.
- Enabled: `gocognit` (min-complexity 25), `cyclop` (max-complexity 25), `dupl` (threshold 100), `nestif` (min-complexity 5), `maintidx` (default under 20), and `nolintlint` (`require-explanation: true`, `require-specific: true`) so a suppression of a strict-only linter must name the linter and give a reason.
- Exclusions: `_test.go` excluded from `gocognit`, `cyclop`, and `maintidx` (matching the ceiling's test policy). `dupl` and `nestif` apply to tests.
- `issues.max-issues-per-linter: 0` and `max-same-issues: 0`, so every new issue is shown.
- No diff base in the file; each environment supplies its own.

Validator check (`.validator/config.yml`, entry point `"."`):

```yaml
      - lint-strict:
          command: ./.validator/go-offline.sh golangci-lint run -c .golangci-strict.yml --new-from-merge-base=origin/main ./...
```

CI (`.github/workflows/ci.yml`): no new job. Add a second step to the existing `lint` job, after the current `golangci-lint-action` step:

```yaml
      - name: golangci-lint (strict, new issues only)
        uses: golangci/golangci-lint-action@v7
        with:
          args: -c .golangci-strict.yml
          only-new-issues: true
```

`only-new-issues` uses the pull request's diff from GitHub, so it is correct for PRs targeting either `main` or `dev` and needs no full-history checkout. The existing `lint` job and validator `lint` check are unchanged and remain the repository-wide ceiling.

Strict-only suppressions behave as follows, verified during design:

- The ceiling config's `nolintlint` does not report a directive for a linter that config does not enable (a `//nolint:dupl // <reason>` directive produced 0 issues), while it does report an unused directive for an enabled linter (a control `//nolint:gocritic` was flagged). So `//nolint:dupl // <reason>` passes the ceiling tier.
- Because the ceiling therefore never checks strict-only directives, the strict config enables its own `nolintlint`. With it enabled, a bare `//nolint:dupl` is reported (`should provide explanation`).

### 3. Dead-code check

New script `.validator/deadcode.sh` (executable, `set -euo pipefail`), called by both the validator and CI:

1. Fail with an install hint if `deadcode` is not on `PATH`.
2. Run `deadcode -test ./...` and `deadcode -test -tags dev_audit ./...`. A nonzero exit from either fails the script with the tool's stderr.
3. Report the intersection of the two outputs (functions unreachable in both builds). Print it and exit 1 if non-empty; exit 0 otherwise.

Validator check: `./.validator/go-offline.sh ./.validator/deadcode.sh`. CI: steps in the existing `lint` job that run `go install golang.org/x/tools/cmd/deadcode@v0.50.0`, then `./.validator/deadcode.sh`.

Measured baseline (design time, darwin; linux output matched):

| Mode | Findings |
|---|---|
| default, no `-test` | 48 |
| `dev_audit`, no `-test` | 43 |
| default, `-test` | 9 |
| `dev_audit`, `-test` | 7 |
| intersection of both `-test` runs | 6 |

One-time cleanup in this change removes the six: `internal/agentcall/contract.go` `ToolNames`, `internal/audit/checkpoint.go` `SortGitFileStats`, `internal/runlock/runlock.go` `CheckPID`, `internal/tuistyle/format.go` `LerpColor` and `SpinnerFrame`, and `internal/tuistyle/logo.go` `LogoBlockHeight`. Remove each with any now-unused helpers or tests that exist only to exercise it. Each deadcode run takes about 1 to 2 seconds.

### 4. Duplication ceiling

New config `.jscpd.json` at the repository root:

- `format`: `["yaml", "bash"]`
- `minTokens`: 70
- `threshold`: 3.5
- `ignore`: `worktrees/**`, `testdata/**`, `openspec/**`, `.validator/cache/**`, `validator_logs/**`, `**/node_modules/**`, `.git/**` (root-anchored; a `**/openspec/**` pattern would wrongly exclude `workflows/openspec/`)
- `reporters`: `["console"]`, `gitignore: true`
- `noSymlinks`: `true`. The tracked `.agents/skills` symlink points at `.claude/skills`; jscpd on Linux (CI) follows it and counts every skill script twice (5.88% measured), while on macOS it does not. Skipping symlinks gives the same 3.1% on both.

New script `.validator/duplication.sh`:

1. Fail with `npm install -g jscpd@4.3.0` as the hint if `jscpd` is missing or `jscpd --version` is not `4.3.0`.
2. Run `jscpd .` from the repository root (config picked up from `.jscpd.json`); its exit code is the check result.

Validator check: `./.validator/duplication.sh` (not wrapped by `go-offline.sh`; it is not a Go tool). CI: steps in the existing `lint` job using `actions/setup-node`, `npm install -g jscpd@4.3.0`, then `./.validator/duplication.sh`.

All new CI work lives in the existing `lint` job; no CI job or test suite is added.

Measured baseline with this scope: 3.15% (240 of 7,628 lines, 9 clones; YAML 4.92%, bash 1.60%), leaving about 26 lines of headroom under 3.5%.

### 5. Provisioning and developer commands

- `docs/development.md`: add `go install golang.org/x/tools/cmd/deadcode@v0.50.0` and `npm install -g jscpd@4.3.0` to the install list, and describe the two lint tiers and the two scripts in the Linting section.
- `Makefile`: `make lint` runs the ceiling, then `golangci-lint run -c .golangci-strict.yml --new-from-merge-base=origin/main ./...`, then `./.validator/deadcode.sh` and `./.validator/duplication.sh`.

## Decisions

- **Fresh `implementor` session instead of `lead-agent`.** The pass needs only the diff and the ledger, not the lead's conversation. Cost and model follow the user's implementor profile, and Codex-based profiles are covered because the prompt does not depend on a Claude-only skill. Rejected: a new built-in role (touches profiles, onboarding, and profilewrite for one step).
- **Two golangci-lint configurations.** One configuration cannot apply different thresholds to old and new code. `dupl` reports on the cloned lines, so it catches new clones inside old functions. gocognit, cyclop, and `maintidx` report on the function declaration and `nestif` on the outermost `if` of a nest (verified during design: a four-deep nest was reported on the outer `if` line), so edits inside existing functions or nests are not seen by the strict tier and stay bounded only by the ceiling. Rejected: lowering the ceiling to 25 repository-wide (unbounded refactor backlog) and a `go/ast` function-range wrapper (deferred until creep is observed).
- **No new CI jobs.** The strict lint, deadcode, and duplication checks run as extra steps in the existing `lint` job, with their tools installed as setup steps there, per the project rule against adding CI jobs or suites.
- **Diff base per environment.** The validator compares with `origin/main`; CI uses the action's PR diff. Putting `new-from-merge-base` in the config file would mis-scope pull requests into `dev`.
- **`deadcode -test`, intersection of both builds.** Without `-test`, most findings are functions only tests call (for example `config.Load`, `textfmt.InterpolateShellSafe`); removing them means rewriting tests and discarding legitimate seams. The intersection avoids false positives for code reachable only under one build tag. Accepted gap: functions in files compiled into only one build (for example `internal/devaudit/provider_disabled.go`) are not checked.
- **jscpd for non-Go files only, as a ceiling.** `dupl` is the per-change Go clone gate. jscpd has no diff mode, so it is a repository-wide ceiling just above today's level; it stops growth of workflow YAML and shell duplication, which nothing else checks.
- **`dupl` applies to tests.** Repeated test setup was a large share of the useful cleanup the old pass produced, and the strict tier only reports changed lines.
- **Pinned versions with presence checks.** `deadcode@v0.50.0` and `jscpd@4.3.0` are pinned in docs and CI; the scripts fail with an install hint rather than downloading at run time, since validator runs may lack network access.

## Risks / Trade-offs

- **Strict-tier friction on new code.** Agents may hit gocognit/cyclop 25 or `dupl` on new code and need to restructure. Mitigation: `//nolint:<linter> // <reason>` is allowed (nolintlint requires the explanation), and the agent pass and validator retry loop absorb the work.
- **Complexity, nesting, and maintainability creep in edited existing code** is not seen by the strict tier: gocognit, cyclop, and `maintidx` report on the unchanged declaration line and `nestif` on the unchanged outer `if`. Complexity stays bounded by the 35 ceiling; deeper nesting and a lower maintainability index inside existing code are not gated. Accepted; revisit with the function-range wrapper (or a baseline comparison) if observed.
- **jscpd ceiling permits new clones** while total duplication stays under 3.5%. Accepted; the agent pass looks for duplication within the diff.
- **Local and CI tool drift.** A developer with a different jscpd version gets a clear failure from the version check; deadcode version drift is unchecked but low risk.
- **Stale `origin/main` locally** widens or narrows the strict tier's diff. Accepted; the CI job uses the authoritative PR diff.

## Migration Plan

Land in one pull request: workflow step and test, strict config, scripts, `.jscpd.json`, validator checks, steps in the CI `lint` job, docs, Makefile, and the six dead-function removals. Rollback is reverting the pull request; no persisted state or user configuration changes.
