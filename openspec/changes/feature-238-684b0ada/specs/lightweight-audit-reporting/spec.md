## ADDED Requirements

### Requirement: Audit issue bodies are durable and repairable

GitHub issue publication SHALL pass the redacted issue body to the GitHub CLI on stdin through the explicit `--body-file -` option. A development-audit build SHALL provide a development-only repair operation, `audit repair-issues <audit-session-dir>`, that reads only the findings that audit's local report records as created. For each such finding, it SHALL restore the intended redacted body and durable markers only when GitHub reports that the linked issue in `Codagent-AI/agent-runner` has an `[auto-audit]` title prefix and a body that is exactly the historical `-` placeholder. It SHALL leave every other issue unchanged.

#### Scenario: Issue is published with an explicit stdin body
- **WHEN** the correctness stage creates an issue for a confirmed defect
- **THEN** it invokes `gh issue create` with `--body-file -` and supplies the redacted body, including its durable markers, on stdin

#### Scenario: Historical placeholder issue is repaired
- **WHEN** a locally recorded created finding points to an `[auto-audit]` GitHub issue with body `-`
- **THEN** the repair operation replaces that body with the redacted finding body and durable markers

#### Scenario: Edited issue is protected
- **WHEN** the linked issue has a body other than `-` or is not an auto-audit issue
- **THEN** the repair operation leaves it unchanged

## MODIFIED Requirements

### Requirement: External rows are an allowlisted high-level projection

Each spreadsheet row SHALL represent one validated executed-leaf-step observation for one execution session and SHALL contain only the approved identity, cost, aggregate change, and categorical judgment fields defined by `workflow-value-observation`, plus the audit-level judge usage fields defined below.

The initial `step_value_v1` worksheet SHALL use this exact ordered header: `schema_version`, `observation_id`, `observed_at_utc`, `project`, `workflow`, `source_run_id`, `execution_session_id`, `audit_run_id`, `trigger`, `source_outcome`, `step_id`, `step_outcome`, `lineage`, `duration_ms`, `cost_usd`, `total_tokens`, `source_models`, `git_attribution`, `commit_shas`, `files_changed`, `lines_added`, `lines_deleted`, `overall_value`, `change_effect`, `unique_contribution`, `downstream_evidence`, `confidence`, `evidence_coverage`, `judge_model`, `rubric_version`, `note`.

The current `step_value_v2` worksheet SHALL use the complete `step_value_v1` header, in the same order, followed by these audit-level judge columns in this order: `judge_cli`, `judge_effort`, `audit_judge_attempts`, `audit_judge_total_tokens`, `audit_judge_token_coverage`, `audit_judge_cost_usd`, `audit_judge_cost_coverage`. Rows written by this reporter SHALL carry `step_value_v2` in `schema_version`. The judge columns SHALL be the audit's judge CLI, reasoning effort, launched judge attempt count, aggregate total judge tokens with coverage, and aggregate judge USD cost with coverage, taken from the audit-level judge usage summary defined by `audit-judge-usage`. Every row from the same audit SHALL carry identical judge values. Those values describe the whole audit, not the row's step, so consumers SHALL aggregate them once per `audit_run_id` and SHALL NOT sum them across rows. The existing `judge_model` column SHALL continue to carry the resolved judge model.

Judge totals whose coverage is `none` SHALL be written empty, never `0`. A pending report assembled without an audit-level judge usage summary, as with audits completed before this capability existed, SHALL still deliver, with every audit-level judge column except any known `judge_cli` and `judge_effort` left empty.

The `project` field SHALL be a sanitized Git hosting `owner/repository` slug derived from the source repository's configured remote when available, without its host, protocol, credentials, query, or path. When no suitable remote exists, it SHALL use only the source repository root's basename. It MUST NOT contain an absolute local path.

The optional note SHALL be a single line of no more than 280 Unicode characters. Value validation SHALL apply these deterministic note checks: a note longer than 280 characters or containing a line break is unsafe, and a note containing `://`, `/`, `\`, or (case-insensitively) `ghp_`, `sk-`, or `token=` is unsafe as detailed evidence. When a proposed note fails these checks, validation SHALL omit only that note before any row is written, SHALL retain the otherwise validated categorical observation, and SHALL record a reason-only diagnostic in the local report that does not reproduce the note text. These checks are the only automated note filtering; keeping a note free of other detail, such as evidence excerpts or transcript-like content, rests on the judge's instructions and is not enforced by the reporter.

Apart from optional note text that passes the checks above, the reporter MUST NOT write transcripts, transcript summaries, prompts, responses, tool calls, command output, source code, diffs, artifact contents, evidence excerpts, filenames or paths, private URLs, or other detailed run material. The judge's instructions SHALL limit the optional note to a bounded statement of the concrete contribution or harm and SHALL tell the judge not to include a transcript, a transcript summary, or paths.

#### Scenario: Local report contains detailed evidence
- **WHEN** the local observation includes consulted diffs, output, paths, or evidence references
- **THEN** the spreadsheet projection omits those fields and writes only allowlisted high-level values

#### Scenario: Note contains prohibited detail
- **WHEN** a proposed note contains a URL, a path separator, or a recognized secret marker (`ghp_`, `sk-`, or `token=`), or is multi-line or longer than 280 characters
- **THEN** validation omits the optional note before any row is written while retaining the validated categorical observation
- **AND** the local report records a reason-only diagnostic without the rejected note text

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

### Requirement: Reporting failure is local and non-blocking

The complete validated audit report SHALL be committed locally before external reporting begins. A validation, authentication, API, rate-limit, or write failure SHALL retain that report for retry, record a reporting warning on the audit, and SHALL NOT change the source workflow's result. The pending local report SHALL retain a non-secret delivery error independently of audit-stage warnings, and completing the audit after a reporting failure SHALL NOT erase that delivery error or the audit's reporting warning. A successful retry SHALL mark the report delivered and clear only its delivery error, while retaining the original observations, their identities, and any audit-stage warning.

#### Scenario: Google API is unavailable
- **WHEN** the local audit report is complete but the Sheets API request fails
- **THEN** the report remains retryable locally and the source workflow outcome is unchanged

#### Scenario: Report assembled without a connection is retried elsewhere
- **WHEN** a pending report whose frozen destination is unconfigured is retried on a machine with a configured reporting connection
- **THEN** the retry adopts that machine's destination and delivers the original observations, while a report frozen to a configured destination changes only through explicit migration

#### Scenario: Reporting later succeeds
- **WHEN** reporting is retried after a transient failure
- **THEN** the original validated observations are written without rerunning the model audit

#### Scenario: Completion follows reporting failure
- **WHEN** external delivery fails before the audit workflow completes
- **THEN** after completion the local report remains pending with its delivery error, and the audit link keeps both its reporting warning and its separate audit-stage warning

#### Scenario: Retry succeeds
- **WHEN** a pending report is later delivered successfully
- **THEN** the report is marked delivered and its delivery error is cleared without rerunning audit models or changing observation identity
