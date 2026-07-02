---
title: Troubleshooting
group: Reference
order: 9
description: Common Agent Runner failures and recovery commands.
---

# Troubleshooting

Most Agent Runner failures are workflow resolution, parameter, session, or continuation issues. The sections below list the common causes and the command or YAML change that fixes each one.

## Missing Required Parameter

Pass the parameter positionally or as `key=value`.

```bash
agent-runner review-pr 42
agent-runner review-pr pr_number=42
```

## Unknown Workflow

Use a project/user workflow name, a path-like name under `.agent-runner/workflows`, or a built-in name such as `core:run-validator`.

```bash
agent-runner core:run-validator
agent-runner openspec:plan-change my-change
```

## Unknown CLI Adapter

Valid step-level `cli` values are:

| CLI |
| --- |
| `claude` |
| `codex` |
| `copilot` |
| `cursor` |
| `opencode` |

## Agent Step Requires Agent

Fresh sessions need an agent profile. Add `agent: planner`, `agent: implementor`, or another configured profile, or use `session: resume`, `session: inherit`, or a declared named session when that is what you intend.

```yaml
- id: plan
  agent: planner
  prompt: "Plan the change."
```

## Interactive Step Will Not Advance

The workflow advances when any continuation trigger is detected:

| Trigger | Action |
| --- | --- |
| `/next` | Type `/next` and press Enter. |
| `Ctrl-]` | Press the keyboard continuation shortcut. |
| Injected continuation marker | Ask the agent to continue, or write the prompt so it completes the step using its injected continuation-marker instruction. |

If the CLI exits without one of these triggers, the step is aborted and the workflow can be resumed.

## Debug A Run

From an inactive run detail view, press `d` to launch the built-in debug workflow for that run.

You can also launch the workflow directly:

```bash
run_id="replace-with-run-id"
session_dir="/path/to/session-dir"

agent-runner run core:debug
agent-runner run core:debug failed_run_id="$run_id"
agent-runner run core:debug failed_session_dir="$session_dir"
```

Read-only debug inspection commands are available for state, audit summaries, and embedded workflow YAML:

```bash
run_id="replace-with-run-id"
session_dir="/path/to/session-dir"
workflow_ref="openspec:plan-change"

agent-runner debug --state "$run_id"
agent-runner debug --audit-summary "$run_id"
agent-runner debug --state-dir "$session_dir"
agent-runner debug --audit-summary-dir "$session_dir"
agent-runner debug --show-workflow "$workflow_ref"
```

## Resume A Run

Use the TUI:

```bash
agent-runner -resume
```

Or resume a specific run ID from the current project:

```bash
run_id="replace-with-run-id"
agent-runner -resume "$run_id"
```
