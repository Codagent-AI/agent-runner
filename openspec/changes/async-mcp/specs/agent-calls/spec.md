## ADDED Requirements

### Requirement: Agent-call job identity

After Agent Runner accepts a valid `call_agent` request, the `call_agent` MCP tool SHALL return a structured result that includes a stable `call_id` and a non-terminal status (`accepted` or `running`) without waiting for the child to finish. Validation, eligibility, target, safety, and concurrency rejections SHALL remain pre-acceptance structured errors with no `call_id` and MUST NOT spawn a child.

A retry of the same accepted `call_agent` request ID SHALL return that same `call_id` and MUST NOT spawn another child or wait for the existing child to finish.

#### Scenario: Accepted start returns a call id
- **WHEN** an authenticated `call_agent` request passes Runner validation and is accepted
- **THEN** the `call_agent` tool returns a `call_id` and a non-terminal status of `accepted` or `running` before the child reaches a terminal result

#### Scenario: Rejected start has no call id
- **WHEN** a `call_agent` request fails schema, eligibility, target, override, self-session, or concurrency validation
- **THEN** the tool returns a structured error with no `call_id` and does not spawn a child

#### Scenario: Idempotent start retry
- **WHEN** the same accepted `call_agent` request ID is retried while the child is still running
- **THEN** Agent Runner returns the original `call_id` without spawning another child or waiting for completion

### Requirement: Agent-call status retrieval

When the agent-call integration is provisioned, Agent Runner SHALL expose `get_agent_call`. The tool MUST require the `call_id` returned by `call_agent` for the same parent attempt. An unknown `call_id` SHALL return a structured error and MUST NOT spawn a child.

While the call is not terminal, `get_agent_call` SHALL return immediately with `call_id`, a non-terminal status (`accepted` or `running`), the requested target, and elapsed time, and MUST NOT include the child's final response or transcript. When the call is terminal, `get_agent_call` SHALL return immediately with `call_id`, a terminal status, and the same structured success or error that the blocking `call_agent` tool previously returned for that outcome. A later `get_agent_call` with that `call_id` SHALL return the cached terminal result.

`get_agent_call` MUST NOT count as a second in-flight agent call.

#### Scenario: In-progress poll is immediate
- **WHEN** the parent invokes `get_agent_call` with the `call_id` of a still-running child
- **THEN** the tool returns immediately with non-terminal status, target, and elapsed time and does not include the child's final response

#### Scenario: Terminal poll returns the child result
- **WHEN** the parent invokes `get_agent_call` after the child succeeds
- **THEN** the tool returns the child's final response and identifies the requested target

#### Scenario: Terminal failure is cached
- **WHEN** the child fails and the parent later invokes `get_agent_call` with that `call_id`
- **THEN** the tool returns the cached structured error without launching another child

#### Scenario: Unknown call id is rejected
- **WHEN** `get_agent_call` receives a `call_id` that does not belong to the active parent attempt
- **THEN** the tool returns a structured error and does not spawn a child

### Requirement: Explicit agent-call cancellation

When the agent-call integration is provisioned, Agent Runner SHALL expose `cancel_agent_call`. The tool MUST require the `call_id` of the call to cancel. Canceling a running call SHALL terminate the child, retain its terminal evidence, cache a canceled result for that `call_id`, release the in-flight slot, and keep the parent attempt active.

Canceling a call that is already terminal SHALL return the cached terminal result and MUST NOT start a new child or re-signal a finished process. An unknown `call_id` SHALL return a structured error. `cancel_agent_call` MUST NOT count as a second in-flight agent call.

#### Scenario: Parent cancels a running child
- **WHEN** the parent invokes `cancel_agent_call` with the `call_id` of a running child
- **THEN** Agent Runner terminates the child, caches a canceled result, and allows a later `call_agent` from the same parent attempt

#### Scenario: Cancel of a finished call is a no-op
- **WHEN** the parent invokes `cancel_agent_call` with a `call_id` whose child already succeeded or failed
- **THEN** the tool returns that cached terminal result and does not spawn or kill another process

#### Scenario: Unknown cancel id is rejected
- **WHEN** `cancel_agent_call` receives a `call_id` that does not belong to the active parent attempt
- **THEN** the tool returns a structured error and does not affect any child

## MODIFIED Requirements

### Requirement: Agent-call acceptance boundary

Agent Runner SHALL accept an agent call only after authenticating the request, validating its schema, confirming the parent is eligible to use `call_agent`, resolving and validating the target and invocation overrides, enforcing self-session and concurrency safety, and reserving the request ID. The call SHALL become accepted after those checks succeed and before Agent Runner attempts to launch the child CLI.

An invalid, ineligible, or distinct concurrent request SHALL be rejected before acceptance and MUST NOT create call execution evidence. A CLI launch failure after acceptance SHALL be a failed accepted call. The original `call_agent` start and an idempotent retry of the same request ID SHALL return that call's `call_id` without another launch attempt. `get_agent_call` with that `call_id` SHALL return the cached structured failure.

#### Scenario: Validated request is accepted before launch
- **WHEN** an authenticated agent-call request passes all Runner validation and safety checks and its request ID is reserved
- **THEN** Agent Runner accepts the call before attempting to launch the child CLI

#### Scenario: Invalid request remains rejected
- **WHEN** an agent-call request fails schema, eligibility, target, override, self-session, or concurrency validation
- **THEN** Agent Runner rejects it without creating call execution evidence

#### Scenario: CLI launch failure is an accepted failure
- **WHEN** an accepted call fails while launching its child CLI
- **THEN** `call_agent` and a retry with the same request ID return the same `call_id` without another launch attempt, and `get_agent_call` returns the cached structured failure

### Requirement: Agent-call tool availability

Agent Runner SHALL expose the process-local agent-call tools `call_agent`, `get_agent_call`, and `cancel_agent_call` to an interactive or autonomous workflow agent step if and only if that step statically declares `tools: [call_agent]`. Agent Runner MUST derive availability from the validated declaration rather than any authored, interpolated, engine-enriched, system, child, or later conversational prompt text. An omitted or empty tools list SHALL provide no agent-call integration. An agent started by `call_agent` MUST NOT receive the tools regardless of its prompt, profile, or parent's declaration. Eligibility failures SHALL explain that `call_agent` was not enabled for the active step declaration and MUST NOT instruct the user to add prompt text.

#### Scenario: Interactive enabled parent receives the tools
- **WHEN** Agent Runner starts an interactive agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`, `get_agent_call`, and `cancel_agent_call`

#### Scenario: Autonomous enabled parent receives the tools
- **WHEN** Agent Runner starts an autonomous agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`, `get_agent_call`, and `cancel_agent_call`

#### Scenario: Declaration works without prompt token
- **WHEN** an agent step declares `tools: [call_agent]` and its prompt does not contain `call_agent`
- **THEN** Agent Runner provisions the agent-call tools

#### Scenario: Prompt token alone does not enable the tools
- **WHEN** an agent step's prompt contains `call_agent` but the step omits `tools`
- **THEN** the agent does not receive the agent-call tools

#### Scenario: Autonomous enabled parent receives pre-authorized access
- **WHEN** Agent Runner provisions agent-call tools for an autonomous agent step that declares `call_agent`
- **THEN** only the Runner-owned `call_agent`, `get_agent_call`, and `cancel_agent_call` tools are pre-authorized and their invocation does not wait for interactive approval

#### Scenario: Interactive enabled parent uses normal tool approval
- **WHEN** Agent Runner provisions agent-call tools for an interactive agent step that declares `call_agent`
- **THEN** invocation follows that CLI's normal MCP tool-approval flow

#### Scenario: Called child cannot delegate recursively
- **WHEN** `call_agent` starts a child agent whose supplied prompt mentions `call_agent`
- **THEN** the child does not receive the agent-call tools

#### Scenario: Ineligible error cites declaration
- **WHEN** a parent without a `call_agent` declaration submits an agent-call request
- **THEN** Agent Runner rejects it with guidance about the active step declaration rather than prompt
  contents

### Requirement: Synchronous autonomous execution

Agent Runner SHALL execute a valid agent call as a nested child of the parent attempt until the child succeeds, fails, or is canceled. The child SHALL keep running even when no MCP `tools/call` remains open. The child SHALL run autonomous-headless through the normal profile, system-prompt, permission, and CLI-adapter resolution paths regardless of the target profile's default mode.

The called child SHALL receive its resolved profile system prompt and the supplied call prompt. It SHALL NOT receive workflow-engine step enrichment because an agent call is not a workflow step.

#### Scenario: Interactive profile is forced headless
- **WHEN** a call targets a profile whose default mode is interactive
- **THEN** Agent Runner executes the child in autonomous-headless mode

#### Scenario: Profile and invocation settings are resolved
- **WHEN** a call targets a profile with model, effort, and system-prompt settings and supplies valid overrides
- **THEN** the child receives the resolved profile settings with the call's overrides applied

#### Scenario: Child continues without an open MCP wait
- **WHEN** a valid child call is running and the `call_agent` MCP request has already returned
- **THEN** Agent Runner keeps the child running until it succeeds, fails, or is canceled

#### Scenario: Call omits workflow-step enrichment
- **WHEN** a valid call executes under a workflow engine that enriches ordinary agent steps
- **THEN** the called child receives its profile system prompt and supplied call prompt without workflow-step enrichment

### Requirement: Long-running MCP execution

Agent Runner MUST NOT impose a fixed duration limit on a valid agent call. The process-local MCP tools `call_agent`, `get_agent_call`, and `cancel_agent_call` SHALL complete without waiting for the child to finish and MUST NOT depend on host MCP tool-execution timeout configuration or progress notifications to keep the child alive. When a supported host exposes a process-local MCP tool-execution timeout control, adapters MAY still raise or disable a generic short default; that configuration MUST NOT be required for a long child to complete. Progress notifications MUST NOT be treated as a substitute for start/poll/cancel or for client-side cancellation.

#### Scenario: Start does not wait for the child
- **WHEN** a parent invokes `call_agent` for a child that will run longer than the host MCP `tools/call` wait
- **THEN** `call_agent` returns a `call_id` before that host wait expires and the child remains running

#### Scenario: Poll does not wait for the child
- **WHEN** a parent invokes `get_agent_call` while the child is still running
- **THEN** the poll returns immediately with non-terminal status even if the host has a short MCP `tools/call` wait

#### Scenario: Host timeout settings are not required
- **WHEN** an enabled parent uses a CLI without a supported MCP tool-execution timeout control
- **THEN** a long child can still complete through `get_agent_call` after `call_agent` has returned

### Requirement: Call safety

Agent Runner MUST reject a named-session call whose resolved CLI session is the parent's active CLI session. Each parent attempt SHALL have at most one agent call in flight; a concurrent distinct `call_agent` request MUST be rejected rather than queued or used to cancel the active child. The structured `call_in_progress` error SHALL identify the active `call_id`, target, and elapsed time and SHALL instruct the caller to poll `get_agent_call` or cancel `cancel_agent_call` before starting another call. Invoking `get_agent_call` or `cancel_agent_call` for the active `call_id` SHALL NOT be rejected as a concurrent call.

#### Scenario: Parent session cannot call itself
- **WHEN** a named-session target resolves to the parent's active CLI session
- **THEN** Agent Runner rejects the call before spawning or resuming a child

#### Scenario: Concurrent second start is rejected
- **WHEN** a parent attempt submits a second distinct `call_agent` request while its first call is still running
- **THEN** Agent Runner returns an instructive `call_in_progress` error that includes the active `call_id` without queueing, canceling, or spawning another child

#### Scenario: Poll during an active call is allowed
- **WHEN** a parent attempt invokes `get_agent_call` with the active `call_id` while that child is running
- **THEN** Agent Runner returns status for that call and does not treat the poll as a concurrent start

#### Scenario: Later call is accepted
- **WHEN** a prior call from the parent attempt has finished
- **THEN** the parent can submit another valid `call_agent`

### Requirement: Results and failures

A successful call SHALL make a structured tool result available through `get_agent_call` containing the child's final response and the requested target kind and name. The result MUST NOT expose the raw CLI session ID, usage, or cost. A validation or child-execution failure SHALL return a structured tool error without automatically failing the parent step or retrying the call.

The control channel SHALL retain its 16 MiB message limit. When an otherwise successful child's encoded tool result exceeds that limit, Agent Runner SHALL return a structured oversized-result error from `get_agent_call`, keep the parent attempt active, and retain the child's persisted output and execution evidence for inspection. Agent Runner SHALL NOT stream or chunk oversized results in this change.

#### Scenario: Named-session success result
- **WHEN** a child targeted through `session: implementor-session` succeeds
- **THEN** `get_agent_call` contains the child's final response and identifies the named-session target

#### Scenario: Profile success result
- **WHEN** a child targeted through `agent: implementor` succeeds
- **THEN** `get_agent_call` contains the child's final response and identifies the profile target

#### Scenario: Child failure returns control to parent
- **WHEN** the child process fails
- **THEN** `get_agent_call` returns a structured error, keeps the parent attempt active, and performs no automatic retry

#### Scenario: Parent explicitly retries
- **WHEN** a call returns a failure and the parent submits a later valid `call_agent`
- **THEN** Agent Runner treats the later call as a separate invocation

#### Scenario: Oversized successful response retains evidence
- **WHEN** a child succeeds but its encoded tool result exceeds the 16 MiB control-message limit
- **THEN** `get_agent_call` returns a structured oversized-result error, keeps the parent active, and retains the child's persisted output and execution evidence

### Requirement: Cancellation propagation

When the parent attempt is canceled, stopped, or exits while an agent call is running, Agent Runner SHALL terminate the called child and MUST NOT allow it to continue independently. The parent SHALL retain the outcome dictated by its existing cancellation, stop, or exit behavior.

After acceptance, the child SHALL be leased to the parent attempt, not to a live MCP request or a single authenticated control connection. When the client cancels a `call_agent`, `get_agent_call`, or `cancel_agent_call` MCP request, the MCP bridge exits, or that control connection is lost, Agent Runner MUST NOT cancel the child solely for that reason. The parent attempt SHALL still retrieve or cancel that call through a later authenticated request using the same `call_id`.

#### Scenario: Parent cancellation terminates child
- **WHEN** the parent attempt is canceled while its child is running
- **THEN** Agent Runner terminates the child and preserves the parent's cancellation outcome

#### Scenario: Parent exit leaves no orphan
- **WHEN** the parent process exits while its child is running
- **THEN** Agent Runner terminates the child and no called-agent process remains running independently

#### Scenario: MCP start timeout does not kill the child
- **WHEN** the client times out or cancels the `call_agent` MCP request after the call is accepted
- **THEN** Agent Runner leaves the child running and a later `get_agent_call` with that `call_id` can observe it

#### Scenario: MCP poll timeout does not kill the child
- **WHEN** the client times out or cancels an in-flight `get_agent_call` request
- **THEN** Agent Runner leaves the child running and a later `get_agent_call` with that `call_id` can observe it

#### Scenario: Bridge restart does not kill an accepted child
- **WHEN** the MCP bridge exits or loses its authenticated control connection after a call is accepted and before the child is terminal
- **THEN** Agent Runner leaves the child running for the parent attempt and does not treat the disconnect as cancellation
