# Task: Improve the new tab

## Goal

Implement the full `improve-new-tab` change: render per-group headers and descriptions on the new tab, curate the built-in group order, and support a `hidden: true` workflow YAML field with a `h` keybinding to toggle visibility of hidden workflows. All in one delivery.

## Background

You MUST read these files before starting:

- `openspec/changes/improve-new-tab/proposal.md` — motivation and scope (the "why").
- `openspec/changes/improve-new-tab/design.md` — full architecture, decisions, and migration plan (the "how"). This is the authoritative source for every implementation choice; read it end-to-end.
- `openspec/changes/improve-new-tab/specs/new-tab-layout/spec.md` — acceptance criteria for the new tab behavior. Every `#### Scenario` is a test case.
- `openspec/changes/improve-new-tab/specs/builtin-workflows/spec.md` — acceptance criteria for the per-namespace metadata file and the underscore-prefix reservation.

These files specify the work end-to-end. Do not infer behavior; read them.

### Code you will be touching

Read these files to understand current structure before editing:

- `internal/model/step.go` — `Workflow` struct at line 601 gains a `Hidden bool` field tagged `yaml:"hidden,omitempty" json:"hidden,omitempty"`.
- `internal/discovery/discovery.go` — adds `Hidden` to `WorkflowEntry`; adds `GroupMetadata` type and the `_group.yaml` loader; both `enumerateLocalDir` and `enumerateBuiltinFS` skip basenames starting with `_` during workflow enumeration.
- `internal/discovery/discovery_test.go` — new tests for metadata file present/absent/malformed, `_`-prefixed file exclusion, and `Hidden` round-trip.
- `internal/listview/newtab.go` — replaces `filtered []int` with `filtered []filteredRow` (tagged struct: `{kind rowKind, index int}` where `rowKind ∈ {workflowRow, headerRow, descriptionRow, separatorRow}`). Rewrites `buildFilteredRows`, `firstSelectableRow`, `moveNewTabCursor`, `newTabCurrentEntry`, `computeGroupIndices`, and `renderNewTab` against the new model. Adds `var builtinGroupOrder = []string{"spec-driven", "openspec", "onboarding", "core"}`.
- `internal/listview/model.go` — `newTabState` gains `groups []discovery.GroupMetadata` and `showHidden bool`. `handleListKey` adds `case "h"`. A helper `enterNewTab()` resets `showHidden` and rebuilds `filtered`; called from every transition into `tabNew` (the `n` shortcut and from `nextTab`/`prevTab` when the result is `tabNew`).
- `internal/listview/view.go` — `helpParts()` at line 465 appends `h hidden` when `activeTab == tabNew`.
- `internal/listview/newtab_test.go` — refactor existing assertions for `filteredRow`; add coverage for ordering, headers/descriptions, empty-group omission, hidden-toggle behavior, and header rows being non-selectable.
- `internal/listview/model_test.go` — test that `h` toggles `showHidden`, that entering `tabNew` resets it, and that `h` while the search box has focus is captured as input (existing routing in `handleSearchKey` at `newtab.go:41-44` already handles this — confirm it's untouched).
- `internal/loader/loader_test.go` — confirm `hidden: true` round-trips through `LoadWorkflow`.
- `workflows/spec-driven/_group.yaml`, `workflows/openspec/_group.yaml`, `workflows/onboarding/_group.yaml`, `workflows/core/_group.yaml` — new files. Schema `display_name: string` + `description: string`. See `design.md` migration plan for suggested copy.
- `workflows/core/finalize-pr.yaml`, `workflows/core/implement-task.yaml`, `workflows/core/run-validator.yaml`, `workflows/core/review-proposal.yaml`, `workflows/spec-driven/plan-change.yaml`, `workflows/spec-driven/implement-change.yaml`, `workflows/openspec/plan-change.yaml`, `workflows/openspec/implement-change.yaml` — add `hidden: true` to YAML frontmatter. `workflows/onboarding/*.yaml` stays as-is. `workflows/spec-driven/change.yaml`, `workflows/spec-driven/simple-change.yaml`, `workflows/openspec/change.yaml`, `workflows/openspec/simple-change.yaml` stay user-facing.

### Constraints

- **TDD**: per the project's CLAUDE.md, write failing tests for substantive behavior changes before production code. Configuration-only edits (the YAML files in `workflows/`) and styling tweaks don't require tests.
- **Behavior preservation**: the `[]int` → `[]filteredRow` refactor must keep every current behavior. Doing it as the first commit (with no other changes) is recommended so the diff is bisectable, but the task is delivered as one final commit/PR per project convention.
- **Hidden is display-only**: nothing in `runner`, `exec`, or `loader/composition.go` consults `Workflow.Hidden`. CLI invocation (`agent-runner run <name>`), name resolution, and sub-workflow references all behave identically whether `hidden` is set or not.
- **Underscore convention**: discovery skips basenames whose first character is `_` during workflow enumeration in both `enumerateLocalDir` and `enumerateBuiltinFS`. The metadata loader has its own dedicated path that reads `_group.yaml` explicitly.
- **Group ordering**: project group first, user group second, then `builtinGroupOrder` in declared sequence, then any unlisted built-in namespaces alphabetically. Drop any group with zero visible workflows after hidden-filtering and search-filtering.
- **Toggle reset**: `showHidden` resets to `false` on every transition into `tabNew` (the `n` keypress and tab cycling that lands on `tabNew`). Initial state is also `false`.
- **Search interaction**: when `searchFocused` is true, rune input is appended to search text by the existing handler at `internal/listview/newtab.go:41-44`. Do not add a competing `h` handler in the search path.
- **Commit convention**: follow the project's `type: lowercase description` rule from CLAUDE.md (e.g., `feat: render group headers on new tab` or `feat: improve new tab grouping and visibility`).

## Done When

- Every `#### Scenario` in `specs/new-tab-layout/spec.md` and `specs/builtin-workflows/spec.md` is covered by a passing test.
- `make test` passes; `make lint` is clean; `make fmt` shows no diff.
- The new tab in the running TUI shows the group sequence: Project → User → spec-driven → openspec → onboarding → core (with empty groups omitted).
- Each visible group renders with a header and description above it.
- These sub-workflow YAMLs carry `hidden: true` and are absent from the new tab by default, then visible after pressing `h`:
  - `workflows/core/finalize-pr.yaml`
  - `workflows/core/implement-task.yaml`
  - `workflows/core/run-validator.yaml`
  - `workflows/core/review-proposal.yaml`
  - `workflows/spec-driven/plan-change.yaml`
  - `workflows/spec-driven/implement-change.yaml`
  - `workflows/openspec/plan-change.yaml`
  - `workflows/openspec/implement-change.yaml`
  - `workflows/onboarding/advanced.yaml`
  - `workflows/onboarding/guided-workflow.yaml`
  - `workflows/onboarding/step-types-demo.yaml`
  - `workflows/onboarding/validator.yaml`
- All four `_group.yaml` files (`spec-driven`, `openspec`, `onboarding`, `core`) exist with both `display_name` and `description` set, and ship in the embedded built-in set.
- `agent-runner run core:finalize-pr` (and every other workflow marked `hidden: true`) still executes normally.
- `agent-runner run core:_group` returns a workflow-not-found error.
- `workflows/onboarding/onboarding.yaml`, `workflows/onboarding/help.yaml`, plus `workflows/spec-driven/change.yaml`, `workflows/spec-driven/simple-change.yaml`, `workflows/openspec/change.yaml`, and `workflows/openspec/simple-change.yaml`, remain user-facing on the new tab.
