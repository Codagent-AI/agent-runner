## Context

Agent Runner's TUI uses `lipgloss.AdaptiveColor{Dark: ..., Light: ...}` tokens (in `internal/tuistyle/styles.go`) for every styled foreground. At render time, lipgloss picks the Dark or Light variant based on `lipgloss.HasDarkBackground()`, which probes the terminal via OSC 11 with `COLORFGBG` as a fallback. Detection is unreliable across common terminals — iTerm2 and VS Code's integrated terminal can disagree on the same machine, and tmux/ssh/exotic terminals frequently misreport. The user-visible failure: light-on-light or dark-on-dark text rendering, making the UI unreadable.

A prior agent attempted a fix by replacing all `AdaptiveColor` tokens with fixed `lipgloss.Color` values matching the previous Light palette (currently in `git stash@{0}` of this worktree, **not applied**, and **not to be applied**). That patch hard-coded the light palette for everyone, regressing dark-terminal users.

The right fix is explicit user theme selection persisted to disk, with no path that lets the TUI render against an unset theme. The user must pick `light` or `dark` once, and that choice is the source of truth thereafter.

## Goals / Non-Goals

**Goals:**
- The TUI never renders against a misdetected theme — the user's explicit choice is always the source of truth.
- Existing color tokens in `internal/tuistyle/styles.go` keep their values and types unchanged; only how `Dark` vs `Light` gets selected at render time changes.
- The first-launch flow is obvious to the user, doesn't bury them in flags, and uses detection only as a default-cursor hint.
- Settings persistence uses a new file independent of `~/.agent-runner/config.yaml` so future user-facing preferences have a clean place to live.

**Non-Goals:**
- Painting an explicit application background. Terminal background continues to show through.
- An `auto` value persisted in the settings file. Detection only seeds the modal's default cursor.
- Environment variable, CLI flag, or config-file override. Settings file is the only source.
- A "Settings" tab or screen in the list TUI. Editing the persisted theme means hand-editing `settings.yaml` or deleting it to re-trigger the modal.
- Per-token color overrides.
- Terminal-specific detection heuristics.
- Any other settings in `settings.yaml` beyond `theme:`.
- Migration from `config.yaml`.

## Approach

### Theme application via lipgloss

`internal/tuistyle/styles.go` keeps all `lipgloss.AdaptiveColor` tokens as they are. The fix is to ensure `lipgloss.HasDarkBackground()` returns the user's chosen value rather than the probe result, by calling `lipgloss.SetHasDarkBackground(true|false)` once at process startup.

This is preferable to converting tokens to fixed `lipgloss.Color` (per-theme) because:
- It localizes the change to one site (the startup wiring) rather than touching every consumer of `tuistyle`.
- It preserves the `AdaptiveColor` shape so future themes (if we ever ship one) continue to work the same way.
- It makes the `Dark`/`Light` palette pair the single source of truth for what each theme looks like.

`lipgloss.SetHasDarkBackground` must be called *before* any rendering — including before any styles are rendered into strings, since `AdaptiveColor.value()` snapshots `HasDarkBackground` at render time. Calling it once at the top of `run()` in `cmd/agent-runner/main.go`, after settings are loaded and before any bubbletea program is constructed, is sufficient. Tests that need a deterministic theme can call `lipgloss.SetHasDarkBackground(...)` directly in their setup.

### Settings file

New file `~/.agent-runner/settings.yaml`. Schema for this change:

```yaml
theme: light    # or: dark
```

Unknown top-level keys are ignored at load time so future settings can be added without breaking older binaries.

The file lives alongside `~/.agent-runner/config.yaml` but is owned by a separate package and has a separate schema. This avoids entangling user-prefs with the existing profile/agents config-loading code.

New package: `internal/usersettings/` exposing approximately:

```go
package usersettings

type Theme string  // "light" | "dark"

type Settings struct {
    Theme Theme  // empty when unset/invalid; caller decides whether to prompt
}

// Load reads ~/.agent-runner/settings.yaml. Missing file, unparseable file,
// non-mapping root, missing theme key, and invalid theme value all return
// (Settings{}, nil) — i.e., they are signaled by Theme == "" rather than by
// error. Only true I/O errors (e.g., permission denied on read) return error.
func Load() (Settings, error)

// Save writes the file atomically (temp + rename) and creates the parent
// directory if missing.
func Save(s Settings) error

// Path returns the resolved settings.yaml path for diagnostic messages.
func Path() (string, error)
```

The "missing/invalid is not an error" convention keeps the call site simple: callers check `theme == ""` to decide whether to prompt, and let real I/O errors bubble up.

### First-launch modal

New package: `internal/themeprompt/` containing a small bubbletea program that:
1. Renders a centered card with `Light` and `Dark` options.
2. Pre-selects the cursor based on `lipgloss.HasDarkBackground()` (called *before* any `SetHasDarkBackground` call so the probe still runs).
3. Handles up/down/left/right arrow keys to change selection.
4. On Enter, returns the selection.
5. On Esc or Ctrl+C, returns a "cancelled" sentinel.

The package exports a single `Prompt() (Theme, bool, error)` (or similar) that runs the bubbletea program to completion and returns the choice. The caller in `main.go` is responsible for translating the result into a `usersettings.Save` call or an exit.

The modal's own colors come from whatever lipgloss is currently doing — we deliberately don't attempt visual perfection on the prompt itself, since by definition no theme is persisted yet. A safe default is to use `lipgloss.AdaptiveColor` tokens just like the rest of the TUI, accepting that the prompt's contrast may briefly look the same way the rest of the TUI looked when the user came in to fix it. This is acceptable because: (a) the prompt is small and high-contrast in either palette, and (b) it's the only screen the user will see in this state.

### Wiring in main.go

The check sits in `cmd/agent-runner/main.go`'s `run()`, after argument parsing, after `-version` / `-validate` early-exit branches, after the `-headless`/`AGENT_RUNNER_NO_TUI` branch (which bypasses TUI entirely), and **before** any bubbletea program is constructed for the list, run-view, paramform, or `-resume` selector.

Pseudocode:

```go
if !willEnterTUI(flags, env) {
    // -headless, -version, -validate, AGENT_RUNNER_NO_TUI=1, -resume <id> headless
    return runWithoutTUI(...)
}

settings, err := usersettings.Load()
if err != nil {
    // real I/O error
    fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
    return 1
}

if settings.Theme == "" {
    chosen, ok, err := themeprompt.Prompt()
    if err != nil {
        fmt.Fprintf(os.Stderr, "agent-runner: %v\n", err)
        return 1
    }
    if !ok {
        // user pressed Esc / Ctrl+C
        return 1  // or whatever the project's "cancelled by user" code is
    }
    settings.Theme = chosen
    if err := usersettings.Save(settings); err != nil {
        fmt.Fprintf(os.Stderr, "agent-runner: failed to save settings: %v\n", err)
        return 1
    }
}

applyTheme(settings.Theme)  // calls lipgloss.SetHasDarkBackground(...)
// ... proceed into the requested TUI ...
```

The branching point ("will we enter a TUI?") already exists implicitly in `main.go` because `-headless`, `-validate`, `-version`, and explicit `-resume <id>` are handled before the TUI launches today. The implementer should consolidate the check into a single helper rather than scattering it across each entry path.

For the auto-launched live run-view (a workflow runs from a non-TUI invocation but ends up rendering live progress), the same check applies: any path that will produce styled TUI output must guarantee theme is set first. In practice this means calling the load/prompt/apply sequence at the top of `run()` for any invocation that *could* enter a TUI, even if that branch isn't taken in this particular run. The simplest implementation: load + (prompt-if-needed) + apply unconditionally for invocations that aren't in `-headless` / `-validate` / `-version`. The cost (one file read) is negligible.

### Atomic write

Standard temp-and-rename:

```go
dir := filepath.Dir(path)
if err := os.MkdirAll(dir, 0o755); err != nil { return err }
tmp, err := os.CreateTemp(dir, "settings-*.yaml.tmp")
// ... write YAML ...
// ... close ...
if err := os.Rename(tmp.Name(), path); err != nil { return err }
```

On rename failure, attempt to remove the tmp file but don't mask the original error. File mode `0o600` is appropriate (user-only) since `~/.agent-runner/config.yaml` is similarly user-scoped.

### Detection-only-for-cursor

`lipgloss.HasDarkBackground()` is called by `themeprompt.Prompt()` before `SetHasDarkBackground` is ever invoked, so the probe still runs naturally. The call is one-shot and its result is only used to position the cursor. The persisted value is always the literal string `light` or `dark`, never `auto`.

## Decisions

**D1. Use `SetHasDarkBackground` rather than converting tokens to fixed Color.**
  - Why: smaller blast radius, preserves the `AdaptiveColor` two-variant model, keeps `internal/tuistyle/styles.go` intact.
  - Alternative considered: define two parallel palettes (`lightPalette`, `darkPalette`) and switch between them. Rejected because it doubles the maintenance burden every time we add or tweak a token.

**D2. Separate `~/.agent-runner/settings.yaml` rather than extending `config.yaml`.**
  - Why: `config.yaml` has a strict profile/agents schema and validation. Mixing user-UI prefs into it would entangle two unrelated concerns. A new file gives future user-prefs a clean home.
  - Alternative considered: a top-level `ui:` key in `config.yaml`. Rejected to avoid cross-contamination of schemas and load-error handling.

**D3. First-launch flow is a bubbletea modal, not a CLI prompt or auto-write.**
  - Why: every TUI entry point already requires bubbletea, and the modal slots in cleanly. A plain stdin prompt would feel out of place in a TUI app and would be awkward when the user landed in the TUI from `-inspect <id>`.
  - Alternative considered: silently auto-write whatever detection says on first run. Rejected because it would hide the very problem this change is fixing — when detection is wrong, the silent default would be wrong, and the user wouldn't know the file existed to fix it.

**D4. Missing/invalid settings → re-prompt, not fail.**
  - Why: keeps the recovery path uniform. Any state that isn't "valid theme persisted" is treated as "ask the user again".
  - Alternative considered: hard error on parse failure to alert the user that something corrupted their config. Rejected because the user's recovery is "delete and re-pick" anyway, and the modal does that for them.

**D5. No env var override.**
  - Why: env-var precedence (e.g., `AGENT_RUNNER_THEME=dark`) was an early proposal. It was deliberately rejected by the user during planning. Adding it later if needed is straightforward; removing it would be a breaking change.

**D6. Esc/Ctrl+C exits without writing.**
  - Why: the user explicitly didn't want a "skip" path. Exiting cleanly is the only escape hatch from the modal, and it preserves "next launch will re-prompt".

**D7. Detection only seeds cursor; not persisted.**
  - Why: persisting an `auto` value would re-introduce the original bug whenever detection later disagrees with itself across launches.

**D8. Headless / non-TUI paths are unaffected.**
  - Why: those paths produce no styled rendering. Forcing a theme prompt on a `-headless` run would regress automation use cases (CI, scripts).

## Risks / Trade-offs

- **The prompt itself may render with the same broken contrast that motivated this change**, since by definition no theme is persisted when it appears. Acceptable: the prompt is small, only two options, and a one-time experience. The user can read it well enough to pick. After picking, every subsequent launch is fixed.
- **Users who land in the TUI in a context without a working stdin/TTY will be unable to dismiss the prompt** (e.g., piping output). This is consistent with how the TUI already requires a TTY today (`AGENT_RUNNER_NO_TUI=1` is the existing escape hatch); the failure mode shifts from "TUI with bad colors" to "TUI refuses to start". The `-headless` flag and existing env var remain valid bypass mechanisms.
- **Hand-editing `settings.yaml` is the only way to change theme post-first-launch** until a settings UI is added later. The user accepted this in planning. Documenting "delete the file to re-prompt" in user-facing docs is a small but real ergonomic cost.
- **`lipgloss.SetHasDarkBackground` is a global mutable**. If a test in this codebase ever runs the TUI in-process in parallel with another test that needs a different background, they will race. Existing tests in `internal/runview/view_test.go` already call `lipgloss.SetColorProfile` globally, so the pattern is established. New tests that depend on theme should set `HasDarkBackground` explicitly in their setup.
- **Concurrent overlapping writes** (two processes confirming themes simultaneously) are handled by the rename atomicity, but the *last writer wins*. Acceptable: the value is a single small enum and either is valid; no data is lost.
