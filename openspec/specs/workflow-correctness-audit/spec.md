# workflow-correctness-audit Specification

## Purpose
TBD - created by archiving change audit-step. Update Purpose after archive.
## Requirements
### Requirement: Correctness audit distinguishes Runner defects from run failures

The correctness stage SHALL examine the selected source execution for behavior attributable to Agent Runner that conflicts with the audit-launch snapshot of the injected Agent Runner checkout, specifications, or workflow intent. It SHALL inspect relevant launch-time repository context before confirming a current defect and SHALL distinguish Runner defects from expected workflow behavior, user or project errors, external dependency failures, and unsupported suspicions. Build-time provenance SHALL be retained as context, but a build-to-launch mismatch SHALL NOT by itself block a finding verified against the launch-time snapshot.

#### Scenario: Run failure is caused by project code
- **WHEN** source evidence shows that a workflow step failed because the audited project did not compile
- **THEN** the correctness audit does not classify that failure alone as an Agent Runner defect

#### Scenario: Orchestration behavior conflicts with current contract
- **WHEN** run evidence and current Agent Runner code or specifications support a reproducible orchestration defect
- **THEN** the correctness audit may confirm the defect

#### Scenario: Evidence remains inconclusive
- **WHEN** the auditor cannot verify a suspicion after inspecting relevant evidence
- **THEN** it retains the suspicion only in the local report and does not file an issue

### Requirement: Confirmed new defects create focused GitHub issues

For each confirmed, non-duplicate Agent Runner defect, the correctness stage SHALL create one GitHub issue in `Codagent-AI/agent-runner`. Each created issue title SHALL begin with `[auto-audit]`. Each issue SHALL describe observed and expected behavior, affected run context, reproduction or verification guidance, and concise evidence sufficient for a maintainer to investigate.

Issue content SHALL redact credentials, secret-like values, private URLs, and identifying local paths. It SHALL avoid full transcripts, large command outputs, source dumps, or other unnecessary detailed evidence. The local audit report SHALL retain the relationship between the finding and the created issue.

#### Scenario: New defect is confirmed
- **WHEN** repository inspection and source-run evidence confirm a defect and no open duplicate exists
- **THEN** the correctness stage files one focused issue in `Codagent-AI/agent-runner` with an `[auto-audit]` title prefix and records its URL locally

#### Scenario: Several symptoms share one cause
- **WHEN** multiple observed symptoms are verified as manifestations of the same defect
- **THEN** the correctness stage files one issue describing the related symptoms rather than one issue per symptom

#### Scenario: Issue evidence contains sensitive material
- **WHEN** useful local evidence contains a credential, private URL, or identifying local path
- **THEN** the issue uses a redacted or generalized description and leaves the detailed evidence local

### Requirement: Open duplicates are not recreated or updated

Before filing a confirmed defect, the correctness stage SHALL search open issues in `Codagent-AI/agent-runner` for the same underlying problem. When an open duplicate exists, it SHALL skip issue creation, SHALL NOT automatically comment on or modify the existing issue, and SHALL record the existing issue URL in the local audit report.

#### Scenario: Open duplicate exists
- **WHEN** a confirmed defect matches an open issue for the same underlying problem
- **THEN** no new issue or comment is created and the existing issue URL is recorded locally

#### Scenario: Similar issue has a different cause
- **WHEN** an open issue has similar symptoms but repository evidence shows a materially different underlying defect
- **THEN** the correctness stage may file a separate focused issue that distinguishes the causes

#### Scenario: Matching issue is closed and defect has recurred
- **WHEN** the only matching issue is closed and current evidence confirms the defect still occurs
- **THEN** the correctness stage may file a new issue that references the closed issue as prior history

### Requirement: Correctness audit never modifies the repository

The correctness stage SHALL use read-only repository access for verification. It SHALL NOT edit files, make commits, apply fixes, alter workflow configuration, or treat issue creation as authorization for implementation.

#### Scenario: Fix appears straightforward
- **WHEN** the auditor identifies an apparent code fix for a confirmed defect
- **THEN** it may describe the likely area in the issue but does not modify the repository

#### Scenario: Issue filing fails
- **WHEN** a confirmed new defect cannot be filed because GitHub is unavailable or authorization is insufficient
- **THEN** the local report retains the confirmed finding and filing failure, and the source workflow outcome remains unchanged

