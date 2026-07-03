---
title: Setup
group: Getting Started
order: 3
description: Set up Agent Runner in a Git project and run the first workflow.
---

# Setup

If you prefer guided setup instead of reading docs, run Agent Runner from a Git project:

```bash
cd /path/to/project
agent-runner
```

On first launch, Agent Runner opens native setup and then offers to run the onboarding workflow. If setup is already complete, run the onboarding workflow directly:

```bash
agent-runner run onboarding:onboarding
```

For a disposable tutorial project, you can reset onboarding and start over:

```bash
agent-runner -reset-onboarding
```

That reset clears onboarding settings, project `.validator/` state, and saved onboarding runs, so do not use it casually in a project with validator configuration you want to keep.

## Prerequisites

- Git
- At least one installed and authenticated agent CLI: `claude`, `codex`, `copilot`, `cursor`, or `opencode`
- `agent-plugin` available when installing helper CLIs outside Homebrew

Agent Runner workflows operate on real projects. The onboarding workflow refuses to run its guided implementation phase in an empty directory or a directory that is not inside a Git repository.

## Initialize Git Repo

Start from a real project directory that has already been initialized as a Git repository. Agent Runner's guided workflow operates on the current project, and its implementation and validation phases depend on Git status, diffs, stashes, and staged changes.

If the directory is empty or is not a Git repository yet, initialize the repository before continuing. The exact commands depend on the project and Git workflow, so use the normal setup path for that project.

## Install Agent Runner

Homebrew is the preferred install path on macOS and Linux:

```bash
brew tap Codagent-AI/tap
brew install --cask agent-runner
```

The Homebrew cask installs Agent Runner and its helper CLIs, including `agent-plugin` and `agent-validator`.

On Linux without Homebrew, download the latest release tarball for your architecture and install helper CLIs separately:

```bash
curl -LO https://github.com/Codagent-AI/agent-runner/releases/latest/download/agent-runner_linux_amd64.tar.gz
tar xzf agent-runner_linux_amd64.tar.gz
sudo mv agent-runner /usr/local/bin/
npm install -g agent-validator @codagent-ai/agent-plugin
```

## Set Up Agent Validator

Agent Validator is used by Agent Runner's built-in validation loops. Set it up before relying on Agent Runner for implementation work.

If you installed Agent Runner with Homebrew, the `agent-validator` CLI is already installed. If you used the Linux tarball path above, install it separately with the helper CLIs.

Follow the Agent Validator [Setup guide](https://github.com/Codagent-AI/agent-validator/blob/main/docs/setup.md), or run the core setup commands:

```bash
agent-validate init
```

Then open your coding agent in the same project and run:

```text
/validator-setup
```

Check the resulting configuration:

```bash
agent-validate list
agent-validate detect
```

Doing normal Agent Validator setup first gives that phase a sensible baseline and keeps the later validation demo focused on the feedback loop.

## Optional: Set Up OpenSpec

Skip this step unless you want to use the `openspec:*` built-in workflows. The `spec-driven:*` workflows do not require OpenSpec.

Install the `openspec` CLI, then initialize OpenSpec from the project root:

```bash
npm install -g @fission-ai/openspec@latest
openspec init --tools none .
mkdir -p openspec/changes openspec/specs
```

Agent Runner uses Codagent skills for agent behavior, so `--tools none` avoids installing OpenSpec agent skills or slash commands. The directory creation line makes sure the folders expected by Agent Runner's OpenSpec workflows exist even in a new project.

After this setup, use `openspec:*` workflows when you want OpenSpec change artifacts and `spec-driven:*` workflows when you want a lighter-weight flow.

## Launch Native Setup

Run Agent Runner from the project root:

```bash
agent-runner
```

On first launch, native setup walks through:

1. Choosing the planner CLI and model for interactive workflow steps.
2. Choosing the implementor CLI and model for autonomous implementation steps.
3. Choosing the autonomous backend and permission mode.
4. Choosing global or project config scope.
5. Installing Codagent agent skills through `agent-plugin`.
6. Choosing whether to run the onboarding workflow demo.

Global setup writes user-level Agent Runner config. Project setup writes `.agent-runner/config.yaml` in the current repository and installs project-scoped agent skills.

## Run Your First Workflow

Start with `spec-driven:simple-change`. It is the smallest built-in workflow for normal project work and does not require OpenSpec.

Run it from the project root:

```bash
agent-runner run spec-driven:simple-change
```

The workflow will ask what change you want to make, create a focused task, run an autonomous implementor, and then run Agent Validator through the validation loop.

Keep the first task small: fix a typo, add a log line, rename a variable, or make another scoped change. The workflow expects the project to start from a clean, validated state; if `agent-validator detect` reports unvalidated changes, validate or commit those changes before starting.

If you set up OpenSpec, use the OpenSpec variant for the same small-change flow:

```bash
agent-runner run openspec:simple-change my-change
```

Use a short kebab-case change name, such as `add-user-search` or `fix-login-copy`.

To browse other workflows:

```bash
agent-runner -list
```

Common next workflows are `spec-driven:simple-change`, `spec-driven:change`, and `openspec:change` if the project uses OpenSpec.

For workflow authoring, see [Writing Workflows](writing-workflows.md). For commands and flags, see [CLI Reference](cli-reference.md).
