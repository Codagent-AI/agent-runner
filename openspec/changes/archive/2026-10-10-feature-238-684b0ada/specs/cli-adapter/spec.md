## ADDED Requirements

### Requirement: Headless Claude runs without background tasks

When the Claude adapter builds a headless invocation, it SHALL contribute `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` to the spawned process's environment. It SHALL also contribute `BASH_DEFAULT_TIMEOUT_MS=600000` unless the runner's own environment already defines `BASH_DEFAULT_TIMEOUT_MS`, in which case the inherited value SHALL be left unchanged. These entries SHALL apply to every headless Claude invocation the runner spawns, including workflow agent steps, inline repair agents, and `call_agent` children. They SHALL apply only to the spawned process and MUST NOT modify the runner's own environment or any user, project, or global Claude configuration. Interactive and autonomous-interactive Claude invocations SHALL NOT receive these entries.

#### Scenario: Headless agent step disables background tasks
- **WHEN** an autonomous headless Claude agent step is spawned and the runner's environment does not define `BASH_DEFAULT_TIMEOUT_MS`
- **THEN** the spawned process's environment contains `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` and `BASH_DEFAULT_TIMEOUT_MS=600000`

#### Scenario: Inherited default Bash timeout is preserved
- **WHEN** a headless Claude invocation is spawned and the runner's environment defines `BASH_DEFAULT_TIMEOUT_MS=900000`
- **THEN** the spawned process's environment contains `BASH_DEFAULT_TIMEOUT_MS=900000` and `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`

#### Scenario: Inherited background-task setting is overridden
- **WHEN** a headless Claude invocation is spawned and the runner's environment defines `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=0`
- **THEN** the spawned process's environment contains `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`

#### Scenario: Agent-call child on Claude
- **WHEN** a parent agent's `call_agent` request spawns a headless Claude child
- **THEN** the child process's environment contains `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`

#### Scenario: Interactive Claude step is unchanged
- **WHEN** an interactive or autonomous-interactive Claude agent step is spawned
- **THEN** the adapter contributes neither `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS` nor `BASH_DEFAULT_TIMEOUT_MS`

#### Scenario: No persistent configuration changes
- **WHEN** a headless Claude invocation is spawned with these entries
- **THEN** the runner's own environment and all user, project, and global Claude settings files are unchanged

#### Scenario: Other adapters are unchanged
- **WHEN** a headless Codex, Cursor, Copilot, or OpenCode invocation is spawned
- **THEN** the adapter contributes neither `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS` nor `BASH_DEFAULT_TIMEOUT_MS`

## MODIFIED Requirements

### Requirement: Agent-call tool provisioning

Every registered CLI adapter SHALL provision the process-local agent-call integration for an
interactive or autonomous parent invocation whose trusted invocation metadata enables `call_agent`.
The integration SHALL expose the Runner-owned MCP tools `call_agent`, `get_agent_call`, and
`cancel_agent_call`. Adapters MUST NOT inspect prompt text to determine availability. The integration
MUST NOT modify global or project agent configuration and MUST NOT be provisioned to an ordinary
step, a step with an omitted or empty tools list, or an agent started by `call_agent`. If an adapter
cannot prepare the required integration, Agent Runner SHALL fail the declared parent step with the
preparation cause before launching the parent CLI.

For a Runner-launched OpenCode invocation receiving the agent-call integration, Agent Runner SHALL replace inherited `OPENCODE_CONFIG_CONTENT`, `OPENCODE_PERMISSION`, and `OPENCODE_DISABLE_AUTOUPDATE` values with invocation-owned values rather than merging inherited values into the Runner-owned MCP and permission configuration. This replacement SHALL remain process-local and MUST NOT modify persistent global or project configuration.

#### Scenario: Registered adapter provisions the tool
- **WHEN** Agent Runner prepares a parent invocation whose trusted metadata enables `call_agent` through any registered CLI adapter
- **THEN** the invocation exposes `call_agent`, `get_agent_call`, and `cancel_agent_call`

#### Scenario: Prompt does not participate in adapter provisioning
- **WHEN** two otherwise equivalent invocations have the same enabled tools but different prompt text
- **THEN** the adapter gives them the same agent-call integration

#### Scenario: Enabled parent mode does not change availability
- **WHEN** Agent Runner prepares an interactive or autonomous parent invocation that enables `call_agent`
- **THEN** the adapter provisions the same three agent-call tools in either mode while preserving the mode's existing approval behavior

#### Scenario: Unenabled parent omits the integration
- **WHEN** Agent Runner prepares a parent invocation whose trusted metadata does not enable `call_agent`
- **THEN** the adapter does not provision the agent-call integration

#### Scenario: Called child omits the integration
- **WHEN** Agent Runner prepares an agent invocation started by `call_agent`
- **THEN** the adapter does not provision agent-call tools to that child

#### Scenario: User configuration remains unchanged
- **WHEN** an adapter provisions the agent-call integration for a parent invocation
- **THEN** no global or project agent configuration is created or modified

#### Scenario: OpenCode integration replaces inherited invocation configuration
- **WHEN** an enabled OpenCode parent inherits `OPENCODE_CONFIG_CONTENT`, `OPENCODE_PERMISSION`, or `OPENCODE_DISABLE_AUTOUPDATE`
- **THEN** the spawned parent receives the Runner-owned values for that invocation without merging the inherited values or modifying persistent user or project configuration

#### Scenario: Provisioning failure prevents launch
- **WHEN** an adapter cannot prepare a safe process-local agent-call integration
- **THEN** Agent Runner fails the parent step before launching its CLI and reports the preparation cause

### Requirement: Long-running tool controls

When a supported CLI exposes process-local control over MCP tool-execution timeouts, its adapter MAY configure the Runner-owned server so a generic short host default does not govern the agent-call tools, while preserving an explicit deadline configured by the user or requesting client. That configuration MUST NOT be required for a long child to complete: when a parent CLI's MCP client aborts a long tool request regardless of configuration, `call_agent` and `get_agent_call` SHALL return a non-terminal status with the `call_id` before that abort, and `get_agent_call` SHALL collect the later result. An adapter MUST NOT introduce a Runner-level child duration limit when the host exposes no such control. Timeout handling MUST remain isolated to the spawned parent and MUST NOT modify global or project configuration.

#### Scenario: Supported timeout control is applied process-locally
- **WHEN** an enabled parent uses a CLI with a supported MCP tool-execution timeout setting that its adapter configures (for example Codex `tool_timeout_sec` in the invocation's private Codex home, or the Copilot `--additional-mcp-config` server `timeout`)
- **THEN** the adapter configures the Runner-owned server for long-running agent-call tools without changing persistent user or project settings

#### Scenario: Adapter without timeout control adds no Runner deadline
- **WHEN** an enabled parent uses a CLI without a supported MCP tool-execution timeout setting
- **THEN** Agent Runner preserves the host's native behavior and introduces no fixed child duration limit of its own

#### Scenario: Host that aborts long tool requests still supports long children
- **WHEN** an enabled parent uses a CLI whose MCP client aborts a long tool request regardless of configuration (Cursor) and the child is still running
- **THEN** `call_agent` returns the `call_id` with a non-terminal status before the host abort, the child keeps running, and `get_agent_call` collects the later result
