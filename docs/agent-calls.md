---
title: Agent Calls
group: Guides
order: 6
description: Let an agent step start a nested Agent Runner profile or named session and collect the result by polling.
---

# Agent Calls

Agent calls let an agent delegate a task discovered during its turn while Agent Runner keeps session resolution, execution controls, output, and metrics outside the model. The parent invokes the Runner-owned `call_agent` tool, which returns a `call_id` immediately, then collects the child's response or a structured failure with `get_agent_call`. `cancel_agent_call` aborts a running child without ending the parent step.

An agent call is a nested execution beneath its parent attempt. It is not a workflow step and does not change workflow sequencing.

## Enable The Tool

Declare `call_agent` in the parent agent step's static `tools` sequence:

```yaml
steps:
  - id: implement
    agent: lead
    mode: autonomous
    tools: [call_agent]
    prompt: |
      Implement the requested change. Use codagent:call-agent with a fresh
      `agent: crosscheck` when an independent review would help.
```

`call_agent` is the only supported Runner-owned YAML tool name. Declaring it provisions three MCP tools: `call_agent`, `get_agent_call`, and `cancel_agent_call`. The field must be a YAML sequence, is valid only on agent steps, and is static: unknown names, duplicates, scalars, and placeholders are rejected while loading. Omitting `tools` or declaring `tools: []` enables no Runner-owned tools. Prompt text never changes availability, whether it is authored directly, interpolated, added by an engine, or supplied later in the conversation.

The integration is process-local for the spawned parent. Agent Runner does not edit global or project CLI configuration. Children started by `call_agent` never receive the tools, even if their prompt mentions them, so delegation is single-level.

## Choose A Target

Every invocation requires `prompt` and exactly one target:

| Field | Meaning | Allowed overrides |
| --- | --- | --- |
| `agent: <profile>` | Start a fresh session using an Agent Runner profile. | `cli`, `model`, `workdir` |
| `session: <name>` | Create or resume a workflow-declared named session. | `model`, `workdir` |

The full supported field set is `prompt`, `agent`, `session`, `cli`, `model`, and `workdir`. `session` accepts declared names only; `new`, `resume`, and `inherit` are workflow-step strategies, not call targets. A named-session call cannot also set `agent` or `cli`.

A typical profile-targeted request is equivalent to:

```json
{
  "prompt": "Review the current diff and report consequential defects.",
  "agent": "reviewer",
  "model": "sonnet",
  "workdir": "."
}
```

A named-session request is equivalent to:

```json
{
  "prompt": "Continue the implementation and run focused tests.",
  "session": "implementor-session"
}
```

The agent CLI presents `call_agent` as a tool; these objects document its fields rather than a shell command for users to run.

## Start, Poll, And Cancel

`call_agent` returns as soon as Agent Runner accepts the request. The structured start result includes a stable `call_id` and a non-terminal status (`accepted` or `running`). It does not wait for the child, so a host MCP `tools/call` timeout cannot abort a long child by itself.

Collect the outcome with `get_agent_call` and the same `call_id`:

- While the child is running, the poll returns immediately with `call_id`, status, target, and elapsed time. It does not include the child's final response.
- When the child is terminal, the poll returns that cached success or structured error, including `call_id` and status. Later polls return the same cached result.
- An unknown `call_id` is a structured error and does not spawn a child.

`cancel_agent_call` terminates a running child for that `call_id`, caches a canceled result, and leaves the parent attempt active so it can start another call. Canceling an already-finished call returns the cached terminal result. `get_agent_call` and `cancel_agent_call` are not a second in-flight child.

Do not treat an `accepted` or `running` start or poll as success. A parent that never polls still leaves the child running until the parent attempt ends; it just never sees the result.

## Execution And Safety

Called children always run autonomous-headless through the resolved profile and CLI adapter. They receive the profile system prompt and the call's `prompt`, but not workflow-step enrichment. An omitted `workdir` inherits the parent's effective directory; an override must remain valid under the normal agent-step workdir rules and within the same worktree.

Calls are serial per parent attempt:

- only one call may be in flight; a distinct concurrent `call_agent` request is rejected instead of queued or used to cancel the active child;
- the `call_in_progress` error includes the active `call_id` and tells the caller to poll `get_agent_call` or cancel `cancel_agent_call`;
- after a call finishes, the parent may make another call; and
- a named session that resolves to the parent's active CLI session is rejected to prevent concurrent turns against the same native session.

A call is accepted only after authentication, schema and target validation, safety checks, and request-ID reservation. Rejections before that boundary create no child execution and have no `call_id`. Once accepted, the call remains visible even if the CLI fails to launch. Retrying the same request ID returns the same `call_id` without waiting; `get_agent_call` returns the cached result, including a post-accept launch failure.

Autonomous parents receive pre-authorized access only to the Runner-owned `call_agent`, `get_agent_call`, and `cancel_agent_call` tools. Interactive parents use their CLI's normal MCP approval flow. Agent Runner imposes no fixed child duration limit. Where a CLI supports a process-local MCP timeout setting, Runner may still raise a generic short default; that setting is not required for a long child to complete through start/poll.

The child is leased to the parent attempt. Canceling or stopping the parent, or parent exit, terminates the child and retains terminal evidence. Timing out, canceling, or restarting a `call_agent` / `get_agent_call` MCP request after acceptance, or losing the stdio bridge or one control connection, does not kill the child. A later authenticated poll or cancel with the same `call_id` still works. A child failure returns control to the parent as a structured error; it does not automatically fail the parent step or retry the call.

## Named Session Reuse

Declare named sessions at workflow scope as described in [Sessions And Modes](sessions-and-modes.md):

```yaml
sessions:
  - name: implementor-session
    agent: implementor
```

Calls and ordinary workflow steps share the same run-scoped named-session map. First use creates and persists the CLI session; later calls or steps resume it. A call-level model override affects only that invocation and does not change the profile pinned by the declaration.

Each `agent: <profile>` call is fresh, even when the same profile is called repeatedly. Those session IDs are retained as execution evidence but are not added to the named-session map.

## Live And Completed Views

An accepted call appears beneath its parent with an `↗` glyph and an explicit label such as `call session: implementor-session` or `call agent: reviewer`. The parent shows its call count. Repeated calls remain separate and are ordered by acceptance time.

For an autonomous-headless parent, the live run view follows the active call and streams its stdout and stderr into the child's detail pane, separate from parent output. Manual navigation pauses auto-follow. When an interactive parent owns the terminal, Agent Runner does not interrupt it to draw the TUI; accumulated calls appear when terminal ownership returns.

Selecting a call shows its request and parent identity, target, resolved profile, CLI, model, session metadata, working directory, prompt, outcome, duration, metrics, error, and retained output. In an inactive run, a completed call with a known native CLI session ID can use the normal direct-session resume action.

The completed summary treats a parent with calls as a container. Entering it shows `parent turn` followed by each call. Usage and cost include the parent attempts and every call exactly once. Duration remains the parent's wall-clock duration because nested child time overlaps the time the parent spent waiting or polling.

## Evidence And Output

Run evidence stays in the normal run directory described in [Run State And Audit](run-state-and-audit.md):

- `audit.log` contains `agent_call_start` and `agent_call_end` metadata under the parent prefix;
- `output/<call-prefix>.out` and `.err` retain call-specific stdout and stderr using the same limits and privacy behavior as other headless execution output;
- `run-metrics.json` stores one `kind: "agent-call"` execution record per terminal accepted call, including failed launches; and
- `state.json` persists named-session mappings, including sessions first created by calls.

The run view reads full child output from the output files. It does not rebuild a child response from audit metadata.

## Troubleshooting

- If the tools are absent, confirm the active parent agent step declares `tools: [call_agent]`. Mentioning `call_agent` in the prompt does not enable them.
- If an interactive call waits, complete the CLI's normal tool-approval prompt. Autonomous parents pre-authorize only these Runner-owned tools.
- If `call_agent` returns a `call_id` with status `accepted` or `running`, poll `get_agent_call` until the call is terminal. Do not treat that start result as the child's response.
- If a host reports `MCP error -32001: Request timed out` on `call_agent`, the start should already have returned a `call_id`. Poll `get_agent_call`; do not assume the child died. Cursor and similar hosts time out a single `tools/call` around 60 seconds, and progress notifications do not extend that wait.
- If `get_agent_call` is missing after a start that returned a `call_id`, that is a blocker. Do not wait on another `call_agent` or substitute a different delegation path.
- If a second `call_agent` reports `call_in_progress`, poll or cancel the active `call_id`. Calls do not queue or run in parallel.
- To abort a running child without ending the parent step, invoke `cancel_agent_call`. Canceling the MCP `tools/call` for start or poll does not stop the child.
- If a named target is rejected, confirm it is declared, is not `new`, `resume`, or `inherit`, and does not resolve to the parent's own active session.
- If usage or cost is unavailable, the child CLI may not have launched or may not have reported the metric. Failed launches remain visible but do not reduce usage coverage.
- Agent calls do not provide recursive delegation, parallel fan-out, interactive children, call-specific duration budgets, or workflow-engine enrichment.
