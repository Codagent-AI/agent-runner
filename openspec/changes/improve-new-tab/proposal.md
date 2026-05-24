## Why

The new tab (the workflow picker shown when starting a new run) lists workflow groups in a fixed order driven by discovery internals — project, then user, then builtins sorted alphabetically by namespace (`core`, `onboarding`, `openspec`, `spec-driven`). Groups are separated only by a blank line, with no heading and no description, so new users can't tell what each group is *for* (e.g., what distinguishes `openspec` from `spec-driven`, or why `core` exists), and the order doesn't reflect the order in which users are likely to want to discover workflows.

We want the home screen to read top-to-bottom as a curated, self-explanatory menu: user-authored workflows on top (the user's own tools come first), then built-in groups in a sequence we control, each with a short description above it explaining what that group is for.

## What Changes

- Add a per-group header on the new tab, rendered above each group's workflow rows, containing a group label and a short description.
- Add a configurable ordering for built-in workflow groups so the order they appear in the new tab is decoupled from filesystem/alphabetical order.
- Project-scope and user-scope workflows SHALL render as two separate groups (each with its own header and description) above all built-in groups by default. The built-in group sequence is **spec-driven → openspec → onboarding → core**.
- Each built-in namespace (`workflows/<ns>/`) SHALL contribute its own display name and description via a per-namespace metadata file embedded alongside the workflow YAMLs. The project and user scope groups SHALL get their headers and descriptions from static copy defined in the listview code (they don't live under `workflows/<ns>/`).
- Workflows MAY declare themselves as sub-workflows by adding `hidden: true` to their YAML frontmatter. Hidden workflows SHALL be omitted from the new tab's default view but SHALL remain runnable via `agent-runner run <name>` and other non-TUI entry points.
- The new tab SHALL provide a keybinding that toggles the visibility of hidden workflows, allowing users to reveal and run them from the TUI when needed.
- The search/filter behavior, row rendering, selection model, and other keybindings on the new tab are unchanged.

## Capabilities

### New Capabilities

- `new-tab-layout`: Defines how the new-tab home screen presents discovered workflows — group ordering, group headers, per-group descriptions, and how project/user/builtin scopes map onto display groups. Covers the visual layout above each group, the rule that project and user groups render above all built-in groups, the built-in group sequence (spec-driven → openspec → onboarding → core), and the default-hide behavior plus reveal-toggle keybinding for `hidden` workflows.

### Modified Capabilities

- `builtin-workflows`: Adds the per-namespace metadata file (display name and description, consumed by `new-tab-layout`) as an embedded part of each `workflows/<ns>/` directory. Whether the same file also carries an ordering hint, or whether ordering is configured elsewhere, is deferred to the `new-tab-layout` spec. The existing requirements about YAML workflow files, sub-workflow resolution, and bundled assets are unchanged; this is purely additive.

The optional `hidden: true` workflow YAML field is introduced and specified by `new-tab-layout` (it is a display-layer hint with no effect on workflow loading, resolution, or execution).

## Technical Approach

The change is render-layer plus a small new embedded file format. No data-model upheaval.

```
┌─────────────────────────────────────────────────────────────┐
│                       new tab (TUI)                         │
│                                                             │
│   ┌─────────────────────────────────────────────────────┐   │
│   │ 🔍 Search...                            (12 workflows)  │  (count badge already exists today)
│   └─────────────────────────────────────────────────────┘   │
│                                                             │
│   ── Project workflows ─────────────────────────────────    │  ← new: group header + description
│   Workflows defined in this project's .agent-runner dir.    │
│                                                             │
│     deploy           Push the current branch                │
│                                                             │
│   ── User workflows ────────────────────────────────────    │  ← new
│   Workflows you've added to your home .agent-runner dir.    │
│                                                             │
│     run-tests        Run the full test suite                │
│                                                             │
│   ── Core ──────────────────────────────────────────────    │  ← new
│   General-purpose workflows shipped with agent-runner.      │
│                                                             │
│     core:finalize-pr      ...                               │
│     core:implement-task   ...                               │
│                                                             │
│   ── Onboarding ────────────────────────────────────────    │  ← new
│   Guided tours for new users — start here.                  │
│                                                             │
│     onboarding:onboarding ...                               │
│      ...                                                    │
└─────────────────────────────────────────────────────────────┘
```

(The diagram shows project and user as separate groups, consistent with the existing `(Scope, Namespace)` group identity. The search box and its `(N workflows)` count are reproduced as-is from today's UI — no change to that line is in scope.)

Key technical decisions:

1. **Group identity stays as `(Scope, Namespace)`.** The existing `groupKey{scope, ns}` in `internal/listview/newtab.go` already uniquely identifies groups; we just change how groups are *ordered* and *labeled*, not how they're partitioned. Per `internal/discovery/discovery.go:32`, `Namespace` is always empty for project- and user-scope entries regardless of subdirectory layout, so project and user always form exactly one group each (two groups total at the top), and built-ins form one group per namespace below.

2. **Built-in group metadata ships embedded.** Each `workflows/<ns>/` directory gains an optional metadata file (e.g., `group.yaml` — exact filename TBD in design) carrying at least `display_name` and `description`. This file is embedded via the existing `builtinworkflows` embed, read by `internal/discovery` during enumeration, and surfaced on `WorkflowEntry` (or a sibling per-group struct returned from discovery). Missing metadata falls back to a sensible default: namespace name as display, empty description. Whether the same file also carries an ordering hint is covered by Decision #5.

3. **Project/User group copy is hardcoded.** These scopes don't live under `workflows/<ns>/`, so they don't have a place to put a metadata file. Hardcoding one short header + description string for the project group and one for the user group in `internal/listview` is simpler than inventing a new file location.

4. **Render the header inside the existing scroll budget.** The new-tab body currently treats group separators as blank rows in the filtered list (`buildFilteredRows` emits `-1` entries). The header + description rows extend this pattern: a small struct identifying header rows is added to the filtered list alongside the workflow indices, and `renderNewTab` renders them as multi-line non-selectable rows. Scroll math (`maxRows`, `adjustOffset`) accounts for header height.

5. **Built-in group ordering is hardcoded in Go.** The sequence of built-in groups lives in `internal/listview` as a `var builtinGroupOrder []string` constant. Adding a new builtin namespace requires a code edit to position it in the sequence — which already happens for every namespace change since builtins are embedded at build time. When a namespace is not listed in the configured order (or two groups tie), the renderer falls back to today's behavior: alphabetical by namespace within each scope. The design phase considered and rejected per-namespace `order` fields and a top-level `workflows/groups.yaml`; see `design.md` for rationale.

Detailed ordering rules, the exact metadata-file schema, header styling, and the rendering behavior for a group whose rows are all filtered out by the search box are deferred to the spec and design phases.

## Out of Scope

- Run list ordering and headers on the *Active / Inactive / Worktrees* tabs — those are governed by `list-runs` and unchanged here.
- Collapsing/expanding groups on the new tab (e.g., click-to-collapse). Headers are decorative-only in this change.
- A user-configurable group order (e.g., `agent-runner config set group-order ...`). Ordering is controlled by the built-in metadata files and code defaults; runtime customization can come later.
- Showing per-workflow descriptions differently — row rendering stays as-is.
- Tab bar, search box, footer/help bar — no changes.
- Discovery semantics: which directories are scanned, shadowing rules, parse-error handling. Unchanged.

## Impact

- **Code:**
  - `internal/listview/newtab.go` — `buildFilteredRows`, `computeGroupIndices`, `renderNewTab`, `renderNewTabRow`, and scroll math gain awareness of header rows.
  - `internal/listview/model.go` and `newtab_test.go` — model adjustments and new render tests.
  - `internal/discovery/discovery.go` — read and surface per-namespace metadata; possibly a new `GroupMetadata` struct exposed alongside `WorkflowEntry`.
  - `internal/tuistyle/styles.go` — styles for the group header / divider rule and description line.
- **Embedded assets:** New metadata files added under `workflows/core/`, `workflows/onboarding/`, `workflows/openspec/`, `workflows/spec-driven/`.
- **Specs:** New `new-tab-layout` spec; delta to `builtin-workflows` for the metadata file.
- **No CLI surface changes.** Run/resume/inspect commands behave identically. The `Workflow` JSON serialization gains an optional `hidden` field (omitempty), which is the only externally visible structural change — no existing consumer is known to parse this output, and the field defaults to absent when unset.
- **No dependency changes** anticipated; metadata parsing reuses existing YAML loader.
