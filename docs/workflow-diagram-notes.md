# Workflow Diagram — Notes

## Skill Used

`base-config:d2-diagram` — generates D2 diagrams from descriptions.

## Source Files

- `docs/workflow-diagram.d2` — source
- `docs/workflow-diagram.svg` — rendered SVG
- `docs/workflow-diagram.png` — rendered PNG (3000px wide, via `rsvg-convert`)

## What the Diagram Shows

The full flokay change lifecycle decomposed into four workflow files:

1. **flokay.yaml** — top-level: create → proposal → specs → design → tasks → review → implement (sub-workflow) → verify → archive → archive-verify → finalize
2. **implement-change.yaml** — for-each loop over `tasks/*.md`, invokes implement-task per file
3. **implement-task.yaml** — agent implements the task, then invokes run-gauntlet
4. **run-gauntlet.yaml** — counted retry loop (max 3): shell gauntlet with `capture` + `break_if: success`, then agent fix with `session: inherit` + `skip_if: previous_success`

## Layout Approach

The final layout uses a **2-column grid** at the root level:

```d2
layout: "" {
  grid-rows: 1
  grid-columns: 2
  ...
  flokay: { direction: down }
  sub_workflows: { direction: down }
}
```

### Why the grid was necessary

D2's layout engines (both ELK and dagre) propagate the root `direction` into all containers, overriding container-level `direction` settings. This meant:

- `direction: right` at root → all steps inside flokay spread horizontally (wrong)
- `direction: down` at root → sub-workflows placed below flokay (wrong)
- Transparent wrapper containers with `direction: right` → ELK ignored them

The grid container forces the two columns to sit side by side regardless of root direction, letting each column's internal `direction: down` work correctly. Cross-container connection arrows between the grid cells don't render, so the relationship between flokay's implement step and the sub-workflows is communicated via the sub-workflows container label and dashed text in the step label.

### Legend placement

`near: bottom-center` on a root-level shape places it below everything without needing a connection.

## Key Flokay Changes Noted

`flokay.yaml` was updated during this session to use the new sub-workflow syntax:
- Added `idea_file` optional param
- `proposal` step now interpolates `{{file:idea_file}}`
- `implement` step replaced with `workflow: implement-change.yaml` + `params`
- `design` and `tasks` separated as headless steps
