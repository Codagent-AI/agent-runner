# Task: Build the control plane and prove feasibility

## Goal

Run the blocking Phase 0 feasibility spikes, finalize the five-CLI completion matrix, and implement the step control channel: the per-run Unix-socket server, per-attempt credentials, idempotent JSON protocol, client subcommands, completion state machine, audit events, and the `TurnDurabilityProbe` interface — all with unit tests. This is the foundation every later piece of the change builds on, and nothing here changes production behavior yet.

## Background

You MUST read these files before starting:

- `openspec/changes/make-pty-great/proposal.md` — why the PTY parser is being replaced by direct terminal handoff plus an out-of-band control channel
- `openspec/changes/make-pty-great/design.md` — the binding design; especially "Control plane", "Completion state machine", "Turn durability", "Job control", "Crash safety", and "Five-CLI completion matrix"
- `openspec/changes/make-pty-great/specs/step-control-channel/spec.md` — the behavioral contract for this task

### Phase 0 spikes (do these first; they are blocking)

Write the findings to `openspec/changes/make-pty-great/phase0-findings.md`. Later work consumes this document, so record concrete flag syntax, config snippets, and session-store record shapes — not just pass/fail.

1. **Wait ownership**: prove that spawning via `exec.Cmd.Start()` (never calling `cmd.Wait()`) and reaping in a supervisor loop via `unix.Wait4(pid, &status, unix.WUNTRACED|unix.WCONTINUED, nil)` observes stop, continue, and exit correctly on both macOS and Linux. Fallback if `Wait4` misbehaves on either platform: `waitid` with `WNOWAIT` for observation. Not calling `cmd.Wait()` is safe here because inherited streams mean no pipe-copier goroutines.
2. **Foreground spawn**: prove `syscall.SysProcAttr{Setpgid: true, Foreground: true, Ctty: <controlling-tty fd>}` places the child's fresh process group as the terminal's foreground group before exec returns (no SIGTTIN race), on both platforms.
3. **Watchdog**: prove the pipe-EOF pattern — a sibling process holding the read end of a pipe whose write end only the parent holds detects parent death via EOF and can verify a pid's identity by process start time before signaling (start time via `/proc/<pid>/stat` on Linux, `sysctl kern.proc.pid` on macOS).
4. **Approval cells (five CLIs)**: for each of Claude, Codex, Copilot, Cursor, and OpenCode, prove a narrow pre-approval that lets the agent run an exact absolute-path command with fixed arguments without a human approval prompt — and confirm broader forms (extra args, chaining, other subcommands) are NOT covered. Candidate mechanisms per the design matrix: Claude `--allowedTools "Bash(<abs> step complete)"`; Codex `-c` config approval override; Copilot granular `--allow-tool` shell scoping; Cursor permissions config allowlist; OpenCode `opencode.json` bash allow-pattern.
5. **Durability cells (five CLIs)**: for each CLI, prove a semantic committed-turn signal — either a native post-turn hook configured ephemerally (Claude: `Stop` hook via `--settings`; Codex: `notify` on `agent-turn-complete`) or an explicitly completed assistant-turn record in the native session store (Claude: `~/.claude/projects/<encoded-cwd>/<id>.jsonl`; Codex: `~/.codex/sessions/YYYY/MM/DD/*.jsonl`; Copilot: session dirs with `workspace.yaml`; Cursor: local chat store; OpenCode: `opencode db` query). File writes or mtimes are NOT evidence.
6. **`/next` packaging**: choose how the thin per-CLI `/next` custom commands ship (existing agent-skills plugin or a new plugin) and record the choice.

Binding rule: a CLI whose narrow approval cannot be expressed safely gets a documented alternative integration (never a broadened permission); a CLI without provable turn evidence is recorded as needing a documented alternative — a disguised sleep is never acceptable. Cells that fail get their alternative chosen and documented in the findings before this task completes.

### Control plane implementation

New package `internal/interactive/` (`control.go`, `durability.go`), plus subcommands in `cmd/agent-runner/`:

- **Endpoint**: one Unix socket per run at `$TMPDIR/agent-runner-<uid>/<run-hash>.sock` (user-private 0700 directory; `sun_path` is limited to ~104 bytes on macOS, which is why the socket does not live in the run directory). Write a pointer file to the socket path in the run directory. Lifecycle (binding): started after the run lock is acquired and before the terminal is first released for an interactive step; kept alive across steps; credential rotated and invalidated per step attempt; events rejected and audited when no interactive step is active; closed and unlinked on every normal exit path; on resume, a stale socket is removed only after the run lock proves no previous runner owns it.
- **Child environment contract**: `AGENT_RUNNER_CONTROL_SOCKET`, `AGENT_RUNNER_RUN_ID`, `AGENT_RUNNER_STEP_ID`, `AGENT_RUNNER_CONTROL_TOKEN` (fresh single-use UUID-derived token per step attempt).
- **Protocol**: single-line JSON per message; 4 KiB message bound; 5 s read/write deadlines per connection. Request `{"type":"complete_step","run_id":…,"step_id":…,"token":…,"request_id":…}`; response `{"ok":true}` or `{"ok":false,"error":…}`. The server caches accepted `(attempt, request_id)` pairs so a retry after a lost acknowledgement returns `{"ok":true}` instead of a stale-token rejection. A `{"type":"turn_committed",…}` message type exists for post-turn hooks, authenticated with the same attempt credential: "single-use" applies to completion acceptance only — the credential remains valid for `turn_committed` messages until the step concludes.
- **Client subcommands**: `agent-runner step complete` reads only the env vars, dials, sends with a generated `request_id`, retries once on lost acknowledgement, and exits non-zero with a message explaining it must run inside an interactive agent step session when the env vars are absent. `agent-runner internal turn-committed` sends the turn-committed message the same way (used by CLI hooks).
- **Probe interface** (in `internal/cli`):

  ```go
  type TurnDurabilityProbe interface {
      Checkpoint(sessionID string) (Checkpoint, error)
      WaitForCommittedTurn(ctx context.Context, sessionID string, after Checkpoint) error
  }
  ```

  Adapter implementations are NOT part of this task — implement the interface, the checkpoint type, and the orchestration.
- **Completion state machine** (`durability.go`, unit-tested against fake probes): accept → audit `completion_requested` → acknowledge (intermediate event only, never step success) → `Checkpoint` at accept time → `WaitForCommittedTurn` bounded at 30 s of active runtime (the clock must be pausable, for later suspension support) → on confirmation, signal readiness to terminate and record success; on timeout, audit `durability_failure` naming the CLI, session ID, timeout, and inspected artifact, and report failure. If the child exits while waiting, the wait continues against the native session store for the remaining bound (hook evidence can no longer arrive from an exited process): confirmation still yields success, an elapsed bound follows the durability-failure path.
- **Audit events**: `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`. Use the real `audit.EventLogger` interface (see `internal/audit/`) — never an empty interface.

### Conventions

TDD (failing test first for behavior); tests live next to the source package; use `google/go-cmp` for structured comparisons; local stubs/fakes, no mocking framework; `make fmt` before finishing; Go version from `go.mod`.

## Spec

### Requirement: Per-run control endpoint

Before spawning an interactive or autonomous-interactive agent step, the runner SHALL ensure a private, per-run local control endpoint exists, accessible only to the local user. The endpoint's address and the step's completion credential SHALL be exposed to the child process through environment variables. The endpoint SHALL be closed and removed when the run ends. If the endpoint cannot be created, the step SHALL fail before the CLI process is spawned, with a descriptive error.

#### Scenario: Control environment variables present in the child
- **WHEN** the runner spawns the CLI process for an interactive agent step
- **THEN** the child's environment contains the control endpoint address and the current step attempt's completion credential

#### Scenario: Endpoint creation failure fails the step
- **WHEN** the runner cannot create the control endpoint for a run
- **THEN** the interactive step fails with a descriptive error before the CLI process is spawned

#### Scenario: Endpoint is private to the local user
- **WHEN** the control endpoint exists for a run
- **THEN** it is not accessible to other users of the machine

### Requirement: Per-step completion credential

Each interactive step attempt SHALL receive a fresh single-use completion credential. The runner SHALL accept a completion event only when it carries the credential issued for the currently running step attempt. Completion events carrying a stale credential (from an earlier attempt), an unknown credential, or a malformed payload SHALL be rejected without advancing the workflow, and each rejection SHALL be recorded in the audit log. Single-use applies to completion acceptance: after a completion event is accepted, further completion events are handled per "Completion event semantics", while the same attempt credential SHALL remain valid for authenticating turn-durability evidence (such as a committed-turn signal) until the step concludes.

#### Scenario: Current credential accepted
- **WHEN** a completion event arrives carrying the credential issued for the currently running step attempt
- **THEN** the runner accepts the event and completes the step

#### Scenario: Stale credential rejected
- **WHEN** a completion event arrives carrying a credential issued for a previous step attempt
- **THEN** the runner rejects the event, does not advance the workflow, and records the rejection in the audit log

#### Scenario: Malformed event rejected
- **WHEN** a connection to the control endpoint delivers a payload that is not a well-formed completion event
- **THEN** the runner rejects it, does not advance the workflow, and records the rejection in the audit log

#### Scenario: Committed-turn evidence accepted after completion
- **WHEN** a committed-turn signal carrying the current attempt's credential arrives after the completion event was accepted but before the step concludes
- **THEN** the runner accepts it as turn-durability evidence rather than rejecting the credential as consumed

### Requirement: Completion event semantics

A valid completion event SHALL mark the currently running interactive step as `success`, conclude the completion handshake (see "Completion acknowledgement and turn durability"), trigger graceful termination of the CLI process (per `interactive-terminal-handoff`), and advance the workflow. Completion events are success-only: the event carries no outcome parameter. Duplicate completion events after the first accepted one SHALL be ignored. A completion event arriving when no interactive step is running SHALL be rejected.

#### Scenario: Agent-initiated completion advances the workflow
- **WHEN** the agent sends a valid completion event during an interactive step
- **THEN** the step outcome is `success`, the CLI is terminated gracefully, and the workflow advances to the next step

#### Scenario: Duplicate completion event ignored
- **WHEN** a second completion event arrives after one has already been accepted for the current step
- **THEN** the runner ignores it and the workflow state is unaffected

#### Scenario: Completion event with no active interactive step
- **WHEN** a completion event arrives while no interactive step is running
- **THEN** the runner rejects it and the workflow state is unaffected

### Requirement: Completion acknowledgement and turn durability

Because the completion event is sent from inside the agent's own turn, accepting it SHALL NOT immediately terminate the CLI, and acceptance SHALL be recorded as an intermediate audit event only — never as step success. The runner SHALL first acknowledge the accepted event to the submitting client, so the completion invocation can return to the agent. The runner SHALL then wait for semantic evidence that the CLI's turn is durably committed (an adapter-supplied committed-turn confirmation — a native post-turn signal or an explicitly completed assistant turn recorded in the CLI's native session store after acceptance; never file-write timing alone), bounded by a timeout that counts active runtime only (the clock pauses while the child is suspended). On confirmation, the runner terminates the CLI gracefully and records the step outcome as `success`. If the bound elapses without confirmation, the runner SHALL record a durability failure in the audit log — naming the CLI, session ID, timeout, and inspected artifact — terminate the CLI gracefully, record the step outcome as `failed`, and stop the workflow. If the CLI process exits after acceptance but before confirmation, the runner SHALL continue seeking committed-turn evidence from the CLI's native session store for the remainder of the bound (a post-turn signal can no longer arrive from an exited process); confirmation yields `success`, and an elapsed bound follows the durability-failure path. A resumed workflow issues a fresh attempt credential and retries the step normally.

#### Scenario: Client receives acknowledgement before termination begins
- **WHEN** a valid completion event is accepted
- **THEN** the submitting client receives a success acknowledgement before any termination signal is sent to the CLI's process group

#### Scenario: Durability confirmation times out
- **WHEN** a completion event has been accepted and no committed-turn confirmation arrives within the active-runtime bound
- **THEN** the runner records a durability failure naming the CLI, session ID, timeout, and inspected artifact, terminates the CLI gracefully, records the step outcome as `failed`, and stops the workflow

#### Scenario: CLI exits before durability is confirmed
- **WHEN** the CLI process exits after a completion event was accepted but before committed-turn confirmation
- **THEN** the runner continues the durability check against the CLI's native session store for the remainder of the bound, and the step outcome is `success` on confirmation or follows the durability-failure path otherwise

> Note: this task implements and unit-tests the state machine with fake probes and fake process handles. The scenarios above touching real CLI termination and workflow advancement become fully verifiable end-to-end once the direct-execution cutover task is also complete; the "Session resumable after completion" and "Resumed workflow retries after durability failure" scenarios from this requirement are likewise proven there.

### Requirement: Event types (audit-log-entries, modified)

The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `error`, and — for interactive-step control and supervision — `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, and `child_continued`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Control-plane events recognized
- **WHEN** the audit logger receives a `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, or `control_rejected` event during an interactive step
- **THEN** it writes the entry without error, as an intermediate event distinct from `step_end`

#### Scenario: Job-control events recognized
- **WHEN** the audit logger receives a `child_stopped` or `child_continued` event while an interactive step's child is suspended or resumed
- **THEN** it writes the entry without error

> Note: `child_stopped`/`child_continued` are emitted by the supervisor built in the direct-execution cutover task; this task registers all the event types and emits the control-plane ones.

### Requirement: In-session completion command

The runner binary SHALL provide a `step complete` subcommand that reads the control endpoint address and completion credential from the environment and sends a completion event. The command is in-session only: when the control environment variables are absent, it SHALL exit non-zero with a message explaining that it must be run from within an interactive agent step session. The command SHALL NOT provide cross-terminal run targeting.

#### Scenario: Command run inside an interactive step session
- **WHEN** `agent-runner step complete` runs in an environment containing the control variables for the currently running step attempt
- **THEN** a completion event is sent and the workflow advances with outcome `success`

#### Scenario: Command run outside a step session
- **WHEN** `agent-runner step complete` runs in an environment without the control variables
- **THEN** the command exits non-zero with a message explaining it must be run from within an interactive agent step session

## Done When

`openspec/changes/make-pty-great/phase0-findings.md` exists with all six spike results recorded (ten matrix cells proven or their alternatives documented, spikes 1–3 passing on macOS and Linux, `/next` packaging chosen). The control server, client subcommands, probe interface, state machine, and audit events exist with unit tests covering every scenario above at the unit level. `make test` and `make lint` pass, and the existing real-agent E2E suite (five interactive + five headless) still passes as a regression gate — production interactive execution is unchanged.
