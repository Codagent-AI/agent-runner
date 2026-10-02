## ADDED Requirements

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

## MODIFIED Requirements

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
