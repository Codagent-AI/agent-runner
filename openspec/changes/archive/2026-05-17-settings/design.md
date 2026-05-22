## Context

The TUI already has one settings-related modal: the blocking first-launch theme picker enforced by the `tui-theme` capability. That modal pre-seeds from `lipgloss.HasDarkBackground()`, exits the whole process on cancel, and runs *before* any other bubbletea program is constructed.

This change introduces a second, behaviorally different surface: an in-session editor for user settings, opened with `s` from the run list, that overlays the list, pre-seeds from the *persisted* theme, and on cancel returns to the list (it does not exit). Today its only field is theme; the design must leave room for more fields without a rewrite.

Two implementation choices are worth fixing in writing so the implementer does not re-litigate them: (1) whether to reuse the first-launch theme modal component, and (2) how to make a theme change visibly take effect mid-session.

## Goals / Non-Goals

**Goals:**
- A small, isolated component for the in-session editor that is easy to extend with new fields later.
- A theme change that visibly takes effect immediately without restarting the process.
- A bordered-overlay rendering pattern that other future overlays in the listview can reuse.
- Zero behavioral change to the existing first-launch theme modal.

**Non-Goals:**
- Generalizing the editor to handle every key in `settings.yaml`. Lifecycle keys stay app-managed.
- Building a multi-screen settings UI (tabs, sections, etc.). One overlay, one form, growing one field at a time.
- Surfacing the editor from screens other than the run list.

## Approach

### Modal component: build a new one, don't reuse the first-launch modal

The first-launch theme modal is tightly bound to behaviors that the in-session editor must NOT have:

- It pre-seeds from `lipgloss.HasDarkBackground()` (the in-session editor pre-seeds from the persisted value).
- It exits the runner on Esc / Ctrl+C (the in-session editor closes the overlay on Esc and only Ctrl+C quits).
- It is single-purpose and single-field by intent; the in-session editor is designed to grow.
- It runs *before* any other TUI exists, so it owns the whole screen; the in-session editor must compose with a live listview behind it.

Reusing the existing component would mean parameterizing the pre-seed source, the cancel behavior, and the rendering mode — at which point most of the "shared" code is a thin shell with two callers that each carry their own logic. Cleaner to build a separate component (`internal/settingseditor/` or similar) shaped for in-session, multi-field use from day one, even though today it only renders one field.

The bordered-overlay box style itself IS worth factoring. Add a shared style (e.g., `tuistyle.OverlayBox`) so both this editor and any future overlay use the same border, padding, and color tokens.

### Mid-session theme application

The current `tui-theme` requirement says the theme is applied "exactly once per process invocation." That has to relax. The mechanism on save:

1. Write `settings.yaml` (via the existing `usersettings` write path).
2. Call `lipgloss.SetHasDarkBackground(newValue)`.
3. Force the active bubbletea program to re-render its entire view so every `AdaptiveColor` token resolves against the new background.

Step 3 is the load-bearing part. Bubbletea's `View()` is called on every model update; the issue is that lipgloss caches the resolved color for a given style+background pair. The implementer should validate that `SetHasDarkBackground` alone is enough to invalidate cached resolution before the next `View()`. If it is not, the editor's save command should additionally emit a message that forces a re-render — `tea.WindowSizeMsg` with the current size is the conventional trigger that causes all styled widgets to re-measure. The implementer should pick whichever of these is sufficient and add a test that proves a saved theme change is visible on the next frame.

The mid-session re-application path is only triggered by the in-session editor. The first-launch modal continues to apply the theme once before the TUI exists, as it does today.

### Editor as a listview submodel

The editor is owned by the listview model as an optional submodel:

- When closed, listview behaves exactly as today.
- When open, listview's `Update` delegates key messages to the editor and renders the editor overlaid on top of its normal view. Listview's own state (cursor, tab, scroll) is held untouched.
- The editor returns one of two completion messages to the listview: `Saved{settings}` or `Cancelled{}`. On `Saved`, the listview triggers theme re-application (see above) and then drops the submodel. On `Cancelled`, it just drops the submodel.

This keeps the editor decoupled from listview's internals — listview only needs to know how to open it, render it on top, and react to its two completion messages.

## Decisions

- **Build a new editor component**, not a parameterized version of the first-launch theme modal. Rationale: divergent pre-seed source, divergent cancel behavior, divergent composition context, and the editor is designed to grow while the first-launch modal is single-purpose.
- **Factor the bordered-overlay visual style** into `tuistyle` so it is reusable, even though only one overlay exists today. This is cheap and avoids style drift when the next overlay shows up.
- **Re-apply the theme by calling `lipgloss.SetHasDarkBackground` and forcing a full re-render** of the active bubbletea program. The implementer validates whether `SetHasDarkBackground` alone is sufficient; if not, dispatch `tea.WindowSizeMsg` (or the equivalent existing re-layout trigger) on save.
- **Editor lives as a listview submodel** with two completion messages (`Saved` / `Cancelled`). Listview owns triggering theme re-application on save.
- **Pre-selection source for the in-session editor is the persisted theme**, not `lipgloss.HasDarkBackground()`. By the time this editor is reachable, an authoritative persisted value always exists.

## Risks / Trade-offs

- **Lipgloss caching surprises**: if `SetHasDarkBackground` does not invalidate cached `AdaptiveColor` resolutions, a save will appear to do nothing until the user provokes a redraw. Mitigation: the design explicitly calls for a force-redraw step and a test that proves a saved change is visible on the next frame.
- **Duplication between the two theme surfaces**: the in-session editor and the first-launch modal both have a Light/Dark toggle and similar visual styling. We accept a small amount of UI duplication in exchange for keeping the two surfaces' behaviors cleanly separable.
- **Help-bar real estate**: adding `s settings` to the help bar consumes a few columns on every list view. Acceptable; the existing help bar already adapts to narrow widths.
