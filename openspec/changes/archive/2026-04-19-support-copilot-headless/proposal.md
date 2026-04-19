## Why

GitHub Copilot CLI is a third viable agent backend alongside Claude and Codex. Users want to run headless workflow steps against it without pre-provisioning a separate wrapper.

## What Changes

- Register a new `copilot` CLI adapter in the compile-time registry alongside `claude` and `codex`.
- Support `session: new` and `session: resume` for copilot agent steps in **headless** mode.
- Map step/profile inputs to copilot CLI flags: `-p` (prompt), `--allow-all-tools` (required for non-interactive), `--output-format json` (for session discovery), `--resume=<id>` (for resume), `--model`, `--reasoning-effort`, and `--no-ask-user` (from `DisallowedTools: ["AskUserQuestion"]`).
- Discover the session ID by parsing the final `{"type":"result", "sessionId":"..."}` JSONL event from stdout.
- Fail at runtime with a clear error when a copilot step is invoked in interactive mode. Interactive copilot support is out of scope for this change.
- `CopilotAdapter.SupportsSystemPrompt()` returns `false` (no native `--append-system-prompt` equivalent); callers fall back to prompt wrapping, matching the Codex pattern.

## Capabilities

### New Capabilities
- `copilot-cli-support`: headless CLI adapter for GitHub Copilot — arg construction, session resume, session ID discovery from JSONL, interactive-mode rejection.

### Modified Capabilities
- `cli-adapter`: registry now includes `copilot`; system-prompt capability declaration extended to cover the copilot adapter.

## Out of Scope

- Interactive (PTY) copilot steps. The adapter rejects at runtime if invoked with `Headless=false`; adding interactive support is a future change.
- Auto-generation of a default copilot profile. Users opt in manually via `.agent-runner/config.yaml`.
- Pre-assigning a session UUID before spawn (like the Claude adapter does via `--session-id`). Copilot adapter uses post-exit discovery exclusively (matching the Codex fresh-session pattern).
- Non-default permission posture. The adapter always passes `--allow-all-tools` because copilot requires it for non-interactive mode; finer-grained `--allow-tool` / `--deny-tool` control is not exposed.

## Impact

- Code: new `internal/cli/copilot.go` adapter + tests; registry entry in `internal/cli/adapter.go`.
- Config validation (`internal/config/`): accept `cli: copilot` as a valid profile value.
- No changes to executor, state, or audit shape — copilot flows through the existing `Adapter` interface.
- No new external dependencies (copilot CLI must be installed on the user's PATH).
