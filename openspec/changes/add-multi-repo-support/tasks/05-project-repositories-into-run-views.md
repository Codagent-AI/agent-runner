# Task: Project Repository Execution into Audit and Run Views

## Goal

Represent explicit repository execution consistently in audit data, metrics artifacts, historical and live run trees, selected detail, summaries, output filtering, and pull-request chrome, while flattening the transparent implicit repository so existing single-repository presentation remains unchanged.

## Background

You MUST read:

- `openspec/changes/add-multi-repo-support/design.md`, especially **Audit, Pull Requests, Metrics, and Run Views**, **Persistence and Resume**, and **Risks and Mitigations**.
- `openspec/changes/add-multi-repo-support/specs/audit-log-entries/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/view-run/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/live-run-view/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/run-complete-screen/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/run-metrics-artifact/spec.md`.
- `openspec/changes/add-multi-repo-support/specs/run-pull-request-link/spec.md`.
- `openspec/changes/add-multi-repo-support/test-plan.md` for `INT-004` and `E2E-003`.

Relevant implementation seams:

- Extend `internal/audit/types.go`, `internal/audit/logger.go`, `internal/runner/` fan-out emission, `internal/exec/step_audit.go`, and every nested/control/error emission helper so explicit active repositories add fields and `repo:<name>` nesting while implicit/workspace events retain legacy shape.
- Add `NodeRepository` and repository metadata to `internal/runview/tree.go` and model types. Reconstruct explicit repository containers from persisted selection/state plus audit lifecycle evidence so never-started repositories appear pending; do not infer selected order from config maps or event order.
- Extend `internal/runview/audit.go`, `projection.go`, `resolve.go`, `selected_detail.go`, `output.go`, `summary.go`, `breadcrumb.go`, and rendering/navigation helpers for name-only rows, drill-in/out, aggregate detail, repository-specific output/evidence/metrics/PRs, and implicit flattening.
- Extend `internal/liverun/` event application and focus logic so repository status updates incrementally, deepest-child auto-follow crosses repository transitions, failure focuses the nested failing step, and manual pause/jump-to-live behavior is preserved.
- Extend `internal/metrics/` serialization to add explicit repository name/root fields on step, iteration, and agent-call records without changing workspace or implicit records; ensure rollups count each execution once.
- Treat pull-request label parsing and hyperlink safety independently. Render all current links in workspace-first then persisted repository order, preserve links at every drill depth and summary, and use visible-width measurement rather than OSC 8 byte length.
- Update `docs/run-state-and-audit.md` and `docs/usage-and-cost-tracking.md` for the additive repository fields, evidence isolation, tree hierarchy, and ordered PR links.
- Build PTY tests with terminal-screen reconstruction as required by repository guidance; raw differential repaint bytes are not reliable assertions.

## Spec

The following approved requirements and scenarios are binding.

### Requirement: Repository boundary event data

Each explicit configured repository execution MUST emit one `repository_start` and one terminal `repository_end`. The start event MUST identify the repository name, canonical root, zero-based position, and total selected repository count. The end event MUST include the repository outcome and duration. The transparent implicit `default` repository MUST retain legacy audit shape without repository boundary events.

#### Scenario: Pending repository never starts
- **WHEN** backend fails before pending repository frontend begins
- **THEN** the audit log contains no repository boundary pair for frontend

#### Scenario: Implicit repository compatibility
- **WHEN** a scope-aware run executes through the implicit `default` repository
- **THEN** the audit log retains the legacy step prefixes and omits repository boundary events and explicit repository fields

### Requirement: Repository identity on active events

Every audit event emitted while an explicit configured repository is active MUST include `repository_name` and `repository_dir` fields in addition to its nesting prefix. Events emitted without an explicit active repository, including the transparent implicit `default` path, MUST omit these fields rather than writing empty values.

#### Scenario: Nested and control events
- **WHEN** a loop, sub-workflow, agent call, control event, or error event is emitted while frontend is active
- **THEN** the event includes frontend's repository name and root

#### Scenario: Root error during repository execution
- **WHEN** an unexpected error uses an empty root prefix while a repository is active
- **THEN** its explicit repository fields still identify the active repository

### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `repository_start`, `repository_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `agent_call_start`, `agent_call_end`, `error`, `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, and `child_continued`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Repository events are container boundaries
- **WHEN** the audit logger receives a `repository_start` or `repository_end` event
- **THEN** it records the event as a repository execution boundary rather than an ordinary workflow step

### Requirement: Nesting prefix

Every audit log entry SHALL include a nesting prefix that encodes the full path to the current execution point. Repository executions carry `repo:<repository_name>`. Loop steps carry their iteration index as `step_name:N`. Sub-workflows are marked with `sub:workflow_name`. Top-level steps use `[step_name]`. Root-scoped events (`run_start`, `run_end`, `error`) use an empty prefix string, while explicit repository fields retain active repository identity on a root-scoped error.

#### Scenario: Nested repository step
- **WHEN** step `implement-task` executes inside repository-scoped group `implement-task-groups` and loop `task-loop` at iteration 0 for backend
- **THEN** entries have prefix `[implement-task-groups, repo:backend, task-loop:0, implement-task]`

### Requirement: Context snapshot on start events

Start events (`run_start`, `repository_start`, `step_start`, `iteration_start`, `sub_workflow_start`, `agent_call_start`) SHALL include the full context snapshot: all params and all captured variables available at that point. An `agent_call_start` SHALL use the parent attempt's current context snapshot. While a repository is active, the available captures MUST consist of run-level workspace captures plus captures scoped to the active repository and MUST exclude captures belonging to other repositories.

#### Scenario: Repository start context
- **WHEN** a start event is emitted while backend is active
- **THEN** its context includes workspace captures and backend captures but excludes captures owned by frontend

### Requirement: End event data

End events (`step_end`, `run_end`, `repository_end`, `iteration_end`, `sub_workflow_end`, `agent_call_end`) SHALL include the outcome (`success`, `failed`, `aborted`, `exhausted`, `skipped`) and duration in milliseconds.

#### Scenario: Repository end includes outcome and duration
- **WHEN** a repository execution reaches a terminal outcome
- **THEN** its `repository_end` entry includes that outcome and the repository execution's duration in milliseconds

### Requirement: Repository execution containers

The run view MUST represent each explicit repository execution as one container level in the workflow tree. The container label MUST be the configured repository name without a `repo` prefix or repository path. Workspace-scoped work MUST remain outside repository containers.

#### Scenario: Repository containers follow workspace work
- **WHEN** workspace planning is followed by repository execution for backend and frontend
- **THEN** the tree shows the planning steps at workspace level and separate `backend` and `frontend` containers at the repository-scoped position

#### Scenario: Repository body remains nested
- **WHEN** backend contains implementation, validation, and pull-request steps
- **THEN** those steps appear beneath the backend container and do not appear as peers of frontend's steps

#### Scenario: Implicit single repository remains flattened
- **WHEN** a scope-aware run uses the implicit `default` repository in a traditional single-repository project
- **THEN** the run view omits the repository container and preserves the existing visible workflow shape

### Requirement: Repository container navigation and detail

A repository container MUST support the existing selection, inline expansion, drill-in, breadcrumb, log scoping, status, and aggregate-detail behaviors used by other execution containers. Its status MUST summarize only execution belonging to that repository.

#### Scenario: Repository detail is aggregate
- **WHEN** backend is selected
- **THEN** its detail summarizes backend's outcome, duration, metrics, and pull-request result without including frontend execution

#### Scenario: Failed repository is identifiable
- **WHEN** a nested backend step fails and frontend remains pending
- **THEN** backend shows a failed container status while frontend shows pending

#### Scenario: Saved run reconstructs repositories
- **WHEN** the user inspects a saved multi-repository run
- **THEN** the repository containers and their nested execution are reconstructed from persisted state and audit evidence

### Requirement: Live repository visibility

The live run view MUST show explicit repository executions using the same named repository containers as the saved run view. Repository containers MUST update their status and nested output as audit events arrive.

#### Scenario: Next repository starts
- **WHEN** frontend begins after backend completes
- **THEN** backend remains visibly complete and frontend becomes active

### Requirement: Auto-follow tracks active repository execution

While auto-follow is engaged, the cursor and visible tree MUST follow the deepest active execution through its repository container. Moving between repositories MUST use the same automatic ancestry and focus behavior as moving between existing nested containers.

#### Scenario: Manual navigation pauses repository following
- **WHEN** the user manually selects another row or drills to another scope while repository execution continues
- **THEN** new repository activity does not steal focus

#### Scenario: Jump to live restores repository following
- **WHEN** the user invokes the existing jump-to-live action after pausing on another repository
- **THEN** the view returns to the active repository's deepest active execution and re-engages auto-follow

#### Scenario: Failure selects nested repository step
- **WHEN** a nested backend step fails
- **THEN** the view focuses that failed step within backend rather than selecting only the repository container

### Requirement: Live repository output isolation

Live detail output selected within a repository MUST include only the selected execution subtree. Switching repository selection MUST replace the detail with that repository's accumulated output without losing persisted output from completed repositories.

#### Scenario: Completed repository output remains available
- **WHEN** backend completes and frontend becomes active
- **THEN** the user can manually return to backend and inspect its accumulated output

### Requirement: Repository rows in completion summaries

At a summary level containing explicit repository executions, the completion summary MUST show one row per repository in persisted affected-repository order. Each repository row MUST roll up only that repository's nested outcome, duration, token usage, and cost.

#### Scenario: Failed repository row
- **WHEN** backend fails before frontend starts
- **THEN** backend shows a failed outcome and frontend shows a pending outcome

### Requirement: Repository summary drill-down

A repository summary row MUST be drillable using the existing summary navigation. Drilling into a repository MUST show its immediate child execution rows and MUST scope displayed totals to that repository.

#### Scenario: Repository breadcrumb
- **WHEN** the user drills into backend's completion summary
- **THEN** the breadcrumb appends `backend` while retaining the run's comma-separated pull-request segment

### Requirement: Run totals include workspace and repositories once

The run totals line MUST aggregate workspace-scoped execution and every repository execution without double-counting child metrics already included in a repository row. Existing unavailable-cost and partial-pricing behavior MUST apply independently to repository rows and the run total.

#### Scenario: Called-agent metrics remain nested once
- **WHEN** a repository-scoped parent includes called-agent metrics in its existing rollup
- **THEN** the repository and run totals do not add the called-agent metrics a second time

### Requirement: Repository identity in metrics records

Every `run-metrics.json` step, iteration, and agent-call record produced while an explicit repository is active MUST include `repository_name` and `repository_dir` fields in addition to its nesting prefix. Workspace records and transparent implicit-repository records MUST omit those fields. Run-level aggregation MUST continue to include workspace and repository records exactly once without requiring external consumers to parse repository identity from the prefix.

#### Scenario: External consumer groups repository metrics
- **WHEN** an external consumer reads records from a multi-repository run
- **THEN** it can group explicit repository records by `repository_name` without decoding nesting prefixes

### Requirement: Run view links the recorded pull request in the breadcrumb

When a run has recorded pull-request URLs, the run-view breadcrumb SHALL include a pull-request segment after the run status, rendered in the same dim style and separated by the same `·` separator as the existing recorded-version and profile-set segments. A multi-repository run SHALL show every current repository pull request in persisted affected-repository order, separated by `, `. A run-level URL, when present, SHALL precede repository URLs. When a run has no recorded URL, the breadcrumb SHALL be unchanged from its current form.

Each displayed pull request SHALL be an independent OSC 8 terminal hyperlink whose target is its complete recorded URL and whose visible text is a short label. The label SHALL be `PR #<number>` when the URL's path has the form `/<owner>/<repo>/pull/<number>`, and SHALL be `PR` for any other URL, so a non-GitHub or unparseable URL still renders and still links rather than showing a malformed number.

Whether a URL may be linked and how it is labelled SHALL be independent decisions. A recorded URL SHALL be linked when it is safe to embed as a hyperlink target — that is, when it contains no control characters, uses the `https` scheme, has a non-empty host, and carries no userinfo component. Host and path SHALL NOT affect linkability. A recorded URL failing any safety condition SHALL render the plain `PR` label with no OSC 8 escape sequence at all.

#### Scenario: Multiple repository pull requests are comma separated
- **WHEN** backend records pull request 62 and frontend records pull request 17 in that affected-repository order
- **THEN** the workspace breadcrumb includes `· PR #62, PR #17`, with each label independently linked to its repository's complete URL

#### Scenario: Repository completion order does not reorder links
- **WHEN** recorded repository events are replayed in an order different from the persisted affected-repository order
- **THEN** the breadcrumb orders repository pull requests by persisted affected-repository order

#### Scenario: Unsafe URL renders unlinked
- **WHEN** a run records a URL that contains control characters, uses a scheme other than `https`, or carries a userinfo component
- **THEN** its position in the comma-separated segment shows a plain `PR` label containing no OSC 8 escape sequence

#### Scenario: Chrome alignment unaffected by escapes
- **WHEN** the breadcrumb includes multiple hyperlinked pull-request labels
- **THEN** the chrome logo and rule are positioned using visible label and separator width only

### Requirement: Repository pull requests in run detail

When a repository has a recorded pull-request URL, its container detail MUST display that repository's current pull-request link. The run breadcrumb MUST continue to display the complete comma-separated list defined by the pull-request-link capability.

#### Scenario: Repository detail shows its own pull request
- **WHEN** backend and frontend have different recorded pull-request URLs and backend is selected
- **THEN** backend detail links only backend's current pull request

## Test Plan

- `INT-004`: Extend `internal/runview/historical_integration_test.go` and live-run integration coverage with complete/failed persisted state and audit fixtures containing workspace work, two explicit repositories plus pending state, nested calls/loops/sub-workflows, duplicate capture names, evidence, usage/cost, safe/unsafe PR URLs, and an implicit control. Feed the same events incrementally to live models. Run `go test ./internal/runview ./internal/liverun` and the normal `make test` phase.
- `E2E-003`: Add a bounded POSIX PTY E2E under `cmd/agent-runner` using terminal-screen reconstruction. Observe a deterministic slow two-repository run live, exercise pause/jump-to-live, repository selection/drill-down/summary, finish and inspect the saved run, then inspect an implicit control. Run it in the normal `go test ./...` CI job.

## Done When

- Explicit repository lifecycle and all nested/control/error events carry correct audit identity and prefix data; implicit/workspace records retain their old serialized shape.
- The repository fan-out boundary emits exactly one `repository_start` and terminal `repository_end` per started explicit repository, and active repository identity reaches runner, step, loop, sub-workflow, agent-call, control, PR, and root-error emission helpers.
- `repository_start` and `repository_end` are registered audit event types, are projected as container boundaries rather than steps, and terminal repository events include outcome and duration.
- Historical reconstruction combines persisted affected order/status with audit evidence, including pending never-started repositories, and presents one name-only `NodeRepository` level that is flattened for implicit runs.
- Repository rows support inline expansion, drill-in/out, breadcrumbs, scoped logs/output/evidence, status, metrics, duration, and their own PR detail without sibling leakage.
- Live event application updates repository containers immediately; auto-follow crosses repositories at the deepest active child; manual navigation remains stable; jump-to-live and nested failure focus work.
- Summary rows and totals use persisted order and aggregate workspace/repository/agent-call metrics exactly once, preserving existing unavailable/partial cost behavior.
- `run-metrics.json` adds repository fields only for explicit active repositories and remains directly groupable by external consumers.
- Breadcrumb PR links are workspace-first then repository-order, live-updating, independently safe/labelled, retained at every drill depth and summary, and measured by visible width.
- `INT-004` and `E2E-003` pass for live, saved, failed, pending, unsafe-link, duplicate-output, metrics, and implicit-control cases.
- Go changes are formatted with `make fmt`; focused and broader tests pass before handing off.
