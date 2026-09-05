# Task: Add the development-only linked audit lifecycle and replay commands

## Goal

Add a private `dev_audit` provider and generic post-finalization hook that reserve, snapshot, launch, link, inspect, and replay audit runs without blocking or changing their source runs. Supported local builds include the hidden capability and injected Agent Runner checkout provenance; ordinary and release builds contain no command, workflow asset, hook, setting, or dormant implementation.

## Background

You MUST read:

- `openspec/changes/audit-step/proposal.md` for the development-only, automatic, linked-run, non-recursive, replayable product intent
- `openspec/changes/audit-step/design.md` sections 1, 2, 5, and 9 for the provider boundary, launch-time source snapshot, durable reservation state machine, detached self-exec, finalization ownership, hidden workflow injection, reciprocal links, status/replay surface, and intake ordering
- `openspec/changes/audit-step/specs/development-audit-availability/spec.md` for build exclusion, automatic activation, profile and source provenance
- `openspec/changes/audit-step/specs/automatic-run-audit/spec.md` for eligibility, source authority, TUI/headless independence, resume lineage, transition ordering, and inspectability
- `openspec/changes/audit-step/specs/audit-log-lifecycle/spec.md` for source/audit lifecycle events and durability ordering
- `openspec/changes/audit-step/specs/run-audit-replay/spec.md` for explicit historical execution-session selection and snapshot immutability
- `openspec/changes/audit-step/test-plan.md` for `INT-001` and `INT-002`

The generic hook belongs in `internal/runner/` and must not import development-only audit, Google, GitHub, or workflow concerns. The tagged provider and its embedded hidden asset live under `internal/devaudit/`, with build-constrained tagged and inert untagged implementations. Create exactly one production asset at `internal/devaudit/workflows/audit/run-audit-v1.0.yaml`, with canonical identity `audit:run-audit`, `hidden: true`, and the seven ordered stage contracts from design section 5. This task owns that stable asset identity/topology and the injection mechanism; command handlers and model prompts may be implemented against those contracts without creating a second workflow asset.

Integrate command registration in `cmd/agent-runner/`; workflow loading/catalog injection through `workflows/`, `internal/loader/`, `internal/discovery/`, and `internal/workflowcatalog/`; run discovery through `internal/runs/`; persistent state through `internal/model/` and `internal/stateio/`; locking through `internal/runlock/`; and post-run ordering around `runner.ExecuteFromHandle` plus `cmd/agent-runner/main.go`'s frozen-intake launch. The injected asset path must satisfy `workflowcatalog.RequiredFilenamePattern`, namespace grouping, loader parsing, and ordinary hidden-workflow discovery behavior.

`Makefile` and `dev.sh` must add `-tags dev_audit` and linker variables for the absolute checkout root, build revision, and dirty state. `.goreleaser.yaml` must remain untagged. Add a dedicated tagged job to `.github/workflows/ci.yml` for build, tests, formatting, lint, `gosec`, and `govulncheck`. Tests must build temporary binaries and must not overwrite `bin/agent-runner`.

The coordinator must wait only for successful child process creation, never audit completion. Use `exec.Cmd` directly with the current executable, explicit argv, redirected streams, and Darwin/Linux detachment adapters; do not invoke a shell. Reserve by source run ID plus execution-session ID before releasing finalization ownership, persist `reserved`/`started`/`failed`, reuse reservations after duplicate terminal handling, and ensure audit-kind runs cannot recurse. The immutable launch snapshot and launch-time Agent Runner checkout snapshot must be complete before the source can resume. Missing `crosscheck` resolution is an audit diagnostic, not a source failure; freeze the resolved profile name and actual CLI/model/reasoning-effort provenance in the audit request.

`AT-002` is exercised during change acceptance, not by this implementation task. Automated lifecycle coverage is `INT-002`; do not substitute raw PTY scraping or assign the acceptance flow to the implementor.

## Spec

### Requirement: Audit capability exists only in development-audit builds

Agent Runner SHALL compile the automatic audit hook, hidden workflow asset, replay and setup commands, and concrete audit integrations only into a development-audit build produced by the repository's supported local build paths. `make build` and `dev.sh` SHALL produce development-audit builds. Release and ordinary untagged builds SHALL NOT register an audit command, inject an audit workflow, or provide a runtime setting that can enable the capability.

#### Scenario: Local Make build is used
- **WHEN** the operator builds Agent Runner through `make build`
- **THEN** the resulting binary contains the development audit capability and automatically audits eligible executions

#### Scenario: Local development script is used
- **WHEN** the operator runs Agent Runner through `dev.sh`
- **THEN** the resulting process contains the same development audit capability

#### Scenario: Production release is built
- **WHEN** Agent Runner is built through the production release process without the development audit tag
- **THEN** its commands, workflow catalog, and runtime lifecycle contain no audit option

#### Scenario: Runtime configuration attempts to enable production audit
- **WHEN** an untagged binary reads user or project configuration
- **THEN** no configuration value can enable the absent audit capability

### Requirement: Development auditing needs no enablement setting

A development-audit build SHALL automatically attempt to audit every eligible finalized execution. Audit enablement, model selection, and an Agent Runner repository path SHALL NOT be added to layered user or project configuration.

Both model stages SHALL resolve the existing `crosscheck` role using the profile-set name recorded by the source run and the profile configuration available at audit launch. The audit SHALL freeze the resolved CLI, model, and reasoning-effort provenance that it actually invokes. It SHALL NOT claim that a profile-set name alone reproduces an earlier resolved agent definition. Failure to resolve that agent SHALL fail or degrade only the linked audit and SHALL NOT alter the source execution.

#### Scenario: Eligible local execution completes
- **WHEN** an eligible workflow execution is finalized by a development-audit build
- **THEN** Agent Runner attempts to launch the linked audit without consulting an enablement setting

#### Scenario: Source profile resolves crosscheck
- **WHEN** the source run's recorded profile-set name resolves a valid `crosscheck` agent at audit launch
- **THEN** both model audit stages use that resolved definition and the audit freezes its actual CLI, model, and effort provenance

#### Scenario: Source profile cannot resolve crosscheck
- **WHEN** the source run's recorded profile-set name cannot resolve `crosscheck` at audit launch
- **THEN** the audit records a diagnostic failure and preserves the source result

### Requirement: The build identifies its Agent Runner source

The supported development build paths SHALL inject the absolute Agent Runner checkout root and available build Git provenance into the tagged binary. At audit launch, Agent Runner SHALL verify that root, record its then-current revision and dirty state, and create a read-only snapshot for correctness verification. That launch-time snapshot SHALL be authoritative for whether the suspected behavior is a current Agent Runner defect. Build-time provenance SHALL remain diagnostic context and SHALL NOT require a separate repository setting.

Before correctness publication, Agent Runner SHALL verify that the launch-time snapshot identifies the Agent Runner module. Missing, wrong-module, or incomplete launch-time source SHALL reduce coverage and prevent issue creation unless other matching repository evidence independently verifies the finding. A difference between build-time and launch-time provenance SHALL be recorded and MAY reduce confidence, but SHALL NOT by itself block publication of a defect verified against the authoritative launch-time snapshot.

#### Scenario: Local binary runs from another project
- **WHEN** a development-audit binary built in the Agent Runner checkout audits a workflow in another project
- **THEN** the correctness stage inspects the injected Agent Runner checkout rather than the audited project's repository as Runner source

#### Scenario: Two machines use different checkouts
- **WHEN** development binaries are built from different absolute checkout paths on two machines
- **THEN** each binary uses the path and provenance injected by its own build

#### Scenario: Build checkout moved or removed
- **WHEN** the injected checkout path cannot be verified at audit time
- **THEN** the audit records unavailable Runner source coverage and does not file a source-verified issue from that evidence

#### Scenario: Checkout changed after build
- **WHEN** the current checkout revision or dirty state materially differs from the recorded build provenance
- **THEN** the discrepancy is retained locally while the verified audit-launch snapshot remains authoritative for current-defect assessment

#### Scenario: Launch snapshot is not Agent Runner source
- **WHEN** the injected path resolves at launch but its snapshot does not identify the Agent Runner module
- **THEN** correctness publication is blocked unless independent matching repository evidence verifies the finding

### Requirement: Eligible executions trigger automatic auditing

In a development-audit build, Agent Runner SHALL automatically launch exactly one audit run for each finalized top-level execution session of a workflow in the canonical `openspec` or `spec-driven` namespace. Successful, failed, and stopped outcomes SHALL all be eligible.

Nested sub-workflows, audit workflows, and workflows outside those namespaces SHALL NOT trigger automatic auditing.

#### Scenario: Successful OpenSpec execution triggers audit
- **WHEN** a development-audit build finalizes a successful top-level `openspec` execution session and its durable evidence
- **THEN** Agent Runner launches exactly one linked audit run for that execution session

#### Scenario: Failed spec-driven execution triggers audit
- **WHEN** a development-audit build finalizes a failed top-level `spec-driven` execution session and its durable evidence
- **THEN** Agent Runner launches exactly one linked audit run for that execution session

#### Scenario: Stopped eligible execution triggers audit
- **WHEN** a development-audit build finalizes the durable evidence for a stopped eligible execution session
- **THEN** Agent Runner launches exactly one linked audit run for that execution session

#### Scenario: Production build launches nothing
- **WHEN** an eligible execution session reaches a terminal outcome in an untagged or release build
- **THEN** Agent Runner does not launch an audit run

#### Scenario: Nested sub-workflow does not trigger independent audit
- **WHEN** an OpenSpec or spec-driven workflow executes as a nested sub-workflow
- **THEN** its completion does not independently launch an audit run

#### Scenario: Audit workflow does not recurse
- **WHEN** an audit workflow reaches a terminal outcome
- **THEN** it does not launch another audit workflow

#### Scenario: Unrelated workflow does not trigger audit
- **WHEN** a top-level workflow outside the `openspec` and `spec-driven` namespaces reaches a terminal outcome
- **THEN** Agent Runner does not automatically launch an audit run

#### Scenario: Duplicate terminal handling is idempotent
- **WHEN** the same source execution session's terminal state is handled more than once
- **THEN** no more than one automatic audit run is launched for that execution session

### Requirement: Audit launch is independent of the run view

Agent Runner SHALL launch the audit asynchronously as soon as the source outcome and durable evidence are finalized. Launch SHALL NOT wait for the user to exit the source run view. The audit SHALL execute as a separate linked headless run and SHALL continue independently if the source run view exits.

#### Scenario: Completion view remains available during audit
- **WHEN** a source workflow completes while its live run view remains open
- **THEN** the linked audit starts without closing or replacing the source completion view

#### Scenario: Audit continues after source view exits
- **WHEN** the user exits the source run view after the linked audit starts
- **THEN** the audit continues independently

#### Scenario: Headless source execution triggers audit
- **WHEN** an eligible source workflow runs headlessly and reaches a terminal outcome
- **THEN** Agent Runner launches the linked headless audit without requiring a TUI

#### Scenario: Active audit does not replace displayed source outcome
- **WHEN** the linked audit remains active after the source execution completes
- **THEN** inspection of the source run continues to show the source workflow's terminal outcome

### Requirement: Source outcome remains authoritative

Audit launch, execution, correctness issue filing, and dataset reporting SHALL NOT alter the source workflow's outcome, completion state, resumability, or process exit status. Failures in post-run auditing SHALL be retained as warnings associated with the audit lifecycle.

For this delivery unit, implement the source-isolation and lifecycle-state portion for every failure class: the post-finalization API must permit audit completion, reporting warnings, and correctness-publication failures to be appended to the linked audit/source metadata without rewriting the source outcome or exit status. Concrete Google delivery and GitHub issue mutation are outside this task's implementation surface.

#### Scenario: Audit failure does not fail successful source
- **WHEN** the source execution succeeds and its linked audit fails
- **THEN** the source remains successful and its process exit status remains successful

#### Scenario: Successful audit does not complete failed source
- **WHEN** the source execution fails and its linked audit succeeds
- **THEN** the source remains failed and resumable

#### Scenario: Audit launch failure is non-blocking
- **WHEN** the development-audit coordinator cannot launch the injected audit
- **THEN** it records a warning without replacing the source outcome or exit status

#### Scenario: Dataset failure is non-blocking
- **WHEN** the audit cannot write its value observations to the configured dataset
- **THEN** the source outcome remains unchanged and the local audit result remains available

#### Scenario: Correctness issue filing failure is non-blocking
- **WHEN** the correctness audit cannot create a GitHub issue for a confirmed defect
- **THEN** the source outcome remains unchanged and the filing failure is retained with the audit result

### Requirement: Resumed executions remain distinguishable

Each resumed Agent Runner invocation SHALL constitute a distinct execution session eligible for its own audit. Audit linkage SHALL identify both the stable source run and the execution session that triggered the audit. Later audits SHALL retain lineage to earlier execution sessions so downstream reporting can recognize overlapping or superseded evidence.

#### Scenario: Resumed run creates distinct audits
- **WHEN** a failed source run is audited, resumed, and later succeeds
- **THEN** each finalized execution session has a separately linked audit under the same source run identity

#### Scenario: Revisited steps are marked as overlapping
- **WHEN** a resumed execution session revisits steps already covered by an earlier audit
- **THEN** the later audit identifies the overlapping evidence rather than presenting those step observations as an independent sample

#### Scenario: Linked audits distinguish execution sessions
- **WHEN** two audits belong to different execution sessions of the same source run
- **THEN** both retain the source run identity and distinct execution-session identities

### Requirement: Post-run transitions coexist

When an eligible successful source run also has a frozen intake route, Agent Runner SHALL launch the audit before transferring foreground ownership to the intake route. Audit launch failure SHALL NOT prevent the intake route from proceeding.

#### Scenario: Audit launches before intake route
- **WHEN** a development-audit build completes an eligible successful source run with a frozen intake route
- **THEN** Agent Runner launches the linked audit and then allows intake routing to proceed

#### Scenario: Failed audit launch does not block intake route
- **WHEN** audit launch fails before a frozen intake route is launched
- **THEN** Agent Runner records the audit warning and still allows intake routing to proceed

#### Scenario: Audit does not claim foreground without intake route
- **WHEN** an eligible source run has no frozen intake route
- **THEN** the asynchronous audit launch does not introduce a new foreground transition

### Requirement: Audit relationship is inspectable

The automatic audit workflow SHALL be hidden from ordinary workflow discovery. Each audit run SHALL durably identify its source run and execution session so their relationship can be inspected later.

The initial capability SHALL require no new TUI placement, navigation, or separate audit-run storage hierarchy. Linked audit runs MAY appear in ordinary run history, where their explicit run kind and reciprocal linkage SHALL distinguish them from source runs. Existing list and view paths, including those in production binaries that encounter run data created by a development build, SHALL tolerate these entries without exposing launch or replay capability. The existing source run view SHALL continue to show the source completion state.

#### Scenario: Audit workflow is hidden from ordinary discovery
- **WHEN** a user browses ordinary launchable workflows without revealing hidden workflows
- **THEN** the automatic audit workflow is not listed

#### Scenario: Audit identifies its source
- **WHEN** a user or tool inspects an audit run
- **THEN** the source run and triggering execution session can be identified

#### Scenario: Source identifies launched audit
- **WHEN** a user or tool inspects persisted source-run evidence after audit launch
- **THEN** the linked audit run can be identified

#### Scenario: Source run view remains unchanged
- **WHEN** a linked audit exists for a completed source run
- **THEN** the existing source run view may remain focused on source completion without embedding the audit workflow's steps

#### Scenario: Audit appears in ordinary run history
- **WHEN** ordinary run discovery encounters a linked audit run
- **THEN** it can list or view the run safely and its audit kind and source linkage remain inspectable

#### Scenario: Production binary encounters local audit history
- **WHEN** an untagged or release binary reads a runs directory containing audit runs created by a development-audit build
- **THEN** ordinary list and view operations do not fail and no audit command or automatic capability becomes available

### Requirement: Source and audit lifecycle events are linked

Agent Runner SHALL durably record audit launch requested, audit launched, audit launch failed, audit completed, and reporting warning events as applicable. A successful launch SHALL link the source run and execution session to the audit run, and the audit run SHALL contain the reciprocal source identifiers.

Audit launch SHALL occur only after the source terminal outcome, metrics, step checkpoints, and other required durable evidence have been flushed.

#### Scenario: Audit launch succeeds
- **WHEN** an eligible execution session is finalized and its linked audit starts
- **THEN** durable lifecycle evidence links the source run and execution session to the audit run in both directions

#### Scenario: Audit launch fails
- **WHEN** Agent Runner cannot start the audit
- **THEN** the source audit log records the failed launch and reason without changing the source outcome

#### Scenario: Audit completes with reporting warning
- **WHEN** model auditing completes but Google Sheets reporting fails
- **THEN** the audit lifecycle records its completion and reporting warning separately

#### Scenario: Source evidence is not yet durable
- **WHEN** the source reaches a terminal outcome but required evidence has not finished flushing
- **THEN** Agent Runner does not launch the audit until finalization succeeds or records a non-blocking launch warning if it cannot be finalized

### Requirement: Recorded executions can be audited explicitly

The development-audit build SHALL provide an explicit audit invocation that accepts a recorded source run and audits its durable evidence without rerunning or resuming the source workflow. Untagged and release builds SHALL NOT provide this invocation.

When the source run contains more than one execution session, the invocation SHALL require or resolve an unambiguous execution session. It MUST NOT silently combine sessions into one observation.

#### Scenario: Historical execution is audited without rerun
- **WHEN** a user explicitly requests an audit of a recorded source run and execution session
- **THEN** Agent Runner launches the same hidden audit workflow used for automatic auditing without executing the source workflow again

#### Scenario: Replay is absent from production
- **WHEN** an untagged or release binary is invoked
- **THEN** no audit replay command is registered

#### Scenario: Session selection is ambiguous
- **WHEN** a recorded run contains multiple execution sessions and the replay request does not identify one unambiguously
- **THEN** Agent Runner declines to start the audit and identifies the available execution sessions

#### Scenario: Source evidence is unavailable
- **WHEN** the requested run or execution session cannot be resolved to durable evidence
- **THEN** the explicit audit exits with a diagnostic and does not mutate the source run

### Requirement: Replays produce distinct append-only observations

Every explicit replay SHALL have a distinct audit-run identity and SHALL mark its value observations as replay-generated. Replaying an already audited execution SHALL append a new observation set rather than overwriting the earlier local report or dataset rows.

For this delivery unit, implement replay identity and lifecycle semantics: each replay reserves a new audit-run ID and `replay` trigger against one selected source execution session, while report-retry launch metadata reuses the existing audit-run ID and never starts a replay. Observation projection and external row delivery are outside this task's implementation surface.

#### Scenario: Previously audited execution is replayed
- **WHEN** an execution session with an existing audit is explicitly audited again
- **THEN** the new audit and its step observations have a new audit-run identity, identify the replay trigger, and preserve the earlier observations

#### Scenario: Retried reporting is not a new replay
- **WHEN** reporting is retried for an existing completed audit report
- **THEN** Agent Runner retries the same observation identities rather than creating a new audit or new observations

### Requirement: Replay is non-mutating

Explicit audit replay SHALL create and use an immutable launch-time snapshot of the selected durable source-run evidence and SHALL treat the source repository as read-only. It SHALL NOT alter the source run's state, outcome, resumability, commits, working tree, or recorded evidence. Later source-run changes SHALL NOT invalidate or silently enter an already launched replay.

#### Scenario: Replay completes
- **WHEN** an explicit replay succeeds or fails
- **THEN** the source run and source repository remain unchanged by the audit

#### Scenario: Source run changes during replay
- **WHEN** the selected source run gains later evidence after the replay's launch snapshot is complete
- **THEN** the replay continues against its snapshot and reports only the selected evidence version

## Test Plan

- `INT-001: Development and production build boundary`: build tagged and untagged temporary binaries; verify local tag/provenance injection, one hidden asset, tagged-only command routing, inert production configuration, binary marker absence, and safe list/view of tagged audit history by an untagged binary.
- `INT-002: Durable post-finalization launch and process independence`: exercise success, failure, stopped, duplicate terminal, launch-error, audit-kind, unrelated, nested, and frozen-intake cases against real persistence and a helper child. Prove evidence is flushed before reservation/snapshot, one reservation is reused, reciprocal IDs agree, source semantics remain authoritative, detachment survives parent completion, and audit launch attempt precedes but never blocks intake transfer.

## Done When

- `internal/runner/` exposes a generic read-only post-finalization summary/hook after durable terminal state, metrics, events, and checkpoints have flushed; its default is inert and it has no audit-specific imports.
- Tagged and untagged provider implementations compile separately; only the tagged provider embeds and injects `internal/devaudit/workflows/audit/run-audit-v1.0.yaml`, supplies audit/status/replay/internal launch commands, post-finalization hook, integration-command registration boundary, and injected build source provenance. The asset has canonical identity `audit:run-audit`, `hidden: true`, and exactly the seven ordered stage IDs/contracts from design section 5.
- Automatic eligibility is exactly top-level canonical `openspec` or `spec-driven` executions with successful, failed, or stopped outcomes; nested workflows, unrelated workflows, and audit-kind runs never launch an audit.
- Reservation, immutable evidence/source snapshots, reciprocal source/audit links, lifecycle states, and detached self-exec are durable and idempotent per source run plus execution session. Snapshot completion occurs while finalization ownership still prevents resume races.
- TUI and headless callers receive the original source outcome/exit status without waiting for audit completion; the source completion view remains source-focused; audit launch attempt precedes a frozen intake handoff and cannot block it.
- Explicit replay requires an unambiguous execution session, creates a new audit identity, never reruns/resumes/mutates source work, and status/ordinary list/view paths safely expose only persisted identity and linkage.
- `Makefile`, `dev.sh`, `.goreleaser.yaml`, and `.github/workflows/ci.yml` enforce the tagged-local/untagged-production boundary and tagged build/test/lint/security coverage.
- The single asset names all seven final stage contracts without adding silent skips or temporary success behavior. Until a stage handler exists, an accidental invocation fails only the linked audit with an explicit diagnostic and cannot affect the source; focused intermediate workflow tests execute through the last available stage using Runner's existing bounded-run/test controls rather than pretending the remaining stages succeeded.
- Focused tests plus `INT-001` and `INT-002` pass on supported Darwin/Linux behavior, `make fmt` passes, and the normal untagged suite remains green.
