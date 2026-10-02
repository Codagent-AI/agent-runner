# Development Guide

## Prerequisites

- The Go version declared in [../go.mod](../go.mod)
- At least one supported agent CLI installed and authenticated if you run agent-backed workflows locally: Claude Code, Codex, Copilot, Cursor, or OpenCode

## Go toolchain setup

Install Go via Homebrew:

```bash
brew install go
```

Install required development tools:

```bash
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
go install golang.org/x/tools/cmd/deadcode@v0.50.0
npm install -g jscpd@4.3.0
```

The Go tools install to `~/go/bin/`, and `jscpd` installs to npm's global bin directory (`$(npm prefix -g)/bin`). To make both available system-wide (including to non-interactive shells like those used by Agent Validator), add the paths via `/etc/paths.d/`:

```bash
echo "$HOME/go/bin" | sudo tee /etc/paths.d/go
echo "$(npm prefix -g)/bin" | sudo tee /etc/paths.d/npm-global
```

Open a new terminal afterwards. Verify with:

```bash
/bin/sh -c 'which golangci-lint jscpd'
```

If you only add `~/go/bin` to `.zshrc`, it will work in your terminal but **not** in tools that spawn `/bin/sh` subprocesses.

## Make targets

To build from source, use the Go version declared in [../go.mod](../go.mod):

```bash
make build       # compiles to bin/agent-runner
make test        # run tests
make lint        # run maintainability gates
```

### Build and quality

| Target | Command | Description |
|--------|---------|-------------|
| `make build` | `go build -o bin/agent-runner ./cmd/agent-runner` | Compile binary |
| `make test` | `go test -tags dev_audit ./...` | Run all tests, including development audit code |
| `make test-verbose` | `go test -v ./...` | Run tests with output |
| `make test-cover` | `go test -coverprofile=...` | Run tests with coverage report |
| `make lint` | `golangci-lint`, `deadcode`, `jscpd` | Run ceiling lint, changed-line strict lint, dead-code and duplication gates |
| `make fmt` | `goimports -w .` | Format code |

### Running without building (`./dev.sh`)

`./dev.sh` is a thin wrapper around `go run` that passes all arguments through unchanged. Use it exactly like the compiled binary:

```bash
./dev.sh openspec:plan-change my-change
./dev.sh --validate openspec:plan-change change_name=my-change
./dev.sh --resume plan-change-2026-04-03T23-19-18-552111Z
```

> **Why not `make`?** `make` intercepts `--flag` arguments as its own options, so flags like `--resume` can't be passed through. `./dev.sh` avoids this.

## Linting

The project uses two golangci-lint v2 tiers. `.golangci.yml` is the repository-wide ceiling. Key settings:

- **gocognit**: max complexity 35
- **funlen**: max 100 lines / 60 statements
- **cyclop**: max cyclomatic complexity 35
- **gocritic**: diagnostic, style, and performance checks enabled
- **nolintlint**: all `//nolint` directives must have an explanation and be linter-specific

Test files have relaxed rules (see `.golangci.yml` exclusions).

`.golangci-strict.yml` applies tighter complexity limits, duplication, nesting, and maintainability checks only to changed lines relative to `origin/main` locally or to the pull request in CI. A justified suppression uses `//nolint:<linter> // <reason>`.

`./.validator/deadcode.sh` reports functions unreachable in both regular and `dev_audit` test builds. `./.validator/duplication.sh` enforces a 3.5% duplication ceiling for YAML and shell files. Both scripts require their tools to be installed beforehand.

## Testing

Tests live next to source files (Go convention). Run a specific package:

```bash
go test ./internal/runner -v
go test ./internal/exec -run TestExecuteAgentStep
```

The project uses `google/go-cmp` for test comparisons instead of `reflect.DeepEqual`.

Testability is achieved through interfaces (`ProcessRunner`, `GlobExpander`, `Logger`) rather than mocking frameworks. Test files define their own stub implementations.

## Security scanning

```bash
gosec ./...         # static security analysis
govulncheck ./...   # known vulnerability detection
```

Both are also run by the validator checks (see `.validator/checks/`).

## Validating changes

Use the `/validator-run` skill to run the full quality gate suite before committing. This runs all validator checks (build, test, lint, security) and code quality reviews, then fixes any issues found.

## Development audit worksheet version

The reporter writes `step_value_v2` rows with audit-level judge usage after the original v1 columns. On its first delivery to a tab with the exact `step_value_v1` header and no trailing header content, it writes only the seven new header cells (`AF1:AL1`) and then appends the row. Existing data rows remain unchanged. Any other header mismatch leaves the report pending for retry.

After a tab upgrades, an older Runner binary still expecting v1 will leave its deliveries pending until it is updated. Judge columns are repeated on each observation row, so downstream totals must de-duplicate by `audit_run_id`. The detailed per-attempt records and coverage are in the audit run's `judge-usage/` and `local-report.json` artifacts.
