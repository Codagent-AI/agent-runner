## Context

Agent Runner exposes a `cli.Adapter` interface (`internal/cli/adapter.go`) that constructs CLI invocation args for each backend. Today's registry contains four adapters: `claude`, `codex`, `copilot`, `cursor`. Two of them — Copilot and Cursor — implement `cli.InteractiveRejector` and fail any interactive step at runtime. OpenCode has no adapter. The behavioral specs for this change have already been settled (`openspec/changes/more-agents/specs/`):

- `cli-adapter` — adds a cross-cutting "every adapter supports both modes" requirement, codifies that `AskUserQuestion` is blocked only in headless, and extends registry / system-prompt-capability requirements for the new CLI.
- `copilot-cli-support` — REMOVE the interactive-rejection requirement.
- `cursor-cli-support` — REMOVE the interactive-rejection requirement.

This design fills in the architectural decisions: exact CLI flags per adapter per mode, session-ID discovery for the new interactive paths, and a new cross-cutting rule banning permission-loosening flags in interactive mode.

## Goals / Non-Goals

**Goals:**
- Make `cli: copilot` and `cli: cursor` work in interactive mode.
- Add `cli: opencode` as a fully wired adapter (both modes).
- Specify exact flag mappings per adapter per mode, with no `BuildArgsInput` field unaccounted for.
- Define session-ID discovery for the three new interactive code paths.
- Pin adapter-side `SupportsSystemPrompt()` stances (closes deferred-to-design markers in the spec).

**Non-Goals:**
- Changing the `cli.Adapter` interface itself or its optional protocols.
- Changing how the runner attaches to interactive sessions (PTY, suspend/resume hooks, TUI rendering).
- Reworking existing per-adapter specs beyond REMOVE-ing the rejection requirements.
- Per-CWD scoping for OpenCode's interactive session-ID discovery (deferred — see Risks).

## Approach

### Architecture (no interface changes)

```
                ┌───────────────────────────────┐
                │   internal/exec/agent.go      │
                │   (mode-agnostic dispatch)    │
                └───────────────┬───────────────┘
                                │
                      cli.Adapter (unchanged)
                                │
   ┌──────────┬──────────┬──────┴─────┬──────────┬──────────┐
   ▼          ▼          ▼            ▼          ▼          ▼
 claude     codex      copilot      cursor    opencode   registry
                       (+ inter.)  (+ inter.)  (NEW)
```

The `cli.Adapter` interface, `BuildArgsInput` struct, and all optional protocols (`OutputFilter`, `StdoutWrapper`, `HeadlessResultFilter`, `InteractiveRejector`, `SessionStore`) stay as-is. `InteractiveRejector` is retained on the interface so future backends that genuinely don't support interactive mode can opt in; Copilot and Cursor simply stop implementing it.

### Per-adapter flag mapping

Every `BuildArgsInput` field is accounted for in both modes. Flags marked **(NEW)** are introduced by this change.

#### Copilot

| Field           | Headless (existing)         | Interactive (NEW)           |
|-----------------|-----------------------------|-----------------------------|
| Prompt          | `-p <prompt>`               | `-i <prompt>`               |
| SystemPrompt    | ignored                     | ignored                     |
| SessionID+Resume| `--resume=<id>`             | `--resume=<id>`             |
| Model (fresh)   | `--model <m>`               | `--model <m>`               |
| Effort (fresh)  | `--reasoning-effort <e>`    | `--reasoning-effort <e>`    |
| Headless toggle | `-p` + `-s`                 | (omit `-p`/`-s`)            |
| Autonomy        | `--allow-all` + `--autopilot` | (none — see Decisions)    |
| DisallowedTools | `--no-ask-user`             | (empty — runner-side gate)  |

#### Cursor (`agent`)

| Field           | Headless (existing)                              | Interactive (NEW)            |
|-----------------|--------------------------------------------------|------------------------------|
| Prompt          | positional                                       | positional                   |
| SystemPrompt    | ignored                                          | ignored                      |
| SessionID+Resume| `--resume=<id>`                                  | `--resume=<id>`              |
| Model (fresh)   | `--model <m>`                                    | `--model <m>`                |
| Effort          | ignored (no CLI flag)                            | ignored                      |
| Headless toggle | `-p` + `--output-format stream-json` + `--trust` | (omit all three)             |
| Autonomy        | `--force`                                        | (none — see Decisions)       |
| DisallowedTools | ignored (no CLI flag)                            | ignored                      |

#### OpenCode (NEW adapter)

OpenCode uses a subcommand split: headless invokes `opencode run`, interactive invokes default `opencode`. Several flags exist only on `run`.

| Field           | Headless (`opencode run`)               | Interactive (`opencode`)        |
|-----------------|-----------------------------------------|---------------------------------|
| Prompt          | positional `<message>`                  | `--prompt <text>`               |
| SystemPrompt    | ignored                                 | ignored                         |
| SessionID+Resume| `-s <id>`                               | `-s <id>`                       |
| Model (fresh)   | `--model <provider/model>`              | `--model <provider/model>`      |
| Effort (fresh)  | `--variant <e>`                         | ignored (run-only flag)         |
| Headless toggle | `run` subcommand + `--format json`      | omit `run`                      |
| Autonomy        | `--dangerously-skip-permissions`        | (none — flag is run-only)       |
| DisallowedTools | ignored (no CLI flag)                   | ignored                         |

### Session-ID discovery

| Adapter / mode             | Approach                                                                                          |
|----------------------------|---------------------------------------------------------------------------------------------------|
| Copilot — both modes       | Existing filesystem scan: `~/.copilot/session-state/<id>/workspace.yaml` matched on CWD + spawn time. Mode-agnostic; no new code. |
| Cursor headless            | Existing JSONL parse of `session_id` from `--output-format stream-json`. Unchanged.               |
| Cursor interactive (NEW)   | No verified session-ID channel; adapter declines to guess and returns `""`. Subsequent `session: resume` against a Cursor agent therefore starts fresh. (Original design proposed a filesystem scan of `~/.cursor/chats/*/<chat-uuid>/store.db`; dropped during implementation review.) |
| OpenCode headless (NEW)    | Parse `sessionID` field from `opencode run --format json` JSONL events. Verified live: every event carries `"sessionID":"ses_..."`. |
| OpenCode interactive (NEW) | Filesystem scan of `~/.local/share/opencode/storage/session_diff/ses_*.json` by mtime, filtered by spawn time, newest match. |

### Permission posture in interactive mode

New cross-cutting requirement (added to `cli-adapter` spec delta):

> In interactive mode, no adapter SHALL emit a flag that bypasses or pre-approves the underlying CLI's permission/approval prompts. The human at the terminal supervises permissions.

This drives the flag-table cells marked "(none)" above. It also confirms that existing Claude and Codex interactive invocations (which already pass no permission flags) remain compliant.

### `SupportsSystemPrompt()` stances (closes deferred-to-design markers)

Verified against `--help` output for each binary: none of `copilot`, `agent` (cursor), or `opencode` exposes a system-prompt flag. All three adapters declare `SupportsSystemPrompt() == false`. The runner's existing fallback (prepending system-prompt content to the user prompt) applies for these adapters.

| Adapter   | Stance        | Evidence                                              |
|-----------|---------------|-------------------------------------------------------|
| Claude    | true          | `--append-system-prompt` exists.                      |
| Codex     | false         | No flag in `--help`.                                  |
| Copilot   | false         | No flag in `--help`.                                  |
| Cursor    | false         | No flag in `--help`.                                  |
| OpenCode  | false         | No flag in `--help`.                                  |

## Decisions

1. **No permission-loosening flags in interactive mode (cross-cutting).** Lifted into `cli-adapter` spec. Rationale: the human at the terminal supervises; auto-grant flags pre-empt that supervision. Concrete effect: drop `--allow-all` from Copilot interactive, drop `--force` from Cursor interactive. Headless invocations keep their autonomy flags (no human present).

2. **Copilot prompt delivery in interactive: `-i <prompt>` not stdin.** `copilot --help` documents `-i, --interactive <prompt>` for exactly this purpose ("Start interactive mode and automatically execute this prompt"). No need for stdin plumbing.

3. **Cursor session-ID discovery in interactive: filesystem mtime scan, not `agent create-chat` pre-generation.** Considered using `agent create-chat` to mint a UUID up front and pass it via `--resume` (Claude's pattern). Rejected for now because `create-chat` doesn't take a `--workspace` flag in `--help`, so the chat may be created in the wrong workspace context. Reconsider as a fallback if mtime scans prove unreliable.

4. **OpenCode subcommand split is honored at the adapter level.** `opencode run` for headless, default `opencode` for interactive. The adapter's `BuildArgs` switches on `input.Headless` to choose the subcommand. This is the only adapter whose mode toggle is a subcommand, not a flag.

5. **OpenCode `--variant` and `--dangerously-skip-permissions` are `run`-only.** The interactive command silently ignores effort (`--variant`) — same pattern as Cursor's effort-ignored requirement. No interactive autonomy flag exists; the cross-cutting permission rule makes that a non-issue.

6. **`InteractiveRejector` interface stays.** Removing the Copilot and Cursor implementations leaves the interface unused, but it's cheap and future-proofs the abstraction for a backend that genuinely lacks interactive mode.

## Risks / Trade-offs

- **[Cursor interactive session-ID ambiguity]** → Filesystem mtime scan may pick the wrong chat under concurrent sessions in the same workspace. Mitigation: log a warning when multiple post-spawn-time candidates exist (matches the existing Copilot pattern).

- **[OpenCode interactive — no per-CWD scoping]** → The `~/.local/share/opencode/storage/session_diff/ses_*.json` filenames don't carry CWD; matching is by mtime only. Mitigation: same warn-on-ambiguity pattern. A follow-up could query `~/.local/share/opencode/opencode.db` for CWD scoping.

- **[OpenCode interactive — no effort flag]** → `--variant` is `run`-only, so any `effort:` profile setting is silently dropped in interactive opencode steps. Documented limitation (matches Cursor's effort handling). The workflow still runs.

- **[Behavior change for Claude]** → None. Claude already passes no permission flags in interactive; the new cross-cutting rule codifies the status quo for it.

- **[Behavior change for Copilot/Cursor pre-this-change]** → They never ran interactively at all (rejected at runtime), so users had no pre-existing expectation. The "no permission loosening" rule applies to first-day behavior.

## Migration Plan

Code-only change. No state migration. Rollout is a normal release.

**Steps:**
1. Update `cli-adapter` spec delta to add the new "No permission loosening in interactive mode" requirement (this design surfaced it).
2. Implement Copilot interactive arg construction in `internal/cli/copilot.go`; remove `InteractiveModeError()`.
3. Implement Cursor interactive arg construction in `internal/cli/cursor.go`; remove `InteractiveModeError()`; add interactive session-ID discovery helper (filesystem scan).
4. Add `internal/cli/opencode.go` with both modes, register in `internal/cli/adapter.go`.
5. Add `opencode` to `internal/config/config.go` `validCLI` map and update the related error message.
6. Update tests:
    - Remove `InteractiveRejector` tests for Copilot and Cursor in `internal/cli/adapter_test.go`.
    - Update `internal/exec/agent_test.go` interactive-rejection scenarios.
    - Add interactive-mode arg-construction tests for Copilot, Cursor, OpenCode.
    - Add full OpenCode adapter test suite (mirroring Copilot's coverage).

**Rollback:** revert. No data migration to undo.

**External requirements:** `opencode` must be on `$PATH` for users selecting it.

## Open Questions

- None blocking. The Cursor `agent create-chat` fallback path for session-ID discovery is a known recoverable option if the mtime scan proves unreliable in practice.
