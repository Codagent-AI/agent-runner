## Context

agent-runner has no visibility layer. The only way to see what runs exist, what state they're in, or whether one is active is to manually inspect `~/.agent-runner/projects/`. The `--resume` flag blindly picks the most recent session with no user choice. This change introduces a terminal UI as the primary entry point for inspecting and resuming workflow runs.

The TUI is a product investment — the vision is for agent-runner to evolve into a delightful terminal experience. This is the first step.

## Goals / Non-Goals

**Goals:**
- `--list`, `--resume` (no session), and bare `agent-runner` all launch the run list TUI
- TUI shows runs across three scopes: current directory, worktrees, all directories
- PID lock file enables reliable active-run detection
- codagent.dev visual identity with dark/light theme support via lipgloss `AdaptiveColor`
- 2s auto-refresh, pulsing active indicator

**Non-Goals:**
- Inspect UI (drill into a run's step detail) — planned as a follow-up change
- Run deletion or cleanup
- Filtering, sorting, or search
- Remote or multi-machine state

## Approach

### New packages

**`internal/runlock`**

PID lock file lifecycle. Knows nothing about workflows — only about process liveness.

```go
Write(sessionDir string) error         // write lock file with os.Getpid()
Delete(sessionDir string)              // delete lock file (best-effort)
Check(sessionDir string) LockStatus   // Active | Stale | None
```

`LockStatus`:
- `None` — no lock file present
- `Active` — lock file present, PID is alive
- `Stale` — lock file present, PID is dead

**`internal/runs`**

Run discovery and status assembly. Reads session directories, delegates lock checks to `runlock`.

```go
type RunInfo struct {
    SessionID    string
    SessionDir   string
    WorkflowName string
    CurrentStep  string   // empty if completed
    Status       Status   // Active | Inactive | Completed
    StartTime    time.Time
}

ListForDir(projectDir string) ([]RunInfo, error)
```

Status determination:
- `runlock.LockActive` → `Active`
- `runlock.LockStale` → `Inactive`
- `runlock.LockNone` + `state.json` present → `Inactive`
- `runlock.LockNone` + no `state.json` → `Completed`

Sorted most recent first (session ID timestamp, parsed from directory name).

**`internal/tui`**

bubbletea Model. Three tabs; Worktrees and All tabs have a two-level sub-state (Picker → RunList).

```text
Model
├── activeTab      Tab (CurrentDir | Worktrees | All)
├── currentDir     string
├── projectsRoot   string           // ~/.agent-runner/projects/
├── currentRuns    []runs.RunInfo
├── worktrees      []WorktreeEntry  // nil = not a git repo or no worktrees
├── allDirs        []DirEntry
├── subView        SubView (Picker | RunList)
├── pickerCursor   int
├── listCursor     int
├── selectedDir    string           // for Worktrees/All drill-in
├── refreshTick    tea.Ticker       // 2s — reloads run data
└── pulseTick      tea.Ticker       // 50ms — drives active dot pulse
```

Worktrees and All tabs both use the same Picker → RunList navigation pattern. Esc from RunList returns to Picker.

### Styling

Library: `github.com/charmbracelet/bubbletea` + `github.com/charmbracelet/lipgloss`.

All colors defined in `internal/tui/styles.go` using `lipgloss.AdaptiveColor` (dark/light variant per token). Palette derived from codagent.dev:

```text
Token           Dark      Light
─────────────────────────────────
active green    #4ade80   #16a34a   (pulse: ↔ #2d8f57 / #86efac)
inactive amber  #f0a830   #b45309
completed gray  #4b5a6e   #9ca3af
accent cyan     #5ce0d8   #0891b2   (header, active tab, cursor ▶)
body text       #c9d1d9   #1f2937
dim text        #4b5a6e   #9ca3af
selected text   #ffffff   #111827
```

Visual rules:
- No outer border — flat layout
- Status shown as colored dot only (`●` / `○` / `✓`), no text label
- Header: "Agent Runner" in accent cyan
- Active tab: bold + cyan underline. Inactive tabs: dim.
- Selected row: `▶` cursor in cyan + bright white text. Unselected rows: dim.
- Active `●` pulses between two green shades on a ~1s sinusoidal cycle (driven by 50ms tick)
- Scroll indicator: single thin bar on right edge when list overflows (lipgloss viewport)
- Narrow terminals: truncate workflow name first

### Runner changes

**`initRunState`** — after session directory is created, call `runlock.Write(sessionDir)`. Non-fatal on error (spec requires run to proceed without lock if write fails).

**`finalizeRun`** — call `runlock.Delete(sessionDir)` before returning, regardless of outcome.

**Project `meta.json`** — on any run start, if `~/.agent-runner/projects/<encoded>/meta.json` does not exist, write it:
```json
{"path": "/Users/paul/codagent/agent-runner"}
```
Used by the All tab to display human-readable directory paths. `EncodePath` is lossy so this is the only reliable source of the original CWD. Pre-existing runs without `meta.json` show the encoded directory name until a new run is started from that directory.

### CLI changes

```text
agent-runner                       → handleList() — TUI
agent-runner --list                → handleList() — TUI
agent-runner --resume              → handleList() — TUI
agent-runner --resume <id>         → handleResume(id)
agent-runner <workflow> [params]   → handleRun(...)
agent-runner --validate <workflow> → handleValidate(...)
```

`--resume` stays a `bool` flag. When `--resume` is set and `args[0]` is present, it is treated as the session ID. Further positional args are rejected. This avoids any change to the flag library.

`--session` flag is removed. Resume hint printed on failure changes from `--resume --session %s` to `--resume %s`.

### Worktree detection

Run `git worktree list --porcelain` from the current directory. Parse output to collect all worktree paths (main checkout + linked worktrees). For each path, encode it via `audit.EncodePath` and check if `~/.agent-runner/projects/<encoded>/runs/` exists and has entries.

Current directory's worktree entry is listed first; remaining entries sorted alphabetically by directory basename. If `git` is not in PATH or the directory is not a git repo, the Worktrees tab is hidden entirely.

## Decisions

**`--resume` as bool with optional positional arg** — keeps `flag.Bool` unchanged. Session ID becomes `args[0]` when `--resume` is set. Cleaner UX than `--resume=<id>` (equals syntax) and avoids adding a flag library dependency.

**`internal/runlock` as a separate package** — lock file has a distinct concern (process liveness) from state file (workflow progress). Keeping them separate avoids coupling and makes `runlock` independently testable.

**`internal/runs` as discovery layer** — TUI and any future tooling share one well-tested function for reading run state. TUI never touches state files or lock files directly.

**Two-level navigation for Worktrees and All** — mirrors the mental model: pick a scope, then see its runs. Flat grouped lists were considered but harder to navigate with keyboard and harder to extend (e.g., future actions on a whole directory).

**meta.json for project path** — `EncodePath` replaces `/`, `.`, `_` with `-`, making reverse-engineering the original path unreliable. Storing the original path in a sidecar file is the simplest correct solution.

**AdaptiveColor for theming** — one palette definition covers both dark and light terminals. No runtime theme-switching logic needed.

## Risks / Trade-offs

- **PID alive check on Windows** — `os.FindProcess` on Windows always returns non-nil regardless of whether the process is alive. This codebase targets macOS. If Windows support is added later, `runlock.Check` needs a platform-specific implementation. → Acceptable for now; document the gap.
- **git dependency for worktrees** — Worktrees tab runs `git worktree list --porcelain`. If `git` is not in PATH, the tab is hidden silently. → Graceful degradation, no crash.
- **2s filesystem polling** — simpler than inotify/fsevents and sufficient at this scale. → Acceptable given the use case.
- **Stale `meta.json` on directory move** — if a project directory is renamed or moved, the stored path becomes wrong. It will be overwritten on the next run from that directory. → Self-healing, low impact.

## Migration Plan

Three breaking CLI changes:

1. **`--session` flag removed** — migrate `--resume --session <id>` to `--resume <id>`.
2. **`--resume` with no session ID shows TUI** — previously auto-resumed the most recent session.
3. **Bare `agent-runner` shows TUI** — previously printed a usage error.

No data migration. All existing run state, audit logs, and session directories are compatible. `meta.json` files are created lazily on next run — old directories without them degrade gracefully to showing encoded names in the All tab.
