# Agent Runner

CLI workflow orchestrator for AI agents written in Go. Runs multi-step workflows by spawning separate agent sessions for each step, keeping orchestration deterministic and outside the agent.

## Why

Agents are good at execution, bad at orchestration. When given a complex multi-step workflow, they lose track of sequence, skip steps, accumulate stale context, and ignore instructions buried deep in prompts. Agent Runner solves this by moving orchestration out of the agent entirely. Each step gets a fresh or resumed session, a focused prompt in the highest-attention position, and a single responsibility.

## Why not use an existing workflow tool?

There are many YAML-based workflow engines (Argo, Kestra, Step Functions) and CLI task runners (Taskfile, Just, Make). The cloud/server orchestrators have rich control flow but can't run local CLI processes. The CLI task runners can run shell commands but collapse into bash scripts the moment you need loop-until with multi-step bodies, mid-pipeline output capture, or conditional branching. None of them have the concepts that agent orchestration requires: session management across steps, interactive/headless mode switching, prompt-based agent steps, or signal-based advancement. Agent Runner borrows proven workflow primitives (for-each, loop-until, sub-workflows, output capture) from these systems and adds a purpose-built runtime for orchestrating stateful conversational agents. See [docs/WHY-AGENT-RUNNER.md](docs/WHY-AGENT-RUNNER.md) for the full comparison.

## Features

- **Three step modes**: interactive (collaborative), headless (autonomous), shell (CLI commands)
- **Session management**: `new`, `resume`, or `inherit` sessions across steps and sub-workflows
- **Loops**: counted loops (`loop: { max: N }`) and for-each loops (`loop: { over, as }`) with `break_if` conditions
- **Sub-workflows**: compose workflows from reusable workflow files with parameter passing
- **Output capture**: capture shell stdout into variables for use in subsequent steps (`capture` field with tee behavior)
- **Flow control**: `continue_on_failure`, `skip_if: previous_success`, `break_if: success|failure`
- **Per-step model override**: specify which model an agent step should use (`model` field)
- **State and resumption**: `agent-runner-state.json` persists after each step for resume on interruption
- **Audit logging**: structured log of every execution event (step start/end, iterations, sub-workflows) for post-failure troubleshooting
- **Engines**: pluggable lifecycle hooks for prompt enrichment, step validation, and state management
- **PTY support**: improved terminal I/O for interactive agent sessions (via Go's creack/pty)

## Install

Requires [Go](https://golang.org) 1.23+.

```bash
go build ./cmd/agent-runner
./agent-runner validate workflows/flokay.yaml
```

Or use the Makefile:

```bash
make build       # compiles to agent-runner binary
make test        # run tests
make lint        # run golangci-lint
```

## Quick start

```bash
# Validate a workflow
./agent-runner validate workflows/flokay.yaml

# Run a workflow with parameters
./agent-runner run workflows/flokay.yaml my-change-name

# Start from a specific step
./agent-runner run workflows/flokay.yaml my-change-name --from design

# Resume an existing Claude session into a new workflow
./agent-runner run workflows/plan-change.yaml my-change --session <session-id>

# Resume an interrupted workflow
./agent-runner resume path/to/agent-runner-state.json
```

## How it works

Agent Runner reads a YAML workflow file and executes steps sequentially. Each step is one of several types:

| Type | What happens | Use case |
|------|-------------|----------|
| **interactive** | Agent runs with full stdin. User works with it, types `/continue` to advance. | Collaborative steps (proposal, specs, design) |
| **headless** | Agent runs with `-p` flag. Output streams to terminal. Auto-advances on exit. | Autonomous steps (tasks, review, implementation) |
| **shell** | Runs a shell command directly, no agent. | CLI operations (`openspec new`, `git commit`) |
| **loop** | Repeats child steps (counted or for-each). | Iterating over tasks, retry loops |
| **sub-workflow** | Invokes another workflow file. | Reusable workflow composition |

```
agent-runner (harness)
  |
  +-- step 1: shell        -> sh -c "openspec new change my-feature"
  +-- step 2: interactive  -> claude "Write the proposal..."
  +-- step 3: headless     -> claude -p "Generate specs..."
  +-- step 4: loop (per-task)
  |     +-- step 4a: headless  -> claude -p "Implement {{task_file}}"
  |     +-- step 4b: sub-workflow -> workflows/run-gauntlet.yaml
  +-- step 5: headless     -> claude -p "Finalize..."
```

### Session management

Each agent step declares a session strategy:

- **`session: new`** -- Fresh session, no prior context. Agent reads what it needs from disk.
- **`session: resume`** -- Continues the most recent session within the current workflow. Also picks up a session seeded via `--session`.
- **`session: inherit`** -- Crosses sub-workflow boundaries to resume the parent workflow's most recent session.

### State and resumption

Agent Runner writes `agent-runner-state.json` after each step. If a workflow is interrupted, `agent-runner resume` picks up from where it left off, including persisted session IDs, captured variables, and parameters. State is recursive -- nested loops and sub-workflows track their own position.

### Engines

Workflows can declare an **engine** that hooks into the execution lifecycle:

- **`enrichPrompt`** -- Append context (templates, output paths, dependencies) to step prompts
- **`validateStep`** -- Verify expected output was created after a step
- **`validateWorkflow`** -- Check workflow structure at load time
- **`getStateDir`** -- Control where the state file lives

The built-in `openspec` engine integrates with the [OpenSpec](https://github.com/pacaplan/openspec) CLI to inject artifact context and validate artifact completion.

## Workflow format

```yaml
name: my-workflow
description: "What this workflow does"
agent: claude-code

params:
  - name: change_name
    required: true

engine:                          # optional
  type: openspec
  change_param: change_name

steps:
  - id: create
    mode: shell
    command: openspec new change "{{change_name}}"

  - id: proposal
    mode: interactive
    session: new
    prompt: /flokay:propose "{{change_name}}"

  - id: implement
    workflow: implement-change.yaml
    params:
      change_name: "{{change_name}}"

  - id: verify
    mode: headless
    session: new
    model: sonnet
    prompt: "Verify the implementation"
```

### Step fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Unique step identifier. Used for `--from`, state tracking, and engine matching. |
| `mode` | agent/shell | `interactive`, `headless`, or `shell` |
| `prompt` | agent steps | Prompt passed to the agent. Supports `{{param}}` interpolation. |
| `command` | shell steps | Shell command to execute. Supports `{{param}}` interpolation. |
| `session` | no | `new` (default), `resume`, or `inherit`. Only applies to agent steps. |
| `model` | no | Model override for agent steps. Passed as `--model <value>` to claude. |
| `capture` | no | Variable name to capture shell stdout into. Shell steps only. |
| `continue_on_failure` | no | If `true`, workflow continues even if this step fails. |
| `skip_if` | no | `previous_success` -- skip this step if the prior step succeeded. |
| `break_if` | no | `success` or `failure` -- break out of enclosing loop on this condition. |
| `loop` | no | `{ max: N }` for counted loops, `{ over: glob, as: var }` for for-each. |
| `steps` | loop/group | Nested child steps (required for loops, optional for groups). |
| `workflow` | sub-workflow | Path to another workflow YAML file. |
| `params` | sub-workflow | Parameters to pass to the sub-workflow. |

### Parameter interpolation

Parameters declared in `params:` are passed as positional arguments:

```bash
./agent-runner run workflow.yaml value1 value2
```

Referenced in prompts and commands as `{{param_name}}`. Captured variables from shell steps are also available via `{{var_name}}`.

## CLI reference

```
agent-runner run <workflow.yaml> [params...] [--from <step>] [--session <id>]
agent-runner validate <workflow.yaml> [params...]
agent-runner resume <state-file-path>
```

### --session

Seeds the workflow with an existing Claude session ID. The first step that uses `session: resume` will continue that conversation instead of starting fresh. Steps using `session: new` are unaffected.

This is useful when you've been discussing an idea with Claude and want to transition into a structured workflow -- Claude keeps the conversational context from your discussion.

```bash
# You've been chatting with Claude about a feature idea...
# Now formalize it into a change, with Claude retaining the context:
./agent-runner run workflows/plan-change.yaml my-feature --session abc-123-def
```

The seed propagates through sub-workflows and loop iterations, so it works regardless of nesting depth. If no step uses `session: resume`, the seeded session is ignored.

## Configuration

Configuration resolves in layers, each overriding the previous:

1. **Global (user-level)** — `default_agent`, `default_model`
2. **Project-level** — project-specific defaults
3. **Workflow-level** — `agent` field in the workflow YAML
4. **Step-level** — `model` field on individual steps

This means a workflow can declare `agent: claude-code` at the top and override with `model: sonnet` on a single review step.

## Planned: Workflow extensibility

Users will be able to extend base workflows without redefining the entire pipeline:

- **Extend** a base workflow and inherit its steps
- **Override** specific steps (agent, prompt, mode) while keeping the rest
- **Add** new steps at specific positions

Design goal: modify behavior without rewriting entire workflows.

## Architecture

```
.
├── cmd/
│   ├── agent-runner/
│   │   ├── main.go            # CLI entry (Cobra)
│   │   └── helpers.go         # process runner, glob expander
│   └── pty-poc/               # PTY proof of concept
│       ├── main.go
│       └── terminal.go
│
├── internal/
│   ├── model/
│   │   ├── step.go            # Step, Loop, Param, Workflow structs
│   │   ├── context.go         # ExecutionContext, nesting
│   │   └── state.go           # RunState serialization
│   │
│   ├── loader/
│   │   └── loader.go          # YAML loading, param interpolation
│   │
│   ├── runner/
│   │   ├── runner.go          # Workflow execution loop
│   │   └── resume.go          # State restoration
│   │
│   ├── exec/                  # Step executors
│   │   ├── agent.go           # Agent step executor
│   │   ├── shell.go           # Shell step executor
│   │   ├── loop.go            # Loop executor
│   │   ├── subworkflow.go     # Sub-workflow executor
│   │   ├── dispatch.go        # Step type routing
│   │   └── interfaces.go      # ProcessRunner, GlobExpander, Logger
│   │
│   ├── engine/
│   │   ├── engine.go          # Engine interface & registry
│   │   └── openspec/
│   │       └── openspec.go    # OpenSpec engine implementation
│   │
│   ├── session/
│   │   └── session.go         # Session resolution (new, resume, inherit)
│   │
│   ├── flowctl/
│   │   └── flowctl.go         # skip_if, break_if evaluation
│   │
│   ├── textfmt/
│   │   ├── interpolation.go   # {{variable}} interpolation
│   │   └── format.go          # Formatting utilities
│   │
│   ├── stateio/
│   │   └── stateio.go         # State file read/write
│   │
│   ├── audit/
│   │   ├── types.go           # Event types
│   │   └── logger.go          # AuditLogger
│   │
│   └── validate/
│       └── workflow.go        # Workflow constraint validation
│
├── go.mod
├── go.sum
└── Makefile
```

## Development

```bash
make build        # compile to agent-runner binary
make test         # run all tests
make test-verbose # run tests with output
make test-cover   # run tests with coverage
make lint         # run golangci-lint
make fmt          # format code (goimports)
```

## License

MIT
