---
title: Agent Calls
group: Guides
order: 6
description: Let an agent step synchronously delegate work to another Agent Runner profile or named session.
---

# Agent Calls

Agent calls let an agent delegate a task discovered during its turn while Agent Runner keeps session resolution, execution controls, output, and metrics outside the model. The parent invokes the Runner-owned `call_agent` tool, waits for one autonomous child, and receives the child's response or a structured failure.

An agent call is a nested execution beneath its parent attempt. It is not a workflow step and does not change workflow sequencing.

## Enable The Tool

Put the literal, case-sensitive text `call_agent` in the parent step's authored `prompt` template:

```yaml
steps:
  - id: implement
    agent: lead
    mode: autonomous
    prompt: |
      Implement the requested change. Use call_agent with agent `reviewer`
      when an independent review would help.
```

Agent Runner checks the authored template before interpolation or workflow-engine enrichment. Text introduced only by a variable or engine does not enable the tool. A step without the literal token receives no agent-call integration.

The integration is process-local for the spawned parent. Agent Runner does not edit global or project CLI configuration. Children started by `call_agent` never receive the tool, even if their prompt mentions it, so delegation is single-level.

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

## Execution And Safety

Called children always run autonomous-headless through the resolved profile and CLI adapter. They receive the profile system prompt and the call's `prompt`, but not workflow-step enrichment. An omitted `workdir` inherits the parent's effective directory; an override must remain valid under the normal agent-step workdir rules and within the same worktree.

Calls are synchronous and serial per parent attempt:

- the parent tool invocation remains pending until the child succeeds, fails, or is canceled;
- only one call may be in flight; a distinct concurrent request is rejected instead of queued or used to cancel the active child;
- after a call finishes, the parent may make another call; and
- a named session that resolves to the parent's active CLI session is rejected to prevent concurrent turns against the same native session.

A call is accepted only after authentication, schema and target validation, safety checks, and request-ID reservation. Rejections before that boundary create no child execution. Once accepted, the call remains visible even if the CLI fails to launch. Retrying the same request ID returns the cached result; a later request is a distinct call.

Autonomous parents receive pre-authorized access only to the Runner-owned `call_agent` tool. Interactive parents use their CLI's normal MCP approval flow. Agent Runner imposes no fixed child duration limit. Where a CLI supports a process-local MCP timeout setting, Runner avoids its generic short default; an explicit client deadline is still honored.

The live request leases the child. Canceling or stopping the parent, canceling the tool request, or losing the bridge/control connection terminates the child and retains terminal evidence. A child failure returns control to the parent as a structured error; it does not automatically fail the parent step or retry the call.

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

Selecting a call shows its request and parent identity, target, resolved profile, CLI, model, session metadata, working directory, prompt, outcome, duration, metrics, error, and retained output. In an inactive run, a successful call with a known native CLI session ID can use the normal direct-session resume action.

The completed summary treats a parent with calls as a container. Entering it shows `parent turn` followed by each call. Usage and cost include the parent attempts and every call exactly once. Duration remains the parent's wall-clock duration because synchronous child time overlaps the time the parent spent waiting.

## Evidence And Output

Run evidence stays in the normal run directory described in [Run State And Audit](run-state-and-audit.md):

- `audit.log` contains `agent_call_start` and `agent_call_end` metadata under the parent prefix;
- `output/<call-prefix>.out` and `.err` retain call-specific stdout and stderr using the same limits and privacy behavior as other headless execution output;
- `run-metrics.json` stores one `kind: "agent-call"` execution record per terminal accepted call, including failed launches; and
- `state.json` persists named-session mappings, including sessions first created by calls.

The run view reads full child output from the output files. It does not rebuild a child response from audit metadata.

## Troubleshooting

- If the tool is absent, confirm the authored YAML prompt itself contains the exact text `call_agent`; interpolated text does not count.
- If an interactive call waits, complete the CLI's normal tool-approval prompt. Autonomous parents pre-authorize only this Runner-owned tool.
- If a second call reports `call_in_progress`, wait for or cancel the active call. Calls do not queue or run in parallel.
- If a named target is rejected, confirm it is declared, is not `new`, `resume`, or `inherit`, and does not resolve to the parent's own active session.
- If a call stops unexpectedly, check for an explicit client deadline, parent cancellation, bridge exit, or lost control connection. Progress events do not override client timeout policy.
- If usage or cost is unavailable, the child CLI may not have launched or may not have reported the metric. Failed launches remain visible but do not reduce usage coverage.
- Agent calls do not provide recursive delegation, parallel fan-out, interactive children, call-specific duration budgets, or workflow-engine enrichment.
