# Agent Runner

CLI workflow orchestrator for AI agents, written in Go. Runs multi-step workflows by spawning separate agent sessions per step.

> **Pre-release**: This project is under active development. No backwards compatibility guarantees are made at this time.

## Directory structure

```
cmd/
  agent-runner/       CLI entry point (Cobra)
  pty-poc/            PTY proof of concept (creack/pty)
internal/
  model/              Core types: Step, Workflow, ExecutionContext, RunState
  loader/             YAML loading, parameter interpolation
  runner/             Workflow execution loop, resume logic
  exec/               Step executors: agent, shell, loop, sub-workflow, dispatch
  engine/             Engine interface, registry, openspec implementation
  session/            Session resolution (new, resume, inherit)
  flowctl/            Flow control: skip_if, break_if
  textfmt/            String interpolation, formatting
  stateio/            State file read/write
  audit/              Structured audit logging
  validate/           Workflow constraint validation
openspec/             OpenSpec artifact files
workflows/            Workflow YAML definitions
testdata/             Test fixture files
docs/                 User guide, design docs
```

## Key patterns

- **Interfaces for testability**: `exec.ProcessRunner`, `exec.GlobExpander`, `exec.Logger` — tests use stubs, no mocking framework
- **All executors in one package**: `internal/exec/` holds agent, shell, loop, sub-workflow executors together to avoid circular imports
- **`Param.Required` uses `*bool`**: nil defaults to required (matching original TS behavior where `Required` defaults to `true`)
- **`LastSessionStepID`**: Solves Go map ordering — tracks the most recent session key since Go maps are unordered
- **`EngineRef` is `interface{}`**: Avoids circular import between model and engine packages; callers type-assert to `engine.Engine`
- **`audit.EventLogger`**: Real interface (not empty) used for audit event emission

## Commands

```
make build          # compile binary
make test           # run tests
make lint           # golangci-lint (strict)
make fmt            # goimports
go run ./cmd/agent-runner [run|validate|resume]
```
