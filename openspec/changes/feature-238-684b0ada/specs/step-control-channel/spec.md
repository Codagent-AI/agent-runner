## MODIFIED Requirements

### Requirement: Private per-run control endpoint

Before the first parent agent step that receives runner control integration spawns, Agent Runner SHALL lazily create a local Unix socket in a user-private directory and retain it for the run. The directory SHALL be accessible only to the local user and the socket SHALL be readable and writable only by that user. The run directory SHALL point to the socket. Endpoint creation failure SHALL fail the step before spawn; normal run exit SHALL close and unlink it; stale cleanup SHALL require proof of the run lock. Interactive and autonomous-headless parent agent processes SHALL receive the control context required by their enabled runner tools. For autonomous parents, agent-call control eligibility SHALL be derived solely from the validated tool declaration. Agents started by `call_agent` MUST NOT receive usable parent control context.

#### Scenario: Child receives control context
- **WHEN** an interactive parent agent starts
- **THEN** it receives `AGENT_RUNNER_CONTROL_SOCKET`, `AGENT_RUNNER_RUN_ID`, `AGENT_RUNNER_STEP_ID`, `AGENT_RUNNER_ATTEMPT_ID`, and `AGENT_RUNNER_CONTROL_TOKEN`

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

#### Scenario: Endpoint is private to the local user
- **WHEN** the control endpoint exists for a run
- **THEN** its directory and socket are accessible only to the local user who started the run

## REMOVED Requirements

### Requirement: Fresh authenticated attempt

**Reason**: Its scenario "Lost agent-call client cancels leased execution" no longer matches the code. An accepted agent call is leased to the parent attempt, so losing the client connection does not cancel the child. A MODIFIED block cannot drop that scenario name, so the requirement is replaced.

**Migration**: Replaced by "Fresh authenticated attempt credential", which keeps the same authentication rules and states that a lost client connection leaves the child running.

## ADDED Requirements

### Requirement: Fresh authenticated attempt credential

Every parent agent step attempt that receives runner control integration SHALL receive a fresh attempt ID and credential. The server SHALL accept events and requests only for the active run, step, and credential; agent-call, `get_agent_call`, and `cancel_agent_call` requests SHALL also carry the active attempt ID. Malformed, stale, unknown, or inactive events and requests SHALL be rejected and audited without advancing the workflow. Authenticating and admitting an agent-call request for Runner processing MUST NOT by itself mark the call accepted; agent-call acceptance occurs only after the Runner validation and request-ID reservation boundary. A repeated accepted completion request ID SHALL return its original acknowledgement idempotently. A repeated accepted `call_agent` request ID SHALL return the same call, identified by its original `call_id`, without spawning another child. After acceptance, an agent call SHALL be leased to the parent attempt, not to a single authenticated client connection; loss of that connection before a terminal result is delivered MUST NOT cancel the child. Concluding or replacing the parent attempt SHALL end that lease. The credential remains usable for committed-turn evidence until the attempt concludes.

#### Scenario: Current completion is accepted
- **WHEN** a well-formed completion request carries the active attempt's credential
- **THEN** the server accepts and acknowledges it

#### Scenario: Current agent call is admitted for processing
- **WHEN** a well-formed agent-call request carries the active parent attempt's credential and attempt ID
- **THEN** the server authenticates and admits the request for Runner validation without marking the call accepted

#### Scenario: Stale completion is rejected
- **WHEN** a request carries an earlier attempt's credential
- **THEN** the server rejects and audits it without changing workflow state

#### Scenario: Malformed request is rejected
- **WHEN** a connection to the control endpoint delivers a payload that is not a single well-formed control message
- **THEN** the server rejects and audits it without changing workflow state

#### Scenario: Acknowledgement retry is idempotent
- **WHEN** the same accepted completion request ID is retried after a lost response
- **THEN** the server returns the original successful acknowledgement

#### Scenario: Agent-call retry is idempotent
- **WHEN** the same accepted `call_agent` request ID is retried while its child is running or after it finishes
- **THEN** the server returns the original call, with its original `call_id` and current or terminal result, without spawning another child

#### Scenario: Lost agent-call client does not cancel the child
- **WHEN** the authenticated client connection for an accepted in-progress call closes before a terminal result is delivered
- **THEN** the server leaves the child running for the parent attempt and a later authenticated `get_agent_call` with that `call_id` can observe it

#### Scenario: Committed-turn evidence accepted after completion
- **WHEN** a committed-turn signal carrying the current attempt's credential arrives after the completion request was accepted but before the step concludes
- **THEN** the server accepts it as turn-durability evidence rather than rejecting the credential as consumed

### Requirement: Completion event semantics

Completion SHALL be success-only: a completion request carries no outcome, and an accepted completion can end the step only as `success` after durability is confirmed or as `failed` through the durability-failure path. After a completion is accepted for an attempt, further completion requests for that attempt, including ones with a new request ID, SHALL receive the originally accepted acknowledgement and MUST NOT deliver a second completion or otherwise change workflow state. A completion request that arrives while no step attempt is active SHALL be rejected and audited. A completion request for an active attempt that is not completion-eligible, such as an autonomous-headless parent that holds control context only for agent calls, SHALL be rejected with an explanation that autonomous steps finish when the agent exits.

#### Scenario: Duplicate completion is ignored
- **WHEN** a second completion request with a different request ID arrives after a completion was accepted for the current attempt
- **THEN** the server returns the originally accepted acknowledgement and the workflow state is unaffected

#### Scenario: Completion with no active step
- **WHEN** a completion request arrives while no step attempt is active
- **THEN** the server rejects and audits it and the workflow state is unaffected

#### Scenario: Completion is unavailable to an agent-call-only attempt
- **WHEN** an autonomous-headless parent that declares `call_agent` runs `agent-runner step complete`
- **THEN** the server rejects the request, explains that autonomous steps finish when the agent exits, and delivers no completion

### Requirement: Completion instruction injection

Agent Runner SHALL append completion instructions to the prompt for every interactive and autonomous-interactive agent step. The instructions SHALL tell the agent to run the absolute-path `step complete` command with its shell tool, with no extra arguments, as the final action of the response once the step is complete. Autonomous-headless agent steps SHALL NOT receive completion instructions because they finish when the agent process exits.

#### Scenario: Interactive step receives completion instructions
- **WHEN** Agent Runner builds the prompt for an interactive agent step
- **THEN** the prompt includes instructions to signal completion by running the absolute-path completion command

#### Scenario: Autonomous-interactive step receives completion instructions
- **WHEN** Agent Runner builds the prompt for an autonomous-interactive agent step
- **THEN** the prompt includes the same completion instructions

#### Scenario: Autonomous-headless step receives no completion instructions
- **WHEN** Agent Runner builds the prompt for an autonomous-headless agent step
- **THEN** the prompt does not include completion instructions

### Requirement: Universal completion surface

For every interactive or autonomous-interactive agent step backed by a CLI whose adapter supports that invocation context, the agent SHALL have a working way to signal completion through the control channel, and no step SHALL depend on CLI-specific terminal output to advance. An adapter that rejects interactive invocation (currently OpenCode) SHALL fail such a step before spawn with an explanation instead of offering a completion path. For autonomous-interactive steps, an adapter that pre-approves the exact completion command (Claude and Cursor) SHALL let it run without human approval. When the Cursor adapter, outside `yolo` permission mode, detects a user or project deny rule that overrides its completion pre-approval, the step SHALL fail before spawn with an explanation instead of waiting for approval.

#### Scenario: Any registered CLI can complete a step
- **WHEN** an interactive agent step runs with any registered CLI that supports interactive invocation and the agent follows the injected completion instructions
- **THEN** the completion request is delivered through the control channel and the workflow advances with outcome `success`

#### Scenario: Pre-approved autonomous-interactive completion does not wait for approval
- **WHEN** an autonomous-interactive step runs on an adapter that pre-approves the exact completion command and its agent runs that command
- **THEN** the command runs without waiting for human permission approval

#### Scenario: CLI without interactive support fails before spawn
- **WHEN** an interactive or autonomous-interactive agent step resolves to OpenCode
- **THEN** the step fails before spawn with an explanation that OpenCode does not support interactive steps, and no completion path is offered

#### Scenario: Blocked autonomous-interactive completion fails early
- **WHEN** an autonomous-interactive Cursor step starts outside `yolo` permission mode and a user or project Cursor deny rule covers the completion command
- **THEN** the step fails before spawn with an explanation naming the deny rule instead of hanging while awaiting approval
