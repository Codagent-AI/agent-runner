# Agent Runner

Agent Runner is a local workflow runner for coding agents.

It lets you define multi-step workflows - plan, implement, validate, fix, review, open a PR - and run those steps through the agent CLIs you already use: Claude Code, Codex, Copilot, Cursor, and OpenCode.

The key difference: the workflow lives outside the agent's context window. The agent still reasons, writes code, and fixes problems. Agent Runner owns the sequencing, retries, state, validation loops, and resumption, so "run the validator and fix failures until it passes" becomes a workflow, not a prompt the model might forget.

## Quick start

```bash
brew tap Codagent-AI/tap
brew install --cask agent-runner
agent-runner
```

That's all you need to do. First launch opens the guided setup and tutorial.

## Why

Coding agents are good at execution. They are much less reliable at orchestration.

You can ask an agent to follow a long checklist, but the checklist still lives in the same context window as the implementation details, error logs, side quests, and model drift. It might skip a step. It might stop early and ask if you're "ready to proceed." It might run validation once, see failures, and decide the task is "mostly done." Classic tiny goblin behavior.

Agent Runner flips the control flow around. Instead of asking the agent to remember the workflow, Agent Runner runs the workflow and calls the agent for each step.

That gives you:

- deterministic sequencing
- resumable run state
- explicit validation and fix loops
- separate or resumed agent sessions per step
- shell, script, and UI steps alongside agent steps
- workflows that work across multiple agent CLIs

The agent handles judgment and code. Agent Runner handles order, state, and enforcement.

## Use Agent Runner when

- You want validation and fix loops to run every time, not only when the agent remembers.
- You use more than one agent CLI and want workflows that are not locked to one vendor.
- You want to combine interactive planning with autonomous implementation, shell commands, tests, review agents, and PR steps.
- You want long-running work to be inspectable and resumable instead of buried in a chat transcript.
- You already have repeatable agent workflows, but they currently live in prompts, docs, or muscle memory.

## What Agent Runner is not

Agent Runner is not another agent framework, chat UI, or replacement for Claude Code, Codex, Copilot, Cursor, or OpenCode. It does not try to be the brain.

It is the workflow layer around those tools. You keep using the agent CLIs you already have. Agent Runner decides which step runs next, what context the step receives, when to retry, what output to capture, and where the run state lives.

## Features

- **TUI-first run management**: browse workflow definitions, start runs, inspect run history, and resume interrupted runs.
- **Multiple agent CLIs**: built-in adapters for `claude`, `codex`, `copilot`, `cursor`, and `opencode`.
- **Existing CLI auth**: Agent Runner launches installed CLI tools as subprocesses, so workflows can use your existing subscriptions and local credentials instead of requiring direct API integration.
- **Agent profiles**: configure named agents such as `planner`, `implementor`, and `summarizer` in global or project config.
- **Session control**: start fresh agent sessions, resume prior ones, or inherit context across sub-workflows so validation fixes can happen in the same context that wrote the code.
- **Real workflow control**: retry validation loops, skip fix steps when checks pass, stop loops on success, and fan out over files or tasks.
- **Output capture**: pass test results, validator reports, generated plans, or user input from one step into the next.
- **Step types**: agent, shell, script, UI, loop, group, and sub-workflow steps.
- **State, audit, and run views**: each run writes `state.json` and `audit.log` under `~/.agent-runner/projects/.../runs/<run-id>/`.
- **Built-in workflows**: namespaced workflows for OpenSpec, spec-driven planning, validation, implementation, onboarding, and PR finalization.

## Example: validate, fix, retry

This workflow runs Agent Validator, captures its report, asks the agent to fix failures, and retries up to three times.

```yaml
name: validate-and-fix
description: "Run validation, ask the agent to fix failures, retry up to 3 times"

steps:
  - id: validator-retry
    loop:
      max: 3
    steps:
      - id: run-validator
        command: agent-validator run --report
        capture: validator_output
        capture_stderr: true
        continue_on_failure: true
        break_if: success

      - id: fix-violations
        session: resume
        mode: autonomous
        prompt: |
          The validator found failures. Fix the issues you reasonably agree with.

          <validator-output>
          {{validator_output}}
          </validator-output>
        skip_if: previous_success
        continue_on_failure: true
```

Agent Runner enforces the loop. The agent only sees the focused task for the current step.

## Workflow discovery

Agent Runner resolves workflow names from:

1. Project workflows: `.agent-runner/workflows/<name>.yaml`
2. User workflows: `~/.agent-runner/workflows/<name>.yaml`
3. Built-ins: `<namespace>:<name>`, such as `openspec:plan-change`, `spec-driven:simple-change`, or `core:run-validator`

Project workflows shadow user workflows with the same name. Built-ins are embedded into the binary from [workflows](workflows).

## Works with your existing agent CLIs

Agent Runner launches installed CLI tools as subprocesses. If you already use Claude Code, Codex, Copilot, Cursor, or OpenCode, Agent Runner can run workflows through those tools using their existing authentication.

That means workflows can use subscription-backed CLIs and local filesystem/git context instead of requiring every agent step to go through a direct API integration.

Agent profiles are loaded from built-in defaults, then `~/.agent-runner/config.yaml`, then `.agent-runner/config.yaml`. Project config wins over global config. The default profile set includes:

- `planner`: interactive Claude profile
- `implementor`: autonomous Claude profile
- `summarizer`: lightweight autonomous Claude profile

User settings live in `~/.agent-runner/settings.yaml` and include TUI theme, autonomous backend behavior, and autonomous permission mode.

## Documentation

- [User Guide](docs/USER-GUIDE.md) covers workflow authoring, sessions, loops, sub-workflows, engines, audit logging, and troubleshooting.
- [Development Guide](docs/development.md) covers local setup, build/test/lint commands, and validation.

## Status

Agent Runner is pre-release. It is ready for serious early users who are comfortable with CLI tools, YAML workflows, and rough edges.

The core value I want feedback on: does moving workflow control outside the agent make your coding-agent work more reliable? If setup, workflow authoring, or the mental model breaks for you, please open an issue.

## License

MIT
