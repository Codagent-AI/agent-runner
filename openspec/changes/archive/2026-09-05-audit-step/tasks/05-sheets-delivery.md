# Task: Import Google credentials, deliver Sheets rows, and prove complete CLI journeys

## Goal

Complete the development audit by importing compatible Google OAuth material into protected Runner-owned storage, projecting only the exact `step_value_v1` allowlist to one existing worksheet, making delivery append-only and retry-safe, and adding stable tagged CLI end-to-end coverage for automatic audit, resume, replay, and report retry.

## Background

You MUST read:

- `openspec/changes/audit-step/proposal.md` for the deliberately narrow one-operator Google Sheets experiment, local-first reports, and prohibition on external detailed evidence
- `openspec/changes/audit-step/design.md` sections 5–7 and 9 for report assembly, exact header/order, project sanitization, OAuth import, protected connection state, direct Sheets REST calls, locks, observation-ID deduplication, destination freezing, replay, and retry semantics
- `openspec/changes/audit-step/specs/development-audit-availability/spec.md` requirement “Google connection state is private and does not control auditing”
- `openspec/changes/audit-step/specs/lightweight-audit-reporting/spec.md` for OAuth, external projection, exact schema, append/retry, and non-blocking failure behavior
- `openspec/changes/audit-step/specs/automatic-run-audit/spec.md`, `openspec/changes/audit-step/specs/run-audit-replay/spec.md`, and `openspec/changes/audit-step/specs/workflow-value-observation/spec.md` for the complete automatic/resume/replay journeys that delivery closes
- `openspec/changes/audit-step/test-plan.md` for `INT-005`, `INT-006`, `E2E-001`, and `E2E-002`

Keep the tagged setup/retry command wiring in `cmd/agent-runner/` and the concrete credential store, destination resolver, OAuth client, worksheet projection, delivery lock/state, and retry logic under `internal/devaudit/`. Reuse `internal/stateio.WriteJSONAtomic` or an equally safe atomic pattern, but enforce the stricter contract: protected parent storage under `~/.agent-runner/` is mode `0700` and the development-audit connection record is mode `0600`. This record is integration state, not `internal/config` layered project/user configuration or `internal/usersettings` enablement state. Its reader implements the stage-6 destination-resolver contract so report assembly freezes spreadsheet ID/tab plus `step_value_v1`, or an explicit unavailable/unusable state, without copying secrets into the report.

Complete stage 7 in the existing `internal/devaudit/workflows/audit/run-audit-v1.0.yaml` asset in place. Preserve canonical identity `audit:run-audit`, `hidden: true`, the seven-stage order, and all established stage IDs; do not create or embed a second audit workflow.

During implementation, confirm the actual compatible installed-application client and authorized-user token JSON shapes from the operator's existing Google credential material. Treat that as input-format compatibility work inside this outcome: validate required fields and scopes, copy only refresh-capable values, and do not retain source paths or add a Hermes runtime/code dependency. No generalized sink interface, spreadsheet creation, interactive formatting, hosted service, or destination setting belongs in scope.

Use direct OAuth2 and Sheets REST calls through injectable HTTP transports. Supply the protected connection-record resolver consumed during stage-6 report assembly, then resolve OAuth secrets only during delivery against the already-frozen spreadsheet ID/tab and `step_value_v1` schema in the completed report. Validate the exact ordered header and never repair it. Serialize reporters per spreadsheet/tab with a user-scoped lock. Read existing `observation_id` values before append and again after ambiguous responses. A report retry reuses the same observations and never calls either model; a replay has a new audit ID and new observations.

Build every row from an explicit allowlist, not report serialization. Never export transcripts or summaries, prompts/responses, tool calls, command output, source/diffs, artifact contents, evidence excerpts/references, paths/filenames, private URLs, secrets, or unknown local fields. Unknown numeric values are blank. Reject an unsafe note rather than sending it. `project` is a sanitized hosting `owner/repository` slug or only the repository-root basename.

Automated tests must use isolated homes, synthetic credentials, controlled OAuth/Sheets servers, deterministic fake `crosscheck` agents, and fake GitHub endpoints/executables. They must not access the operator's Google or GitHub accounts. Do not execute `AT-003`; live credential compatibility remains agent acceptance outside implementation.

## Spec

### Requirement: Google connection state is private and does not control auditing

The development-audit build SHALL provide a one-time setup operation that imports compatible Google OAuth client and authorized-user token material together with an existing spreadsheet ID and worksheet tab into protected Agent Runner user storage. This record SHALL be integration state rather than layered user or project configuration, and its presence SHALL NOT enable or disable automatic auditing.

Missing, malformed, incomplete, or unusable Google connection state SHALL prevent or degrade only external reporting. The complete local audit report SHALL remain available for retry and the source outcome SHALL remain unchanged.

#### Scenario: Connection is imported
- **WHEN** the operator supplies compatible OAuth files and an existing spreadsheet destination to the setup operation
- **THEN** Agent Runner copies the required values into protected user-scoped storage without retaining a runtime dependency on the source application

#### Scenario: No Google connection exists
- **WHEN** an eligible local audit runs before Google setup is complete
- **THEN** model auditing completes locally where possible and reporting records a retryable warning

#### Scenario: Connection changes during audit
- **WHEN** the operator changes the stored destination after an audit has frozen its completed report
- **THEN** retry uses the destination identity frozen for that audit unless the operator explicitly requests migration

#### Scenario: Connection contains secrets
- **WHEN** OAuth client or token material is imported
- **THEN** it is stored atomically in a user-only record and is not copied into project configuration, run artifacts, logs, model prompts, or the spreadsheet

### Requirement: Google Sheets is the initial external dataset

The initial lightweight reporting destination SHALL be one existing Google spreadsheet and worksheet tab recorded by the development-audit setup operation. Agent Runner SHALL call the Google Sheets API directly and SHALL NOT require a Hermes installation, Hermes process, hosted Agent Runner service, or generalized reporting-sink framework.

Spreadsheet creation, sharing, and interactive formatting are outside the reporting operation. The reporter SHALL validate that the recorded spreadsheet and tab are accessible and have the expected versioned header before writing.

#### Scenario: Configured sheet matches the schema
- **WHEN** the spreadsheet and tab are accessible and their header matches the supported schema
- **THEN** Agent Runner can report validated value observations to that tab

#### Scenario: Spreadsheet is missing
- **WHEN** the recorded spreadsheet or tab cannot be found or accessed
- **THEN** reporting fails with a non-blocking warning and the complete local report remains available

#### Scenario: Header does not match
- **WHEN** the configured tab has missing, reordered, or unsupported columns
- **THEN** Agent Runner writes no observation rows and reports the schema mismatch without modifying the sheet structure

### Requirement: Existing Google OAuth credentials can be imported

The development-audit setup operation SHALL support a one-time import of an existing Google installed-application OAuth client credential and authorized-user token into an atomically written Agent Runner development-audit connection record. The parent storage directory SHALL be user-only and the record SHALL have mode `0600`. After import, Google API access SHALL be independent of the source application and source credential-file locations. Agent Runner SHALL NOT place OAuth client secrets, access tokens, or refresh tokens in project configuration, run artifacts, audit logs, model prompts, or the spreadsheet.

#### Scenario: Existing credential is imported
- **WHEN** the operator imports a compatible OAuth client credential and user token
- **THEN** Agent Runner stores the required credential material in its own protected user scope and can authenticate without the source application

#### Scenario: Source application is unavailable later
- **WHEN** imported credentials are configured and the application that originally created them is absent
- **THEN** Agent Runner's Sheets reporting does not depend on that application at runtime

#### Scenario: Credential lacks Sheets access
- **WHEN** the imported credential is valid but lacks the scope or permission needed for the recorded spreadsheet
- **THEN** reporting produces a non-blocking authorization warning and retains the local report

#### Scenario: Imported source file is project-local
- **WHEN** the operator imports compatible credential material from a file inside a project tree
- **THEN** Agent Runner uses only its protected copy at runtime and does not retain the source path in run or reporting artifacts

### Requirement: External rows are an allowlisted high-level projection

Each spreadsheet row SHALL represent one validated executed-leaf-step observation for one execution session and SHALL contain only the approved identity, cost, aggregate change, and categorical judgment fields defined by `workflow-value-observation`.

The initial `step_value_v1` worksheet SHALL use this exact ordered header: `schema_version`, `observation_id`, `observed_at_utc`, `project`, `workflow`, `source_run_id`, `execution_session_id`, `audit_run_id`, `trigger`, `source_outcome`, `step_id`, `step_outcome`, `lineage`, `duration_ms`, `cost_usd`, `total_tokens`, `source_models`, `git_attribution`, `commit_shas`, `files_changed`, `lines_added`, `lines_deleted`, `overall_value`, `change_effect`, `unique_contribution`, `downstream_evidence`, `confidence`, `evidence_coverage`, `judge_model`, `rubric_version`, `note`.

The `project` field SHALL be a sanitized Git hosting `owner/repository` slug derived from the source repository's configured remote when available, without its host, protocol, credentials, query, or path. When no suitable remote exists, it SHALL use only the source repository root's basename. It MUST NOT contain an absolute local path.

The optional note SHALL be a single line of no more than 280 Unicode characters. The reporter SHALL reject a note containing a URL, local path, secret-like value, or evidence excerpt rather than export that detail.

The reporter MUST NOT write transcripts, transcript summaries, prompts, responses, tool calls, command output, source code, diffs, artifact contents, evidence excerpts, filenames or paths, private URLs, or other detailed run material. The optional note SHALL remain a short high-level judgment and MUST NOT summarize a transcript or reproduce detailed evidence.

#### Scenario: Local report contains detailed evidence
- **WHEN** the local observation includes consulted diffs, output, paths, or evidence references
- **THEN** the spreadsheet projection omits those fields and writes only allowlisted high-level values

#### Scenario: Note contains prohibited detail
- **WHEN** a proposed note contains a local path, evidence excerpt, or transcript-like content
- **THEN** validation rejects or sanitizes the note before any row is written

#### Scenario: Unknown metric is reported
- **WHEN** cost, tokens, or another approved metric is unknown
- **THEN** its spreadsheet value remains explicitly empty or unknown according to the schema and is not written as zero

#### Scenario: Project has a GitHub remote
- **WHEN** the source repository remote identifies `Codagent-AI/agent-runner`
- **THEN** the project field is `Codagent-AI/agent-runner` and contains no URL or local path

#### Scenario: Project has no usable remote
- **WHEN** the source repository has no remote from which an owner/repository slug can be derived
- **THEN** the project field is the repository root basename only

### Requirement: Reporting is append-only and retry-safe

A completed audit SHALL append one row per executed-leaf-step observation. Replaying the same source execution SHALL append a new observation set with a distinct audit-run identity and replay trigger. Retrying delivery of an already completed audit SHALL use the existing observation identity and MUST NOT append a duplicate row for that observation.

#### Scenario: Automatic audit is reported
- **WHEN** a completed automatic audit has three validated step observations not already present in the sheet
- **THEN** reporting appends three rows marked as automatic and leaves existing rows unchanged

#### Scenario: Source audit is replayed
- **WHEN** the same source execution is audited again explicitly
- **THEN** reporting appends new rows with the new audit-run identity and replay trigger while preserving prior rows

#### Scenario: Ambiguous append is retried
- **WHEN** a prior write may have reached Google before its response was lost
- **THEN** the retry checks the stable observation identity and does not create a second row for the same observation

### Requirement: Reporting failure is local and non-blocking

The complete validated audit report SHALL be committed locally before external reporting begins. A validation, authentication, API, rate-limit, or write failure SHALL retain that report for retry, record a reporting warning on the audit, and SHALL NOT change the source workflow's result.

#### Scenario: Google API is unavailable
- **WHEN** the local audit report is complete but the Sheets API request fails
- **THEN** the report remains retryable locally and the source workflow outcome is unchanged

#### Scenario: Reporting later succeeds
- **WHEN** reporting is retried after a transient failure
- **THEN** the original validated observations are written without rerunning the model audit

## Test Plan

- `INT-005: Google connection import and OAuth independence`: use isolated homes, synthetic installed-client/token JSON, a controlled token server, malformed/insufficient-scope variants, deleted source files, and changed destinations. Prove `0700`/`0600` storage, required-field-only copies, no leaked paths/secrets, refresh without Hermes/source files, non-blocking errors, audit independence, and frozen-destination retry unless explicitly migrated.
- `INT-006: Google Sheets projection and retry contract`: use a stateful Sheets server for exact/missing/reordered/unsupported headers, pre-existing IDs, failures, ambiguous committed appends, and concurrent reporters. Prove exact ordered rows, sanitized projects, strict field allowlisting, blank unknowns, unsafe-note rejection, no structural mutation, append/replay semantics, ID reuse, ambiguous-response deduplication, per-destination serialization, and local-first retry state.
- `E2E-001: Automatic audit completes independently after a successful headless run`: run a temporary tagged CLI against an isolated eligible Git fixture, recorded fake `crosscheck`, delayed audit sentinel, and controlled Sheets/fake-GitHub boundaries. Prove source return precedes audit completion, exactly one non-recursive link, frozen evidence and resolved model provenance, one row per leaf, owned child aggregation, separate metrics, complete local report, allowlisted requests, no value-originated issue, and retry with no new row/model call.
- `E2E-002: Failed execution, resume, replay, and report retry preserve lineage`: run failure then resume, automatic audits for both sessions, explicit replay of one unambiguous session, and a failed-then-retried delivery. Prove source authority/resumability, immutable snapshots, distinct session IDs, overlap lineage, new replay identities without source execution, retry identity/model-call stability, and unchanged source state/Git content.

## Done When

- The tagged setup command imports validated compatible Google client/token fields and destination into an atomic user-only connection record; source files/paths and secrets do not enter configuration, runs, logs, prompts, reports, or network projections.
- Missing, malformed, insufficient-scope, inaccessible, or unusable connection state only records a retryable reporting warning; it never controls audit launch or changes the source outcome.
- The protected connection-record reader implements the stage-6 destination resolver, allowing report assembly to freeze spreadsheet ID/tab plus `step_value_v1` or an explicit unavailable/unusable state before delivery; the reporter resolves secrets only at delivery, validates the exact header without repair, acquires a per-spreadsheet/tab user lock, checks existing observation IDs, and safely resolves ambiguous responses.
- Projection is built from the exact ordered allowlist; unknown numbers are blank, project slugs are sanitized, unsafe notes block their row, and prohibited detailed/local/secret fields cannot appear in an HTTP request.
- Automatic audits append once, replays append distinct identities, and report retries reuse the original report/IDs without invoking source work or either model.
- Stage 7 is wired into the existing `internal/devaudit/workflows/audit/run-audit-v1.0.yaml` asset; no second audit workflow asset is created or embedded.
- `INT-005`, `INT-006`, `E2E-001`, and `E2E-002` pass in tagged CI using only isolated local fakes and controlled HTTP servers; no real Google/GitHub mutation occurs.
- `make fmt`, normal untagged tests, and the full tagged build/test/lint/`gosec`/`govulncheck` CI matrix pass.
