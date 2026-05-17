# tui-theme Specification

## Purpose
TBD - created by archiving change themes. Update Purpose after archive.
## Requirements
### Requirement: Theme key in the settings file
The TUI theme SHALL be persisted as a top-level `theme:` key in `~/.agent-runner/settings.yaml` (defined by the `user-settings-file` capability). The value SHALL be the lowercase string `light` or the lowercase string `dark`. Any other value (including the empty string, `auto`, integer, mapping, sequence, or capitalized variants like `Light` / `DARK`) SHALL be treated as if the key were not set.

#### Scenario: Light value persisted
- **WHEN** the settings file contains `theme: light`
- **THEN** the runner reads the persisted theme as `light`

#### Scenario: Dark value persisted
- **WHEN** the settings file contains `theme: dark`
- **THEN** the runner reads the persisted theme as `dark`

#### Scenario: Capitalized value treated as unset
- **WHEN** the settings file contains `theme: Light` or `theme: DARK`
- **THEN** the runner treats the theme as unset (and will trigger the first-launch modal at TUI startup)

#### Scenario: Auto value treated as unset
- **WHEN** the settings file contains `theme: auto`
- **THEN** the runner treats the theme as unset (and will trigger the first-launch modal at TUI startup)

#### Scenario: Non-string value treated as unset
- **WHEN** the settings file contains a non-string `theme:` value such as `theme: 7` or `theme: [light]`
- **THEN** the runner treats the theme as unset

### Requirement: First-launch modal triggers
Whenever a TUI entry path is about to render, the runner SHALL ensure a valid theme is set. If no valid theme is currently persisted (because the settings file is absent, unparseable, has a non-mapping root, has the `theme:` key missing, or has an invalid `theme:` value), the runner SHALL display a blocking theme-selection modal before any other TUI program starts. After the user makes a valid selection in the modal, the runner SHALL persist the choice and continue into the originally requested TUI.

TUI entry paths that SHALL trigger the check are: bare invocation that lands in the list TUI, `-list`, `-inspect <run-id>`, `-resume` without a session ID (which launches the TUI selector), and the live run-view that auto-launches when a workflow starts. Paths that bypass the TUI entirely SHALL NOT trigger the modal because they perform no styled rendering. Bypass paths include: `-headless`, `AGENT_RUNNER_NO_TUI=1`, `-validate`, `-version` / `-v`, and `-resume <session-id>` running headlessly.

#### Scenario: Settings file is missing
- **WHEN** the user runs `agent-runner` and `~/.agent-runner/settings.yaml` does not exist
- **THEN** the theme-selection modal is displayed before the list TUI renders

#### Scenario: Theme key is missing from an existing file
- **WHEN** the file parses successfully but contains no `theme:` key
- **THEN** the theme-selection modal is displayed before any TUI renders

#### Scenario: Theme value is invalid
- **WHEN** the file contains `theme: chartreuse` or `theme: auto` or `theme: 7` or `theme: Light`
- **THEN** the theme-selection modal is displayed before any TUI renders

#### Scenario: Settings file is unparseable
- **WHEN** the settings file exists but is not valid YAML or its root is not a mapping
- **THEN** the theme-selection modal is displayed before any TUI renders

#### Scenario: Headless invocation does not trigger modal
- **WHEN** the user runs `agent-runner -headless ...` or sets `AGENT_RUNNER_NO_TUI=1` and the theme is unset
- **THEN** no modal is displayed and execution proceeds without theme selection

#### Scenario: Validate flag does not trigger modal
- **WHEN** the user runs `agent-runner -validate <workflow>` and the theme is unset
- **THEN** no modal is displayed and validation proceeds normally

#### Scenario: Version flag does not trigger modal
- **WHEN** the user runs `agent-runner -version` and the theme is unset
- **THEN** no modal is displayed and the version string is printed normally

#### Scenario: Subsequent launch does not re-prompt
- **WHEN** the user has previously confirmed a theme and then launches `agent-runner` again
- **THEN** the modal is not displayed; the TUI starts directly with the persisted theme applied

### Requirement: Theme-selection modal behavior
The theme-selection modal SHALL present exactly two choices: `Light` and `Dark`. The cursor SHALL be pre-selected on one of them based on `lipgloss.HasDarkBackground()` evaluated *before* any explicit theme has been applied to lipgloss in this process — `Dark` if it returns true, `Light` if it returns false. The user SHALL be able to navigate between the two options using arrow keys (up/down and left/right both supported) and confirm with Enter. On Enter, the runner SHALL persist the chosen value via the settings-file write path defined by the `user-settings-file` capability and then allow the originally requested TUI to render. Pressing Esc or Ctrl+C SHALL exit the runner without writing to the settings file and without rendering any other TUI.

#### Scenario: Cursor pre-selects dark from detection
- **WHEN** the modal opens and `lipgloss.HasDarkBackground()` returns true
- **THEN** the `Dark` option is pre-selected

#### Scenario: Cursor pre-selects light from detection
- **WHEN** the modal opens and `lipgloss.HasDarkBackground()` returns false
- **THEN** the `Light` option is pre-selected

#### Scenario: User confirms the pre-selected option
- **WHEN** the modal opens with `Dark` pre-selected and the user presses Enter without moving the cursor
- **THEN** the settings file is written with `theme: dark`, the modal closes, and the originally requested TUI renders

#### Scenario: User changes selection before confirming
- **WHEN** the modal opens with `Dark` pre-selected, the user presses up-arrow to move to `Light`, then Enter
- **THEN** the settings file is written with `theme: light` (not `dark`)

#### Scenario: User cancels with Esc
- **WHEN** the user presses Esc on the modal
- **THEN** the runner exits with non-zero status; no settings are written; no other TUI is rendered

#### Scenario: User cancels with Ctrl+C
- **WHEN** the user presses Ctrl+C on the modal
- **THEN** the runner exits with non-zero status; no settings are written; no other TUI is rendered

#### Scenario: Write failure surfaces an error
- **WHEN** the user confirms a theme but the settings-file write fails (e.g., permission denied)
- **THEN** the runner prints an error identifying the file path and exits non-zero; no other TUI is rendered

### Requirement: Theme application
After the persisted theme has been resolved (whether from a pre-existing settings file or from a freshly confirmed modal selection), and before any other bubbletea program is constructed, the runner SHALL apply the theme by calling `lipgloss.SetHasDarkBackground(true)` for `dark` or `lipgloss.SetHasDarkBackground(false)` for `light`. The runner SHALL apply the theme at least once per process invocation, at TUI startup.

The runner SHALL additionally re-apply the theme whenever the persisted theme value is changed mid-session by the in-session settings editor (defined by the `user-settings-editor` capability). On such a mid-session change the runner SHALL both call `lipgloss.SetHasDarkBackground` with the new value AND cause the currently running bubbletea program to re-render its full view so that every adaptive color token resolves to the new variant on the next frame. The visible result SHALL be indistinguishable from what the user would have seen had they restarted the process with the new theme persisted.

The existing `lipgloss.AdaptiveColor` tokens defined in `internal/tuistyle/styles.go` SHALL remain unchanged in value and type; their resolution at render time SHALL deterministically follow the most recently applied theme rather than terminal probing.

#### Scenario: Light theme applied at startup
- **WHEN** the persisted theme is `light` and the runner reaches TUI startup
- **THEN** `lipgloss.SetHasDarkBackground(false)` has been called and the TUI renders with the Light variant of every adaptive color token

#### Scenario: Dark theme applied at startup
- **WHEN** the persisted theme is `dark` and the runner reaches TUI startup
- **THEN** `lipgloss.SetHasDarkBackground(true)` has been called and the TUI renders with the Dark variant of every adaptive color token

#### Scenario: Theme overrides terminal probe at startup
- **WHEN** the persisted theme is `light` but the terminal would have probed as dark-background under the previous behavior
- **THEN** the TUI still renders with the Light variant of every adaptive color token, regardless of probe outcome

#### Scenario: Mid-session change from light to dark
- **WHEN** the TUI is running with the Light variant in effect and the user saves a change to `theme: dark` in the in-session settings editor
- **THEN** `lipgloss.SetHasDarkBackground(true)` is called and the next rendered frame of the active TUI uses the Dark variant of every adaptive color token, without restarting the process

#### Scenario: Mid-session change from dark to light
- **WHEN** the TUI is running with the Dark variant in effect and the user saves a change to `theme: light` in the in-session settings editor
- **THEN** `lipgloss.SetHasDarkBackground(false)` is called and the next rendered frame of the active TUI uses the Light variant of every adaptive color token, without restarting the process

#### Scenario: Mid-session save with no theme change
- **WHEN** the TUI is running and the user saves the editor without changing the theme
- **THEN** the runner is permitted to call `lipgloss.SetHasDarkBackground` with the unchanged value, and the visible TUI continues to render with the same theme variant as before

#### Scenario: Cancelling the editor does not change the applied theme
- **WHEN** the TUI is running and the user opens the editor, moves the cursor to a different theme option, and then cancels with Esc
- **THEN** `lipgloss.SetHasDarkBackground` is NOT re-called and the running TUI continues to render with the previously applied theme variant

#### Scenario: Adaptive color tokens unchanged
- **WHEN** the implementer compares `internal/tuistyle/styles.go` color declarations before and after this change
- **THEN** every token is still declared as `lipgloss.AdaptiveColor` with the same Dark/Light hex values; no token has been replaced with a fixed `lipgloss.Color`
