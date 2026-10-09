- [x] Implement the change described by these files using TDD, as `CLAUDE.md` requires. Satisfy every spec scenario and every `INT-*` / `E2E-*` obligation in the test plan.

  **Contract** (`internal/agentcall/contract.go`)
  - Add `Request.FollowUp` (`follow_up`) and `Request.Timeout` (`timeout`).
    - In the schema, add a third `oneOf` branch: `follow_up` without `agent`, `session`, or `cli`.
    - `Validate` requires exactly one target (`invalid_target`) and rejects `cli` with `follow_up` (`invalid_request`).
    - `Validate` parses `timeout` with `time.ParseDuration` and requires 1s–24h (`invalid_request`). Add a `TimeoutDuration()` helper.
  - Add `TargetFollowUp = "follow_up"` and the error codes `timed_out` and `not_resumable`.
  - Add `Response.Activity`, plus `Response.Details` with `Details`, `SessionDetails`, `GitDelta`, and `GitCommit` as shown in `design.md` §1.
  - Update the three tool descriptions. `call_agent` must state that `details.git` observes `HEAD` movement and is not authorship.

  **Call records, follow-ups, and the known model** (`internal/exec/agent_call.go`)
  - Retain on `acceptedAgentCall`:
    - the `resolvedAgentCall`;
    - `nativeSessionID`: the discovered ID, or the pre-assigned ID once the CLI has launched;
    - `knownModel`;
    - the settlement, deadline timer, activity tracker, and start `HEAD`.
  - Add the `follow_up` branch to `resolve`, following `design.md` §2:
    - Reject with `unknown_call`, `call_in_progress`, `invalid_target` (named-session source), `not_resumable`, `self_session`, or `invalid_model` as specified.
    - Inherit profile, CLI, effort, model, and workdir from the directly referenced record.
    - Resolve an explicit `workdir` with `resolveAgentCallWorkdir` against the parent.
    - Set `resume = true` with the source's native session.
    - Session strategy is `resume`, and nothing is written to `NamedSessions`.
  - Add the optional `cli.ResumeModelApplier` interface and implement it on Codex only. Add a registry-wide test that every adapter's declaration matches whether its resume args contain the model.
  - Compute `knownModel` in `resolve` (`design.md` §4). Never report an unapplied requested model, including a model override on a named-session resume.
  - Before launching a follow-up, check `cli.SessionStore.SessionExists(sessionID, workdir)` when the adapter supports it. If the session is missing, return an accepted `not_resumable` failure without launching. Never fall back to a fresh session.

  **Settlement and deadline** (`design.md` §3)
  - Implement a single `trySettle` under `h.mu`, fed by:
    - a new `AgentInvocation.OnExited(exitCode)` hook, called in `InvokeAgent` right after the process wait and before usage or session post-processing;
    - pre-launch and `not_resumable` failures;
    - the deadline callback via an injectable `AfterFunc`;
    - `HandleCancelAgentCall`;
    - attempt teardown.
  - Only the winner signals termination through `context.WithCancelCause`.
  - Build the response, error code and status, `details.exit`, `details.duration` (acceptance to settlement), the `agent_call_end` outcome / `error_code`, and metrics from the frozen settlement only. Context state must not decide them.
  - A timeout is status `failed` with code `timed_out` and outcome `failed`. Add an `afterSettle` test hook so the race tests are deterministic.

  **Details and git delta** (new `internal/exec/agent_call_details.go`)
  - Add an injectable `AgentCallHandlerOptions.Git` runner with a 5 s bound per command.
  - Capture the start `HEAD` before launch. After settlement, collect the end `HEAD`, run the `merge-base --is-ancestor` check, and run `git log -n 51 start..end`.
  - Report the states `captured`, `not_git`, `unavailable`, and `non_linear`, and cap commits at 50 with `truncated`.
  - Attach `Details` to every terminal response with a `call_id`, including pre-launch and oversized-result failures, but never to snapshots or rejections. Git failures never change the outcome.

  **Activity** (`internal/cli`, `internal/exec`)
  - Add the optional `cli.HeadlessActivitySummarizer` and implement it for Claude `stream-json` and Codex `exec --json` per `design.md` §5. It must never read text, command, argument, or output fields.
  - Add `activityTracker`:
    - Its stdout writer goes into `childStdoutCapture`'s multiwriter, with a 1 MiB line cap, and feeds the summarizer.
    - Its recency-only stderr tee is composed into `StderrWrapper` around any adapter wrapper, with bytes unchanged.
    - `Describe(now)` clamps to 200 runes on one line and falls back to `last output … ago` / `no output yet`.
  - Set `Response.Activity` on non-terminal snapshots.
  - Add recorded Claude and Codex JSONL fixtures under `internal/cli/testdata/`.

  **Control channel and bridge**
  - Add `MessageAgentCallActivity = "agent_call_activity"` to the control allow-lists and to `SendAgentCallFromEnvironment`.
    - Payload: exactly one of `call_id` or `start_request_id`.
    - It dispatches to an optional `control.AgentCallActivityHandler`; a handler without it is rejected with `control_failure`.
    - `AgentCallHandler.HandleAgentCallActivity` answers immediately from `marshalSnapshotLocked` and rejects unknown IDs with `unknown_call`.
  - In `internal/agentcall/bridge.go`, enable progress for `call_agent` and `get_agent_call`. On each tick, fetch activity with a 2 s bound and use it as the notification message, falling back to `called agent is still running`.

  **Evidence and views**
  - `agent_call_start` adds `timeout` and `follow_up_of`. `agent_call_end` adds `error_code`, `timeout`, `exit`, and `git_state`.
  - Read these as optional fields in `internal/metrics/collector.go`, `internal/audit/summary.go`, and `internal/runview`.
  - Label follow-ups `call follow-up: <call_id>` in `runview/tree.go`, and show timeout, follow-up source, error code, and git state in `selected_detail.go`.
  - Older audit logs must still load unchanged.

  **Docs**
  - Update `docs/agent-calls.md`:
    - document `follow_up`, `timeout`, `details`, and activity;
    - replace the statements that Runner imposes no child duration limit and that calls lack call-specific duration budgets;
    - add troubleshooting for `timed_out` and `not_resumable`.
  - Do not edit the sibling Agent Skills repository.

  **Tests**
  - Add unit tests next to each package.
  - Add the integration tests:
    - INT-001 and INT-002 in `internal/exec/agent_call_bridge_integration_test.go`;
    - INT-003 in `internal/exec/agent_call_followup_integration_test.go`;
    - INT-004 in `internal/exec/agent_call_details_integration_test.go`;
    - INT-005 in `internal/cli` (with `internal/exec` for the output-equivalence check);
    - INT-006 in `internal/runview/historical_integration_test.go` and `internal/metrics/collector_test.go`.

    All run in the existing `go test ./...` with fake CLI scripts and second-scale timeouts.
  - Add E2E-001 and E2E-002 to `cmd/agent-runner/real_agent_e2e_test.go` under the existing `e2e_agents` tag. These are run locally, not in CI.

  Do not change existing status values, existing error codes, named-session model handling, or the workflow `tools: [call_agent]` declaration. Do not archive this change before `openspec/changes/async-mcp` is archived, because this change's MODIFIED `Long-running MCP execution` block builds on the async contract. Finish with `make fmt`, `make test`, and `make lint` passing.

  Source files:
  - [proposal.md](proposal.md)
  - [specs/agent-calls/spec.md](specs/agent-calls/spec.md)
  - [specs/cli-adapter/spec.md](specs/cli-adapter/spec.md)
  - [specs/step-control-channel/spec.md](specs/step-control-channel/spec.md)
  - [design.md](design.md)
  - [test-plan.md](test-plan.md)
  - [decisions.md](decisions.md)
