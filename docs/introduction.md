---
title: Introduction
group: Getting Started
order: 1
description: What Agent Runner is, when to use it, and how it fits around existing agent CLIs.
---

# Introduction

Agent Runner is a local workflow runner for coding agents. It runs multi-step workflows through the agent CLIs you already use while keeping orchestration outside the agent's context window.

## What Agent Runner Does

Agent Runner lets you define workflows such as plan, implement, validate, fix, review, and open a PR. The agent still reasons, writes code, and fixes problems, while Agent Runner owns sequencing, retries, state, validation loops, and resumption.

That means "run the validator and fix failures until it passes" becomes a workflow, not a long prompt the model has to remember.

## Why

Coding agents are good at execution. They are less reliable at orchestration.

There is a big difference between asking an agent to run a workflow and having a workflow that runs the agent.

You can write the clearest instruction in the world: implement the task, run tests and linters, review the diff, fix failures, re-review, open the PR, wait for CI, address review comments. The agent may do most of it. But "most" is not a reliable engineering workflow.

Agent Runner flips that around. Instead of the agent calling your workflow code when it happens to remember, Agent Runner tells the agent what to do next. It is a deterministic workflow layer outside the agent's context window.

So you don't have to remember what comes next, or babysit the workflow to make sure each validation loop actually runs. Agent Runner does that for you. It runs those steps automatically and *autonomously*, in sequence.

## When To Use It

Use Agent Runner when:

| Use case | Why it helps |
| --- | --- |
| Validation and fix loops must run every time | Agent Runner enforces the loop instead of relying on the agent to remember it. |
| You use more than one agent CLI | Workflows are not locked to one vendor. |
| You combine planning, implementation, shell commands, tests, reviews, and PR steps | Each activity can be a distinct workflow step. |
| Long-running work needs to be inspectable and resumable | Run state lives outside the chat transcript. |
| Repeatable workflows currently live in prompts, docs, or muscle memory | The workflow becomes executable YAML. |

## What It Is Not

Agent Runner is not another agent framework, chat UI, or replacement for Claude Code, Codex, Copilot, Cursor, or OpenCode. It does not try to be the brain.

It is the workflow layer around those tools. You keep using the agent CLIs you already have. Agent Runner decides which step runs next, what context the step receives, when to retry, what output to capture, and where the run state lives.

## Features

| Feature | Description |
| --- | --- |
| TUI-first run management | Browse workflow definitions, start runs, inspect run history, and resume interrupted runs. |
| Multiple agent CLIs | Built-in adapters for `claude`, `codex`, `copilot`, `cursor`, and `opencode`. |
| Existing CLI auth | Agent Runner launches installed CLI tools as subprocesses, so workflows can use existing subscriptions and local credentials. |
| Agent profiles | Configure named agents such as `planner`, `implementor`, and `summarizer` in global or project config. |
| Session control | Start fresh agent sessions, resume prior ones, or inherit context across sub-workflows. |
| Workflow control | Retry validation loops, skip fix steps when checks pass, stop loops on success, and fan out over files or tasks. |
| Output capture | Pass test results, validator reports, generated plans, or user input from one step into the next. |
| Step types | Compose agent, shell, script, UI, loop, group, and sub-workflow steps. |
| State, audit, and run views | Each run writes `state.json` and `audit.log` under `~/.agent-runner/projects/.../runs/<run-id>/`. |
| Built-in workflows | Namespaced workflows for OpenSpec, spec-driven planning, validation, implementation, onboarding, and PR finalization. |

## Existing Agent CLIs

Agent Runner launches installed CLI tools as subprocesses. If you already use Claude Code, Codex, Copilot, Cursor, or OpenCode, Agent Runner can run workflows through those tools using their existing authentication.

This lets workflows use subscription-backed CLIs and local filesystem/git context instead of requiring every agent step to go through a direct API integration.

## Status

Agent Runner is pre-release. It is ready for serious early users who are comfortable with CLI tools, YAML workflows, and rough edges.

The core value to evaluate is whether moving workflow control outside the agent makes coding-agent work more reliable. If setup, workflow authoring, or the mental model breaks for you, open an issue.
