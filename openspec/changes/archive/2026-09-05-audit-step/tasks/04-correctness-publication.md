# Task: Verify Runner defects, publish deduplicated GitHub issues, and assemble local reports

## Goal

Implement the correctness half of the audit workflow and its stage-6 local-report boundary: a separate `crosscheck` model may investigate Runner behavior against immutable run evidence and the authoritative launch-time Agent Runner source snapshot, but only validated, redacted, confirmed, non-duplicate defect candidates may create one idempotent issue in `Codagent-AI/agent-runner`. After correctness validation/publication reaches a durable outcome, atomically assemble the complete local report before any Google Sheets delivery.

## Background

You MUST read:

- `openspec/changes/audit-step/proposal.md` for the strict separation between informational value records and actionable correctness findings
- `openspec/changes/audit-step/design.md` sections 4, 5, 6, 8, and 9 for source-snapshot authority, structured correctness candidates, model/deterministic responsibility, stable finding markers, semantic and exact duplicate search, redaction, retry behavior, repository non-mutation, stage-6 report assembly, delivery state, and replay identities
- `openspec/changes/audit-step/specs/workflow-correctness-audit/spec.md` for classification, publication, duplicate, and read-only requirements
- `openspec/changes/audit-step/specs/development-audit-availability/spec.md` requirement “The build identifies its Agent Runner source” for the publication coverage gate
- `openspec/changes/audit-step/specs/audit-evidence-preparation/spec.md` requirement “Auditors may inspect detailed local evidence selectively” for consultation and locality rules
- `openspec/changes/audit-step/test-plan.md` for `INT-007`

Implement correctness schemas, validation/redaction, finding state, and publication in the tagged audit package under `internal/devaudit/`; integrate the deterministic publisher with the existing authenticated `gh` executable boundary rather than adding a GitHub credential or SDK configuration. Use an explicit command runner with fixed repository targeting and test it with a stateful fake executable that records argv/stdin. The model runs from the audit workspace, has read-only evidence/source access, and writes only candidate JSON. It must not receive a repository working directory or any code-editing authority.

Complete stages 4, 5, and 6 in the existing `internal/devaudit/workflows/audit/run-audit-v1.0.yaml` asset in place. Preserve canonical identity `audit:run-audit`, `hidden: true`, the seven-stage order, and all established stage IDs; do not create or embed a second audit workflow. Focused workflow tests execute through stage 6 and stop before external delivery.

The candidate contract must cover observed and expected behavior, reproducibility/verification guidance, affected component, evidence references, confidence, semantic duplicate result, and normalized defect key. Deterministic code must validate provenance and evidence references, reject unsafe or unconfirmed content, redact credentials/private URLs/identifying local paths, group symptoms sharing a cause, derive a stable finding ID and hidden body marker, search both semantic candidates and the exact marker, and persist all outcomes before/after a `gh` call. An open duplicate is linked without issue/comment mutation. A closed match may be referenced only for a currently verified recurrence. Ambiguous or failed creation must be retryable and exact-marker-safe.

When correctness processing finishes or degrades, atomically assemble the complete versioned local report from deterministic evidence/value artifacts plus correctness findings, duplicate links, created issue URLs, publication failures, consultation ledger, fingerprints, model provenance, and automatic/replay identities. The report schema owns an optional destination identity (`spreadsheet_id` and worksheet `tab`), `schema_version: step_value_v1`, and an explicit unavailable/unusable destination state. Populate those fields at assembly time through a small destination-resolver interface; this task supplies fake configured, absent, and unusable resolvers for report tests, while the concrete protected connection-record reader plugs into that interface. Secrets remain outside the resolver result and report. Preserve partial diagnostics when a model output is absent. The committed report is the only input to external Sheets delivery and must exist before that delivery begins.

Do not execute agent acceptance flow `AT-004`; implementation and automated testing must use fake candidates and fake `gh` only. Never manufacture or publish a real issue during development or CI.

## Spec

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

### Shared publication gate: authoritative Runner source

At audit launch, Agent Runner SHALL verify the injected checkout root, record its then-current revision and dirty state, and create a read-only snapshot for correctness verification. That launch-time snapshot SHALL be authoritative for whether the suspected behavior is a current Agent Runner defect. Missing, wrong-module, or incomplete launch-time source SHALL reduce coverage and prevent issue creation unless other matching repository evidence independently verifies the finding. A difference between build-time and launch-time provenance SHALL be recorded and MAY reduce confidence, but SHALL NOT by itself block publication of a defect verified against the authoritative launch-time snapshot.

#### Scenario: Build checkout moved or removed
- **WHEN** the injected checkout path cannot be verified at audit time
- **THEN** the audit records unavailable Runner source coverage and does not file a source-verified issue from that evidence

#### Scenario: Checkout changed after build
- **WHEN** the current checkout revision or dirty state materially differs from the recorded build provenance
- **THEN** the discrepancy is retained locally while the verified audit-launch snapshot remains authoritative for current-defect assessment

#### Scenario: Launch snapshot is not Agent Runner source
- **WHEN** the injected path resolves at launch but its snapshot does not identify the Agent Runner module
- **THEN** correctness publication is blocked unless independent matching repository evidence verifies the finding

## Test Plan

- `INT-004: Evidence preparation and model-output containment` (correctness/report portion): validate adversarial correctness outputs and consultation references against immutable evidence/provenance; prove fabricated measured fields, unsafe content, unknown references, and frozen-snapshot mutation cannot reach GitHub or Sheets; and prove stage-6 report assembly captures every value/correctness field plus configured or explicitly unavailable destination identity only after correctness publication state is durable.
- `INT-007: GitHub issue publication and deduplication contract`: feed confirmed, inconclusive, open-duplicate, closed-recurrence, multi-symptom, unsafe-content, ambiguous, and failed candidates through the same stateful fake-`gh` boundary used by the tagged command. Prove only validated confirmed non-duplicates target `Codagent-AI/agent-runner`; titles and stable markers are correct; bodies are focused and redacted; symptoms sharing a cause create one issue; open duplicates receive no issue/comment; recurrence and retry rules are idempotent; failures remain local; value output cannot invoke the publisher; and both repositories retain identical Git state.

## Done When

- The correctness model contract distinguishes project/user/external failures and inconclusive suspicions from reproducible Runner defects verified against current specifications and the authoritative launch-time source snapshot.
- Candidate validation requires confirmed status, normalized defect key, supported evidence references, adequate source provenance, safe bounded content, and a model-supplied semantic duplicate result before publication is even considered.
- The deterministic publisher derives a stable finding ID/hidden marker, redacts prohibited material, groups same-cause symptoms, searches semantic candidates and exact markers, and performs at most one `gh issue create` against exactly `Codagent-AI/agent-runner` for each confirmed new defect.
- Open duplicates are linked locally without issue or comment mutation; closed recurrence is explicit; ambiguous/failed calls remain retryable and exact-marker retries cannot duplicate an issue.
- After correctness publication succeeds, skips, or fails, the complete local report is atomically committed before external Sheets delivery and includes deterministic evidence/value state, consultations, fingerprints, actual judge provenance, finding/duplicate/publication/URL/failure state, `step_value_v1`, a resolver-supplied frozen destination or explicit unavailable/unusable state, and stable automatic/replay observation identities.
- Stages 4–6 are wired into the existing `internal/devaudit/workflows/audit/run-audit-v1.0.yaml` asset; no second audit workflow asset is created or embedded.
- Pre/post Git status and HEAD tests prove the audited project and Agent Runner checkout are never edited or committed; the value stage has no path to the publisher.
- The correctness/report portion of `INT-004`, `INT-007`, focused tagged tests, `make fmt`, and the fake-`gh` contract suite pass without any real GitHub authentication or mutation.
