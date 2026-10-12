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

#### Scenario: Registered adapter provisions the tools
- **WHEN** Agent Runner prepares a parent invocation whose trusted metadata enables `call_agent` through any registered CLI adapter
- **THEN** the invocation exposes `call_agent`, `get_agent_call`, and `cancel_agent_call`

#### Scenario: Prompt does not participate in adapter provisioning
- **WHEN** two otherwise equivalent invocations have the same enabled tools but different prompt text
- **THEN** the adapter gives them the same agent-call integration

#### Scenario: Enabled parent mode does not change availability
- **WHEN** Agent Runner prepares an interactive or autonomous parent invocation that enables `call_agent`
- **THEN** the adapter provisions the same three agent-call tools in either mode

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

When a supported CLI exposes process-local control over MCP tool-execution timeouts, its adapter MAY configure the Runner-owned server so a generic short host default does not govern the agent-call tools, while preserving an explicit deadline configured by the user or requesting client. That configuration MUST NOT be required for a long child to complete through start/poll. An adapter MUST NOT introduce a Runner-level child duration limit when the host exposes no such control. Timeout handling MUST remain isolated to the spawned parent and MUST NOT modify global or project configuration.

#### Scenario: Supported timeout control remains process-local
- **WHEN** an enabled parent uses a CLI with a supported MCP tool-execution timeout setting
- **THEN** the adapter may configure the Runner-owned server without changing persistent user or project settings

#### Scenario: Adapter without timeout control still supports long children
- **WHEN** an enabled parent uses a CLI without a supported MCP tool-execution timeout setting
- **THEN** Agent Runner still returns `call_agent` without waiting for the child and allows `get_agent_call` to collect the later result
