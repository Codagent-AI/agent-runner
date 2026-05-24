# Agent Runner

Agent Runner is a Go CLI workflow orchestrator for AI agents. It runs multi-step workflows by spawning separate agent sessions per step, keeping sequencing, state, retries, and resumption outside the agent.

## Why

Agents are good at execution but unreliable at orchestration. Long multi-step prompts drift, skip steps, accumulate stale context, and bury important instructions. Agent Runner keeps the workflow deterministic: each step gets a focused prompt, an explicit session strategy, and persisted state.

## Features

- **TUI-first run management**: browse workflow definitions, start runs, inspect run history, and resume interrupted runs.
- **Multiple agent CLIs**: built-in adapters for `claude`, `codex`, `copilot`, `cursor`, and `opencode`.
- **Agent profiles**: configure named agents such as `planner`, `implementor`, and `summarizer` in global or project config.
- **Session strategies**: use `new`, `resume`, `inherit`, or named sessions declared in workflow YAML.
- **Step types**: agent, shell, script, UI, loop, group, and sub-workflow steps.
- **Loops and flow control**: counted loops, for-each loops, `continue_on_failure`, `skip_if`, and `break_if`.
- **Capture and interpolation**: capture shell, script, UI, or autonomous agent output into typed variables for later steps.
- **State, audit, and run views**: each run writes `state.json` and `audit.log` under `~/.agent-runner/projects/.../runs/<run-id>/`.
- **Built-in workflows**: namespaced workflows for OpenSpec, spec-driven planning, validation, implementation, and PR finalization.

## Install

### Homebrew

```bash
brew tap Codagent-AI/tap
brew install --cask agent-runner
```

### From Source

Use the Go version declared in [go.mod](go.mod).

```bash
make build       # compiles to bin/agent-runner
make test        # run tests
make lint        # run golangci-lint
```

## Quick Start

```bash
# Browse workflow definitions and runs
agent-runner

# List runs for the current directory
agent-runner -list

# Validate a workflow; validation params use key=value syntax
agent-runner -validate openspec:plan-change change_name=my-change

# Run a built-in workflow with positional params
agent-runner openspec:plan-change my-change

# Run a built-in workflow with key=value params
agent-runner openspec:plan-change change_name=my-change

# Resume from the run list
agent-runner -resume

# Resume or inspect a specific run ID from the current project
agent-runner -resume <run-id>
agent-runner -inspect <run-id>
```

## Documentation

- [User Guide](docs/USER-GUIDE.md) covers workflow authoring, sessions, loops, sub-workflows, engines, audit logging, and troubleshooting.
- [Development Guide](docs/development.md) covers local setup, build/test/lint commands, and validation.

## Workflow Discovery

Agent Runner resolves workflow names from:

1. Project workflows: `.agent-runner/workflows/<name>.yaml`
2. User workflows: `~/.agent-runner/workflows/<name>.yaml`
3. Built-ins: `<namespace>:<name>`, such as `openspec:plan-change` or `core:run-validator`

Project workflows shadow user workflows with the same name. Built-ins are embedded into the binary from [workflows](workflows).

## Workflow Format

```yaml
name: review-change
description: "Review a change and run validation"

params:
  - name: change_name
    required: true

sessions:
  - name: lead-agent
    agent: planner

steps:
  - id: review
    session: lead-agent
    prompt: "Review the change {{change_name}}."

  - id: validate
    command: agent-validator run --report
    capture: validator_output
    capture_stderr: true
    continue_on_failure: true

  - id: fix
    session: resume
    mode: autonomous
    prompt: |
      Fix the validator failures:
      {{validator_output}}
    skip_if: previous_success
```

### Common Step Fields

| Field | Applies to | Description |
| --- | --- | --- |
| `id` | all steps | Unique step identifier used for state, audit logs, and resume. |
| `prompt` | agent | Prompt sent to the agent. |
| `agent` | agent | Agent profile name for `session: new`; required for new agent sessions. |
| `session` | agent | `new`, `resume`, `inherit`, or a declared named session. |
| `mode` | agent, shell, UI | `interactive`, `autonomous`, or `ui`. Shell steps may use `mode: interactive`. |
| `cli` | agent | Step-level CLI adapter override. |
| `model` | agent | Step-level model override. |
| `command` | shell | Shell command executed through `/bin/sh`. |
| `script` | script | Static path to a bundled or workflow-local script. |
| `workflow` | sub-workflow | Relative workflow path or built-in reference. |
| `params` | sub-workflow | Parameters passed to the child workflow. |
| `loop` | loop | `{ max: N }` or `{ over: glob, as: name }`. |
| `steps` | loop, group | Child steps. |
| `capture` | shell, script, UI, autonomous agent | Variable name for captured output. |
| `capture_stderr` | shell | Append stderr to captured output when the shell command fails. |
| `capture_format` | script | `text` or `json`; JSON captures lists or maps of strings. |
| `workdir` | shell, script, agent | Working directory for the subprocess. |
| `continue_on_failure` | all steps | Continue after a failed step. |
| `skip_if` | all steps | `previous_success` or `sh: <command>`. |
| `break_if` | loop body steps | `success` or `failure`. |

## Configuration

Agent profiles are loaded from built-in defaults, then `~/.agent-runner/config.yaml`, then `.agent-runner/config.yaml`. Project config wins over global config. The default profile set includes:

- `planner`: interactive Claude profile
- `implementor`: autonomous Claude profile
- `summarizer`: lightweight autonomous Claude profile

User settings live in `~/.agent-runner/settings.yaml` and include TUI theme, autonomous backend behavior, and autonomous permission mode.

## Development

```bash
make build        # compile to bin/agent-runner
make test         # run all tests
make test-verbose # run tests with output
make test-cover   # run tests with coverage
make lint         # run golangci-lint
make fmt          # format code with goimports
```

## License

MIT
