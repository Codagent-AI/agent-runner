## ADDED Requirements

### Requirement: Main-menu discoverability

The debug workflow SHALL be discoverable as a standard builtin under the `core` namespace (registered name: `core:debug`) and SHALL appear in the new-workflow tab of the home TUI alongside other `core:` workflows. No special pinning, highlighting, dedicated top-level shortcut, or alternate entry point is added for it.

#### Scenario: Debug workflow appears in new-workflow tab
- **WHEN** the user opens the new-workflow tab of the home TUI
- **THEN** the discovered workflow list includes `core:debug` alongside other `core:` workflows

#### Scenario: Debug workflow launches with no params from menu
- **WHEN** the user selects `core:debug` from the new-workflow tab and starts it
- **THEN** the workflow launches with neither `failed_session_dir` nor `failed_run_id` set, triggering the cold-start interactive run-selection flow defined by the `workflow-debugger` capability

#### Scenario: No special menu affordance
- **WHEN** the home TUI is rendered
- **THEN** there is no pinned, highlighted, or top-level entry for `core:debug` outside of the standard discovery list

### Requirement: Run-view `d` keybinding launches debug

The run view SHALL provide a `d` keyboard action that launches `core:debug` with `failed_run_id` pre-filled to the currently-viewed run's id. The action SHALL be available whenever the viewed run is **inactive** (any non-active status, including `failed`, `completed`, and otherwise inactive) AND the live-run-view is not currently executing a workflow. It SHALL be available at any drill depth. It SHALL become available **immediately** when a live run transitions to a terminal state — the user SHALL NOT need to exit and re-enter the run view to use it.

#### Scenario: d on inactive run launches debug with run id
- **WHEN** the viewed run is inactive, the live-run-view is not running a workflow, and the user presses `d`
- **THEN** `core:debug` launches with `failed_run_id` set to the current run's id

#### Scenario: d ignored while run is active
- **WHEN** the viewed run is active (the live-run TUI is still executing the workflow, or the run lock is active)
- **THEN** pressing `d` does nothing

#### Scenario: d available at any drill depth
- **WHEN** the user is drilled inside a sub-workflow, loop, or iteration in an inactive run and presses `d`
- **THEN** `core:debug` launches with `failed_run_id` set to the top-level run id (drill depth does not affect the action; the param always refers to the outer run)

#### Scenario: d becomes available immediately on live-run termination
- **WHEN** a workflow finishes in the live-run-view (success or failure) and transitions to a terminal state
- **THEN** `d` becomes bound and usable without the user exiting and re-entering the run view

#### Scenario: Help bar advertises d when available
- **WHEN** the gate for `d` is satisfied
- **THEN** the help bar includes an entry for the `d` binding

#### Scenario: Help bar omits d when unavailable
- **WHEN** the gate for `d` is not satisfied (run is active, or live-run-view is running a workflow)
- **THEN** the help bar does not include the `d` entry

### Requirement: Onboarding-failure modal trigger

When the onboarding workflow (`onboarding:onboarding`) run reaches a terminal failure status — i.e. a step errored or the run otherwise ended in a non-success state — the home TUI SHALL display the onboarding-failure modal **instead of** returning the user directly to the home screen. The modal SHALL NOT fire on user cancellation (Ctrl+C, or Escape-at-top-level confirmation during the run) or on a clean `break_if`-driven early exit. The modal SHALL fire each time onboarding ends in failure unless `settings.onboarding.dismissed` is set, in which case the failed run SHALL return to the home screen without the modal.

#### Scenario: Onboarding failure triggers modal
- **WHEN** the onboarding run ends in a terminal failure status and `settings.onboarding.dismissed` is unset
- **THEN** the home TUI renders the onboarding-failure modal instead of going straight to the home screen

#### Scenario: User cancellation does not trigger modal
- **WHEN** the onboarding run ended because the user pressed Ctrl+C or confirmed an Escape-at-top-level quit during the run
- **THEN** the modal does not render; the user is returned to the home screen as before

#### Scenario: Clean break_if exit does not trigger modal
- **WHEN** the onboarding run ended via a clean `break_if`-driven early exit (no step error)
- **THEN** the modal does not render

#### Scenario: Persistent dismissal suppresses modal
- **WHEN** `settings.onboarding.dismissed` is set and the onboarding run fails
- **THEN** the modal does not render; the user is returned to the home screen

#### Scenario: Repeated failures continue to trigger modal until dismissed
- **WHEN** onboarding fails across multiple sessions and `settings.onboarding.dismissed` remains unset
- **THEN** the modal renders on each failure

### Requirement: Onboarding-failure modal layout

The onboarding-failure modal SHALL be rendered as a centered overlay on top of the home screen using the same overlay primitive used by the splash modal. The modal SHALL contain, in order: a headline reading `Onboarding failed unexpectedly`, the failure-reason string from the run (the same string the run-view failure surface displays for the run's root failure), and two action buttons. The action buttons SHALL be **Debug now** and **Skip**, in that left-to-right order; **Debug now** SHALL be the focused button on first render. While the modal is visible, the home-screen chrome (tabs, run lists) SHALL remain rendered around the overlay.

#### Scenario: Modal headline
- **WHEN** the modal is visible
- **THEN** its top line reads "Onboarding failed unexpectedly"

#### Scenario: Modal shows failure reason
- **WHEN** the modal is visible
- **THEN** it contains the same root failure-reason string the run-view failure surface displays for the onboarding run

#### Scenario: Modal button order and initial focus
- **WHEN** the modal renders for the first time in a session
- **THEN** two buttons are visible labeled **Debug now** and **Skip**, in that left-to-right order, with **Debug now** focused

#### Scenario: Overlay preserves home chrome
- **WHEN** the modal is visible
- **THEN** the home-screen chrome, tabs, and run lists are still rendered around the modal (only the area behind the modal is occluded)

### Requirement: Onboarding-failure modal key handling

While the modal is visible, the home TUI SHALL ignore its normal list-view key bindings and SHALL route key input to the modal. The modal SHALL support the following bindings:

- **Left**, **Right**, **Tab**, and **Shift+Tab** SHALL toggle button focus between **Debug now** and **Skip**.
- **Enter** and **Space** SHALL activate the currently focused button.
- **Esc** SHALL behave identically to activating **Skip**.
- **Ctrl+C** SHALL continue to exit agent-runner immediately, as it does elsewhere in the home TUI, and SHALL NOT write `settings.onboarding.dismissed`.

#### Scenario: Tab and arrow toggle focus
- **WHEN** the modal is visible with **Debug now** focused and the user presses **Tab** or **Right**
- **THEN** focus moves to **Skip**

#### Scenario: Shift+Tab and left toggle focus back
- **WHEN** the modal is visible with **Skip** focused and the user presses **Shift+Tab** or **Left**
- **THEN** focus moves to **Debug now**

#### Scenario: Enter activates focused button
- **WHEN** the modal is visible and the user presses **Enter** or **Space**
- **THEN** the currently focused button's action runs

#### Scenario: Esc acts as Skip
- **WHEN** the modal is visible and the user presses **Esc**
- **THEN** the **Skip** action runs (the modal closes and `settings.onboarding.dismissed` is written per the Skip-action requirement)

#### Scenario: Ctrl+C exits without persisting
- **WHEN** the modal is visible and the user presses **Ctrl+C**
- **THEN** agent-runner exits immediately without writing `settings.onboarding.dismissed`

#### Scenario: List-view shortcuts inert while modal visible
- **WHEN** the modal is visible and the user presses `s`, `?`, `r`, `q`, arrow keys other than focus toggles, or other list-view bindings
- **THEN** the settings editor does not open, no help workflow starts, no run starts, tabs do not change, the run-list cursor does not move, and the only effect of input is on the modal itself

### Requirement: Debug-now action

Activating the **Debug now** button SHALL close the modal and SHALL launch `core:debug` with `failed_session_dir` pre-filled to the failed onboarding run's session directory. **Debug now** SHALL NOT write any settings.

#### Scenario: Debug now launches debug workflow
- **WHEN** the user activates **Debug now**
- **THEN** the modal closes and `core:debug` launches with `failed_session_dir` set to the onboarding run's session directory

#### Scenario: Debug now does not persist
- **WHEN** the user activates **Debug now**
- **THEN** the runner does not write `settings.onboarding.dismissed` or any other settings as a result of the activation

### Requirement: Skip action

Activating the **Skip** button SHALL close the modal, write `settings.onboarding.dismissed` to the current RFC3339 timestamp via the user-settings atomic-write path, and return the user to the home screen. If the settings write succeeds, future onboarding failures SHALL NOT display the modal. If the settings write fails, the modal SHALL still close for the current session, an inline error SHALL be shown on the home screen identifying the failure, and the modal MAY appear again on the next onboarding failure because suppression was not persisted.

#### Scenario: Skip persists timestamp
- **WHEN** the user activates **Skip** and the settings write succeeds
- **THEN** `~/.agent-runner/settings.yaml` contains `onboarding.dismissed` set to the current RFC3339 timestamp

#### Scenario: Future failure honors persistent dismissal
- **WHEN** the user activates **Skip** in one session, restarts agent-runner, and onboarding subsequently fails
- **THEN** the onboarding-failure modal does not render

#### Scenario: Skip write failure surfaces inline error
- **WHEN** the user activates **Skip** and the settings write returns an error
- **THEN** the modal closes, an inline error appears on the home screen identifying that the onboarding-dismissed preference could not be saved, and `settings.onboarding.dismissed` is not set in the file
