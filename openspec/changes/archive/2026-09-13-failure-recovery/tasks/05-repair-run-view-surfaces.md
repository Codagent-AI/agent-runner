# Task: Render repair attempts and failure evidence in the run view, run list, and live view

## Goal

Let a user see what happened when a check failed or was repaired: the run view shows repair suffixes on the check row, attempt children (`attempt N`, `repair N`, `rerun N`), a `Failure evidence` section with the classified reason and the guarded agent's response, repair metadata in the check's detail, and a legend entry; the run list shows the failure reason on failed rows; the live view follows the cursor into an active repair attempt. Pre-change audit logs render exactly as before.

## Background

Read `openspec/changes/failure-recovery/proposal.md` for motivation. The runtime already produces the evidence this task renders. Verify the following before building on it:

- Audit prefix grammar includes an `attempt:N` token after the owning check (`[verify-draft-pr, attempt:1, open-draft-pr]` for a replayed step, `[check-plan, attempt:1, repair]` for an inline repair agent, `[check, attempt:N, check]` for an internal check rerun). `runview.parsePrefix` in `internal/runview/audit.go` already parses it alongside `sub:` and `call:`.
- Events `repair_attempt_start` (data: `attempt`, `form`, `target`, failed check `exit_code`/`stdout`/`stderr`), `repair_attempt_end` (data: `attempt`, `form`, `outcome`, `exit_code`), and `repair_blocked` are intermediate events between the check's single `step_start` and `step_end`, carrying the check's prefix and attempt number.
- A failed shell or script `step_end` carries `guarded_prefix`, `guarded_attempt`, and for a step with `repair`: `repair_form`, `repair_target`, `repair_attempts`, `repair_blocked`. The guarded response is not copied into the check's event; it is the `stdout` of the guarded agent's own `step_end` (and `agent_call_end` events under `call:` prefixes for its calls).
- `state.json` carries `failureReason` for a failed run, written by the runner; `runview.failureReason` (`internal/runview/failure.go`) already prefers it and falls back to the audit-derived string. `exec.ClassifyFailure` renders the reason and can be reused for the detail pane when a run predates `failureReason`.
- `model.RepairFrame` (`checkId`, `form`, `target`, `phase`, `attempts`, `budget`) is present in `NestedStepState.Repair` for a live run.

Design from `openspec/changes/failure-recovery/design.md`:

**Tree (`internal/runview/tree.go`, `internal/runview/audit.go`).** A check node with `Repair` gets children built from audit at ingest time: `repair_attempt_start` creates an `attempt N` node (new `NodeRepairAttempt` type, glyph `⟳`) carrying the failed run's output; events whose prefix contains `attempt:N` attach under the same attempt node: the inline `repair` agent node, or a `rerun N` container holding the replayed steps and the replayed check (`[check, attempt:N, check]`). `repair_attempt_end` closes the attempt node and, when it passed, marks the owning check `success`. This is the single authoritative shape for audit, the tree, and the fixtures. `auditNodeType` and `Tree.resolve` need the new token; `resolve` must create the attempt and rerun containers on demand the way it creates iterations.

**Row labels (`internal/runview/names.go` or wherever `stepRowLabel` lives).** Suffixes from `repair_attempts`, `repair_blocked`, and the live frame: `(repairing N/M)` while an attempt is active, `(repaired N/M)` after recovery, `(blocked)` after a blocked declaration, `(N/M)` after exhaustion; N is attempts used, M is `max` (take `budget` from the frame or `repair_attempts`/`max` from the events; record `max` on `repair_attempt_start` if it is not already emitted, and add it there if needed).

**Selected detail (`internal/runview/selected_detail.go`).** For a failed shell/script node whose `step_end` carries `guarded_prefix`, add a `Failure evidence` rail section before `Current command` / `Current script`, containing the classified reason, the blocked declaration's explanation when present, and the guarded execution's final response looked up by prefix and attempt in the tree (use the existing previous-execution mechanism, `Tree.PreviousExecution`, so a check that was re-executed on resume still exposes the earlier failed node and its evidence). A check with `repair` shows repair form, target, and attempts used in its primary metadata, formatted `repair: rerun open-draft-pr · 0 of 1 used · blocked` (form, target when rerun, `N of M used`, then `blocked` when applicable). An `attempt N` row renders as a shell or script detail of that run; a `repair N` row renders as any headless agent (prompt including the evidence block, response).

**Legend (`internal/runview/view.go` legend overlay).** Add the repair-attempt glyph `⟳` to the type glyph list.

**Run list (`internal/runs/runs.go`, `internal/listview/`).** `runs.RunInfo` gains `FailureReason`, read from `state.json`; `listview` appends it after the step column for inactive, uncompleted runs, truncated with an ellipsis by the existing `fitCell` so the other columns keep their positions. Runs without a failure record show the existing failure reason string.

**Live view (`internal/liverun/`, `internal/runview/` live model).** Auto-follow needs no new rule: attempt children are ordinary descendants and the frontier is whichever leaf is in progress. Confirm that the active-leaf selection lands on the `repair N` row or the replayed step under `rerun N`, that the owning check shows a static in-progress indicator with `(repairing N/M)`, that attempt children are expanded inline while an attempt is active, and that recovery collapses to the check with `✓ (repaired N/M)` and moves follow to the next peer. Fix whatever the existing follow logic gets wrong for these shapes.

**Backward compatibility.** A pre-change audit log with no repair events must render identically to today; add a regression fixture for that.

**Testing conventions.** TDD per `CLAUDE.md`. Extend `internal/runview/tree_test.go`, `internal/runview/audit_test.go`, `internal/runview/selected_detail_test.go`, `internal/runview/names_test.go`, `internal/runview/view_test.go`, `internal/runview/ui_live_test.go`, `internal/runview/historical_integration_test.go`, `internal/listview/model_test.go`, and `internal/runs/runs_test.go`. Recorded audit fixtures go with the existing fixtures (`internal/runview/fixtures_test.go` pattern). Do not use TDD for pure styling (colors, padding). `make fmt`, `make lint`, `make test` before finishing; do not run `make build`.

## Spec

From `specs/view-run/spec.md` (repair-related portions of the modified requirements):

### Requirement: Step list rendering (repair portion)

A check row with `repair` SHALL display a repair suffix once any attempt has run: `(repairing N/M)` while an attempt is active, `(repaired N/M)` after recovery, `(blocked)` after a blocked declaration, and `(N/M)` after exhaustion, where N is attempts used and M is `max`. Each repair attempt SHALL be a child row of the check: a failed check run labeled `attempt N`, followed by either the inline repair agent row or a `rerun N` container whose children are the replayed steps. Attempt children SHALL be shown through the existing inline expansion of direct children when the check is selected or active. Every supported row type, including shell, script, UI, headless agent, interactive agent, agent call, sub-workflow, loop, iteration, group, and repair attempt, SHALL have a type glyph.

#### Scenario: Repaired check row
- **WHEN** a check with `max: 1` failed once and passed after an inline repair
- **THEN** its row shows `✓`, the name, `(repaired 1/1)`, and its type glyph, and when selected expands to show `✗ attempt 1` with the repair-attempt glyph and `✓ repair 1` with the headless agent glyph

#### Scenario: Blocked check row
- **WHEN** a check failed because the guarded agent declared `REPAIR_BLOCKED`
- **THEN** its row shows `✗`, the name, and `(blocked)`, with no attempt children

#### Scenario: Rerun attempt children
- **WHEN** a check with `repair: {rerun: open-draft-pr}` is on its first rerun
- **THEN** the check row expands to `✗ attempt 1` and a `● rerun 1` container whose children are `open-draft-pr` and the replayed check

### Requirement: Detail pane per step type (repair portion)

- **Shell**: `Current command` and `Current output`, with stdout and stderr distinguishable. A failed check with a failure record SHALL additionally show a `Failure evidence` section before `Current command`, containing the classified failure reason, the blocked declaration's explanation when present, and the guarded agent execution's final response. A check with `repair` SHALL show its repair form, target, and attempts used in its primary metadata.
- **Script**: `Current script` and `Current output`, with stdout and stderr distinguishable, with the same `Failure evidence` section and repair metadata as shell steps.

#### Scenario: Failed check detail shows evidence
- **WHEN** a failed `verify-draft-pr` row is selected after `open-draft-pr` declared blocked
- **THEN** the pane shows exit status, `repair: rerun open-draft-pr · 0 of 1 used · blocked`, a `Failure evidence` section with the reason and the `open-draft-pr` response, then `Current command` and `Current output`

#### Scenario: Repair attempt detail
- **WHEN** an `attempt 1` child row is selected
- **THEN** the pane shows that run's exit status and output as a shell or script detail

#### Scenario: Repair agent detail
- **WHEN** a `repair 1` child row is selected
- **THEN** the pane shows the repair agent's profile, CLI, model, prompt including the evidence block, and response, as for any headless agent

### Requirement: Legend overlay

The run view SHALL provide a `?` key that toggles a modal legend overlay showing status glyph meanings and type glyph meanings. The overlay SHALL be dismissible with `?` or Escape.

#### Scenario: Toggle legend overlay on
- **WHEN** the user presses `?` and the legend is not visible
- **THEN** a modal overlay appears showing status glyphs (`●` running, `○` pending, `✓` success, `✗` failed, `⇥` skipped) and type glyphs (`$` shell, ⚙️ headless agent, 💬 interactive agent, ↳ sub-workflow, the loop glyph, the iteration glyph, and the repair-attempt glyph)

From `specs/list-runs/spec.md`:

### Requirement: Failed run rows show the failure reason

A failed run row in the list SHALL display the run's classified failure reason after its status, truncated with an ellipsis to the available column width. Runs with no failure record SHALL show the existing failure reason string.

#### Scenario: Blocked failure in list
- **WHEN** a run failed at `verify-draft-pr` with a blocked declaration
- **THEN** its row shows `failed` followed by `verify-draft-pr failed: … blocked: push rejected: token lacks workflow scope`, truncated to fit

#### Scenario: Reason truncates without breaking columns
- **WHEN** the failure reason is longer than the available width
- **THEN** it is cut with an ellipsis and the other columns keep their positions

From `specs/live-run-view/spec.md`:

### Requirement: Cursor auto-follows the active step (repair portion)

Active leaves include ordinary steps, iterations when they are the execution frontier, UI steps, agent calls, and steps executing inside a repair attempt (the inline repair agent, or a replayed step under a `rerun` container). While a repair attempt is active, the owning check row SHALL show a static in-progress indicator and the `(repairing N/M)` suffix, and its attempt children SHALL be expanded inline. Auto-follow SHALL NEVER drill into or out of a sub-workflow, loop, iteration, group, or agent parent.

#### Scenario: Active step enters an inline repair
- **WHEN** auto-follow is engaged and a check starts its inline repair agent
- **THEN** the check row shows `(repairing 1/1)`, its attempt children expand, and selection moves to the `repair 1` row

#### Scenario: Active step replays under a rerun
- **WHEN** auto-follow is engaged and a rerun replays `open-draft-pr`
- **THEN** selection moves to the replayed `open-draft-pr` row under `rerun 1` without changing the breadcrumb scope

#### Scenario: Recovery collapses to the check
- **WHEN** the replayed check passes
- **THEN** the check row shows `✓` with `(repaired 1/1)` and auto-follow moves to the next peer

From `specs/run-failure-evidence/spec.md`:

### Requirement: Evidence persistence and reconstruction

Failure records SHALL be persisted in the run directory and in audit evidence so that the run view, run list, and debug workflow can reconstruct the failure reason and evidence for a historical run without the workflow definition. Resume SHALL retain earlier failure records.

#### Scenario: Historical run shows evidence
- **WHEN** a failed run is opened in the run view after the workflow file has changed
- **THEN** the failing check's failure evidence, including the guarded agent's response, is displayed

#### Scenario: Evidence survives resume
- **WHEN** a run fails at a check, is resumed, and later completes
- **THEN** the earlier failure record remains inspectable for that attempt

## Test Plan

- `INT-005` (Audit to run view and run list projection): assigned to this task in full. Record audit fixtures for five runs: repaired inline, exhausted inline, blocked rerun, in-progress rerun (live), and a plain failed check that passed after resume; plus one pre-change audit log with no repair events. Build the tree and render the sidebar, the selected detail for the check, an attempt row, and a repair agent row, and the list row. Assert suffixes `(repaired 1/1)`, `(2/2)`, `(blocked)`, `(repairing 1/1)`; attempt children with the `⟳` glyph and a `rerun 1` container; the `Failure evidence` section shows the classified reason and the guarded response; the list row shows the truncated reason; the legend lists the repair-attempt glyph; the resumed run shows the earlier failed execution through the previous-execution context with its `Failure evidence`; the pre-change log renders exactly as before. Runs with `go test ./internal/runview ./internal/listview -run Repair`.

`AT-001` (inspecting these surfaces in the TUI under a PTY) is acceptance work performed later by a tester, not by this task.

## Done When

- Every scenario copied above passes in automated tests and INT-005 passes with `go test ./internal/runview ./internal/listview -run Repair`.
- The tree builds `attempt N`, `repair N`, and `rerun N` nodes from audit alone (no workflow definition needed), with `NodeRepairAttempt` and the `⟳` glyph, and the pre-change fixture renders byte-identically to its pre-change expectation.
- `runs.RunInfo.FailureReason` is populated from `state.json` and rendered in the list row with `fitCell` truncation; rows for runs without it are unchanged.
- The live follow tests cover entering an inline repair, replaying under a rerun, and recovery collapsing to the check.
- `make fmt`, `make lint`, and `make test` pass.
