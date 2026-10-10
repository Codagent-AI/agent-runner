- [x] Implement the change described by these files using TDD, as `CLAUDE.md` requires. Satisfy every spec scenario and the `INT-001` obligation in the test plan.

  **Claude adapter**
  - In `internal/cli/claude.go`, restructure `ClaudeAdapter.SpawnEnv` as shown in `design.md` → Approach.
    - Return `nil, nil` for a nil input.
    - Call `validatedAgentCall(input)`, and return its error wrapped as `claude: prepare agent-call integration: %w`.
    - Keep the headless-only entries exactly as they are today, in the same order: `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`, then `BASH_DEFAULT_TIMEOUT_MS=600000` only when `BASH_DEFAULT_TIMEOUT_MS` is not defined.
    - When the agent-call integration is present and `MCP_TOOL_TIMEOUT` is not defined (`os.LookupEnv`), append `MCP_TOOL_TIMEOUT=` + `strconv.FormatInt(agentCallTimeoutMilliseconds, 10)`. Do this in every invocation context.
    - Return `nil` when there is nothing to contribute.
  - Update the `SpawnEnv` doc comment to describe the agent-call timeout.
  - Update the comment on `agentCallTimeoutMilliseconds` in `internal/cli/agent_call_integration.go`. It is shared by Copilot's per-server `timeout` and Claude's `MCP_TOOL_TIMEOUT`, and it must stay at or below the Node.js 32-bit timer maximum.
  - Do not add a timeout to the shared plugin `.mcp.json`, because `prepareAgentCallPlugin` still passes `0`.
  - Do not set `CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT`, `CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS`, or `MCP_TIMEOUT`.
  - Do not change other adapters or called-child environments.

  **Unit tests (`internal/cli`)**
  - Add `TestClaudeAgentCallSpawnEnvironment` to `adapter_test.go`. Use `agentCallTestInput(t, "claude", ctx)` for `ContextInteractive`, `ContextAutonomousInteractive`, `ContextAutonomousHeadless`, and `ContextExternalUser`.
    - With `MCP_TOOL_TIMEOUT` unset, there is exactly one `MCP_TOOL_TIMEOUT=2147483647`.
    - The headless entries appear only where `IsHeadless()` is true.
    - With `MCP_TOOL_TIMEOUT=60000` set, no `MCP_TOOL_TIMEOUT` entry is returned and the runner's own value is unchanged.
    - An invalid integration descriptor returns an error.
  - Make `TestClaudeHeadlessSpawnEnvironment`, `TestClaudeInteractiveSpawnEnvironment`, and `agentCallTestInput` unset `MCP_TOOL_TIMEOUT` (`t.Setenv` then `os.Unsetenv`) so the developer's environment cannot affect them.
  - In `agent_call_integration_test.go`, extend the Claude case of `TestRegisteredAdaptersProvisionAgentCallProcessLocally` (through `assertAgentCallTimeout` or alongside it) to assert that the prepared env contains `MCP_TOOL_TIMEOUT=2147483647`. The plugin registration must still report timeout `0`.

  **Integration test (INT-001, `internal/exec/agent_test.go`)**
  - Call `buildStepInvocation` with the real `cli.ClaudeAdapter`.
  - Use one step that declares the `call_agent` Runner tool and one step that does not.
  - Set `AGENT_RUNNER_EXECUTABLE` to a temporary executable, and point `HOME` and `XDG_CACHE_HOME` at temporary directories.
  - Run it for `ContextInteractive` and `ContextAutonomousHeadless`.
  - Assert the following:
    - With `call_agent`, the spawn env has exactly one `MCP_TOOL_TIMEOUT=2147483647` and the args include `--plugin-dir`.
    - The interactive env has no `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS`.
    - Without `call_agent`, there is no `MCP_TOOL_TIMEOUT`.
    - With `MCP_TOOL_TIMEOUT=60000` inherited, no entry is added and the runner value is unchanged.

  **Docs**
  - In `docs/agent-calls.md`, update the paragraph "Where a CLI supports a process-local MCP timeout setting…". It must cover the following:
    - Name Claude (`MCP_TOOL_TIMEOUT`) next to Codex and Copilot, and say that a user-set `MCP_TOOL_TIMEOUT` is preserved.
    - On Claude, the idle timeout and interactive auto-backgrounding can still end or detach a waiting call.
    - The child keeps running, and a parent that holds the `call_id` recovers the result with `get_agent_call`.
    - A retried `call_agent` discloses a missing `call_id` through `call_in_progress` only while the original call is still active. It is not a safe general retry after an abort.

  Do not change the agent-call protocol, `get_agent_call` or `cancel_agent_call` semantics, workflow YAML, or persistent Claude configuration. Finish with `make fmt`, `make test`, and `make lint` passing.

  Source files:
  - [proposal.md](proposal.md)
  - [specs/cli-adapter/spec.md](specs/cli-adapter/spec.md)
  - [specs/agent-calls/spec.md](specs/agent-calls/spec.md)
  - [design.md](design.md)
  - [test-plan.md](test-plan.md)
  - [decisions.md](decisions.md)
