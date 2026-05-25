# Task: Runtime helpers for the debug agent

## Goal

Implement the runner-side machinery the debug workflow's agent will lean on: a new `agent-runner debug` subcommand with three read-only inspection operations (`--state`, `--audit-summary`, `--show-workflow`), the supporting audit-summary builder and redaction package, and the resume-handoff marker-file convention that lets any workflow signal "after this run ends successfully, exec `agent-runner --resume <id>`." No UI work in this task — these are purely runner-side helpers.

## Background

This task delivers the runtime contract the debug workflow's agent depends on and the resume-handoff mechanism shared by any future workflow that wants to chain into a resume. Both are general-purpose enough to ship before the workflow itself exists.

### Why this exists

When an agent-runner workflow fails today, the user can't easily debug it: they don't have the source tree, audit log path, or workflow YAML at their fingertips, and the audit log can be multi-megabyte. The debug agent's playbook will shell out to three new read-only commands to gather context without dumping raw files into its context window. The audit-summary command pre-redacts known secret patterns so tokens never reach the agent.

Separately, when the debug agent concludes the user has fixed the underlying problem, the workflow needs a way to ask the runner to resume the failed run instead of dropping the user back at the launching TUI screen. The runner exposes this as a marker-file convention: the workflow writes the failed run's id into a known path in its own session directory, and the runner reads it once the workflow ends.

### Key design decisions

**Single `debug` subcommand with internal flag dispatch**, not three top-level flags and not a full subcommand restructure. The existing CLI is entirely flag-based (`--list`, `--inspect`, `--validate`, `--version`); routing the first non-flag arg `debug` to a subcommand router before normal flag parsing keeps the rest of the CLI untouched while leaving room to add future debug operations.

**Per-session marker file (`<sessionDir>/resume-target`), not a global location or a PTY sentinel.** The file lives in the *signalling workflow's own* session directory — not the target run's directory. The runner already knows its own session dir (it created it), so reading the marker after workflow end is a single `os.ReadFile` with no scanning. Per-session avoids cross-workflow clobbering when multiple debug runs are active concurrently and avoids stale-marker issues from earlier sessions.

**Redaction at read time, not write time.** The on-disk `audit.log` is never modified; redaction is applied by `audit-summary` before output. Pattern set is a package-level slice that future code can extend.

**Bounded summary size, with a path field always present.** `--audit-summary` returns a structured JSON summary capped at 64 KB by default. The full audit-log absolute path is always included so the agent can `grep`/`tail` the file at that path for deeper detail without ever pulling the full file into context.

### Code-touch points

You MUST read these files before starting:

- `openspec/changes/debug-and-report-issues/design.md` — full design context.
- `openspec/changes/debug-and-report-issues/specs/debug-inspection-cli/spec.md` — verbatim spec for the three inspection commands.
- `openspec/changes/debug-and-report-issues/specs/workflow-resume-handoff/spec.md` — verbatim spec for the resume-handoff mechanism.

**New files to create:**

- `cmd/agent-runner/debug_cmd.go` — `debug` subcommand router and the three op handlers.
- `internal/audit/summary.go` — `BuildSummary(r io.Reader, capBytes int) (Summary, error)` and the `Summary`/event-classification types.
- `internal/audit/redact.go` — package-level `Patterns []*regexp.Regexp`, `Redact(s string) string`, `Placeholder = "<REDACTED>"`.
- `internal/resumehandoff/resumehandoff.go` — `MarkerPath(sessionDir string) string` and `Read(sessionDir string) (runID string, ok bool, err error)`. Trims surrounding whitespace; treats empty/whitespace-only contents as `ok=false`.

**Existing files to modify:**

- `cmd/agent-runner/main.go` — branch on `debug` subcommand before existing flag dispatch; after any workflow run reaches a terminal state, call `resumehandoff.Read(sessionDir)` and, on success-outcome + valid id, `syscall.Exec` `agent-runner --resume <id>` (same in-place-exec pattern used by the existing `ResumeRunMsg` handler). On non-success outcomes, discard the marker silently. If the read id doesn't resolve to a known session directory, surface an inline error on the launching screen and return there.

**Constraints and conventions to follow:**

- Existing types to reuse: `internal/model/state.go` `RunState` type for `--state` output; `internal/audit/types.go` `Event` type for parsing audit lines; `internal/stateio/stateio.go` `ReadState` for state loading; `workflows/embed.go` `FS` + `ReadFile` for builtin workflow resolution in `--show-workflow`.
- The three inspection ops MUST be read-only: no file writes, no run-lock acquisition, no TUI surface. They MUST be usable without a TTY (the existing TTY check should not fire for them).
- `--show-workflow` MUST output workflow YAML for the named ref only — do not recursively expand or inline composed sub-workflows. Output is the embedded/stored bytes verbatim, no normalization.
- `--audit-summary` MUST apply redaction (via the new `audit.Redact`) to all string fields in event payloads before serializing the summary. The on-disk `audit.log` MUST NOT be modified.
- Initial redaction patterns: `gh[pousr]_[A-Za-z0-9]+`, `sk-[A-Za-z0-9]+`, `Bearer [A-Za-z0-9._\-]+`, `[A-Za-z0-9_]*_TOKEN=\S+`, `password=\S+`. Replacement string: `<REDACTED>`.
- Tests: this project uses TDD for substantive behavior changes. Write tests first, alongside source, using `google/go-cmp` for structured comparisons (see `CLAUDE.md`). Run `make fmt` before committing.

**Strictly self-contained:** This task does not depend on the debug workflow YAML or any TUI work to ship. Verification end-to-end happens against fixture session dirs you create in the tests.

## Spec

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

The `agent-runner debug --state <run-id>` command SHALL print the contents of `<sessionDir>/state.json` for the given run id to stdout as JSON and exit 0 on success. The output SHALL be valid JSON parseable without additional transformation. The JSON SHALL include at minimum: `workflowFile` (string ref of the workflow that produced the run), `params` (object of input parameters), `status` (string status value), and the latest step pointer if one has been recorded.

#### Scenario: Known run id returns state JSON
- **WHEN** `agent-runner debug --state <run-id>` is invoked for a known run
- **THEN** the run's `state.json` contents are printed to stdout as valid JSON and the command exits 0

#### Scenario: Output includes required fields
- **WHEN** the command succeeds for a started run
- **THEN** the JSON output contains at least `workflowFile`, `params`, and `status` fields

#### Scenario: Unknown run id
- **WHEN** `agent-runner debug --state <run-id>` is invoked with a run id that does not exist
- **THEN** the command exits non-zero and prints an error to stderr naming the missing run id

#### Scenario: Corrupt or missing state.json
- **WHEN** the run's session directory exists but `state.json` is missing or unparseable
- **THEN** the command exits non-zero and prints an error to stderr naming the file and the underlying read or parse error

### Requirement: `debug --audit-summary <run-id>` emits bounded structured summary plus path

The `agent-runner debug --audit-summary <run-id>` command SHALL parse `<sessionDir>/audit.log` for the given run and emit a bounded, structured JSON summary to stdout. The summary SHALL include, at minimum:

- a list of step boundaries (start and end events with their nesting prefix, type, and outcome);
- error events (with their nesting prefix and message);
- run start and run end events;
- sub-workflow boundaries;
- the **absolute path** to the full `audit.log` (in a `path` or equivalent field) so the caller can grep, tail, or otherwise inspect the file for additional detail.

The summary SHALL be capped at a configurable maximum byte size with a default of 64 KB. When the cap is reached, the output SHALL include an explicit boolean `truncated: true` flag and a `dropped_events_count` integer indicating how many events were not represented. When the cap is not reached, the output SHALL include `truncated: false`. The cap SHALL apply to the structured event list only; the audit-log `path` field SHALL be present even on truncation. When the audit log is missing entirely (e.g. the run crashed before any audit event was written), the command SHALL exit 0 with a summary object indicating no events, `truncated: false`, `dropped_events_count: 0`, plus the path where the audit log would have been.

#### Scenario: Known run id with audit log
- **WHEN** `agent-runner debug --audit-summary <run-id>` is invoked for a known run with a populated audit log
- **THEN** the command prints a JSON summary to stdout including step boundaries, error events, run start/end, sub-workflow boundaries, and the absolute path of the audit log, and exits 0

#### Scenario: Summary stays under cap
- **WHEN** the structured event list serializes to fewer than the configured cap bytes
- **THEN** the output JSON includes `truncated: false` and the full event list

#### Scenario: Summary exceeds cap
- **WHEN** the structured event list would exceed the configured cap
- **THEN** the output JSON includes `truncated: true`, a `dropped_events_count` integer, and the audit-log `path` field; the events that are included are bounded by the cap

#### Scenario: Missing audit log
- **WHEN** the run's session directory exists but `<sessionDir>/audit.log` does not exist
- **THEN** the command exits 0 with a JSON summary indicating no events, `truncated: false`, `dropped_events_count: 0`, and a `path` field naming where the audit log would have been

#### Scenario: Unknown run id
- **WHEN** `agent-runner debug --audit-summary <run-id>` is invoked with a run id whose session directory does not exist
- **THEN** the command exits non-zero and prints an error to stderr naming the missing run id

#### Scenario: Output is valid JSON
- **WHEN** the command succeeds (with or without truncation, with or without audit log present)
- **THEN** the stdout output is valid JSON parseable by a standard JSON parser

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

The three inspection commands (`debug --show-workflow`, `debug --state`, `debug --audit-summary`) SHALL be read-only. None of them SHALL modify any file on disk, take any run-lock, acquire any TUI surface, or otherwise affect runner state. They MAY be invoked while the same run is being viewed in another agent-runner process or while another inspection command is running for the same id.

#### Scenario: No disk writes from any inspection command
- **WHEN** any of `debug --show-workflow`, `debug --state`, or `debug --audit-summary` is invoked
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

### Requirement: Workflow can signal a resume target during a run

A workflow run SHALL be able to signal a *resume target* — an existing run id that agent-runner should resume after the current workflow ends — at any point during the run. The signalling mechanism SHALL be a **marker file** at the path `<sessionDir>/resume-target` within the **signalling workflow's own session directory** (not the target run's session directory). The marker file SHALL contain the failed run's id as a single line of text, with surrounding whitespace and trailing newlines ignored on read. The runner SHALL accept at most one effective resume target per workflow run: when the workflow ends, the runner reads the marker file once; if multiple writes occurred during the run, the file's final contents are the effective target (last writer wins). Empty or whitespace-only contents SHALL be treated as no signal. If the file does not exist at workflow-end time, no resume target is recorded.

#### Scenario: Single write recorded
- **WHEN** during a workflow run the agent writes a single non-empty run id to `<sessionDir>/resume-target`
- **THEN** the runner reads that id at workflow-end and treats it as the recorded resume target

#### Scenario: Last writer wins
- **WHEN** the marker file is written multiple times during a workflow run with different values
- **THEN** at workflow-end the runner reads the file's final contents; earlier values are discarded by the act of overwriting

#### Scenario: No file means no resume
- **WHEN** a workflow run completes without the marker file ever being created
- **THEN** no resume target is recorded for the run

#### Scenario: Empty or whitespace-only contents ignored
- **WHEN** a workflow run completes and `<sessionDir>/resume-target` exists but contains only whitespace, only a newline, or nothing
- **THEN** the runner treats it as no recorded resume target

#### Scenario: Trimmed on read
- **WHEN** the marker file contains the id surrounded by whitespace or followed by a trailing newline (e.g. `  abc123\n`)
- **THEN** the runner trims surrounding whitespace before treating the contents as the resume target id

#### Scenario: Marker lives in signalling workflow's own session dir
- **WHEN** the runner checks for the marker
- **THEN** it reads `<sessionDir>/resume-target` where `<sessionDir>` is the session directory of the workflow run that just ended, not the directory of any other run

#### Scenario: Marker file read error treated as no signal
- **WHEN** `<sessionDir>/resume-target` exists but cannot be read (permissions, IO error, or other filesystem failure on the marker itself)
- **THEN** the runner treats it as no recorded resume target, does not attempt to exec, does not surface an error to the user, and returns to the launching TUI screen as for any other completed workflow

### Requirement: Resume target acted on at successful completion

When a workflow run ends with a **success** outcome AND a resume target was recorded during the run, agent-runner SHALL exec `agent-runner --resume <target-run-id>` in place, replacing the current process — using the same in-place-exec pattern that the run-view `r` keybinding uses. If the workflow ends with any non-success outcome (failed, aborted, cancelled), the resume target SHALL be discarded silently and agent-runner SHALL return to the launching TUI screen as it would for any other workflow run.

#### Scenario: Success with target execs --resume in place
- **WHEN** a workflow run ends with outcome success and a resume target is recorded
- **THEN** agent-runner execs `agent-runner --resume <target-run-id>` in place, replacing the current process; the TUI is not returned to

#### Scenario: Failure discards target
- **WHEN** a workflow run ends with outcome failed and a resume target was recorded
- **THEN** the recorded resume target is discarded, no exec is attempted, and the user is returned to the launching TUI screen as for any other failed workflow run

#### Scenario: Cancellation discards target
- **WHEN** a workflow run ends with outcome aborted or cancelled and a resume target was recorded
- **THEN** the recorded resume target is discarded, no exec is attempted, and the user is returned to the launching TUI screen

#### Scenario: Success without target returns to launching screen
- **WHEN** a workflow run ends with outcome success and no resume target was recorded
- **THEN** no exec is attempted; the user is returned to the launching TUI screen as for any other successful workflow run

### Requirement: Invalid resume target falls back gracefully

If the recorded resume target run id does not resolve to a known run at workflow-end time (the session directory does not exist or is unreadable), agent-runner SHALL NOT attempt the exec. It SHALL surface an inline error on the launching TUI screen identifying the invalid target id and the reason (not found, unreadable, etc.), and SHALL otherwise return to that screen as for any other completed workflow.

#### Scenario: Unknown target id falls back
- **WHEN** the recorded resume target run id has no corresponding session directory at workflow-end time
- **THEN** no exec is attempted, an inline error is shown on the launching TUI screen naming the missing run id, and the user is returned to that screen

#### Scenario: Unreadable target session falls back
- **WHEN** the recorded resume target run id resolves to a session directory that is unreadable (permissions, IO error, missing `state.json`)
- **THEN** no exec is attempted, an inline error is shown on the launching TUI screen naming the run id and the read error, and the user is returned to that screen

### Requirement: Resume-handoff is launch-source-independent

The resume-handoff exec SHALL execute regardless of how the originating workflow was launched (home-tab menu, run-view keybinding, modal action, programmatic invocation). The replacement process SHALL behave as if `agent-runner --resume <target-run-id>` had been invoked from a fresh shell — it does NOT inherit the launching screen's drill-in path, selection, or any other TUI state. The resumed run SHALL be the target run identified by the signal, not the workflow run that emitted the signal.

#### Scenario: Launched from home menu
- **WHEN** the originating workflow was launched from the home TUI's new-workflow tab, ends success, and has a valid resume target
- **THEN** the in-place exec runs; the user lands in the run view of the target run as specified by the existing `--resume` behavior

#### Scenario: Launched from run-view keybinding
- **WHEN** the originating workflow was launched from a run-view keybinding (e.g. `d`), ends success, and has a valid resume target
- **THEN** the in-place exec runs; the previous run-view is not restored; the user lands in the run view of the target run

#### Scenario: Launched from modal action
- **WHEN** the originating workflow was launched from a TUI modal action (e.g. the onboarding-failure modal's Debug-now button), ends success, and has a valid resume target
- **THEN** the in-place exec runs; the modal is gone; the user lands in the run view of the target run

#### Scenario: Resumed run is target, not originating workflow
- **WHEN** the in-place exec runs
- **THEN** the run that is resumed is the run identified by the resume target, not the workflow run that emitted the signal

## Done When

All scenarios above are covered by unit tests and passing:

- `audit.BuildSummary` — happy path, redaction substitution, cap truncation with `truncated`/`dropped_events_count`, missing audit log (empty summary with `truncated: false`, `dropped_events_count: 0`, and `path` populated).
- `audit.Redact` — each pattern, non-matching strings preserved, surrounding context preserved.
- `resumehandoff.Read` — missing file → `ok=false`; well-formed content → trimmed id; whitespace-only → `ok=false`; trailing newline trimmed; unreadable marker file → `ok=false` with no error surfaced to the user.
- `agent-runner debug --state|--audit-summary|--show-workflow` — argv routing, success and failure exits, output shape (valid JSON for `--state` and `--audit-summary`; raw bytes for `--show-workflow`).
- `main.go` post-workflow handler — marker present + success outcome triggers `syscall.Exec`; marker absent or non-success outcome does not; invalid id surfaces inline error on the launching screen.

Integration:

- End-to-end resume-handoff: a fixture workflow that writes `<sessionDir>/resume-target` containing a valid run id and ends success → `main.go` post-workflow handler reaches the `syscall.Exec` branch with `agent-runner --resume <id>`. Exec call is observable (e.g. swap the exec impl for a recorder in the test) so the test does not actually replace the process.

`make test`, `make lint`, and `make build` all pass; `make fmt` was run before committing.
