## MODIFIED Requirements

### Requirement: Eligible executions trigger automatic auditing

In a development-audit build, Agent Runner SHALL automatically launch exactly one audit run for each finalized top-level execution session of a workflow in the canonical `openspec` or `spec-driven` namespace. Successful, failed, and stopped outcomes SHALL all be eligible.

Nested sub-workflows, audit workflows, and workflows outside those namespaces SHALL NOT trigger automatic auditing. Eligibility SHALL use the resolved canonical workflow reference recorded for execution: after an optional `builtin:` prefix, the namespace segment SHALL be exactly `openspec/` or `spec-driven/`. A workflow display name, basename, project directory name, or arbitrary absolute path containing one of those words SHALL NOT independently grant eligibility. A top-level finalized execution SHALL have a nonempty execution-session identity to trigger an audit.

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

#### Scenario: Canonical workflow reference is eligible
- **WHEN** a finalized top-level execution records `builtin:openspec/change-v1.0.yaml` or another canonical `openspec/` or `spec-driven/` reference and has an execution-session identity
- **THEN** it is eligible regardless of its display name

#### Scenario: Similar names do not grant eligibility
- **WHEN** an unrelated workflow is named `openspec` or `spec-driven`, or an arbitrary absolute workflow path merely contains such a directory segment
- **THEN** those names alone do not cause an automatic audit

#### Scenario: Incomplete session identity cannot launch audit
- **WHEN** terminal handling lacks an execution-session identity
- **THEN** it does not create an automatic linked audit
