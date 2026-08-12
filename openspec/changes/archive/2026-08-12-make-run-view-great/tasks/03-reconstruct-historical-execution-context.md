# Task: Reconstruct historical execution context and resume behavior

## Goal

Make historical inspection a deterministic projection of durable workflow, state, audit, metrics, and bounded output artifacts. Add latest-attempt audit start ordering, derive the bounded previous-execution rail, initialize failed/completed historical views correctly, and make nested selection—not incidental drill depth—control resume fallback.

Complete the historical integration boundary in `INT-001` with local fixtures and no model, network, credentials, personal run data, or clipboard mutation.

## Background

You MUST read:

- `openspec/changes/make-run-view-great/proposal.md`, especially previous-execution recap, historical view, evidence preservation, and resume decisions.
- `openspec/changes/make-run-view-great/design.md`, especially sections 5 and 9 (“Derive previous execution from audit start order” and “Keep live and historical views on one rendering path”).
- `openspec/changes/make-run-view-great/specs/view-run/spec.md`, especially `Previous execution context`, `Copy selected step detail` scenario `Copy includes selected context`, `Nested-selection resume fallback`, `Historical selected-step initialization`, and the historical/version/recovery scenarios of `Step list rendering` and `Drill-in navigation with breadcrumbs`.
- `openspec/changes/make-run-view-great/specs/live-run-view/spec.md`, especially the historical persisted-output and interactive-no-transcript scenarios under `Real-time step output`.
- `openspec/changes/make-run-view-great/test-plan.md` for `INT-001`.

Relevant production paths:

- `internal/runview/audit.go`: ordered replay, `FileTailer`, event application, attempts, calls, iterations, sub-workflows, skipped/error context, and audit-only reconstruction.
- `internal/runview/tree.go`: `Tree`, `StepNode`, latest-attempt data, leaves/containers, stable node identity, dynamic calls, and traversal.
- `internal/runview/model.go`: `New`, `loadRunTree`, historical entry mode, selected-node initialization, resume actions, call-output loading, and copy/full-output actions.
- `internal/runview/resolve.go`, `breadcrumb.go`, `failure.go`, `output.go`, `summary.go`, and `detail.go`.
- Tests in `audit_test.go`, `resolve_test.go`, `failure_test.go`, `model_test.go`, `agent_call_view_test.go`, `tui_metrics_test.go`, and new historical integration fixtures alongside `internal/runview`.

Implementation constraints:

- Use TDD. Begin with ordered replay and previous-candidate tests, then add the production historical boundary fixture.
- Assign a monotonically increasing in-memory start ordinal while applying genuine execution-start events in replay order. Include `step_start`, `iteration_start`, `sub_workflow_start`, `agent_call_start`, and any equivalent selectable container starts required by the approved design.
- Store the latest ordinal on a logical node. A later attempt intentionally replaces the earlier ordinal and selected-detail attempt metadata, while run totals still include every attempt.
- Resolve the previous execution as the terminal leaf with the greatest nonzero latest ordinal below the selected execution’s ordinal. Exclude containers; include calls and skipped leaves. A pending selected node without an established ordinal has no previous rail.
- Use output-source rules exactly: filtered response for headless agents/calls; stdout for successful shell/script; stderr then stdout for failed shell/script; metadata only for interactive execution; recorded outcome without submitted values for UI; status plus triggering `skip_if` and no output for skipped leaves.
- Sanitize, trim trailing blank rows, wrap at current detail width, and retain the final two visual rows; prefix an ellipsis when earlier rendered content was omitted. Resize must rebuild the bound.
- Generalize the existing bounded call-output loader so a previous call can load the same filtered persisted evidence as a selected call. Never reconstruct a full response from audit payloads.
- Preserve raw evidence and existing large-output bounds.
- Direct resume of a selected agent/call wins. Otherwise search the nearest workflow-bearing selected ancestor, even when reached only through inline expansion; root is the final fallback. Manual drill `path` cannot override nearer selected ancestry.
- Failed historical inspection opens detailed at root manual scope with the failed ancestry expanded and failed leaf selected. Completed runs with structured metrics still open summary-first. Inactive detail has no polling, live follow, streaming, tail-follow, `l`, or animation.
- Preserve recorded version labels or `unversioned`, missing-workflow reconstruction, latest metrics/model semantics, and summary behavior.

## Spec

The following source requirements and scenarios are copied from `specs/view-run/spec.md`.

### Requirement: Previous execution context

When a terminal leaf execution precedes the selected execution in audit start order, selected detail SHALL begin with a visually distinct rail labeled `Previous: <step-name>`. Containers SHALL NOT qualify as previous executions; their terminal leaf descendants MAY qualify. Agent calls and skipped leaf executions SHALL qualify.

The previous rail SHALL show known type, status, outcome or exit status, and duration compactly. When captured output exists, the rail SHALL additionally show at most the final two nonblank visual rows after filtering, sanitization, and wrapping to the current detail width. If earlier rendered content was omitted, the excerpt SHALL begin with an ellipsis. A resize SHALL recompute the two visual rows.

For headless agents and agent calls, the excerpt SHALL come from the ordinary filtered response. For successful shell and script executions it SHALL come from stdout. For failed shell and script executions it SHALL prefer stderr and fall back to stdout. Interactive agent and interactive shell executions SHALL show only known metadata and SHALL NOT fabricate a transcript. UI executions SHALL show their recorded outcome without submitted form values. A skipped execution SHALL show `skip_if` and the triggering expression and SHALL NOT show an output excerpt.

The rail SHALL be omitted when no earlier terminal leaf exists. For a re-executed logical step, ordering and recap metadata SHALL use its latest attempt.

#### Scenario: Headless agent recap uses response tail
- **WHEN** a completed headless agent precedes the selected execution and has more than two wrapped response rows
- **THEN** its previous rail shows the final two rows with a leading ellipsis

#### Scenario: Successful script recap uses stdout tail
- **WHEN** a successful script precedes the selected execution
- **THEN** its previous rail shows up to the final two nonblank visual rows of stdout

#### Scenario: Failed script recap prefers stderr
- **WHEN** a failed script has both stdout and stderr
- **THEN** its previous rail shows the final two nonblank visual rows of stderr

#### Scenario: Interactive agent recap has no transcript
- **WHEN** a completed interactive agent precedes the selected execution
- **THEN** its previous rail shows known interactive-agent metadata, outcome, and duration without response text

#### Scenario: Agent call qualifies as previous execution
- **WHEN** a completed agent call is the latest completed leaf before the selected execution
- **THEN** the rail label identifies that call and its excerpt uses the call's separately filtered response

#### Scenario: Skipped execution shows reason without output
- **WHEN** a skipped leaf is the latest terminal execution before the selected execution
- **THEN** its previous rail shows skipped status and the triggering `skip_if` expression without a fabricated output excerpt

#### Scenario: Container resolves to completed leaf
- **WHEN** a completed sub-workflow precedes the selected step and its last completed child leaf is the latest prior execution
- **THEN** the previous rail identifies the child leaf rather than the sub-workflow container

#### Scenario: First execution has no previous rail
- **WHEN** no terminal leaf precedes the selected execution
- **THEN** selected detail omits the previous rail

#### Scenario: Resize preserves two-row bound
- **WHEN** the detail pane width changes
- **THEN** the previous excerpt is rewrapped and still occupies at most two visual rows

The production-populated previous section also controls this copied-detail scenario:

#### Scenario: Copy includes selected context
- **WHEN** a previous-execution rail is visible and the user presses `c`
- **THEN** the copied text includes directory, breadcrumb, previous execution, primary metadata, current input, and current response/output

### Requirement: Nested-selection resume fallback

For a completed inactive run, direct resume of the selected resumable agent or agent call SHALL take precedence. When the selected row is not directly resumable, the `r` fallback SHALL search the nearest workflow-bearing ancestor of the selected row, including an ancestor reached only through inline expansion, and SHALL resume the last resumable agent execution inside that workflow. If no such ancestor exists, it SHALL use the root workflow. Manual drill scope SHALL NOT override the nearer selected ancestry.

#### Scenario: Inline nested selection scopes resume
- **WHEN** a non-resumable row selected through inline expansion lies inside a sub-workflow with an earlier resumable agent execution
- **THEN** `r` resumes the last resumable execution in that nearest sub-workflow

#### Scenario: Selected direct resume takes precedence
- **WHEN** the selected row is a resumable agent or agent call
- **THEN** `r` resumes that exact selected execution

#### Scenario: Root fallback remains available
- **WHEN** no workflow-bearing ancestor below the root contains a resumable execution
- **THEN** `r` falls back to the last resumable execution in the root workflow

### Requirement: Historical selected-step initialization

An inactive historical detail view SHALL use the same selectable tree and selected-step detail without live auto-follow, jump-to-live, response streaming, tail-follow, or animated progress.

When a failed run opens directly in detailed view, the tree SHALL expand the failed leaf's ancestry and select that leaf. Other historical detail entries SHALL expand the ancestry of the selection established by their existing entry behavior. Completed runs with structured metrics SHALL continue to open on the summary screen before the user switches to detail.

#### Scenario: Failed historical run selects failed leaf
- **WHEN** a failed run is opened for inspection
- **THEN** the detailed view expands the failed ancestry and selects the failed leaf

#### Scenario: Completed metrics run still opens summary
- **WHEN** a completed run with structured metrics is opened
- **THEN** the summary screen appears first and the selected ancestry is expanded when the user switches to detail

#### Scenario: Historical navigation remains manual
- **WHEN** the user navigates an inactive historical detail view
- **THEN** selection and drill scope change only in response to user input

The following source scenarios also control this task:

#### Scenario: Executed steps recover without workflow file
- **WHEN** a saved run's workflow file is unavailable but audit history identifies executed top-level steps
- **THEN** the tree reconstructs those executed rows and does not report a missing-workflow error

#### Scenario: Historical detail loads persisted output
- **WHEN** a terminal run's recorded execution is selected
- **THEN** the pane reads its output files and applies the existing large-output threshold

#### Scenario: Interactive execution has no transcript
- **WHEN** an interactive agent or interactive shell exits
- **THEN** Agent Runner creates no transcript output files and selected detail fabricates no response

## Test Plan

- `INT-001: Historical view projects durable run artifacts`

Implement this as ordinary Go integration coverage alongside `internal/runview`, included by `go test ./...` and CI.

Use an isolated temporary project/session with a resolvable nested workflow and real serialized `state.json`, ordered `audit.log`, `run-metrics.json`, and output files. Include completed shell/script leaves, a failed script with both streams, interactive and headless agents, a dynamic fake call with separately persisted raw output, repeated attempts, a skipped leaf with triggering `skip_if`, nested containers with more than five children, inline selection whose nearest workflow ancestor differs from drill scope, structured metrics, and output beyond the lazy-loading threshold. Add failed-run and missing-workflow variants.

Exercise the production historical constructor, representative terminal sizes and navigation, semantic selected detail, plain copy construction without writing the system clipboard, and full-output loading.

Completion signals:

- replay produces deterministic latest-attempt order;
- previous context chooses the correct leaf and source for every representative type;
- semantic copy of production-selected detail includes the derived previous execution together with directory, breadcrumb, primary metadata, complete input, and the currently loaded response/output;
- interactive execution has no fabricated transcript;
- calls use bounded persisted files and ordinary adapter filtering, never an audit response;
- call evidence/resume semantics and compact metadata remain intact;
- unavailable/absent/legacy metrics and resumed/inherited model resolution remain correct;
- skipped recap shows its reason and no output;
- nearest selected workflow ancestry controls `r`;
- saved versions/`unversioned`, failed entry, summary-first entry, copy/full input, lazy/full output, and missing-workflow recovery all work.

Constraints: local fixtures and fake CLIs only; bounded file reads; no network, credentials, model invocation, API cost, personal data, or system clipboard write.

## Done When

- Full audit replay and incremental event application assign identical deterministic ordinals.
- Previous-execution selection and rendering satisfy every scenario above, including retry, container, call, skipped, UI, interactive, and resize cases.
- Production historical copy includes the audit-derived previous rail without duplicating or omitting any selected-detail section.
- Historical failed/completed entry and inactive behavior match the approved specifications.
- Resume selection uses exact selected ancestry precedence and remains unavailable when the run/call is not eligible.
- Existing version, metrics, summary, recovery, filtering, raw-evidence, and large-output behavior is preserved.
- `INT-001` passes through production constructors and real serialized fixtures.
- Targeted `internal/runview` tests and the repository test suite pass for this boundary.
