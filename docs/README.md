---
title: Documentation
group: Getting Started
order: 0
description: Index of Agent Runner documentation pages.
---

# Documentation

Agent Runner documentation is split into focused pages for GitHub and the docs site. Read the pages in sidebar order if you're new, or jump directly to the reference you need.

## Getting Started

| Page | Description |
| --- | --- |
| [Introduction](introduction.md) | What Agent Runner is, when to use it, and how it fits around agent CLIs. |
| [Quickstart](quickstart.md) | Install Agent Runner, launch the TUI, and run common commands. |
| [Setup](setup.md) | Set up Agent Runner in a Git project and run the onboarding workflow. |

## Guides

| Page | Description |
| --- | --- |
| [Writing Workflows](writing-workflows.md) | YAML workflow authoring, step types, parameters, variables, loops, sub-workflows, flow control, capture, and interpolation. |
| [Agent Profiles](agent-profiles.md) | Named profiles that set an agent step's mode, CLI, model, and effort. |
| [Sessions And Modes](sessions-and-modes.md) | Session strategies and the `interactive`, `autonomous`, and `ui` mode model. |
| [Agent Calls](agent-calls.md) | Synchronous in-turn delegation to Agent Runner profiles and named sessions. |
| [Direct Terminal Handoff](direct-terminal-handoff.md) | How interactive agent and shell steps share the real terminal, including completion, durability, and job control. |

## Usage

| Page | Description |
| --- | --- |
| [Usage And Cost Tracking](usage-and-cost-tracking.md) | Token categories, CLI support, reported cost, coverage, and the `run-metrics.json` artifact. |

## Reference

| Page | Description |
| --- | --- |
| [CLI Reference](cli-reference.md) | Commands and flags generated from current `agent-runner --help` output. |
| [Built-In Workflows](built-in-workflows.md) | Embedded workflow namespaces and workflow purposes. |
| [Run State And Audit](run-state-and-audit.md) | Run storage, state files, audit logs, output files, and run views. |
| [Troubleshooting](troubleshooting.md) | Common failures and recovery commands. |

## Development

| Page | Description |
| --- | --- |
| [Development Guide](development.md) | Build, test, lint, and validate Agent Runner from source. |
| [Development Sandbox](dev/sandbox.md) | Run local source and external payloads in the Docker sandbox. |
