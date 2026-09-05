## ADDED Requirements

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
