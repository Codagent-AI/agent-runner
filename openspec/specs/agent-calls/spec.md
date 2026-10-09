# agent-calls Specification

## Purpose
TBD - created by archiving change call-agent. Update Purpose after archive.
## Requirements
### Requirement: Agent-call tool availability

Agent Runner SHALL expose `call_agent` to an interactive or autonomous workflow agent step if and
only if that step statically declares `tools: [call_agent]`. Agent Runner MUST derive availability
from the validated declaration rather than any authored, interpolated, engine-enriched, system, child,
or later conversational prompt text. An omitted or empty tools list SHALL provide no agent-call
integration. An agent started by `call_agent` MUST NOT receive the tool regardless of its prompt,
profile, or parent's declaration. Eligibility failures SHALL explain that `call_agent` was not enabled
for the active step declaration and MUST NOT instruct the user to add prompt text.

An interactive parent running in external-user mode SHALL receive the same pre-authorized access as
an autonomous parent, because its headless turns cannot show an approval prompt. Its access SHALL
remain available on every resumed turn of the step.

#### Scenario: Interactive enabled parent receives the tool
- **WHEN** Agent Runner starts an interactive agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Autonomous enabled parent receives the tool
- **WHEN** Agent Runner starts an autonomous agent step declaring `tools: [call_agent]`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Declaration works without prompt token
- **WHEN** an agent step declares `tools: [call_agent]` and its prompt does not contain `call_agent`
- **THEN** Agent Runner provisions the tool

#### Scenario: Prompt token alone does not enable the tool
- **WHEN** an agent step's prompt contains `call_agent` but the step omits `tools`
- **THEN** the agent does not receive the `call_agent` tool

#### Scenario: Autonomous enabled parent receives pre-authorized access
- **WHEN** Agent Runner provisions `call_agent` for an autonomous agent step that declares it
- **THEN** only the Runner-owned `call_agent` tool is pre-authorized and its invocation does not wait for interactive approval

#### Scenario: Interactive enabled parent uses normal tool approval
- **WHEN** Agent Runner provisions `call_agent` for an interactive agent step that declares it, outside external-user mode
- **THEN** invocation follows that CLI's normal MCP tool-approval flow

#### Scenario: External-user interactive parent receives pre-authorized access
- **WHEN** Agent Runner provisions `call_agent` for an interactive agent step that declares it, in external-user mode
- **THEN** on every turn of the step, only the Runner-owned agent-call tools are pre-authorized, and invocation does not wait for approval

#### Scenario: Called child cannot delegate recursively
- **WHEN** `call_agent` starts a child agent whose supplied prompt mentions `call_agent`
- **THEN** the child does not receive the `call_agent` tool

#### Scenario: Ineligible error cites declaration
- **WHEN** a parent without a `call_agent` declaration submits an agent-call request
- **THEN** Agent Runner rejects it with guidance about the active step declaration rather than prompt
  contents

### Requirement: Invocation fields and valid forms

A `call_agent` invocation MUST include a `prompt` and exactly one target:
- `agent: <profile>` for a fresh profile-backed session;
- `session: <declared-name>` for a workflow-declared named session; or
- `follow_up: <call_id>` to resume the session of an earlier call from the same parent attempt.

Overrides by target:
- An `agent` target SHALL accept `cli`, `model`, and `workdir` as optional overrides.
- A `session` target SHALL accept `model` and `workdir` as optional overrides and SHALL use the CLI resolved from its declared profile.
- A `follow_up` target SHALL accept `model` and `workdir` under the follow-up rules and SHALL use the CLI of the call it references.
- Every target SHALL accept an optional `timeout`.

The `session` field is exclusively for declared named sessions; `new`, `resume`, and `inherit` are not valid agent-call targets. Any invocation outside these forms SHALL be rejected without spawning a child.

#### Scenario: Agent profile creates a fresh session
- **WHEN** a valid call specifies `prompt` and `agent: implementor`
- **THEN** Agent Runner starts a fresh session using the `implementor` profile

#### Scenario: Declared named session is selected
- **WHEN** a valid call specifies `prompt` and `session: implementor-session`
- **THEN** Agent Runner targets the workflow-declared `implementor-session` without requiring an `agent` field

#### Scenario: Earlier call is followed up
- **WHEN** a valid call specifies `prompt` and `follow_up: c1` for an eligible earlier call `c1`
- **THEN** Agent Runner resumes `c1`'s session without requiring an `agent` or `session` field

#### Scenario: Fresh-profile invocation overrides are applied
- **WHEN** a valid `agent`-targeted call includes `cli`, `model`, or `workdir`
- **THEN** Agent Runner applies those fields using the corresponding agent-step override semantics

#### Scenario: Named-session invocation overrides are applied
- **WHEN** a valid `session`-targeted call includes `model` or `workdir`
- **THEN** Agent Runner applies those fields while retaining the CLI resolved from the named session's declared profile

#### Scenario: Multiple targets are rejected
- **WHEN** a call specifies `follow_up` together with `agent` or `session`
- **THEN** Agent Runner rejects it with `invalid_target` and spawns no child

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

Agent Runner MUST NOT impose a fixed duration limit on a valid agent call. Only a caller-supplied `timeout` bounds a call's duration. A child's survival MUST NOT depend on any MCP request staying open, on host MCP tool-execution timeout configuration, or on progress notifications. The process-local MCP integration SHALL avoid allowing a generic short host tool timeout to govern called-agent execution when the host exposes a supported timeout control, while preserving an explicit deadline configured by the user or requesting client. When a supported host exposes such a control, adapters SHALL raise or disable a generic short default so one waiting `call_agent` can cover a long child. Where a host enforces an unconfigurable `tools/call` abort, its parents SHALL receive a wait budget shorter than that abort and reach the result by polling. When an MCP client supplies a progress token, the bridge SHALL emit rate-limited progress notifications while the child remains active. Progress notifications MUST NOT be treated as a substitute for client-side timeout configuration, polling, or cancellation.

#### Scenario: Configurable host timeout does not bound the call
- **WHEN** a supported host exposes a process-local MCP tool-execution timeout control
- **THEN** Agent Runner provisions `call_agent` so the host's generic short default does not terminate an otherwise active child

#### Scenario: Requested progress is reported
- **WHEN** an MCP client invokes `call_agent` with a progress token and the child remains active
- **THEN** the bridge emits rate-limited progress notifications until the call reaches a terminal result

#### Scenario: Child outlives an aborted request
- **WHEN** a host aborts an open `call_agent` request for a child that runs longer than its `tools/call` wait
- **THEN** the child remains running and the parent can still reach its result with `get_agent_call`

#### Scenario: Bounded poll returns before the host abort
- **WHEN** a parent whose host enforces a short `tools/call` wait invokes `get_agent_call` while the child is still running
- **THEN** the poll returns a non-terminal status before that host wait expires

#### Scenario: Host timeout settings are not required
- **WHEN** an enabled parent uses a CLI without a supported MCP tool-execution timeout control
- **THEN** a long child can still complete through `get_agent_call` after `call_agent` has returned

#### Scenario: Caller timeout is the only Runner bound
- **WHEN** a call omits `timeout` and its child runs for many hours
- **THEN** Agent Runner does not terminate it for its duration, and a call that supplies `timeout` is terminated only when that timeout elapses

### Requirement: Working-directory behavior

The called child SHALL remain within the current worktree established for the run. Agent Runner SHALL use the Git worktree containing the run's launch directory as that boundary; when the launch directory is not inside a Git worktree, the launch directory itself SHALL be the boundary. Loading a workflow file from a different repository MUST NOT change the boundary.

When `workdir` is omitted, the child SHALL use the parent's effective working directory. A relative `workdir` SHALL resolve from the parent's effective working directory. Every resolved `workdir`, including a path reached through a symbolic link, MUST remain inside the current worktree boundary.

#### Scenario: Omitted workdir uses parent directory
- **WHEN** a parent running from an effective working directory invokes `call_agent` without `workdir`
- **THEN** the child runs from the parent's effective working directory

#### Scenario: Relative workdir uses parent directory as its base
- **WHEN** a parent running from `/repo/backend` invokes `call_agent` with `workdir: frontend`
- **THEN** Agent Runner resolves the child's working directory as `/repo/backend/frontend`

#### Scenario: Workdir cannot escape the current worktree
- **WHEN** a call supplies a path or symbolic link that resolves outside the current worktree boundary
- **THEN** Agent Runner rejects the workdir without spawning the child

#### Scenario: External workflow file does not move the boundary
- **WHEN** Agent Runner launches inside one worktree and loads a workflow file located in another repository
- **THEN** called children remain confined to the worktree containing the launch directory

### Requirement: Session behavior

An `agent` target SHALL start a fresh session for every call and MUST NOT add that session to the named-session map. A `session` target SHALL use the existing run-scoped named-session declaration and map, creating the session on first successful use or resuming the stored CLI session on subsequent use. A failed, canceled, or runner/transport-error call MUST NOT establish a new named-session entry.

#### Scenario: Repeated profile calls remain fresh
- **WHEN** two calls independently target `agent: implementor`
- **THEN** Agent Runner starts two distinct CLI sessions and stores neither as a named session

#### Scenario: Named session is created on first call
- **WHEN** a successful call targets a declared named session with no stored CLI session
- **THEN** Agent Runner creates the CLI session and stores its ID under the declared name

#### Scenario: Unsuccessful first call does not establish named-session state
- **WHEN** a call targeting a declared named session with no stored CLI session fails, is canceled, or encounters a runner or transport error
- **THEN** Agent Runner leaves the named-session entry unset

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

The control channel SHALL retain its 16 MiB message limit. When an otherwise successful child's encoded tool result exceeds that limit, Agent Runner SHALL return a structured oversized-result error, keep the parent attempt active, and retain the child's persisted output and execution evidence for inspection. Agent Runner SHALL NOT stream or chunk oversized results in this change.

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

#### Scenario: Oversized successful response retains evidence
- **WHEN** a child succeeds but its encoded tool result exceeds the 16 MiB control-message limit
- **THEN** the tool returns a structured oversized-result error, keeps the parent active, and retains the child's persisted output and execution evidence

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

### Requirement: Per-call deadline

`call_agent` SHALL accept an optional `timeout` for every target kind. The value MUST be a duration string such as `90s`, `45m`, or `2h`, at least one second and at most 24 hours. An empty, unparseable, non-positive, or out-of-range value MUST be rejected before acceptance with an `invalid_request` error and no child.

The deadline SHALL run from acceptance. When it elapses before the call is terminal, Agent Runner SHALL terminate the child the same way `cancel_agent_call` does and SHALL retain its terminal evidence. The call SHALL end with status `failed` and an error with code `timed_out` that states the configured timeout. The deadline SHALL be enforced by the Runner, independently of any open MCP request, so a parent blocked inside `call_agent` or `get_agent_call` receives the `timed_out` result as that tool's response. Every accepted call SHALL settle exactly once. Settlement happens at the first of these events: the child CLI process exits on its own, the deadline elapses, an explicit `cancel_agent_call` is received, or parent teardown begins. A call that ends before launch settles at that failure. The first event fixes the call's outcome: its status, error code, and exit evidence. No later event SHALL change that outcome. This holds even when the event arrives while Agent Runner is still collecting usage, session, or git evidence and before the result is published. A deadline that elapses after the child exited on its own SHALL NOT terminate anything or produce `timed_out`. A cancel received after settlement SHALL return the settled result once it is published. The response, `details`, audit entry, and metrics record SHALL all describe the same settled outcome.

A call without `timeout` SHALL have no Runner-imposed deadline.

#### Scenario: Waiting parent receives a timed-out result
- **WHEN** a parent with an unbounded wait budget calls `call_agent` with `timeout: 10m` and the child is still running ten minutes after acceptance
- **THEN** Agent Runner terminates the child and the open `call_agent` request returns status `failed` with error code `timed_out`

#### Scenario: Polling parent collects a timed-out result
- **WHEN** a parent with a positive wait budget polls `get_agent_call` after the call's deadline has elapsed
- **THEN** the poll returns the cached `failed` result with error code `timed_out` and the in-flight slot is free

#### Scenario: Child finishing before the deadline is unaffected
- **WHEN** a call with a timeout finishes successfully before the deadline
- **THEN** the result is the ordinary success and no termination is signaled

#### Scenario: Deadline after child exit does not change the outcome
- **WHEN** a child exits successfully, and its deadline elapses while Agent Runner is still collecting evidence and has not yet published the result
- **THEN** the call's result is the child's success with `details.exit` reporting its exit code, and the audit entry and metrics record show success

#### Scenario: Cancel after child exit returns the child's outcome
- **WHEN** `cancel_agent_call` arrives after the child exited on its own but before its result is published
- **THEN** the cancel returns the child's own terminal result, not `call_canceled`, and no termination is signaled

#### Scenario: Explicit cancel wins over a later deadline
- **WHEN** the parent cancels a call before its deadline elapses
- **THEN** the call ends as canceled with `call_canceled`, not `timed_out`

#### Scenario: Invalid timeout is rejected before acceptance
- **WHEN** `call_agent` receives `timeout: 0s`, `timeout: 25h`, or `timeout: soon`
- **THEN** the tool returns an `invalid_request` error with no `call_id` and spawns no child

#### Scenario: Omitted timeout keeps unbounded execution
- **WHEN** a call omits `timeout`
- **THEN** Agent Runner does not terminate the child for running long

#### Scenario: Timed-out call keeps its evidence
- **WHEN** a call times out
- **THEN** its retained output, `agent_call_end` audit entry, and agent-call metrics record show a failed outcome, the `timed_out` error, and the configured timeout

### Requirement: Follow-up by call ID

`call_agent` SHALL accept `follow_up: <call_id>` as a third target, mutually exclusive with `agent` and `session`. A follow-up SHALL resume the native CLI session of the referenced call and run the new prompt there. The referenced call MUST be either an `agent:`-targeted call or an earlier follow-up, MUST come from the same parent attempt, and MUST be terminal. Any terminal status is allowed (`succeeded`, `failed`, or `canceled`, including `timed_out`), provided Agent Runner discovered that call's native session.

A follow-up SHALL inherit the profile, CLI, effort, effective model, and effective working directory of the call it directly references. Each link in a chain therefore inherits the overrides of the call before it. Overrides:
- `cli` MUST be rejected.
- `workdir` SHALL follow the ordinary agent-call working-directory rules, resolving relative paths against the parent's effective directory.
- `timeout` SHALL apply only to that follow-up.
- `model` SHALL be accepted when it equals the inherited model, or when the resolved CLI applies a model override on resume. A different model for a CLI that does not apply one on resume MUST be rejected with `invalid_model` before acceptance.

A follow-up SHALL receive its own new `call_id`. Its result target SHALL identify it as a follow-up, naming the referenced `call_id`. A follow-up session MUST NOT be added to the run's named-session map.

A follow-up MUST be rejected before acceptance, with no `call_id` and no child, in these cases:
- `unknown_call` when the referenced ID does not belong to the active parent attempt;
- `call_in_progress` when the referenced call is still running;
- `invalid_target` when the referenced call targeted a named session;
- `not_resumable` when Agent Runner has no discovered native session for the referenced call;
- `self_session` when that session is the parent's active CLI session.

Once accepted, a follow-up whose CLI cannot resume the session SHALL end as a structured failure. When Agent Runner can tell before launch that the CLI has no stored session for the effective working directory, that failure SHALL use code `not_resumable` and the CLI SHALL NOT be launched. It MUST NOT start a fresh session in its place.

#### Scenario: Lead sends fixes back to an implementor
- **WHEN** a call to `agent: implementor` succeeds with `call_id` `c1`, and the parent then calls `call_agent` with `follow_up: c1` and a new prompt
- **THEN** Agent Runner resumes `c1`'s native CLI session with the new prompt and returns a new `call_id` whose target names `c1` as the followed-up call

#### Scenario: Follow-up inherits workdir and model
- **WHEN** call `c1` ran with `workdir: services/api` and `model: gpt-5-codex`, and a follow-up supplies only `follow_up: c1` and a prompt
- **THEN** the follow-up runs in `services/api` with `gpt-5-codex` on `c1`'s CLI

#### Scenario: Chained follow-up inherits from the call it names
- **WHEN** follow-up `c2` of `c1` set `workdir: services/web`, and the parent then calls with `follow_up: c2` and no `workdir`
- **THEN** the new follow-up runs in `services/web`

#### Scenario: Explicit workdir override follows ordinary rules
- **WHEN** a follow-up supplies a relative `workdir`
- **THEN** Agent Runner resolves it against the parent's effective directory and rejects it with `invalid_workdir` if it escapes the worktree

#### Scenario: Unsupported model change is rejected
- **WHEN** a follow-up of a Claude call supplies a `model` different from the one that call used
- **THEN** the tool returns `invalid_model` with no `call_id` and spawns no child

#### Scenario: Supported model change is applied
- **WHEN** a follow-up of a Codex call supplies a different `model`
- **THEN** the resumed child runs with that model and the result reports it as the effective model

#### Scenario: CLI override is rejected
- **WHEN** a follow-up includes `cli`
- **THEN** the tool returns `invalid_request` with no `call_id` and spawns no child

#### Scenario: Unknown call ID is rejected
- **WHEN** `follow_up` names a call ID that the active parent attempt never accepted
- **THEN** the tool returns `unknown_call` with no `call_id` and spawns no child

#### Scenario: Running call cannot be followed up
- **WHEN** `follow_up` names the call that is still in flight
- **THEN** the tool returns `call_in_progress` with no new `call_id` and the running child is unaffected

#### Scenario: Named-session call cannot be followed up
- **WHEN** `follow_up` names a call that targeted `session: implementor-session`
- **THEN** the tool returns `invalid_target`, directing the caller to target the session by name

#### Scenario: Call without a discovered session is not resumable
- **WHEN** `follow_up` names a call whose CLI never launched or whose native session was never discovered
- **THEN** the tool returns `not_resumable` with no `call_id` and spawns no child

#### Scenario: Follow-up of a failed or timed-out call
- **WHEN** a call ended with `timed_out` after its native session was discovered, and the parent follows it up
- **THEN** Agent Runner accepts the follow-up and resumes that session

#### Scenario: Resume failure does not start a fresh session
- **WHEN** an accepted follow-up's CLI cannot resume the referenced session
- **THEN** the follow-up ends as a structured failure and no fresh session is started for it

#### Scenario: Follow-up does not create named-session state
- **WHEN** a follow-up succeeds
- **THEN** the run's named-session map is unchanged

#### Scenario: Follow-up appears as a separate call
- **WHEN** a parent makes a follow-up call
- **THEN** the run view and run evidence show it as its own call beneath the parent, labeled as a follow-up of the referenced call ID, and its audit and metrics records name the referenced call

### Requirement: Structured call details

Every terminal agent-call response with a `call_id` SHALL include a `details` object. This covers success, failure, cancellation, timeout, accepted launch failure, and oversized-result errors, whether the response is returned by `call_agent`, `get_agent_call`, or `cancel_agent_call`. Pre-acceptance rejections and non-terminal snapshots MUST NOT include `details`. `details` SHALL contain:

- **Exit status:** the child CLI's exit code when it exited. Otherwise it SHALL state that the CLI never launched or was terminated by Agent Runner.
- **Duration:** time from acceptance to settlement, in the same format as `elapsed`. Time spent collecting evidence after settlement is excluded.
- **Session descriptor:** the CLI, the effective model when it is known, whether a native session was resumed, and whether the call can be followed up. The descriptor MUST NOT contain the raw native CLI session ID. The model counts as known in three cases:
  - Agent Runner passed it to a fresh session.
  - Agent Runner passed it on a resume through a CLI that applies a model on resume.
  - A follow-up inherited it from a call whose model was known.

  Otherwise the model SHALL be omitted. A requested model that the CLI did not apply MUST NOT be reported as the model. This includes a model override on a named-session resume through a CLI that omits the model on resume.
- **Git `HEAD` delta:** an observation for the call's effective working directory, with:
  - a `state` of `captured`, `not_git`, `unavailable`, or `non_linear`;
  - the starting and ending `HEAD` commit IDs when they were captured;
  - when the state is `captured` or `non_linear`, the commits reachable from the ending `HEAD` but not from the starting `HEAD`, each with its abbreviated commit ID and subject, newest first, capped at 50 entries with a `truncated` flag.

The `state` values mean:
- `not_git`: the working directory is not inside a git repository.
- `unavailable`: the starting or ending `HEAD` could not be captured, including when a bounded git command did not finish in time.
- `non_linear`: the ending `HEAD` does not descend from the starting `HEAD`.

The git delta is an observation of `HEAD` movement during the call. It is not a claim that the child authored those commits, and the tool description SHALL say so. Collecting git evidence MUST be bounded in time. Its failure MUST NOT change the call's status, error, or response, and MUST NOT delay termination of a canceled or timed-out child.

The existing `result.response` and `error` fields SHALL keep their current meaning. `details`, like the rest of the result, MUST NOT expose usage or cost.

#### Scenario: Successful call reports details
- **WHEN** a child exits with code 0 after committing two commits on the current branch
- **THEN** the terminal result includes the final response and a `details` object with exit code 0, the duration, the session descriptor, and a `captured` git delta listing both commits

#### Scenario: Failed call reports details
- **WHEN** a child exits with code 2
- **THEN** the structured error includes `details` with exit code 2 and the call's duration

#### Scenario: Launch failure reports never launched
- **WHEN** an accepted call's CLI fails to launch
- **THEN** the structured error includes `details` stating the CLI never launched

#### Scenario: Timed-out call reports termination
- **WHEN** a call times out
- **THEN** its `details` state that Agent Runner terminated the CLI and report the duration up to termination

#### Scenario: Session descriptor omits the native ID
- **WHEN** any terminal result is returned
- **THEN** its session descriptor names the CLI, the effective model when known, whether a session was resumed, and whether a follow-up is possible, and does not contain the native CLI session ID

#### Scenario: Unapplied named-session model override is not reported
- **WHEN** a call targets a named session that already has a Claude session, with a `model` different from the session's original model
- **THEN** the result's session descriptor omits the model rather than reporting the requested one

#### Scenario: Exit evidence matches the settled outcome
- **WHEN** a call settles by timeout or explicit cancel
- **THEN** `details.exit` states that Agent Runner terminated the CLI, and `details.duration` ends at settlement

#### Scenario: No commits yields an empty captured delta
- **WHEN** `HEAD` does not move during the call
- **THEN** the git delta is `captured` with equal starting and ending `HEAD`s and no commits

#### Scenario: Non-git working directory
- **WHEN** a call runs in a working directory outside any git repository
- **THEN** the git delta state is `not_git` and the call's outcome is unaffected

#### Scenario: Rewritten history is flagged
- **WHEN** the child resets or checks out so the ending `HEAD` does not descend from the starting `HEAD`
- **THEN** the git delta state is `non_linear` and both `HEAD`s are reported

#### Scenario: Large commit range is truncated
- **WHEN** more than 50 commits are reachable from the ending `HEAD` but not from the starting `HEAD`
- **THEN** the git delta lists the 50 newest and sets `truncated`

#### Scenario: Git evidence failure does not change the outcome
- **WHEN** a git command for the delta fails or does not finish within its bound
- **THEN** the git delta state is `unavailable` and the call's status, error, and response are those of the child's execution

#### Scenario: Non-terminal snapshot has no details
- **WHEN** `get_agent_call` returns a `running` snapshot
- **THEN** the snapshot does not include `details`

### Requirement: Informative call progress

While an accepted call is running, Agent Runner SHALL maintain an activity summary for it. The summary is one line of at most 200 characters naming the child's most recent recognized event and, where available, its tool name, plus how long ago that event occurred. When the child's CLI provides no recognizable activity, the summary SHALL instead state how long ago the child last wrote to stdout or stderr, or that it has written to neither yet. Structured activity SHALL be recognized from stdout only. Stderr content MUST NOT appear in any summary. The summary MUST NOT include message text, tool arguments, tool output, or the child's final response.

When an MCP client supplies a progress token, each rate-limited progress notification the bridge emits during a call SHALL carry the call's current activity summary as its message. Non-terminal `call_agent`, `get_agent_call`, and `cancel_agent_call` snapshots SHALL include the current activity summary. If the summary cannot be obtained, the notification or snapshot SHALL fall back to a generic still-running message, and the tool call MUST NOT fail.

#### Scenario: Heartbeat names the child's current tool
- **WHEN** a Claude child's latest stream event is a tool use of `Bash`, and the bridge emits a progress notification
- **THEN** the notification message names the tool-use event, the `Bash` tool, and how long ago it started

#### Scenario: Heartbeat for a CLI without activity parsing
- **WHEN** the child's CLI provides no recognizable activity events
- **THEN** each progress notification states how long ago the child last wrote output, or that it has written none

#### Scenario: Stderr-only output counts as recent output
- **WHEN** a running child writes only to stderr and has written nothing to stdout
- **THEN** the activity summary reports recent output rather than no output, and contains none of the stderr text

#### Scenario: Polling parent sees activity
- **WHEN** a parent with a positive wait budget receives a `running` snapshot from `get_agent_call`
- **THEN** the snapshot includes the call's current activity summary

#### Scenario: Activity omits content
- **WHEN** the child's latest event is an assistant message or a tool result
- **THEN** the activity summary names the event kind without including its text or output

#### Scenario: Unavailable activity does not fail the tool
- **WHEN** the bridge cannot obtain the activity summary for a heartbeat
- **THEN** it emits a generic still-running progress message and the `call_agent` request continues

