## MODIFIED Requirements

### Requirement: Google Sheets is the initial external dataset

The initial lightweight reporting destination SHALL be one existing Google spreadsheet and worksheet tab recorded by the development-audit setup operation. Agent Runner SHALL call the Google Sheets API directly and SHALL NOT require a Hermes installation, Hermes process, hosted Agent Runner service, or generalized reporting-sink framework.

Spreadsheet creation, sharing, and interactive formatting are outside the reporting operation. The reporter SHALL validate that the recorded spreadsheet and tab are accessible and have the expected versioned header before writing. The current supported header is `step_value_v2`.

When a configured tab's header row exactly matches the prior `step_value_v1` header, and every cell to the right of it in the header row is empty, the reporter SHALL upgrade the tab in place. It does this by writing only the `step_value_v2` trailing header cells into those empty cells, then delivers the report. The upgrade SHALL NOT modify, reorder, or remove existing header cells, existing data rows, or other tabs. Rows written before the upgrade SHALL remain valid and read as empty (unknown) in the added columns. The upgrade and row append SHALL happen under the same destination delivery lock. A failure to write the upgraded header SHALL be a non-blocking reporting failure that retains the local report for retry. Any other header, including a `step_value_v1` header with non-empty cells to its right, SHALL be treated as a mismatch.

#### Scenario: Configured sheet matches the schema
- **WHEN** the spreadsheet and tab are accessible and their header matches the supported schema
- **THEN** Agent Runner can report validated value observations to that tab

#### Scenario: Spreadsheet is missing
- **WHEN** the recorded spreadsheet or tab cannot be found or accessed
- **THEN** reporting fails with a non-blocking warning and the complete local report remains available

#### Scenario: Header does not match
- **WHEN** the configured tab has missing, reordered, or unsupported columns
- **THEN** Agent Runner writes no observation rows and reports the schema mismatch without modifying the sheet structure

#### Scenario: Prior-version header is upgraded
- **WHEN** the configured tab's header exactly matches `step_value_v1` and the header cells to its right are empty
- **THEN** the reporter appends the `step_value_v2` judge column names after the existing header, leaves existing rows unchanged, and appends the new observation rows under the `step_value_v2` layout

#### Scenario: Prior-version header has content to its right
- **WHEN** the configured tab's header matches `step_value_v1` but a cell to its right in the header row is non-empty
- **THEN** Agent Runner writes no observation rows and does not modify the sheet, and reports the schema mismatch

#### Scenario: Header upgrade write fails
- **WHEN** writing the upgraded header cells fails
- **THEN** no observation rows are appended, the reporting failure is recorded as a non-blocking warning, and the local report remains pending for retry

### Requirement: External rows are an allowlisted high-level projection

Each spreadsheet row SHALL represent one validated executed-leaf-step observation for one execution session and SHALL contain only the approved identity, cost, aggregate change, and categorical judgment fields defined by `workflow-value-observation`, plus the audit-level judge usage fields defined below.

The initial `step_value_v1` worksheet SHALL use this exact ordered header: `schema_version`, `observation_id`, `observed_at_utc`, `project`, `workflow`, `source_run_id`, `execution_session_id`, `audit_run_id`, `trigger`, `source_outcome`, `step_id`, `step_outcome`, `lineage`, `duration_ms`, `cost_usd`, `total_tokens`, `source_models`, `git_attribution`, `commit_shas`, `files_changed`, `lines_added`, `lines_deleted`, `overall_value`, `change_effect`, `unique_contribution`, `downstream_evidence`, `confidence`, `evidence_coverage`, `judge_model`, `rubric_version`, `note`.

The current `step_value_v2` worksheet SHALL use the complete `step_value_v1` header, in the same order, followed by these audit-level judge columns in this order: `judge_cli`, `judge_effort`, `audit_judge_attempts`, `audit_judge_total_tokens`, `audit_judge_token_coverage`, `audit_judge_cost_usd`, `audit_judge_cost_coverage`. Rows written by this reporter SHALL carry `step_value_v2` in `schema_version`. The judge columns SHALL be the audit's judge CLI, reasoning effort, launched judge attempt count, aggregate total judge tokens with coverage, and aggregate judge USD cost with coverage, taken from the audit-level judge usage summary defined by `audit-judge-usage`. Every row from the same audit SHALL carry identical judge values. Those values describe the whole audit, not the row's step, so consumers SHALL aggregate them once per `audit_run_id` and SHALL NOT sum them across rows. The existing `judge_model` column SHALL continue to carry the resolved judge model.

Judge totals whose coverage is `none` SHALL be written empty, never `0`. A pending report assembled without an audit-level judge usage summary, as with audits completed before this capability existed, SHALL still deliver, with every audit-level judge column except any known `judge_cli` and `judge_effort` left empty.

The `project` field SHALL be a sanitized Git hosting `owner/repository` slug derived from the source repository's configured remote when available, without its host, protocol, credentials, query, or path. When no suitable remote exists, it SHALL use only the source repository root's basename. It MUST NOT contain an absolute local path.

The optional note SHALL be a single line of no more than 280 Unicode characters. The reporter SHALL reject a note containing a URL, local path, secret-like value, or evidence excerpt rather than export that detail.

The reporter MUST NOT write transcripts, transcript summaries, prompts, responses, tool calls, command output, source code, diffs, artifact contents, evidence excerpts, filenames or paths, private URLs, or other detailed run material. The optional note SHALL remain a short high-level judgment and MUST NOT summarize a transcript or reproduce detailed evidence.

#### Scenario: Local report contains detailed evidence
- **WHEN** the local observation includes consulted diffs, output, paths, or evidence references
- **THEN** the spreadsheet projection omits those fields and writes only allowlisted high-level values

#### Scenario: Note contains prohibited detail
- **WHEN** a proposed note contains a local path, evidence excerpt, or transcript-like content
- **THEN** validation omits the optional note while retaining the validated categorical observation

#### Scenario: Rejected optional note does not block an audit
- **WHEN** an otherwise valid model observation contains a note rejected by the evidence-safety checks
- **THEN** value validation omits that note from the validated observation and continues the audit
- **AND** the original model output remains available locally for diagnosis
- **AND** invalid categorical judgments and unknown evidence references still fail validation

#### Scenario: Unknown metric is reported
- **WHEN** cost, tokens, or another approved metric is unknown
- **THEN** its spreadsheet value remains explicitly empty or unknown according to the schema and is not written as zero

#### Scenario: Project has a GitHub remote
- **WHEN** the source repository remote identifies `Codagent-AI/agent-runner`
- **THEN** the project field is `Codagent-AI/agent-runner` and contains no URL or local path

#### Scenario: Project has no usable remote
- **WHEN** the source repository has no remote from which an owner/repository slug can be derived
- **THEN** the project field is the repository root basename only

#### Scenario: Audit rows carry audit-level judge usage
- **WHEN** an audit with three step observations recorded judge usage totaling 1,200,000 tokens and $4.25 across four attempts with complete coverage
- **THEN** each of the three appended rows carries `judge_cli`, `judge_effort`, `audit_judge_attempts` of 4, `audit_judge_total_tokens` of 1200000, `audit_judge_token_coverage` of `complete`, `audit_judge_cost_usd` of 4.25, and `audit_judge_cost_coverage` of `complete`

#### Scenario: Judge cost is unreported
- **WHEN** the audit's judge reported no USD cost for any attempt
- **THEN** `audit_judge_cost_usd` is empty and `audit_judge_cost_coverage` is `none` on every row from that audit

#### Scenario: Pending report predates judge usage
- **WHEN** a pending report assembled before judge usage recording is delivered to a `step_value_v2` tab
- **THEN** its rows are appended with the audit-level judge usage columns empty rather than zero
