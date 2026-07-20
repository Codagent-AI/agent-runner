## ADDED Requirements

### Requirement: Agent-call tool availability

Agent Runner SHALL expose a `call_agent` tool to an interactive or autonomous agent step when that step's workflow-authored prompt template contains the literal, case-sensitive substring `call_agent`. Agent Runner SHALL evaluate this condition before prompt interpolation and workflow-engine enrichment. A step whose authored prompt does not contain that substring SHALL NOT receive the tool. An agent started by `call_agent` MUST NOT receive the tool regardless of its prompt.

#### Scenario: Interactive enabled parent receives the tool
- **WHEN** Agent Runner starts an interactive agent step whose authored prompt contains `call_agent`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Autonomous enabled parent receives the tool
- **WHEN** Agent Runner starts an autonomous agent step whose authored prompt contains `call_agent`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Prompt without token receives no tool
- **WHEN** Agent Runner starts an ordinary agent step whose authored prompt does not contain `call_agent`
- **THEN** the agent does not receive the `call_agent` tool

#### Scenario: Autonomous enabled parent receives pre-authorized access
- **WHEN** Agent Runner provisions `call_agent` for an autonomous agent step whose authored prompt contains `call_agent`
- **THEN** only the Runner-owned `call_agent` tool is pre-authorized and its invocation does not wait for interactive approval

#### Scenario: Interactive enabled parent uses normal tool approval
- **WHEN** Agent Runner provisions `call_agent` for an interactive agent step whose authored prompt contains `call_agent`
- **THEN** invocation follows that CLI's normal MCP tool-approval flow

#### Scenario: Called child cannot delegate recursively
- **WHEN** `call_agent` starts a child agent
- **THEN** the child does not receive the `call_agent` tool

### Requirement: Invocation fields and valid forms

A `call_agent` invocation MUST include a `prompt` and exactly one target: `agent: <profile>` for a fresh profile-backed session, or `session: <declared-name>` for a workflow-declared named session. An `agent` target SHALL accept `cli`, `model`, and `workdir` as optional overrides. A `session` target SHALL accept `model` and `workdir` as optional overrides and SHALL use the CLI resolved from its declared profile. The `session` field is exclusively for declared named sessions; `new`, `resume`, and `inherit` are not valid agent-call targets. Any invocation outside these forms SHALL be rejected without spawning a child.

#### Scenario: Agent profile creates a fresh session
- **WHEN** a valid call specifies `prompt` and `agent: implementor`
- **THEN** Agent Runner starts a fresh session using the `implementor` profile

#### Scenario: Declared named session is selected
- **WHEN** a valid call specifies `prompt` and `session: implementor-session`
- **THEN** Agent Runner targets the workflow-declared `implementor-session` without requiring an `agent` field

#### Scenario: Fresh-profile invocation overrides are applied
- **WHEN** a valid `agent`-targeted call includes `cli`, `model`, or `workdir`
- **THEN** Agent Runner applies those fields using the corresponding agent-step override semantics

#### Scenario: Named-session invocation overrides are applied
- **WHEN** a valid `session`-targeted call includes `model` or `workdir`
- **THEN** Agent Runner applies those fields while retaining the CLI resolved from the named session's declared profile

### Requirement: Synchronous autonomous execution

Agent Runner SHALL execute a valid agent call synchronously and SHALL keep the parent tool invocation pending until the child finishes. The child SHALL run autonomous-headless through the normal profile, system-prompt, permission, and CLI-adapter resolution paths regardless of the target profile's default mode.

The called child SHALL receive its resolved profile system prompt and the supplied call prompt. It SHALL NOT receive workflow-engine step enrichment because an agent call is not a workflow step.

#### Scenario: Interactive profile is forced headless
- **WHEN** a call targets a profile whose default mode is interactive
- **THEN** Agent Runner executes the child in autonomous-headless mode

#### Scenario: Profile and invocation settings are resolved
- **WHEN** a call targets a profile with model, effort, and system-prompt settings and supplies valid overrides
- **THEN** the child receives the resolved profile settings with the call's overrides applied

#### Scenario: Parent waits for child completion
- **WHEN** a valid child call is running
- **THEN** the parent tool invocation remains pending until the child succeeds, fails, or is canceled

#### Scenario: Call omits workflow-step enrichment
- **WHEN** a valid call executes under a workflow engine that enriches ordinary agent steps
- **THEN** the called child receives its profile system prompt and supplied call prompt without workflow-step enrichment

### Requirement: Long-running MCP execution

Agent Runner MUST NOT impose a fixed duration limit on a valid agent call. The process-local MCP integration SHALL avoid allowing a generic short host tool timeout to govern called-agent execution when the host exposes a supported timeout control, while preserving an explicit deadline configured by the user or requesting client. When an MCP client supplies a progress token, the bridge SHALL emit rate-limited progress notifications while the child remains active. Progress notifications MUST NOT be treated as a substitute for client-side timeout configuration or cancellation.

#### Scenario: Configurable host timeout does not bound the call
- **WHEN** a supported host exposes a process-local MCP tool-execution timeout control
- **THEN** Agent Runner provisions `call_agent` so the host's generic short default does not terminate an otherwise active child

#### Scenario: Requested progress is reported
- **WHEN** an MCP client invokes `call_agent` with a progress token and the child remains active
- **THEN** the bridge emits rate-limited progress notifications until the call reaches a terminal result

### Requirement: Working-directory behavior

The called child SHALL run in the same worktree as its parent. When `workdir` is omitted, the child SHALL use the parent's effective working directory. When `workdir` is supplied, Agent Runner SHALL apply the existing agent-step validation and resolution rules.

#### Scenario: Omitted workdir uses parent directory
- **WHEN** a parent running from an effective working directory invokes `call_agent` without `workdir`
- **THEN** the child runs from the parent's effective working directory

#### Scenario: Explicit workdir is honored
- **WHEN** a valid call supplies `workdir: frontend`
- **THEN** the child runs from `frontend` according to existing agent-step workdir semantics

### Requirement: Session behavior

An `agent` target SHALL start a fresh session for every call and MUST NOT add that session to the named-session map. A `session` target SHALL use the existing run-scoped named-session declaration and map, creating the session on first use or resuming the stored CLI session on subsequent use.

#### Scenario: Repeated profile calls remain fresh
- **WHEN** two calls independently target `agent: implementor`
- **THEN** Agent Runner starts two distinct CLI sessions and stores neither as a named session

#### Scenario: Named session is created on first call
- **WHEN** a call targets a declared named session with no stored CLI session
- **THEN** Agent Runner creates the CLI session and stores its ID under the declared name

#### Scenario: Workflow-created named session is reused
- **WHEN** an ordinary workflow step already created the targeted named session
- **THEN** the call resumes that stored CLI session

#### Scenario: Call-created named session is reused
- **WHEN** an earlier call created the targeted named session
- **THEN** a later call resumes that stored CLI session

### Requirement: Call safety

Agent Runner MUST reject a named-session call whose resolved CLI session is the parent's active CLI session. Each parent attempt SHALL have at most one agent call in flight; a concurrent distinct request MUST be rejected rather than queued or used to cancel the active child. The structured `call_in_progress` error SHALL identify the active target and elapsed time and SHALL instruct the caller that agent calls are serial and the active call must finish or be canceled first.

#### Scenario: Parent session cannot call itself
- **WHEN** a named-session target resolves to the parent's active CLI session
- **THEN** Agent Runner rejects the call before spawning or resuming a child

#### Scenario: Concurrent second call is rejected
- **WHEN** a parent attempt submits a second call while its first call is still running
- **THEN** Agent Runner returns an instructive `call_in_progress` error without queueing, canceling, or spawning another child

#### Scenario: Later call is accepted
- **WHEN** a prior call from the parent attempt has finished
- **THEN** the parent can submit another valid call

### Requirement: Results and failures

A successful call SHALL return a structured tool result containing the child's final response and the requested target kind and name. The result MUST NOT expose the raw CLI session ID, usage, or cost. A validation or child-execution failure SHALL return a structured tool error without automatically failing the parent step or retrying the call.

#### Scenario: Named-session success result
- **WHEN** a child targeted through `session: implementor-session` succeeds
- **THEN** the tool result contains the child's final response and identifies the named-session target

#### Scenario: Profile success result
- **WHEN** a child targeted through `agent: implementor` succeeds
- **THEN** the tool result contains the child's final response and identifies the profile target

#### Scenario: Child failure returns control to parent
- **WHEN** the child process fails
- **THEN** the tool returns a structured error, keeps the parent attempt active, and performs no automatic retry

#### Scenario: Parent explicitly retries
- **WHEN** a call returns a failure and the parent submits a later valid call
- **THEN** Agent Runner treats the later call as a separate invocation

### Requirement: Cancellation propagation

When the parent attempt is canceled, stopped, or exits while an agent call is running, Agent Runner SHALL terminate the called child and MUST NOT allow it to continue independently. The parent SHALL retain the outcome dictated by its existing cancellation, stop, or exit behavior.

The live MCP request and its authenticated control connection SHALL lease the called execution. When the client cancels the MCP request, the MCP bridge exits, or that control connection is lost before a result is returned, Agent Runner SHALL cancel the called child, retain its terminal evidence, make the in-flight slot available, and keep the parent attempt active. A retry with the same accepted request ID SHALL receive the cached canceled result rather than spawning another child.

#### Scenario: Parent cancellation terminates child
- **WHEN** the parent attempt is canceled while its child is running
- **THEN** Agent Runner terminates the child and preserves the parent's cancellation outcome

#### Scenario: Parent exit leaves no orphan
- **WHEN** the parent process exits while its child is running
- **THEN** Agent Runner terminates the child and no called-agent process remains running independently

#### Scenario: MCP cancellation releases the call slot
- **WHEN** the client cancels an active `call_agent` request
- **THEN** Agent Runner cancels the child, retains its terminal evidence, and allows the parent attempt to make a later call

#### Scenario: Bridge connection loss cancels the child
- **WHEN** the MCP bridge exits or loses its authenticated control connection before the child returns
- **THEN** Agent Runner cancels the child and does not leave an abandoned in-flight call

### Requirement: Nested execution evidence

Agent Runner SHALL retain each called child's output, session bookkeeping, usage, cost, outcome, and duration as nested execution evidence attributable to the parent attempt. Called-agent usage and cost SHALL contribute to run totals without being exposed in the tool result.

#### Scenario: Successful child contributes evidence
- **WHEN** a called child succeeds with reported usage or cost
- **THEN** Agent Runner records its output, outcome, duration, usage, and cost beneath the parent and includes the reported metrics in run totals

#### Scenario: Failed child retains evidence
- **WHEN** a called child fails
- **THEN** Agent Runner retains its output, failure, duration, and any available usage or cost beneath the parent attempt
