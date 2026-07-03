# splash-modal Specification

## Purpose
Define the one-time splash modal trigger, layout, key handling, and dismissal persistence.
## Requirements
### Requirement: Splash modal trigger condition

The home (listview) TUI SHALL display the splash modal once per TUI session when all of the following hold on the first home-screen render of that session:

- `settings.splash.dismissed` is unset;
- both stdin and stdout are TTYs.

When any condition is false, the home TUI SHALL render normally without the splash, and SHALL NOT write any splash settings. The splash trigger SHALL be independent of native setup completion and of onboarding demo completion or dismissal: it is neither gated behind them nor suppressed by them.

#### Scenario: Fresh home-screen entry shows splash
- **WHEN** the user starts `agent-runner` on a TTY, lands on the home screen, and `settings.splash.dismissed` is unset
- **THEN** the splash modal renders over the home screen on its first render

#### Scenario: Persistently dismissed splash does not appear
- **WHEN** `settings.splash.dismissed` is set and the user starts `agent-runner` on a TTY
- **THEN** the home screen renders without the splash

#### Scenario: Non-TTY suppresses the splash
- **WHEN** the runner enters the home TUI with stdin or stdout connected to a pipe
- **THEN** the splash does not render and no splash settings are written

#### Scenario: Splash is independent of setup and onboarding state
- **WHEN** the user reaches the home screen and `settings.setup.completed_at`, `settings.onboarding.completed_at`, and `settings.onboarding.dismissed` are any combination of set or unset, while `settings.splash.dismissed` is unset
- **THEN** the splash modal renders

### Requirement: Splash shows once per TUI session

Within a single `agent-runner` TUI process, the splash modal SHALL render at most once. After the user closes it via either button, re-entering the home screen later in the same process (for example after returning from the run view, the settings editor, or a workflow run) SHALL NOT re-display the splash.

#### Scenario: Returning from run view does not retrigger splash
- **WHEN** the user dismisses the splash, opens a run view, and returns to the home screen in the same process
- **THEN** the splash does not render again

#### Scenario: Closing the settings editor does not retrigger splash
- **WHEN** the user dismisses the splash, opens the settings editor with `s`, and closes the editor
- **THEN** the splash does not render again

#### Scenario: New process re-evaluates the trigger
- **WHEN** the user closes the splash with **Got it**, exits Agent Runner, and starts a new `agent-runner` session on a TTY
- **THEN** the splash renders again on the first home-screen render of the new process

### Requirement: Splash modal layout

The splash modal SHALL be rendered as a centered overlay on top of the home screen using the same overlay rendering used by the in-session settings editor. It SHALL contain the following text, in this order, with each blank line below preserved as a blank line in the rendered modal:

```
Welcome to Agent Runner!

Select a workflow and press 'r' to get started.

From this screen you can also:
  - Browse runs in the current directory, your worktrees, or across all projects
  - Press ? for help, s for settings, q to quit
```

Below the text, the modal SHALL render two action buttons in this left-to-right order: **Got it** and **Don't show again**. The currently focused button SHALL be visually distinguished from the unfocused button. On first render, **Got it** SHALL be the focused button.

While the splash is visible, the home screen content underneath SHALL remain rendered around the overlay (i.e., the splash does not blank out the entire screen).

#### Scenario: Splash text content
- **WHEN** the splash modal is visible
- **THEN** its body contains the literal lines specified above, including the headline `Welcome to Agent Runner!`, the call to action `Select a workflow and press 'r' to get started.`, the `From this screen you can also:` heading, and the two bullet lines

#### Scenario: Button order and focus
- **WHEN** the splash modal renders for the first time in a session
- **THEN** two buttons are visible labeled **Got it** and **Don't show again**, in that left-to-right order, with **Got it** focused

#### Scenario: Overlay preserves home content
- **WHEN** the splash modal is visible
- **THEN** the home-screen chrome, tabs, and run lists are still rendered around the modal (only the area behind the modal is occluded)

### Requirement: Splash modal key handling

While the splash modal is visible, the home TUI SHALL ignore its normal list-view key bindings (tab navigation, cursor movement, `s`, `?`, `r`, `q`, `enter`, etc.) and SHALL route key input to the splash. The splash SHALL support the following key bindings:

- **Left**, **Right**, **Tab**, and **Shift+Tab** SHALL toggle button focus between **Got it** and **Don't show again**.
- **Enter** and **Space** SHALL activate the currently focused button.
- **Esc** SHALL behave identically to activating the **Got it** button (session-only dismissal).
- **Ctrl+C** SHALL continue to exit the application immediately, as it does elsewhere in the home TUI, and SHALL NOT write `settings.splash.dismissed`.

#### Scenario: Tab toggles focus
- **WHEN** the splash is visible with **Got it** focused and the user presses **Tab**
- **THEN** focus moves to **Don't show again**

#### Scenario: Shift+Tab toggles focus back
- **WHEN** the splash is visible with **Don't show again** focused and the user presses **Shift+Tab**
- **THEN** focus moves to **Got it**

#### Scenario: Enter activates focused button
- **WHEN** the splash is visible with **Don't show again** focused and the user presses **Enter**
- **THEN** the **Don't show again** action runs

#### Scenario: Esc dismisses for the session
- **WHEN** the splash is visible and the user presses **Esc**
- **THEN** the splash closes and behaves as if **Got it** had been activated

#### Scenario: List-view shortcuts inert while splash is visible
- **WHEN** the splash is visible and the user presses `s`, `?`, `r`, `q`, **Tab**, **Right**, **Up**, **Down**, or **Enter**
- **THEN** the settings editor does not open, no help workflow starts, no run starts or resumes, the home tabs do not change, the run-list cursor does not move, and the only effect is on the splash itself (focus change, activation, or no-op)

#### Scenario: Ctrl+C exits the app
- **WHEN** the splash is visible and the user presses **Ctrl+C**
- **THEN** the application exits without writing `settings.splash.dismissed`

### Requirement: Got it action

Activating the **Got it** button SHALL close the splash modal for the current TUI session only. It SHALL NOT modify `~/.agent-runner/settings.yaml`. After **Got it** runs, the home screen SHALL be visible and fully interactive, and the splash SHALL NOT reappear in the same process.

#### Scenario: Got it closes the splash
- **WHEN** the user activates **Got it**
- **THEN** the splash modal is no longer rendered and the home screen accepts its normal key bindings

#### Scenario: Got it does not persist
- **WHEN** the user activates **Got it** and a fresh `agent-runner` is launched afterward on a TTY
- **THEN** the splash modal renders again because `settings.splash.dismissed` was not written

#### Scenario: Got it write failure does not occur because no write is attempted
- **WHEN** the user activates **Got it**
- **THEN** the runner does not attempt to write `~/.agent-runner/settings.yaml`

### Requirement: Don't show again action

Activating the **Don't show again** button SHALL close the splash modal and SHALL persist suppression by writing `settings.splash.dismissed` as the current RFC3339 timestamp using the user-settings atomic-write path. If the write succeeds, future TUI sessions on a TTY SHALL NOT display the splash. If the write fails, the splash SHALL still close for the current session, an inline error SHALL be shown on the home screen identifying the failure, and the splash MAY appear again on the next TUI session because suppression was not persisted.

#### Scenario: Don't show again persists timestamp
- **WHEN** the user activates **Don't show again** and the settings write succeeds
- **THEN** `~/.agent-runner/settings.yaml` contains `splash.dismissed` set to the current RFC3339 timestamp

#### Scenario: Future session honors persistent dismissal
- **WHEN** the user activates **Don't show again** in one session, restarts `agent-runner` on a TTY, and reaches the home screen
- **THEN** the splash modal does not render

#### Scenario: Write failure surfaces inline error
- **WHEN** the user activates **Don't show again** and the settings write returns an error
- **THEN** the splash modal closes, an inline error appears on the home screen identifying that the splash preference could not be saved, and `settings.splash.dismissed` is not set in the file
