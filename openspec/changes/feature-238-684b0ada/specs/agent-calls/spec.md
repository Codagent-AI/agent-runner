## ADDED Requirements

### Requirement: Agent-call job identity

After Agent Runner accepts a valid `call_agent` request, the accepted call SHALL have a stable `call_id`. `call_agent` SHALL wait for the child up to the parent's wait budget and then return either the call's terminal result or that `call_id` with a non-terminal status (`accepted` or `running`). Validation, eligibility, target, override, self-session, and concurrency rejections SHALL remain pre-acceptance structured errors, SHALL NOT assign a `call_id` to the rejected request, and MUST NOT spawn a child.

The wait budget SHALL be unbounded by default, so `call_agent` waits for the child and the parent cannot end its turn on a call it never collected. A parent CLI whose MCP client aborts a long `tools/call` SHALL instead have a positive budget shorter than that abort, and its parent collects the outcome by polling `get_agent_call`.

A retry of the same accepted `call_agent` request ID SHALL return that same call's `call_id` and current or terminal result and MUST NOT spawn another child.

#### Scenario: Waiting parent receives the child result
- **WHEN** an accepted `call_agent` request belongs to a parent whose wait budget is unbounded
- **THEN** the tool holds the request open until the call is terminal and returns the `call_id`, terminal status, and the child's structured result or error

#### Scenario: Bounded parent receives a call id
- **WHEN** an accepted `call_agent` request belongs to a parent with a positive wait budget and the child is still running when that budget expires
- **THEN** the tool returns the `call_id` with a non-terminal status of `accepted` or `running` and the child keeps running

#### Scenario: Rejected start has no call id
- **WHEN** a `call_agent` request fails schema, eligibility, target, override, self-session, or concurrency validation
- **THEN** the tool returns a structured error, assigns no `call_id` to that request, and does not spawn a child

#### Scenario: Idempotent start retry
- **WHEN** the same accepted `call_agent` request ID is retried while the child is still running
- **THEN** Agent Runner returns the original `call_id` without spawning another child

### Requirement: Agent-call status retrieval

When the agent-call integration is provisioned, Agent Runner SHALL expose `get_agent_call`. The tool MUST require the `call_id` returned by `call_agent` for the same parent attempt. An unknown `call_id` SHALL return a structured `unknown_call` error and MUST NOT spawn a child.

`get_agent_call` SHALL wait on the same budget as `call_agent`. When the call is terminal, it SHALL return the `call_id`, a terminal status (`succeeded`, `failed`, or `canceled`), and the same structured success or error that `call_agent` would have returned for that outcome. When the child is still running at the end of the budget, it SHALL return the `call_id`, a non-terminal status (`accepted` or `running`), the requested target, and elapsed time, and MUST NOT include the child's final response or transcript. A later `get_agent_call` with that `call_id` SHALL return the cached terminal result.

`get_agent_call` MUST NOT count as a second in-flight agent call.

#### Scenario: In-progress poll returns a snapshot at the budget
- **WHEN** a parent with a positive wait budget invokes `get_agent_call` with the `call_id` of a child that is still running when the budget expires
- **THEN** the tool returns non-terminal status, target, and elapsed time and does not include the child's final response

#### Scenario: Terminal poll returns the child result
- **WHEN** the parent invokes `get_agent_call` after the child succeeds
- **THEN** the tool returns the child's final response and identifies the requested target

#### Scenario: Terminal failure is cached
- **WHEN** the child fails and the parent later invokes `get_agent_call` with that `call_id`
- **THEN** the tool returns the cached structured error without launching another child

#### Scenario: Unknown call id is rejected
- **WHEN** `get_agent_call` receives a `call_id` that does not belong to the active parent attempt
- **THEN** the tool returns a structured error and does not spawn a child

### Requirement: Uncollected agent calls fail the parent step

When a parent agent attempt would otherwise succeed but ends while an accepted call is still non-terminal, Agent Runner SHALL record that step as failed and SHALL explain that the child was canceled and its result never collected. Agent Runner MUST NOT record such a step as a success, because attempt teardown kills the child and later steps would otherwise consume evidence the child never wrote. This SHALL apply to ordinary agent steps and to interactive steps in external-user mode.

#### Scenario: Parent ends with a call still running
- **WHEN** a parent agent process exits successfully while an accepted call has not reached a terminal status
- **THEN** the step outcome is failed and its evidence names the uncollected `call_id`

#### Scenario: Collected calls leave the outcome alone
- **WHEN** a parent agent process exits successfully after every accepted call has reached a terminal status
- **THEN** the step keeps its own outcome

### Requirement: Explicit agent-call cancellation

When the agent-call integration is provisioned, Agent Runner SHALL expose `cancel_agent_call`. The tool MUST require the `call_id` of the call to cancel. Canceling a running call SHALL terminate the child, retain its terminal evidence, cache a canceled result for that `call_id`, release the in-flight slot, and keep the parent attempt active. `cancel_agent_call` SHALL wait for the canceled call to become terminal and return that result; if the cancel request itself ends first, it SHALL return the call's current non-terminal snapshot.

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

### Requirement: Parent session discovery excludes called children

Agent Runner SHALL record each agent-call child's CLI session ID as soon as it is known, including while the child is still running when launch output or chat metadata already identifies it. When Agent Runner discovers the parent CLI session after spawn, during live durability confirmation or after exit, it SHALL pass those child session IDs to the parent adapter's session discovery as exclusions. The Cursor adapter's interactive session discovery, which scans Cursor's local chat store for the invocation workspace, SHALL NOT return an excluded ID, and same-workspace chats that are not nested children SHALL still make that discovery fail closed when more than one match remains. This requirement does not oblige other adapters' session discovery to honor the exclusions.

#### Scenario: Exclusions are forwarded to parent discovery
- **WHEN** an agent-call child's CLI session ID is known and Agent Runner discovers the parent's CLI session
- **THEN** the parent adapter's session discovery receives that child session ID as an exclusion

#### Scenario: Nested child chat does not hide the parent
- **WHEN** an interactive Cursor parent and a nested agent-call child both have Cursor chats in the same workspace
- **AND** Agent Runner discovers the parent session after the child session ID is known
- **THEN** discovery returns the parent session rather than an empty ID

#### Scenario: In-flight child chat does not hide the parent
- **WHEN** an interactive Cursor parent discovers its session while a nested agent-call child is still running
- **AND** the child's session ID is already known from launch output or chat metadata
- **THEN** discovery excludes that child session and returns the parent session

#### Scenario: Unrelated extra chats remain ambiguous
- **WHEN** two matching Cursor chats exist for an interactive Cursor parent's workspace and neither is a nested agent-call child
- **THEN** parent session discovery returns the empty string

### Requirement: Agent-call cancellation propagation

When the parent attempt is canceled, stopped, or exits while an agent call is running, Agent Runner SHALL terminate the called child and MUST NOT allow it to continue independently. The parent SHALL retain the outcome dictated by its existing cancellation, stop, or exit behavior.

After acceptance, the child SHALL be leased to the parent attempt, not to a live MCP request or a single authenticated control connection. When the client cancels or times out a `call_agent` or `get_agent_call` MCP request, the MCP bridge exits, or that control connection is lost, Agent Runner MUST NOT cancel the child for that reason. The parent attempt SHALL still retrieve or cancel that call through a later authenticated request using the same `call_id`. Only parent-attempt teardown or `cancel_agent_call` SHALL cancel an accepted child.

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

## MODIFIED Requirements

### Requirement: Agent-call tool availability

Agent Runner SHALL expose the process-local agent-call tools `call_agent`, `get_agent_call`, and
`cancel_agent_call` to an interactive or autonomous workflow agent step if and only if that step
statically declares `tools: [call_agent]`. That one declaration SHALL provision all three tools;
`get_agent_call` and `cancel_agent_call` are not step-model tool names. Agent Runner MUST derive
availability from the validated declaration rather than any authored, interpolated, engine-enriched,
system, child, or later conversational prompt text. An omitted or empty tools list SHALL provide no
agent-call integration. An agent started by `call_agent` MUST NOT receive the tools regardless of its
prompt, profile, or parent's declaration. Eligibility failures SHALL explain that `call_agent` was not
enabled for the active step declaration and MUST NOT instruct the user to add prompt text.

An interactive parent running in external-user mode SHALL receive the same pre-authorized access as
an autonomous parent, because its headless turns cannot show an approval prompt. Its access SHALL
remain available on every resumed turn of the step.

#### Scenario: Interactive enabled parent receives the tool
- **WHEN** Agent Runner starts an interactive agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`, `get_agent_call`, and `cancel_agent_call`

#### Scenario: Autonomous enabled parent receives the tool
- **WHEN** Agent Runner starts an autonomous agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`, `get_agent_call`, and `cancel_agent_call`

#### Scenario: Declaration works without prompt token
- **WHEN** an agent step declares `tools: [call_agent]` and its prompt does not contain `call_agent`
- **THEN** Agent Runner provisions the agent-call tools

#### Scenario: Prompt token alone does not enable the tool
- **WHEN** an agent step's prompt contains `call_agent` but the step omits `tools`
- **THEN** the agent does not receive the agent-call tools

#### Scenario: Empty tools receive no integration
- **WHEN** an agent step declares `tools: []`
- **THEN** Agent Runner does not provision the agent-call tools

#### Scenario: Autonomous enabled parent receives pre-authorized access
- **WHEN** Agent Runner provisions the agent-call tools for an autonomous agent step that declares `call_agent`
- **THEN** only the Runner-owned `call_agent`, `get_agent_call`, and `cancel_agent_call` tools are pre-authorized and their invocation does not wait for interactive approval

#### Scenario: Interactive enabled parent uses normal tool approval
- **WHEN** Agent Runner provisions the agent-call tools for an interactive agent step that declares `call_agent`, outside external-user mode
- **THEN** invocation follows that CLI's normal MCP tool-approval flow

#### Scenario: External-user interactive parent receives pre-authorized access
- **WHEN** Agent Runner provisions `call_agent` for an interactive agent step that declares it, in external-user mode
- **THEN** on every turn of the step, only the Runner-owned agent-call tools are pre-authorized, and invocation does not wait for approval

#### Scenario: Called child cannot delegate recursively
- **WHEN** `call_agent` starts a child agent whose supplied prompt mentions `call_agent`
- **THEN** the child does not receive the agent-call tools

#### Scenario: Ineligible error cites declaration
- **WHEN** a parent without a `call_agent` declaration submits an agent-call request
- **THEN** Agent Runner rejects it with guidance about the active step declaration rather than prompt
  contents

### Requirement: Agent-call acceptance boundary

Agent Runner SHALL accept an agent call only after authenticating the request, validating its schema, confirming the parent is eligible to use `call_agent`, resolving and validating the target and invocation overrides, enforcing self-session and concurrency safety, and reserving the request ID. The call SHALL become accepted after those checks succeed and before Agent Runner attempts to launch the child CLI.

An invalid, ineligible, or distinct concurrent request SHALL be rejected before acceptance and MUST NOT create call execution evidence. A CLI launch failure after acceptance SHALL be a failed accepted call. The original `call_agent` start, any idempotent retry of the same request ID, and `get_agent_call` for that call's `call_id` SHALL report the same `call_id` and the cached structured failure without another launch attempt.

#### Scenario: Validated request is accepted before launch
- **WHEN** an authenticated agent-call request passes all Runner validation and safety checks and its request ID is reserved
- **THEN** Agent Runner accepts the call before attempting to launch the child CLI

#### Scenario: Invalid request remains rejected
- **WHEN** an agent-call request fails schema, eligibility, target, override, self-session, or concurrency validation
- **THEN** Agent Runner rejects it without creating call execution evidence

#### Scenario: CLI launch failure is an accepted failure
- **WHEN** an accepted call fails while launching its child CLI
- **THEN** a retry with the same request ID returns the same `call_id` without another launch attempt, and `get_agent_call` returns the cached structured failure

### Requirement: Synchronous autonomous execution

Agent Runner SHALL execute a valid agent call as a nested child of the parent attempt until the child succeeds, fails, or is canceled. With the default unbounded wait budget, the `call_agent` tool invocation SHALL remain pending until the child finishes. The child SHALL keep running even when no MCP `tools/call` remains open. The child SHALL run autonomous-headless through the normal profile, system-prompt, permission, and CLI-adapter resolution paths regardless of the target profile's default mode.

The called child SHALL receive its resolved profile system prompt and the supplied call prompt. It SHALL NOT receive workflow-engine step enrichment because an agent call is not a workflow step.

#### Scenario: Interactive profile is forced headless
- **WHEN** a call targets a profile whose default mode is interactive
- **THEN** Agent Runner executes the child in autonomous-headless mode

#### Scenario: Profile and invocation settings are resolved
- **WHEN** a call targets a profile with model, effort, and system-prompt settings and supplies valid overrides
- **THEN** the child receives the resolved profile settings with the call's overrides applied

#### Scenario: Parent waits for child completion
- **WHEN** a valid child call is running and the parent's wait budget is unbounded
- **THEN** the parent's `call_agent` invocation remains pending until the child succeeds, fails, or is canceled

#### Scenario: Child continues without an open MCP wait
- **WHEN** a valid child call is running and the `call_agent` MCP request has already returned
- **THEN** Agent Runner keeps the child running until it succeeds, fails, or is canceled

#### Scenario: Call omits workflow-step enrichment
- **WHEN** a valid call executes under a workflow engine that enriches ordinary agent steps
- **THEN** the called child receives its profile system prompt and supplied call prompt without workflow-step enrichment

### Requirement: Long-running MCP execution

Agent Runner MUST NOT impose a fixed duration limit on a valid agent call. After acceptance, a child's survival MUST NOT depend on any MCP request staying open, on host MCP tool-execution timeout configuration, or on progress notifications. When a supported host exposes a process-local MCP tool-execution timeout control, the adapter SHALL raise that timeout for the Runner-owned server so the host's generic short default does not end a waiting `call_agent` request for a long child. Where a host enforces an unconfigurable `tools/call` abort, its parents SHALL receive a wait budget shorter than that abort and reach the result by polling `get_agent_call`. When an MCP client supplies a progress token on `call_agent`, the bridge SHALL emit rate-limited progress notifications while that request remains open. Progress notifications MUST NOT be treated as a substitute for either the raised timeout or polling.

#### Scenario: Configurable host timeout does not bound the call
- **WHEN** a supported host exposes a process-local MCP tool-execution timeout control
- **THEN** Agent Runner provisions the agent-call tools so the host's generic short default does not terminate a waiting `call_agent` request for an otherwise active child

#### Scenario: Requested progress is reported
- **WHEN** an MCP client invokes `call_agent` with a progress token and the request remains open while the child is active
- **THEN** the bridge emits rate-limited progress notifications until that request returns

#### Scenario: Child outlives an aborted request
- **WHEN** a host aborts an open `call_agent` request for a child that runs longer than its `tools/call` wait
- **THEN** the child remains running and the parent can still reach its result with `get_agent_call`

#### Scenario: Bounded poll returns before the host abort
- **WHEN** a parent whose host enforces a short `tools/call` wait invokes `get_agent_call` while the child is still running
- **THEN** the poll returns a non-terminal status before that host wait expires

### Requirement: Call safety

Agent Runner MUST reject a named-session call whose resolved CLI session is the parent's active CLI session. Each parent attempt SHALL have at most one agent call in flight; a concurrent distinct `call_agent` request MUST be rejected rather than queued or used to cancel the active child. The structured `call_in_progress` error SHALL identify the active call's `call_id`, target, and elapsed time and SHALL instruct the caller to poll `get_agent_call` or cancel with `cancel_agent_call` before starting another call. Invoking `get_agent_call` or `cancel_agent_call` for the active `call_id` SHALL NOT be rejected as a concurrent call.

#### Scenario: Parent session cannot call itself
- **WHEN** a named-session target resolves to the parent's active CLI session
- **THEN** Agent Runner rejects the call before spawning or resuming a child

#### Scenario: Concurrent second call is rejected
- **WHEN** a parent attempt submits a second distinct `call_agent` request while its first call is still running
- **THEN** Agent Runner returns an instructive `call_in_progress` error that includes the active `call_id` without queueing, canceling, or spawning another child

#### Scenario: Poll during an active call is allowed
- **WHEN** a parent attempt invokes `get_agent_call` with the active `call_id` while that child is running
- **THEN** Agent Runner returns status for that call and does not treat the poll as a concurrent start

#### Scenario: Later call is accepted
- **WHEN** a prior call from the parent attempt has finished
- **THEN** the parent can submit another valid `call_agent`

### Requirement: Results and failures

A successful call SHALL produce a structured tool result containing the `call_id`, a `succeeded` status, the child's final response, and the requested target kind and name. That result SHALL be returned by `call_agent` when the call becomes terminal within its wait budget and by `get_agent_call` otherwise. The result MUST NOT expose the raw CLI session ID, usage, or cost. A validation or child-execution failure SHALL return a structured tool error without automatically failing the parent step or retrying the call.

The control channel SHALL retain its 16 MiB message limit. When an otherwise successful child's encoded tool result exceeds that limit, Agent Runner SHALL return a structured oversized-result error in place of the result, keep the parent attempt active, and retain the child's persisted output and execution evidence for inspection. Agent Runner SHALL NOT stream or chunk oversized results.

#### Scenario: Named-session success result
- **WHEN** a child targeted through `session: implementor-session` succeeds
- **THEN** the terminal tool result contains the child's final response and identifies the named-session target

#### Scenario: Profile success result
- **WHEN** a child targeted through `agent: implementor` succeeds
- **THEN** the terminal tool result contains the child's final response and identifies the profile target

#### Scenario: Child failure returns control to parent
- **WHEN** the child process fails
- **THEN** the terminal tool result is a structured error, the parent attempt stays active, and no automatic retry occurs

#### Scenario: Parent explicitly retries
- **WHEN** a call returns a failure and the parent submits a later valid `call_agent`
- **THEN** Agent Runner treats the later call as a separate invocation

#### Scenario: Oversized successful response retains evidence
- **WHEN** a child succeeds but its encoded tool result exceeds the 16 MiB control-message limit
- **THEN** the terminal tool result is a structured oversized-result error, the parent stays active, and the child's persisted output and execution evidence are retained

## REMOVED Requirements

### Requirement: Cancellation propagation

**Reason**: Its scenarios "MCP cancellation releases the call slot" and "Bridge connection loss cancels the child" describe the old request-leased model. Accepted children are now leased to the parent attempt, and dropping an MCP request or control connection no longer cancels them (`internal/exec/agent_call.go` `awaitAgentCallResult`, `childParentContext`).

**Migration**: Replaced by "Agent-call cancellation propagation" (parent teardown still kills the child; MCP or connection loss does not) and "Explicit agent-call cancellation" (`cancel_agent_call`).
