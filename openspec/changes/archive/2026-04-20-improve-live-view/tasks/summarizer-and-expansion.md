# Task: summarizer profile + left-pane step expansion

## Goal

Two independent additions that do not touch the core log-pane refactor:

1. Add `summarizer` as a built-in default agent profile so workflow authors can reference `agent: summarizer` on a fresh repo without editing config.
2. Add an inline read-only expansion under the selected step in the left pane that recursively shows the active-descendant chain down to the currently running (or last-completed) step.

## Background

### summarizer profile

`internal/config/config.go::defaultConfig()` builds the map of profiles written to `.agent-runner/config.yaml` when that file does not yet exist. It currently has four profiles: `interactive_base`, `headless_base`, `planner`, `implementor`. Add a fifth:

```go
"summarizer": {
    DefaultMode: "headless",
    CLI:         "claude",
    Model:       "haiku",
    Effort:      "low",
},
```

This is a standalone base profile (no `extends`). It intentionally does not inherit from `headless_base` — `summarizer` uses haiku at low effort to stay cheap, and that independence should be explicit. If it extended `headless_base` it would silently inherit any future model change on that profile.

The profile is always written to auto-generated config files; users who already have a config are unaffected. The existing `validate()` function will accept it because it satisfies all base-profile constraints (`default_mode` and `cli` are both set, `effort` is `"low"` which is in `validEffort`).

**Key file:** `internal/config/config.go` — edit `defaultConfig()` only. No other file needs changing for this part.

### left-pane step expansion

The left pane currently shows one row per sibling at the current drill-in level, built by `buildStepRows()` in `internal/runview/view.go`. After this task, the selected step's row is followed immediately by read-only expansion rows that recursively display its active-descendant chain.

**Active-descendant chain:** starting from the selected node, walk to the child that is `StatusInProgress` (or, if nothing is in-progress, the last non-pending child). Repeat from that child. Stop at a leaf or a node with no non-pending children. Emit one indented row per level.

**Display rules:**
- Each expansion row shows the descendant's status glyph, type glyph, and name — same fields as a regular step row, but indented 2 spaces per depth level and not prefixed with the cursor arrow.
- Expansion rows are visually dimmed (use `tuistyle.DimStyle`) to distinguish them from real selectable rows.
- Arrow-key navigation (`moveCursor`) must skip expansion rows entirely — it moves only among the current drill-in level's direct children. The expansion is display-only; to navigate into a nested step the user presses `Enter` to drill in.
- Non-selected steps show no expansion (collapsed to a single row as today).
- When the selected step is a leaf or all its descendants are pending, no expansion rows appear.

**`buildExpansionRows(selected *StepNode) []string`** is a new function in `view.go`. It is called from `buildStepRows()` right after the selected row is appended:

```go
rows[i] = m.renderStepRow(n, true)
rows = append(rows[:i+1], append(m.buildExpansionRows(n), rows[i+1:]...)...)
```

Wait — `rows` is a slice pre-allocated to `len(children)`. The simplest approach is to build the list separately: build rows normally, then insert expansion rows after the selected index. Or rebuild `rows` as a `[]string` appended incrementally.

Expansion rows do not affect `m.cursor` — the cursor still indexes into `currentChildren()`, not into the visual row list. The `moveCursor` function operates on `currentChildren()` and is unchanged.

**Choosing the "active" descendant at each level:** prefer the child with `StatusInProgress`. If none, prefer the last child that is not `StatusPending`. If all children are pending, stop (no expansion).

**Key file:** `internal/runview/view.go` — add `buildExpansionRows`, modify `buildStepRows` to call it.

**Reference types (from `internal/runview/tree.go`):**
- `StepNode.Status` — `StatusInProgress`, `StatusSuccess`, `StatusFailed`, `StatusSkipped`, `StatusPending`
- `StepNode.Children []*StepNode`
- `StepNode.Type` — for the type glyph
- `StepNode.IsContainer()` — returns true for loop, sub-workflow, iteration nodes

## Spec

### Requirement: Config file auto-generation (MODIFIED)
When `.agent-runner/config.yaml` does not exist, the runner SHALL generate it with five default profiles:
- `interactive_base`: default_mode=interactive, cli=claude, model=opus, effort=high
- `headless_base`: default_mode=headless, cli=claude, model=opus, effort=high
- `planner`: extends interactive_base (no overrides)
- `implementor`: extends headless_base (no overrides)
- `summarizer`: default_mode=headless, cli=claude, model=haiku, effort=low

#### Scenario: Config file missing on startup
- **WHEN** the runner starts and `.agent-runner/config.yaml` does not exist
- **THEN** the runner creates the file with the five default profiles and proceeds normally

#### Scenario: Config file already exists
- **WHEN** the runner starts and `.agent-runner/config.yaml` exists
- **THEN** the runner loads and uses it as-is without modifying it

#### Scenario: Summarizer profile resolves to claude + haiku
- **WHEN** a workflow step references `agent: summarizer` and the generated config is unchanged
- **THEN** the resolved profile has default_mode=headless, cli=claude, model=haiku, effort=low

### Requirement: Step list recursive expansion under selected step
The step list SHALL show an inline read-only expansion only under the currently selected step. Expansion SHALL recurse through every nesting level (sub-workflow → loop → iteration → sub-workflow → …) down to the deepest active descendant (the currently running descendant, or the last completed descendant if nothing is running). Non-selected ancestors and siblings SHALL render as a single collapsed row each. When selection changes, the previous expansion SHALL collapse and the new selection's expansion SHALL be rendered. Arrow-key navigation SHALL move selection only among the current drill-in level's steps and SHALL skip expansion rows (expansion is read-only).

#### Scenario: Expansion recurses to deepest active descendant
- **WHEN** the selected step is a sub-workflow whose active descendant is several nesting levels deep (e.g., sub-workflow → loop iteration → sub-workflow → agent step)
- **THEN** the step list renders an indented row for each intermediate level down to the deepest active descendant

#### Scenario: Non-selected container collapses
- **WHEN** the user moves selection off an expanded container onto another step
- **THEN** the previously expanded container collapses to a single row, and the newly selected step expands recursively under itself (if it has active descendants)

#### Scenario: Selected step with no active descendant
- **WHEN** the selected step is a leaf step, or a container whose descendants are all pending
- **THEN** no expansion is rendered; the selected step occupies a single row like any other

#### Scenario: Expansion rows are read-only
- **WHEN** the user presses an arrow key while the cursor is on a container with an inline expansion
- **THEN** the cursor moves to the next or previous direct child of the current drill-in level, skipping the expansion rows; to interact with nested content the user drills in with Enter

## Done When

- `config_test.go` covers all three summarizer scenarios: generated config has five profiles, existing config is untouched, resolved summarizer profile has the correct fields.
- `buildExpansionRows` is called from `buildStepRows` and produces correctly indented rows for a multi-level active-descendant chain.
- `moveCursor` still moves only among direct children (not expansion rows) — verified by a unit test that sets up a selected container with in-progress children and asserts cursor positions after up/down presses.
- All existing tests pass.
