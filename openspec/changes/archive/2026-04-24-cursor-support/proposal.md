## Why

The runner supports three CLI backends today — `claude`, `codex`, and `copilot`. Cursor's coding agent ships as a standalone CLI (binary name `agent`) and users want to drive it through agent-runner for the same reasons they drive the others: workflow orchestration, session resume across steps, named sessions, and parallel dispatch. Cursor's CLI has a stable headless mode (`-p` + `--output-format stream-json`) that emits a `session_id` on every event, so the integration slots into the existing adapter shape without new abstractions.

## What Changes

- Add a `cursor` CLI adapter in `internal/cli/` alongside `claude.go`, `codex.go`, `copilot.go`.
- Register `cursor` in the adapter registry (`internal/cli/adapter.go`).
- Add `cursor` to the valid CLI allowlist in `internal/config/config.go` and update the related error message.
- Add `cursor` to the headless-only CLI map in `cmd/agent-runner/main.go` (matches copilot).
- Adapter constructs headless-only invocations using `agent -p --output-format stream-json --force --trust [--model <m>] [--resume=<id>] <prompt>`.
- Adapter implements `InteractiveRejector` so `cli: cursor` + `mode: interactive` fails at runtime with a descriptive error (matches copilot's behavior).
- Adapter implements `DiscoverSessionID` by parsing the `session_id` field from the first JSON line on the captured process output (codex-style parsing, different payload).
- `SupportsSystemPrompt()` returns `false` (no native cursor flag for it); `DisallowedTools` is silently ignored (cursor has no tool-level restriction flag today).

## Capabilities

### New Capabilities
- `cursor-cli-support`: defines how the runner integrates with the Cursor CLI (`agent`) as a headless-only agent backend, covering invocation flags, session resume, session ID discovery, and interactive-mode rejection.

### Modified Capabilities
<!-- none -->

## Out of Scope

- Interactive (TTY) cursor sessions. Cursor's interactive UI is not supported in this release; attempting interactive mode fails at runtime, matching copilot. Adding interactive support later would be a separate change.
- Model base-name resolution (e.g. mapping `sonnet` → `sonnet-4-thinking` via `agent --list-models`). Users must configure Cursor's exact model IDs. The other three adapters also pass `--model` through verbatim; introducing a resolution layer belongs in a separate change that touches all adapters uniformly.
- Reasoning-effort mapping. Cursor's CLI has no `--reasoning-effort` (or equivalent) flag, so `effort` values on profiles targeting cursor are silently ignored.
- MCP auto-approval (`--approve-mcps`), path restrictions (`--allow-paths`, `--readonly-paths`, `--blocked-patterns`), and sandbox mode (`--sandbox`). Copilot's integration skips the analogous knobs too; these can be added incrementally if users ask.
- Streaming partial text deltas (`--stream-partial-output`). The runner captures whole process output today; no other adapter streams token-level deltas.

## Impact

- **New file**: `internal/cli/cursor.go` (adapter + session-ID parsing helper).
- **New file**: `internal/cli/cursor_test.go` (or new `TestCursorAdapter` block in `adapter_test.go`, matching the existing convention).
- **Modified**: `internal/cli/adapter.go` (registry entry).
- **Modified**: `internal/config/config.go` (validCLI map, error message listing valid CLIs).
- **Modified**: `internal/config/config_test.go` if existing tests pin the error-message contents.
- **Modified**: `cmd/agent-runner/main.go` (headless-only CLI map).
- **Modified**: `internal/exec/agent_test.go` (add an interactive-rejection test for cursor mirroring the existing copilot test at line 1036).
- **Dependencies**: none. The `agent` CLI is assumed to be installed and authenticated by the user, just like copilot.
