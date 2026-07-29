## Why

The run view makes it unnecessarily difficult to understand both where execution is happening and what the current step is doing.

The left pane shows only the current workflow level with a shallow, read-only expansion under the selected row. During nested workflows, the active leaf can therefore be hidden behind collapsed ancestors. Step names are also truncated to a fixed 20-character budget before the pane is measured, so indentation and long workflow names make the execution path hard to scan even though the pane width itself is already content-aware.

The right pane has the opposite problem: it renders one continuous log containing every started step and recursively nested child block. Finding the selected or active step requires scrolling through prior steps and repeated metadata, so the most relevant input and response are easily lost.

The run view should instead foreground the active execution path and make the detail pane about one step at a time. This will make live workflows easier to follow and completed runs faster to inspect without removing access to full step responses or the persisted evidence used for deeper debugging.

## What Changes

### Shared Run View

- Replace selected-row, read-only expansion with a flattened tree of visible workflow rows.
- Make every visible tree row selectable, including nested workflow steps, loop iterations, and agent calls. Arrow-key navigation traverses the flattened visible tree.
- Keep manual drill-in as an optional zoom that scopes the tree, breadcrumb, sub-workflow header, resume behavior, and run summary. Manual drill-in is never required to inspect a nested row.
- Expand the ancestry of the selected leaf while keeping unrelated containers collapsed.
- Replace the fixed 20-character step-name budget with pane-aware truncation:
  - indentation and the full visible row participate in width measurement;
  - names are truncated only when the measured sidebar reaches its cap;
  - preserving the minimum usable detail width takes priority when the terminal cannot fit both panes at their preferred widths;
  - there is no proportional sidebar ceiling; on extremely narrow terminals, names truncate first, fixed tree chrome is preserved while it fits, and the detail pane may fall below its normal 20-column minimum only when the terminal cannot physically fit that minimum plus the tree chrome and whitespace gap.
- Keep the sidebar width stable while the user is reading: it may grow when a newly visible row requires more space, but it does not shrink until the terminal is resized or the run view is re-entered.
- Replace the continuous cross-step log in the right pane with a detail view for the selected step.
- Structure the selected-step detail view around:
  - a short, bounded recap of the most recent terminal leaf execution that precedes the selected execution in audit start order;
  - the selected step's effective prompt, command, or script, truncated to at most three rendered lines;
  - the selected step's full response or output.
- Build the previous-execution recap deterministically from recorded run data, including identity, outcome, duration when available, and a short response excerpt. Use the latest attempt of a re-executed step, include agent calls and skipped leaf executions, and omit the recap when no earlier terminal leaf exists. A skipped execution shows the triggering `skip_if` expression and no fabricated output. The run view will not invoke an agent to generate summaries.
- Allow the three-line input preview to be expanded on demand. Copying selected-step detail always includes the complete untruncated input even while the preview is collapsed.
- Retain useful primary metadata without reproducing every diagnostic field:
  - always show status, step type, and outcome or duration when available;
  - show agent profile, CLI, and model for agent steps, and also show the target for agent-call executions;
  - show concise usage and cost data when available;
  - show exit status for shell and script steps;
  - retain `capture` when present, and show `skip_if` or `break_if` when it explains the recorded outcome;
  - omit session IDs, workdirs, request IDs, session strategy, other inactive modifiers, and similar diagnostic fields from the primary presentation unless needed for an available action or an error.
- Give container rows a compact selected-step detail surface containing their identity, workflow params or loop counters, outcome and duration, and a bounded roll-up of direct-child statuses and durations.
- Preserve access to exceptionally large responses through the existing lazy-loading safeguard: the pane may initially show the retained tail, with the existing action available to load the complete output.
- Preserve copy and resume actions, workflow-defined UI steps, status indicators, manual drill scope, and run summaries while adapting them to selectable nested rows and selected-step detail.
- Preserve saved-run workflow-version labels, unversioned historical inspection, agent-call count and evidence semantics, unavailable-metrics markers, legacy metric omission, and resumed/inherited model resolution from their existing capability contracts.
- Make Enter the only drill-in action for a selected drillable container. Manual drill-in selects the first real direct child, preserves the existing single-sub-workflow auto-flatten special case, and scopes resume fallback to the nearest workflow-bearing ancestor of the selected row.
- Keep tree and detail navigation discoverable by updating the legend/help text for selectable nested rows, agent calls, groups, input expansion, full-output loading, tail-follow, jump-to-live, and Enter-based drill-in.
- Remove the continuous run-as-a-narrative presentation, recursive detail-block nesting, cross-step scroll synchronization, and cross-step auto-scroll. Persisted output and audit evidence remain unchanged for diagnostics outside the primary run-view presentation.

### Live Run View

- When auto-follow is engaged, expand the active ancestry, select the active leaf itself, and keep that row visible as execution advances.
- Never automatically drill into a sub-workflow, loop, iteration, or agent call. Automatic following does not change drill scope.
- Manual navigation or drill-in pauses auto-follow. The jump-to-live action returns to the root tree when necessary, expands the active path, selects the active leaf, and re-engages auto-follow without drilling into it.
- While auto-follow is paused, keep the active ancestry expanded whenever it is inside the current manual drill scope; moving selection changes the detail pane without collapsing or hiding that active path.
- Treat an active agent call as an active leaf and stream its response separately from its parent agent.
- Continue streaming the active response and tail-following it unless the user scrolls away. Preserve the in-response progress indicator while the selected agent or agent call is running.
- Keep sidebar width grow-only for the duration of the live view, except when an explicit terminal resize requires recalculation, so streaming response text does not repeatedly rewrap.

### Historical Run View

- Use the same tree and selected-step detail design without live auto-follow, jump-to-live, streaming, tail-follow, or animated progress behavior.
- Initially expand the ancestry of the step selected by the existing entry behavior, including the failed step for a failed run.
- Preserve the existing behavior in which completed runs with structured metrics open on the summary screen before the detailed view.
- Once the detailed view is open, leave navigation and drill scope entirely under manual control.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `view-run`:
  - modify step-list rendering, name truncation, drill-in navigation, keyboard focus, pending-step detail, copy behavior, large-output integration, and per-step detail contracts for selectable flattened tree rows and selected-step content;
  - replace read-only selected-row expansion with selectable ancestry expansion;
  - remove continuous-log selection semantics, cross-step scroll synchronization, cross-step auto-scroll, and recursive detail-block nesting;
  - retain manual drill scope for breadcrumbs, sub-workflow context, resume behavior, and level-scoped summaries.
- `live-run-view`:
  - modify cursor auto-follow to expand and select the active leaf without automatic drill-in or drill-out;
  - preserve response streaming and detail-pane tail-follow within the selected active leaf;
  - replace the existing drill-depth auto-follow scenarios with active-ancestry expansion and root-tree jump-to-live behavior;
  - retain manual-navigation pause behavior and live workflow-defined UI-step integration.
- `ui-step`:
  - modify live run-view key ownership so Enter handles drilling only when a non-UI container is selected, remove the separate `d` drill shortcut, and route only keys applicable to the selected form control into the form.
- The now-current `call-agent` and `workflow-versioning` contracts are superseded where necessary: agent calls remain distinct selectable executions with `(N calls)`, filtered persisted output, raw evidence, and resume behavior, while saved historical breadcrumbs retain recorded version labels.

## Technical Approach

Reuse the existing run tree and active-step tracking as the source of truth for both panes.

The sidebar renderer will derive flattened visible rows from the current manual drill scope and expansion state. Auto-follow will expand the active ancestry at the root and move selection directly to the active leaf without mutating drill depth. Manual navigation will use the same flattened row model, while an explicit drill action will continue to set the optional scope used by breadcrumbs and summaries.

Pane measurement will use the untruncated visible rows, including indentation, and then apply truncation only if the sidebar cap is reached. The normal detail-pane minimum wins when space is constrained, without a percentage ceiling on the sidebar. If the terminal is too narrow to fit essential tree chrome, the whitespace gap, and 20 detail columns, the name is already reduced to an ellipsis and the detail width degrades below 20 only as the unavoidable final fallback. A remembered sidebar width will prevent shrink-induced reflow during a run-view session; live execution may grow that width as the active path reveals wider rows.

The detail renderer will build a single selected-step view instead of recursively concatenating blocks for all started steps. It will resolve the prior terminal leaf from audit execution order for the recap, render the first three visual lines of the selected step's effective input with an expansion state, and then render the selected step's complete response subject to the existing large-output loading safeguard. Step-specific metadata will be filtered into a compact primary header while retaining diagnostic data in the underlying run records. Containers will use a bounded direct-child roll-up in place of recursively nested child blocks.

Existing navigation state will continue to distinguish automatic following from manual exploration. Selecting another step will change the detail pane and its selected ancestry without changing manual drill scope or collapsing an active ancestry that lies inside that scope. The tree viewport will minimally scroll to the active row while active-follow is engaged and to the selected row while active-follow is paused. Live auto-follow will maintain a separate active ancestry and return selection to it only through automatic execution while follow is engaged or an explicit jump-to-live action. Copy, scrolling, tail-follow, refresh, workflow-defined UI rendering, saved-run version presentation, resume fallback, and resize behavior will be adapted to the new selected-step content boundaries.

## Out of Scope

- Generating previous-step summaries with an AI model.
- Changing workflow execution, step ordering, audit events, or output capture.
- Removing diagnostic metadata from stored state or audit records.
- Redesigning the run summary screen, run list, workflow-definition view, or workflow-defined UI steps beyond the integration needed for the new tree and selected-step detail behavior.
- Removing the existing safeguard for exceptionally large output.
- Introducing a general-purpose terminal layout framework.
- Providing a continuous whole-run narrative inside the primary detail pane.

## Impact

- `internal/runview/` tree flattening, active-path expansion, pane sizing, detail rendering, scrolling, copying, and associated tests.
- The `view-run` and `live-run-view` specifications.
- The archived `call-agent` and `workflow-versioning` specifications, plus the still-unarchived `start-run` change where it overlaps run-view entry behavior.
- Run-view users inspecting live, failed, completed, or resumed workflows.
- No expected changes to workflow YAML, execution semantics, persisted run formats, agent adapters, or external dependencies.
