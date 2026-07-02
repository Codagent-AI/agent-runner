# Agent Runner

Agent Runner is a local workflow runner for coding agents.

It lets you define multi-step workflows - plan, implement, validate, fix, review, open a PR - and run those steps through the agent CLIs you already use: Claude Code, Codex, Copilot, Cursor, and OpenCode.

The key difference: the workflow lives outside the agent's context window. The agent still reasons, writes code, and fixes problems. Agent Runner owns the sequencing, retries, state, validation loops, and resumption.

https://github.com/user-attachments/assets/e40c22f5-87dc-4158-9cc0-6bf9f6ede0ba

## Quick start

**macOS and Linux (Homebrew)**

```bash
brew tap Codagent-AI/tap
brew install --cask agent-runner
agent-runner
```

First launch opens the guided setup and tutorial. The Homebrew install also installs Agent Runner's helper CLIs, including `agent-plugin` and `agent-validator`.

**Linux tarball fallback**

If you are not using Homebrew, download the latest Linux tarball for your architecture from [releases](https://github.com/Codagent-AI/agent-runner/releases):

```bash
curl -LO https://github.com/Codagent-AI/agent-runner/releases/latest/download/agent-runner_linux_amd64.tar.gz
tar xzf agent-runner_linux_amd64.tar.gz
sudo mv agent-runner /usr/local/bin/
```

Tarball installs do not install helper CLIs. Install them separately before onboarding:

```bash
npm install -g agent-validator @codagent-ai/agent-plugin
```

## Why

Coding agents are good at execution. They are less reliable at orchestration.

Agent Runner flips the control flow around. Instead of asking the agent to remember the workflow, Agent Runner runs the workflow and calls the agent for each step. The agent handles judgment and code. Agent Runner handles order, state, and enforcement.

## Features

- **TUI-first run management**: browse workflow definitions, start runs, inspect run history, and resume interrupted runs.
- **Multiple agent CLIs**: built-in adapters for `claude`, `codex`, `copilot`, `cursor`, and `opencode`.
- **Existing CLI auth**: Agent Runner launches installed CLI tools as subprocesses, so workflows can use your existing subscriptions and local credentials.
- **Agent profiles**: configure named agents such as `planner`, `implementor`, and `summarizer`.
- **Session control**: start fresh agent sessions, resume prior ones, or inherit context across sub-workflows.
- **Workflow control**: retry validation loops, skip fix steps when checks pass, stop loops on success, and fan out over files or tasks.
- **Output capture**: pass test results, validator reports, generated plans, or user input from one step into the next.
- **Step types**: agent, shell, script, UI, loop, group, and sub-workflow steps.
- **State, audit, and run views**: each run writes `state.json` and `audit.log` under `~/.agent-runner/projects/.../runs/<run-id>/`.
- **Built-in workflows**: namespaced workflows for OpenSpec, spec-driven planning, validation, implementation, onboarding, and PR finalization.

## Works with your existing agent CLIs

Agent Runner launches installed CLI tools as subprocesses. If you already use Claude Code, Codex, Copilot, Cursor, or OpenCode, Agent Runner can run workflows through those tools using their existing authentication.

That means workflows can use subscription-backed CLIs and local filesystem/git context instead of requiring every agent step to go through a direct API integration.

## Documentation

The Markdown docs in [`docs/`](docs/) are the source of truth for the documentation site.

- [Introduction](docs/introduction.md)
- [Quickstart](docs/quickstart.md)
- [Writing Workflows](docs/writing-workflows.md)
- [Agent Profiles](docs/agent-profiles.md)
- [Sessions And Modes](docs/sessions-and-modes.md)
- [CLI Reference](docs/cli-reference.md)
- [Built-In Workflows](docs/built-in-workflows.md)
- [Run State And Audit](docs/run-state-and-audit.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Development Guide](docs/development.md)

## Status

Agent Runner is pre-release. It is ready for serious early users who are comfortable with CLI tools, YAML workflows, and rough edges.

The core value I want feedback on: does moving workflow control outside the agent make your coding-agent work more reliable? If setup, workflow authoring, or the mental model breaks for you, please open an issue.

## License

MIT
