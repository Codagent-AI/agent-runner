---
title: Sessions And Modes
group: Guides
order: 5
description: Session strategies and the interactive, autonomous, and UI mode model.
---

# Sessions And Modes

Agent Runner keeps workflow orchestration outside the agent, but it can still preserve or isolate agent context between steps. Session strategies control continuity, while modes control how each step runs.

## Sessions

Agent steps support these session strategies:

| Session | Meaning |
| --- | --- |
| `new` | Start a fresh session using the step's `agent` profile. |
| `resume` | Resume the most recent session in the current workflow context. |
| `inherit` | In a sub-workflow, resume the parent workflow's most recent session. |
| named session | Resume or create a declared session such as `lead-agent`. |

The first agent step defaults to `session: new`, so it must specify an `agent` profile. Later agent steps default to `session: resume` and continue the most recent session unless you set a different session.

## Named Sessions

Named sessions are declared at the workflow top level:

```yaml
sessions:
  - name: lead-agent
    agent: planner
  - name: reviewer-agent
    agent: planner

steps:
  - id: draft
    session: lead-agent
    prompt: "Draft the proposal."

  - id: review
    session: reviewer-agent
    mode: autonomous
    prompt: "Review the proposal."
```

> [!WARNING]
> Named session names cannot be `new`, `resume`, or `inherit`. Those words are reserved for built-in session strategies.

## Modes

Agent Runner uses modes to choose the runtime behavior for a step:

| Mode | Applies to | Meaning |
| --- | --- | --- |
| `interactive` | Agent and shell steps | Run in a PTY with user interaction. |
| `autonomous` | Agent steps | Run without user interaction, using either a headless backend or an interactive backend with autonomy instructions. |
| `ui` | UI steps | Render an Agent Runner UI prompt inside the live run TUI. |

## Interactive Agent Steps

Interactive agent steps hand the real terminal directly to the agent CLI. The workflow advances when the current step sends an authenticated completion event:

| Method | Meaning |
| --- | --- |
| Ask the agent to continue | The agent follows its injected instruction and runs the absolute-path completion client. |
| Native completion command | Type `/agent-runner:next` in Claude, Copilot, or Cursor. In Codex, invoke `$agent-runner-next`. |
| Injected completion instruction | The agent runs `agent-runner step complete` through the private control channel when its work is done. |

Agent Runner does not draw a continuation overlay or intercept a global keyboard shortcut. If the CLI exits before completion is accepted, the step is aborted and the workflow can be resumed.

## Autonomous Agent Steps

Autonomous steps run without user interaction. Depending on `~/.agent-runner/settings.yaml`, autonomous steps may run in headless mode or in an interactive backend with autonomy instructions.

Capturing an autonomous agent step forces headless execution so `stdout` can be captured reliably.

## Interactive Shell Steps

Shell steps normally run through `sh -c`, resolved from `PATH`. A shell step can set `mode: interactive` to run in a PTY.

```yaml
- id: open-shell-tool
  command: ./scripts/manual-tool.sh
  mode: interactive
```

Interactive shell steps cannot use `capture`.

## UI Steps

UI steps render inside the live run TUI and require a TTY.

```yaml
- id: choose-cli
  mode: ui
  title: "Choose CLI"
  body: "Select the CLI for this run."
  inputs:
    - kind: single_select
      id: cli
      prompt: "CLI"
      options: ["claude", "codex"]
      default: "claude"
  actions:
    - label: "Continue"
      outcome: continue
  capture: setup_inputs
  outcome_capture: setup_action
```

`capture` stores UI inputs as a map. `outcome_capture` stores the selected action outcome as a string.
