# Task: TUI and CLI entry point

## Goal

Implement the run list terminal UI using bubbletea + lipgloss, and update the CLI entry point so that `agent-runner`, `agent-runner --list`, and `agent-runner --resume` (with no session ID) all launch it. Remove the `--session` flag.

## Background

**Why this exists:** The run list TUI is the primary new user-facing capability of this change. It gives users visibility into all workflow runs — their status, current step, and when they started — and lets them resume a run by pressing Enter. The CLI changes make it the natural default when no workflow is specified.

**TUI design overview (read `design.md` and `tui-mockups.md` for full details):**

Three tabs: **Current Dir** | **Worktrees** | **All**. The Worktrees and All tabs both use two-level navigation: a picker (list of worktrees or directories) → drill-in to see that scope's runs. Esc goes back from drill-in to picker. Tab / Shift+Tab cycles between the three tabs. The Worktrees tab is hidden when the current directory is not inside a git repo or has no sibling worktrees with runs.

Bare `agent-runner` with no args, `--list`, and `--resume` with no session ID all route to the same `handleList()` function in `cmd/agent-runner/main.go`.

**Key files to read before starting:**
- `openspec/changes/list-runs/design.md` — full architecture, package layout, styling decisions
- `openspec/changes/list-runs/tui-mockups.md` — visual reference for all views and color annotations
- `cmd/agent-runner/main.go` — current CLI flag parsing; all changes land here
- `internal/runs/runs.go` — `ListForDir`, `ReadProjectPath`, `RunInfo`, `Status` (built in the prior task; read this before writing the TUI)
- `go.mod` — add bubbletea and lipgloss here

**Dependencies to add:**
```
github.com/charmbracelet/bubbletea
github.com/charmbracelet/lipgloss
```

Run `go get` for both and commit the updated `go.mod` and `go.sum` as part of this task.

**`internal/tui` package structure:**

```
internal/tui/
  styles.go    — lipgloss AdaptiveColor palette (all color tokens in one place)
  model.go     — bubbletea Model, Init, Update, View
  worktree.go  — git worktree detection via `git worktree list --porcelain`
```

**Color palette (`styles.go`)** — use `lipgloss.AdaptiveColor` for every token:

```go
// Dark variant / Light variant
activeGreen   = AdaptiveColor{Dark: "#4ade80", Light: "#16a34a"}
// active pulse oscillates between activeGreen and activeDim:
activeDim     = AdaptiveColor{Dark: "#2d8f57", Light: "#86efac"}
inactiveAmber = AdaptiveColor{Dark: "#f0a830", Light: "#b45309"}
completedGray = AdaptiveColor{Dark: "#4b5a6e", Light: "#9ca3af"}
accentCyan    = AdaptiveColor{Dark: "#5ce0d8", Light: "#0891b2"}
bodyText      = AdaptiveColor{Dark: "#c9d1d9", Light: "#1f2937"}
dimText       = AdaptiveColor{Dark: "#4b5a6e", Light: "#9ca3af"}
selectedText  = AdaptiveColor{Dark: "#ffffff", Light: "#111827"}
```

**bubbletea model:**

```go
type tab int
const (
    tabCurrentDir tab = iota
    tabWorktrees
    tabAll
)

type subView int
const (
    subViewPicker  subView = iota
    subViewRunList
)

type Model struct {
    // tabs
    activeTab   tab
    worktreeTab struct {
        subView      subView
        pickerCursor int
        listCursor   int
        worktrees    []WorktreeEntry // nil = not a git repo
        selectedDir  string
    }
    allTab struct {
        subView      subView
        pickerCursor int
        listCursor   int
        dirs         []DirEntry
        selectedDir  string
    }
    currentDirCursor int

    // data
    projectDir     string          // current working directory
    projectsRoot   string          // ~/.agent-runner/projects/
    currentRuns    []runs.RunInfo
    pulsePhase     float64         // 0.0–1.0, advances each 50ms tick
}

type WorktreeEntry struct {
    Name    string          // directory basename
    Path    string          // full path
    Encoded string          // audit.EncodePath(Path)
    Runs    []runs.RunInfo
}

type DirEntry struct {
    Path    string          // original path from meta.json (or encoded if missing)
    Encoded string
    Runs    []runs.RunInfo
}
```

**Tickers:**
- `refreshTick`: `tea.Every(2*time.Second, ...)` — reloads run data from disk on each tick
- `pulseTick`: `tea.Every(50*time.Millisecond, ...)` — advances `pulsePhase`; used to lerp the active `●` color between `activeGreen` and `activeDim`

**Active dot pulse:** On each pulseTick, advance `pulsePhase` by `(50ms / 1000ms) * 2π`. Compute interpolated color using a sine wave: `t = (sin(pulsePhase) + 1) / 2` then lerp between the two hex values component-wise. Use lipgloss `Color(hexString)` with the computed hex for that frame.

**Worktree detection (`worktree.go`):**

Run `git worktree list --porcelain` from the current directory. Parse the output: each worktree block starts with `worktree <path>`, followed by `branch refs/heads/<name>` (or `detached`). Collect all paths. The first entry is always the main checkout. Sort: current directory first, then rest alphabetically by basename. For each path, check if `~/.agent-runner/projects/<audit.EncodePath(path)>/runs/` exists and has entries — if not, include in the list but show "no runs".

If `git` is not in PATH or the command fails or the directory is not a git repo, return nil (Worktrees tab is hidden).

**Key bindings:**

| Key | Action |
|-----|--------|
| ↑ / k | Move cursor up |
| ↓ / j | Move cursor down |
| Tab | Next tab |
| Shift+Tab | Previous tab |
| Enter | On picker: drill in. On run list: exit TUI and resume selected run |
| Esc | On run list (Worktrees/All drill-in): go back to picker |
| q / Ctrl+C | Quit |

**Resuming from the TUI:** When the user presses Enter on a run in a run list view, the TUI returns a special `tea.Cmd` that causes the program to quit and emit the selected `RunInfo.SessionDir`. Back in `main.go`, `handleList()` detects the selected session and calls `handleResume(sessionID)` to resume it.

**Scroll:** Use lipgloss's `lipgloss.NewStyle().MaxHeight(...)` or a simple offset-based viewport in the model. Show a scroll indicator character on the right edge when the list exceeds the available height.

**Narrow terminal handling:** When rendering run list rows, truncate `WorkflowName` to fit before truncating `CurrentStep`. Use `lipgloss.Width` to measure and `runewidth` (already a transitive dep of bubbletea) to truncate safely.

**CLI changes in `cmd/agent-runner/main.go`:**

1. Add `--list` bool flag.
2. Change `--resume` to remain a `bool` flag but remove the automatic `*resumeFlag = true` when `*sessionFlag != ""` logic.
3. Remove `--session` flag entirely (delete the `flag.String("session", ...)` line and all uses).
4. When `--resume` is set: treat `args[0]` (if present) as the session ID; further positional args are an error.
5. When no flags are set and `len(args) == 0`: call `handleList()`.
6. `--list` set: call `handleList()`.
7. `--resume` with no args: call `handleList()`.
8. `--resume <id>`: call `handleResume(args[0])`.
9. Update `resolveResumeStatePath` to remove the "find most recent by mtime" branch — it now only handles the non-empty session ID case. The no-session case goes to `handleList()`, never to `resolveResumeStatePath`.
10. Update the flag usage text to reflect the removed `--session` flag and new `--list` flag.

**`handleList()` function:**
```go
func handleList() int {
    // Build and run the bubbletea program.
    // On quit with no selection: return 0.
    // On quit with a selected session: call handleResume(sessionID).
}
```

## Spec

### Requirement: --list launches the run list TUI

The CLI SHALL accept a `--list` flag that launches a terminal UI showing workflow runs.

#### Scenario: --list launches TUI
- **WHEN** `--list` is passed
- **THEN** the terminal UI launches showing runs for the current project directory

#### Scenario: --resume without session ID launches TUI
- **WHEN** `--resume` is passed without a session ID
- **THEN** the terminal UI launches

#### Scenario: No arguments launches TUI
- **WHEN** `agent-runner` is invoked with no flags and no arguments
- **THEN** the terminal UI launches

### Requirement: Current directory view

The default TUI view SHALL show runs for the current project directory, sorted most recent first.

#### Scenario: Runs exist for current directory
- **WHEN** the TUI opens and runs exist for the current project directory
- **THEN** those runs are shown sorted most recent first

#### Scenario: No runs for current directory
- **WHEN** the TUI opens and no runs exist for the current project directory
- **THEN** an empty state is shown with the option to switch to the all-directories view

### Requirement: All-directories view

The TUI SHALL provide a view showing all project directories with runs. This view SHALL always be reachable from the current directory view.

#### Scenario: Navigate to all-directories view
- **WHEN** the user switches to the All tab
- **THEN** a directory picker is shown listing all project directories that have runs

### Requirement: Worktree view

When inside a git repo with sibling worktrees, the TUI SHALL show a Worktrees tab allowing the user to view runs for each working copy.

#### Scenario: Sibling worktrees detected
- **WHEN** the current directory is inside a git repo and sibling worktrees exist
- **THEN** the Worktrees tab is shown, listing all working copies (main checkout plus worktrees), including those with no runs

#### Scenario: No sibling worktrees
- **WHEN** the current directory is not inside a git repo or no sibling worktrees exist
- **THEN** the Worktrees tab is not shown

### Requirement: Resume from TUI

Pressing Enter on a run in the TUI SHALL exit the TUI and resume that run. Only inactive runs (resumable) SHALL be selectable for resume. Active and completed runs SHALL not be resumable from the TUI.

#### Scenario: Resume inactive run from TUI
- **WHEN** the user presses Enter on an inactive run
- **THEN** the TUI exits and the selected run is resumed

#### Scenario: Completed run not resumable
- **WHEN** the user presses Enter on a completed run
- **THEN** nothing happens (the run is not selectable for resume)

#### Scenario: Active run not resumable
- **WHEN** the user presses Enter on an active run
- **THEN** nothing happens (the run is not selectable for resume)

### Requirement: Resume by session ID (modified)

The CLI SHALL accept `--resume` optionally followed by a session ID. The separate `--session` flag is removed.

#### Scenario: Resume with explicit session ID
- **WHEN** `--resume <id>` is passed and a session with that ID exists
- **THEN** the runner resumes workflow execution from that session's saved state

#### Scenario: Resume with nonexistent session ID
- **WHEN** `--resume <id>` is passed and no session matches that ID
- **THEN** the runner exits with an error indicating the session was not found

#### Scenario: Resume rejects extra positional arguments
- **WHEN** `--resume` is passed with more than one positional argument
- **THEN** the runner exits with an error indicating resume mode accepts at most one argument (the session ID)

## Done When

- `go.mod` includes bubbletea and lipgloss; `go build ./...` succeeds.
- `agent-runner` with no args launches the TUI without error.
- `agent-runner --list` launches the TUI.
- `agent-runner --resume` with no args launches the TUI.
- `agent-runner --resume <valid-session-id>` resumes that session (existing behavior preserved).
- `agent-runner --session` is no longer a recognized flag.
- The TUI renders all three tabs (Current Dir, Worktrees if in a git repo, All), navigates correctly, and exits cleanly on `q`.
- Pressing Enter on an inactive run in the TUI exits and resumes that run.
- Pressing Enter on a completed or active run does nothing (not selectable for resume).
- All spec scenarios above are addressed.

# TUI Mockups — Run List

---

## Color palette (codagent.dev theme)

```
                      DARK            LIGHT
Active ●   green   #4ade80         #16a34a    (pulse: ↔ #2d8f57 / #86efac)
Inactive ○ amber   #f0a830         #b45309
Completed ✓ gray   #4b5a6e         #9ca3af
Accent (tabs,      #5ce0d8         #0891b2
  header, cursor)  (cyan)
Body text          #c9d1d9         #1f2937
Dim text           #4b5a6e         #9ca3af
Selected text      #ffffff         #111827
```

No outer border. Status shown as colored dot only (no "active" / "inactive" text label).
"Agent Runner" header in cyan. AdaptiveColor picks dark vs light variant.
Scroll indicator: a single thin bar on the far right edge of the list area (lipgloss).
  Not a │ per row — one continuous bar rendered by the TUI framework.

---

## Current Dir tab (default)

```
  Agent Runner                                                           

  ● Current Dir    ○ Worktrees    ○ All

  ~/codagent/agent-runner

▶  plan-change        design           ●  09:14 today
   plan-change        write-specs      ○  04:27 today
   plan-change        write-specs      ○  Mar 29
   session-review     —                ✓  Apr 09




  ↑↓ navigate   enter resume   tab switch tab   q quit
```

Notes:
- "Agent Runner" in cyan. Timestamp dim/neutral on the right.
- Active tab: bold + cyan underline. Inactive tabs: dim.
- ▶ cursor in cyan. Selected row: bright white text. Unselected: dim.
- ● pulses between two green shades (~1s cycle).
- Scroll indicator: single thin bar on right edge when list overflows.
- Truncate workflow name first on narrow terminals.

---

## Worktrees tab — picker

```
  Agent Runner                                                           

  ○ Current Dir    ● Worktrees    ○ All

  ~/codagent/agent-runner  (git repo)

▶  agent-runner    ~/codagent/agent-runner                          2 runs
   list-runs       ~/codagent/agent-runner/worktrees/list-runs      ● 1 active
   main            ~/codagent/agent-runner/worktrees/main           no runs




  ↑↓ navigate   enter view runs   tab switch tab   q quit
```

Worktrees tab only shown when inside a git repo with at least one worktree.
Current dir entry always first, rest alphabetical by name.

---

## Worktrees tab — drilled into a worktree's runs

```
  Agent Runner                                                           

  ○ Current Dir    ● Worktrees    ○ All

  ← Worktrees  /  list-runs

▶  plan-change        design           ●  09:14 today
   plan-change        write-specs      ○  Mar 29




  ↑↓ navigate   enter resume   esc back   tab switch tab   q quit
```

---

## All tab — directory picker

```
  Agent Runner                                                           

  ○ Current Dir    ○ Worktrees    ● All

  All project directories

▶  ~/codagent/agent-runner                               3 runs  ● 1 active
   ~/codagent/agent-runner/worktrees/list-runs           1 run   ● 1 active
   ~/other-project                                       1 run




  ↑↓ navigate   enter view runs   tab switch tab   q quit
```

---

## All tab — drilled into a directory's runs

```
  Agent Runner                                                           

  ○ Current Dir    ○ Worktrees    ● All

  ← All  /  ~/codagent/agent-runner

▶  plan-change        design           ●  09:14 today
   plan-change        write-specs      ○  04:27 today
   session-review     —                ✓  Apr 09




  ↑↓ navigate   enter resume   esc back   tab switch tab   q quit
```

---

## Empty state (Current Dir has no runs)

```
  Agent Runner                                                           

  ● Current Dir    ○ Worktrees    ○ All

  ~/codagent/agent-runner


               No runs found for this directory.

               Press tab to view other scopes.




  tab switch tab   q quit
```

