## Context

The runner already has three adapters with well-established patterns:

- `claude` — pre-generated UUID passed via `--session-id`, rich flag set, supports system prompt and disallowed tools.
- `codex` — discovers session ID by parsing a JSON `thread.started` event from stream-json output; no system prompt support; ignores disallowed tools.
- `copilot` — headless-only; uses `--allow-all --autopilot -s`; discovers session ID by scanning `~/.copilot/session-state/` and matching on `workspace.yaml.cwd`; implements `InteractiveRejector`.

Cursor ships a coding-agent CLI (binary `agent`). Reverse-engineering the bundled JS (`~/.local/share/cursor-agent/versions/<ver>/index.js` and `7434.index.js`) revealed the full headless flag surface and the stream-json event schema. This design records the non-obvious decisions that came out of that investigation so the implementer doesn't need to re-derive them.

## Goals / Non-Goals

**Goals:**
- Support headless cursor steps end-to-end: fresh session, resume, session-ID persistence across steps.
- Match copilot's integration shape closely (headless-only + `InteractiveRejector`) so users who already run copilot workflows have a minimal-surprise experience.
- Keep the adapter small: no model resolution, no MCP/sandbox knobs, no streaming deltas.

**Non-Goals:**
- Interactive cursor support (cursor's interactive UI is out of scope; `InteractiveRejector` blocks it cleanly).
- Model base-name resolution (e.g. `sonnet` → `sonnet-4-thinking`). The validator cross-repo does this with `agent --list-models`; none of the agent-runner adapters do, and adding it here would create inconsistency.
- Reasoning-effort mapping — cursor has no equivalent flag.
- Tool-level restrictions — cursor has no equivalent flag.
- Streaming partial output (`--stream-partial-output`) — the runner captures whole process output; no adapter streams deltas.

## Approach

### Flag selection

From the full cursor CLI option surface:

| Flag | Decision | Reason |
|---|---|---|
| `-p, --print` | **Use always** | Required for any non-interactive run. Cursor gates several other flags behind it. |
| `--output-format stream-json` | **Use always** | Required for session-ID discovery. `text` gives no session ID. `json` emits only one terminal `result` event, losing the upfront `system/init` event used as a progress signal; stream-json is strictly more useful. Only valid with `-p`. |
| `-f, --force` | **Use always** | Equivalent to copilot's `--allow-all`; grants autonomous tool execution. |
| `--trust` | **Use always** | Cursor prompts to trust the workspace interactively otherwise; only accepted together with `-p`/headless. |
| `--resume=<id>` | **Use on resume** | Positional `[chatId]` is documented as optional (`--resume` alone resumes the latest chat). We always pass an explicit ID, so `--resume=<id>` (equals form) is unambiguous and matches the copilot style. |
| `--model <m>` | **Use on fresh only** | Dropped on resume, matching all three existing adapters. |
| `--sandbox`, `--approve-mcps`, `--allow-paths`, `--readonly-paths`, `--blocked-patterns` | **Skip** | Copilot's adapter skips the analogous knobs; add later on user demand. |
| `--stream-partial-output` | **Skip** | Token-level deltas not needed given whole-output capture. |
| `--skip-worktree-setup` | **Skip** | Not relevant to runner-driven flows. |
| `--yolo` | **Skip** | Alias of `--force`; no reason to prefer it. |

### Final command shape

- **Fresh headless:** `agent -p --output-format stream-json --force --trust [--model <m>] <prompt>`
- **Resume headless:** `agent -p --output-format stream-json --force --trust --resume=<id> <prompt>`

### Session-ID discovery

The bundled JS emits events like these on stdout when `--output-format stream-json` is active:

```jsonl
{"type":"system","subtype":"init","apiKeySource":"login","cwd":"/path","session_id":"chat-abc-123","model":"composer-1.5","permissionMode":"default"}
{"type":"assistant","message":{...},"session_id":"chat-abc-123","timestamp_ms":1234}
{"type":"tool_call","subtype":"started","session_id":"chat-abc-123",...}
{"type":"result","subtype":"success","duration_ms":42,"is_error":false,"result":"...","session_id":"chat-abc-123","request_id":"..."}
```

Every event carries `session_id`. The discovery strategy is: scan the captured stdout line by line, JSON-decode each line, and return the first object's `session_id` string. Tolerate non-JSON lines and lines without the field. This is **codex-style parsing with a different payload**; no filesystem scan is needed (unlike copilot) and no UUID is pre-generated (unlike claude).

Structurally this looks a lot like codex's `discoverCodexThreadFromOutput`: the implementer should feel free to model `cursor.go`'s discovery helper on that rather than on copilot's filesystem scanner.

### Headless-only enforcement

Two layers, both mirroring copilot:

1. **Runtime** — implement `cli.InteractiveRejector` so the runner's agent executor catches `mode: interactive` for `cli: cursor` and fails the step with a descriptive error. This is where the "headless only" rule is actually enforced.
2. **main.go headless map** — add `"cursor": true` to the map at `cmd/agent-runner/main.go:499` so headless-default behavior kicks in for cursor steps even when the profile doesn't specify a mode.

Configuration loading must still succeed even when a profile declares `cli: cursor` with `default_mode: interactive`; the failure is a runtime concern only. This matches the existing copilot scenario.

### Unsupported inputs

- `SystemPrompt` → `SupportsSystemPrompt()` returns `false`; the adapter ignores it and the runner's generic fallback (prepend to user prompt) applies, same as codex/copilot.
- `Effort` → no flag emitted, no error. Silently ignored.
- `DisallowedTools` → no flag emitted, no error. Silently ignored. (Codex uses the same pattern.)

## Decisions

### Why stream-json over json
`--output-format json` would be simpler (parse one object at the end) but it only emits the final `result` event, losing the upfront `session/init` event. That means on a process that crashes mid-run we'd have no session ID to persist, defeating the resume story. `stream-json` gives us session_id on the very first event.

### Why not pre-generate the session ID
Cursor's CLI has no flag to accept a client-provided session ID (unlike claude's `--session-id`). We must discover it from output. Pre-generating would require cursor to add a flag upstream — out of scope.

### Why the config name is `cursor`, not `cursor-agent`
The binary name is `agent` (confusing on its own) and the product name is Cursor. Config users will type `cli: cursor` mentally aligned with the product. The adapter's `BuildArgs` emits `agent` as argv[0]; the user never has to know about that.

### Why skip model resolution
The validator repo resolves base names like `sonnet` → `sonnet-4-thinking` via `agent --list-models`. None of the three existing agent-runner adapters do this — they pass `--model` through verbatim. Introducing resolution for cursor alone would create an inconsistency; introducing it uniformly belongs in a separate change that touches all four adapters and has its own spec. For this change, users configure cursor's exact model IDs (e.g. `gpt-5.3-codex`, `opus-4.6-thinking`, `composer-1.5`).

## Risks / Trade-offs

- **CLI output format drift.** If a future cursor release changes the stream-json event shape or removes `session_id` from the `system/init` event, discovery breaks. Mitigation: the discovery helper already tolerates missing fields and falls through to later events — any event type with `session_id` will do.
- **Authentication is out of band.** Like copilot, the user is responsible for logging in (`agent login`). The adapter does not attempt to detect or surface auth failures beyond the process's own exit code and error output.
- **Missing system-prompt support.** Users who rely on system prompts will see the generic fallback (prepend-to-user-prompt) behavior that codex and copilot already use. Not a regression.
- **No model resolution.** Users will hit confusing errors if they configure a non-existent model ID; cursor will surface its own error. Acceptable for phase 1; users already manage model IDs for the other adapters.
