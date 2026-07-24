# step-control-channel Specification

## Purpose
Define authenticated out-of-band completion and turn-durability behavior for interactive agent steps.
## Requirements
### Requirement: Private per-run control endpoint

Before the first parent agent step that receives runner control integration spawns, Agent Runner SHALL lazily create a local Unix socket in a user-private directory and retain it for the run. The run directory SHALL point to the socket. Endpoint creation failure SHALL fail the step before spawn; normal run exit SHALL close and unlink it; stale cleanup SHALL require proof of the run lock. Interactive and autonomous-headless parent agent processes SHALL receive the control context required by their enabled runner tools. Agents started by `call_agent` MUST NOT receive usable parent control context.

#### Scenario: Interactive parent receives control context
- **WHEN** an interactive parent agent starts
- **THEN** it receives `AGENT_RUNNER_CONTROL_SOCKET`, `AGENT_RUNNER_RUN_ID`, `AGENT_RUNNER_STEP_ID`, and `AGENT_RUNNER_CONTROL_TOKEN`

#### Scenario: Autonomous parent receives control context
- **WHEN** an autonomous-headless parent agent whose authored prompt contains `call_agent` starts
- **THEN** it receives the control context required to invoke `call_agent`

#### Scenario: Ineligible autonomous parent receives no control context
- **WHEN** an autonomous-headless parent agent whose authored prompt does not contain `call_agent` and that has no other enabled runner tool starts
- **THEN** it does not receive runner control context

#### Scenario: Called child receives no parent control context
- **WHEN** Agent Runner starts a child through `call_agent`
- **THEN** the child does not receive usable control context for the parent attempt

#### Scenario: Endpoint creation fails
- **WHEN** the private endpoint cannot be created
- **THEN** the step fails before the CLI is spawned

### Requirement: Fresh authenticated attempt

Every parent agent step attempt that receives runner control integration SHALL receive a fresh credential. The server SHALL accept events and requests only for the active run, step, attempt, and credential. Malformed, stale, unknown, or inactive events and requests SHALL be rejected and audited without advancing the workflow. Authenticating and admitting an agent-call request for Runner processing MUST NOT by itself mark the call accepted; agent-call acceptance occurs only after the Runner validation and request-ID reservation boundary. A repeated accepted completion request ID SHALL return its original acknowledgement idempotently. A repeated accepted agent-call request ID SHALL return the same eventual or completed tool result without spawning another child. An accepted agent call SHALL be leased to its authenticated client connection; loss of that connection before delivery of a result SHALL cancel the child and cache its canceled terminal result. The credential remains usable for committed-turn evidence until the attempt concludes.

#### Scenario: Current completion is accepted
- **WHEN** a well-formed completion request carries the active attempt's credential
- **THEN** the server accepts and acknowledges it

#### Scenario: Current agent call is admitted for processing
- **WHEN** a well-formed agent-call request carries the active parent attempt's credential
- **THEN** the server authenticates and admits the request for Runner validation without marking the call accepted

#### Scenario: Stale completion is rejected
- **WHEN** a request carries an earlier attempt's credential
- **THEN** the server rejects and audits it without changing workflow state

#### Scenario: Completion acknowledgement retry is idempotent
- **WHEN** the same accepted completion request ID is retried after a lost response
- **THEN** the server returns the original successful acknowledgement

#### Scenario: Agent-call retry is idempotent
- **WHEN** the same accepted agent-call request ID is retried while its child is running or after it finishes
- **THEN** the server returns the original call's eventual or completed result without spawning another child

#### Scenario: Lost agent-call client cancels leased execution
- **WHEN** the authenticated client connection for an accepted in-progress call closes before receiving a result
- **THEN** the server cancels the called child and retains the canceled result for an idempotent retry

### Requirement: In-session completion client

The binary SHALL expose `agent-runner step complete`. It SHALL read endpoint and credential data only from the inherited control environment, send a completion event with a unique request ID, and exit nonzero with guidance when that environment is absent. It SHALL NOT accept run, step, socket, or token overrides on the command line. Prompt instructions SHALL use the absolute executable path. CLI-native completion commands SHALL invoke this same client.

#### Scenario: Agent completes the active step
- **WHEN** the agent runs the exact absolute-path client inside its interactive session
- **THEN** the active attempt receives an authenticated completion request

#### Scenario: Command runs outside a session
- **WHEN** the control environment is absent
- **THEN** the client exits nonzero and does not target any run

### Requirement: Acknowledgement precedes termination

Completion acceptance SHALL be an intermediate state, not success. The server SHALL capture the adapter's accept-time durability checkpoint, record `completion_requested` and `completion_acknowledged`, and return the acknowledgement before sending any termination signal. Early committed-turn hook events that arrive before acceptance SHALL be acknowledged and ignored rather than failing the agent turn.

#### Scenario: Tool call returns before shutdown
- **WHEN** a valid completion request is accepted
- **THEN** its client receives a success acknowledgement before CLI termination begins

#### Scenario: Hook fires before completion acceptance
- **WHEN** a native turn hook sends `turn_committed` with no accepted completion
- **THEN** the server acknowledges and ignores it without failing or advancing the step

### Requirement: Semantic turn durability

After acknowledgement, Agent Runner SHALL wait up to 30 seconds of active runtime for semantic evidence that a completed assistant turn was recorded after the checkpoint. Evidence MAY be an authenticated native post-turn event or an adapter probe of an explicit completed-turn record; modification time, file quiescence, and fixed sleeps are insufficient. The clock SHALL pause while the child is suspended.

On confirmation, the step SHALL become `success` and the CLI group SHALL be terminated gracefully. On timeout or probe failure, Agent Runner SHALL record `durability_failure` with CLI, session ID, bound, inspected artifact, and cause; terminate the CLI; record `failed`; and stop the workflow. If the child exits after acceptance, store inspection SHALL continue for the remaining bound. A resumed workflow SHALL retry with a new credential.

#### Scenario: Completing turn is resumable
- **WHEN** an interactive step succeeds through the control channel and a later step resumes its session
- **THEN** the resumed session contains the turn that invoked completion

#### Scenario: Durability cannot be proven
- **WHEN** the active-runtime bound expires without semantic committed-turn evidence
- **THEN** the step fails visibly rather than advancing on an assumption

#### Scenario: Child exits during durability wait
- **WHEN** the CLI exits after acceptance but before confirmation
- **THEN** Agent Runner continues inspecting its native store until confirmation or the remaining bound expires

### Requirement: Completion integration preserves supervision

Adapters MAY pre-approve only the exact absolute-path completion command with fixed `step complete` arguments. A CLI that cannot express this safely SHALL keep its normal interactive approval prompt rather than broaden permissions. Process-local native commands and hooks SHALL be injected for the spawned process without requiring global user installation or project-file changes. Failure to prepare a required integration SHALL fail before spawn.

#### Scenario: Unrelated commands remain supervised
- **WHEN** an interactive agent runs a command other than exact completion
- **THEN** the CLI's normal approval behavior is unchanged

#### Scenario: Native command is process-local
- **WHEN** Agent Runner adds an adapter-native completion command or hook
- **THEN** it is available to the spawned CLI without global installation or project mutation

