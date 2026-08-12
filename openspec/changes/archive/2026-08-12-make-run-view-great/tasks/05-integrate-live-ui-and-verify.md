# Task: Integrate live UI ownership and verify the complete run view

## Goal

Embed workflow-defined UI steps in the selected-execution detail without making the form modal. Preserve the pending request while users navigate or drill elsewhere, route only keys applicable to the selected form, remove the separate `d` drill shortcut, and complete the full live coordinator integration boundary in `INT-002`.

Finish with repository-wide formatting, tests, lint, OpenSpec validation, and cleanup of obsolete run-view code/tests left behind by the approved replacement. Do not execute or claim agent-acceptance flows; those remain post-implementation acceptance obligations.

## Background

You MUST read:

- `openspec/changes/make-run-view-great/proposal.md`, especially preservation of workflow-defined UI steps, Enter-only drilling, and live jump-to-active behavior.
- `openspec/changes/make-run-view-great/design.md`, especially section 8 (“Integrate live UI as selected-node detail”) and the implementation/verification section.
- `openspec/changes/make-run-view-great/specs/live-run-view/spec.md`, especially `UI steps render inside the live-run-view chrome` plus the active-follow contracts it depends on.
- `openspec/changes/make-run-view-great/specs/ui-step/spec.md`, especially `Live run-view navigation remains available during UI steps`.
- `openspec/changes/make-run-view-great/test-plan.md` for `INT-002`, the explicit absence of new E2E obligations, and the later `AT-*` flows that implementors must not claim.

Relevant production paths:

- `internal/runview/ui_live_test.go`, `model.go`, `detail.go`, `view.go`, and selected-tree/drill/help handling.
- `internal/uistep/handler.go` and `handler_test.go`.
- `internal/liverun/coordinator.go`, `messages.go`, and coordinator tests around `UIRequestMsg`.
- The non-UI live integration surfaces in `internal/liverun` and `internal/runview` exercised by `INT-002`.

Implementation constraints:

- Use TDD around key ownership and an unresolved reply channel.
- Keep the `uistep.Model` alive independently of tree selection. Navigating away changes selected detail but cannot resolve, discard, or recreate the pending request.
- Render the live form only when its exact UI node is selected. Otherwise render ordinary selected detail for the chosen node and route no form keys.
- Up/Down always belong to tree navigation. Escape drills out one manual scope or performs the live top-level Escape behavior. `j`/`k` scroll selected UI content. `q` and Ctrl+C remain run-view chrome actions.
- Route Left/Right, Tab, Shift-Tab, Enter, and other form keys only when applicable to the focused form control. An inapplicable key must not be consumed or resolve the form.
- Enter drills a selected non-UI container even while another UI request is pending. It submits only when it is applicable to the selected UI control. Remove `d` drilling and all help text/tests that advertise it.
- `l` returns to root scope, expands/selects the active UI leaf, restores form key routing, and re-engages live follow without drilling or resolving.
- UI body/form rendering must remain bounded by selected-detail width/height and use `Current form`; after resolution, durable recorded outcome appears under `Current outcome`.
- Previous-execution recap for UI may show recorded outcome but never submitted form values.
- Do not introduce a separate modal, terminal layout framework, persisted UI state format, workflow YAML change, audit schema, or new dependency.
- `INT-002` must use real production message boundaries, deterministic local subprocesses/fake CLIs, isolated files, and zero-cost operation.

## Spec

The following source requirements and scenarios are copied from `specs/live-run-view/spec.md` and `specs/ui-step/spec.md`.

### Requirement: UI steps render inside the live-run-view chrome

When the active execution is `mode: ui`, the live run view SHALL render its title, body, inputs, and actions inside the ordinary selected-detail chrome. The root tree SHALL expose the UI step through inline active-ancestry expansion without automatic drill-in, and the active UI row SHALL be selected while auto-follow is engaged.

The workflow breadcrumb and tree SHALL remain visible. UI content SHALL remain bounded by the detail width and height. The selected UI detail SHALL group form content under `Current form` and its eventual recorded result under `Current outcome`.

When auto-follow is paused because the user navigated or manually drilled elsewhere, a newly active UI step SHALL NOT force a scope or selection change. The explicit `l` action SHALL return to root scope, expand and select the active UI leaf, and re-engage follow without resolving the UI step.

Existing UI input routing and quit behavior SHALL remain: Left/Right, Tab, Shift-Tab, Enter, and other keys operate the selected form only when applicable to its focused control; Up/Down remain tree navigation; Ctrl+C exits immediately; and keys consumed by the form SHALL NOT trigger chrome actions. Enter SHALL drill only when a non-UI drillable container is selected. The live run view SHALL NOT retain a separate `d` drill shortcut.

#### Scenario: Workflow tree remains visible during UI step
- **WHEN** a UI step is selected and active
- **THEN** the workflow breadcrumb, inline active tree, and UI form are visible together

#### Scenario: UI body wraps within detail pane
- **WHEN** UI content exceeds the available detail width
- **THEN** it wraps within that pane rather than extending beyond the chrome

#### Scenario: Active UI ancestry expands without drilling
- **WHEN** auto-follow is engaged and execution enters a nested UI step
- **THEN** the root tree expands and selects the UI leaf without changing breadcrumb scope

#### Scenario: Paused manual scope is preserved
- **WHEN** the user manually navigated or drilled away before a UI step becomes active
- **THEN** the UI step remains pending without forcing a scope or selection change

#### Scenario: Jump-to-live returns to UI without drilling
- **WHEN** a UI step is active and the user presses `l`
- **THEN** the view returns to root scope, expands and selects the UI leaf, and leaves the form unresolved

#### Scenario: UI controls do not trigger chrome actions
- **WHEN** a selected UI control consumes Left, Right, Tab, Shift-Tab, or Enter
- **THEN** the form handles the key without triggering navigation or quit

#### Scenario: Ctrl+C during selected UI exits
- **WHEN** the user presses Ctrl+C while a UI step is selected
- **THEN** the application exits immediately and the UI step remains unresolved

### Requirement: Live run-view navigation remains available during UI steps

When a `mode: ui` step is rendered inside the live run view, it SHALL NOT behave as a modal that blocks workflow-tree navigation. The user SHALL be able to select another real tree row while the UI step remains pending. Navigating away SHALL pause active-step auto-follow, preserve the active ancestry whenever it remains inside the current manual scope, and show ordinary selected detail for the chosen row.

When the active UI row is selected, Left/Right, Tab, Shift-Tab, Enter, and other keys SHALL be routed to the UI form only when applicable to the focused control. Up/Down SHALL remain owned by workflow-tree navigation. Escape SHALL drill out one manually entered scope or perform the live run view's top-level Escape behavior. `j` and `k` SHALL scroll overflowing selected UI content without resolving it.

Enter SHALL remain the ordinary run-view drill action when selection is on a non-UI drillable container, including while a different UI step remains pending. The separate `d` drill shortcut SHALL be removed and SHALL NOT appear in help.

The jump-to-live action `l` SHALL return to root manual scope when necessary, expand the active UI ancestry inline, select the UI leaf, and re-engage auto-follow without drilling into the UI step or resolving it. The run-view `q` and Ctrl+C behavior SHALL remain available. Standalone UI rendering outside the live run view MAY retain its existing input navigation.

#### Scenario: Tree navigation leaves UI pending
- **WHEN** a UI step is pending and the user selects another real tree row
- **THEN** selected detail changes, auto-follow pauses, and the UI step remains unresolved

#### Scenario: Navigation works away from active UI
- **WHEN** selection is away from a pending active UI step
- **THEN** workflow-tree and selected-detail keys operate on the chosen row rather than the form

#### Scenario: Enter drills selected non-UI container
- **WHEN** the user selects a non-UI drillable container and presses Enter while a UI step is pending
- **THEN** the run view manually drills into that container and leaves the UI step unresolved

#### Scenario: Escape drills out during pending UI
- **WHEN** the user is manually drilled into a container and presses Escape while a UI step is pending
- **THEN** the run view returns to the parent manual scope without resolving the UI step

#### Scenario: j and k scroll selected UI content
- **WHEN** the active UI row is selected and its content exceeds the detail height
- **THEN** `j` and `k` scroll that content without changing selection or resolving the form

#### Scenario: Jump-to-live returns without drilling
- **WHEN** a UI step is active and auto-follow is paused
- **THEN** pressing `l` returns to root scope, expands and selects the UI leaf, re-engages follow, and leaves the form unresolved

#### Scenario: Quit remains available
- **WHEN** the active UI row is selected and the user presses `q`
- **THEN** the live run view starts its ordinary quit flow rather than routing `q` to the form

#### Scenario: Form action keys remain routed
- **WHEN** an active UI form is selected and the user operates Left/Right, Tab, Shift-Tab, or Enter on a control to which that key applies
- **THEN** the form handles the applicable action without triggering tree navigation

#### Scenario: Inapplicable form key is not consumed
- **WHEN** the active UI row is selected and a key is not applicable to the focused form control
- **THEN** the UI model does not consume it or accidentally resolve the form

#### Scenario: d is not a drill shortcut
- **WHEN** the user presses `d` while a UI step is pending
- **THEN** the run view does not treat `d` as drill-in

## Test Plan

- `INT-002: Live coordinator messages and output preserve view ownership`

Implement this as ordinary Go integration coverage across `internal/runview` and `internal/liverun`, included by `go test ./...` and CI.

Use an isolated live session with a nested workflow tree/output directory and deterministic local subprocess/fake-CLI fixtures. Emit delayed stdout/stderr, split ANSI sequences, enough rows to scroll, and independently addressed parent/call output. Drive real `liverun.Coordinator` and `TUIProcessRunner` messages through the same forwarding terminal-program boundary used by Bubble Tea.

The integration journey must:

1. advance from a parent into nested leaves and a fake call;
2. stream output while selected;
3. navigate elsewhere, scroll earlier response, and continue output/execution;
4. exercise `t` and `l`;
5. issue a live UI request, navigate away, drill elsewhere, return with `l`, scroll the form, and resolve it; and
6. finish once successfully and once with a nested failure.

Assert active-leaf expansion without drill mutation; separate parent/call attribution; display sanitization with raw persistence; split-sequence handling; escaped output paths; deterministic first-byte latency; off-selection accumulation; paused selection/offset ownership; continuous `t`; root active `l`; unresolved UI navigation and restored form routing; and root-scope failed-leaf terminal selection.

Constraints: local deterministic subprocesses and fake CLIs only; no real agent, network, credentials, personal data, model invocation, or API cost.

There is no new automated `E2E-*` obligation. The test plan explicitly leaves full-terminal visual judgment to later agent acceptance. Do not run or claim `AT-001` through `AT-004` or human-only testing in this implementation task.

## Done When

- A pending live UI request survives arbitrary permitted tree selection and manual drill changes.
- Form keys are routed only for the exact selected UI node and applicable focused control; chrome/navigation keys retain their specified ownership.
- Enter, Escape, `j`/`k`, `l`, `q`, Ctrl+C, and the absence of `d` satisfy every scenario above.
- UI form/outcome rendering remains within selected-detail chrome and recap/copy paths never expose submitted values improperly.
- `INT-002` passes through real coordinator, process-runner, message, persistence, tree, detail, and UI boundaries.
- Obsolete continuous-log, direct-child cursor, legacy auto-follow, modal UI, and `d`-drill code/tests are removed rather than left as dormant alternatives.
- Run `make fmt`, targeted package tests, `make test`, `make lint`, and `openspec validate make-run-view-great --strict`; all pass.
