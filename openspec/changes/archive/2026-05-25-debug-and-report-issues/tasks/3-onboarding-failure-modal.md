# Task: Onboarding-failure modal + --onboarding-from routing

## Goal

Replace today's silent drop-to-home behavior on onboarding failure with an overlay modal that offers two choices: **Debug now** (launches `core:debug` against the failed onboarding run) or **Skip** (persists `settings.onboarding.dismissed` so the modal does not appear again). After this task, an unsuccessful first-run onboarding leaves the user with an actionable next step rather than an unexplained drop to the home screen.

## Background

Today the onboarding workflow (`onboarding:onboarding`) is launched from `main.go` via an internal `--onboarding-from` flag that returns the user to the home screen regardless of outcome. There is no surfacing of the failure reason and no diagnostic action. This task adds the missing surface — a modal mirroring the existing splash-modal overlay — and the routing change that makes it appear.

### Why this exists

For brand-new users, an onboarding failure is the worst possible first-run experience: the workflow disappears and the user is left on a home screen that means nothing to them yet. The modal turns that moment into either a triage path (Debug now → `core:debug`) or a deliberate, persistent opt-out (Skip → `Onboarding.Dismissed`). The persistence flag matters: a user who already triaged once and chose to defer should not be re-prompted on every subsequent run.

### Key design decisions

**Mirror the splash-modal overlay pattern.** The splash modal is the existing precedent for a centered overlay on the home screen; reuse the same overlay primitive so the visual behavior (centered, home chrome still rendered around it, dim/border treatment) is consistent. Do not invent a new overlay primitive.

**Failure trigger is terminal failure only, not cancellation or `break_if`.** A user who hits Ctrl+C or confirms an Escape-to-quit during onboarding made a choice — surfacing a failure modal there would be wrong. A clean `break_if`-driven exit (no step error) likewise is not a failure. The trigger is "step errored or run otherwise ended in a non-success state", excluding cancellation and clean break_if.

**Persistence via `settings.onboarding.dismissed` as RFC3339 timestamp.** Boolean was tempting; timestamp lets us reason later about how long ago the user opted out without a schema change. Use the existing `usersettings.Save()` atomic write — do not write the file directly.

**Routing change in `main.go`.** Today the `--onboarding-from` branch returns the user to the home screen unconditionally. After this task it routes failures into the home TUI with a new `WithOnboardingFailure(...)` option that the listview model uses to render the modal on first paint. Success continues to return to the home screen with no modal.

**Debug-now launches `core:debug` with `failed_session_dir`** (a path) not `failed_run_id`. The onboarding run id is not necessarily easy to reach at the modal layer, but its session directory is on hand because we have just returned from running it. The `core:debug` workflow accepts either; design lets `failed_run_id` win if both are supplied.

**Debug-now does NOT write `Onboarding.Dismissed`.** Only **Skip** writes the flag. A user who triages but doesn't dismiss should still see the modal on subsequent failures.

**Skip write failure does not block UX.** If the atomic write returns an error, close the modal anyway, surface an inline error on the home screen, and accept that the modal will likely re-appear next failure. Do not block the user behind a settings file we cannot write.

### Code-touch points

You MUST read these files before starting:

- `openspec/changes/debug-and-report-issues/design.md` — full design context, including the modal placement and the `WithOnboardingFailure` option signature.
- `openspec/changes/debug-and-report-issues/specs/failure-debug-entry-points/spec.md` — verbatim spec for the modal trigger, layout, key handling, Debug-now action, and Skip action.
- `internal/listview/splash.go` (and any sibling `splash_*.go` files) — splash modal implementation; this is the structural template for the new modal (overlay primitive, focus model, key routing, render-around-chrome behavior).
- `internal/listview/model.go` — where `WithOnboardingFailure(...)` option is added and where the modal state is held.
- `cmd/agent-runner/main.go` — the existing `--onboarding-from` flag's failure branch is where the new routing call goes; mirror the pattern that today routes success back to the home screen.
- `internal/config/usersettings/` — the `Save()` atomic-write path for `Onboarding.Dismissed`. Read its current API and follow it; do not bypass `Save()`.

**New files to create (only as needed):**

- `internal/listview/onboarding_failure_modal.go` (or co-locate inside an existing splash file if that matches the codebase convention) — the modal type, its `View()`, `Update()`, and the option helper `WithOnboardingFailure(sessionDir, reason string)` (arg order matches `design.md`).

**Existing files to modify:**

- `internal/listview/model.go` — accept the new option in the listview constructor; hold the modal state alongside existing splash state; route input to the modal when it's visible; ensure the existing list-view shortcuts (`s`, `?`, `r`, `q`, arrow keys, tab) are inert while the modal is visible.
- `cmd/agent-runner/main.go` — in the `--onboarding-from` flow, when the run ends with a non-success outcome that is **not** Ctrl+C/Escape cancellation and **not** a clean `break_if` exit, call the home TUI with `WithOnboardingFailure(sessionDir, reason)`. Pass the failure-reason string the run-view failure surface would use for the run's root failure. Read `settings.onboarding.dismissed` before deciding whether to attach the option — if set, skip the modal and return to home as today.
- `internal/config/usersettings/settings.go` (or whichever file holds the settings struct) — add the `Onboarding.Dismissed string` field if not already present. Atomic write goes through the existing `Save()` path.

### Modal interaction summary

- Headline: `Onboarding failed unexpectedly`.
- Body: the failure-reason string from the run (same string the run-view failure surface displays for the root failure).
- Buttons: **Debug now** on left (focused on first render), **Skip** on right.
- Key bindings: Left/Right/Tab/Shift+Tab toggle focus; Enter and Space activate the focused button; Esc acts as Skip; Ctrl+C exits agent-runner without persisting `Onboarding.Dismissed`.
- While visible, the modal swallows all other home-TUI shortcuts (settings editor `s`, help `?`, run `r`, quit `q`, tab changes, run-list arrows). The only effect of input is on the modal.

### Constraints and conventions

- Mirror the splash modal pattern; do not introduce a different overlay primitive.
- Use the existing `usersettings.Save()` atomic write for `Onboarding.Dismissed`. Do not write the YAML file directly.
- Modal state is per-session — once dismissed (Skip) the file flag suppresses future renders; once shown and acted on within a session, it does not re-render in that session.
- TDD applies — write failing tests for: trigger gating (terminal failure vs cancellation vs break_if vs dismissed-flag-set), focus toggle behavior, button activation, Esc-as-Skip, Ctrl+C-does-not-persist, key swallow against list-view shortcuts, Debug-now launches `core:debug` with `failed_session_dir`, Skip persists timestamp, Skip-write-failure surfaces inline error and still closes modal.
- Use `google/go-cmp` for structured comparisons in tests (per project policy).
- Run `make fmt`, `make test`, `make lint`, `make build` before committing.

**Strictly self-contained:** This task assumes `core:debug` already exists as a launchable builtin workflow accepting `failed_session_dir` as a param, and that the resume-handoff mechanism is wired up runner-side. You do not implement them in this task.

## Spec

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

## Done When

- `internal/listview/` has the onboarding-failure modal (new file or co-located with the splash modal), implementing the layout, focus model, and key handling defined above. Tests cover focus toggle, Enter/Space activation, Esc-as-Skip, Ctrl+C-does-not-persist, and list-view-shortcut suppression.
- `internal/listview/model.go` accepts a `WithOnboardingFailure(sessionDir, reason)` option and renders the modal on first paint when the option is set.
- `cmd/agent-runner/main.go` routes terminal-failure onboarding outcomes into the home TUI with `WithOnboardingFailure(...)`; cancellation, clean `break_if` exits, and runs with `settings.onboarding.dismissed` already set continue to return to the home screen with no modal.
- `internal/config/usersettings/` exposes `Onboarding.Dismissed` (RFC3339 timestamp string) and its `Save()` path writes it atomically; the Skip action goes through `Save()`.
- Skip write-failure path closes the modal and surfaces an inline error on the home screen (covered by a test).
- Debug-now launches `core:debug` with `failed_session_dir` set to the failed onboarding run's session directory (covered by a test).
- `make fmt`, `make test`, `make lint`, `make build` all pass.
