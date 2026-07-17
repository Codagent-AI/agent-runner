# Phase 0 findings

This document records the blocking process-supervision and adapter feasibility
evidence consumed by the direct-execution and completion/durability tasks.

## Environment

The adapter cells were checked on macOS with the locally installed CLIs on
2026-07-16. The process spikes were rerun on 2026-07-17 on Darwin 25.1.0
(arm64) and Linux 6.12.76 (arm64, Docker):

| CLI | Version |
|---|---|
| Claude Code | 2.1.212 |
| Codex CLI | 0.144.5 |
| GitHub Copilot CLI | 1.0.71 (1.0.70 during the original adapter capture) |
| Cursor Agent | 2026.07.16-899851b |
| OpenCode | 1.17.15 |

CLI help/config validation established that every injected flag or config key
is accepted by the corresponding installed binary. The checked-in adapter
tests preserve the exact emitted forms so version drift fails visibly.

## Process supervision

The same throwaway Go spike was compiled and run natively on macOS and inside
the repository's Linux development container. Both executions used inherited
streams; no `StdoutPipe`, `StderrPipe`, or Go pipe-copy goroutine existed.

### Wait ownership

The parent constructed the child with `exec.Command`, called `Start`, and then
exclusively reaped it with:

```go
for {
    pid, err := unix.Wait4(childPID, &status,
        unix.WUNTRACED|unix.WCONTINUED, nil)
    // classify stop, continue, and exit; cmd.Wait is never called
}
```

The child raised `SIGSTOP`, the supervisor observed it and sent `SIGCONT`, the
supervisor observed continuation, and the child exited with status 23. Both
platforms reported:

```text
WAIT4 PASS stop=true continue=true exit=23 cmd.Wait=false
```

Linux's `unix.WaitStatus` predicates classify the events directly. Darwin's
BSD status encoding needs a small normalization in the supervisor: the raw
`SIGSTOP` status (`0x117f` in this run) satisfies x/sys's `Continued` helper,
while the subsequent continuation status (`0x137f`) satisfies `Stopped` with
`StopSignal() == SIGCONT`. The `Wait4` syscall delivered each event exactly
once and no `waitid` fallback was needed; production code must normalize those
Darwin predicate results rather than treating the helpers as portable event
labels.

### Foreground spawn

The spike opened `/dev/tty`, inherited it for all child streams, and started a
fresh process group with the binding attributes:

```go
cmd.SysProcAttr = &syscall.SysProcAttr{
    Setpgid:   true,
    Foreground: true,
    Ctty:       int(tty.Fd()), // parent descriptor for Foreground
}
```

Immediately after `Start` returned, before waiting for the child, a
`TIOCGPGRP` query returned the child's pid/pgid on both platforms. The observed
results were:

```text
Darwin: FOREGROUND PASS child_pgid=61761 tty_foreground=61761 observed_immediately_after_start=true
Linux:  FOREGROUND PASS child_pgid=79 tty_foreground=79 observed_immediately_after_start=true
```

Reclaiming the foreground after the child exits requires the supervisor to
ignore `SIGTTOU` around `TIOCSPGRP`; without that normal job-control guard,
Darwin returns `EIO` to the background parent. Spawn itself showed no initial
`SIGTTIN` race on either platform.

### Parent-death watchdog

The parent created a pipe, kept only its write end, and passed the read end as
fd 3 to a sibling watchdog. Closing the writer (the same kernel EOF condition
produced when the parent dies) woke the watchdog. Before signaling, it compared
the expected process-start identity with a fresh lookup:

- Linux: field 22 (`starttime`) from `/proc/<pid>/stat`, parsed after the final
  `)` so spaces or parentheses in the command name do not shift fields.
- macOS: `unix.SysctlKinfoProc("kern.proc.pid", pid)` and
  `Proc.P_starttime` (seconds plus microseconds), which uses the
  `CTL_KERN/KERN_PROC/KERN_PROC_PID/<pid>` sysctl MIB.

With a matching identity the watchdog sent `SIGTERM` and the target was reaped
as signaled. With an intentionally wrong start time the watchdog exited
without signaling and a signal-0 probe confirmed that the target was still
alive. Both platforms reported the matching and mismatch passes:

```text
WATCHDOG MATCH PASS eof=true identity=<platform-start-time> signal=SIGTERM
WATCHDOG MISMATCH PASS identity_match=false signal_sent=false target_alive=true
```

## Completion and approval matrix

### Claude Code

- Exact approval: `--allowedTools "Bash(<quoted-absolute-path> step complete)"`.
  The rule contains no `:*` suffix, wildcard, connector, or substitution.
- Durability hook: an additive JSON value passed through `--settings` installs
  a process-local `Stop` command that invokes
  `<absolute-path> internal turn-committed`.
- `/next`: a generated, user-cache plugin loaded only for the spawned process
  via `--plugin-dir`. Its command contains the quoted absolute client command.
- Store fallback: a completed record is `type: "assistant"`,
  `message.role: "assistant"`, and a terminal `message.stop_reason`. Tool-use
  and paused-turn records are not terminal evidence.

### Codex CLI

- Alternative to a permission override: the fixed completion client runs as a
  normal command inside Codex's existing sandbox. The adapter does not set
  `approval_policy = "never"`, danger-full-access, or any other interactive
  permission override. This avoids treating an argv prefix rule as an exact
  full-command authorization.
- Durability hook: `-c
  'notify=["<absolute-path>","internal","turn-committed"]'`. `notify` is a
  top-level additive override and fires for `agent-turn-complete`.
- `/next`: no custom slash-command package is injected. Codex continues to use
  the universal absolute-path instruction.
- Store fallback: the explicit terminal record is an `event_msg` whose
  `payload.type` is `task_complete`. A final assistant message alone is not
  sufficient.

### GitHub Copilot CLI

- Exact approval: `--allow-tool=shell(<quoted-absolute-path> step complete)`.
  Copilot's permission help identifies this form as an exact shell-command
  match; the `:*` prefix form is deliberately absent.
- `/next`: the same generated process-local plugin format used for Claude is
  loaded with `--plugin-dir` and contains the absolute command.
- Store evidence: `~/.copilot/session-state/<id>/events.jsonl` records
  `assistant.turn_start`, one or more `assistant.message` events, and the
  semantic terminal record `assistant.turn_end`. Only `assistant.turn_end`
  after the checkpoint is accepted.

### Cursor Agent

- Exact approval: Cursor's `Shell(command:args)` permission syntax matches the
  command and argument patterns separately (the bare `Shell(commandBase)` form
  is a prefix allowlist and remains unusable). Verified against Cursor
  2026.07.16-899851b: with `Shell(<absolute-path>:step complete)` in
  `permissions.allow`, the exact command runs without prompting, the same
  command with an extra argument is rejected, and the exact command followed
  by `&&` chaining is rejected. Explicit user deny rules take precedence over
  allow rules per Cursor's documented semantics.
- Delivery: the adapter never modifies the user's files. It reads the user's
  `cli-config.json` (from `CURSOR_CONFIG_DIR` or `~/.cursor`), writes a
  private per-invocation copy with the narrow rule appended to
  `permissions.allow` (deny rules preserved), and sets `CURSOR_CONFIG_DIR` to
  the private directory for the spawned process only. Verified: Cursor's chat
  store follows `CURSOR_CONFIG_DIR`, so the private directory symlinks
  `chats` back to the user's real store — without the link, sessions are
  stranded per invocation and resume, discovery, and durability all break.
  Authentication is unaffected by the redirect (verified against a private
  directory containing only `cli-config.json`).
- Autonomous rules: under `autonomous_permission_mode: yolo` the adapter
  emits `--force` in both autonomous-headless and autonomous-interactive
  invocations. Conservative autonomous-interactive invocations proceed
  normally: the narrow pre-approval gives completion an approval-free path.
- Plugin: the adapter generates an isolated plugin in the user's cache and
  loads it for the spawned process with `--plugin-dir`. The plugin contains
  no hook of any kind; it consists of a manifest and a single slash command.
- `/next`: the isolated plugin includes `commands/next.md`, which instructs
  the agent to run the quoted absolute-path completion client
  (`<path> step complete`) and then finish its response. Completion otherwise
  relies on the same absolute-path instructions injected into the prompt as
  for other CLIs. There is no Stop hook, so no `internal turn-committed`
  signal exists for Cursor; durability is confirmed only by the store
  fallback below.
- Store fallback (the only durability evidence for Cursor): Cursor's
  `store.db` records assistant messages and tool exchanges as JSON blobs in
  the `blobs` table, but it has no terminal committed-turn marker — new
  assistant text and arbitrary tool results are persisted while a turn is
  still in progress, so neither is committed-turn evidence. The shipped probe
  is receipt-based: after the control server accepts a completion, the client
  prints `agent-runner completion receipt <request_id>` to stdout, and
  inspection of a live store confirmed Cursor persists the shell tool's
  captured stdout in the corresponding `role: "tool"` / `tool-result` blob
  (both the `result` field and `experimental_content` carry the full command
  output). The probe checkpoints the content-addressed row IDs at acceptance
  and succeeds only when a post-checkpoint tool-result row contains the exact
  receipt. Because the receipt is only printed after acceptance, its presence
  proves the store committed the completion exchange and everything recorded
  before it — causal, timing-free evidence. The receipt-free
  `WaitForCommittedTurn` fails immediately and honestly for Cursor.
  Residual limitation: assistant text emitted after the completion command in
  the same turn could still be clipped; the injected instructions tell the
  agent to run the command as the final action of the response. Queries use
  exponential backoff; database mtime and quiet periods are ignored.

### OpenCode

- Exact approval: a process-local `OPENCODE_PERMISSION` value contains one
  `bash` rule, `<quoted-absolute-path> step complete: allow`. It has no
  catch-all or wildcard. The installed config resolver accepted this inline
  form.
- `/next`: `OPENCODE_CONFIG_CONTENT` adds a `next` custom command whose shell
  interpolation runs the same quoted absolute command. Both environment
  overrides apply only to the spawned process.
- Store evidence: `opencode db` exposes assistant messages with
  `data.role: "assistant"`, `data.time.completed`, and `data.finish`. The probe
  requires a newly completed final assistant message and rejects
  `finish: "tool-calls"` as an intermediate record.

## Durability fixtures

Redacted fixtures under `internal/cli/testdata/durability/` retain the captured
record shapes for all five stores. Tests checkpoint the baseline, add an
intermediate/non-terminal record, and then add a semantic terminal record.
Every wait is context-bounded, and absence of evidence returns the context
error rather than converting elapsed time into success.

## Packaging choice

Native commands are injected per process instead of installed globally or
written into the target repository:

- Claude and Copilot load a generated cache plugin with the exact absolute
  command.
- Cursor loads its generated `/next` completion plugin, which carries no
  hooks.
- OpenCode receives an inline custom command through
  `OPENCODE_CONFIG_CONTENT`.
- Codex uses the universal completion instruction because its current plugin
  surface does not provide a `/next` command with stronger semantics than the
  baseline.

This keeps completion additive and avoids persistent edits to user config.
