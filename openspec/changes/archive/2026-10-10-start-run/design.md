## Context

The agent-runner TUI currently has three tabs (Current Dir, Worktrees, All) that display existing runs. Starting a new run requires dropping to the CLI with `agent-runner run <workflow> key=value...`. There is no way to discover available workflows or launch one from within the TUI.

The existing codebase uses bubbletea with lipgloss for styling. No charmbracelet component libraries (bubbles/textinput, etc.) are used — all views are custom. Workflow resolution exists for single-name lookup across three scopes (project-local `.agent-runner/workflows/`, user-global `~/.agent-runner/workflows/`, embedded builtins via `embed.FS`), but no enumeration function exists. A `WorkflowDescriptor` type (`internal/model/descriptor.go`) already carries `Path` and `DisplayName` for runs.

## Goals / Non-Goals

**Goals:**
- Add a "new" tab to the list TUI for discovering and launching workflows
- Support browsing workflow definitions without starting a run
- Collect workflow parameters via a TUI form before launch
- Make bare `agent-runner` invocation land on the new tab by default
- Add search/filter to the new tab

**Non-Goals:**
- Editing or scaffolding workflow YAML from the TUI
- Parameter types beyond strings (enums, booleans, file pickers)
- Persisting or recalling prior parameter values
- Changes to the CLI `run` command contract

## Approach

### New package: `internal/discovery/`

A new package handles workflow enumeration across all three scopes. It exposes a single function:

```go
func Enumerate(builtinFS fs.FS, projectDir, userDir string) ([]WorkflowEntry, error)
```

`WorkflowEntry` extends the existing `WorkflowDescriptor` concept:

```go
type WorkflowEntry struct {
    Name        string      // canonical name: "deploy" or "core:finalize-pr"
    Description string      // from parsed YAML, empty if absent
    Scope       Scope       // Project, User, or Builtin
    Path        string      // source path (embedded path or disk path)
    Params      []model.Param
    ParseError  error       // non-nil for malformed YAML files
}

type Scope int
const (
    ScopeProject Scope = iota
    ScopeUser
    ScopeBuiltin
)
```

Enumeration walks each scope's directory (or `fs.WalkDir` for the embed.FS), parses each `.yaml`/`.yml` file via the existing loader, and collects entries. Project-local names shadow user-global names (matching resolution precedence). Results are ordered: project first, then user, then builtin. Within each scope, alphabetical by name. Within builtins, sub-grouped by namespace (namespaces sorted alphabetically).

Malformed files produce an entry with `ParseError` set — the name is derived from the file path, but `Description`, `Params`, and other parsed fields are absent.

### List view: "new" tab

The list view model (`internal/listview/`) gains a fourth tab constant and associated state:

```go
const (
    tabNew tab = iota  // new tab is first
    tabCurrentDir
    tabWorktrees
    tabAll
)
```

Tab bar renders as: `● New  ○ Current Dir  ○ Worktrees  ○ All`. Keybinding `n` switches to the new tab.

**New tab state** stored in the model:

```go
type newTabState struct {
    workflows    []discovery.WorkflowEntry
    filtered     []int            // indices into workflows after filter
    cursor       int              // position in filtered list
    offset       int              // scroll offset
    searchText   string
    searchFocused bool            // true when search box has focus
}
```

**Workflow enumeration is eager** — called during `listview.New()` alongside run loading. The workflow list is ready when the tab is first displayed.

**Search box** sits above the list. Focus defaults to the first list item (not the search box). Up arrow from the first item moves focus to the search box. Down arrow or Enter from the search box moves focus back to the list. When the search box has focus, printable keystrokes go to filter text; when the list has focus, action keybindings (`r`, `Enter`, etc.) work normally.

Filter matches against canonical name or source path (substring match, case-insensitive). The first occurrence of the search substring in the displayed name is highlighted in `AccentCyan`. A count label `(N workflows)` is right-aligned next to the search box, showing the filtered count.

**Grouping** uses blank lines between scope groups and between builtin namespaces. No header rows — the namespace prefix in workflow names (e.g. `core:finalize-pr`) makes grouping self-evident, and blank-line separators avoid cursor-skip complexity. When a filter is active, groups with no matching workflows are collapsed entirely (blank line omitted).

**Row rendering:**
- Cursor row: `›` in `AccentCyan`, name in `SelectedText` bold, description in `BodyText`
- Non-cursor rows: name in `BodyText`, description in `DimText`
- Malformed rows: name in `FailedRed`, error message in `FailedRed` replacing description
- Descriptions truncated with `…` to fit terminal width

**Keybindings on the new tab** (when list has focus):
- `Enter`: navigate to workflow definition view
- `r`: start a run (param form if params, else launch directly)
- `Enter` and `r` ignored on malformed rows
- `↑`/`↓`: navigate list (↑ from first item moves to search box)
- `q`: quit

**Default tab on entry:**
- Bare `agent-runner`: new tab
- `--resume` (no arg): current-dir tab
- `--list`: current-dir tab
- Return from run view: whichever tab was active when the user entered the run view

### Workflow definition view

A new entry mode `FromDefinition` in the run view (`internal/runview/`):

```go
const (
    FromList Entered = iota
    FromInspect
    FromLiveRun
    FromDefinition  // new
)
```

`FromDefinition` initializes the run view with:
- Step tree populated from the workflow definition file, all steps `pending`
- No session directory, no audit log, no run state
- No live refresh, no auto-follow, no auto-scroll
- Breadcrumb shows workflow canonical name only (no run ID, time, or status)

Drill-in, step list rendering, detail pane (temporary pending blocks), keyboard focus, scrolling, and legend overlay all work unchanged — the existing code already handles the "all pending from workflow file" case.

The `r` keybinding is repurposed in this mode: instead of resume-run (which requires an inactive run), it means "start run." The help bar shows `r start run`.

Escape at top level returns to the list TUI (same as `FromList`).

### Param form

A new bubbletea model in `internal/paramform/` using `charmbracelet/bubbles/textinput` for input fields.

**Layout:**
- Workflow name in `AccentCyan` bold at top
- Description in `DimText` below
- One text input per parameter, vertically stacked
- Labels left-aligned in `BodyText`, right-padded to align inputs
- Required marker `*` in `FailedRed` after the label
- Input borders `│` in `DimText`, focused field borders in `AccentCyan`
- Input text in `BodyText`, defaults shown until edited
- `‹ Start ›` button below the fields in `AccentCyan`

**Navigation:** Tab/Shift+Tab between fields and the Start button. Arrow keys move text cursor within a field. Focus is visually indicated by the border color change.

**Submission:** Enter on the last field or on the Start button triggers submit. Validation checks all required fields have non-empty values. On failure, error messages appear below offending fields in `FailedRed`. On success, returns the param map.

**Cancellation:** Escape returns to the previous view (definition view or list tab). All input is discarded.

The param form is a modal — it replaces the list view entirely (no tab bar shown).

### Run launch via exec-self

When the user starts a run (from the new tab via `r`, from the definition view via `r`, or after the param form completes), the TUI exits and the current process execs itself:

```
syscall.Exec("agent-runner", ["agent-runner", "run", "<workflow>", "param1=value1", ...], env)
```

This reuses the existing CLI entry path exactly — `handleRun` sets up the execution context, session directory, audit logger, and launches the live-run view. No complex in-process model wiring needed.

**Post-run navigation:** When a run reaches a terminal state (completed or failed) and the user presses Escape at top level, instead of exiting the program, the process execs itself as `agent-runner --resume` (no arg) — which opens the list TUI on the current-dir tab. The just-completed run appears in the run list. This applies to both new runs started from the TUI and resumed runs.

This is a behavioral change for `FromLiveRun` mode: today Escape at top level after completion exits the program. With this change, it navigates to the list. The same change applies to runs resumed via `r` from the run list.

### New dependency

Add `github.com/charmbracelet/bubbles` to `go.mod` for the `textinput` component used by the param form. This is the standard companion library to bubbletea — widely used, maintained by the same team.

## Decisions

### Discovery as a separate package (not extending loader)
**Decision:** New `internal/discovery/` package.
**Rationale:** The loader focuses on loading a single workflow by name. Enumeration is a different operation (walk directories, parse each file, handle errors gracefully per file). Separation keeps both packages focused.
**Alternative considered:** Extending `internal/loader/` — rejected because it would mix single-resolution and batch-enumeration concerns.

### Exec-self for run launch (not in-process transition)
**Decision:** Use `syscall.Exec` to relaunch agent-runner with the run command.
**Rationale:** Reuses the existing CLI entry path exactly, avoiding complex in-process wiring between the list model and the run machinery. Post-run Escape execs back to the list TUI on the current-dir tab — a cleaner destination than "back to the new tab" since the user's run now lives in the current-dir context.
**Alternative considered:** In-process transition keeping the bubbletea program alive — rejected because it requires the list view to construct the full runner/session/audit infrastructure, tightly coupling list and run views. The exec-self pattern is already proven by the resume-run flow.
**Trade-off:** Brief screen flash on exec transitions. No "back to exactly where I was in the new tab" — but current-dir is the natural destination after a run.

### Post-run Escape goes to current-dir tab (not new tab)
**Decision:** After a run completes/fails, Escape execs to `agent-runner --resume` (no arg) → current-dir tab.
**Rationale:** After starting a run, the natural destination is where you can see that run and other runs for the project. The workflow list is for discovery; once you've started a run, you're past that phase.
**Applies to:** Both new runs started from the TUI and resumed runs. This is a behavioral change — today `FromLiveRun` post-completion Escape exits the program.

### Eager workflow enumeration (not lazy)
**Decision:** Enumerate workflows during `listview.New()`.
**Rationale:** Enumeration is cheap (a handful of YAML files). With bare `agent-runner` defaulting to the new tab, it's needed on most launches. Lazy loading adds complexity for negligible benefit.

### bubbles/textinput for param form (not custom)
**Decision:** Pull in `charmbracelet/bubbles` dependency.
**Rationale:** Text input has real edge cases (cursor movement, wide characters, paste). The bubbles library is the standard companion to bubbletea, maintained by the same team. Building custom inputs would be more code with more bugs.

### Blank-line grouping (not header rows)
**Decision:** Separate scope groups and builtin namespaces with blank lines, not non-selectable header rows.
**Rationale:** Header rows complicate cursor movement (skip logic, offset calculations). The namespace prefix in workflow names makes grouping self-evident. Blank lines provide visual separation without interaction complexity.

### Search focus model
**Decision:** Cursor defaults to the first list item. Up arrow from the first item moves to the search box. Down arrow from the search box returns to the list.
**Rationale:** Matches the Claude Code resume session pattern the user referenced. Action keybindings only fire when the list has focus, cleanly avoiding the conflict between typing `r` as search text vs. triggering "start run."

## Risks / Trade-offs

- **Exec-self screen flash** → Acceptable trade-off for architectural simplicity. The flash is brief (sub-second) and consistent with existing resume behavior.
- **No back-to-new-tab after run** → By design. Current-dir tab is the right destination post-run. If users want to start another run, `n` switches to the new tab.
- **Eager enumeration on every launch** → Could slow startup if a user has hundreds of workflow files. Unlikely in practice; can be optimized later with caching if needed.
- **New dependency (bubbles)** → Minimal risk. Same team as bubbletea, maintained in lockstep. Only the `textinput` component is used. This is a new direct dependency — add it to `go.mod`.

## Open Questions

None — all architectural decisions are resolved. The `--list` flag is a pre-existing entry point (identical to `--resume` with no arg) whose behavior is unchanged: it opens the list TUI on the current-dir tab. No design decision was needed.
