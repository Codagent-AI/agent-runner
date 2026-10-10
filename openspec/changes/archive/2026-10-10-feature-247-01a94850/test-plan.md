## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

Most of the behavior is a pure decision inside `ClaudeAdapter.SpawnEnv`: which context, whether the
integration is present, and whether the variable is inherited. Unit tests in `internal/cli` (see
`design.md` → Testing) cover it. One integration obligation covers the wiring that unit tests
cannot prove: a workflow step that declares `call_agent`, run through the real Claude adapter,
produces a spawn environment carrying the timeout, and a step without `call_agent` does not.

No automated E2E test is added. Proving that Claude Code honors `MCP_TOOL_TIMEOUT` would require a
real authenticated Claude session waiting past Claude's default tool timeout, which is many hours.
That is too slow, too costly, and too flaky for CI, and what it would show is Claude's behavior, not
Runner's. Runner's contract stops at the spawned process's environment, which INT-001 and the
acceptance pass observe directly.

## Integration Tests

### INT-001: call_agent step wiring delivers the Claude timeout to the spawn environment

- Covers:
  - `cli-adapter` "Long-running tool controls": the headless and interactive Claude scenarios, the
    user-set value scenario, and the "without agent-call integration" scenario.
  - `agent-calls` "Long-running MCP execution": the Claude wall-clock scenario, at the Runner
    boundary.
- Boundary: `buildStepInvocation` (`internal/exec/agent.go`) wiring step tools, the
  `RunnerIntegration` descriptor, the real `cli.ClaudeAdapter` (`BuildInvocationArgs` and
  `SpawnEnvForInvocation`), and the returned spawn environment.
- Setup:
  - A `model.Step` that declares the `call_agent` Runner tool, and a second step that does not.
  - `AGENT_RUNNER_EXECUTABLE` pointed at a temporary executable file.
  - `HOME` and `XDG_CACHE_HOME` set to temporary directories so the plugin cache is isolated.
  - `MCP_TOOL_TIMEOUT` unset, using `t.Setenv` then `os.Unsetenv`.
  - Contexts: `ContextInteractive` and `ContextAutonomousHeadless`.
- Action:
  - Call `buildStepInvocation` for each step and context.
  - Repeat the `call_agent` case with `t.Setenv("MCP_TOOL_TIMEOUT", "60000")`.
- Assertions:
  - With `call_agent` and the variable unset, the spawn env contains exactly one
    `MCP_TOOL_TIMEOUT=2147483647` in both contexts.
  - The args include `--plugin-dir`.
  - The interactive env has no `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS`.
  - Without `call_agent`, the spawn env has no `MCP_TOOL_TIMEOUT` entry.
  - With the variable already set, the spawn env has no `MCP_TOOL_TIMEOUT` entry and
    `os.Getenv("MCP_TOOL_TIMEOUT")` is still `60000`.
- Execution: `internal/exec/agent_test.go`, run by the existing `go test ./...` (`make test`) in
  the CI `test` job.

## End-to-End Tests

None. See Coverage Strategy.

## Acceptance Testing Envelope

- Environments and sandboxes:
  - The local repository checkout or worktree, with `./dev.sh` running the CLI from source.
  - Temporary workflow YAML files under a scratch directory.
  - Claude Code is installed locally (`claude` 2.1.296 at the time of planning).
- Credentials and secrets: the local Claude Code login of the machine's user, if one exists. No
  other credentials are needed, and none may be created.
- Authorized effects:
  - Short real Claude runs, a few prompts in total, to check that a `call_agent`-enabled step
    launches and that the agent-call tools are available. Token cost is small.
  - Writing scratch workflows and temporary directories. Clean them up afterwards.
  - The agent-call plugin cache under the user cache directory, which Runner already manages.
- Off limits:
  - Changing the user's global or project Claude configuration (`~/.claude`, `.claude/`,
    `.mcp.json`).
  - Setting `MCP_TOOL_TIMEOUT` persistently in shell profiles.
  - Long or expensive child runs, and any network effects beyond the Claude API calls those
    prompts make.
- Permitted substitutes:
  - A stub `claude` executable placed first on `PATH` that records its environment (`env` dumped
    to a file) and exits. Use it to observe the spawned process's `MCP_TOOL_TIMEOUT`:
    - interactive and headless, with and without `call_agent`;
    - with an inherited value set in the launching shell.

    Prefer the stub for environment observation, and the real Claude only to confirm launch still
    works.
  - Agent-runner's dev audit or value-audit output may substitute for the stub where it already
    reports spawn env.
- Known risk areas:
  - The shared agent-call plugin directory is also used by Cursor. Confirm the generated
    `.mcp.json` still has no `timeout` field.
  - Existing headless-only settings (`CLAUDE_CODE_DISABLE_BACKGROUND_TASKS`,
    `BASH_DEFAULT_TIMEOUT_MS`) must not leak into interactive runs.
  - Called children (`internal/exec/agent_call.go`) must not gain `MCP_TOOL_TIMEOUT`, because they
    carry no integration.
  - Accepted limitations, not defects (see `design.md`): Claude's idle timeout and interactive
    auto-backgrounding can still end or detach a long waiting call. The process-wide variable
    also raises the ceiling for other MCP servers in that Claude process. Whether Claude Code
    actually honors the variable is Claude's behavior and is not proven by acceptance.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| cli-adapter: Long-running tool controls (Claude headless/interactive, user-set value, no integration) | INT-001 | — | — |
| agent-calls: Long-running MCP execution (Claude wall-clock timeout raised) | INT-001 | — | — |
