## Why

Three problems with how CLI adapters handle autonomous agent operation:

1. **Naming mismatch.** The codebase calls it "headless," which describes an implementation mechanism (no TTY/print mode). The actual intent is "autonomous" — the agent works independently without human intervention. Headless invocation is one way to achieve autonomy, but not the only way.

2. **Permission inconsistency.** Claude passes no permission flags for headless mode, causing failures on fresh installs with no accrued trust state. Meanwhile, Codex, OpenCode, and Copilot pass maximally-permissive "nuclear" flags that bypass all safety checks — throwing away sandboxing, path restrictions, and approval gates. Every adapter should use the least-permissive flag that allows autonomous operation.

3. **Billing exposure.** Starting 2026-06-15, Anthropic bills programmatic/headless Claude usage at API rates rather than subscription pricing. Every headless Claude step in a workflow will cost money. Running autonomous Claude steps via an interactive session with autonomy instructions in the system prompt avoids this entirely.

## What Changes

### Rename headless to autonomous

Rename the user-facing mode concept: `mode: headless` becomes `mode: autonomous` in workflow YAML and profile schema, `default_mode: headless` becomes `default_mode: autonomous` in agent profiles, and corresponding code-level fields (e.g. `Headless bool` in `BuildArgsInput`).

The two valid mode values become `interactive` and `autonomous`. The word "headless" is retired as a mode name but retained as an `autonomous_backend` value, where it describes the invocation mechanism (print/exec mode, no TTY) rather than the user-facing concept.

`mode: headless` and `default_mode: headless` in workflow YAML and profile config are accepted as aliases for `autonomous` with a deprecation warning logged at load time. This gives existing users a migration path instead of a hard break.

### Autonomous backend setting

A new user setting controls how autonomous steps are invoked. The setting is editable from the settings modal (`s` key) and persisted in `~/.agent-runner/settings.yaml` under `autonomous_backend`. Three options:

- **Headless** (`headless`): The current approach. Invokes the CLI in print/exec mode (`-p`, `exec`, etc.). The process runs, produces output, and exits.
- **Interactive** (`interactive`): Invokes the CLI in interactive mode with system prompt instructions to work autonomously and signal continuation when done — the same continuation mechanism that interactive steps already use. Adapter-specific autonomy flags (e.g., Copilot's `--autopilot`) are passed where available.
- **Interactive for Claude** (`interactive-claude`): Claude autonomous steps use the interactive backend; all other adapters use headless. This is the pragmatic middle ground — Claude is the adapter where the billing change matters, and the other adapters don't have the same billing concern.

Default: `headless` (preserves current behavior). When any interactive option is selected but no TTY is available (CI, Docker), the runner falls back to headless for that invocation and logs a warning so users aware of the billing implications can see when fallback occurs.

### Permission alignment

Align all five CLI adapters to use middle-ground permission flags for autonomous steps — permissive enough to operate without stalling, but not the nuclear bypass that disables all safety checks. These flags apply regardless of which backend is active.

Specific flag changes per adapter:

- **Claude** (`internal/cli/claude.go`): Add `--permission-mode acceptEdits`. Currently passes nothing beyond `-p`, which fails on fresh installs.
- **Codex** (`internal/cli/codex.go`): Replace `--dangerously-bypass-approvals-and-sandbox` with `--sandbox workspace-write`. Keeps the filesystem sandbox active while stopping approval prompts.
- **OpenCode** (`internal/cli/opencode.go`): Remove `--dangerously-skip-permissions`. Default behavior already allows workspace edits without prompting.
- **Copilot** (`internal/cli/copilot.go`): Replace `--allow-all` with `--allow-tool='write'`. Grants file-write permission without also granting unrestricted shell, path, and URL access.
- **Cursor** (`internal/cli/cursor.go`): Remove `--force` from autonomous args; keep `--trust` (required for non-interactive Cursor to work at all). Without `--force`, Cursor uses its default permission mode rather than force-allowing all commands.

These flags were manually tested against all five CLIs.

### System prompt enrichment for interactive autonomous

When the interactive backend is active for a step, the runner prepends autonomy instructions to the step's system prompt: work autonomously, do not ask for human input, signal continuation when done. This uses the existing system prompt delivery mechanism (`--append-system-prompt` on Claude, prompt prepend on others).

Adapter-specific autonomy flags are also passed in interactive autonomous mode where available (e.g., Copilot's `--autopilot`). These are additive to permission flags — for example, Copilot in interactive autonomous mode receives both `--allow-tool='write'` (permission) and `--autopilot` (autonomy behavior).

## Capabilities

### Modified Capabilities
- `agent-profiles`: Profile schema changes `default_mode` values from `interactive|headless` to `interactive|autonomous`. Built-in default profiles update accordingly (`headless_base` keeps its name but its `default_mode` becomes `autonomous`).
- `user-settings-file`: Add `autonomous_backend` as a recognized top-level key with values `headless`, `interactive`, or `interactive-claude`.
- `user-settings-editor`: Add an "Autonomous Backend" field to the settings modal with the three options above.

## Out of Scope

- Re-defaulting built-in profiles away from Claude.
- Automatic TTY detection as the *sole* mechanism — the setting is explicit, with TTY detection only as a fallback when interactive is selected but unavailable.
- Renaming profile names like `headless_base` — the profile name is a user-facing identifier, not a mode value.

## Impact

- `internal/model/` — rename headless mode constant to autonomous
- `internal/usersettings/settings.go` — add `AutonomousBackend` field and type with three values; update parse/marshal
- `internal/usersettings/settings_test.go` — test load/save of the new field
- `internal/settingseditor/editor.go` — add "Autonomous Backend" field with three options; generalize navigation to handle multiple fields
- `internal/settingseditor/editor_test.go` — test multi-field navigation and save
- `internal/cli/adapter.go` — rename `Headless` field in `BuildArgsInput`; add field or value to distinguish autonomous backend (headless vs interactive)
- `internal/cli/claude.go` — add `--permission-mode acceptEdits` for autonomous; omit `-p` for interactive backend; add autonomy system prompt
- `internal/cli/codex.go` — replace bypass flag with `--sandbox workspace-write`
- `internal/cli/opencode.go` — remove `--dangerously-skip-permissions`
- `internal/cli/copilot.go` — replace `--allow-all` with `--allow-tool='write'`; pass `--autopilot` in interactive autonomous
- `internal/cli/cursor.go` — remove `--force` from autonomous args
- `internal/cli/adapter_test.go` — update expected args in all autonomous test cases; add interactive-backend test cases
- `internal/exec/` — update references from headless to autonomous
- `internal/runner/` — read autonomous backend setting; route autonomous steps to correct backend
- `openspec/specs/agent-profiles/spec.md` — update mode values from headless to autonomous
- `openspec/specs/user-settings-file/spec.md` — add `autonomous_backend` key
- `openspec/specs/user-settings-editor/spec.md` — add autonomous backend field to editor
- `workflows/*.yaml` — rename `mode: headless` to `mode: autonomous` in built-in workflows
- **DEPRECATION** for users who reference `mode: headless` in workflow YAML or `default_mode: headless` in config — accepted as aliases with a warning; will be removed in a future release
- **BREAKING** for users who depend on the current nuclear bypass permission behavior
