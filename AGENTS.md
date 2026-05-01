# Agent Runner

Agent Runner is a Go CLI workflow orchestrator for AI agents. It runs multi-step workflows by spawning separate agent sessions per step, keeping orchestration deterministic and outside the agent.

This project is pre-release and under active development. Prefer clear, well-tested changes over preserving accidental compatibility.

## Project Map

- `cmd/agent-runner/`: CLI entry point and command wiring
- `internal/model/`: core workflow, step, state, and execution-context types
- `internal/loader/`: workflow YAML loading, composition, and interpolation setup
- `internal/runner/`: workflow execution and resume loop
- `internal/exec/`: step executors for agent, shell, loop, group, dispatch, and sub-workflow steps
- `internal/engine/`: pluggable workflow lifecycle hooks, including the OpenSpec engine
- `internal/cli/`: adapters for agent CLIs such as Claude, Codex, Cursor, and Copilot
- `internal/session/`: session resolution for `new`, `resume`, and `inherit`
- `internal/flowctl/`: `skip_if` and `break_if` behavior
- `internal/textfmt/`: interpolation and output formatting helpers
- `internal/runview/`, `internal/listview/`, `internal/liverun/`, `internal/pty/`, `internal/tuistyle/`: terminal UI and PTY behavior
- `internal/config/`, `internal/discovery/`, `internal/stateio/`, `internal/audit/`, `internal/validate/`: supporting runtime services
- `workflows/`: built-in workflow YAML files embedded into the binary
- `openspec/specs/`: current behavioral specs; archived changes under `openspec/changes/archive/` are historical
- `testdata/`: workflow fixtures used by tests
- `docs/`: user and development documentation

## Development Workflow

- Use test-driven development for substantive behavior changes. For bug fixes, add a failing test that reproduces the bug before changing production code.
- Tests are not required for trivial changes such as styling-only tweaks, copy edits, comments, or other cases where a focused unit test would not exercise meaningful behavior.
- Keep tests next to the source package. Use local stubs and small fakes instead of adding a mocking framework.
- Prefer `google/go-cmp` for structured comparisons in tests.
- Run targeted tests while iterating, then broader checks before finishing.
- Format Go code with `goimports` through `make fmt`.
- Use the Go version declared in `go.mod`.

## Common Commands

```bash
make build          # go build -o bin/agent-runner ./cmd/agent-runner
make test           # go test ./...
make lint           # golangci-lint run ./...
make fmt            # goimports -w .
./dev.sh <args>     # run the CLI from source; passes flags through safely
```

For focused test runs:

```bash
go test ./internal/runner -run TestName
go test ./internal/exec -run TestName -v
```

## Architecture Notes

- Keep all step executor implementations in `internal/exec/` to avoid circular imports.
- Keep model types independent from engine and executor packages. Higher layers can adapt or type-assert as needed.
- Built-in workflows are embedded from `workflows/`; changing YAML there changes what ships in the binary.
- `Param.Required` is a `*bool`; `nil` means required by default.
- Audit logging uses the real `audit.EventLogger` interface. Do not replace it with empty interfaces.

## Commit Messages

Use `type: lowercase description`.

Allowed types: `fix`, `feat`, `chore`, `refactor`, `test`, `docs`.

Do not use scopes, ticket prefixes, or capitalized descriptions.

Workflow-driven commits (created by a workflow step) may prepend `[<step_id>]` before `type: description` for traceability; this is the only allowed prefix.

Examples:

- `fix: stop writing config to project dir`
- `feat: add param-form run launch flow`
- `refactor: extract session resolution into helper`
- `[archive] chore: archive openspec documents for add-foo`
