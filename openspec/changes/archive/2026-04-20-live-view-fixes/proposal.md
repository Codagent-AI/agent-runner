## Why

The recently-shipped `improve-live-view` change introduced several regressions and UX rough edges in the run view. Nested-step expansion in the sidebar is broken (steps are negatively indented and only the last child is shown), the duration label runs its units together with no space, blocks in the log butt directly against each other with no visual breathing room, agent blocks render two blank lines above the duration instead of one, loop rows are missing a type glyph, and long step names overflow the sidebar.

## What Changes

- **Sidebar inline expansion**: when the selected step is a container, the expansion SHALL show its **direct children only** (not recurse to the deepest active descendant). For a loop, the expansion lists each iteration row with its status; for a sub-workflow, the expansion lists each direct child step. Expansion rows SHALL never display iteration parameters or binding values.
- **Sidebar indentation fix**: inline-expanded children render at the correct positive indent under the selected parent (current behavior renders them at an outdented / negative offset).
- **Loop type glyph**: loop step rows get a type glyph (currently missing, which makes loops visually indistinguishable from untyped rows). The iteration counter `(N/M)` continues to render.
- **Iteration rows never show params**: regardless of whether an iteration appears as a top-level step, inline under a selected parent, or as a drilled-in row, its sidebar row SHALL NOT display parameter or binding-value text.
- **Sidebar step-name truncation**: step names longer than 20 visual characters SHALL be truncated to the first 17 characters + `…`. Log block separators SHALL continue to render the full step name.
- **Duration formatting**: insert a space between each duration unit (e.g., `121m 48s`, `1h 2m 3s`) wherever the duration string is rendered.
- **Inter-block spacing in the log**: a blank line SHALL separate adjacent top-level blocks in the log (equivalent to: a blank line immediately precedes every block separator except the first in view).
- **Trailing prompt blank stripped**: agent blocks SHALL render exactly one blank line above the duration/exit line; the second blank line currently caused by a trailing newline in the rendered prompt text SHALL be eliminated.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `view-run`: loop step rows gain a type glyph; sidebar inline expansion under the selected step now shows direct children only (not recursive); iteration rows never display params/binding values; long step names in the sidebar truncate at 20 chars. Duration formatting, inter-block spacing, and prompt-trailing-blank cleanup are rendering fixes and are not re-specified.

## Out of Scope

- Changes to the log pane's recursive nesting (sub-workflow and loop blocks in the log still contain all their started descendants inline at arbitrary depth — only the sidebar expansion changes).
- Changes to drill-in navigation, breadcrumbs, scroll sync, auto-follow, or auto-scroll.
- Changes to the underlying audit log, workflow execution, or step model.
- New status glyphs or status semantics.

## Impact

- **Affected packages**: `internal/runview` (`view.go` for sidebar expansion and truncation; `detail.go` for duration formatting, inter-block spacing, and trailing-blank cleanup; glyph constants wherever type glyphs are defined).
- **Affected specs**: `view-run` only.
- **Unaffected**: audit log format, engine interface, workflow execution, CLI surface.
- **Risk areas**: golden-file tests in `internal/runview` will need updates for every rendering change.
