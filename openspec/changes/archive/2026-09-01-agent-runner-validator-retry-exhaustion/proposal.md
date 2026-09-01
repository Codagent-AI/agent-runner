## Why

The built-in validator retry workflow can successfully repair the last allowed failing attempt and then exhaust before validating that repair. The containing workflow fails using stale validator evidence even though the final code may be valid. This defect was observed in an external evaluation run whose artifacts are read-only incident evidence. If the required follow-up validation still fails, that unresolved result should remain visible without discarding otherwise completed workflow work.

## What Changes

- Give the validator workflow three repair opportunities and require a validator invocation after every successful repair, including the third.
- Stop repairing after the final verification. A remaining validator failure becomes a first-class, non-blocking warning.
- Add a terminal warning status for explicitly non-blocking step failures while preserving their underlying failure or exhaustion evidence.
- Complete warning-bearing runs successfully with the user-visible status `complete with warnings (N)` in live, listed, and inspected run views.
- Let users press `w` to jump to and cycle through the originating warning steps.

## Capabilities

### New Capabilities

- `validator-retry-verification`: Ensure every allowed validator repair is followed by validation and unresolved final verification is surfaced as a warning.

### Modified Capabilities

- `step-flow-control`: Support an explicit non-blocking warning terminal status without turning recovered failures into final warnings.
- `workflow-bundled-scripts`: Permit script steps to use the generic warning-on-failure flow-control modifier.
- `audit-log-entries`: Persist warning origin and completion-summary evidence without replacing the underlying execution outcome.
- `view-run`: Render warning steps and provide warning navigation for live and historical runs.
- `live-run-view`: Display `complete with warnings (N)` while retaining the normal post-completion landing behavior.
- `run-complete-screen`: Keep warning-bearing completion in the detailed run view with warning navigation available.

## Out of Scope

- Re-running, mutating, or determining the validity of the external evaluation run and candidate worktree used only as read-only incident evidence.
- Changing generic counted-loop iteration or exhaustion semantics.
- Asking a later lead or assumptions-review step to remediate validator warnings.
- Automatically remediating warnings or requiring user acknowledgment before the workflow continues or exits.
- Treating validator failures that are repaired and subsequently pass as final warnings.

## Impact

The change affects the built-in validator workflow, workflow step flow-control validation, execution and completion-state propagation, audit evidence, run discovery, and the live and historical run-view TUI. Existing clean successes and blocking failures retain their current behavior. Regression coverage must be written first and must exercise final-attempt repair, follow-up validation, unresolved-warning completion, persistence, rendering, and navigation.
