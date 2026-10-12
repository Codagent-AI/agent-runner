## MODIFIED Requirements

### Requirement: Private per-run control endpoint

Before the first parent agent step that receives Runner control integration spawns, Agent Runner SHALL
lazily create a local Unix socket in a user-private directory and retain it for the run. The run
directory SHALL point to the socket. Endpoint creation failure SHALL fail the step before spawn;
normal run exit SHALL close and unlink it; stale cleanup SHALL require proof of the run lock.
Interactive and autonomous-headless parent agent processes SHALL receive the control context required
by their enabled Runner-owned tools. For autonomous parents, `call_agent` control eligibility SHALL be
derived solely from the validated tool declaration. Agents started by `call_agent` MUST NOT receive
usable parent control context.

#### Scenario: Interactive parent receives existing completion control context
- **WHEN** an interactive parent agent starts
- **THEN** it receives the control context required by the existing interactive completion behavior

#### Scenario: Declared autonomous parent receives control context
- **WHEN** an autonomous-headless parent agent declares `tools: [call_agent]`
- **THEN** it receives the control context required to invoke `call_agent`

#### Scenario: Prompt token does not create autonomous control context
- **WHEN** an autonomous-headless parent mentions `call_agent` in its prompt but does not declare it
  and has no other enabled Runner-owned tool
- **THEN** it does not receive Runner control context

#### Scenario: Omitted and empty tools do not create autonomous control context
- **WHEN** an autonomous-headless parent omits `tools` or declares `tools: []` and has no other
  enabled Runner-owned tool
- **THEN** it does not receive Runner control context

#### Scenario: Called child receives no parent control context
- **WHEN** Agent Runner starts a child through `call_agent`
- **THEN** the child does not receive usable control context for the parent attempt

#### Scenario: Endpoint creation fails
- **WHEN** the private endpoint cannot be created
- **THEN** the step fails before the CLI is spawned
