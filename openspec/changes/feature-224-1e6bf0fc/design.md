## Context

The Runner-owned agent-call integration has three layers:

- **Contract and bridge** (`internal/agentcall`): `contract.go` defines `Request`, `Response`, `Result`, `Error`, the MCP schemas, and strict decoding. `bridge.go` is the stdio MCP server spawned per parent CLI (`agent-runner internal call-agent-mcp`). It forwards each tool call to the Runner over the control socket and, for `call_agent` only, emits a content-free progress notification every 15 s while the request is open.
- **Control channel** (`internal/control/control.go`): each bridge request is a fresh unix-socket connection carrying `agent_call`, `agent_call_get`, or `agent_call_cancel`. The request is authenticated against the active attempt and dispatched to `control.AgentCallHandler`.
- **Runtime** (`internal/exec/agent_call.go`): `AgentCallHandler` is attempt-scoped.
  - `resolve` validates the request and produces a `resolvedAgentCall` (profile, adapter, CLI, model, workdir, session ID, resume flag).
  - `HandleAgentCall` accepts the call, creates an `acceptedAgentCall` record (`callID`, `status`, `cancel`, `done`, `response`, `childSessionID`), and runs `finishAccepted` in a goroutine.
  - `execute` builds CLI args and calls `InvokeAgent`. `childStdoutCapture` tees raw child stdout into a bounded buffer that is used for session discovery.
  - `finalizeExecution` emits `agent_call_end`, maps errors to statuses, enforces the 16 MiB result limit, and caches the response on the record.
  - Metrics (`internal/metrics/collector.go`), audit summaries (`internal/audit/summary.go`), and the run view (`internal/runview`) rebuild call records from the `agent_call_start` / `agent_call_end` audit data.

These constraints shape the design:

- The `async-mcp` change is implemented but not archived. This change's `agent-calls` delta modifies requirements that `async-mcp` also modifies, so `async-mcp` must be archived first.
- The handler record does not retain the resolved invocation settings, so a resume today can only come from a named session's stored ID.
- Adapters differ on resume. Claude, Copilot, Cursor, and OpenCode omit `--model` on resume, while Codex passes `-m`. Claude stores session transcripts per working directory, which `cli.SessionStore.SessionExists(sessionID, workdir)` exposes.
- Child stdout for Claude (`stream-json`) and Codex (`exec --json`) is JSONL. Copilot also emits JSONL, but no summarizer is required for it in this change.

## Goals / Non-Goals

**Goals:**
- A Runner-enforced, per-call `timeout` that reaches a parent blocked inside one `tools/call`.
- A `follow_up: <call_id>` target that resumes an earlier `agent:` child's native session with inherited settings.
- A `details` object on every terminal response with a `call_id`.
- Activity-bearing progress notifications and non-terminal snapshots.
- Additive changes only: existing requests, responses, audit records, and metrics stay valid.

**Non-Goals:**
- Parallel calls, follow-ups across parent attempts, a profile-level or default duration limit, and authoritative commit attribution.
- Changing how named-session calls handle model overrides.
- Activity summarizers for Copilot, Cursor, or OpenCode.

## Approach

### 1. Contract (`internal/agentcall/contract.go`)

- `Request` gains `FollowUp *string` (`json:"follow_up"`) and `Timeout *string` (`json:"timeout"`).
  - The MCP schema adds `follow_up` and `timeout` as non-empty strings.
  - `oneOf` gains a third branch: `follow_up` is required, and `agent`, `session`, and `cli` are forbidden.
- `TargetKind` gains `TargetFollowUp = "follow_up"`. For a follow-up, `Target.Name` is the referenced `call_id`.
- `Request.Validate` changes:
  - Exactly one of `agent`, `session`, or `follow_up` is required (`invalid_target`).
  - `cli` is rejected with `follow_up` (`invalid_request`).
  - `timeout` is parsed with `time.ParseDuration` and must be within `[1s, 24h]` (`invalid_request`). A new `Request.TimeoutDuration()` helper returns the parsed value.
- New error codes: `CodeTimedOut = "timed_out"` and `CodeNotResumable = "not_resumable"`.
- `Response` gains two fields:
  - `Activity string` (`json:"activity,omitempty"`), set only on non-terminal snapshots.
  - `Details *Details` (`json:"details,omitempty"`), set only on terminal responses with a `call_id`.

```go
type Details struct {
    Exit     string          `json:"exit"`                // "exited" | "not_launched" | "terminated"
    ExitCode *int            `json:"exit_code,omitempty"` // set when Exit == "exited"
    Duration string          `json:"duration"`            // acceptance → terminal, elapsed format
    Session  SessionDetails  `json:"session"`
    Git      GitDelta        `json:"git"`
}
type SessionDetails struct {
    CLI        string `json:"cli"`
    Model      string `json:"model,omitempty"`  // effective model passed to the child
    Resumed    bool   `json:"resumed"`
    FollowUp   bool   `json:"follow_up"`        // eligible as a follow_up source
}
type GitDelta struct {
    State     string      `json:"state"` // captured | not_git | unavailable | non_linear
    StartHead string      `json:"start_head,omitempty"`
    EndHead   string      `json:"end_head,omitempty"`
    Commits   []GitCommit `json:"commits,omitempty"`
    Truncated bool        `json:"truncated,omitempty"`
}
type GitCommit struct { SHA, Subject string } // abbreviated SHA; subject truncated to 200 chars
```

- Tool descriptions are updated:
  - `call_agent` describes `follow_up`, `timeout`, and `details`, and states that `details.git` observes `HEAD` movement and does not prove the child authored those commits.
  - `get_agent_call` notes that `running` snapshots carry `activity`.

### 2. Call record and resolution (`internal/exec/agent_call.go`)

- `acceptedAgentCall` gains:
  - `resolved *resolvedAgentCall`, retained after acceptance;
  - `nativeSessionID string`, the discovered session ID, or the pre-assigned ID (Claude) once the CLI launched;
  - `cancel context.CancelCauseFunc`, replacing `context.CancelFunc`;
  - `deadline *time.Timer`;
  - `activity *activityTracker`;
  - `startHead`, the git `HEAD` captured at launch, plus the git root state.
- `resolve` gains a `follow_up` branch that runs before profile resolution:
  1. Under `h.mu`, look up `h.byCallID[name]`.
     - Not found: `unknown_call`.
     - Source is `h.active` or non-terminal: `call_in_progress`, with the active call's ID and elapsed time.
     - Source target kind is `session`: `invalid_target`.
     - `nativeSessionID == ""`: `not_resumable`.
  2. Copy the source's resolved profile, adapter, CLI, model, profile name, and workdir.
  3. Apply a `model` override.
     - If it differs from the inherited model and the adapter does not implement `cli.ResumeModelApplier` returning true, reject with `invalid_model`.
     - Otherwise probe it with `adapter.ProbeModel` as for other targets.
  4. Apply a `workdir` override with the existing `resolveAgentCallWorkdir(parent.Worktree, parent.Workdir, requested)`. When `workdir` is omitted, use the source's effective workdir.
  5. Set `sessionID = source.nativeSessionID` and `resume = true`.
  6. If the CLI matches the parent's and the session equals `activeParentSessionID()`, reject with `self_session`.
- `agentCallSessionStrategy` returns `"resume"` for follow-ups. Follow-ups never write `NamedSessions`, because only the `TargetSession` branch writes it.
- In `execute`, before launch, when `resume` is true for a follow-up and the adapter implements `cli.SessionStore`, check `SessionExists(sessionID, workdir)`. If it is false, return an accepted `not_resumable` failure without launching. This prevents Claude from silently starting a fresh session after a workdir override. Every other resume error surfaces as the CLI's non-zero exit (`execution_failed`). The ordinary agent-step fallback to a fresh session is never used for calls.

### 3. Deadline and settlement

All terminal paths share one synchronized decision.

- `acceptedAgentCall` gains `settlement *callSettlement`, guarded by `h.mu`:

```go
type settlementKind int // settledExited | settledLaunchFailed | settledTimedOut | settledCanceled | settledAborted
type callSettlement struct {
    kind     settlementKind
    at       time.Time // duration boundary
    exitCode *int      // set only for settledExited
}
```

- `h.trySettle(record, kind, exitCode) bool` sets `record.settlement` only if it is nil and reports whether this caller won. It is the only writer, and every terminal path calls it:
  - **Natural exit:** `AgentInvocation` gains an `OnExited(exitCode int)` hook. `InvokeAgent` calls it immediately after the process wait returns, before usage extraction and session discovery. The hook calls `trySettle(settledExited, &code)`.
  - **Launch failure:** `preLaunchFailure` and the `not_resumable` precheck call `trySettle(settledLaunchFailed)`.
  - **Deadline:** `record.deadline = time.AfterFunc(timeout, ...)`. The callback runs `if trySettle(settledTimedOut) { cancel(errAgentCallTimedOut) }`. A callback already in flight after natural exit loses `trySettle` and does nothing.
  - **Explicit cancel:** `HandleCancelAgentCall` runs `if trySettle(settledCanceled) { cancel(errAgentCallCanceled) }`, then waits on `done` as today. If it lost, it returns the published result of the winner.
  - **Teardown:** a goroutine watching the attempt context calls `trySettle(settledAborted)`. `childCtx` derives from the attempt context, so the process is already being killed.
- `childCtx, cancel := context.WithCancelCause(parent)` is retained only to deliver termination to the process. Context state never decides the outcome.
- After `InvokeAgent` returns, `execute` reads `record.settlement` under `h.mu` and builds the response from it, not from `runErr` or `ctx.Err()`:
  - `settledExited`: today's success or `execution_failed` mapping from the exit code and invocation outcome.
  - `settledTimedOut`: `CodeTimedOut`, status `failed`, outcome `OutcomeFailed`, and a message naming the configured timeout.
  - `settledCanceled`: `call_canceled` / `canceled` / `OutcomeAborted`.
  - `settledAborted`: the existing teardown mapping.
  - If no settlement exists when `InvokeAgent` returns, for example a runner error before the exit hook, `execute` settles it as `settledExited`, or as `settledLaunchFailed` when the CLI never launched.
- `finalizeExecution` stops `record.deadline` (a no-op if it already fired) and collects evidence. It cannot change the settlement, because evidence collection happens strictly after settlement. The response, `Details`, `agent_call_end` `outcome` / `error_code`, and metrics are all derived from the one settlement value.
- The timer belongs to the attempt-scoped record, not to an MCP request. A parent blocked in `call_agent` or `get_agent_call` therefore wakes on `record.done` and receives the `timed_out` response.
- Test seams:
  - an injectable `AgentCallHandlerOptions.AfterFunc` for the deadline timer;
  - an `afterSettle` hook that can pause finalization.

  These make the races deterministic: deadline during evidence collection, cancel during evidence collection, and deadline before exit.

### 4. Details and git delta

- New file `internal/exec/agent_call_details.go`. Git access goes through an injectable `AgentCallHandlerOptions.Git` (`func(ctx, dir string, args ...string) (string, error)`). It defaults to `exec.CommandContext("git", ...)` with `-C dir`, and every call is bounded by `agentCallGitTimeout = 5 * time.Second`.
- Capture before launch (in `execute`, after args are built):
  - `git rev-parse --is-inside-work-tree` in `call.workdir`: a failure exit means `not_git`, and any other error means `unavailable`.
  - Then `git rev-parse HEAD`. An unborn `HEAD` makes the start state `unavailable`.
- Finalize happens in `finalizeExecution`, before the response is cached and after the child has exited, so it never delays termination:
  1. `git rev-parse HEAD` for the end `HEAD`.
  2. `git merge-base --is-ancestor start end`. A non-zero result means `non_linear`.
  3. `git log --format=%h%x00%s -n 51 start..end` for the commits. Over 50 sets `truncated`, and the list keeps the newest 50.
  - Any timeout or error yields `unavailable` with whatever `HEAD`s were captured.
- `buildDetails(record, resolved, execution)` fills the rest.
  - `Exit` comes from the frozen settlement, never from later context state:
    - `settledExited` gives `exited` with its `exitCode`;
    - `settledLaunchFailed` gives `not_launched`;
    - the other kinds give `terminated`.
  - `Duration`: `settlement.at - record.started`, rounded like `elapsed`.
  - `Session.Model`: `resolved.knownModel`, omitted when empty. `knownModel` is computed in `resolve`:
    - for a fresh `agent` call or a fresh named session: the model passed to the CLI;
    - for a resume through an adapter implementing `cli.ResumeModelApplier`: the model passed;
    - for a follow-up through any other adapter: the source record's `knownModel`;
    - for a named-session resume through any other adapter: empty, because the named-session map stores only the session ID, so the session's original model is unknown, and a requested override is not applied.

    Named-session request handling is otherwise unchanged.
  - `Session.FollowUp`: true when the target kind is `agent` or `follow_up` and `nativeSessionID != ""`.
- `Details` is attached to every terminal response, including `preLaunchFailure` and the oversized-result failure, before the size check. `oversizedAgentCallFailure` re-attaches it. Details are small: at most about 50 × 250 bytes.
- `nativeSessionID` is set in `finalizeExecution` from `DiscoveredSessionID`. It falls back to `invocation.SessionID` when `CLILaunched` is true (Claude's pre-assigned UUID).

### 5. Activity (`internal/cli`, `internal/exec`)

- New optional adapter interface in `internal/cli/adapter.go`:

```go
// HeadlessActivitySummarizer recognizes one complete stdout line of a
// running autonomous-headless child. ok=false leaves the previous summary.
type HeadlessActivitySummarizer interface {
    SummarizeActivity(line []byte) (summary string, ok bool)
}
```

- Claude (`stream-json`):
  - `assistant` with a `tool_use` block becomes `tool_use: <name>`. A `text` block becomes `assistant message`, and a `thinking` block becomes `thinking`.
  - `user` with a `tool_result` block becomes `tool_result`.
  - `system` with subtype `init` becomes `session started`, and `result` becomes `turn finished`.
  - Events with `parent_tool_use_id` are prefixed with `subagent `.
- Codex (`exec --json`): `item.started` / `item.completed` become `<event>: <item.type>`. For `mcp_tool_call` the tool name is appended (`… mcp_tool_call <server>/<tool>`). `turn.started`, `turn.completed`, and `error` map to their event names.
- Summaries never read text, command, argument, or output fields.
- `activityTracker` (exec) exposes two writers.
  - The stdout writer is added to `childStdoutCapture`'s `io.MultiWriter`, so it receives raw stdout alongside the existing capture.
  - The stderr writer is a recency-only tee composed into `AgentInvocation.StderrWrapper` around any adapter `StderrWrapper`. It forwards bytes unchanged and never parses or stores stderr content.
  - Both writers update `lastOutput` on every write. Only the stdout writer feeds the summarizer.
  - It splits lines with a 1 MiB per-line cap, discarding overlong lines.
  - For each line it calls the summarizer, if the adapter has one, and stores `{summary, at}`.
  - It is mutex-protected and never errors, so it cannot affect output.
- `activityTracker.Describe(now)` composes the one-line text and clamps it to 200 runes, with newlines replaced:
  - with a recognized event: `"<summary> (<age> ago)"`;
  - with output but no recognized event: `"no recognized activity; last output <age> ago"`;
  - with nothing written to either stream: `"no output yet"`.
- `marshalSnapshotLocked` sets `Response.Activity` for non-terminal records.

### 6. Control channel and bridge

- New message type `MessageAgentCallActivity = "agent_call_activity"`, authenticated and attempt-scoped like the other agent-call messages and added to the same allow-lists.
  - Payload: `{"call_id": "..."}` or `{"start_request_id": "..."}`, exactly one. The start-request form resolves the deferred spec choice: a bridge blocked in `call_agent` does not yet know its `call_id`, but it does know the request ID it generated.
  - The handler is an optional interface `control.AgentCallActivityHandler { HandleAgentCallActivity(ctx, AgentCallRequest) json.RawMessage }`, type-asserted so existing fakes still compile. A handler without it is rejected with a structured `control_failure`.
  - `AgentCallHandler.HandleAgentCallActivity` looks up `h.byCallID` or `h.accepted` and returns `marshalSnapshotLocked(record)` immediately: status, elapsed, and activity for running calls, or the cached terminal response.
  - An unknown ID returns `unknown_call`. It never waits, never touches `active`, and never changes the deadline.
- `SendAgentCallFromEnvironment` accepts the new type.
- Bridge:
  - `invokeBridge` enables progress for `call_agent` and `get_agent_call`.
  - On each tick it calls `options.Send(ctx2, MessageAgentCallActivity, newRequestID, payload)`, where `ctx2` has a 2 s timeout. For `call_agent` the payload holds the start request ID; for `get_agent_call` it holds the `call_id`.
  - It uses `response.Activity` as the notification message when the response is non-empty and error-free. Otherwise it falls back to `"called agent is still running"`.
  - The fetch runs in the tick branch with a 2 s bound, which can delay delivery of a just-finished result by at most 2 s. That is acceptable against a 15 s interval and keeps the bridge single-threaded.

### 7. Evidence and views

- `agent_call_start` adds `timeout` (the string, when set), `follow_up_of` (the source `call_id`), and `session_strategy: "resume"` for follow-ups. `target_kind` / `target_name` carry `follow_up` / the source `call_id`.
- `agent_call_end` adds `error_code` alongside `error`. A timeout records `outcome: "failed"`, `error_code: "timed_out"`, and `timeout`. It also adds `git_state` and `exit` so a rebuild from audit can show them.
- `metrics.ExecutionRecord` and `audit.summary` read the new optional fields (`timeout`, `follow_up_of`, `error_code`). Older records leave them empty.
- `runview/tree.go` labels `follow_up` calls as `call follow-up: <source call_id>`. `selected_detail.go` shows timeout, follow-up source, error code, and git state when present.

### Data flow (follow-up with timeout)

```
lead ──call_agent{follow_up:c1, timeout:"20m", prompt}──▶ bridge ──agent_call──▶ control ──▶ handler.resolve
   │                                                         │                                  │ c1 terminal, agent-target, native session known
   │◀── progress "tool_use: Bash (4s ago)" ◀── activity RPC ◀─┤ every 15 s                       ▼
   │                                                         │                         accept c2, AfterFunc(20m)
   │                                                         │                         execute: SessionExists? → InvokeAgent(resume)
   │◀─────────── {call_id:c2, status, result|error, details} ◀── record.done ◀── finalize: git delta, details, cache
```

## Decisions

- **One synchronized settlement per call** decides the outcome. Natural exit (signalled by an `OnExited` hook before post-processing), deadline, cancel, and teardown all race through `trySettle`. Context cancel causes alone cannot express "the child already exited", because natural completion never cancels the context. Using `trySettle` closes the window between process exit and result publication, which bounded git collection widens.
- **Timed-out calls are `failed` with `timed_out`**, not a new status. `IsTerminalStatus` and callers stay unchanged.
- **The record retains `resolvedAgentCall`** rather than re-resolving the source's profile at follow-up time. Profiles could change mid-run, and the follow-up must continue with the exact CLI, model, and workdir the child used.
- **Resume model capability is an optional adapter interface (`cli.ResumeModelApplier`)**, implemented only by Codex, rather than a hard-coded CLI list in exec. A registry-wide test asserts that each adapter's declaration matches whether its resume args contain the model.
- **Follow-ups check `SessionExists` before launch and never fall back to a fresh session**, unlike ordinary agent steps. The spec forbids silently starting a fresh child.
- **Activity is computed in the Runner and pulled by the bridge** over a new control message keyed by start request ID or `call_id`. This keeps the bridge stateless, reuses authentication and attempt scoping, and gives poll snapshots the same text.
- **Git evidence runs after the child exits, under a 5 s per-command bound, through an injectable runner** so tests need no real git when focusing on outcome mapping. One temp-repo test covers the real commands.
- **`Details` lives on `Response`, not `Result`**, because failures must carry it too and `Result` exists only on success.

## Risks / Trade-offs

- **Activity summarizers depend on CLI stream formats.** Claude and Codex event shapes may drift. Unrecognized lines are ignored and the summary falls back to output recency, so drift degrades the message, never the call. Unit tests use recorded JSONL fixtures under `internal/cli/testdata`.
- **Progress still resets Claude Code's idle timeout.** Informative heartbeats do not stop a hung child from holding a lead. `timeout` is the remedy, and leads that need protection must pass it. A default ceiling was rejected as out of scope.
- **Git commands in large repositories.** `git log -n 51` and `merge-base` are cheap, and the 5 s bound caps the worst case. A slow repository yields `unavailable` rather than a delay.
- **Shared worktree writes by the parent.** A Cursor parent can commit while polling, and those commits appear in the delta. The spec and tool description present the delta as an observation, not attribution.
- **Archive ordering with `async-mcp`.** If this change is archived first, its MODIFIED `Long-running MCP execution` block would overwrite the main spec with text that assumes the async contract. Mitigation: archive `async-mcp` first. Tasks will note this.

## Migration Plan

Everything is additive: new optional request fields, a new target kind, new response fields, new audit keys, and a new control message type. Existing workflows, bridges, and persisted runs keep working. A bridge and runner from the same binary are always version-matched, because the bridge is spawned as `agent-runner internal call-agent-mcp`. Rollback is a plain revert. Older audit logs simply lack the new keys.

## Open Questions

None.
