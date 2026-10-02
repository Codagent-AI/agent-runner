# automatic-run-audit Specification

## Purpose
TBD - created by archiving change audit-step. Update Purpose after archive.
## Requirements
### Requirement: Eligible executions trigger automatic auditing

In a development-audit build with automatic auditing resumed, Agent Runner SHALL automatically launch exactly one audit run for each finalized top-level execution session of a workflow in the canonical `openspec` or `spec-driven` namespace. While automatic auditing is paused, no execution SHALL trigger an automatic audit (see "Automatic auditing is temporarily paused"). The eligibility rules below still define which executions would be audited once auditing is resumed. Successful and failed outcomes SHALL be eligible. A stopped outcome, where the user interrupted and exited a resumable run, SHALL NOT trigger an audit.

Nested sub-workflows, audit workflows, and workflows outside those namespaces SHALL NOT trigger automatic auditing. Eligibility SHALL use the resolved canonical workflow reference recorded for execution: after an optional `builtin:` prefix, the namespace segment SHALL be exactly `openspec/` or `spec-driven/`. A workflow display name, basename, project directory name, or arbitrary absolute path containing one of those words SHALL NOT independently grant eligibility. A top-level finalized execution SHALL have a nonempty execution-session identity to trigger an audit.

#### Scenario: Successful OpenSpec execution triggers audit
- **WHEN** a development-audit build with automatic auditing resumed finalizes a successful top-level `openspec` execution session and its durable evidence
- **THEN** Agent Runner launches exactly one linked audit run for that execution session

#### Scenario: Failed spec-driven execution triggers audit
- **WHEN** a development-audit build with automatic auditing resumed finalizes a failed top-level `spec-driven` execution session and its durable evidence
- **THEN** Agent Runner launches exactly one linked audit run for that execution session

#### Scenario: Paused build does not trigger audit
- **WHEN** a development-audit build with automatic auditing paused finalizes an otherwise eligible top-level execution session
- **THEN** Agent Runner does not launch an audit run for that execution session

#### Scenario: User-stopped execution does not trigger audit
- **WHEN** the user interrupts and exits an otherwise eligible execution session, finalizing it as stopped
- **THEN** Agent Runner does not launch an audit run for that execution session

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

#### Scenario: Canonical workflow reference is eligible
- **WHEN** a finalized top-level execution records `builtin:openspec/change-v1.0.yaml` or another canonical `openspec/` or `spec-driven/` reference and has an execution-session identity
- **THEN** it is eligible regardless of its display name

#### Scenario: Similar names do not grant eligibility
- **WHEN** an unrelated workflow is named `openspec` or `spec-driven`, or an arbitrary absolute workflow path merely contains such a directory segment
- **THEN** those names alone do not cause an automatic audit

#### Scenario: Incomplete session identity cannot launch audit
- **WHEN** terminal handling lacks an execution-session identity
- **THEN** it does not create an automatic linked audit

### Requirement: Reserved launches can be explicitly reconciled

A development-audit build SHALL provide `audit reconcile <source-run> --session <execution-session-id>` for an existing automatic audit reservation. It SHALL preserve the original audit identity, refuse an active source run, and never rerun the source workflow. Launching, started, and completed links SHALL be safe no-ops; failed or inconsistent reservations SHALL be rejected.

#### Scenario: Interrupted reservation is reconciled
- **WHEN** an automatic audit link is durably `reserved` after the source run is no longer active
- **THEN** explicit reconciliation launches that original audit identity

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

#### Scenario: Status reports whether rows were delivered
- **WHEN** a user or tool runs `audit status` for a source run
- **THEN** each linked audit reports an outcome of `delivered`, `pending-delivery`, `failed` with its reason, or `active`, where a completed audit with no local report is `failed`

#### Scenario: Audit appears in ordinary run history
- **WHEN** ordinary run discovery encounters a linked audit run
- **THEN** it can list or view the run safely and its audit kind and source linkage remain inspectable

#### Scenario: Production binary encounters local audit history
- **WHEN** an untagged or release binary reads a runs directory containing audit runs created by a development-audit build
- **THEN** ordinary list and view operations do not fail and no audit command or automatic capability becomes available

### Requirement: Automatic auditing is temporarily paused

Automatic post-run auditing in a development-audit build SHALL be paused exactly when the source-level automatic-audit switch is compiled as off, and resumed when it is compiled as on. The switch's current source value is off, so development-audit builds built from this change ship paused. While auditing is paused, a finalized execution SHALL NOT launch an audit run, regardless of eligibility. It SHALL also NOT create an audit lifecycle record, audit link, audit run directory, audit run state, or audit lifecycle events for that execution, and the source run's own outcome, state, and exit status SHALL be unchanged.

The pause SHALL be controlled by a single source-level switch compiled into the binary. No runtime flag, environment variable, or user or project configuration value SHALL be able to resume automatic auditing. Resuming SHALL require only flipping that switch, which restores the automatic-audit behavior defined by the other requirements of this capability without other source changes.

The pause SHALL NOT remove or disable explicit audit operations. `audit` usage output, `audit status`, `audit replay`, `audit reconcile`, `audit retry`, `audit setup`, and the internal audit entry point that replay launches SHALL keep working as their existing requirements specify. The hidden audit workflow SHALL remain available to those operations.

#### Scenario: Paused build finalizes an eligible execution
- **WHEN** a development-audit build with automatic auditing paused finalizes a successful or failed top-level `openspec` or `spec-driven` execution session
- **THEN** no audit run is launched and the source run has no audit lifecycle record, audit link, or linked audit run

#### Scenario: Paused build leaves source outcome unchanged
- **WHEN** an eligible execution finalizes in a paused development-audit build
- **THEN** the source run's terminal outcome, completion state, resumability, and process exit status are the same as they would be with no audit capability

#### Scenario: Paused build does not block intake routing
- **WHEN** a paused development-audit build completes an eligible successful source run with a frozen intake route
- **THEN** intake routing proceeds without an audit launch or audit warning

#### Scenario: Configuration cannot resume automatic auditing
- **WHEN** a paused development-audit binary reads any user or project configuration or environment
- **THEN** no value causes it to launch an automatic audit

#### Scenario: Explicit replay works while paused
- **WHEN** an operator runs `audit replay <source-run> --session <execution-session-id>` against a recorded execution in a paused development-audit build
- **THEN** Agent Runner creates and launches a linked replay audit as specified for explicit replay

#### Scenario: Audit usage remains available while paused
- **WHEN** an operator runs `agent-runner audit` with no subcommand in a paused development-audit build
- **THEN** it prints audit usage and exits as it did before the pause

#### Scenario: Existing reservations remain reconcilable while paused
- **WHEN** a source run already holds a durable `reserved` automatic audit link created before the pause
- **THEN** `audit reconcile` for that execution session behaves as specified for reconciliation

#### Scenario: Resuming restores automatic auditing
- **WHEN** the source-level switch is flipped to resume automatic auditing and the binary is rebuilt
- **THEN** eligible executions again launch exactly one automatic linked audit as specified, with no other change required

