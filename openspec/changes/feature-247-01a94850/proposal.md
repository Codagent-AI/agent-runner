## Why

A Claude parent that calls a long-running child through `call_agent` depends on Claude Code's
MCP tool-execution timeout. Agent Runner sets nothing for it today. The Codex adapter writes
`tool_timeout_sec` into its private `CODEX_HOME`, and the Copilot adapter writes a per-server
`timeout` into its MCP config. The Claude adapter adds the agent-call plugin but leaves the
timeout to whatever Claude Code version is installed. Claude Code has changed its MCP timeout
behavior more than once. For example, one release regressed stdio tool calls to roughly a
one-second default. So a Claude parent's single waiting `call_agent` can be cut short by a host
default rather than by the user or the child.

Children survive an aborted request: a parent that holds the call's `call_id` can recover with `get_agent_call`. That costs an
extra polling round trip and parent turns, though, and depends on the parent agent noticing the
abort. The current `cli-adapter` "Long-running tool controls" and `agent-calls` "Long-running MCP
execution" requirements say a supported host's process-local timeout control SHALL be raised.
Claude exposes one through `MCP_TOOL_TIMEOUT` and does not comply. PR #239 (#238) softens that
clause to MAY to match the code. This change brings Claude in line with Codex and Copilot, so
every host that offers a wall-clock tool-execution timeout control has it raised.

This change raises Claude's wall-clock ceiling only. It does not promise an uninterrupted
blocking wait on Claude. Claude Code has two other limits that the issue does not ask to change:

- **Idle timeout:** from v2.1.203, Claude Code applies an idle timeout (30 minutes by default) to
  stdio MCP calls, and `MCP_TOOL_TIMEOUT` does not raise it. The Runner bridge emits progress
  only when the client supplies a progress token, and it is not established that Claude does.
  So a silent wait for a very long child can still be aborted.
- **Auto-backgrounding:** in interactive sessions, Claude Code v2.1.212 and later move a
  main-conversation MCP call into a background task after about two minutes. The result arrives
  later rather than in the original call.

In both cases the child keeps running. A parent that holds the `call_id` recovers the result
through `get_agent_call` or the backgrounded task. If the request ended before the `call_id`
reached the parent, a retried `call_agent` discloses it through `call_in_progress`, but only
while the child is still active. Once the child has finished, a retry starts a new child. That
lost-ID gap already exists, and this change neither creates nor fixes it.

## What Changes

- When a Claude invocation includes the Runner agent-call integration, the Claude adapter adds
  `MCP_TOOL_TIMEOUT` to the spawned process's environment. This applies to both interactive and
  headless invocations, autonomous or not. The value is the same long ceiling Copilot uses,
  `2147483647` ms. That is the largest value a Node.js timer accepts without overflowing to an
  immediate timeout.
- If `MCP_TOOL_TIMEOUT` is already defined in the runner's environment, the adapter leaves it
  unchanged. The user's explicit deadline wins, matching how `BASH_DEFAULT_TIMEOUT_MS` is already
  handled.
- Invocations without the agent-call integration get no `MCP_TOOL_TIMEOUT`.
- The timeout lives only in the spawned Claude process's environment. No user, project, or global
  Claude configuration is written.
- Specs and docs: Claude becomes a host that applies the supported wall-clock timeout control. If
  PR #239 has merged, the change restores the "supported host SHALL raise its wall-clock tool
  timeout control" guarantee, because every adapter whose host exposes that control would then
  comply. The wording is scoped to the wall-clock ceiling. It must not claim Claude keeps one
  uninterrupted blocking wait, given the idle timeout and the interactive auto-backgrounding
  described above.

No breaking changes. No public interface, workflow schema, or persisted state changes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cli-adapter`: "Long-running tool controls" gains Claude-specific scenarios. With the agent-call
  integration, the Claude process receives a long `MCP_TOOL_TIMEOUT` unless the user already
  set one. Without it, the variable is not added. The adapter's process-local environment
  contract (`SpawnEnv`) extends to interactive Claude invocations that carry the integration.
- `agent-calls`: "Long-running MCP execution" states that the wall-clock tool-execution timeout
  control is raised on every host that exposes it, Claude included. It returns to SHALL wording
  if #239 softened it to MAY. The requirement says that host idle or backgrounding behavior can
  still end or detach a waiting call. A parent holding the `call_id` then recovers the result
  through `get_agent_call`. `call_in_progress` discloses a missing ID only while the call is
  active. Child survival still never depends on this setting.

## Technical Approach

The work belongs in `ClaudeAdapter.SpawnEnv` (`internal/cli/claude.go`). Its entries are applied
only to the spawned CLI process, for both headless and interactive spawns. Today that method
returns nothing for interactive invocations. It will now use `validatedAgentCall(input)` to decide
whether to add `MCP_TOOL_TIMEOUT`, and keep its existing headless-only entries
(`CLAUDE_CODE_DISABLE_BACKGROUND_TASKS`, `BASH_DEFAULT_TIMEOUT_MS`) unchanged. The value comes
from the shared `agentCallTimeoutMilliseconds` constant in `agent_call_integration.go`, so Claude
and Copilot cannot drift.

Key decision: use the process environment variable the issue names, not a per-server `timeout`
field in the generated plugin `.mcp.json`. Claude Code reportedly honors a per-server field that
overrides `MCP_TOOL_TIMEOUT` for that server only, which would be tighter scoping. Three things
argue against it here:

- the issue specifies `MCP_TOOL_TIMEOUT`;
- reports say per-server handling has regressed across Claude Code releases, and whether a
  plugin-sourced `.mcp.json` honors it is unverified;
- the same generated plugin directory is also loaded by Cursor, whose handling of the field is
  unknown.

The trade-off is that `MCP_TOOL_TIMEOUT` also raises the ceiling for any other MCP server the
user loads into that one Claude process. That is acceptable because it is limited to the Runner
spawn that carries the agent-call integration, and a user who sets the variable keeps their own
value. Design can revisit the per-server field once it is verified. Claude documents that the
per-server value also sets the idle-window floor, so it is also the likely way to close the
idle-timeout gap later. It must not go into the plugin folder Cursor shares until Cursor's
handling is verified.

Interactive invocations keep Claude's default auto-backgrounding. The adapter does not set
`CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS` or extend `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS` to
interactive runs. Backgrounding a long call in an interactive session is reasonable host
behavior, and changing it would be a separate policy decision.

Tests are adapter unit tests next to the existing Claude `SpawnEnv` coverage. They cover headless
and interactive with the integration, without the integration, and an inherited user value.

## Out of Scope

- Changing timeouts for Codex, Copilot, Cursor, or OpenCode, including adding a Cursor wait budget
  or polling changes.
- Claude Code's startup timeout (`MCP_TIMEOUT`).
- Claude Code's MCP idle timeout (`CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT`, which also applies to stdio
  from v2.1.203), and checking whether Claude sends progress tokens. Idle aborts remain
  recoverable through `get_agent_call` when the parent holds the `call_id`. A process-local idle policy, or a verified Claude-only
  per-server timeout, is follow-up work.
- Interactive auto-backgrounding (`CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS`) and any change to
  background-task behavior.
- Writing per-server timeout fields into the shared agent-call plugin `.mcp.json`.
- Any Runner-level duration limit on called children, or changes to `get_agent_call` and
  `cancel_agent_call` semantics.
- Reconciling the rest of the #239 spec text beyond the timeout clause this change affects.
- Recovering a call whose `call_id` never reached the parent after that call has completed, for
  example through an attempt-scoped lookup of accepted calls. This is an existing limitation and
  separate follow-up work.

## Impact

- Code: `internal/cli/claude.go` (`SpawnEnv`) and its tests. It possibly reuses helpers in
  `internal/cli/agent_call_integration.go`.
- Specs: `openspec/specs/cli-adapter/spec.md` and `openspec/specs/agent-calls/spec.md` timeout
  requirements.
- Docs: `docs/agent-calls.md` names Claude among the CLIs whose MCP wall-clock timeout Runner
  raises. It also notes the idle-timeout and interactive-backgrounding limits and the
  `get_agent_call` recovery path, including that a retried `call_agent` is not a safe general
  retry after an abort.
- Users: a Claude parent's waiting `call_agent` is no longer bounded by Claude Code's default
  wall-clock tool timeout. Very long silent waits can still hit Claude's idle timeout. Interactive
  parents may still see long calls backgrounded after about two minutes. In both cases the child
  continues, and a parent holding the `call_id` recovers the result through `get_agent_call` or
  the background task.
  Other MCP servers in the same Runner-spawned Claude process share the raised ceiling. A
  user-set `MCP_TOOL_TIMEOUT` is untouched.
- Dependencies: none new. Relies on Claude Code reading `MCP_TOOL_TIMEOUT` in milliseconds.
