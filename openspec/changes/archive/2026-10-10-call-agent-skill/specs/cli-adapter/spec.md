## MODIFIED Requirements

### Requirement: Agent-call tool provisioning

Every registered CLI adapter SHALL provision the process-local `call_agent` integration for an
interactive or autonomous parent invocation whose trusted invocation metadata enables `call_agent`.
Adapters MUST NOT inspect prompt text to determine availability. The integration MUST NOT modify
global or project agent configuration and MUST NOT be provisioned to an ordinary step, a step with an
omitted or empty tools list, or an agent started by `call_agent`. If an adapter cannot prepare the
required integration, Agent Runner SHALL fail the declared parent step with the preparation cause
before launching the parent CLI.

For a Runner-launched OpenCode invocation receiving the `call_agent` integration, Agent Runner SHALL
replace inherited `OPENCODE_CONFIG_CONTENT`, `OPENCODE_PERMISSION`, and
`OPENCODE_DISABLE_AUTOUPDATE` values with invocation-owned values rather than merging inherited
values into the Runner-owned MCP and permission configuration. This replacement SHALL remain
process-local and MUST NOT modify persistent global or project configuration.

#### Scenario: Registered adapter provisions the declared tool
- **WHEN** Agent Runner prepares a parent invocation whose trusted metadata enables `call_agent`
  through any registered CLI adapter
- **THEN** the invocation exposes the `call_agent` tool

#### Scenario: Prompt does not participate in adapter provisioning
- **WHEN** two otherwise equivalent invocations have the same enabled tools but different prompt text
- **THEN** the adapter gives them the same agent-call integration

#### Scenario: Enabled parent mode does not change availability
- **WHEN** Agent Runner prepares an interactive or autonomous parent invocation that enables
  `call_agent`
- **THEN** the adapter provisions the same capability while preserving the mode's existing approval
  behavior

#### Scenario: Unenabled parent omits the integration
- **WHEN** Agent Runner prepares a parent invocation whose trusted metadata does not enable
  `call_agent`
- **THEN** the adapter does not provision the agent-call integration

#### Scenario: Called child omits the integration
- **WHEN** Agent Runner prepares an agent invocation started by `call_agent`
- **THEN** the adapter does not provision `call_agent` to that child

#### Scenario: User configuration remains unchanged
- **WHEN** an adapter provisions `call_agent` for a parent invocation
- **THEN** no global or project agent configuration is created or modified

#### Scenario: OpenCode integration replaces inherited invocation configuration
- **WHEN** an enabled OpenCode parent inherits `OPENCODE_CONFIG_CONTENT`, `OPENCODE_PERMISSION`, or
  `OPENCODE_DISABLE_AUTOUPDATE`
- **THEN** the spawned parent receives the Runner-owned values for that invocation without merging
  inherited values or modifying persistent user or project configuration

#### Scenario: Provisioning failure prevents launch
- **WHEN** an adapter cannot prepare a safe process-local integration for a parent that enables
  `call_agent`
- **THEN** Agent Runner fails the parent step before launching its CLI and reports the preparation
  cause
