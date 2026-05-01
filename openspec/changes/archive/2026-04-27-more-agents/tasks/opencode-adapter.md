# Task: Add OpenCode adapter

## Goal

Add a new `cli.Adapter` implementation for the OpenCode CLI, supporting both headless and interactive modes. Register it in the adapter registry, accept `cli: opencode` in the configuration validator, and provide test coverage mirroring the existing Copilot adapter.

## Background

OpenCode (`opencode`) is an open-source coding agent. Today Agent Runner has no adapter for it; users selecting it would fail config validation. After this task, `cli: opencode` is a valid value in agent profiles and workflows, and the runner can spawn OpenCode in either mode.

OpenCode's CLI uses a subcommand split: headless invokes `opencode run`, interactive invokes default `opencode` (TUI). Several flags (`--variant`, `--dangerously-skip-permissions`, `--format`) only exist on `run`; the interactive command takes a smaller flag set. The adapter switches on `input.Headless` to choose the subcommand and builds the appropriate flag list.

OpenCode does not expose a system-prompt flag (verified against `opencode --help`), so `SupportsSystemPrompt()` returns `false`; the runner's existing fallback (prepending system-prompt content to the user prompt) applies.

OpenCode model identifiers use a `provider/model` format (e.g., `anthropic/claude-opus-4`). Pass through `input.Model` verbatim; do not transform.

OpenCode's reasoning effort surface is `--variant` (provider-specific values like `high`, `max`, `minimal`). Pass `input.Effort` through verbatim. Note: `--variant` is `run`-only — interactive opencode silently ignores effort, matching Cursor's effort-ignored pattern.

OpenCode's autonomy flag is `--dangerously-skip-permissions`, also `run`-only. Headless emits it. Interactive does not (no flag exists, and the cross-cutting "no permission loosening in interactive" rule would forbid it anyway).

### Files you must read

- `openspec/changes/more-agents/proposal.md` — change motivation
- `openspec/changes/more-agents/design.md` — full flag tables and decisions
- `openspec/changes/more-agents/specs/cli-adapter/spec.md` — cross-cutting requirements (registry, system-prompt capability, mode coverage, AskUserQuestion gating, no permission loosening)
- `internal/cli/adapter.go` — `BuildArgsInput`, `Adapter`, `DiscoverOptions`, registry, `KnownCLIs`
- `internal/cli/copilot.go` — closest reference for an adapter that uses filesystem-scan session discovery; mirror its structure
- `internal/cli/cursor.go` — reference for parsing JSONL output to extract a session ID
- `internal/cli/codex.go` — reference for an adapter handling both modes
- `internal/cli/adapter_test.go` — test patterns to mirror
- `internal/config/config.go` — see line 71 (`validCLI` map) and line 441 (error message listing valid CLIs)
- `internal/config/config_test.go` — config test patterns

### OpenCode flag mapping

Per the design's flag table:

| Field           | Headless (`opencode run`)              | Interactive (`opencode`)        |
|-----------------|----------------------------------------|---------------------------------|
| Prompt          | positional `<message>`                 | `--prompt <text>`               |
| SystemPrompt    | ignored                                | ignored                         |
| SessionID+Resume| `-s <id>`                              | `-s <id>`                       |
| Model (fresh)   | `--model <provider/model>`             | `--model <provider/model>`      |
| Effort (fresh)  | `--variant <e>`                        | ignored (run-only flag)         |
| Headless toggle | `run` subcommand + `--format json`     | omit `run`                      |
| Autonomy        | `--dangerously-skip-permissions`       | (none — flag is run-only)       |
| DisallowedTools | ignored (no CLI flag)                  | ignored                         |

Fresh-only rule for `--model` and `--variant`: omit on resume (`input.Resume == true` AND `input.SessionID != ""`).

### Session-ID discovery

**Headless** (`opencode run --format json`): every JSONL event includes a `sessionID` field (camelCase). Parse the first event with a non-empty `sessionID` and return it. The format is `ses_<random>`. Be tolerant to non-JSON lines and missing fields (skip and continue), matching the Cursor headless pattern.

**Interactive**: scan `~/.local/share/opencode/storage/session_diff/` for files matching `ses_*.json`. Filter by `mtime >= opts.SpawnTime`. Return the basename (without `.json` extension) of the newest match. Return `""` when no candidates exist. Pattern modeled after `discoverCopilotSession` in `internal/cli/copilot.go`. There is no per-CWD scoping in this directory; log a warning when multiple post-spawn candidates match.

`DiscoverSessionID` dispatches based on `opts.Headless`.

### Optional adapter interfaces

Headless OpenCode emits JSONL on stdout. Implement `OutputFilter` and `StdoutWrapper` (mirror Cursor's pattern in `internal/cli/cursor.go`) to extract assistant text content for the runner's capture variables and live TUI display. The exact event types to extract are the `text` events with `part.type == "text"` and the `step_finish` events for completion signaling — verify against `opencode run --format json` output during implementation.

No `HeadlessResultFilter` is needed unless OpenCode emits known non-fatal stderr that should be suppressed; defer until observed.

### Registry and config wiring

- Register `"opencode": &OpenCodeAdapter{}` in the `registry` map in `internal/cli/adapter.go`.
- Add `"opencode": true` to the `validCLI` map in `internal/config/config.go` (around line 71).
- Update the error message at `internal/config/config.go:441` to list `opencode` alongside `claude, codex, copilot, cursor`.

### Test coverage

- New `internal/cli/opencode_test.go` mirroring `copilot_test.go` (or the pattern shown in `adapter_test.go` if there's no separate copilot test file): cover fresh headless, fresh interactive, resumed headless, resumed interactive, model fresh-only rule, effort fresh-only rule (headless), effort dropped in interactive, autonomy flag in headless only.
- Session-ID discovery tests for both modes, using temp directories or string fixtures.
- `internal/cli/adapter_test.go`: update any test enumerating `KnownCLIs()` to include `opencode`.
- `internal/config/config_test.go`: add a test asserting `cli: opencode` validates successfully and that the error message for an invalid CLI mentions `opencode`.

## Spec

Verbatim from `openspec/changes/more-agents/specs/cli-adapter/spec.md`. Every scenario below must hold for the new OpenCode adapter alongside the existing adapters.

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

### Requirement: CLI adapter registry

The runner SHALL maintain a hard-coded registry of known CLI adapters. Each adapter SHALL be identified by a string key (e.g., `claude`, `codex`, `copilot`, `cursor`, `opencode`). The registry is compile-time — adding a new CLI requires a code change.

#### Scenario: OpenCode CLI resolved
- **WHEN** a step specifies `cli: opencode`
- **THEN** the runner resolves the OpenCode adapter from the registry

#### Scenario: Unknown CLI requested
- **WHEN** a step specifies a `cli` value not in the registry
- **THEN** the runner fails at load time with a validation error indicating the CLI is not supported

### Requirement: Adapter system prompt capability

Each adapter SHALL declare whether it supports native system prompt delivery. The Claude adapter SHALL declare support. The Codex adapter SHALL declare no support. The Copilot, Cursor, and OpenCode adapters' stances are pending verification against their respective CLIs.

#### Scenario: OpenCode adapter declares its stance
- **WHEN** the runner queries the OpenCode adapter for system prompt capability
- **THEN** the adapter returns a definite `SupportsSystemPrompt` boolean

The OpenCode CLI does not expose a system-prompt flag (verified against `opencode --help`); implement `SupportsSystemPrompt() == false`.

## Done When

- `internal/cli/opencode.go` exists and implements `cli.Adapter`, with both modes producing args matching the table above.
- `internal/cli/adapter.go` registry includes `"opencode"`.
- `internal/config/config.go` `validCLI` includes `"opencode"` and the error message lists it.
- All scenarios above pass in tests, plus the OpenCode-specific arg-construction and session-discovery tests described in the test coverage section.
- A workflow declaring `cli: opencode` (in either mode) loads, validates, and spawns the binary.
- `make test` and `make lint` pass.
