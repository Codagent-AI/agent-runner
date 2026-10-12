## ADDED Requirements

### Requirement: Static Runner-owned tools on agent steps

An agent step MAY declare a static YAML sequence named `tools`. The only supported initial entry SHALL
be the exact string `call_agent`. An omitted `tools` field or an explicitly empty sequence on an agent
step SHALL enable no Runner-owned tools. Tool entries MUST NOT be interpolated. Unknown names,
duplicate names, and scalar declarations MUST fail workflow loading with an error that identifies the
invalid `tools` declaration.

#### Scenario: Agent step declares call_agent
- **WHEN** an agent step declares `tools: [call_agent]`
- **THEN** the loaded step records `call_agent` as its enabled Runner-owned tool

#### Scenario: Agent step omits tools
- **WHEN** an agent step has no `tools` field
- **THEN** the loaded step has no enabled Runner-owned tools

#### Scenario: Agent step declares an empty sequence
- **WHEN** an agent step declares `tools: []`
- **THEN** the loaded step has no enabled Runner-owned tools

#### Scenario: Unknown tool is rejected
- **WHEN** an agent step declares a tool name other than the exact supported value `call_agent`
- **THEN** workflow loading fails with an error identifying the unknown tool

#### Scenario: Duplicate tool is rejected
- **WHEN** an agent step declares `tools: [call_agent, call_agent]`
- **THEN** workflow loading fails with an error identifying the duplicate tool

#### Scenario: Scalar tool declaration is rejected
- **WHEN** an agent step declares `tools: call_agent`
- **THEN** workflow loading fails because `tools` must be a sequence

#### Scenario: Tool declaration is not interpolated
- **WHEN** an agent step declares a placeholder or other non-literal value in `tools`
- **THEN** workflow loading rejects it as an unknown tool rather than resolving workflow parameters

### Requirement: Tools field is agent-only

The `tools` field SHALL be valid only on an agent step. Any explicit `tools` field on a shell, script,
UI, loop, group, or sub-workflow step MUST fail workflow loading, including when its sequence is empty.
The same constraint SHALL apply to steps nested inside loops and groups.

#### Scenario: Non-agent step declares call_agent
- **WHEN** a non-agent step declares `tools: [call_agent]`
- **THEN** workflow loading fails with an error stating that `tools` is only allowed on agent steps

#### Scenario: Non-agent step declares an empty sequence
- **WHEN** a non-agent step explicitly declares `tools: []`
- **THEN** workflow loading fails with an error stating that `tools` is only allowed on agent steps

#### Scenario: Nested non-agent declaration is rejected
- **WHEN** a loop or group contains a non-agent child with an explicit `tools` field
- **THEN** recursive step validation rejects that child declaration
