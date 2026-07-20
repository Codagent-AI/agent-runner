## ADDED Requirements

### Requirement: Agent-call tool provisioning

Every registered CLI adapter SHALL provision the `call_agent` tool for an interactive or autonomous parent agent invocation whose workflow-authored prompt template contains the literal `call_agent` substring, using process-local configuration. The integration MUST NOT modify global or project agent configuration and MUST NOT be provisioned to an ordinary step without that substring or to an agent started by `call_agent`. If an adapter cannot prepare the required integration, Agent Runner SHALL fail the enabled parent step with the preparation cause before launching the parent CLI.

#### Scenario: Registered adapter provisions the tool
- **WHEN** Agent Runner prepares a parent agent invocation whose authored prompt contains `call_agent` through any registered CLI adapter
- **THEN** the invocation exposes the `call_agent` tool

#### Scenario: Enabled parent mode does not change availability
- **WHEN** Agent Runner prepares an interactive or autonomous parent agent invocation whose authored prompt contains `call_agent`
- **THEN** the adapter provisions the same `call_agent` capability in either mode

#### Scenario: Unenabled parent omits the integration
- **WHEN** Agent Runner prepares an ordinary parent whose authored prompt does not contain `call_agent`
- **THEN** the adapter does not provision the `call_agent` integration

#### Scenario: Called child omits the integration
- **WHEN** Agent Runner prepares an agent invocation started by `call_agent`
- **THEN** the adapter does not provision `call_agent` to that child

#### Scenario: User configuration remains unchanged
- **WHEN** an adapter provisions `call_agent` for a parent invocation
- **THEN** no global or project agent configuration is created or modified

#### Scenario: Provisioning failure prevents launch
- **WHEN** an adapter cannot prepare a safe process-local `call_agent` integration
- **THEN** Agent Runner fails the parent step before launching its CLI and reports the preparation cause

### Requirement: Long-running tool controls

When a supported CLI exposes process-local control over MCP tool-execution timeouts, its adapter SHALL configure the Runner-owned server so a generic short host default does not govern `call_agent`, while preserving an explicit deadline configured by the user or requesting client. An adapter MUST NOT introduce a Runner-level call duration limit when the host exposes no such control. Timeout handling MUST remain isolated to the spawned parent and MUST NOT modify global or project configuration.

#### Scenario: Supported timeout control is applied process-locally
- **WHEN** an enabled parent uses a CLI with a supported MCP tool-execution timeout setting
- **THEN** the adapter configures the Runner-owned server for long-running calls without changing persistent user or project settings

#### Scenario: Adapter without timeout control adds no Runner deadline
- **WHEN** an enabled parent uses a CLI without a supported MCP tool-execution timeout setting
- **THEN** Agent Runner preserves the host's native behavior and introduces no fixed call duration limit of its own
