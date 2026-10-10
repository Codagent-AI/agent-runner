## MODIFIED Requirements

### Requirement: Long-running tool controls

When a supported CLI exposes process-local control over its MCP tool-execution wall-clock timeout, its adapter SHALL configure that control for the spawned parent so a generic short host default does not govern `call_agent`, while preserving an explicit deadline configured by the user or requesting client. An adapter MUST NOT introduce a Runner-level call duration limit when the host exposes no such control. Timeout handling MUST remain isolated to the spawned parent and MUST NOT modify global or project configuration.

For Claude, the supported control is the `MCP_TOOL_TIMEOUT` environment variable, in milliseconds. When a Claude invocation carries the Runner agent-call integration, the Claude adapter SHALL set `MCP_TOOL_TIMEOUT` to `2147483647` in the spawned Claude process's environment. This SHALL apply to both interactive and headless invocations. When `MCP_TOOL_TIMEOUT` is already defined in Agent Runner's environment, the Claude adapter SHALL leave the inherited value in effect unchanged. When a Claude invocation does not carry the agent-call integration, the Claude adapter MUST NOT add `MCP_TOOL_TIMEOUT`.

The Claude adapter MUST NOT change Claude's MCP idle-timeout or interactive auto-backgrounding behavior as part of this control. Its existing headless-only environment settings remain limited to headless invocations.

#### Scenario: Supported timeout control is applied process-locally
- **WHEN** an enabled parent uses a CLI with a supported MCP tool-execution timeout setting
- **THEN** the adapter configures the Runner-owned server for long-running calls without changing persistent user or project settings

#### Scenario: Adapter without timeout control adds no Runner deadline
- **WHEN** an enabled parent uses a CLI without a supported MCP tool-execution timeout setting
- **THEN** Agent Runner preserves the host's native behavior and introduces no fixed call duration limit of its own

#### Scenario: Headless Claude parent receives the raised tool timeout
- **WHEN** a headless Claude invocation carries the agent-call integration and `MCP_TOOL_TIMEOUT` is not defined in Agent Runner's environment
- **THEN** the spawned Claude process runs with `MCP_TOOL_TIMEOUT=2147483647`

#### Scenario: Interactive Claude parent receives the raised tool timeout
- **WHEN** an interactive Claude invocation carries the agent-call integration and `MCP_TOOL_TIMEOUT` is not defined in Agent Runner's environment
- **THEN** the spawned Claude process runs with `MCP_TOOL_TIMEOUT=2147483647`
- **AND** the spawned process does not receive Runner's headless-only settings, such as disabling background tasks

#### Scenario: User-set Claude tool timeout is preserved
- **WHEN** a Claude invocation carries the agent-call integration and Agent Runner's environment defines `MCP_TOOL_TIMEOUT`
- **THEN** the spawned Claude process runs with the user's `MCP_TOOL_TIMEOUT` value unchanged

#### Scenario: Claude without agent-call integration is unchanged
- **WHEN** a Claude invocation does not carry the agent-call integration
- **THEN** the Claude adapter adds no `MCP_TOOL_TIMEOUT` to the spawned process's environment

#### Scenario: Claude timeout control leaves persistent configuration untouched
- **WHEN** the Claude adapter raises `MCP_TOOL_TIMEOUT` for an invocation
- **THEN** no user, project, or global Claude configuration file is created or modified for the timeout, and Agent Runner's own environment is unchanged
