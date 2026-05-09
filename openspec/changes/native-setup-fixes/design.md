## Context

The native setup feature shipped with a minimal TUI that works but has poor UX (no explanatory copy, no centering, unnecessary screens), bugs (codex model parsing, setup.completed_at not persisting), and an over-engineered four-agent profile shape. The onboarding demo prompt is also misplaced inside the onboarding workflow rather than being part of native setup itself.

## Goals / Non-Goals

**Goals:**
- Fix all reported bugs (codex models, persistence)
- Streamline the wizard flow (remove welcome/confirm screens)
- Simplify the profile shape (2 agents, not 4)
- Integrate the demo prompt into native setup
- Substantially improve the visual presentation

**Non-Goals:**
- Adding back/previous navigation to the wizard
- Migration tooling for existing _base configs
- Changing the step-types-demo content

## Approach

### Startup flow decision tree

The `ensureFirstRunForTUI` function in `cmd/agent-runner/main.go` currently has two sequential checks: native setup trigger → onboarding workflow trigger. After this change, the flow becomes:

```
ensureFirstRunForTUI
├── setup.completed_at unset?
│   └── YES → run native setup TUI
│       ├── setup succeeds
│       │   ├── onboarding already completed/dismissed? → return to home
│       │   └── show demo prompt
│       │       ├── Continue → launch onboarding:onboarding workflow
│       │       ├── Not now → return to home (settings unchanged)
│       │       └── Dismiss → write onboarding.dismissed, return to home
│       └── setup cancelled/failed → return to home
└── setup.completed_at set?
    └── onboarding.completed_at unset AND onboarding.dismissed unset?
        ├── YES → show demo prompt (same TUI, same options)
        │   ├── Continue → launch onboarding:onboarding workflow
        │   ├── Not now → return to home
        │   └── Dismiss → write onboarding.dismissed, return to home
        └── NO → return to home (normal startup)
```

The demo prompt is a reusable component of the native setup TUI. The `ensureFirstRunForTUI` function can call it directly for the re-show case without going through the full setup flow.

### Native setup TUI stages (after changes)

```
stageInteractiveCLI    → "Planner CLI" — pick CLI for interactive/planner work
stageInteractiveModel  → "Planner Model" — pick model (skip if discovery empty)
stageHeadlessCLI       → "Implementor CLI" — pick CLI for headless/implementor work
stageHeadlessModel     → "Implementor Model" — pick model (skip if discovery empty)
stageScope             → "Config Scope" — global or project
stageOverwrite         → (conditional) collision check for planner/implementor
stageDemoPrompt        → Continue / Not now / Dismiss (after successful write)
stageDone              → terminal state
```

Removed stages: `stageWelcome`, `stageConfirm`.

The `NewModel` constructor SHALL initialize with `stageInteractiveCLI` and immediately trigger adapter detection. If detection fails, the model enters `stageDone` with a failure result.

### Renaming the screens

To align with the two-agent shape:
- "Interactive Agent CLI" → the screen explains this is for the **planner** agent
- "Interactive Agent Model" → planner model
- "Headless Agent CLI" → the screen explains this is for the **implementor** agent  
- "Headless Agent Model" → implementor model

The internal field names (`interactiveCLI`, `interactiveModel`, `headlessCLI`, `headlessModel`) can stay since they describe the mode, not the agent name.

### Profile write shape

Before:
```yaml
profiles:
  default:
    agents:
      interactive_base:
        default_mode: interactive
        cli: claude
        model: opus
      headless_base:
        default_mode: headless
        cli: codex
        model: gpt-5
      planner:
        extends: interactive_base
      implementor:
        extends: headless_base
```

After:
```yaml
profiles:
  default:
    agents:
      planner:
        default_mode: interactive
        cli: claude
        model: opus
      implementor:
        default_mode: headless
        cli: codex
        model: gpt-5
```

### Built-in defaults removal

`internal/config/config.go` currently defines four built-in agents. After this change:
```go
"planner":     {DefaultMode: "interactive", CLI: "claude", Model: "opus", Effort: "high"},
"implementor": {DefaultMode: "headless", CLI: "claude", Model: "opus", Effort: "high"},
```

No `interactive_base`, no `headless_base`. The `extends` field is still supported by the config system for user-defined inheritance — it just isn't used by the built-in defaults or the editor.

### Codex model discovery fix

`parseCodexModels` currently tries to unmarshal the output as `[]entry` (a JSON array). The actual `codex debug models` output is `{"models": [...]}`. Fix:

```go
type envelope struct {
    Models []entry `json:"models"`
}
var env envelope
if err := json.Unmarshal([]byte(out), &env); err == nil && len(env.Models) > 0 {
    entries = env.Models
} else {
    // fall back to existing array/single-entry parsing
}
```

Try envelope first, then fall back to the existing parsing for forwards compatibility.

### setup.completed_at persistence bug

The bug needs investigation during implementation. The `write()` function calls `m.deps.Settings.Update(mutator)` which loads settings, applies the mutator (sets `Setup.CompletedAt`), and saves. The `marshalSettings` function checks `settings.Setup.CompletedAt != ""` and writes the `setup:` block. The implementer should:

1. Add a test that calls the real `marshalSettings` with a Settings struct where only `Setup.CompletedAt` is set and verify the output YAML contains `setup: completed_at: ...`.
2. Add a test that calls the default `SettingsStoreFunc` (the `fillDefaults` path) and verify it persists `setup.completed_at` to disk.
3. Check whether the `raw` field on Settings is interfering with the merge during `marshalSettings`.

### Harmonica animation

Use `github.com/charmbracelet/harmonica` for spring-based easing. The approach:

1. Add a `harmonica.Spring` to the Model, configured with a ~200ms settle time.
2. Track a `yOffset float64` that starts at the terminal height (content below viewport) and animates to 0 (centered).
3. On stage transition, set `yOffset` to terminal height and start a tick-based animation loop.
4. Each tick (every ~16ms for ~60fps), update `yOffset` via `spring.Update(yOffset, 0)`.
5. In `View()`, pad the rendered content with `int(yOffset)` blank lines above it to create the scroll-up effect.
6. When `yOffset` is close enough to 0 (< 0.5), snap to 0 and stop ticking.

The animation runs in alt-screen mode (already the case via `tea.WithAltScreen()`), so the scroll effect is clean.

### Centered layout

In `View()`:
1. Render the content block (title + body + options) to a string.
2. Measure its height and width.
3. If terminal height >= 24 and width >= 80: pad with `(termHeight - contentHeight) / 2` blank lines above, and `(termWidth - contentWidth) / 2` spaces on each line.
4. Otherwise: render flush top-left (current behavior).

Use `lipgloss.Place()` for the centering — it handles both axes.

### Onboarding workflow simplification

`workflows/onboarding/onboarding.yaml` becomes:

```yaml
name: onboarding-onboarding
description: Optional Agent Runner workflow demo.
steps:
  - id: step-types-demo
    workflow: step-types-demo.yaml

  - id: set-completed
    command: agent-runner internal write-setting onboarding.completed_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

No intro step, no `set-dismissed` step, no `demo_action` capture, no `skip_if` conditionals.

## Decisions

1. **Two agents, not four.** The `extends` indirection added complexity without value for the default setup. Users who want inheritance can still use `extends` manually. Built-in defaults and the editor both write `planner`/`implementor` directly.

2. **Demo prompt in native setup, not in the workflow.** The workflow engine doesn't have access to the settings store to handle dismiss/not-now semantics correctly. The native Go TUI does. The workflow becomes a thin demo runner.

3. **Harmonica over manual easing.** Harmonica is already a transitive dependency of bubbletea's ecosystem. Spring physics gives a more polished feel than linear interpolation with minimal code.

4. **No migration for existing _base configs.** The config system's `extends` resolution still works. Users with existing `planner: extends interactive_base` configs will continue to work as long as they also have `interactive_base` defined. Only the built-in defaults and the editor output change.

## Risks / Trade-offs

- **Animation jank in exotic terminals.** Some terminal emulators handle rapid redraws poorly. The fallback (no animation in small terminals) partially mitigates this, but animation could be disabled entirely behind a `AGENT_RUNNER_NO_ANIMATION=1` escape hatch if needed.
- **_base removal breadth.** Many test files reference `interactive_base`/`headless_base`. The implementer needs to methodically update all of them. A grep for both names and `extends:` should catch everything.
- **Persistence bug root cause unknown.** The design assumes the bug is in the marshal/save path, but it could be elsewhere (e.g., a race with the onboarding workflow writing settings concurrently). Investigation during implementation is required.
