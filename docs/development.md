# Development Guide

## Prerequisites

- [Go](https://golang.org) 1.23+
- [Claude Code](https://claude.com/claude-code) CLI installed and authenticated

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
```

These install to `~/go/bin/`. To make them available system-wide (including to non-interactive shells like those used by agent-validate), add the path via `/etc/paths.d/`:

```bash
echo "$HOME/go/bin" | sudo tee /etc/paths.d/go
```

Open a new terminal afterwards. Verify with:

```bash
/bin/sh -c 'which golangci-lint'
```

If you only add `~/go/bin` to `.zshrc`, it will work in your terminal but **not** in tools that spawn `/bin/sh` subprocesses.

## Make targets

### Build and quality

| Target | Command | Description |
|--------|---------|-------------|
| `make build` | `go build -o bin/agent-runner ./cmd/agent-runner` | Compile binary |
| `make test` | `go test ./...` | Run all tests |
| `make test-verbose` | `go test -v ./...` | Run tests with output |
| `make test-cover` | `go test -coverprofile=...` | Run tests with coverage report |
| `make lint` | `golangci-lint run ./...` | Run linter (strict config) |
| `make fmt` | `goimports -w .` | Format code |

### Running without building (`./dev.sh`)

`./dev.sh` is a thin wrapper around `go run` that passes all arguments through unchanged. Use it exactly like the compiled binary:

```bash
./dev.sh workflows/plan-change.yaml my-change
./dev.sh --validate workflows/plan-change.yaml
./dev.sh --resume --session plan-change-2026-04-03T23-19-18-552111Z
```

> **Why not `make`?** `make` intercepts `--flag` arguments as its own options, so flags like `--session` can't be passed through. `./dev.sh` avoids this.

## Linting

The project uses golangci-lint v2 with strict rules configured in `.golangci.yml`. Key settings:

- **gocognit**: max complexity 35
- **funlen**: max 100 lines / 60 statements
- **cyclop**: max cyclomatic complexity 35
- **gocritic**: diagnostic, style, and performance checks enabled
- **nolintlint**: all `//nolint` directives must have an explanation and be linter-specific

Test files and the PTY POC have relaxed rules (see `.golangci.yml` exclusions).

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

## PTY POC

The PTY proof of concept lives in `cmd/pty-poc/`. Run it with:

```bash
go run ./cmd/pty-poc
```

This launches a PTY session using `creack/pty` and is the foundation for future terminal UI work with the Charm stack (Bubble Tea).
