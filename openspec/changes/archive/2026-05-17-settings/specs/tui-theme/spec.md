## MODIFIED Requirements

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
