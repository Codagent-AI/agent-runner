## Context

The previous `list-runs` change added a bubbletea/lipgloss run-list TUI and deferred per-run detail inspection to a follow-up. This change is that follow-up: a single-run detail view reached by pressing Enter on any row in the list TUI, or directly via `--inspect <run-id>`. The view renders the workflow's step tree on the left with live status, and a scrollable detail pane on the right.

Most of the required data already exists on disk per run:

- `~/.agent-runner/projects/<encoded-path>/runs/<session-id>/state.json` — latest-step pointer, workflow name/file, params.
- `~/.agent-runner/projects/<encoded-path>/runs/<session-id>/audit.log` — JSONL event stream with rich per-step data (interpolated commands, agent profile/model/cli/session, captured stdout/stderr, loop iterations, sub-workflow path + interpolated params).
- `~/.agent-runner/projects/<encoded-path>/runs/<session-id>/run.lock` — PID lock file (present + alive = active run).

The design reuses these; no changes to the runner/audit-log are required.

## Goals / Non-Goals

**Goals:**
- A navigable single-run view consistent with the list screen's visual language.
- Correctness on active (live-updating), completed, failed, inactive, and pending runs.
- Drill-in navigation through loops, loop iterations, and sub-workflows with breadcrumbs.
- Scrollable per-step output with graceful handling of large buffers and non-UTF8 bytes.
- Resume action on agent steps that exec's the step's agent CLI with `--resume <session-id>` (e.g. `claude --resume <uuid>`), resuming the agent's own conversation — not agent-runner's workflow-run `--resume` flag.
- `--inspect <id>` CLI flag for direct entry.

**Non-Goals:**
- Editing, re-running, aborting, or deleting steps from the view.
- Diffing runs or comparing sessions.
- Remote / multi-machine inspection — local filesystem only.
- Removing the bare "nested-steps group" (`steps:` without `loop:`) from the engine — that's a separate change.
- Prefix matching on run IDs.

## Approach

### Package layout

```
internal/tuistyle/   (new)
  styles.go          — lipgloss AdaptiveColor palette + style instances (moved from tui/)
  format.go          — generic formatters: fitCell, fitCellLeft, adjustOffset,
                       shortenPath, formatTime, lerpColor, parseHex, sanitize
                       (moved from tui/, minus runSummary)
  ticker.go          — doPulse, doRefresh, PulseMsg, RefreshMsg (extracted from tui/model.go)

internal/tui/        (existing, lightly modified)
  model.go, view.go, worktree.go    — list view only
  format.go          — now just runSummary (list-specific)
  styles.go          — re-exports or imports from tuistyle

internal/runview/    (new)
  model.go           — bubbletea Model, Init, Update, View
  tree.go            — StepNode tree + builder (workflow file + audit events → tree)
  audit.go           — JSONL tailer (byte-offset tracked, partial-line buffered)
  detail.go          — per-step-type detail rendering
  output.go          — large-output tail + load-full + U+FFFD sanitize
  breadcrumb.go      — nav path formatting + drill-in/out logic

cmd/agent-runner/
  main.go            — adds --inspect flag; top-level switcher Model
```

### Top-level switcher Model

A single bubbletea `Program` is run for the TUI. The `Program`'s Model is a small switcher that holds either the list sub-model or the runview sub-model and routes messages:

```go
type shell struct {
    list    *tui.Model
    runview *runview.Model
    mode    mode  // showingList | showingRunView
    // set just before tea.Quit so main can dispatch:
    resumeSessionID string
}
```

Messages between sub-models and the shell:
- `tui.ViewRunMsg{SessionDir, ProjectDir}` — list → shell: "user hit Enter on this run"
- `runview.BackMsg` — runview → shell: "Esc at top level, go back to list"
- `runview.ResumeMsg{AgentCLI, SessionID}` — runview → shell: "exec `<AgentCLI> --resume <SessionID>`". Note: this is the **agent CLI's** own session ID (e.g. a Claude UUID), not an agent-runner run ID. `handleResume` resumes workflow runs; the runview resume action resumes a single agent conversation — different subsystems, different ID spaces.
- `runview.ExitMsg` — runview → shell: "`q` — quit the whole program"

On `ResumeMsg` or `ExitMsg`, the shell stores the relevant state and returns `tea.Quit`. After `Program.Run()` returns, `main.go` inspects the stored state: if a resume was requested, it exec's the step's agent CLI directly with `--resume <session-id>` (replacing the current process via `syscall.Exec` so the agent owns the terminal). This is NOT `handleResume`, which is for agent-runner workflow runs.

List state (cursor, active tab, scroll offsets, drilled-in selection) is preserved across list→runview→list round-trips because the list sub-model is kept alive in the shell.

### Step tree model

The runview operates on a `StepNode` tree built by merging the workflow YAML (static structure) with the audit log (runtime data).

```go
type StepNode struct {
    ID        string
    Type      StepType     // shell, headlessAgent, interactiveAgent, loop, subWorkflow
    Status    Status       // pending, inProgress, success, failed, skipped
    Children  []*StepNode  // for loops (iteration nodes) and sub-workflows (inner steps)

    // Static data from workflow YAML:
    StaticCommand    string
    StaticPrompt     string
    StaticWorkflow   string            // sub-workflow canonical name or fallback path
    StaticLoopMax    *int
    StaticLoopOver   string
    StaticLoopAs     string

    // Runtime data from audit events:
    InterpolatedCommand string
    InterpolatedPrompt  string
    InterpolatedParams  map[string]string  // for sub-workflows
    ExitCode            *int
    DurationMs          *int64
    Stdout              []byte
    Stderr              []byte
    CaptureName         string
    AgentProfile        string
    AgentModel          string
    AgentCLI            string
    SessionID           string
    LoopMatches         []string           // for-each resolved paths
    IterationsCompleted int                // for loop nodes
    BreakTriggered      bool
    ErrorMessage        string             // runtime error from audit event

    // Iteration-only fields (when parent is a loop):
    IterationIndex int                     // 0-based internal, displayed 1-based
    BindingValue   string                  // matched value for for-each iterations

    // Auto-flatten marker: when set, drill-in skips straight to FlattenTarget's children.
    FlattenTarget *StepNode
}
```

**Building the tree:**
1. `loader.LoadWorkflow` reads the workflow YAML and populates static fields recursively. Loop bodies are shown as a single loop node (iteration nodes are added at runtime); sub-workflow bodies are loaded lazily on first `sub_workflow_start` event (or on first drill-in when the user Enters a pending sub-workflow row).
2. The audit log is parsed event-by-event; each event's nesting prefix (`[step-a, loop-b:2, sub:foo, step-c]`) locates the target node in the tree. Events mutate the located node (set Status, record interpolated command/prompt, append output chunks for agent steps, etc.).
3. Auto-flatten is computed at tree-build time: for any loop iteration node whose body is a single `Step` with a `workflow:` field, `FlattenTarget` is set to that sub-workflow's first real child-level. Drill-in in the UI checks `FlattenTarget` before descending.

**Status mapping from audit outcomes:**
- `success` / `exhausted` → `success` visual
- `failed` → `failed` visual
- `skipped` → `skipped` visual
- `aborted` → `inProgress` visual (step will resume on next run; no blink when no run is active)
- In-flight (only `step_start` seen, no `step_end`) → `inProgress` visual (blinks when run is active)
- No events at all → `pending`

### Audit log tailing

First entry to a run view: open `audit.log`, read end-to-end, parse every line, apply to the tree, record byte offset at EOF.

On each 2 s refresh (**only while the run is active** — checked via `runlock.Check`):
1. `os.Stat` → if file size equals the stored offset and no partial-line buffer is pending, skip (no new bytes).
2. Seek to stored offset, read new bytes into a buffer concatenated with any partial line held over from last tick.
3. Split by `\n`; every complete line is a JSON event; the remainder (no trailing `\n`) is buffered for next tick.
4. Parse and apply each event as a tree mutation.
5. Advance stored offset by consumed byte count.

Refresh is driven by the shared `tuistyle/ticker.go` helpers so the cadence matches the list screen.

### Rendering

**Layout zones:**
- Header line (`Agent Runner`) — 1 row, top.
- Breadcrumb line — 1 row, format: `← <top-name> [/ <crumb> ...]  ·  started <when>  ·  <run-status>`. The `·  started <when>  ·  <run-status>` suffix is always present at every drill depth. Top-level crumb is the workflow's canonical runnable name (e.g., `implement-change`, `openspec:plan-change`) derived from its resolved path under `workflows/` — sub-workflow crumbs use the same rule with fallback to repo-relative path when outside `workflows/`. Iteration crumbs use `iter N` (1-based).
- Sub-workflow header — 2 rows, only when currently drilled inside a sub-workflow (including auto-flattened targets). Shows `workflow: <canonical-name>` and `params: <name> = <value>, ...`.
- Step list (left column) — width = longest row at the current level, no padding-to-column. Each row: `<status-glyph>  <step-name>[ (N/M)][ <type-glyph>]`. Loops get `(N/M)` after the name, no type glyph. Iterations drop the type glyph too; show `iter N  <binding-value>`.
- Detail pane (right column) — fills remaining width. Header line is the selected step's name. Body is step-type-specific (see "Detail pane content" in spec).
- Help bar — 1 row, bottom, key hints adjusted to the selected step's action.

**Colors (additions to existing list-screen palette):**

```
                          DARK            LIGHT
Failed ✗   red         #f87171         #dc2626
(other tokens unchanged: activeGreen, activeDim, inactiveAmber,
 completedGray, accentCyan, bodyText, dimText, selectedText)
```

Existing palette reused unchanged:

```
                          DARK            LIGHT
Running ●   green      #4ade80 ↔ #2d8f57   #16a34a ↔ #86efac  (pulses)
Pending ○   amber      #f0a830             #b45309
Success ✓   gray       #4b5a6e             #9ca3af
Skipped ⇥   gray       #4b5a6e             #9ca3af
Accent      cyan       #5ce0d8             #0891b2
Body text              #c9d1d9             #1f2937
Dim text               #4b5a6e             #9ca3af
Selected text          #ffffff             #111827
```

Breadcrumb status suffix pulses when run is active (`·  active` green ↔ dim green), renders in red for `·  failed`, gray for `·  completed`, amber for `·  inactive`. Failed step rows tint both the `✗` and the step name in red; other rows keep body-text color.

**Glyphs:**

```
  Status                 Type
  ●  running             $   shell
  ○  pending             ⚙️  headless agent
  ✓  success             💬  interactive agent
  ✗  failed              (none) loop — (N/M) counter is sufficient
  ⇥  skipped             ↳   sub-workflow
```

The legend is reachable via `?` (modal overlay, dismissed with `?` or Esc).

### Mockups

All mockups use real workflows: `implement-change` (top-level) and `implement-task` (sub-workflow reached via auto-flatten).

#### 1. Top level of an active run

```
  Agent Runner

  ← implement-change  ·  started 09:14 today  ·  active

  ●  implement-tasks (3/5)      implement-tasks
  ○  archive $                  loop: for-each over openspec/changes/{{change_name}}/tasks/*.md
  ○  archive-verify $           as: task_file    require_matches: true
  ○  finalize 💬                matches: 5
                                iterations: 3 of 5  (in progress)
                                break_triggered: no

                                press enter to drill in →



  ↑↓ step   pgup/pgdn scroll   enter drill   esc back   q quit
```

#### 2. Drilled into the loop (iteration list)

```
  Agent Runner

  ← implement-change  /  implement-tasks  ·  started 09:14 today  ·  active

  ✓  iter 1   tasks/01-create-spec.md     implement-tasks
  ✓  iter 2   tasks/02-write-runs.md      loop: for-each
  ●  iter 3   tasks/03-write-tui.md       bind: task_file
  ○  iter 4   tasks/04-hook-up-cli.md     matches: 5
  ○  iter 5   tasks/05-add-tests.md       in progress: iter 3

                                          press enter to drill in →



  ↑↓ iteration   pgup/pgdn scroll   enter drill   esc back   q quit
```

#### 3. Enter on iter 3 — auto-flatten past `implement-single-task`

```
  Agent Runner

  ← implement-change  /  implement-tasks  /  iter 3  ·  started 09:14 today  ·  active

  workflow: implement-task
  params:   task_file = tasks/03-write-tui.md

  ●  implement 💬                     implement
  ○  run-validator ↳                  agent: implementor    cli: claude
  ○  check-clean $                    model: (default)      session: new
  ○  commit-leftovers-if-needed 💬    session id: 7f3a-bc91-22ee
  ○  session-report 💬
                                      prompt:
                                      | Implement the task described in
                                      | tasks/03-write-tui.md.
                                      |
                                      | When you are done implementing,
                                      | commit your changes with a clear,
                                      | descriptive commit message. Then
                                      | summarize to the user what you
                                      | changed.

                                      enter → resume session


  ↑↓ step   pgup/pgdn scroll   enter resume   esc back   q quit
```

The iteration is the deepest breadcrumb crumb; the skipped `implement-single-task` sub-workflow step is hidden per the auto-flatten rule, and its path/params surface in the sub-workflow header above the step list.

#### 4. Shell step with large stdout (tail + load full)

```
  Agent Runner

  ← implement-change  ·  started 09:14 today  ·  active

  ✓  implement-tasks (5/5)            archive
  ●  archive $                        $ openspec archive view-run --yes
  ○  archive-verify $
  ○  finalize 💬                      exit: 0       duration: 2.4s

                                      [847 lines total · showing last 35 — press g to load all]

                                      ...
                                      Updated spec files:
                                        - openspec/specs/list-runs/spec.md
                                        - openspec/specs/view-run/spec.md
                                      Cleaned up:
                                        - openspec/changes/view-run/
                                      Done.


  ↑↓ step   pgup/pgdn scroll   g load full   esc back   q quit
```

Banner (`[N lines total · showing last M — press g to load all]`) is persistently visible above the scrollable region; after `g`, it disappears and the full buffer is scrollable. Threshold: **2000 lines or 256 KB, whichever comes first**.

#### 5. Failed step

```
  Agent Runner

  ← implement-change  ·  started 09:14 today  ·  failed

  ✓  implement-tasks (5/5)            archive
  ✗  archive $                        $ openspec archive view-run --yes
  ○  archive-verify $
  ○  finalize 💬                      exit: 1       duration: 180ms

                                      stderr:
                                      | Error: change 'view-run' is not complete.
                                      | 11 of 12 tasks complete.
                                      | Run 'openspec validate view-run' to see issues.

                                      stdout: (empty)


  ↑↓ step   pgup/pgdn scroll   esc back   q quit
```

`✗` and the failed step's name both tint red. `exit:` value renders in red. Breadcrumb suffix flips to `·  failed` in red. When the audit event includes an `error` field (interpolation failure, missing file, missing params), it's shown in place of stderr under the label `error:`.

### Keybindings

| Key | Action |
|---|---|
| ↑ / k, ↓ / j | Move step cursor |
| PgUp / PgDn | Scroll detail pane |
| Enter | Drill into loop/sub-workflow; resume on agent step; no-op on shell |
| Esc | Drill out one breadcrumb level; at top, return to list (or exit if `--inspect`) |
| `g` | Load full output (when truncation banner is visible) |
| `?` | Toggle legend overlay |
| `q` / Ctrl+C | Quit the program |

Mouse wheel scrolls the detail pane when the pointer is over it.

### CLI

- New `--inspect <run-id>` flag. Resolution mirrors `--resume <id>`: only searches the current cwd's project directory. Not found → prints an error and exits with non-zero status.
- No other CLI changes. `--list` and bare invocation still launch the list TUI (which now routes Enter into the runview).

## Decisions

**Single bubbletea Program with a top-level switcher Model.** Alternative: launch separate Programs back-to-back from `main.go`. Chosen for state preservation (list cursor/tab/scroll survive the round-trip for free) and because it's the standard bubbletea pattern for multi-screen apps.

**Separate `runview` package + shared `tuistyle` package.** Alternative: extend the existing `tui.Model` with a run-view sub-state machine. Chosen because the two screens have substantially different state machines and rendering concerns; sharing happens via styles and formatters, not via a monolithic Model.

**Hybrid static + audit-log tree builder.** Alternative: build purely from audit events. Chosen because pending rows (no audit activity yet) and drill-in to pending sub-workflows require the static workflow structure; the spec explicitly calls for both.

**Byte-offset tailing of audit.log, polled only while active.** Alternative: full re-read on every tick. Chosen because audit logs grow unboundedly in long-running loops (the `implement-change` loop could emit thousands of events across iterations); the tail logic is ~50 lines and scales predictably. Inactive runs don't poll at all.

**Resume = tea.Quit + `syscall.Exec` of the step's agent CLI with `--resume <session-id>`.** The runview's resume action resumes the agent's own conversation, not an agent-runner workflow run — so `handleResume` (which takes a run ID) is the wrong path despite the name overlap. Alternative: `os/exec.Cmd` and wait for the child. Chosen `syscall.Exec` so the agent CLI replaces the agent-runner process and inherits the terminal directly (interactive agents need a PTY they own).

**`--inspect` scoped to cwd's project dir (mirrors `--resume`).** Alternative: scan all project dirs for a matching session ID. Chosen for consistency — `--resume` and `--inspect` find sessions the same way; cross-project inspection happens through the list TUI's Worktrees/All tabs.

**Auto-flatten in tree builder, not in UI.** Alternative: check single-sub-workflow pattern at drill-in time in the Update handler. Chosen because tree-time computation centralizes the rule, keeps the UI handlers simple, and gives us a single place to tweak the rule or add tests.

**Large output threshold: 2000 lines or 256 KB.** Best-effort defaults; likely fine for realistic shell outputs. Adjustable via a constant if needed.

**Glyphs: `$`, ⚙️, 💬, (none for loop), ↳.** Loops don't need a glyph because the `(N/M)` counter is unambiguous. Shell `$` and sub-workflow `↳` stay ASCII for portability; agent glyphs use emoji for better recognizability.

## Risks / Trade-offs

- **Partial writes during tail** → Require `\n` termination before parsing; buffer the remainder between ticks. Covered by unit tests with synthetic partial-line inputs.
- **Sub-workflow canonical name fallback** → When a sub-workflow's resolved file is outside `workflows/` (absolute path or `../foo.yaml` crossing the workflows boundary), no canonical `<ns>:<name>` is derivable. Fallback: repo-relative path. Document in sub-workflow header rendering.
- **Blink phase drift between list and runview** → Shared `tuistyle/ticker.go` must own the pulse tick so both screens stay in phase when the user bounces between them.
- **Long loop iteration lists** → For-each loops with hundreds of matches produce a long iteration list. Scrolling is already handled by the step-list viewport, but we should confirm the list uses virtualization/offset-based rendering (the existing list TUI has `adjustOffset` in `format.go`, which moves into `tuistyle` and is reused here).
- **Auto-flatten obscures structure** → Users who genuinely want to see the single `implement-single-task` node lose that ability. Trade-off accepted — the skipped node's path + params are still shown in the sub-workflow header, and this only triggers in the narrow `iteration → single sub-workflow` pattern.
- **Pending-param interpolation without runtime context** → For a pending sub-workflow (never started), the parent's interpolated params are not known. Design decision: show the **raw template string** in the header (e.g., `task_file = {{task_file}}`) rather than fabricate a value.

## Migration Plan

1. Create `internal/tuistyle/` and move `styles.go`, `format.go` (minus `runSummary`), and extract pulse/refresh helpers from `tui/model.go`. Update `tui/` imports.
2. Add `--inspect <id>` flag to `cmd/agent-runner/main.go` (only resolving path; wire later).
3. Build `internal/runview/tree.go` and `audit.go` (pure data, heavily unit-tested; independent of TUI).
4. Build `internal/runview/model.go`, `view.go`, `detail.go`, `output.go`, `breadcrumb.go` (bubbletea wiring and rendering).
5. Add the top-level switcher Model in `cmd/agent-runner/main.go` (or a small `internal/appshell/` package if main gets crowded). Wire list's Enter → runview, runview's Back/Resume/Exit → main.
6. Flip the list's Enter handler from "quit with selection" to "emit `ViewRunMsg`".
7. Update spec requirements as noted below.

No disk-format or audit-log changes. No DB migrations. Rollback is a package revert — no data persists from this change.

## Open Questions

- Confirm `github.com/charmbracelet/x/exp/teatest` is an acceptable test dependency before committing to it for integration tests. If not available, fall back to snapshot tests driven by a captured render cycle.
- Whether to surface the `?` legend key in the help bar at all times (one more keybinding to document) or only as a discoverability nudge on first run.
- If the run is active but has produced only a `run_start` event (no step events yet), should the step list render all rows as `pending` with no "running" indicator on any row? (Current answer: yes — nothing has started.)
