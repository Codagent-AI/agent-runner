# Phase 0 findings

This document records the adapter feasibility evidence consumed by the
completion/durability task. The process-supervision spikes (wait ownership,
foreground spawn, and watchdog behavior) belong to the control-plane task and
are not repeated here.

## Environment

The adapter cells were checked on macOS with the locally installed CLIs on
2026-07-16:

| CLI | Version |
|---|---|
| Claude Code | 2.1.212 |
| Codex CLI | 0.144.5 |
| GitHub Copilot CLI | 1.0.70 |
| Cursor Agent | 2026.07.16-899851b |
| OpenCode | 1.17.15 |

CLI help/config validation established that every injected flag or config key
is accepted by the corresponding installed binary. The checked-in adapter
tests preserve the exact emitted forms so version drift fails visibly.

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

- Failed candidate: Cursor's `Shell(commandBase)` permission entry is a prefix
  allowlist. Inspection of the installed implementation confirms that an
  allowed full pattern also matches strings beginning with that pattern plus a
  space. `Shell(<path> step complete)` would therefore also approve extra
  arguments and is not emitted.
- Alternative integration: the adapter generates an isolated plugin in the
  user's cache and loads it for the spawned process with `--plugin-dir`. Its
  `stop` hook invokes `step complete` and then `internal turn-committed` using
  the absolute executable supplied in a process-local environment variable.
  User and project settings are neither replaced nor modified.
- `/next`: the isolated plugin includes `commands/next.md`; invoking it ends
  the response, after which the same Stop hook routes completion through the
  control channel.
- Store fallback: Cursor's `store.db` contains complete assistant messages as
  JSON blobs. The probe hashes the semantic assistant objects at checkpoint
  time and requires a new assistant object afterward; database mtime and quiet
  periods are ignored.

### OpenCode

- Exact approval: a process-local `OPENCODE_PERMISSION` value contains one
  rule, `<quoted-absolute-path> step complete: allow`. It has no catch-all or
  wildcard. The installed config resolver accepted this inline form.
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
- Cursor loads its generated completion/Stop-hook plugin.
- OpenCode receives an inline custom command through
  `OPENCODE_CONFIG_CONTENT`.
- Codex uses the universal completion instruction because its current plugin
  surface does not provide a `/next` command with stronger semantics than the
  baseline.

This keeps completion additive and avoids persistent edits to user config.
