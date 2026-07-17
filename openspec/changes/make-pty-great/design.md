# Design: Direct Terminal Handoff and Step Control Channel

## Context

Interactive and autonomous-interactive agent steps currently run inside an Agent Runner PTY (`internal/pty`) that parses every byte to detect `/next`, Ctrl-], and a per-attempt output sentinel (`AGENT_RUNNER_CONTINUE_*`). The specs in this change retire that mechanism: the CLI child inherits the user's real terminal, and step completion travels over an out-of-band control channel. See `proposal.md` for motivation and the four delta specs for behavioral requirements.

Key existing seams (verified in code):

- `internal/exec/agent.go` — `interactiveRunnerFn = pty.RunInteractive` is the single swap point for the interactive execution path; `completionInstruction()`/`continueMarkerForContext()` generate the sentinel prompt text to be replaced; `ctx.SuspendHook`/`ctx.ResumeHook` wire the TUI coordinator; `recordSessionOnSpawn`/`discoverAndStoreSession` handle session persistence.
- `internal/liverun/coordinator.go` — `BeforeInteractive`/`AfterInteractive`/`PrepareForStep` implement release/restore with an idempotent skip between consecutive interactive steps; release and restore failures are currently logged and swallowed (must become fatal/surfaced per spec).
- `internal/cli` — every adapter already locates its CLI's native session store for `DiscoverSessionID` (Claude: `~/.claude/projects/<encoded-cwd>/<id>.jsonl`; Codex: `~/.codex/sessions/YYYY/MM/DD/*.jsonl`; Copilot: session dirs with `workspace.yaml`; Cursor: local chat store; OpenCode: storage files / `opencode db`).

This design was reviewed interactively; the completion state machine, job-control contract, endpoint lifecycle, durability-probe contract, and crash/drain handling below were dictated or approved explicitly during that review and are binding.

## Goals / Non-Goals

**Goals:**

- Zero terminal-byte interpretation for interactive agent steps; child behaves exactly as if launched from a shell (including Ctrl-Z).
- Structured, authenticated, idempotent step completion with turn-durability guarantees strong enough that a later `session: resume` step never silently misses the completing turn.
- One universal completion surface (absolute-path shell client) with per-CLI narrow pre-approval; no MCP server.
- Survive runner crashes without orphaning a CLI that owns the terminal.

**Non-Goals:**

- Terminal recording, Windows transport, cross-terminal force-advance, shell-step redesign beyond the specced hardening, Herdr changes (all per proposal Out of Scope).

## Approach

### Package layout

```text
internal/interactive/          (new package)
├── runner.go      # DirectRunner: orchestrates one interactive step end-to-end
├── control.go     # per-run Unix-socket ControlServer, JSON protocol, credential rotation
├── process.go     # child supervisor: wait ownership, job control, termination, watchdog
└── durability.go  # WaitForCommittedTurn orchestration over cli.TurnDurabilityProbe

internal/cli       # + TurnDurabilityProbe implementations per adapter
                   # + narrow completion-command pre-approval emission in BuildArgs
internal/exec      # agent.go: interactive branch calls interactive.Run; control-channel
                   # instructions replace completionInstruction/continueMarker
internal/liverun   # coordinator: error-returning release lease; restore errors surfaced;
                   # consecutive-interactive-step skip retained
cmd/agent-runner   # `step complete` client subcommand; `internal turn-committed` sender
                   # (used by CLI post-turn hooks); `internal watchdog` process
internal/pty       # narrowed to the interactive-shell relay per the pseudo-terminal delta
```

### Control plane

- **Endpoint**: one Unix domain socket per run in a user-private 0700 directory with a short path — `$TMPDIR/agent-runner-<uid>/<run-hash>.sock` — because `sun_path` is limited (~104 bytes on macOS) and run directories can exceed it. The run directory stores a pointer file to the socket path.
- **Lifecycle** (binding): start after the run lock is acquired, before the terminal is first released for an interactive step; keep alive across subsequent steps; rotate and invalidate the credential for every step attempt; reject and audit events when no interactive step is active; close and unlink on every normal runner exit path; on resume, remove a stale socket only after the run lock proves no previous runner owns it.
- **Child environment**: `AGENT_RUNNER_CONTROL_SOCKET`, `AGENT_RUNNER_RUN_ID`, `AGENT_RUNNER_STEP_ID`, `AGENT_RUNNER_CONTROL_TOKEN` (fresh single-use token per step attempt, UUID-derived like today's marker).
- **Protocol**: single-line JSON per message, 4 KiB message bound, 5 s read/write deadlines per connection.
  - Request: `{"type":"complete_step","run_id":…,"step_id":…,"token":…,"request_id":…}`
  - Response: `{"ok":true}` or `{"ok":false,"error":…}`
  - **Idempotent acknowledgement**: the client generates `request_id`; the server caches accepted `(attempt, request_id)` pairs, so a retry after a lost acknowledgement returns `{"ok":true}` rather than a stale-token rejection. Duplicate completions beyond that are no-ops per spec.
  - The `turn-committed` sender uses the same protocol with `{"type":"turn_committed",…}`, authenticated with the same attempt credential: "single-use" applies to completion acceptance only — the credential remains valid for `turn_committed` messages until the step concludes.
- The client subcommand `agent-runner step complete` reads only the env vars, dials, sends, retries once on lost ack, prints a confirmation, exits non-zero with guidance when the env vars are absent (in-session only, per spec).

### Completion state machine (binding)

```text
completion requested
  → validated, completion_requested audited, acknowledged   (intermediate event — NOT success)
  → checkpoint := probe.Checkpoint(sessionID)                (taken at accept time)
  → probe.WaitForCommittedTurn(ctx ≤ 30s active runtime, sessionID, checkpoint)
      ├─ committed → SIGTERM child pgid → ≤3s active runtime → SIGKILL → reap
      │              → outcome success → state flush → TUI restore decision → advance
      ├─ child exits while waiting → keep probing the native session store for the
      │             remaining bound (hook evidence can no longer arrive from an exited
      │             process) → committed: outcome success (no signals needed)
      │                        timeout: durability-failure path below
      └─ timeout  → durability_failure audited (CLI, session ID, timeout, inspected artifact)
                    → CLI terminated gracefully (same SIGTERM/SIGKILL ladder, if still running)
                    → outcome failed → workflow stops
                    (a resumed workflow issues a fresh attempt credential and retries normally)
```

Both the 30 s durability bound and the 3 s termination grace count **active runtime only**: the supervisor tracks stop/continue transitions and pauses both clocks while the job is suspended (Ctrl-Z between acceptance and termination must not consume them).

### Turn durability (design gate 1)

Adapter-owned semantic confirmation; timing is never evidence:

```go
type TurnDurabilityProbe interface {
    Checkpoint(sessionID string) (Checkpoint, error)
    WaitForCommittedTurn(ctx context.Context, sessionID string, after Checkpoint) error
}
```

Evidence hierarchy per adapter: (1) a native post-turn hook emitting `turn_committed` to the control socket, configured ephemerally for the spawned process only; (2) otherwise, inspection of the native session store for an **explicitly completed assistant turn** recorded after the checkpoint — a semantic record, not a file write or mtime; (3) if neither is provable, the adapter is **unresolved**: the durability wait fails honestly rather than disguising a sleep as durability.

### Job control (design gate 3, binding)

A single supervisor goroutine exclusively owns child waiting. Go-specific resolution of wait ownership: `exec.Cmd` is used only to construct and `Start()` the child; `cmd.Wait()` is **never called** (safe: inherited streams mean no pipe-copier goroutines). The supervisor loops on `unix.Wait4(pid, &status, unix.WUNTRACED|unix.WCONTINUED, nil)`. Phase 0 spikes this on macOS and Linux; `waitid(..., WNOWAIT)` is the fallback if `Wait4` misbehaves on either platform.

Spawn: `SysProcAttr{Setpgid: true, Foreground: true, Ctty: <controlling-tty fd>}` — Go sets the child's fresh process group as the terminal's foreground group *before exec returns*, eliminating the spawn/foreground race. Defensively, an initial SIGTTIN stop is treated as recoverable (re-assert foreground, SIGCONT), not fatal.

- **Child stops** (user Ctrl-Z at a cooked-mode moment, CLI self-suspend, external SIGSTOP): save the child's terminal modes → reclaim the foreground (`tcsetpgrp` to the runner's group) → restore the runner/shell-facing terminal modes → SIGSTOP the runner's **entire process group**, so the user's shell sees the whole job suspended.
- **`fg`**: on SIGCONT, verify the runner is foreground → restore the child's saved terminal modes → transfer foreground to the child's group → SIGCONT the child group.
- **`bg`**: do not resume the child, do not touch the terminal; stop again or wait until the runner is foreground.
- The runner neither exits on nor consumes terminal-generated signals while the child owns the foreground.

### Crash safety: watchdog and resume cleanup

Direct inheritance means a crashed runner would otherwise orphan a CLI that owns the terminal:

- **Watchdog**: after spawning the child, the runner spawns `agent-runner internal watchdog` with the child's pid/pgid/process-start-time as arguments and the read end of a pipe whose write end only the runner holds. Pipe EOF means the runner died: the watchdog verifies the child's identity (pid + start time — no PID-reuse kills), SIGTERMs the group, waits the grace, SIGKILLs, and exits. On normal completion the runner closes the pipe after reaping, and the watchdog verifies the child is gone and exits.
- **Resume cleanup**: the run state persists per-attempt metadata (`child_pid`, `pgid`, `start_time`, socket path). On resume, under the run lock: if a process with that pid exists **and** its start time matches, terminate the group gracefully before retrying; otherwise skip (PID reused). Then unlink the stale socket. Start time comes from `/proc/<pid>/stat` on Linux and `sysctl kern.proc.pid` on macOS.

### Five-CLI completion matrix (design gate 2)

Baseline for all five: the agent's shell tool runs the absolute-path client (path injected into the completion instructions via `os.Executable()`; socket and token travel in env vars, so the command takes no arguments). Cells marked *(P0)* are proven in Phase 0 before the main build — they are **blocking**: a CLI whose narrow approval cannot be expressed safely gets a documented alternative integration instead of broadened permissions; a CLI without provable turn evidence is unresolved per the durability rules.

| CLI | No-approval invocation (interactive) | Turn-committed evidence (probe) |
|---|---|---|
| Claude | `--allowedTools "Bash(<abs> step complete)"` — exact command only *(P0)* | Ephemeral `Stop` hook injected via `--settings`, running `agent-runner internal turn-committed` *(P0)*; fallback: completed assistant-turn record in `~/.claude/projects/<encoded>/<id>.jsonl` after checkpoint |
| Codex | Approval override scoped to the exact command via `-c` config *(P0)* | `notify` config on `agent-turn-complete` running the turn-committed sender *(P0)*; fallback: turn-end records in `~/.codex/sessions/**/<id>.jsonl` after checkpoint |
| Copilot | Granular `--allow-tool` shell scoping *(P0)* | Completed-turn record in its session-directory state *(P0)* |
| Cursor | Permissions config allowlist *(P0)* | Completed assistant message in the local chat store after checkpoint *(P0)* |
| OpenCode | `opencode.json` bash permission allow-pattern for the exact command *(P0)* | `opencode db` query for a completed assistant message after checkpoint *(P0)* |

Interactive pre-approval is governed by the `cli-adapter` delta spec: pre-approval may cover only the exact absolute executable path with fixed `step complete` arguments — no wildcards, chaining, or other subcommands. Autonomous-interactive invocations may additionally use each CLI's existing autonomous permission flags (unchanged).

User-typed `/next`: a thin custom command per CLI that supports custom commands (shipped through the agent-skills plugin or a new plugin; exact packaging chosen in Phase 0 alongside the matrix) that simply runs the client.

### Prompt and adapter integration

- `completionInstruction()`/`newContinueMarker()` in `internal/exec/agent.go` are replaced by control-channel instructions: how to run the absolute-path client, and that completion means the step is done. Injection rules are unchanged (interactive and autonomous-interactive yes, headless no; refresh on resume as today).
- `cli.BuildArgsInput` gains the completion-command descriptor (absolute path + fixed args) so adapters can emit their narrow pre-approval flags and any ephemeral hook/notify config.
- `ctx.SuspendHook` becomes error-returning (release lease); `runAgentProcess`'s interactive branch calls `interactive.Run(...)` with args, workdir, env additions, the control server handle, and the adapter's probe.

## Decisions

- **Adapter-owned semantic durability over quiescence watching** — post-event writes followed by silence can be the tool call being recorded, a generation pause, or background DB activity; only an explicit committed-turn record or post-turn hook is evidence. Timeout is a failure, not success: `success` must mean a later `session: resume` is safe.
- **Shell client + narrow allowlist over an MCP server** — one transport, no per-CLI MCP config injection, no second approval surface; MCP tools face their own approval prompts anyway. The carve-out is spec-bounded to the exact command.
- **Full job-control forwarding over auto-resume** — anything else breaks "behaves as if launched from a shell": auto-SIGCONT fights CLIs that deliberately self-suspend, and doing nothing leaves the terminal owned by a stopped group (hang).
- **Per-run lazy endpoint over per-step** — a sequential workflow has at most one active interactive step; per-attempt credentials provide the narrow lifetime boundary without rebinding churn.
- **Supervisor owns Wait4; `cmd.Wait()` unused** — one reaper, no status-stealing race with `os/exec`.
- **`SysProcAttr.Foreground` for spawn** — kernel-side foreground transfer before exec beats any userspace synchronization.
- **Watchdog as a separate process, pipe-EOF triggered** — the only mechanism that works when the runner dies abruptly, portable to macOS (no `PR_SET_PDEATHSIG` there), PID-reuse-safe via start-time verification.

## Risks / Trade-offs

- [Job-control code is subtle] → isolated in `process.go` with a scriptable-child state-machine test, a real-shell outer-PTY E2E, and the Phase 0 wait/foreground spike before anything builds on it.
- [Per-CLI approval syntax or turn-record format may not exist/work] → Phase 0 is blocking; failing cells get documented alternative integrations before the main build; cutover stays all-or-nothing.
- [CLI version drift changes session-store formats] → probe failures surface as loud durability failures, never silent success; the release skill runs the real-agent E2Es.
- [Ephemeral `--settings`/config injection may interact with user-level CLI config] → Phase 0 verifies injection is additive; if a CLI merges destructively, its adapter falls back to the session-store probe and documents the limitation.
- [Watchdog adds a process per interactive step] → negligible cost; it exits with the step.

## Migration Plan

Staged internally within this change (no public flag), per the proposal:

- **Phase 0 (blocking feasibility)**: wait-ownership + `Foreground` spike on macOS/Linux; prove all ten matrix cells (five approvals, five durability evidences) with minimal per-CLI scripts; choose and document alternative integrations for any failing cell; pick the `/next` plugin packaging.
- **Phase 1**: build `internal/interactive` control server + client subcommand + probes with unit tests; old path untouched.
- **Phase 2**: build DirectRunner + job control + watchdog; prove fidelity, durability, and completion on the extended fake-agent harness.
- **Phase 3**: switch `interactiveRunnerFn`; adapters emit pre-approval flags and control instructions; coordinator hardening lands.
- **Phase 4**: delete the PTY agent path (input parser, mouse tracker, sentinel scanner, idle hints) and rewrite marker-asserting tests against the control contract.
- **Phase 5**: harden the retained shell relay (process-group signaling, 1 s drain bound with defined timeout behavior, close ordering, error propagation); run the full deterministic + real-agent suites.

Rollback within the change: each phase is a revertable commit sequence; production behavior changes only at Phase 3.

## Testing

- **Unit**: control server (auth, rotation, idempotent retry, message bound, deadlines, no-active-step rejection); durability probes against fixture session stores per adapter; job-control state machine against a scriptable fake child; watchdog (pipe EOF, PID-reuse guard).
- **Deterministic integration** (existing fake-agent harness, extended): fake agent completes via the socket; all terminal-fidelity cases retained; release/restore failure injection; drain-timeout case for the shell relay.
- **Real-shell job-control E2E**: an outer PTY runs a real job-control shell, launches agent-runner as a foreground job, suspends (both cooperative self-suspend and external SIGSTOP), verifies the shell reports the job stopped, runs `fg`, verifies the child resumes with correct terminal modes.
- **Per-CLI durability E2E (gate 1 proof)**: step 1 tells the agent a unique recall phrase and the agent completes via the control channel; step 2 resumes the same session and asks the agent to repeat the phrase. The phrase appears **only** in step 1's prompt — never in step 2's prompt, environment, state summary, or fixture — so recall proves the completing turn survived. (Called a "recall phrase", not a nonce, to avoid collision with the completion credential.) Runs for all five CLIs.
- Existing five headless real-agent E2Es unchanged.

## Open Questions

None blocking design; all are Phase 0 deliverables with defined failure paths: the ten matrix cells, the Claude `--settings` merge behavior, the Wait4-on-macOS spike outcome, and the `/next` plugin packaging choice.
