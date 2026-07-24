# Task: Build Agent-Call Runtime

## Goal

Implement the Runner-owned synchronous `call_agent` runtime: its typed MCP contract and bridge, authenticated control request, child invocation and session semantics, serial execution safety, structured results, cancellation, and idempotency. The supervising Agent Runner process must remain the sole owner of policy, CLI execution, and durable run state.

## Background

You MUST read these approved artifacts before starting:

- `proposal.md`, especially **What Changes**, **Technical Approach**, and **Out of Scope**.
- `design.md`, especially decisions 1–4 and 6, the cancellation and deduplication risks, and migration steps 3–4.
- `specs/agent-calls/spec.md`, `specs/named-sessions/spec.md`, and `specs/step-control-channel/spec.md` for the acceptance criteria copied below.

Use the shared invocation and mode-neutral control abstractions already present in the worktree. Relevant implementation seams include:

- `internal/exec/agent.go` and the extracted invocation core for profile resolution, autonomous-headless execution, process supervision, output filtering, usage extraction, workdir behavior, and session discovery.
- `internal/control/` for the run-scoped socket, active attempt, credential validation, request registry, connection lifecycle, and control-rejection auditing.
- `internal/session/session.go`, `internal/model/context.go`, and `internal/model/state.go` for declared named sessions, the shared run-scoped name-to-session map, persistence, and resume.
- `internal/cli/adapter.go` for a Runner integration descriptor passed to adapter preparation without embedding attempt credentials in adapter configuration.
- `cmd/agent-runner/internal_cmd.go` for a fixed internal stdio MCP-server subcommand. Add the official MCP Go SDK to `go.mod`/ `go.sum`; do not hand-roll MCP framing or run a persistent daemon.
- A new focused package such as `internal/agentcall/` for the canonical tool schema, request/result/error types, stdio bridge, and supervising handler. Keep step executors in `internal/exec/` and keep model types independent from higher packages.

The workflow-authored prompt template is the capability opt-in. Compute eligibility from the literal, case-sensitive `call_agent` substring before interpolation and engine enrichment. Carry that decision as trusted invocation metadata; interpolated params, system prompts, engine enrichment, and later chat messages must not enable the tool.

The MCP process only translates protocol messages and forwards a typed request over the private socket. It must not resolve profiles, mutate session state, or spawn agents. Authenticated control admission is not call acceptance. The supervising Runner validates the schema, prompt-based eligibility, profiles, fields, workdirs, named declarations, self-session safety, and the one-in-flight rule, then reserves the request ID. The call becomes accepted at that boundary, before the shared invocation core attempts CLI launch.

Use a unique call ID and request ID. The first accepted request owns a shared eventual result; a retry of that request waits for or receives the same result. A different request while one call is active receives `call_in_progress` and never queues, cancels, or preempts the child. Lease an accepted call to its authenticated control connection so MCP cancellation, bridge exit, disconnect, or parent-attempt deactivation terminates the child process group and retains its terminal result. Invalid, ineligible, and distinct concurrent requests remain pre-acceptance rejections with no call execution evidence. If CLI launch fails after acceptance, retain the failed call evidence and cache its structured error for same-request-ID retries.

Fresh `agent` targets do not enter the named-session map or change last-session-step bookkeeping. Named `session` targets use the declaration's profile and CLI, share the existing map with workflow steps, flush a discovered session before success returns, and reject a target whose resolved CLI/session identity equals the parent's active session. Calls always run autonomous-headless in the parent's worktree, receive the profile system prompt and supplied prompt, omit workflow-engine enrichment, and never receive control credentials or agent-call integration.

This task owns runtime eligibility, bridge progress/cancellation, and child suppression. Adapter-specific MCP registration, permissions, and host timeout configuration are implemented through the stable integration descriptor and are outside this file's implementation scope.

## Spec

### From `specs/agent-calls/spec.md`

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

### Requirement: Agent-call acceptance boundary

Agent Runner SHALL accept an agent call only after authenticating the request, validating its schema, confirming the parent is eligible to use `call_agent`, resolving and validating the target and invocation overrides, enforcing self-session and concurrency safety, and reserving the request ID. The call SHALL become accepted after those checks succeed and before Agent Runner attempts to launch the child CLI.

An invalid, ineligible, or distinct concurrent request SHALL be rejected before acceptance and MUST NOT create call execution evidence. A CLI launch failure after acceptance SHALL be a failed accepted call and SHALL return a cached structured error for idempotent retries of the same request ID.

#### Scenario: Validated request is accepted before launch
- **WHEN** an authenticated agent-call request passes all Runner validation and safety checks and its request ID is reserved
- **THEN** Agent Runner accepts the call before attempting to launch the child CLI

#### Scenario: Invalid request remains rejected
- **WHEN** an agent-call request fails schema, eligibility, target, override, self-session, or concurrency validation
- **THEN** Agent Runner rejects it without creating call execution evidence

#### Scenario: CLI launch failure is an accepted failure
- **WHEN** an accepted call fails while launching its child CLI
- **THEN** Agent Runner returns a structured failure and gives a retry with the same request ID that cached failure without another launch attempt

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

### From `specs/named-sessions/spec.md`

### Requirement: Agent-call access to named sessions

An agent call targeting `session: <name>` SHALL use the same declaration visibility, pinned agent profile, run-scoped named-session map, persistence, composition, and drift behavior as a workflow step targeting that name. Agent calls and workflow steps SHALL read and update the same named-session entry. A call-level `model` override SHALL apply to the invocation without changing the agent profile pinned by the declaration. The invocation SHALL use the CLI resolved from the declared profile.

#### Scenario: Call creates named session on first use
- **WHEN** an agent call targets a declared named session with no entry in the run-scoped named-session map
- **THEN** Agent Runner creates the CLI session and stores its ID under the declared name

#### Scenario: Call resumes workflow-created named session
- **WHEN** a workflow step previously created the named session targeted by an agent call
- **THEN** the call resumes the CLI session stored by the workflow step

#### Scenario: Workflow step resumes call-created named session
- **WHEN** an agent call previously created a named session and a later workflow step targets the same name
- **THEN** the workflow step resumes the CLI session stored by the call

#### Scenario: Call resolves declaration through composition
- **WHEN** a call made from a sub-workflow targets a named session declared within its visible workflow composition
- **THEN** Agent Runner resolves the declaration using the same composition rules as a workflow-step reference

#### Scenario: Call-created named session survives workflow resume
- **WHEN** an agent call creates a named session, the runner process exits, and the workflow is resumed
- **THEN** a later call or workflow-step reference resumes the persisted CLI session

#### Scenario: Agent drift behavior applies to call-created session
- **WHEN** a persisted named session created by a call has an agent profile that differs from the current declaration on workflow resume
- **THEN** Agent Runner trusts the persisted session ID and emits the existing agent-drift warning without recreating the session

#### Scenario: Invocation overrides do not change declaration
- **WHEN** an agent call targets a named session and supplies a valid `model` override
- **THEN** Agent Runner applies the override to that invocation while leaving the declaration's pinned agent profile and resolved CLI unchanged

### From `specs/step-control-channel/spec.md`

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

## Done When

- The internal MCP subcommand performs SDK lifecycle negotiation, publishes only the canonical `call_agent` schema, forwards authenticated requests over the run's private socket, emits rate-limited progress when a token is supplied, and maps success/failure into the approved MCP result semantics.
- Table-driven tests reject missing prompts, missing or multiple targets, undeclared or reserved session names, forbidden `cli`/ `mode` combinations, invalid profile/CLI/model/workdir overrides, and named-session self-resume before a child starts.
- Acceptance-boundary tests prove authenticated admission alone creates no call execution, successful Runner validation and request-ID reservation accept the call before launch, pre-acceptance rejection creates no call evidence, and CLI launch failure returns a cached structured failure without a second launch on same-ID retry.
- Focused executor tests prove fresh-profile calls, first-use and resumed named-session calls, workflow-created/call-created session sharing, persistence across Runner resume, model/workdir overrides, forced autonomous-headless execution, omitted engine enrichment, and no recursive Runner tools.
- Control tests prove fresh attempt credentials for eligible interactive and autonomous parents, exact run/step/attempt/token validation, same-request idempotency before and after completion, distinct-request `call_in_progress` rejection, and slot reuse after terminal completion.
- Cancellation tests prove parent cancellation/exit, MCP request cancellation, bridge exit, and control disconnect terminate the entire child process group, cache a canceled result for same-ID retry, release the in-flight slot, and leave the parent attempt active where specified.
- Success returns only target kind/name and filtered final response. Validation and execution failures return stable structured error codes/messages without raw session IDs, usage, cost, automatic retry, or automatic parent-step failure.
- Existing workflow completion and committed-turn clients still work through the generalized control endpoint, and called children receive neither usable `AGENT_RUNNER_CONTROL_*` values nor the Runner integration descriptor.
- Tests for every scenario copied into this task pass at the runtime boundary except the adapter-owned scenarios **Autonomous enabled parent receives pre-authorized access**, **Interactive enabled parent uses normal tool approval**, and **Configurable host timeout does not bound the call**, which are outside this task's implementation gate. Runtime coverage still proves prompt eligibility, called-child suppression, requested progress, and the absence of a Runner-imposed timeout. Run `make fmt`, targeted package tests, and `go test ./...`.
