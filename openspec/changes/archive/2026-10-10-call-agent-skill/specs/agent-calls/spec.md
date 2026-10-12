## MODIFIED Requirements

### Requirement: Agent-call tool availability

Agent Runner SHALL expose `call_agent` to an interactive or autonomous workflow agent step if and
only if that step statically declares `tools: [call_agent]`. Agent Runner MUST derive availability
from the validated declaration rather than any authored, interpolated, engine-enriched, system, child,
or later conversational prompt text. An omitted or empty tools list SHALL provide no agent-call
integration. An agent started by `call_agent` MUST NOT receive the tool regardless of its prompt,
profile, or parent's declaration. Eligibility failures SHALL explain that `call_agent` was not enabled
for the active step declaration and MUST NOT instruct the user to add prompt text.

#### Scenario: Interactive declared parent receives the tool
- **WHEN** Agent Runner starts an interactive agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Autonomous declared parent receives the tool
- **WHEN** Agent Runner starts an autonomous agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Declaration works without prompt token
- **WHEN** an agent step declares `tools: [call_agent]` and its prompt does not contain `call_agent`
- **THEN** Agent Runner provisions the tool

#### Scenario: Prompt token alone does not enable the tool
- **WHEN** an agent step's prompt contains `call_agent` but the step omits `tools`
- **THEN** Agent Runner does not provision the tool

#### Scenario: Empty tools receive no integration
- **WHEN** an agent step declares `tools: []`
- **THEN** Agent Runner does not provision the tool

#### Scenario: Autonomous declared parent receives pre-authorized access
- **WHEN** Agent Runner provisions `call_agent` for an autonomous agent step that declares it
- **THEN** only the Runner-owned `call_agent` tool is pre-authorized and its invocation does not wait
  for interactive approval

#### Scenario: Interactive declared parent uses normal tool approval
- **WHEN** Agent Runner provisions `call_agent` for an interactive agent step that declares it
- **THEN** invocation follows that CLI's normal MCP tool-approval flow

#### Scenario: Called child cannot delegate recursively
- **WHEN** `call_agent` starts a child agent whose supplied prompt mentions `call_agent`
- **THEN** the child does not receive the `call_agent` tool

#### Scenario: Ineligible error cites declaration
- **WHEN** a parent without a `call_agent` declaration submits an agent-call request
- **THEN** Agent Runner rejects it with guidance about the active step declaration rather than prompt
  contents
