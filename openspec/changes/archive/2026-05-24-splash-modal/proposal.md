## Why

After native setup and the optional onboarding demo finish, users land on the home screen with no in-context reminder of how to start a run or where to find help and settings. A lightweight splash modal can re-orient returning users on the home screen without forcing them back through onboarding, and the user controls whether to keep seeing it.

## What Changes

- Add a splash modal that overlays the home (listview) screen on the first home-screen render of each TUI session, suppressed entirely when the user has previously chosen "Don't show again".
- The modal renders a short orientation message and two buttons: **Got it** (close for this session) and **Don't show again** (close and persist suppression).
- Persist the suppression decision in `~/.agent-runner/settings.yaml` under a new `splash.dismissed` RFC3339 timestamp, mirroring the existing `onboarding.dismissed` pattern.
- Gate the splash on interactive TTY use (stdin and stdout both TTY), matching the native-setup trigger gate.

## Capabilities

### New Capabilities

- `splash-modal`: Home-screen orientation modal with session-only dismiss and persistent "don't show again" actions.

### Modified Capabilities

- `user-settings-file`: Add the `splash` mapping with a `dismissed` timestamp field, parsed and round-tripped alongside the existing `setup` and `onboarding` mappings.

## Out of Scope

- Changes to native setup or the onboarding demo workflow; the splash is independent of both.
- Multiple splash variants, A/B copy, or content driven by remote configuration.
- A way to re-enable the splash after "Don't show again" beyond manually editing `~/.agent-runner/settings.yaml`.
- Showing the splash anywhere other than the home (listview) screen (e.g., not in the run view, not during a workflow run).

## Impact

- New rendering and key handling in `internal/listview/` (mirrors the existing `settingseditor` overlay integration).
- New `Splash` field on `usersettings.Settings` with load/save round-trip support in `internal/usersettings/settings.go`.
- New `openspec/specs/splash-modal/spec.md` after archive; `openspec/specs/user-settings-file/spec.md` gains a requirement for the `splash` mapping.
- No CLI flag, command, or workflow YAML changes.
