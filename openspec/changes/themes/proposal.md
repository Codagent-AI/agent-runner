## Why

Agent Runner's TUI uses `lipgloss.AdaptiveColor` tokens for every styled foreground. Lipgloss picks the Dark or Light variant based on terminal background detection (OSC 11 probe with `COLORFGBG` fallback). Detection is unreliable across common terminals — iTerm2 and VS Code's integrated terminal can disagree on the same machine, and tmux/ssh/exotic terminals frequently misreport. The visible failure: a user with a light terminal sees the dark palette rendered, producing low-contrast or near-invisible text. The UI is broken until the user can override the choice.

We need an explicit, persisted theme selection that does not depend on detection succeeding. Detection may still inform the *initial suggestion* the first time the user runs the TUI, but it cannot be the source of truth.

## What Changes

- Add a new global user-settings file at `~/.agent-runner/settings.yaml` containing a `theme:` key whose value is `light` or `dark`.
- Add a first-launch modal that blocks any TUI startup when the settings file is missing, the `theme:` key is missing, the value is invalid, or the file is unparseable. The modal forces the user to pick `Light` or `Dark`. Auto-detection pre-selects the cursor; the user must confirm with Enter. Esc/Ctrl+C exits without writing.
- On confirmation, persist the choice atomically to `~/.agent-runner/settings.yaml` and proceed into whatever TUI entry point was being launched.
- Apply the chosen theme by calling `lipgloss.SetHasDarkBackground(true|false)` once at process startup, before any TUI rendering. Existing `lipgloss.AdaptiveColor` tokens stay as-is and now resolve deterministically to their Dark or Light variant.
- **BREAKING** for users who run the TUI in environments where they cannot answer a prompt (e.g., a pipe with no controlling TTY): the TUI will refuse to start without a persisted theme. Headless / non-TUI execution is unaffected because it does not enter the TUI at all.

## Capabilities

### New Capabilities
- `user-settings-file`: A new global per-user settings file at `~/.agent-runner/settings.yaml`, separate from `~/.agent-runner/config.yaml`. Defines the file's location, format (YAML mapping with forward-compatible unknown-key handling), atomic write semantics, and parent-directory creation. Forward-looking: future user preferences slot in here as additional top-level keys.
- `tui-theme`: Persisted TUI theme selection (`light` or `dark`) layered on top of the settings file, plus the blocking first-launch modal that ensures a valid theme is always set before any TUI renders, and the lipgloss wiring that applies it.

### Modified Capabilities

None. Existing TUI capabilities (`list-runs`, `live-run-view`, `view-run`, etc.) continue to render with the same color tokens — only the resolution of those tokens changes from "detection-dependent" to "settings-driven".

## Out of Scope

- Painting an explicit application background. The terminal's actual background continues to show through. Foreground colors only.
- An `auto` value persisted in `settings.yaml`. Detection only seeds the modal's default cursor; the persisted value is always `light` or `dark`.
- Environment variable override (e.g., `AGENT_RUNNER_THEME`). The settings file is the only source.
- CLI flag override (e.g., `-theme`).
- A "Settings" tab or screen in the list TUI. Editing the persisted theme after first launch is done by hand-editing `settings.yaml` (or deleting it to re-trigger the modal). A settings UI may come later.
- Per-token color overrides (e.g., custom hex for `AccentCyan`).
- Terminal-specific detection heuristics (iTerm2 / VS Code / tmux). The persisted setting is the mitigation.
- Migration of any existing setting from `~/.agent-runner/config.yaml`. The new file is independent.
- Adding any other settings to `settings.yaml` in this change.

## Impact

**Code:**
- New package `internal/usersettings/` — owns reading, writing, validating `~/.agent-runner/settings.yaml`.
- New package or sub-package for the first-launch theme-prompt modal (a small bubbletea program). Likely `internal/themeprompt/`.
- `cmd/agent-runner/main.go` — invoke the settings load + (if needed) modal *before* any bubbletea TUI program starts. Apply theme via `lipgloss.SetHasDarkBackground` once before any rendering. Affects every TUI entry path: bare invocation that lands in the list TUI, `-list`, `-inspect`, `-resume` (TUI form), and the run-view auto-launch when a workflow starts.
- `internal/tuistyle/styles.go` — no changes to color values. Tokens remain `lipgloss.AdaptiveColor`. The reverted patch (stash@{0} from a prior agent) is **not** to be applied.
- `internal/listview/newtab.go` — no signature changes. The reverted patch's `lipgloss.TerminalColor` change is **not** to be applied.
- `internal/tuistyle/styles_test.go` — no changes from the stashed patch's `TestPaletteColors_DoNotDependOnBackgroundDetection`.

**APIs:** None.

**Dependencies:** No new third-party dependencies. Uses existing `github.com/charmbracelet/lipgloss` and `github.com/charmbracelet/bubbletea`.

**Filesystem:** New file `~/.agent-runner/settings.yaml` created on first user choice.

**Tests:** New tests for `internal/usersettings/` (load/save round-trip, missing file, missing key, invalid value, unparseable YAML, atomic write) and for the theme-prompt modal (cursor pre-selection from detection, Enter writes file, Esc/Ctrl+C exits without writing). Existing TUI tests should not regress.
