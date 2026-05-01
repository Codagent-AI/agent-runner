## Why

Agent Runner currently supports four CLI backends, but two of them (Copilot, Cursor) reject interactive mode at runtime, and OpenCode — a popular open-source coding agent — has no adapter at all. This forces users onto Claude or Codex whenever a workflow step needs an interactive session, and locks out OpenCode users entirely. Closing these gaps lets workflows mix and match models and providers freely without restructuring around backend limitations.

- Lift "every registered adapter supports both interactive and headless modes" into a cross-cutting `cli-adapter` requirement and remove the per-adapter rejection requirements from Copilot and Cursor.
- Codify the existing runner-side rule that `AskUserQuestion` is blocked only in headless mode.
- Implement Copilot interactive-mode argument construction in the adapter and remove its `InteractiveRejector`.
- Implement Cursor interactive-mode argument construction in the adapter, including interactive session-ID discovery (the existing `stream-json` channel is `-p`-only), and remove its `InteractiveRejector`.
- Introduce a new `OpenCode` adapter supporting both headless and interactive modes, register it in the adapter registry, and accept `cli: opencode` in agent-profile and workflow configuration.

**Observable behavior change:** Copilot and Cursor steps in interactive mode currently fail at runtime with a documented "interactive mode not supported" error. After this change those steps run instead. Any workflow, test, or external tooling depending on those error messages will need to adapt. No existing headless behavior changes.

## Capabilities

The behavioral contract for "all CLIs work in both modes" is cross-cutting and lives in `cli-adapter`. Per-adapter specs only carry deltas for material differences. There is no new `opencode-cli-support` spec — OpenCode behavior is fully covered by the cross-cutting `cli-adapter` requirements (mirroring the precedent that Claude has no per-adapter spec).

### Modified Capabilities
- `cli-adapter`: ADD a requirement that every registered adapter supports both interactive and headless modes; ADD a requirement that no adapter emits permission-loosening flags in interactive mode (the human at the terminal supervises); ADD a requirement codifying that `AskUserQuestion` is blocked in headless and allowed in interactive; MODIFY the registry requirement to include `codex`, `cursor`, and `opencode` scenarios alongside the existing `claude` and `copilot` ones; MODIFY the system-prompt-capability requirement to declare Copilot, Cursor, and OpenCode all as "no support" (verified against each CLI's `--help`).
- `copilot-cli-support`: REMOVE the "Copilot interactive mode rejected at runtime" requirement.
- `cursor-cli-support`: REMOVE the "Cursor interactive mode rejected at runtime" requirement.

## Technical Approach

The `cli.Adapter` abstraction in `internal/cli/adapter.go` was built for this — adding a backend or a mode is a localized change. The bulk of the work is in the adapter layer and the config validator, with a small executor touchpoint in `internal/exec/agent.go` to make `AskUserQuestion` headless-only.

```text
                        ┌───────────────────────────────┐
                        │   internal/exec/agent.go      │
                        │   (mode-agnostic dispatch)    │
                        └───────────────┬───────────────┘
                                        │
                              cli.Adapter interface
                                        │
        ┌─────────────┬─────────────┬───┴──────────┬─────────────┬─────────────┐
        ▼             ▼             ▼              ▼             ▼             ▼
     claude         codex        copilot         cursor       opencode    (registry)
   (h + i now)   (h + i now)   (h now,         (h now,       (NEW —
                                add i)          add i)        h + i)
```

**Per-backend approach:**

- **Copilot interactive** — invoke `copilot` in interactive mode, dropping both `--allow-all` and `--autopilot` per the cross-cutting "no permission loosening in interactive mode" rule (the human at the terminal supervises). Reuse the existing filesystem-scan session-ID discovery (which is mode-agnostic). Remove `InteractiveModeError()` so the `InteractiveRejector` check in `agent.go:158` no longer fires for Copilot.
- **Cursor interactive** — invoke `agent` without `-p` (which also drops `--output-format stream-json`, `--force`, and `--trust`, all of which are `-p`-only). Cursor has no verified interactive session-ID channel, so the adapter declines to persist a filesystem-guessed chat ID; subsequent steps with `session: resume` against a Cursor agent are treated as fresh.
- **OpenCode (new)** — implement the full `Adapter` interface and register it. The design phase verified OpenCode's CLI surface against `opencode --help`: headless via the `opencode run` subcommand with `--format json` (every event carries a `sessionID` field), interactive via default `opencode --prompt`, session storage at `~/.local/share/opencode/storage/session_diff/ses_*.json`, model format `provider/model`, effort via `--variant` (run-only). See `design.md` for the full per-mode flag table and session-discovery approach.

**Key technical decisions:**

1. **No new optional adapter interfaces.** The existing set (`OutputFilter`, `StdoutWrapper`, `HeadlessResultFilter`, `SessionStore`, etc.) covers what the new modes need; OpenCode can opt in to whichever subset its output format requires.
2. **Cursor interactive declines to discover a session ID.** With no workspace-provenance signal, the adapter returns `""` rather than guessing; subsequent `session: resume` steps against a Cursor agent start fresh. Copilot's filesystem scan stays in place because Copilot session files include workspace metadata.
3. **`InteractiveRejector` stays in the abstraction.** Removing the rejector implementations from Copilot and Cursor leaves the interface in place for future backends that genuinely don't support interactive mode.

## Out of Scope

- Changing the `Adapter` interface itself or introducing new optional interfaces beyond what already exists.
- Changing how the runner attaches to or renders interactive sessions (PTY, TUI styling, suspend/resume hooks) — Cursor and Copilot interactive sessions go through the same path Claude/Codex already use.
- Reasoning-effort, model-selection, or tool-restriction surface area for OpenCode beyond what its CLI natively exposes; if OpenCode lacks an equivalent flag, the adapter ignores the input (the established pattern for Cursor's effort/disallowed-tools handling).
- Any change to built-in workflow YAML in `workflows/` to use the new backends.
- Concurrent-session disambiguation improvements for filesystem-scan session discovery (the Copilot implementation already logs a warning when ambiguous; matching behavior is sufficient).

## Impact

- **Code:** `internal/cli/copilot.go`, `internal/cli/cursor.go`, `internal/cli/adapter.go` (registry), new `internal/cli/opencode.go`, plus tests alongside each. `internal/config/config.go` `validCLI` map and the related error message at line 441.
- **Specs:** modified `openspec/specs/cli-adapter/spec.md` (cross-cutting), and REMOVE-only deltas against `openspec/specs/copilot-cli-support/spec.md` and `openspec/specs/cursor-cli-support/spec.md`.
- **Dependencies:** none expected. OpenCode session-state parsing may need YAML/JSON depending on its on-disk format; both are already in `go.mod`.
- **External requirements:** the `opencode` binary must be on `$PATH` for users who select it (mirrors the existing requirement for `claude`, `codex`, `copilot`, and `agent`).
- **User-visible:** `cli: opencode` becomes a valid value in profiles and workflows; `cli: copilot` and `cli: cursor` no longer fail at runtime when used in interactive steps.
