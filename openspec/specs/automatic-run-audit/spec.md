# automatic-run-audit Specification

## Purpose
TBD - created by archiving change audit-step. Update Purpose after archive.
## Requirements
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

