## Context

`call_agent` is a Runner-owned stdio MCP tool. The MCP process only forwards a typed request over the run's private control socket; the supervising Agent Runner owns validation, child execution, and evidence.

Today that forwarding is a single blocking round-trip: the host's `tools/call` stays open until the child is terminal. The accepted MCP request and its control connection lease the child, so a host timeout or bridge exit cancels it.

Cursor's MCP client times `tools/call` at the TypeScript SDK default of about 60 seconds. There is no Cursor setting or `mcp.json` field to raise it. Cursor does not pass `resetTimeoutOnProgress`, so Runner's progress notifications do not extend the wait. The failure is `MCP error -32001: Request timed out`. That is a host-client limit, not a Runner-imposed deadline. It showed up when sequential `call_agent` crosschecks from Cursor died before any child output returned.

This change keeps Runner ownership and the one-child-at-a-time rule. It changes only how the parent MCP client waits.

## Goals / Non-Goals

**Goals:**

- Let a Cursor (or similarly short-wait) MCP host drive a child that runs longer than the host `tools/call` wait.
- Return from `call_agent` as soon as the call is accepted.
- Collect the terminal result with a later short `get_agent_call`.
- Cancel through an explicit tool, not by dropping an MCP request.
- Keep serial, nested, single-level agent calls and existing evidence/session behavior.

**Non-Goals:**

- Fixing or depending on Cursor timeout configuration.
- Adopting MCP experimental async / task-augmented execution.
- Parallel calls, result streaming, or long-poll waits.
- New workflow YAML tool names.
- Moving child execution into the MCP process.

## Approach

```text
parent CLI
  -> call_agent / get_agent_call / cancel_agent_call
     -> agent-runner internal MCP bridge
        -> authenticated per-run control socket
           -> supervising Runner (owns child lifecycle)
              -> called agent CLI
```

Workflow YAML still declares `tools: [call_agent]`. That one declaration provisions all three MCP tools. Poll and cancel are not step-model tool names.

Control RPCs split today's blocking "start and wait" into:

- start: validate, accept, reserve request ID, return `call_id`, launch the child without the MCP caller waiting;
- get: return current status or the cached terminal result;
- cancel: terminate a running child for that `call_id`.

The MCP bridge must not block `call_agent` on child completion. Each `get_agent_call` is a new short control request.

## Decisions

### 1. Explicit start/poll/cancel tools, not MCP async tasks

Cursor and the other supported CLIs already speak ordinary `tools/call`. MCP task-augmented execution is experimental and client-dependent; Cursor timing out `tools/call` is evidence it would not save us.

`call_agent` starts, `get_agent_call` reads, `cancel_agent_call` aborts. Same contract on Claude, Codex, Copilot, Cursor, and OpenCode.

A dual API (blocking `call_agent` plus async tools) would leave Cursor on a path that still dies at 60 seconds and would teach parents two contracts.

### 2. Lease the child to the parent attempt

After acceptance, losing the MCP request, stdio bridge, or one control connection MUST NOT cancel the child. Otherwise Cursor's timeout still kills the work.

The child dies when the parent attempt is canceled, stopped, or exits, or when `cancel_agent_call` runs. A later MCP bridge process on the same attempt can poll the same `call_id`.

This replaces the current "live MCP request leases the child" rule in agent-calls and step-control-channel.

### 3. Start returns on accept; poll returns immediately

Acceptance already happens before CLI launch. Returning `call_id` at that boundary keeps start well under a 60-second host wait even when spawn is slow; spawn failure is a later terminal `get_agent_call` result.

`get_agent_call` returns the current snapshot. No server-side wait. Short calls take two tool calls (start, then poll). That is cheaper than reintroducing a wait close to Cursor's deadline.

In-progress polls carry `call_id`, status (`accepted` or `running`), target, and elapsed time — not the child transcript. Terminal polls reuse today's structured success/error plus `call_id` and status.

### 4. Identity and concurrency

`call_id` is the accepted-call identity returned to the parent. Poll and cancel require it.

A retry of the same accepted `call_agent` MCP request ID returns that `call_id` immediately and does not spawn again.

A distinct second `call_agent` while one child is running still returns `call_in_progress`, now including the active `call_id`, and tells the caller to poll or cancel. `get_agent_call` and `cancel_agent_call` are not concurrent starts.

### 5. Skill change lands in Agent Skills

The MCP tool descriptions in this repo must be enough for a parent that only reads schemas. The published `codagent:call-agent` skill still says the tool is synchronous, so implementation also edits `/Users/paul/codagent/agent-skills/skills/call-agent/SKILL.md` (sibling commit): start, poll until terminal, explicit cancel, never treat in-progress as success.

### 6. Parent session discovery excludes nested children

An interactive Cursor parent and a Cursor child both write chats under the same workspace. Parent durability confirmation requires exactly one matching chat after spawn. A nested child makes that scan ambiguous, so the parent session ID is empty and checkpoint confirmation fails.

Agent-call records keep each child's discovered session ID, including while the child is still running when launch output or chat metadata already identifies it. Parent `DiscoverSessionID` (live durability and post-exit) excludes those IDs. Unrelated extra chats with no excludes still return empty.

## Risks / Trade-offs

- **BREAKING** for any parent that waits on one `call_agent` tools/call for the child text. Workflow YAML need not change; skills and prompts that assume a blocking result must poll.
- Interactive hosts may prompt once per new tool name. Autonomous parents pre-authorize all three.
- If start is delayed until after a slow spawn, Cursor could still time out. Returning at accept avoids that.
- A parent that never polls still runs the child until the parent attempt ends; it just never sees the result. The skill requires polling.
- Adapter timeout-raising can stay as belt-and-suspenders. It is not the mechanism that makes long children work.
