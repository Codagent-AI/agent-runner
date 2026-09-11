## ADDED Requirements

### Requirement: Poll until terminal result

After a successful `call_agent` start, `codagent:call-agent` SHALL invoke `get_agent_call` with the returned `call_id` until the call is terminal. It MUST NOT treat a non-terminal status as success, MUST NOT start another child to collect the same result, and MUST NOT claim that child work completed while status is `accepted` or `running`. When the poll tool is missing after a start that returned a `call_id`, the skill SHALL report that as a blocker rather than waiting on `call_agent` or substituting another delegation path.

#### Scenario: Long child is collected by polling
- **WHEN** `call_agent` returns a `call_id` and the child is still running
- **THEN** the skill polls `get_agent_call` until it receives a terminal success or structured failure

#### Scenario: In-progress status is not success
- **WHEN** `get_agent_call` returns a non-terminal status
- **THEN** the skill does not report child findings or treat the call as complete

#### Scenario: Missing poll tool is a blocker
- **WHEN** `call_agent` returns a `call_id` but `get_agent_call` is not available
- **THEN** the skill reports the missing capability as a blocker and does not substitute another collection path

### Requirement: Explicit cancel through cancel_agent_call

When the caller or lead aborts an in-flight child without ending the parent step, `codagent:call-agent` SHALL invoke `cancel_agent_call` with that `call_id`. It MUST NOT rely on canceling a `call_agent` or `get_agent_call` MCP request to stop the child. After `cancel_agent_call` returns, the skill SHALL keep polling `get_agent_call` until the call is terminal. It MUST NOT treat a non-terminal cancel result as a freed in-flight slot or start another child until then.

#### Scenario: Lead aborts a running child
- **WHEN** the caller instructs the lead to stop the active child while keeping the parent step running
- **THEN** the skill invokes `cancel_agent_call` with the active `call_id`

#### Scenario: Cancel is not a freed slot
- **WHEN** `cancel_agent_call` returns while status is still `accepted` or `running`
- **THEN** the skill polls `get_agent_call` until the call is terminal and does not start another child yet

## MODIFIED Requirements

### Requirement: Safe single-target invocation

For each invocation, `codagent:call-agent` MUST call the Runner-owned `call_agent` tool with a
non-empty standalone prompt and exactly one target form: a profile through `agent` or a declared named
session through `session`. It MUST NOT send both targets, invent an unavailable target, or invoke more
children than the caller's explicit call budget permits. After `call_agent` returns a `call_id`, that
invocation is not complete until `get_agent_call` returns a terminal result for that `call_id`. A later
skill invocation MAY make another serial call when the enclosing workflow permits it and no child is
in flight.

#### Scenario: Fresh profile target
- **WHEN** the caller selects an available agent profile
- **THEN** the skill invokes exactly that profile with `agent` and omits `session`

#### Scenario: Named session target
- **WHEN** the caller selects an available declared named session
- **THEN** the skill invokes exactly that session with `session` and omits `agent`

#### Scenario: Start is not the terminal result
- **WHEN** `call_agent` returns a `call_id` and non-terminal status
- **THEN** the skill does not use that start result as the child's final response

#### Scenario: Caller budget bounds repeated use
- **WHEN** an enclosing workflow permits at most two child calls
- **THEN** repeated uses of the skill do not cause more than two `call_agent` starts

### Requirement: Clear unavailable-tool and failure handling

The skill MUST fail clearly when the Runner-owned `call_agent` tool is unavailable and MUST NOT
silently substitute shell execution, another subagent system, a collaboration mechanism, or any other
delegation path. When a start, poll, or cancel returns a structured rejection or failure, the skill
SHALL preserve the known failure category and context, SHALL NOT claim that child work completed, and
SHALL NOT silently retry unless the caller separately authorizes another call within its budget.

#### Scenario: Tool is unavailable
- **WHEN** the skill is invoked in a session that does not expose Runner-owned `call_agent`
- **THEN** it reports the missing capability as a blocker and invokes no substitute delegation
  mechanism

#### Scenario: Structured child failure
- **WHEN** `get_agent_call` returns a validation, execution, cancellation, transport, or oversized-result
  failure
- **THEN** the skill reports the available failure details honestly and does not fabricate findings

#### Scenario: Failure is not silently retried
- **WHEN** a call fails and the caller has not authorized another invocation
- **THEN** the skill returns control with the failure rather than making another `call_agent` start
