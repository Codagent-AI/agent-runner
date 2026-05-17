## Why

Today a user's theme can only be chosen on first launch via the blocking selection modal. Once set, there is no in-app way to change it or to discover that user settings exist at all. Users have to edit `~/.agent-runner/settings.yaml` by hand and restart.

## What Changes

- Add an `s` key on the run list that opens a new in-session user-settings editor.
- The editor is rendered as a bordered overlay on top of the run list and currently exposes one setting: theme (Light / Dark).
- Saving writes `~/.agent-runner/settings.yaml` and re-applies the theme immediately, with no restart required.
- Cancelling closes the overlay without writing.
- The list's help line gains `s settings`.
- Relax the existing `tui-theme` "apply once per process" requirement so the theme can be re-applied mid-session.

## Capabilities

### New Capabilities
- `user-settings-editor`: the in-session settings overlay — its opening, field set, keyboard model, save/cancel behavior, and overlay rendering contract.

### Modified Capabilities
- `tui-theme`: theme application is no longer limited to "exactly once per process"; it must also be re-applied (and the TUI re-rendered) when the editor changes it mid-session.
- `list-runs`: add `s` as a list-level keybinding that opens the settings editor and restores the list when the editor closes.

## Out of Scope

- Editing `~/.agent-runner/config.yaml` (agent profiles, agents) — that is a separate capability.
- Editing or surfacing the lifecycle timestamps under `setup` and `onboarding` in `settings.yaml`. They remain app-managed.
- Opening the editor from the run view, the param form, or any screen other than the run list.
- Themes beyond the existing Light / Dark pair.
- Per-project settings — the editor edits the same global file the rest of the app already reads.

## Impact

- `internal/listview/`: add `s` key handling, submodel wiring for the editor, restore behavior on close, help-line entry.
- New package (likely `internal/settingseditor/` or similar) for the editor model — separate from the first-launch theme modal under `cmd/agent-runner/`.
- `internal/tuistyle/`: factor the bordered-overlay box style so the new editor and any future overlays share it.
- `internal/usersettings/`: no schema changes; the editor uses the existing load/save path.
- Theme re-application path: invoke `lipgloss.SetHasDarkBackground` on save and force a full re-render of the active bubbletea program.
