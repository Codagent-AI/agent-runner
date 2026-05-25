# debug-inspection-cli Specification

## Purpose
Provide read-only CLI helpers that let a debug agent inspect run state, audit summaries, and workflow YAML in installed environments where the source tree may not be available.

## Requirements
### Requirement: `debug --show-workflow <ref>` prints resolved YAML

The `agent-runner debug --show-workflow <ref>` command SHALL print the YAML for the named workflow ref to stdout and exit 0 on success. Builtin refs (`builtin:<namespace>/<file>` and the equivalent `<namespace>:<name>` form) SHALL be resolved from the embedded builtin FS. Non-builtin refs (relative paths, absolute paths, `~`-prefixed paths) SHALL be resolved from disk. The output SHALL be the YAML for the **named ref only** — composed sub-workflow references SHALL NOT be inlined, expanded, or followed; they SHALL appear in the output exactly as they appear in the source. The output SHALL be the bytes as embedded or stored, with no normalization, reformatting, or comment stripping.

#### Scenario: Builtin ref returns embedded YAML
- **WHEN** `agent-runner debug --show-workflow core:finalize-pr` is invoked
- **THEN** the embedded YAML for `workflows/core/finalize-pr.yaml` is printed to stdout verbatim and the command exits 0

#### Scenario: On-disk ref returns file bytes
- **WHEN** `agent-runner debug --show-workflow ./my-workflow.yaml` is invoked and the file exists
- **THEN** the file's bytes are printed to stdout verbatim and the command exits 0

#### Scenario: Sub-workflow references preserved
- **WHEN** the requested workflow contains `workflow: plan-change.yaml` references in its YAML
- **THEN** those reference lines appear in the output unmodified; no sub-workflow content is inlined

#### Scenario: Unknown ref
- **WHEN** `agent-runner debug --show-workflow` is invoked with a ref that resolves to neither an embedded builtin nor an existing on-disk file
- **THEN** the command exits non-zero and prints an error to stderr naming the missing ref

#### Scenario: Malformed ref string
- **WHEN** `agent-runner debug --show-workflow` is invoked with a ref string that cannot be parsed (e.g. empty, contains illegal characters)
- **THEN** the command exits non-zero and prints a parse error to stderr

#### Scenario: Output is unnormalized
- **WHEN** the resolved YAML contains comments, blank lines, or non-canonical whitespace
- **THEN** those bytes appear in the output unchanged

### Requirement: `debug --state <run-id>` prints state JSON

The `agent-runner debug --state <run-id>` command SHALL print the contents of `<sessionDir>/state.json` for the given run id to stdout as JSON and exit 0 on success. The command resolves `<run-id>` in the current project, using the same project-scoped lookup as run inspection. The output SHALL be valid JSON parseable without additional transformation. The JSON SHALL include at minimum the fields that `state.json` records for the run: `workflowFile` (string ref of the workflow that produced the run), `params` (object of input parameters), `currentStep` (latest step pointer the runner has recorded), and `completed` (boolean true when the run finished successfully). The command SHALL emit the on-disk JSON contents faithfully rather than rewriting or re-projecting fields.

#### Scenario: Known run id returns state JSON
- **WHEN** `agent-runner debug --state <run-id>` is invoked for a known run
- **THEN** the run's `state.json` contents are printed to stdout as valid JSON and the command exits 0

#### Scenario: Output includes required fields
- **WHEN** the command succeeds for a started run
- **THEN** the JSON output contains at least `workflowFile`, `params`, and `currentStep` fields, and includes `completed` when the run has finished

#### Scenario: Unknown run id
- **WHEN** `agent-runner debug --state <run-id>` is invoked with a run id that does not exist
- **THEN** the command exits non-zero and prints an error to stderr naming the missing run id

#### Scenario: Corrupt or missing state.json
- **WHEN** the run's session directory exists but `state.json` is missing or unparseable
- **THEN** the command exits non-zero and prints an error to stderr naming the file and the underlying read or parse error

### Requirement: `debug --state-dir <session-dir>` prints state JSON by path

The `agent-runner debug --state-dir <session-dir>` command SHALL print the contents of `<session-dir>/state.json` to stdout as JSON and exit 0 on success. Unlike `debug --state <run-id>`, the command SHALL NOT resolve the session through the current project; it SHALL read the provided session directory directly. The command SHALL emit the on-disk JSON contents faithfully rather than rewriting or re-projecting fields.

#### Scenario: Known session dir returns state JSON outside project
- **WHEN** `agent-runner debug --state-dir <session-dir>` is invoked from a current directory unrelated to the failed run's project
- **THEN** the run's `state.json` contents are printed to stdout as valid JSON and the command exits 0

#### Scenario: Invalid session dir
- **WHEN** `agent-runner debug --state-dir <session-dir>` is invoked with a path that is missing, not a directory, or contains no readable `state.json`
- **THEN** the command exits non-zero and prints an error to stderr naming the invalid path or state file

### Requirement: `debug --audit-summary <run-id>` emits bounded structured summary plus path

The `agent-runner debug --audit-summary <run-id>` command SHALL parse `<sessionDir>/audit.log` for the given run and emit a bounded, structured JSON summary to stdout. The summary SHALL include, at minimum:

- a list of step boundaries (start and end events with their nesting prefix, type, and outcome);
- error events (with their nesting prefix and message);
- run start and run end events;
- sub-workflow boundaries, including embedded or on-disk `workflow_path` values when present in audit data;
- failed step and failed sub-workflow events in a top-level `failures` list, including prefix, outcome, exit code, error, stderr/stdout snippets, and workflow path when present;
- the session directory and project directory for the inspected run;
- the **absolute path** to the full `audit.log` (in a `path` or equivalent field) so the caller can grep, tail, or otherwise inspect the file for additional detail.

The summary SHALL be capped at a configurable maximum byte size with a default of 64 KB. When the cap is reached, the output SHALL include an explicit boolean `truncated: true` flag and a `dropped_events_count` integer indicating how many events were not represented. When the cap is not reached, the output SHALL include `truncated: false`. The cap SHALL apply to the structured event list only; the audit-log `path` field SHALL be present even on truncation. When the audit log is missing entirely (e.g. the run crashed before any audit event was written), the command SHALL exit 0 with a summary object indicating no events, `truncated: false`, `dropped_events_count: 0`, plus the path where the audit log would have been.

#### Scenario: Known run id with audit log
- **WHEN** `agent-runner debug --audit-summary <run-id>` is invoked for a known run with a populated audit log
- **THEN** the command prints a JSON summary to stdout including step boundaries, error events, run start/end, sub-workflow boundaries, failures, session/project directories, and the absolute path of the audit log, and exits 0

#### Scenario: Failed command summarized
- **WHEN** an audit log contains a failed `step_end` event with an exit code and stderr/stdout
- **THEN** the output JSON includes a corresponding entry in `failures` with the step prefix, exit code, outcome, and bounded stderr/stdout snippets

#### Scenario: Failed sub-workflow summarized
- **WHEN** an audit log contains a failed `sub_workflow_end` event with workflow metadata
- **THEN** the output JSON includes a corresponding entry in `failures` with the workflow name/path and nesting prefix

#### Scenario: Summary stays under cap
- **WHEN** the structured event list serializes to fewer than the configured cap bytes
- **THEN** the output JSON includes `truncated: false` and the full event list

#### Scenario: Summary exceeds cap
- **WHEN** the structured event list would exceed the configured cap
- **THEN** the output JSON includes `truncated: true`, a `dropped_events_count` integer, and the audit-log `path` field; the events that are included are bounded by the cap

#### Scenario: Missing audit log
- **WHEN** the run's session directory exists but `<sessionDir>/audit.log` does not exist
- **THEN** the command exits 0 with a JSON summary indicating no events, `truncated: false`, `dropped_events_count: 0`, `session_dir`, `project_dir`, and a `path` field naming where the audit log would have been

#### Scenario: Unknown run id
- **WHEN** `agent-runner debug --audit-summary <run-id>` is invoked with a run id whose session directory does not exist
- **THEN** the command exits non-zero and prints an error to stderr naming the missing run id

#### Scenario: Output is valid JSON
- **WHEN** the command succeeds (with or without truncation, with or without audit log present)
- **THEN** the stdout output is valid JSON parseable by a standard JSON parser

### Requirement: `debug --audit-summary-dir <session-dir>` emits audit summary by path

The `agent-runner debug --audit-summary-dir <session-dir>` command SHALL emit the same bounded, redacted JSON summary shape as `debug --audit-summary <run-id>`, but SHALL read directly from the provided session directory instead of resolving a run id in the current project. If the session directory is valid run storage but has no project metadata, such as global onboarding run storage, the command SHALL omit `project_dir` or emit it as an empty string instead of failing.

#### Scenario: Known session dir returns audit summary outside project
- **WHEN** `agent-runner debug --audit-summary-dir <session-dir>` is invoked from a current directory unrelated to the failed run's project
- **THEN** the command prints the run's audit summary, including `session_dir`, `project_dir`, and `path`, and exits 0

#### Scenario: Missing audit log by session dir
- **WHEN** the provided session directory exists but has no `audit.log`
- **THEN** the command exits 0 with an empty summary, `session_dir`, `project_dir`, and the path where the audit log would have been

#### Scenario: Global onboarding session without project metadata
- **WHEN** `agent-runner debug --audit-summary-dir <session-dir>` is invoked for a global onboarding run under onboarding run storage that has no project metadata file
- **THEN** the command exits 0, includes `session_dir` and `path`, and omits `project_dir` or emits it as an empty string

### Requirement: Redaction applied in `debug --audit-summary`

Before emitting the structured summary, the `debug --audit-summary` command SHALL apply a programmatic redaction pass over event payload strings. Matches of a known pattern set SHALL be replaced with the literal placeholder `<REDACTED>`. The initial pattern set SHALL include at minimum:

- GitHub tokens matching `gh[pousr]_[A-Za-z0-9]+`
- OpenAI-style keys matching `sk-[A-Za-z0-9]+`
- HTTP bearer credentials matching `Bearer [A-Za-z0-9._-]+`
- Env-style token assignments matching `[A-Za-z0-9_]*_TOKEN=[^\s]+`
- Password assignments matching `password=[^\s]+`

Redaction SHALL NOT modify the on-disk `audit.log`. Redaction SHALL apply at read time, so adding a new pattern takes effect on the next invocation without rewriting any persisted file.

#### Scenario: Matched value substituted
- **WHEN** an event payload contains a value matching a pattern (e.g. `ghp_AbC123XyZ`)
- **THEN** the value in the output summary is replaced with `<REDACTED>` and the surrounding context is preserved

#### Scenario: On-disk audit log unchanged
- **WHEN** the command runs against an audit log containing a matched value
- **THEN** the on-disk `audit.log` file's bytes are not modified

#### Scenario: Pattern updates apply on next call
- **WHEN** the redaction pattern set is updated (e.g. a new pattern added in a later release) and `debug --audit-summary` is invoked again on the same run
- **THEN** the new pattern is applied to the output of that next invocation

#### Scenario: Redaction is one-way in output
- **WHEN** a caller has received the redacted summary
- **THEN** the caller has no way to reconstruct the original secret value from the summary alone

### Requirement: All inspection commands are read-only

The inspection commands (`debug --show-workflow`, `debug --state`, `debug --state-dir`, `debug --audit-summary`, `debug --audit-summary-dir`) SHALL be read-only. None of them SHALL modify any file on disk, take any run-lock, acquire any TUI surface, or otherwise affect runner state. They MAY be invoked while the same run is being viewed in another agent-runner process or while another inspection command is running for the same id.

#### Scenario: No disk writes from any inspection command
- **WHEN** any debug inspection command is invoked
- **THEN** no file on disk is created, modified, or deleted as a side effect

#### Scenario: No run-lock acquired
- **WHEN** an inspection command is invoked for a run whose run-lock is held by another live process
- **THEN** the inspection command does not attempt to acquire the lock and does not block on it; the command produces its output and exits

#### Scenario: Concurrent inspection invocations succeed
- **WHEN** two inspection commands are invoked concurrently against the same run id
- **THEN** both succeed independently without contention

#### Scenario: Usable without a TTY
- **WHEN** any inspection command is invoked with stdout piped or redirected
- **THEN** the command runs and prints its output; no TTY check fires
