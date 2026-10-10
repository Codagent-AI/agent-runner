# Task: Remove runtime terminal relays and use direct shell handoff

## Goal

Delete the obsolete runtime byte-proxy package and run interactive shell steps through the same direct terminal process and job-control supervisor used by interactive agents.

## Requirements

- Extract a reusable direct terminal-process entry point in `internal/interactive` without coupling shell steps to agent completion or turn durability.
- Run interactive shell commands as `sh -c <command>` with inherited stdin, stdout, and stderr, a dedicated foreground process group, full Ctrl-Z/`fg`/`bg` handling, verified watchdog cleanup, and TUI release/restore.
- Preserve shell-native outcomes: exit code 0 is `success`; nonzero is `failed`; `workdir` remains honored; `mode: interactive` with `capture` remains invalid.
- Do not proxy or retain interactive shell output. Its `step_end` contains command metadata, exit code, and outcome but no `stdout` transcript.
- Delete the production terminal relay, parser, transcript buffer, resize forwarding, drain logic, and their obsolete tests. Keep `creack/pty` only in E2E test code that must synthesize a user terminal.
- Add an E2E that records the outer terminal device and proves the shell child inherits that exact device on stdin, stdout, and stderr.
- Update public docs, canonical specs, and this change's artifacts so all interactive terminal steps consistently use direct handoff.

## Done When

No production package imports a terminal-relay library; no runtime `internal/pty` package remains; targeted shell and interactive supervisor tests pass; the direct-shell terminal-identity E2E passes; `make test`, `make lint`, and `make build` pass.
