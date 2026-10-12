## Context

Agent-call support reaches a Claude parent in two adapter outputs:

- **Args.** `ClaudeAdapter.BuildArgsWithError` (`internal/cli/claude.go`) calls
  `validatedAgentCall(input)`. When the integration is present, it adds `--plugin-dir` for a cached
  plugin directory created by `prepareAgentCallPlugin` (`internal/cli/agent_call_integration.go`).
  In autonomous and external-user contexts it also adds `--allowedTools` for the three
  agent-call tools.
- **Environment.** `ClaudeAdapter.SpawnEnv` returns process-local entries. Today it returns `nil`
  for every non-headless context. For headless contexts it returns:
  - `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`;
  - `BASH_DEFAULT_TIMEOUT_MS=600000`, but only when that variable is not already defined
    (`os.LookupEnv`).

The workflow step path calls `SpawnEnv` through `cli.SpawnEnvForInvocation` in
`buildStepInvocation` (`internal/exec/agent.go`), using the same `BuildArgsInput`, with
`RunnerIntegration` already set for steps that declare the `call_agent` tool. The runner applies the
returned entries to the spawned CLI only, for both interactive and headless spawns (see the
`SpawnEnvContributor` contract in `internal/cli/adapter.go`). Called children
(`internal/exec/agent_call.go`) also go through `SpawnEnvForInvocation`, but they carry no
`RunnerIntegration`, so they are not affected.

Other adapters already raise the timeout:

- Codex writes `tool_timeout_sec = agentCallTimeoutSeconds` into a private `CODEX_HOME`.
- Copilot passes `agentCallTimeoutMilliseconds` (`2147483647`) as the per-server `timeout`.

The shared plugin `.mcp.json` sets no timeout because `prepareAgentCallPlugin` passes `0`. That
plugin directory is also loaded by Cursor.

## Goals / Non-Goals

**Goals:**
- Claude parents with the agent-call integration get `MCP_TOOL_TIMEOUT=2147483647`. This applies
  in every invocation context that can carry the integration.
- A `MCP_TOOL_TIMEOUT` already set in Agent Runner's environment is left in effect.
- No change for invocations without the integration, for other adapters, or for persistent
  configuration.

**Non-Goals:**
- Claude's idle timeout (`CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT`), interactive auto-backgrounding
  (`CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS`), or startup timeout (`MCP_TIMEOUT`).
- Per-server timeout fields in the shared plugin `.mcp.json`.
- Any change to called-child environments or `get_agent_call` semantics.

## Approach

Restructure `ClaudeAdapter.SpawnEnv` into two independent contributions and concatenate them:

```go
func (a *ClaudeAdapter) SpawnEnv(input *BuildArgsInput) ([]string, error) {
	if input == nil {
		return nil, nil
	}
	agentCall, err := validatedAgentCall(input)
	if err != nil {
		return nil, fmt.Errorf("claude: prepare agent-call integration: %w", err)
	}
	var env []string
	if input.InvocationContext().IsHeadless() {
		env = append(env, "CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1")
		if _, defined := os.LookupEnv("BASH_DEFAULT_TIMEOUT_MS"); !defined {
			env = append(env, "BASH_DEFAULT_TIMEOUT_MS=600000")
		}
	}
	if agentCall != nil {
		if _, defined := os.LookupEnv("MCP_TOOL_TIMEOUT"); !defined {
			env = append(env, "MCP_TOOL_TIMEOUT="+strconv.FormatInt(agentCallTimeoutMilliseconds, 10))
		}
	}
	return env, nil
}
```

How this behaves:

- **Ordering and empty results.** Headless entries come first, so existing headless output keeps
  its order and the new entry is appended. An invocation with nothing to contribute still returns
  `nil`, which keeps `TestClaudeInteractiveSpawnEnvironment` and the
  `SpawnEnvForInvocationDefaultsToNil` expectations meaningful.
- **Failure.** If the integration descriptor is invalid, `SpawnEnv` returns the same wrapped error
  `BuildArgsWithError` already returns. In practice `BuildInvocationArgs` fails first. The Codex
  adapter follows the same pattern.
- **Inherited value.** The check uses "defined", not "non-empty", matching
  `BASH_DEFAULT_TIMEOUT_MS`. An explicitly set empty value is the user's choice. `MCP_TOOL_TIMEOUT`
  is not in `claudeEnclosingSessionEnvVars`, so an inherited value reaches the child unchanged.
- **Value source.** The value comes from `agentCallTimeoutMilliseconds`, the constant Copilot
  uses. Its doc comment should note that it is also Claude's `MCP_TOOL_TIMEOUT` and that it must
  stay at or below the Node.js 32-bit timer maximum.

Docs: update the "Where a CLI supports a process-local MCP timeout setting" paragraph in
`docs/agent-calls.md` to name Claude (`MCP_TOOL_TIMEOUT`) and state its limits. On Claude, an
idle-timeout abort or interactive auto-backgrounding can still end or detach the waiting call. The
child keeps running, and a parent that holds the `call_id` recovers with `get_agent_call`. If
the `call_id` never reached the parent, a new `call_agent` discloses it through the
`call_in_progress` rejection, but only while the original call is still active
(`internal/exec/agent_call.go`, where `h.active` is cleared on completion). Once the child has
finished, a new `call_agent` starts a second child, and the earlier result cannot be retrieved
without its ID. The docs must not present a retried `call_agent` as a safe general retry. This is
an existing limitation that the change does not alter; attempt-scoped lookup is follow-up work.

## Decisions

- **Use `SpawnEnv`, not args or plugin config.** `SpawnEnv` is the existing contract for
  process-local environment and already applies to interactive spawns. Writing a timeout into the
  shared plugin `.mcp.json` would also affect Cursor, and Claude's per-server field behavior is
  unverified (see the proposal).
- **Gate on `validatedAgentCall`, not on the context.** The plugin is added in every context, so
  the timeout must be too. Gating on the integration keeps runs without agent calls unchanged.
- **Reuse the constant instead of adding a Claude-specific one.** One ceiling across Copilot and
  Claude, and no risk of a Node timer overflow from a larger number. `strconv.FormatInt` avoids
  formatting drift.

## Risks / Trade-offs

- **Process-wide ceiling.** `MCP_TOOL_TIMEOUT` also governs any other MCP server the user loads
  into that Claude process. This is accepted because it applies only to Runner-spawned parents with
  agent calls, and a user-set value wins.
- **Claude Code may rename or ignore the variable.** The behavior would fall back to Claude's
  default, which is today's behavior. Recovery through `get_agent_call` still works when the
  parent holds the `call_id`.
- **Lost `call_id` after completion.** This already exists and is independent of this change. If a
  host ends the waiting request before the ID reaches the parent and the child finishes before
  the parent retries, the result is unreachable and a retry launches a new child. It is documented
  as a limitation, not fixed here.
- **Interaction with PR #239.** The spec deltas are based on main's current SHALL text. If #239
  merges first, the archive step reconciles the wording. This needs no code change.

## Testing

Unit tests live in `internal/cli`:

- `adapter_test.go`: a new table test, `TestClaudeAgentCallSpawnEnvironment`. Build an agent-call
  input with `agentCallTestInput(t, "claude", ctx)` for `ContextInteractive`,
  `ContextAutonomousInteractive`, `ContextAutonomousHeadless`, and `ContextExternalUser`. Assert:
  - when `MCP_TOOL_TIMEOUT` is unset, the env contains exactly one
    `MCP_TOOL_TIMEOUT=2147483647`, plus the headless entries only in headless contexts (`ContextAutonomousHeadless` and `ContextExternalUser`, per `IsHeadless`);
  - with `t.Setenv("MCP_TOOL_TIMEOUT", "60000")`, no `MCP_TOOL_TIMEOUT` entry is returned and the
    runner's own value is unchanged;
  - an interactive context gets no `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS`.
- Existing `TestClaudeHeadlessSpawnEnvironment` and `TestClaudeInteractiveSpawnEnvironment` keep
  passing unchanged. They cover the case without the integration: nil for interactive, and no
  `MCP_TOOL_TIMEOUT` for headless. Each should unset `MCP_TOOL_TIMEOUT` so they are not affected by
  the developer's environment.
- `agent_call_integration_test.go`: extend `assertAgentCallTimeout` (or its caller) so the
  `claude` case asserts the prepared env carries `MCP_TOOL_TIMEOUT=2147483647`. The plugin
  registration itself must keep timeout `0`, which protects Cursor's shared plugin. Unset
  `MCP_TOOL_TIMEOUT` in `agentCallTestInput`.

No integration test is needed beyond these. The runner already applies `SpawnEnv` entries to both
spawn paths, and existing tests cover that.

## Migration Plan

None. The change is additive and process-local. To roll back, revert the `SpawnEnv` change.
