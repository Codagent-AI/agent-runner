## Context

The onboarding feature shipped on the `onboarding` branch and is now in user testing. Testing surfaced 13 distinct issues; about a third are spec-level (rendering chrome, dispatcher resume, post-exit handoff, form-layout) and the rest are implementation drift, styling consistency, or bugs in the welcome workflow YAML.

This document captures the architectural decisions that span several of those fixes. The remaining bug fixes are mechanical and don't need design rationale.

## Goals / Non-Goals

**Goals:**
- A UI step that renders inside the workflow's live-run-view chrome and looks like a form (input + button) rather than a list selector that re-renders.
- A dispatcher that respects existing run state — resumes incomplete onboarding, hands back to the home screen on exit.
- Visual consistency: theme colors, glyphs, button-style action chrome.
- No new third-party UI dependencies.

**Non-Goals:**
- A general form/input framework. We extend `mode: ui` only enough to make onboarding feel right.
- A new `huh`/`bubblezone` dependency. Existing `bubbles` + `lipgloss` are sufficient.
- Touching content/copy of the onboarding screens.
- Re-architecting the live-run-view. We add a content slot for `mode: ui`; we do not redesign the view.

## Approach

### 1. UI step inside the live-run-view chrome

Today the `mode: ui` step renderer (`internal/exec/ui.go`) appears to take over the screen when its step executes, hiding the live-run-view chrome (`internal/runview/`, `internal/liverun/`). The fix is to make `mode: ui` render *into* the live-run-view's content area rather than alongside it.

Approach: when the live-run-view's active step is `mode: ui`, the live-run-view delegates the content area to a child Bubble Tea model owned by the UI step renderer. The chrome (header + sidebar) stays mounted and rendered by the live-run-view; key events that aren't top-level chrome shortcuts are forwarded to the child model. When the UI step resolves (action fired or cancelled), the child model returns control and the live-run-view advances to the next step exactly as it does for any other step type.

Concretely:

- The UI step renderer becomes a `tea.Model` that can be embedded — it accepts size hints from a parent and renders within those bounds.
- The live-run-view, when it would normally render a "current step" panel, instead renders the UI step's view inside that panel and forwards key/window-size messages to it.
- The existing top-level keybindings (`q`, `Ctrl+C`, Escape at top level) of the live-run-view stay bound at the chrome level. Keys consumed by the embedded UI step (arrows within an input, Tab, Enter on a focused element) are intercepted before the chrome's top-level handler sees them.

Why not a full overlay model? The mockups required chrome-around-content. Embedding cleanly aligns the implementation with that requirement, makes the sidebar's "active step" indicator stay accurate, and avoids divergent input-handling code paths.

### 2. Form layout and focus traversal

Bubble Tea's `bubbles` package does not ship a `button` widget, and we are *not* adding `huh` (deferred dep — adds form/input/select widgets and pulls in additional indirect deps; not warranted for the small surface here).

Approach: implement an explicit form model inside the UI step renderer.

- Form elements: each `input` (single_select for now) plus each `action`. They live in an ordered focus ring.
- Focus traversal: Tab / Shift-Tab move forward / backward across the ring. Arrow keys within a single-select input move the highlighted option, not focus.
- Rendering: each element renders one of two states — focused or unfocused — using `lipgloss` styling. Action buttons in particular use a bordered/padded `lipgloss.Style` that visually reads as a button (rounded border, accent color when focused, dim border when not). The exact palette pulls from `internal/tuistyle` (see §4 below).
- Enter semantics: on a focused action, fires the action. On a focused input, advances focus to the next element (does not fire). If the step declared a default action, pressing Enter while focus is on a non-action element fires the default per the existing "Optional defaults" requirement.
- Initial focus: on the first input if any inputs exist; otherwise on the first action. If a default action is declared and no inputs exist, focus starts on the default action.

Why not just use `bubbles.list` for actions and stop there? That's what's in place now, and it's the source of the "boring" look and the focus-loss bug. A small explicit form model is simpler than wedging buttons into a list widget.

### 3. Dispatcher resume and home-screen handoff

Two related dispatcher behaviors:

#### Resume detection

The dispatcher already checks `settings.onboarding.completed_at` and `settings.onboarding.dismissed`. It also needs to check for an in-flight `onboarding:welcome` run. Approach:

- Use the existing run-state lookup (the same one `agent-runner -resume` uses) to find runs whose workflow id is `onboarding:welcome` and whose state is non-terminal.
- If exactly one is found, resume it.
- If none is found, start a fresh `onboarding:welcome` run.
- If multiple exist (unexpected), resume the most recent and proceed; do not error.

The dispatcher should *not* duplicate state-lookup logic; it should call into `internal/stateio` or whichever package the resume-by-session-id path uses. Implementation note: confirm that the resume path can be invoked programmatically without going through the resume-TUI; if it cannot today, expose a thin entry point.

#### Sister regression check

Before declaring this fix onboarding-only, the implementer SHALL verify that `agent-runner -resume <session-id>` actually picks up where it left off for *any* workflow. The user reported suspicion that resume may be broken in general. If it is, fix the underlying bug; the dispatcher fix should sit on top of working resume.

#### Post-exit handoff

When a dispatcher-launched onboarding run reaches a terminal state, the runner currently leaves the user on the run-view. The fix: after the run terminates, transition to the listview ("home") TUI exactly as if the user had typed `agent-runner` with no arguments.

Concretely: the entry-point routing in `cmd/agent-runner/main.go` (or wherever the dispatcher is wired) records that this invocation went through the dispatcher; when the dispatcher-launched runner returns control, the entry point falls through to the listview path rather than to the post-completion run-view. Direct invocations (`agent-runner run onboarding:welcome`) do not set this flag, so they retain existing post-run-view behavior.

### 4. Visual consistency

#### Theme colors

The UI step renderer must pull colors from `internal/tuistyle` rather than hard-coded values. Specifically:

- Title: same color/weight as run-view section headers.
- Input prompt labels: same as listview / run-view field labels.
- Body: default foreground (no recolor) — markdown rendering already styles headings/emphasis itself.
- Focused action button: accent color (`AccentCyan` or whichever is used elsewhere for "selected" affordances).
- Unfocused action button: dim border / muted foreground.
- Focused input option: existing list-selection highlight color.

#### Glyphs for new step types

`internal/runview/view.go` defines `shellGlyphStyle` (`InactiveAmber`) and `loopGlyphStyle` (`AccentCyan`). Add:

- `scriptGlyphStyle`: `InactiveAmber` (same as shell — `script:` is a sibling primitive). Glyph character: implementer's call, but should visually echo the shell glyph (e.g., a similar but distinguishable mark — `❯` vs `▶`, or some other small variation that reads as "shell-adjacent").
- `uiGlyphStyle`: a different theme color (suggest `AccentMagenta` or whichever color is currently unused in the run-view step list). Glyph character: visually distinct from shell/script (e.g., a form-ish or selection-ish glyph — `◆`, `▣`, etc.).

The exact characters and colors are not load-bearing; the implementer chooses, and visual review can iterate.

### 5. Single-select kind naming alignment

The `ui-step` spec says `kind: single_select`. The model accepts both `single_select` and `single-select`. The bundled `setup-agent-profile.yaml` uses the hyphen form. Decision: canonicalize on the underscore form (it matches the existing identifier regex `^[a-z][a-z0-9_]*$` used elsewhere in ui-step).

- Tighten `internal/model/step.go` to reject `single-select`.
- Update `workflows/onboarding/setup-agent-profile.yaml` (and any other bundled YAML) to `single_select`.
- The spec already documents only the underscore form, so no spec change for this — only impl alignment.

## Decisions

- **No `huh` dependency.** A small explicit form model in `internal/exec/ui.go` is sufficient for the onboarding surface. Reconsider only if we add multi-select, free-text, or validated text input.
- **Embed `mode: ui` inside live-run-view rather than overlaying.** Aligns with mockups, keeps sidebar accurate, avoids divergent input handling.
- **Reuse the existing resume path from the dispatcher.** Do not introduce an onboarding-specific resume mechanism — it would conflict with the existing "No bespoke onboarding state" requirement.
- **Canonicalize `kind: single_select` (underscore).** Matches the spec and the rest of the ui-step regex conventions; tighten the loader and update bundled YAML rather than loosen the spec.
- **Verify general resume before declaring onboarding fixed.** If `agent-runner -resume` is broken across the board, that root cause is in scope here.

## Risks / Trade-offs

- **Embedding the UI step inside live-run-view chrome adds coupling** between two packages that today are independent. We accept this — the alternative (separate render paths) reproduces the bug we're fixing. Keep the boundary clean: the live-run-view delegates a content-area model and forwards messages; it does not reach into the UI step's internals.
- **Custom button styling vs. a battle-tested form library.** Custom styling means we own pixel-level decisions and re-render bugs; a library would shoulder some of that. Trade-off accepted because the surface is small and a new dep is heavy for the scope.
- **General resume regression scope creep.** If the implementer discovers that `-resume` is broken across all workflows, the fix may be larger than this change anticipates. If it grows beyond a focused fix, the implementer should pause and split it out rather than expand this change unboundedly.
- **Dispatcher resume on multiple incomplete runs.** Resuming the most recent silently masks an unexpected state. Acceptable for now (it should be rare; the user can always wipe state); we don't introduce diagnostic logging beyond the existing audit log.
