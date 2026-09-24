## Why

Cursor’s MCP client aborts a `tools/call` after about 60 seconds. Agent Runner’s `call_agent` holds that request open until the child finishes, so any child that needs more than a minute fails with `MCP error -32001: Request timed out` even though Runner itself imposes no duration limit. Progress notifications do not extend Cursor’s wait. This blocks real `codagent:call-agent` crosschecks launched from Cursor.

## What Changes

- `call_agent` keeps waiting for the child on every CLI that can hold a long `tools/call` open, so a waiting parent cannot end its turn on an uncollected call. A Cursor parent gets a wait budget under its 60-second abort: `call_agent` then returns a `call_id`, and the parent collects the terminal result with `get_agent_call`. Either parent may abort with `cancel_agent_call`.
- A parent attempt that ends with an accepted call still running fails the step instead of recording a success on evidence the canceled child never wrote.
- Lease the child to the parent attempt, not the live MCP request. Timing out, canceling, or restarting a start/poll MCP call after acceptance MUST NOT kill the child. Parent step cancel, stop, or exit still kills it.
- Keep workflow YAML as `tools: [call_agent]`. That declaration provisions the three MCP tools; poll and cancel are not new YAML tool names.
- Keep one-in-flight serial calls. `get_agent_call` and `cancel_agent_call` are not a second child.
- Update `docs/agent-calls.md` and the published `codagent:call-agent` skill in the sibling Agent Skills repository so parents start, poll until terminal, and cancel explicitly.

## Capabilities

### New Capabilities

- None. This changes how the existing agent-call integration is used over MCP.

### Modified Capabilities

- `agent-calls`: Bound the MCP wait per parent CLI, add poll and cancel, change the execution lease, return terminal results through `get_agent_call`, and fail a parent step that leaves a call uncollected.
- `cli-adapter`: Provision and (for autonomous parents) pre-authorize `call_agent`, `get_agent_call`, and `cancel_agent_call`.
- `step-control-channel`: Stop leasing an accepted child to a single MCP/control connection; retries of `call_agent` return the same `call_id` without waiting for the child.
- `call-agent-skill`: Drive the start → poll loop (and optional cancel) instead of treating `call_agent` as a synchronous result.

## Out of Scope

- Raising or relying on a host MCP `tools/call` timeout, including Cursor settings that do not exist.
- MCP experimental async / task-augmented execution.
- Unbounded server-side long-poll for hosts that abort a `tools/call`.
- Streaming the child’s transcript through poll results.
- Parallel or queued agent calls.
- New workflow YAML tool names for poll or cancel.
- Recursive `call_agent` on called children.

## Impact

Agent-call MCP contract and stdio bridge (`internal/agentcall`), supervising call runtime (`internal/exec` agent-call path), control-channel agent-call RPCs (`internal/control`), CLI adapter provisioning and autonomous pre-authorization (`internal/cli`), `docs/agent-calls.md`, and tests around bridge, runtime, cancellation, and adapters.

The published skill at `/Users/paul/codagent/agent-skills/skills/call-agent/SKILL.md` is a coordinated sibling-repo edit. Workflow YAML that already declares `tools: [call_agent]` does not need a new tool name, but any parent that waits on one `call_agent` tools/call for the child text will break until it polls.
