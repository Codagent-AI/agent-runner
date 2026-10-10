## MODIFIED Requirements

### Requirement: Private per-run control endpoint

Before the first parent agent step that receives runner control integration spawns, Agent Runner SHALL lazily create a local Unix socket in a user-private directory and retain it for the run. The run directory SHALL point to the socket. Endpoint creation failure SHALL fail the step before spawn; normal run exit SHALL close and unlink it; stale cleanup SHALL require proof of the run lock. Interactive and autonomous-headless parent agent processes SHALL receive the control context required by their enabled runner tools. For autonomous parents, agent-call control eligibility SHALL be derived solely from the validated tool declaration. Agents started by `call_agent` MUST NOT receive usable parent control context.

#### Scenario: Child receives control context
- **WHEN** an interactive parent agent starts
- **THEN** it receives `AGENT_RUNNER_CONTROL_SOCKET`, `AGENT_RUNNER_RUN_ID`, `AGENT_RUNNER_STEP_ID`, and `AGENT_RUNNER_CONTROL_TOKEN`

#### Scenario: Autonomous parent receives control context
- **WHEN** an autonomous-headless parent agent declaring `tools: [call_agent]` starts
- **THEN** it receives the control context required to invoke `call_agent`, `get_agent_call`, and `cancel_agent_call`

#### Scenario: Prompt token does not create autonomous control context
- **WHEN** an autonomous-headless parent mentions `call_agent` in its prompt but does not declare it and has no other enabled runner tool
- **THEN** it does not receive runner control context

#### Scenario: Omitted and empty tools do not create autonomous control context
- **WHEN** an autonomous-headless parent omits `tools` or declares `tools: []` and has no other enabled runner tool
- **THEN** it does not receive runner control context

#### Scenario: Called child receives no parent control context
- **WHEN** Agent Runner starts a child through `call_agent`
- **THEN** the child does not receive usable control context for the parent attempt

#### Scenario: Endpoint creation fails
- **WHEN** the private endpoint cannot be created
- **THEN** the step fails before the CLI is spawned

### Requirement: Fresh authenticated attempt

Every parent agent step attempt that receives runner control integration SHALL receive a fresh credential. The server SHALL accept events and requests only for the active run, step, attempt, and credential. Malformed, stale, unknown, or inactive events and requests SHALL be rejected and audited without advancing the workflow. Authenticating and admitting an agent-call request for Runner processing MUST NOT by itself mark the call accepted; agent-call acceptance occurs only after the Runner validation and request-ID reservation boundary. A repeated accepted completion request ID SHALL return its original acknowledgement idempotently. A repeated accepted `call_agent` request ID SHALL return the same `call_id` without spawning another child and without waiting for the child to finish. After acceptance, an agent call SHALL be leased to the parent attempt, not to a single authenticated client connection; loss of that connection before a terminal result is delivered MUST NOT cancel the child. The credential remains usable for committed-turn evidence until the attempt concludes.

#### Scenario: Current completion is accepted
- **WHEN** a well-formed completion request carries the active attempt's credential
- **THEN** the server accepts and acknowledges it

#### Scenario: Current agent call is admitted for processing
- **WHEN** a well-formed agent-call request carries the active parent attempt's credential
- **THEN** the server authenticates and admits the request for Runner validation without marking the call accepted

#### Scenario: Stale completion is rejected
- **WHEN** a request carries an earlier attempt's credential
- **THEN** the server rejects and audits it without changing workflow state

#### Scenario: Acknowledgement retry is idempotent
- **WHEN** the same accepted completion request ID is retried after a lost response
- **THEN** the server returns the original successful acknowledgement

#### Scenario: Agent-call start retry is idempotent
- **WHEN** the same accepted `call_agent` request ID is retried while its child is running or after it finishes
- **THEN** the server returns the original `call_id` without spawning another child or waiting for completion

#### Scenario: Lost agent-call client does not cancel the child
- **WHEN** the authenticated client connection for an accepted in-progress call closes before a terminal result is delivered
- **THEN** the server leaves the child running for the parent attempt and a later authenticated `get_agent_call` with that `call_id` can observe it
