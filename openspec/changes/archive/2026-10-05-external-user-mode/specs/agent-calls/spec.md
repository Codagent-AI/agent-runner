## MODIFIED Requirements

### Requirement: Agent-call tool availability

Agent Runner SHALL expose `call_agent` to an interactive or autonomous workflow agent step if and
only if that step statically declares `tools: [call_agent]`. Agent Runner MUST derive availability
from the validated declaration rather than any authored, interpolated, engine-enriched, system, child,
or later conversational prompt text. An omitted or empty tools list SHALL provide no agent-call
integration. An agent started by `call_agent` MUST NOT receive the tool regardless of its prompt,
profile, or parent's declaration. Eligibility failures SHALL explain that `call_agent` was not enabled
for the active step declaration and MUST NOT instruct the user to add prompt text.

An interactive parent running in external-user mode SHALL receive the same pre-authorized access as
an autonomous parent, because its headless turns cannot show an approval prompt. Its access SHALL
remain available on every resumed turn of the step.

#### Scenario: Interactive enabled parent receives the tool
- **WHEN** Agent Runner starts an interactive agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Autonomous enabled parent receives the tool
- **WHEN** Agent Runner starts an autonomous agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Declaration works without prompt token
- **WHEN** an agent step declares `tools: [call_agent]` and its prompt does not contain `call_agent`
- **THEN** Agent Runner provisions the tool

#### Scenario: Prompt token alone does not enable the tool
- **WHEN** an agent step's prompt contains `call_agent` but the step omits `tools`
- **THEN** the agent does not receive the `call_agent` tool

#### Scenario: Autonomous enabled parent receives pre-authorized access
- **WHEN** Agent Runner provisions `call_agent` for an autonomous agent step that declares it
- **THEN** only the Runner-owned `call_agent` tool is pre-authorized and its invocation does not wait for interactive approval

#### Scenario: Interactive enabled parent uses normal tool approval
- **WHEN** Agent Runner provisions `call_agent` for an interactive agent step that declares it, outside external-user mode
- **THEN** invocation follows that CLI's normal MCP tool-approval flow

#### Scenario: External-user interactive parent receives pre-authorized access
- **WHEN** Agent Runner provisions `call_agent` for an interactive agent step that declares it, in external-user mode
- **THEN** on every turn of the step, only the Runner-owned agent-call tools are pre-authorized, and invocation does not wait for approval

#### Scenario: Called child cannot delegate recursively
- **WHEN** `call_agent` starts a child agent whose supplied prompt mentions `call_agent`
- **THEN** the child does not receive the `call_agent` tool

#### Scenario: Ineligible error cites declaration
- **WHEN** a parent without a `call_agent` declaration submits an agent-call request
- **THEN** Agent Runner rejects it with guidance about the active step declaration rather than prompt
  contents
