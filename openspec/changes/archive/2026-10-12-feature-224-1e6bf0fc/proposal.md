## Why

The Runner-owned agent-call tools (`call_agent`, `get_agent_call`, `cancel_agent_call`) already let a
lead agent delegate serially to another profile, for example a Claude lead handing implementation to a
Codex implementor. Calls are attributed in metrics, nest in the TUI, and die with the parent. That is
enough for a proof of concept, but four gaps make the path fragile for the orchestrated
implement-change workflow (PR #209, `orchestrate-implementation` step), which today falls back to
CLI-native sub-agents:

- **No deadline.** Runner imposes no child duration limit and raises each host's MCP timeout to the
  maximum, so a hung child can block a step for about 28 hours. A Claude lead blocked inside
  `call_agent` cannot call `cancel_agent_call`, and the bridge's 15-second progress heartbeat keeps
  resetting Claude Code's 30-minute stdio idle timeout, so nothing on the host side rescues it either.
- **No follow-up to an `agent:` child.** Every `agent: <profile>` call starts a fresh session. Only
  workflow-declared `session:` names resume, so a lead that wants to send review fixes back to the
  implementor that wrote the code must be given a fixed pool of named sessions in YAML, sized in
  advance.
- **Thin results.** A successful call returns only the target and final message text. The lead cannot
  tell how long the child ran, how its CLI exited, which session it used, or what it committed without
  re-deriving that from git or trusting the child's prose.
- **Content-free progress.** Heartbeats say "called agent is still running". The lead (and a Cursor
  lead polling `get_agent_call`) cannot tell a working child from a stuck one, which is exactly the
  judgment it needs to decide whether to cancel.

Fixing these now makes `call_agent` a robust replacement for CLI-native sub-agents in serial
orchestration, without waiting for parallel calls.

## What Changes

- **Per-call deadline.** `call_agent` accepts an optional `timeout` (a duration). When it elapses the
  Runner cancels the child through the same path as `cancel_agent_call` and the call ends as a
  structured `timed_out` failure, which a blocked lead receives as its `call_agent` result. Omitting
  `timeout` keeps today's unbounded behavior. Follow-up calls accept `timeout` the same way.
- **Follow-ups by call ID.** `call_agent` gains a third target, `follow_up: <call_id>`, mutually
  exclusive with `agent` and `session`. It resumes the native CLI session of an earlier terminal
  `agent:` call (or follow-up) from the same parent attempt. By default it inherits the directly
  referenced call's resolved profile, CLI, model, and effective working directory, so a follow-up that
  carries only the call ID and a prompt continues exactly where that child ran. It gets its own new
  `call_id`, so a chain of follow-ups stays individually visible, and each link inherits from the call
  it names, so explicit overrides carry forward. Overrides:
  - `cli` is rejected.
  - `workdir` follows the existing agent-call rules: resolved against the parent's effective
    directory and contained in the same worktree.
  - `model` is accepted only when the CLI applies a model on resume (Codex today). For any other CLI,
    an explicit model different from the inherited one is rejected before acceptance rather than
    silently dropped.
  - `timeout` applies to the follow-up only.

  A follow-up is rejected with a structured error, and spawns nothing, when the call ID is unknown,
  still running, targeted a named session (use `session:` instead), or has no discovered native session
  to resume. If the CLI cannot resume that session at launch, the follow-up fails as a structured
  error. It never silently starts a fresh child.
- **Structured result.** Terminal responses, success and failure alike, carry a `details` object:
  - the CLI exit status and the duration;
  - a session descriptor with the CLI, the effective model when it is known (never a requested model
    the CLI did not apply), whether the session was resumed, and whether the call can be followed
    up;
  - an observed git `HEAD` delta for the call's workdir: the `HEAD` captured at launch and at
    finalization, the commits reachable from the final `HEAD` but not the starting one (abbreviated SHA
    and subject, capped and flagged when truncated), and an explicit evidence state.

  The evidence state distinguishes a captured delta from not a git repository, capture failed or timed
  out, and non-linear history (the final `HEAD` does not descend from the starting one). The delta is
  documented as an observation of `HEAD` movement, not proof that the child authored those commits. A
  git evidence failure never changes the call's outcome. The existing `result.response` text is
  unchanged.
- **Informative progress.** Progress heartbeats and non-terminal `get_agent_call` / `cancel_agent_call`
  snapshots carry a short, bounded summary of the child's latest activity, such as its last tool or
  event and how long ago it happened. Adapters that cannot provide it fall back to time since the
  child's last output.
- Update `docs/agent-calls.md`, the MCP tool descriptions and schemas, and the `agent-calls` spec.

No breaking changes: every new request field is optional, the new target is additive, and the new
response fields extend the existing objects.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `agent-calls`: Add the optional call deadline and `timed_out` outcome, the `follow_up` target and its
  eligibility rules, the structured `details` on terminal results, and activity summaries in progress
  notifications and non-terminal snapshots. This delta builds on the start/poll/cancel contract in the
  unarchived `async-mcp` change.
- `cli-adapter`: Adapters optionally expose a short latest-activity summary from a running headless
  child's output stream, and declare whether they apply a model override when resuming a session.
- `step-control-channel`: The bridge can ask the supervising Runner for a running call's activity
  snapshot so each heartbeat carries current progress.

## Technical Approach

All four enhancements stay inside the existing attempt-scoped `AgentCallHandler`
(`internal/exec/agent_call.go`), the shared MCP contract (`internal/agentcall`), and the control
channel. The Runner stays the only place that resolves sessions and owns child lifecycles; the model
only supplies IDs and limits.

- **Deadline.** The handler already owns a per-call cancel function leased to the parent attempt.
  A timer started at acceptance invokes that cancel with a distinct cause, and finalization maps the
  cause to a `timed_out` error code instead of `call_canceled`. Because the timer lives in the Runner,
  it fires even while the lead is blocked in one `tools/call`, which is the case the issue calls out.
  The timeout is validated at acceptance (positive, bounded upper limit) so a bad value is rejected
  before a child exists.
- **Follow-ups.** Today each accepted call record keeps only the discovered child session ID. It will
  also retain the call's resolved invocation settings: profile, CLI, effective model, and effective
  workdir. A `follow_up` target resolves from the referenced record, not from the parent defaults, into
  the same `resume`-mode execution path that named sessions use. It then runs the existing
  self-session guard. Adapters declare whether they apply a model override on resume. Claude, Copilot,
  Cursor, and OpenCode omit `--model` when resuming; Codex passes it. Validation uses that declaration
  so an override is never accepted and then dropped. Call IDs remain scoped to one parent attempt, so a
  follow-up never reaches across steps or runs. Follow-up sessions are not written into the run's
  named-session map.
- **Structured result.** Exit status and duration come from the invocation result the handler already
  builds for metrics. The git delta comes from recording `HEAD` in the call's workdir at launch and
  inspecting `start..end` at finalization, with an ancestry check for non-linear history. Every git
  command runs under a short bounded timeout, so collecting evidence cannot hold a timed-out or
  canceled call open. The native CLI session ID stays out of the MCP result, as it is today:
  `follow_up` makes the `call_id` the lead's resumable handle, and the raw ID remains in audit and
  metrics evidence.
- **Progress.** The child's stdout already passes through an adapter-wrapped capture. An optional
  adapter hook distills the newest stream event into a one-line summary that the handler stores on the
  call record. On each heartbeat tick the bridge requests that snapshot over the control channel, so the
  bridge stays stateless and poll snapshots reuse the same field.

## Out of Scope

- Parallel or queued agent calls; calls remain serial per parent attempt.
- A profile-level or Runner-wide default maximum duration. Only callers that pass `timeout` get a
  deadline, preserving today's behavior for every existing workflow.
- Following up across parent attempts, steps, or runs, or after a workflow resume.
- Following up on named-session calls, which already resume through `session:`.
- Making every CLI apply a model override on resume, and changing how named-session calls handle
  model overrides.
- Authoritative per-call commit attribution (for example commit trailers or provenance hooks). The
  result reports only an observed `HEAD` delta.
- Returning the raw native CLI session ID, usage, or cost through the MCP result.
- Streaming the child transcript through progress or poll results.
- Recursive delegation from called children.
- Archiving `async-mcp`, and updating the published `codagent:call-agent` skill in the sibling Agent
  Skills repository. The new fields are self-describing in the MCP schema and tool descriptions; the
  skill can adopt them in a later change.
- Switching the implement-change workflow's `orchestrate-implementation` step to `call_agent`.

## Impact

- Code: `internal/agentcall` (request schema, target validation, response types, bridge heartbeat),
  `internal/exec/agent_call.go` (deadline timer, follow-up resolution, result details, activity
  snapshots, retained invocation settings, bounded git `HEAD` capture), `internal/control` (activity
  snapshot RPC), and `internal/cli` (optional per-adapter activity summaries, starting with Claude and
  Codex stream formats, plus a resume model-override capability).
- APIs: additive changes to the `call_agent` MCP input schema and to the `call_agent`,
  `get_agent_call`, and `cancel_agent_call` response objects, plus one new error code (`timed_out`)
  and new follow-up rejection codes. Existing callers keep working unchanged.
- Evidence: `agent_call_start` / `agent_call_end` audit data and `run-metrics.json` agent-call records
  gain the timeout, the follow-up source call, and the timed-out outcome. Existing records stay
  readable.
- Docs and specs: `docs/agent-calls.md`, and `agent-calls`, `cli-adapter`, and `step-control-channel`
  deltas.
- Tests: handler, bridge, contract, and adapter tests next to their packages.
