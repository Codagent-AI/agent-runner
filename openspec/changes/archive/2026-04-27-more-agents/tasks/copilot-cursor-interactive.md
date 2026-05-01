# Task: Enable interactive mode for Copilot and Cursor

## Goal

Make `cli: copilot` and `cli: cursor` work in interactive mode. Both adapters currently implement `cli.InteractiveRejector` and fail any interactive step at runtime. After this task, interactive steps using either CLI run normally, with arg construction matching the design's flag tables and (for Cursor) a new filesystem-scan helper for session-ID discovery.

## Background

Agent Runner's `cli.Adapter` interface (`internal/cli/adapter.go`) constructs CLI invocation args for each backend. Today, `copilot` and `cursor` both refuse interactive mode through `InteractiveModeError()`, returned from their `cli.InteractiveRejector` implementations. The runner's interactive-rejection check lives at `internal/exec/agent.go:158`. After this change, the rejector implementations are removed; the existing check still applies to any future adapter that opts in.

The cross-cutting cli-adapter spec already states that every registered adapter must support both modes, and adds a new rule: in interactive mode no adapter may emit a permission-loosening flag (because the human at the terminal supervises). Headless invocations keep their autonomy flags. The runner-side gate at `internal/exec/agent.go:361-365` already populates `DisallowedTools = ["AskUserQuestion"]` only when `headless=true`; both adapters' interactive args therefore see an empty `DisallowedTools` automatically.

### Files you must read

- `openspec/changes/more-agents/proposal.md` — change motivation
- `openspec/changes/more-agents/design.md` — full flag tables and decisions
- `openspec/changes/more-agents/specs/cli-adapter/spec.md` — cross-cutting requirements
- `openspec/changes/more-agents/specs/copilot-cli-support/spec.md` — REMOVED rejection requirement
- `openspec/changes/more-agents/specs/cursor-cli-support/spec.md` — REMOVED rejection requirement
- `internal/cli/adapter.go` — `BuildArgsInput`, `Adapter`, `DiscoverOptions`, `InteractiveRejector`
- `internal/cli/copilot.go` — current Copilot adapter (headless-only)
- `internal/cli/cursor.go` — current Cursor adapter (headless-only)
- `internal/cli/codex.go` — reference for an adapter that handles both modes (filesystem scan in interactive)
- `internal/cli/adapter_test.go` — existing test patterns and `InteractiveRejector` test assertions
- `internal/exec/agent.go` — see lines 157-164 (interactive rejection check) and 361-365 (DisallowedTools gate)
- `internal/exec/agent_test.go` — interactive-rejection scenarios that must be updated

### Copilot interactive arg construction

Per the design's flag table:

| Field           | Headless (existing)             | Interactive (NEW)         |
|-----------------|---------------------------------|---------------------------|
| Prompt          | `-p <prompt>`                   | `-i <prompt>`             |
| SessionID+Resume| `--resume=<id>`                 | `--resume=<id>`           |
| Model (fresh)   | `--model <m>`                   | `--model <m>`             |
| Effort (fresh)  | `--reasoning-effort <e>`        | `--reasoning-effort <e>`  |
| Headless toggle | `-p` + `-s`                     | (omit `-p`/`-s`)          |
| Autonomy        | `--allow-all` + `--autopilot`   | (none)                    |
| DisallowedTools | `--no-ask-user`                 | (empty — runner gates)    |

`-i, --interactive <prompt>` is the documented Copilot flag for "Start interactive mode and automatically execute this prompt" — verified against `copilot --help`. No stdin plumbing needed.

Drop `--allow-all` and `--autopilot` in interactive (no permission loosening; user supervises).

### Cursor interactive arg construction

| Field           | Headless (existing)                              | Interactive (NEW)            |
|-----------------|--------------------------------------------------|------------------------------|
| Prompt          | positional                                       | positional                   |
| SessionID+Resume| `--resume=<id>`                                  | `--resume=<id>`              |
| Model (fresh)   | `--model <m>`                                    | `--model <m>`                |
| Headless toggle | `-p` + `--output-format stream-json` + `--trust` | (omit all three)             |
| Autonomy        | `--force`                                        | (none)                       |
| Effort, DisallowedTools, SystemPrompt | ignored                            | ignored                      |

`--trust` and `--output-format` are `--print`/headless-only per `agent --help`. `--force` is dropped in interactive per the no-permission-loosening rule. The prompt is positional in both modes.

### Cursor interactive session-ID discovery

Cursor has no verified interactive session-ID channel — the `~/.cursor/chats/<workspace-hash>/<chat-uuid>/store.db` layout exposes no provenance to match a chat to the spawned process. The adapter therefore declines to guess: `CursorAdapter.DiscoverSessionID` returns `""` for interactive mode, and subsequent `session: resume` steps against a Cursor agent are treated as fresh.

(The original design proposed a filesystem scan modeled on `discoverCopilotSession`. Implementation review concluded the misattribution risk outweighed the resume convenience and dropped the scan; the JSONL parse is kept for headless.)

### `SupportsSystemPrompt()` stances

No change. Copilot and Cursor already return `false`; design verified neither CLI exposes a system-prompt flag.

### Removing the rejector implementations

- Delete `(*CopilotAdapter).InteractiveModeError` from `internal/cli/copilot.go`.
- Delete `(*CursorAdapter).InteractiveModeError` from `internal/cli/cursor.go`.
- Keep the `cli.InteractiveRejector` interface itself in `adapter.go` — it's retained for future adapters that genuinely don't support interactive.

### Test updates

- `internal/cli/adapter_test.go`: remove test cases asserting that Copilot/Cursor implement `cli.InteractiveRejector` and that `InteractiveModeError()` returns the expected messages. Add tests for the new interactive arg-construction patterns covering: fresh interactive (no resume flag), resumed interactive (no model flag), model on fresh, prompt delivery via `-i` for Copilot and positional for Cursor, absence of permission flags in interactive args.
- `internal/exec/agent_test.go`: update or remove scenarios that asserted Copilot/Cursor interactive steps fail at runtime. The check itself still exists for future use; just remove the Copilot/Cursor expectations.
- Confirm `CursorAdapter.DiscoverSessionID` returns `""` in interactive mode (no filesystem scan).

## Spec

These are the cross-cutting rules the implementation must satisfy. Verbatim from `openspec/changes/more-agents/specs/cli-adapter/spec.md`:

### Requirement: Adapter mode coverage

Every registered CLI adapter SHALL support both interactive and headless modes. The runner SHALL NOT reject either mode for any registered `cli`.

#### Scenario: Interactive step succeeds for any registered CLI
- **WHEN** an agent step runs in interactive mode with any registered `cli`
- **THEN** the runner spawns the CLI without emitting an unsupported-mode error

#### Scenario: Headless step succeeds for any registered CLI
- **WHEN** an agent step runs in headless mode with any registered `cli`
- **THEN** the runner spawns the CLI without emitting an unsupported-mode error

### Requirement: No permission loosening in interactive mode

In interactive mode, no adapter SHALL emit a flag that bypasses or pre-approves the underlying CLI's permission/approval prompts. The human at the terminal supervises permissions; the runner MUST NOT preempt that supervision. Headless invocations MAY emit such flags, since no human is present to approve.

#### Scenario: Adapter omits permission-grant flags in interactive mode
- **WHEN** any adapter constructs args for an interactive step
- **THEN** the args do not include any flag that auto-approves tools, paths, URLs, or commands (e.g., `--allow-all`, `--force`, `--yolo`, `--dangerously-skip-permissions`)

#### Scenario: Headless adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for a headless step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

### Requirement: AskUserQuestion blocked in headless, allowed in interactive

The runner SHALL include `AskUserQuestion` in `DisallowedTools` for every headless agent step — an agent in headless mode has no human to answer. The runner SHALL leave `DisallowedTools` empty for every interactive agent step — the human at the terminal can answer or decline tool calls directly.

#### Scenario: Headless step blocks AskUserQuestion
- **WHEN** the runner builds adapter input for a headless agent step
- **THEN** `DisallowedTools` includes `"AskUserQuestion"`

#### Scenario: Interactive step does not block AskUserQuestion
- **WHEN** the runner builds adapter input for an interactive agent step
- **THEN** `DisallowedTools` is empty

### Removed: Copilot interactive mode rejected at runtime

The existing `Copilot interactive mode rejected at runtime` requirement in `openspec/specs/copilot-cli-support/spec.md` is REMOVED by this change. Copilot interactive steps no longer fail at runtime. Tests asserting on the rejection error message must be removed or updated.

### Removed: Cursor interactive mode rejected at runtime

The existing `Cursor interactive mode rejected at runtime` requirement in `openspec/specs/cursor-cli-support/spec.md` is REMOVED by this change. Cursor interactive steps no longer fail at runtime. Tests asserting on the rejection error message or on `CursorAdapter` implementing `cli.InteractiveRejector` must be removed or updated.

## Done When

- `internal/cli/copilot.go` produces interactive args matching the table above; `InteractiveModeError()` is gone.
- `internal/cli/cursor.go` produces interactive args matching the table above; `InteractiveModeError()` is gone; `DiscoverSessionID` returns `""` for interactive mode (no filesystem scan).
- All scenarios above pass in tests.
- `make test` and `make lint` pass.
- A locally constructed interactive step with `cli: copilot` and `cli: cursor` runs without producing an "interactive mode not supported" error.
