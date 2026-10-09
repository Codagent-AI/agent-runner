## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

Unit tests carry most of this change and are not inventoried here. They live next to their packages:
- contract validation and schema (`internal/agentcall/contract_test.go`);
- handler resolution, rejection codes, cause mapping, and details assembly
  (`internal/exec/agent_call_test.go`, which uses its existing fake `ProcessRunner`);
- bridge heartbeat fallback (`internal/agentcall/bridge_test.go`);
- control dispatch (`internal/control/control_test.go`);
- adapter summarizers and declarations (`internal/cli`).

The integration obligations below cover the places where those units could each pass while the
assembled behavior fails:
- the Runner-side timer reaching an open MCP request through the real bridge and control socket;
- a real child process being terminated;
- resume arguments and the working directory reaching a real subprocess;
- real git;
- recorded CLI stream formats;
- rebuilding the run view from audit.

All integration tests run in the existing CI `test` job (`go test -tags dev_audit -timeout 300s ./...`).
They use fake CLI executables written as small shell scripts into `t.TempDir()`, so they need no
credentials. Each test must finish in a few seconds: use short timeouts (1–3 s) and an injected 100 ms
heartbeat interval. No new CI job or suite is added.

## Integration Tests

### INT-001: Deadline reaches a blocked `call_agent` through the real bridge and control socket
- Covers: `agent-calls` Per-call deadline (waiting parent receives a timed-out result, child finishing before the deadline, explicit cancel wins, evidence); Long-running MCP execution (caller timeout is the only Runner bound).
- Boundary: in-memory MCP client → `agentcall.NewServer` bridge using `EnvironmentSender` → real `control` server on a unix socket → `exec.AgentCallHandler` → real `ProcessRunner` → fake CLI subprocess.
- Setup:
  - Start a control server with an active, call-eligible attempt and an unbounded wait budget.
  - Register a fake `claude`-shaped adapter whose executable sleeps for 60 s and writes its PID to a file.
- Action:
  1. Call `call_agent` with `agent`, `timeout: "2s"`.
  2. Repeat with a fast-exiting fake and `timeout: "10s"`.
  3. Repeat with `timeout: "5s"`, issuing `cancel_agent_call` after 500 ms.
  4. Run deterministic settlement races, using the handler's injectable deadline timer and `afterSettle` pause hook with fast-exiting fakes:
     - fire the deadline while finalization is paused after a natural exit;
     - issue `cancel_agent_call` while finalization is paused after a natural exit;
     - fire the deadline before the fake exits.
- Assertions:
  - The first `tools/call` returns within ~3 s with status `failed` and `error.code == "timed_out"`, and the recorded PID is no longer alive.
  - `agent_call_end` in the audit log has `outcome: failed`, `error_code: timed_out`, and `timeout: "2s"`.
  - The fast call succeeds with no termination.
  - The canceled call ends `canceled` / `call_canceled`, and no later `timed_out` overwrites it.
  - In the race cases, the first two settle as the child's own success: `details.exit == "exited"` with its exit code, and the cancel returns that success. The third settles as `timed_out` with `details.exit == "terminated"`.
  - In every case the MCP response, `details`, the `agent_call_end` `outcome` / `error_code`, and the agent-call metrics record describe the same outcome, and `details.duration` ends at settlement.
- Execution: `internal/exec/agent_call_bridge_integration_test.go`, run by `go test ./internal/exec`.

### INT-002: Progress heartbeats and poll snapshots carry activity from a real child stream
- Covers: `agent-calls` Informative call progress; `step-control-channel` Agent-call activity snapshot (by start request ID and by `call_id`, does not count as a call, unknown or stale rejected).
- Boundary: the same bridge → control → handler → subprocess path as INT-001, with the real Claude adapter's summarizer fed by the child's stdout through the `childStdoutCapture` tee.
- Setup:
  - A fake CLI prints a Claude `stream-json` `assistant` line with a `tool_use` block named `Bash` and input `{"command":"echo SECRET"}`, then sleeps 2 s.
  - The bridge heartbeat interval is set to 100 ms, and the MCP client supplies a progress token.
  - A second fake prints non-JSON text only.
  - A third fake writes `STDERR-SECRET` lines to stderr every 100 ms and nothing to stdout.
- Action:
  1. `call_agent` with the first fake; collect progress notifications.
  2. Separately, with a positive wait budget of 200 ms, call `get_agent_call` while running.
  3. Send an `agent_call_activity` request for an unknown ID and one with a stale attempt ID.
- Assertions:
  - At least one notification message contains `tool_use: Bash` and an age, and none contains `SECRET`. Messages are ≤ 200 characters and single-line.
  - The `running` poll snapshot includes the same `activity`.
  - The non-JSON child yields `last output … ago`.
  - The stderr-only child yields recent output (not `no output yet`), no summary contains `STDERR-SECRET`, and its captured stderr file is byte-identical to the bytes written.
  - Unknown and stale snapshot requests return structured errors, the running child completes normally, and no `call_in_progress` rejection occurs.
- Execution: `internal/exec/agent_call_bridge_integration_test.go`.

### INT-003: Follow-up resumes the referenced session with inherited settings in a real subprocess
- Covers: `agent-calls` Follow-up by call ID (lead sends fixes back, inherits workdir and model, chained inheritance, explicit workdir rules, supported and unsupported model change, resume failure does not start a fresh session, no named-session state); `cli-adapter` Resume model-override capability.
- Boundary: `AgentCallHandler` → real adapter `BuildArgs` (Claude and Codex) → real `ProcessRunner` → fake CLI that records argv and cwd to a file, emits a session-discoverable line, and exits 0.
- Setup:
  - A temp worktree with `services/api` and `services/web` subdirectories.
  - For the Claude case, a temp `HOME` with a Claude project transcript for the session in `services/api` only.
- Action:
  1. Call `agent` with `workdir: services/api` and `model: m1`, giving `c1`.
  2. Call `follow_up: c1` with only a prompt, giving `c2`.
  3. Call `follow_up: c2` with `workdir: services/web`.
  4. Call `follow_up: c1` with a different `model`, on both Codex and Claude.
  5. Call a declared named session whose stored Claude session was started with `m1`, passing `model: m2`.
- Assertions:
  - `c2`'s recorded argv uses the adapter's resume form with `c1`'s session ID, and its cwd is `services/api`. For Codex, argv carries `-m m1`.
  - The Claude follow-up with `workdir: services/web` returns an accepted `not_resumable` failure, and the fake CLI was never executed (no argv file).
  - The Codex model change is applied (`-m m2` in argv, `details.session.model == "m2"`).
  - The Claude model change is rejected with `invalid_model` and no `call_id`.
  - The run's `NamedSessions` map is unchanged by follow-ups.
  - The named-session resume with `m2` succeeds with no `--model` in argv, as today, and its `details.session` omits `model` rather than reporting `m2`.
  - A Claude follow-up of a fresh `c1` reports the inherited `m1`.
- Execution: `internal/exec/agent_call_followup_integration_test.go`.

### INT-004: Git `HEAD` delta against real git repositories
- Covers: `agent-calls` Structured call details (successful details, no commits, non-git workdir, rewritten history, truncation, git failure does not change outcome).
- Boundary: `AgentCallHandler` details collection → real `git` binary → temp repositories mutated by a fake CLI subprocess.
- Setup: `git init` in temp directories with an initial commit, using local `user.name` / `user.email` config only. Fake CLI variants:
  - one creates two commits;
  - one creates 55 commits;
  - one runs `git reset --hard HEAD~1` after a commit made before the call;
  - one runs in a non-git temp dir;
  - one makes no change.
  - The unavailable case injects a git runner that blocks past its bound.
- Action: run one `agent` call per variant and read the terminal `details`.
- Assertions:
  - The two-commit variant is `captured`, with both subjects newest first and correct start and end `HEAD`s.
  - 55 commits gives 50 entries with `truncated: true`.
  - Reset gives `non_linear` with both `HEAD`s.
  - The non-git dir gives `not_git`.
  - No change gives `captured` with equal `HEAD`s and no commits.
  - The blocking git runner gives `unavailable` within the bound, while status and response match the child's success.
  - `details.exit` is `exited` with `exit_code: 0` on all variants, and `details.session` never contains the native session ID.
- Execution: `internal/exec/agent_call_details_integration_test.go`. It requires `git` on `PATH`, which the CI runner already provides.

### INT-005: Activity summarizers against recorded real CLI streams leave output unchanged
- Covers: `cli-adapter` Headless activity summaries (Claude tool use, Codex tool activity, malformed lines ignored, captured output unchanged, adapters without summaries).
- Boundary: the real Claude and Codex adapters' `SummarizeActivity` and `WrapStdout` over JSONL captured from the actual CLIs.
- Setup: add recorded fixtures under `internal/cli/testdata/` from real headless runs:
  - Claude `stream-json` including `tool_use`, `tool_result`, a subagent event, and `result`;
  - Codex `exec --json` including `command_execution`, `mcp_tool_call`, and `agent_message`.
  - Insert one malformed line into each.
- Action: stream each fixture through `childStdoutCapture` with the activity tee enabled and with it disabled.
- Assertions:
  - The summary sequence matches a golden list of event kinds and tool names.
  - No summary contains any text, command, argument, or output field value from the fixture.
  - The malformed line leaves the previous summary in place.
  - Captured bytes and wrapped downstream output are identical with and without the tee.
  - The Copilot, Cursor, and OpenCode adapters do not implement the summarizer interface.
- Execution: `internal/cli/activity_test.go` and `internal/exec`.

### INT-006: Timed-out and follow-up calls rebuild from audit into metrics and the run view
- Covers: `agent-calls` Follow-up appears as a separate call; Per-call deadline (timed-out call keeps its evidence).
- Boundary: real audit log written by the handler → `metrics` collector → `audit` summary → `runview` historical model.
- Setup: run, with fakes, a parent that makes three calls: an `agent` call `c1`, a `follow_up: c1` call, and a call that times out. Persist the run directory.
- Action: load the completed run through the historical run-view path and `run-metrics.json` generation.
- Assertions:
  - The tree shows three calls beneath the parent, labeled `call agent: <profile>`, `call follow-up: c1`, and the timed-out call.
  - The selected-call detail shows the timeout, follow-up source, error code `timed_out`, and git state.
  - Metrics contain three `kind: "agent-call"` records with `follow_up_of` and `timeout` populated where applicable.
  - A pre-change audit fixture without the new keys still loads unchanged.
- Execution: `internal/runview/historical_integration_test.go` (existing file) and `internal/metrics/collector_test.go`.

## End-to-End Tests

### E2E-001: Real Claude lead follows up a child by call ID under a timeout
- Covers: the issue's core journey, a lead orchestrating an implementor and sending a follow-up back to the same child. Also covers the structured details a real lead receives.
- Surface: the `agent-runner --headless` binary running a catalog workflow, extending the existing real-agent harness.
- Setup:
  - The existing `prepareRealAgentE2E(t, "claude")` temp workspace, initialized as a git repo, with the harness's isolated `HOME`.
  - The `claude_headless_smoke` profile for both lead and child.
  - The workflow step declares `tools: [call_agent]`.
- Journey:
  1. The lead calls `agent: claude_headless_smoke` with `timeout: "10m"`. It asks the child to invent a two-word token and keep it only in memory: not in any file, commit, or reply. The child also commits an unrelated file (`notes.txt` with fixed content) and replies with only `ready`.
  2. The lead then calls `follow_up: <call_id>`, asking the child to write the token it invented into `token.txt`, without the lead knowing or supplying the token.
  3. The lead writes the first call's `details.git.commits` count and the follow-up's `details.session.resumed` into a result file.
  4. Negative control, in the same run: the lead makes a fresh `agent: claude_headless_smoke` call asking for "the token you invented earlier" to be written into `control.txt`.
- Assertions (the continuation oracle):
  - Native-session evidence:
    - Both calls' `agent_call_end` entries carry the same `resolved_session_id`.
    - The isolated `HOME`'s Claude transcript for that session ID contains both the original prompt and the follow-up prompt, plus the follow-up's response.
    - No new Claude transcript file was created during the follow-up call.
  - Token continuity: `token.txt` holds a two-word token that appears in no workspace file or git object written before the follow-up.
  - The negative control's `control.txt` is absent or does not match `token.txt`, and its call has a distinct session ID. This shows the oracle fails for a fresh session.
  - The result file reports one commit and `resumed: true`.
  - The run completes, and its audit shows the follow-up's `target_kind: follow_up`.
- Execution: `cmd/agent-runner/real_agent_e2e_test.go` under the existing `e2e_agents` build tag, selected by `E2E_AGENTS=claude`. Run locally with credentials, not in CI, matching the existing real-agent tests.

### E2E-002: Real Claude lead blocked on a hung child receives `timed_out`
- Covers: the issue's motivating failure, a Claude lead blocked in `call_agent` that cannot cancel.
- Surface: the same harness as E2E-001.
- Setup: the same workspace.
- Journey:
  1. The lead calls `agent: claude_headless_smoke` with `timeout: "30s"`, asking the child to run `sleep 600` in its shell before answering.
  2. The lead writes the returned `error.code` to a file and finishes.
- Assertions:
  - The step completes in well under 10 minutes, and the file contains `timed_out`.
  - No `sleep 600` process from the child remains.
  - Audit shows `outcome: failed` and `error_code: timed_out` for the call.
- Execution: `cmd/agent-runner/real_agent_e2e_test.go` (`e2e_agents`, `E2E_AGENTS=claude`).

## Acceptance Testing Envelope

- Environments and sandboxes:
  - The local checkout via `./dev.sh`.
  - Temp directories and temp git repositories created for the pass.
  - The `e2e_agents` real-agent harness, which isolates `HOME`-scoped state and writes catalog workflows into a temp workspace.
  - Completed runs can be inspected with the run view and `agent-runner` debug, metrics, and audit commands. Per `CLAUDE.md`, starting a run from the TUI under a synthetic PTY does not work, so runs are launched headless and then inspected.
- Credentials and secrets:
  - Use only credentials the real-agent harness already loads: the Claude OAuth token file and existing Codex, Copilot, Cursor, or OpenCode logins on this machine, if present.
  - Do not create, print, or copy credentials.
- Authorized effects:
  - Real agent turns against existing local logins. Keep them to a small number of short prompts (roughly ten or fewer child calls in total) to bound cost.
  - Local commits only inside temp repositories.
  - Delete temp workspaces and any leftover child processes afterwards.
- Off limits:
  - Pushing, opening PRs, or touching remotes.
  - Editing global or project CLI configuration (`~/.claude`, `~/.codex`, `~/.cursor`, and so on) outside the harness's isolated `HOME`.
  - The live Agent Factory and its runs.
  - Commits in this repository's checkout.
  - The sibling Agent Skills repository.
- Permitted substitutes:
  - Fake CLI executables (shell scripts emitting recorded JSONL) when a real CLI or its login is unavailable. Record which CLIs were substituted.
  - A Codex lead or child may be replaced by Claude for journey checks when Codex is not logged in. Codex-specific model-on-resume behavior must then rely on INT-003.
- Known risk areas:
  - Races between cancel, timeout, and natural completion.
  - Timer cleanup after finalization: no late `timed_out` and no leaked goroutines.
  - Follow-up workdir and model inheritance across chains.
  - Claude's per-directory session store.
  - Stream-format drift in activity summaries.
  - Activity text leaking message or tool content.
  - Git evidence in shared worktrees, for example a Cursor parent committing while polling, which is an accepted limitation shown as observation.
  - Archive ordering with the unarchived `async-mcp` change.
  - Accepted limitation: informative heartbeats still reset Claude Code's 30-minute idle timeout, so only `timeout` bounds a hung child.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| agent-calls: Per-call deadline and settlement | INT-001, INT-006 | E2E-002 | — |
| agent-calls: Long-running MCP execution (caller timeout is the only Runner bound) | INT-001 | E2E-002 | — |
| agent-calls: Follow-up by call ID | INT-003, INT-006 | E2E-001 | — |
| agent-calls: Invocation fields and valid forms (follow-up target) | INT-003 | E2E-001 | — |
| agent-calls: Structured call details (exit, duration, known model) | INT-001, INT-003, INT-004 | E2E-001 | — |
| agent-calls: Informative call progress (incl. stderr recency) | INT-002 | — | — |
| cli-adapter: Resume model-override capability | INT-003 | — | — |
| cli-adapter: Headless activity summaries | INT-002, INT-005 | — | — |
| step-control-channel: Agent-call activity snapshot | INT-002 | — | — |
| Journey: lead orchestrates an implementor and sends fixes back | INT-003 | E2E-001 | — |
| Journey: blocked lead rescued from a hung child | INT-001 | E2E-002 | — |
