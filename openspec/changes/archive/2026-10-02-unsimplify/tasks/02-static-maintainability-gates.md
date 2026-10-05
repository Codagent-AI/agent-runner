# Task: Add static maintainability gates to the validator and CI

## Goal

Add deterministic maintainability gates that run identically in Agent Validator and the existing CI `lint` job: a strict `golangci-lint` tier reported only on changed lines, a `deadcode` check for unreachable functions (after a one-time cleanup of current findings), and a `jscpd` duplication ceiling for non-Go files. These let tools own textual clones, complexity, nesting, and dead code almost for free, so the agent maintainability pass in `core:verify-change` only needs to look for what tools cannot see. Today nothing in this repository checks duplication, nesting depth, maintainability index, or unreachable exported functions.

## Background

Current state:

- `.golangci.yml` (golangci-lint v2, local 2.11.4) is the repository-wide ceiling: gocognit 35, cyclop 35, funlen 100/60, plus errcheck, govet, staticcheck, ineffassign, unused, gocritic, revive, misspell, unconvert, nolintlint (`require-explanation`, `require-specific`). `_test.go` is excluded from cyclop, gocognit, funlen, errcheck. `run.build-tags: [dev_audit]`. Leave this file and the existing validator `lint` check and CI lint step unchanged.
- `.validator/config.yml` entry point `"."` runs `build`, `lint`, `test`, `security-code`, each wrapped by `.validator/go-offline.sh` (sets `GOPROXY=off`, a project-local module cache, adds `$GOPATH/bin` and `~/go/bin` to `PATH`).
- `.github/workflows/ci.yml` runs on pull requests to `main` and `dev`; its `lint` job does checkout, `actions/setup-go@v5` (go 1.26), then `golangci/golangci-lint-action@v7`.
- `Makefile` target `lint:` runs `golangci-lint run ./...`.
- `docs/development.md` lists tool installs (`go install … golangci-lint`, `gosec`, `govulncheck`) and has a Linting section describing `.golangci.yml`, and a commands table row for `make lint`.

Constraints:

- No new CI jobs or test suites; all new CI work goes as extra steps in the existing `lint` job, with tools installed as setup steps there.
- No Runner (Go) behavior change beyond the dead-code removals.
- Do not pay down existing complexity, nesting, or clone findings beyond what `deadcode` needs.
- Scripts must not download tools at run time; validator runs may lack network access. They fail with an install hint instead.

### 1. Strict lint tier

New file `.golangci-strict.yml`:

- `version: "2"`, `run.build-tags: [dev_audit]`, `linters.default: none`.
- Enabled: `gocognit` (min-complexity 25), `cyclop` (max-complexity 25), `dupl` (threshold 100), `nestif` (min-complexity 5), `maintidx` (default, under 20), and `nolintlint` (`require-explanation: true`, `require-specific: true`), so a suppression of a strict-only linter must name the linter and give a reason.
- Exclusions: `_test.go` excluded from `gocognit`, `cyclop`, and `maintidx`. `dupl` and `nestif` apply to tests.
- `issues.max-issues-per-linter: 0` and `issues.max-same-issues: 0`.
- No diff base in the file (no `new-from-merge-base`); each environment supplies its own, because a base in the file would mis-scope pull requests into `dev`.

Validator check in `.validator/config.yml`, entry point `"."`:

```yaml
      - lint-strict:
          command: ./.validator/go-offline.sh golangci-lint run -c .golangci-strict.yml --new-from-merge-base=origin/main ./...
```

CI: add a second step to the existing `lint` job after the current `golangci-lint-action` step:

```yaml
      - name: golangci-lint (strict, new issues only)
        uses: golangci/golangci-lint-action@v7
        with:
          args: -c .golangci-strict.yml
          only-new-issues: true
```

Verified during design: the ceiling config's `nolintlint` does not report a `//nolint:dupl // <reason>` directive (dupl is not enabled there), so that suppression passes the ceiling; the strict config's own `nolintlint` reports a bare `//nolint:dupl` (`should provide explanation`). gocognit, cyclop, and `maintidx` report on the function declaration line and `nestif` on the outermost `if`, so strict limits apply to new functions and new nests only; that gap is accepted.

### 2. Dead-code check

New script `.validator/deadcode.sh` (executable, `#!/usr/bin/env bash`, `set -euo pipefail`), called by both validator and CI:

1. If `deadcode` is not on `PATH`, fail with an install hint (`go install golang.org/x/tools/cmd/deadcode@v0.50.0`).
2. Run `deadcode -test ./...` and `deadcode -test -tags dev_audit ./...`. A nonzero exit from either fails the script with the tool's stderr.
3. Report the intersection of the two outputs (functions unreachable in both builds). Print it and exit 1 if non-empty; exit 0 otherwise. (`deadcode` itself exits 0 when it reports findings, hence the wrapper.)

Validator check (entry point `"."`): `deadcode` with command `./.validator/go-offline.sh ./.validator/deadcode.sh`. CI `lint` job steps: `go install golang.org/x/tools/cmd/deadcode@v0.50.0`, then `./.validator/deadcode.sh`.

Rationale: without `-test`, most findings are functions only tests call (legitimate seams); the intersection avoids false positives for code reachable under only one build tag. Accepted gap: functions in files compiled into only one build (for example `internal/devaudit/provider_disabled.go`) are not checked.

One-time cleanup: the design-time baseline intersection is six functions. Remove each, together with any now-unused helpers or tests that exist only to exercise it:

- `internal/agentcall/contract.go` `ToolNames`
- `internal/audit/checkpoint.go` `SortGitFileStats`
- `internal/runlock/runlock.go` `CheckPID`
- `internal/tuistyle/format.go` `LerpColor` and `SpinnerFrame`
- `internal/tuistyle/logo.go` `LogoBlockHeight`

Re-run the script after removal; if it reports anything else (for example code orphaned by the removals), clean that up too until it exits 0.

### 3. Duplication ceiling for non-Go files

New config `.jscpd.json` at the repository root:

- `format`: `["yaml", "bash"]`
- `minTokens`: 70
- `threshold`: 3.5
- `ignore`: `worktrees/**`, `testdata/**`, `openspec/**`, `.validator/cache/**`, `validator_logs/**`, `**/node_modules/**`, `.git/**`. These must be root-anchored; a `**/openspec/**` pattern would wrongly exclude `workflows/openspec/`.
- `reporters`: `["console"]`, `gitignore: true`

New script `.validator/duplication.sh` (executable, `set -euo pipefail`):

1. Fail with `npm install -g jscpd@4.3.0` as the hint if `jscpd` is missing or `jscpd --version` is not `4.3.0`.
2. Run `jscpd .` from the repository root (config from `.jscpd.json`); its exit code is the check result.

Validator check (entry point `"."`): `duplication` with command `./.validator/duplication.sh` (not wrapped by `go-offline.sh`; it is not a Go tool). CI `lint` job steps: `actions/setup-node`, `npm install -g jscpd@4.3.0`, then `./.validator/duplication.sh`.

Design-time baseline: 3.15% (240 of 7,628 lines, 9 clones; YAML 4.92%, bash 1.60%), about 26 lines of headroom under 3.5%. Confirm the measured value is still under the threshold with this change's other edits; do not raise the threshold or refactor existing YAML to make room.

### 4. Developer provisioning and commands

- `docs/development.md`: add `go install golang.org/x/tools/cmd/deadcode@v0.50.0` and `npm install -g jscpd@4.3.0` to the install list; in the Linting section, describe the two lint tiers (ceiling `.golangci.yml`, strict `.golangci-strict.yml` on changed lines only, with `//nolint:<linter> // <reason>` allowed) and the two scripts; update the `make lint` description in the commands table.
- `Makefile`: `make lint` runs the ceiling (`golangci-lint run ./...`), then `golangci-lint run -c .golangci-strict.yml --new-from-merge-base=origin/main ./...`, then `./.validator/deadcode.sh` and `./.validator/duplication.sh`.

## Spec

This task implements no specification requirement; `openspec/changes/unsimplify/specs/` covers only the workflow step. The behavior to deliver is defined by the design decisions above.

## Test Plan

`openspec/changes/unsimplify/test-plan.md` assigns no `INT-*` or `E2E-*` obligations. The gates are proven by running them against this repository in Agent Validator and in the existing CI `lint` job (the CI run happens on this change's pull request, after implementation); do not add fixture-based script tests or new CI jobs. The full violation-injection probe set (gocognit/cyclop, Go clones including in `_test.go`, nesting, dead exported functions, YAML/shell clones above 3.5%, suppressions) belongs to the later acceptance pass in a scratch clone, not to this task. A quick local sanity check that a gate fails on an injected violation is fine, but remove the injection and never commit it.

## Done When

- `.golangci-strict.yml`, `.jscpd.json`, `.validator/deadcode.sh`, and `.validator/duplication.sh` exist as specified; both scripts are executable.
- `.validator/config.yml` entry point `"."` has `lint-strict`, `deadcode`, and `duplication` checks with the commands above; existing checks unchanged.
- `.github/workflows/ci.yml`'s existing `lint` job has the strict lint step, deadcode install and run steps, and Node setup, jscpd install, and duplication run steps; no new job.
- The six dead functions (and any helpers or tests that only served them) are removed, and `./.validator/go-offline.sh ./.validator/deadcode.sh` exits 0.
- `./.validator/duplication.sh` exits 0 at the 3.5% threshold.
- `golangci-lint run -c .golangci-strict.yml --new-from-merge-base=origin/main ./...` passes on this branch, and a bare `//nolint:dupl` is reported by the strict tier while `//nolint:dupl // <reason>` passes both tiers.
- `docs/development.md` and the `Makefile` `lint` target are updated as described.
- `make lint` and `make test` pass.
- An Agent Validator run on the branch executes the new `lint-strict`, `deadcode`, and `duplication` checks, and all three pass alongside the existing checks.
