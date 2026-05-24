## Context

The new tab (workflow picker) on the agent-runner TUI today lists workflows in a single flat list, separated only by blank-line breaks between scopes/namespaces. There are no group headers, no descriptions, and the order is driven by filesystem enumeration. This change introduces per-group headers + descriptions, a curated built-in group order, and a `hidden: true` workflow YAML field so that sub-workflows (which only exist to be called from other workflows) don't clutter the picker.

The relevant code lives in three packages:

- `internal/model/step.go` — `Workflow` struct (YAML schema).
- `internal/discovery/discovery.go` — workflow enumeration across project / user / builtin scopes.
- `internal/listview/` — the TUI: `model.go` (state, key handling), `newtab.go` (filtering, rendering).

Built-in workflows are embedded via `workflows/` and exposed through `builtinworkflows.FS`.

## Goals / Non-Goals

**Goals:**
- Render each workflow group with a clear header + description on the new tab.
- Curate the built-in group sequence: spec-driven → openspec → onboarding → core.
- Let workflow authors mark sub-workflows as `hidden: true` to keep them out of the picker without removing them from the runtime.
- Provide a TUI keybinding (`h`) to reveal hidden workflows on demand.
- Keep CLI behavior (`agent-runner run <name>`) and sub-workflow resolution unchanged.

**Non-Goals:**
- Per-workflow ordering within a group (today's alphabetical order stays).
- User-configurable group order or labels.
- Collapse/expand interaction on groups.
- Surfacing `hidden` workflows in any other view (run list, worktree picker, etc.).
- A persisted "show hidden" preference — the toggle is session-scoped and resets every time the new tab is entered.

## Approach

### Component layout

```
                           ┌──────────────────────────┐
                           │  workflows/<ns>/         │
                           │   _group.yaml  (new)     │
                           │   *.yaml (workflows)     │
                           └────────────┬─────────────┘
                                        │ embedded
                                        ▼
   ┌─────────────────────────────────────────────────────────┐
   │ internal/discovery                                      │
   │   Enumerate() → []WorkflowEntry  (+ Hidden bool)        │
   │   EnumerateGroups() → []GroupMetadata  (new)            │
   │   loaders skip basenames starting with "_"              │
   └────────────────────────┬────────────────────────────────┘
                            │
                            ▼
   ┌─────────────────────────────────────────────────────────┐
   │ internal/listview                                       │
   │   newTabState { workflows, groups, filtered,            │
   │                 showHidden, cursor, … }                 │
   │   buildFilteredRows() → []filteredRow                   │
   │   renderNewTab() emits header/description/workflow rows │
   │   handleListKey: case "h" toggles showHidden            │
   └─────────────────────────────────────────────────────────┘
```

### Data model changes

**`internal/model/step.go` — Workflow struct (`step.go:601`)**

Add one field:

```go
type Workflow struct {
    Name        string        `yaml:"name" json:"name"`
    Description string        `yaml:"description,omitempty" json:"description,omitempty"`
    Hidden      bool          `yaml:"hidden,omitempty" json:"hidden,omitempty"`  // NEW
    Params      []Param       `yaml:"params,omitempty" json:"params,omitempty"`
    Sessions    []SessionDecl `yaml:"sessions,omitempty" json:"sessions,omitempty"`
    Steps       []Step        `yaml:"steps" json:"steps"`
    Engine      *EngineConfig `yaml:"engine,omitempty" json:"engine,omitempty"`
}
```

`Hidden` is purely declarative. No code in `runner`, `exec`, or `loader/composition.go` consults it; sub-workflow resolution and execution proceed exactly as today.

**`internal/discovery/discovery.go` — WorkflowEntry**

```go
type WorkflowEntry struct {
    CanonicalName string
    Description   string
    Hidden        bool          // NEW — mirror of Workflow.Hidden
    Params        []model.Param
    SourcePath    string
    Namespace     string
    Scope         Scope
    ParseError    string
}
```

Both `loadLocalEntries` and `loadBuiltinEntries` already load the workflow to extract `Description` (`discovery.go:186-188, 234-237`); they get one extra line to copy `workflow.Hidden`.

**`internal/discovery/discovery.go` — GroupMetadata (new)**

```go
type GroupMetadata struct {
    Namespace   string  // "" for ScopeProject and ScopeUser
    Scope       Scope
    DisplayName string
    Description string
}
```

New API:

```go
// EnumerateGroups returns metadata for every (Scope, Namespace) group present
// in entries. Built-in group metadata is loaded from `workflows/<ns>/_group.yaml`
// in builtinFS; project and user groups receive hardcoded display names and
// descriptions from this package.
func EnumerateGroups(builtinFS fs.FS, entries []WorkflowEntry) []GroupMetadata
```

`entries` is the sole source of truth for which groups exist — `EnumerateGroups` does not enumerate filesystems directly. It walks `entries` to discover the set of `(Scope, Namespace)` keys, then for each built-in key reads `workflows/<ns>/_group.yaml` from `builtinFS` and falls back to defaults (`DisplayName = ns`, `Description = ""`) if absent or malformed. Project/user groups never read from disk; their copy is hardcoded in this package. Malformed `_group.yaml` parses log a debug message but do not surface a fatal error.

### `_group.yaml` schema

```yaml
display_name: Spec-Driven
description: End-to-end change flows using lightweight specs.
```

Both fields are optional. Unknown fields are ignored (parser tolerance — we do not want a future field addition to break older builds, though in practice everything ships together).

### Discovery enumeration changes

In both `enumerateLocalDir` and `enumerateBuiltinFS`, **skip files whose basename starts with `_`** during the workflow-YAML walk. This excludes `_group.yaml` from being misinterpreted as a workflow called `core:_group`. Implementation: one extra check inside the existing `WalkDir` callbacks:

```go
if strings.HasPrefix(d.Name(), "_") {
    return nil
}
```

The underscore-prefix convention becomes a reserved namespace for builtin-workflows metadata.

### Listview state

```go
type newTabState struct {
    workflows  []discovery.WorkflowEntry
    groups     []discovery.GroupMetadata  // NEW, ordered for rendering
    filtered   []filteredRow              // CHANGED from []int
    showHidden bool                       // NEW
    cursor     int
    offset     int
    searchText string
    searchFocused bool
}

type rowKind int
const (
    workflowRow rowKind = iota
    headerRow
    descriptionRow
    separatorRow
)

type filteredRow struct {
    kind  rowKind
    index int  // workflow index for workflowRow; group index for header/description; unused for separator
}
```

The change from `[]int` to `[]filteredRow` is the largest internal refactor. It touches:

- `buildFilteredRows` (signature + algorithm)
- `firstSelectableRow` (skip non-workflowRow kinds)
- `moveNewTabCursor` (skip non-workflowRow kinds when navigating)
- `newTabCurrentEntry` (dereference via `kind == workflowRow`)
- `computeGroupIndices` (replaced by storing group index directly on header/description rows)
- `renderNewTab` (switch on kind to render workflow vs header vs description vs separator)
- All `newtab_test.go` test cases that assert on filtered contents

### Group ordering

In `internal/listview/newtab.go`:

```go
// builtinGroupOrder dictates the top-to-bottom rendering order for built-in
// workflow groups on the new tab. Namespaces not listed here render after
// listed ones, alphabetically.
var builtinGroupOrder = []string{
    "spec-driven",
    "openspec",
    "onboarding",
    "core",
}
```

The ordering function:

1. Project group (if non-empty) first.
2. User group (if non-empty) second.
3. Built-in groups: iterate `builtinGroupOrder` first; for each namespace in the list, emit its group if non-empty. Then append remaining non-empty built-in groups sorted alphabetically.
4. Drop any group with zero visible workflows after filtering.

### Filtering pipeline

`buildFilteredRows` rewritten as:

```go
func buildFilteredRows(
    workflows []discovery.WorkflowEntry,
    groups    []discovery.GroupMetadata,
    filter    string,
    showHidden bool,
) []filteredRow {
    // 1. Drop entries where Hidden && !showHidden.
    // 2. Apply search filter (matchesFilter, unchanged).
    // 3. Partition remaining workflows by (Scope, Namespace).
    // 4. Walk groups in the configured order; for each group with ≥1 surviving workflow,
    //    emit: headerRow, descriptionRow, workflowRow…, separatorRow (unless last).
}
```

### Key handling

Add a single case to `handleListKey` in `internal/listview/model.go`:

```go
case "h":
    if m.activeTab == tabNew {
        m.newTab.showHidden = !m.newTab.showHidden
        m.newTab.filtered = buildFilteredRows(
            m.newTab.workflows, m.newTab.groups,
            m.newTab.searchText, m.newTab.showHidden,
        )
        m.newTab.cursor = firstSelectableRow(m.newTab.filtered)
    }
```

When `searchFocused` is true, key events are routed to `handleSearchKey`, which already captures all printable runes as search input (`newtab.go:41-44`). So pressing `h` inside the search box appends "h" to the search text rather than toggling — no extra code needed.

### Toggle reset on new-tab entry

Every transition into `tabNew` resets `showHidden = false`. The places to handle:

- `case "n":` in `handleListKey` (`model.go:434`)
- `nextTab()` and `prevTab()` when the result is `tabNew` (`model.go:568, 584`)
- Initial state in `New()` (already zero-valued — no change needed)
- Settings editor close path doesn't touch `activeTab` — no change needed

A small helper `m.enterNewTab()` centralises the reset + filter rebuild and is called from each of the above transitions.

### Help bar

`internal/listview/view.go` renders the footer/help bar. When `activeTab == tabNew`, append `h hidden` to the existing shortcut list. (No spec scenarios fail this — it's a UX polish that matches the requirement "the help bar SHALL include `h hidden`".)

### Migration: built-in YAML edits

Per-namespace `_group.yaml` files (4 new files):

```yaml
# workflows/spec-driven/_group.yaml
display_name: Spec-Driven
description: End-to-end change flows using lightweight specs.

# workflows/openspec/_group.yaml
display_name: OpenSpec
description: Change planning and implementation using OpenSpec.

# workflows/onboarding/_group.yaml
display_name: Onboarding
description: Guided tours and demos for new users.

# workflows/core/_group.yaml
display_name: Core
description: General-purpose sub-workflows invoked by other workflows and skills.
```

(Exact copy can be refined; the schema is what matters.)

Workflows that gain `hidden: true`:

- `workflows/core/finalize-pr.yaml`
- `workflows/core/implement-task.yaml`
- `workflows/core/run-validator.yaml`
- `workflows/core/review-proposal.yaml`
- `workflows/spec-driven/plan-change.yaml`
- `workflows/spec-driven/implement-change.yaml`
- `workflows/openspec/plan-change.yaml`
- `workflows/openspec/implement-change.yaml`

`workflows/openspec/simple-change.yaml`, `workflows/openspec/change.yaml`, `workflows/spec-driven/change.yaml`, `workflows/spec-driven/simple-change.yaml`, and all `workflows/onboarding/*.yaml` stay user-facing.

### Tests

- `internal/loader/loader_test.go` — confirm `hidden: true` round-trips through `LoadWorkflow`.
- `internal/discovery/discovery_test.go` — new test cases:
  - `_group.yaml` present → group metadata exposed.
  - `_group.yaml` absent → defaults.
  - `_group.yaml` malformed → defaults, no fatal error.
  - File beginning with `_` is not enumerated as a workflow.
  - `hidden: true` workflow is enumerated with `Hidden = true`.
- `internal/listview/newtab_test.go` — refactor existing tests to the new `filteredRow` model; add cases for:
  - Group order (project, user, then `builtinGroupOrder`, then alphabetical fallback).
  - Hidden workflows omitted when `showHidden == false`.
  - Hidden workflows visible when `showHidden == true`.
  - Empty groups omitted entirely.
  - Header/description rows are non-selectable (cursor skips them).
  - Search filter that empties a group drops the group entirely.
- `internal/listview/model_test.go` — pressing `h` toggles; switching into `tabNew` resets `showHidden` to false.

## Decisions

1. **Hardcode the built-in group order in Go.** Built-in namespaces only change through PRs to the agent-runner repo, which always involves code review. Hardcoding the sequence puts the ordering decision next to the code that consumes it and removes the friction of choosing/maintaining integer order values. Alternatives considered: per-namespace `order` field (introduces tie-resolution and renumbering friction) and top-level `workflows/groups.yaml` (spooky action at a distance for a small fixed list).

2. **Reserve underscore-prefixed filenames as namespace metadata.** Using `_group.yaml` and excluding underscore-prefixed basenames during workflow enumeration is forward-compatible (we can add `_examples.yaml` or `_help.yaml` later without filename collisions) and follows a widely understood convention. The alternative — special-casing the literal name `group.yaml` — would have to be updated every time we add a new metadata file type.

3. **Tagged-struct filtered rows.** The current `[]int` with `-1` sentinels is already at its readability limit with one sentinel. Adding two more (header, description) would make the code opaque. `filteredRow{kind, index}` is barely more code and clarifies every site that touches the list.

4. **`hidden` is display-layer only.** No code outside discovery + listview consults it. This preserves CLI semantics, sub-workflow resolution, and run/resume behavior verbatim. The field is purely a UX hint about the picker.

5. **Toggle resets on every new-tab entry.** "Fresh" matches the user's mental model that the picker starts in its canonical state each time they open it. The session-scoped persistence (alternative) would let stale toggle state hide workflows the user expected to see.

## Risks / Trade-offs

- **[Risk] Existing tests in `internal/listview/newtab_test.go` need significant rewrites** for the `[]int` → `[]filteredRow` migration → Mitigation: do this refactor as the first commit, before any feature-bearing changes, so the diff is clean and bisectable.
- **[Risk] Reserving `_` prefix is a new convention not documented elsewhere** → Mitigation: the `builtin-workflows` spec delta in this change documents it explicitly. Add a one-paragraph note in `CLAUDE.md` or `docs/` covering the convention.
- **[Risk] Help-bar text drift** — the help bar is currently rendered in `view.go` and other places sometimes contain similar strings → Mitigation: grep for existing help-bar locations and update consistently; covered by a model-level test.
- **[Trade-off] Hardcoded group order means adding a new built-in namespace requires a code edit** in `builtinGroupOrder`. Acceptable because new built-in namespaces are rare and already require Go-side changes (workflows are embedded at build time). New unknown namespaces still render — just alphabetically after the listed ones.
- **[Trade-off] No persisted "show hidden" preference**. Users who frequently run sub-workflows from the TUI must press `h` each session. Acceptable for v1 — hidden workflows are by definition rarely-needed direct entry points.
- **[Trade-off] `showHidden` resets on every transition into `tabNew`, including tab-cycling.** A user who is on the new tab with hidden visible, presses `tab` to peek at another tab, and cycles back will lose the toggle state — even though search text and cursor position persist across tab switches. This was an explicit product choice: "fresh = each visit" matches the picker's intent as a curated menu. If this surprises users in practice, narrow the reset to only the `n` keypress and initial entry.

## Migration Plan

The deliverable is a single PR (one task in `tasks.md`). The numbered steps below are a **recommended internal commit sequence** within that PR — bisectable and easy to review one chunk at a time — not separate tasks.

1. **Refactor commit (no behavior change):** introduce `filteredRow` and rewrite `buildFilteredRows`, `firstSelectableRow`, `moveNewTabCursor`, `newTabCurrentEntry`, `renderNewTab` against the new model. Adjust `newtab_test.go` to assert against `filteredRow` values. CI green confirms the structural change is benign.
2. **Add `Hidden` to `model.Workflow` and `WorkflowEntry`,** plumbed through `loader` and `discovery`. Default behavior unchanged because no built-in YAML carries `hidden: true` yet.
3. **Add `GroupMetadata` + `_group.yaml` loader** to discovery. Skip `_`-prefixed basenames during workflow enumeration. Defaults preserve current "no header" rendering until step 4.
4. **Render group headers + descriptions** in `renderNewTab`, ordered by `builtinGroupOrder` with project/user pinned on top.
5. **Add `h` toggle** in `handleListKey` and reset on `tabNew` entry. Help bar text updates.
6. **Author `_group.yaml` files** for all four built-in namespaces.
7. **Add `hidden: true`** to the eight sub-workflows listed above.
8. **Update the `builtin-workflows` spec doc** with the underscore-prefix convention (already covered in this change's spec delta).

**Rollback strategy:** Steps 1-5 are code-only and can be reverted by reverting commits. Steps 6-7 (YAML edits) are independently revertable; reverting them returns the picker to showing the previously hidden workflows but does not break anything.

## Open Questions

- Exact copy for `_group.yaml` `description` fields and for the hardcoded project/user descriptions — leave to authoring during implementation; UX-tunable later without behavior impact.
- Header visual styling (rule character, color) — the existing `tuistyle` palette has obvious choices (a separator rule using `─`, group color from `GroupColors`, dim description). Pinning this in design.md is over-specification; the implementer can pick within the palette.
- Whether `_group.yaml` should support a workflow ordering hint within a group (e.g., feature workflows first, helpers later). Not in scope for this change but worth noting as a potential follow-up if the picker grows further.
