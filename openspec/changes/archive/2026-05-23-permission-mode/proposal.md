## Why

Autonomous Agent Runner workflows (`implement-task`, `implement-change`, `simple-change` and their OpenSpec variants) depend on the underlying CLI being able to run ordinary tools — shell, file edits, git, tests — without stopping for a human approval prompt. Today this works on Claude, Codex, Copilot, and OpenCode because each adapter passes its CLI's "autonomous-friendly" permission flag. The Cursor adapter passes only `--trust` (which trusts the workspace, not tools) and so blocks every shell call, including `git status --short` and `pytest` (see issue #33).

Patching Cursor to add `--force` would unbreak Cursor today, but the underlying decision — how much authority Agent Runner pre-grants to an unattended agent — is a user-facing trade-off that varies by CLI and by trust model. The next missing flag on the next CLI will re-create the same problem. We need a single cross-CLI setting that makes this trade-off explicit, lets the user opt in, and gives adapters a consistent knob to honor.

## What Changes

- Add a top-level `autonomous_permission_mode` setting to `~/.agent-runner/settings.yaml` with values `conservative` (default) and `yolo`.
- Show the setting in the native setup TUI on its own screen, immediately after the existing Autonomous Backend screen, with explicit risk copy on the `yolo` option.
- Show the setting in the user-settings editor as a third field, after Theme and Autonomous Backend, also with explicit risk copy.
- In autonomous invocation contexts (both autonomous-headless and autonomous-interactive), CLI adapters honor the setting:
  - `conservative` — adapters emit today's per-CLI permission flags unchanged (Claude `--permission-mode acceptEdits`, Codex `--sandbox workspace-write`, Copilot `--allow-tool=write --autopilot`, Cursor `--trust` only, OpenCode none).
  - `yolo` — adapters MAY additionally emit each CLI's broadest-authority flag (e.g., Cursor `--force`, Claude `--permission-mode bypassPermissions`, Codex `--sandbox danger-full-access`, Copilot `--allow-all-tools`). The exact per-CLI flag is captured in `design.md` and is subject to implementation verification.
- Interactive (human-supervised) context is unchanged. The `cli-adapter` "no permission loosening in interactive mode" rule still holds; the new setting does not apply there.
- Resolves the user-visible problem in issue #33 by giving Cursor users a documented way to enable autonomous shell execution — opting in to `yolo` adds `--force` to the Cursor invocation.

## Capabilities

### Modified Capabilities

- `user-settings-file`: adds the `autonomous_permission_mode` key, valid values, default, and validation behavior.
- `user-settings-editor`: adds the permission-mode field to the editor field list, with pre-selection from persisted value and explicit risk copy on the yolo option.
- `native-setup`: adds a Permission Mode selection screen after the Autonomous Backend screen, with `conservative` pre-selected and risk copy on yolo.
- `cli-adapter`: adds a requirement that autonomous-context adapters honor the new setting; the breadth of permission flags they emit is bounded by it.
- `cursor-cli-support`: makes `--force` conditional on `yolo` mode (today it is unconditionally omitted).
- `copilot-cli-support`: allows the adapter to emit a broader-authority flag in `yolo` mode (today only `--allow-tool=write --autopilot` is documented).

## Out of Scope

- Per-tool allowlist configuration (e.g., "allow git but not curl"). The setting is a binary mode; users wanting finer control configure their CLI directly.
- Per-step or per-workflow overrides of the permission mode.
- Changing the `autonomous_backend` setting or any of its semantics — the two settings are orthogonal.
- Touching interactive-context behavior or the `cli-adapter` "no permission loosening in interactive mode" rule.
- Telemetry, prompts, or warnings when running in `yolo` mode beyond the setup/editor risk copy.
- Per-CLI defaults beyond what each CLI already emits today.

## Impact

- `internal/usersettings/settings.go` — new field, parser, validator, marshaller.
- `internal/onboarding/native/native.go` — new setup screen between Autonomous Backend and the skills-install stage.
- `internal/settingseditor/editor.go` — new editor field, keyboard model update, pre-selection logic.
- `internal/cli/cursor.go`, `internal/cli/copilot.go`, `internal/cli/claude.go`, `internal/cli/codex.go` — adapter args adjusted to honor the setting in autonomous contexts.
- `BuildArgsInput` (or its equivalent) — gains a permission-mode field plumbed from settings through the runner to each adapter.
- Tests in `internal/usersettings`, `internal/settingseditor`, `internal/onboarding/native`, `internal/cli/*`.
- No data migration: missing key defaults to `conservative`, which preserves today's emitted flags for every adapter except Cursor (where today's behavior is the bug, not the baseline).
