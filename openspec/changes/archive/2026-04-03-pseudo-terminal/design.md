## Context

Agent Runner currently hardcodes Claude CLI invocation in `internal/exec/agent.go`, discovers session IDs by scanning `~/.claude/projects/` (race-prone heuristic), and uses a `.agent-runner-signal` file for interactive step control. The PTY POC in `cmd/pty-poc/` proves that hosting CLIs inside a pseudo-terminal works for both Claude and Codex, intercepting `/next` and keyboard shortcuts without a sideband file.

## Goals / Non-Goals

**Goals:**
- Support Claude and Codex as interchangeable CLI backends via a per-step `cli` field
- Replace the signal file mechanism with PTY-based continue interception for interactive steps
- Replace heuristic session ID discovery with deterministic methods where possible (Claude: `--session-id`; Codex headless: `--json` event parsing) and improve reliability for remaining cases (Codex interactive: CWD-matched filesystem scan)

**Non-Goals:**
- Workflow-level or project-level CLI defaults (future change)
- Configurable keyboard shortcut for continue (future change)
- Supporting CLIs beyond Claude and Codex

## Decisions

### CLI adapter interface (`internal/cli`)

Single interface with a method per concern. Each adapter encapsulates CLI-specific arg construction and session discovery.

```go
type Adapter interface {
    BuildArgs(opts BuildArgsOptions) []string
    DiscoverSessionID(opts DiscoverOptions) (string, error)
}

type BuildArgsOptions struct {
    Prompt    string
    Mode      StepMode
    SessionID string
    Model     string
}
```

A hard-coded registry (`map[string]Adapter`) is populated at init with `"claude"` and `"codex"`. A `Get(name string) (Adapter, error)` function returns the adapter or an error for unknown CLIs.

Validation of the `cli` field happens at load time in `Step.Validate()`. The list of known adapter names is passed as a `[]string` to avoid the model package importing `internal/cli`.

### Claude adapter

| Scenario | Command |
|---|---|
| Fresh interactive | `claude --session-id <uuid> <prompt>` |
| Fresh headless | `claude --session-id <uuid> -p <prompt>` |
| Resume interactive | `claude --resume <uuid> <prompt>` |
| Resume headless | `claude --resume <uuid> -p <prompt>` |
| Model override | `--model <m>` appended when specified |

Session discovery: the runner generates a UUID upfront and passes it via `--session-id`. The adapter's `DiscoverSessionID` returns this same UUID. Fully deterministic — no filesystem scanning.

### Codex adapter

| Scenario | Command |
|---|---|
| Fresh interactive | `codex --no-alt-screen <prompt>` |
| Fresh headless | `codex exec --json <prompt>` |
| Resume interactive | `codex resume --no-alt-screen <uuid> <prompt>` |
| Resume headless | `codex exec resume <uuid> <prompt>` |
| Model override | `-m <m>` appended when specified |

Session discovery (headless): `codex exec --json` emits `{"type":"thread.started","thread_id":"<uuid>"}` as its first JSONL event. The adapter parses `thread_id` from this event. Deterministic.

Session discovery (interactive): scan `~/.codex/sessions/YYYY/MM/DD/` for the most recent `.jsonl` file created after spawn time. The file's first line contains `{"type":"session_meta","payload":{"id":"<uuid>","cwd":"..."}}` — match on CWD to reduce false matches.

### PTY package (`internal/pty`)

Extracted from `cmd/pty-poc/main.go` into a standalone package with a clean entrypoint:

```go
func RunInteractive(cmd string, args []string, opts Options) (Result, error)

type Options struct {
    Env []string
}

type Result struct {
    ContinueTriggered bool
    ExitCode          int
}
```

Internal components (not exported):
- **PTY lifecycle** — creates PTY via `pty.StartWithSize`, sets stdin to raw mode, restores on exit
- **I/O proxy** — goroutine reads PTY master to stdout (with hint timer reset); main loop reads stdin through input processor
- **Input processor** — escape sequence state machine (`escNone`/`escSawEsc`/`escInCSI`/`escInStringSeq`), line buffer tracking, `/next` detection, keyboard shortcut detection. Bytes batched and flushed to preserve escape sequence integrity
- **Graceful termination** — on continue trigger: SIGTERM, 3 second timeout, then SIGKILL
- **Idle hint** — 800ms timer after last PTY output, draws dim/reverse bar at bottom row, clears on next output
- **Resize handler** — listens for SIGWINCH, propagates new dimensions to PTY via `pty.Setsize`
- **Terminal restore** — restores original termios state and clears terminal mode overrides on exit

### Execution flow changes

`ExecuteAgentStep` becomes:

1. Resolve CLI adapter from `step.CLI` (default `"claude"`)
2. Build prompt (interpolation + engine enrichment) — unchanged
3. Resolve session ID from state — unchanged
4. Generate new UUID if fresh session and adapter needs it (Claude)
5. Call `adapter.BuildArgs(...)` — replaces `buildAgentArgs()`
6. Emit audit event — unchanged (add `cli` to event data)
7. If headless: `runner.RunAgent(args)`, then `adapter.DiscoverSessionID(...)`, store in state
8. If interactive: `pty.RunInteractive(cmd, args, opts)`. If `result.ContinueTriggered`: outcome success, discover and store session ID. Otherwise: outcome aborted, print resume message, exit runner.

### Model changes

- `Step` gains `CLI string` field (`yaml:"cli,omitempty"`)
- `Workflow.Agent` field removed
- `Workflow.ApplyDefaults()` no longer sets `Agent`
- `ExecutionContext.AgentCmd` field removed
- `Step.Validate()` rejects `cli` on shell steps and validates `cli` against known adapter names

### Removals

- `StartAgent` from `ProcessRunner` interface
- `waitForSignalOrExit`, `cleanSignalFile`, `readSignalAction`, signal file constant
- `findConversationID`, `encodeCwd`, `discoverAndStoreSession`
- `.agent-runner-signal` file mechanism entirely

### What stays the same

- Session resolution (`session.ResolveResumeSession`, `session.ResolveInheritSession`)
- Engine prompt enrichment
- Audit event structure (with added `cli` field)
- Shell step execution
- Loop, sub-workflow, group execution

## Risks / Trade-offs

- **Codex interactive session discovery is best-effort** — Unlike Claude where the runner generates the UUID upfront, Codex interactive sessions require scanning `~/.codex/sessions/`. Mitigated by matching CWD from `session_meta` in the JSONL. If Codex adds `--session-id` in the future, the adapter can switch to deterministic.
- **Codex headless requires `--json` flag** — Changes the output format. The adapter must parse JSONL and extract `thread_id` from the `thread.started` event. If Codex changes the event schema, parsing breaks.
- **PTY adds complexity to interactive steps** — Raw terminal mode, escape sequence parsing, and signal handling. Mitigated by the POC proving the approach and isolating the code in a testable `internal/pty` package.
- **3 second SIGTERM timeout** — A reasonable default. Can be adjusted without spec changes since it's an implementation detail.
- **Breaking change: `agent` field removal** — Low impact; it only existed as a default that was always `claude`.

## Migration Plan

1. Remove `Workflow.Agent` field — update all in-repo workflow YAML files to remove `agent:`.
2. Remove signal file code and `/continue` skill entirely.
3. `cmd/pty-poc/` can be removed after the PTY package is proven in production.
