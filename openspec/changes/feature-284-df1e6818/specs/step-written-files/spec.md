## ADDED Requirements

### Requirement: Steps record the workflow output files they wrote

For each executable leaf step (agent, shell, and script steps, including repair-cycle check runs), Agent Runner SHALL observe the run's workflow output root, `{{session_dir}}/output/`, at the step's start and at its end. It SHALL record on that step's `step_end` event every file under the root that the step created, modified, or deleted. Each entry SHALL identify the file's path relative to the output root, its change kind (`created`, `modified`, or `deleted`), and the content identity of the version before the step (absent for `created`) and after the step (absent for `deleted`).

Files that Agent Runner itself maintains under the output root SHALL NOT be recorded as step-written. This includes the per-step output stream files Runner writes for its own run view. Files outside the output root SHALL NOT be observed by this capability, including Runner bookkeeping in the session directory (`audit.log`, `run-metrics.json`, `state.json`, validator metrics), audit snapshots, Runner's evidence store, and `{{session_dir}}/scratch/`.

Container steps (loops, groups, sub-workflows) SHALL NOT record written files of their own. A write made during a leaf step by an agent that the step called belongs to that leaf step. When an executable step runs inside another executable step's observation window (for example a repair cycle's repair agent or internal check run inside the owning check), each write SHALL be recorded exactly once, on the step that was running when it happened. The enclosing step SHALL record its own writes from before the nested step started and after it ended, as separate segments, and SHALL NOT lose them. A step whose observation found no change SHALL record an explicit empty list, not omit the field, so that "wrote nothing" stays distinguishable from "not observed".

#### Scenario: Step creates a findings file
- **WHEN** an agent step writes `{{session_dir}}/output/acceptance-findings.md`, which did not exist when the step started
- **THEN** its `step_end` lists `acceptance-findings.md` as `created` with the content identity of the version the step left

#### Scenario: Step appends to an existing ledger
- **WHEN** a step appends a section to `{{session_dir}}/output/acceptance-assumptions.md`, which an earlier step created
- **THEN** its `step_end` lists the file as `modified` with distinct before and after content identities

#### Scenario: Step writes nothing
- **WHEN** a step completes without changing any file under the output root
- **THEN** its `step_end` records an empty written-file list

#### Scenario: Runner-maintained files are not credited
- **WHEN** a step runs and Runner updates `audit.log`, `run-metrics.json`, and that step's own output stream files
- **THEN** none of those files appears in the step's written-file list

#### Scenario: Scratch work is not observed
- **WHEN** a step clones a repository and builds it under `{{session_dir}}/scratch/`
- **THEN** none of those files appears in the step's written-file list

#### Scenario: Container step does not double-record
- **WHEN** a loop iteration's leaf step writes an output file
- **THEN** the leaf step's `step_end` records the write and the enclosing loop's and iteration's end events do not

#### Scenario: Repair check run claims its own writes
- **WHEN** a check step's internal repair-cycle run writes an output file while the owning check step is still running
- **THEN** the internal run's `step_end` records the write and the owning check's `step_end` does not record it again

#### Scenario: Owning check keeps its own earlier write
- **WHEN** a failing check appends finding A to a ledger, its repair agent appends B, and its internal check run appends C to the same ledger
- **THEN** the owning check's `step_end` records A, the repair agent's records B, and the internal run's records C, each exactly once

### Requirement: Step-written file versions and deltas are retained durably

For each recorded `created` or `modified` file, Agent Runner SHALL retain in the run directory a bounded copy of the version the step left and the step's contribution to it. Retained evidence SHALL be addressed by content identity, so a later step that overwrites the file does not destroy an earlier step's retained version or contribution. When the version before the step has not been retained, Agent Runner SHALL try to retain it as the baseline when the step starts, within the same bounds.

A file's content identity is complete only when Runner hashed its entire content. A file whose identity is incomplete because it exceeds the hashing bound SHALL be treated as changed whenever its file metadata changed. Matching partial hashes SHALL NOT establish that its content is unchanged, and its contribution SHALL be unresolved.

The step's contribution SHALL be determined as follows:

- **Append:** when the before identity is complete and the after version begins with exactly the before content, the contribution is the appended bytes. This holds even when the before version itself was too large to retain.
- **Other edits:** when both versions are completely retained, the contribution is a line diff in which unchanged lines appear only as context, never as added content.
- **Created:** for a created file, the contribution is its whole content.
- **Unresolved:** otherwise the contribution SHALL be recorded as unresolved with a reason, and only a whole-file excerpt of the after version is kept, as context.

Retention SHALL be bounded per file and per step. A contribution exceeding its bound SHALL be truncated and marked truncated. When a file is binary or not valid UTF-8 text, Runner SHALL record its identity and size without a textual contribution and mark the contribution unavailable.

#### Scenario: Later round overwrites a file
- **WHEN** acceptance round 1 writes `acceptance-findings.md` and round 2 later rewrites it
- **THEN** round 1's retained version and contribution remain available after round 2 completes

#### Scenario: Append is retained as a delta
- **WHEN** a step appends one section to a ledger that already contains earlier steps' sections
- **THEN** the retained contribution for that step contains the appended section and not the earlier sections

#### Scenario: Append to a ledger larger than the retention bound
- **WHEN** a step appends one section to a ledger whose earlier content exceeds the per-file retention bound
- **THEN** the retained contribution is exactly the appended section

#### Scenario: Scattered edits keep the unchanged middle as context
- **WHEN** a step edits a ledger's heading and its footer, and an earlier finding between them is unchanged
- **THEN** the retained contribution shows the two edits as changes and the unchanged finding only as context

#### Scenario: Edit to a file too large to diff
- **WHEN** a step rewrites the middle of an output file larger than the retention bound
- **THEN** the contribution is recorded as unresolved with a reason, and only a context excerpt of the new version is kept

#### Scenario: Same-size change to a file beyond the hashing bound
- **WHEN** a step rewrites part of an output file larger than the hashing bound without changing its size
- **THEN** the file is recorded as modified with an incomplete identity and an unresolved contribution, not as unchanged

#### Scenario: Baseline cannot be retained
- **WHEN** Runner cannot retain a file's before version at step start and the step then edits the file without appending
- **THEN** the contribution is recorded as unresolved with a reason

#### Scenario: Large file exceeds the bound
- **WHEN** a step creates an output file larger than the per-file retention bound
- **THEN** Runner retains a truncated portion and marks the retained evidence as truncated

#### Scenario: Binary output file
- **WHEN** a step writes a binary file under the output root
- **THEN** the written-file entry records its identity and size and marks the textual contribution unavailable

### Requirement: Step repository changes are retained as bounded patches

When a step has an attributable working-tree delta under the existing Git checkpoint rules, Agent Runner SHALL retain a bounded patch of that delta from the step's starting checkpoint to its ending checkpoint, referenced from the step's `step_end`. Each changed path's contribution SHALL follow the same append, diff, created, and unresolved rules as step-written output files. When the patch exceeds its bound, Runner SHALL keep a truncated portion and mark it truncated. Runner SHALL NOT copy whole repository files at step end.

#### Scenario: Uncommitted change is retained as a patch
- **WHEN** a step edits a tracked repository file without committing it
- **THEN** its `step_end` references a retained patch showing that step's edit

#### Scenario: Later step changes the same file again
- **WHEN** a later step edits the same repository file before anything is committed
- **THEN** each step's retained patch shows only the change made between its own start and end checkpoints

#### Scenario: Git checkpoint unavailable
- **WHEN** a step's starting or ending Git checkpoint is unavailable
- **THEN** no patch is retained for that step and its step record states that the patch is unavailable

### Requirement: Written-file observation is best-effort and bounded

Observation and retention SHALL be bounded per step, across both of its boundaries and its repository baselines: in directory entries visited, directory depth, bytes read, bytes stored, and elapsed observation time. Observation SHALL read only regular files and SHALL NOT follow symbolic links. Reaching a bound, an unreadable file, or any other observation or retention error SHALL be recorded on the step's `step_end` as truncated or unavailable written-file coverage, with a reason. Such a condition SHALL NOT change the step's outcome, fail the run, or prevent later steps from running.

#### Scenario: Output root holds more files than the enumeration bound
- **WHEN** the output root contains more files than the per-step enumeration bound
- **THEN** the step completes with its normal outcome and its `step_end` marks written-file coverage as truncated

#### Scenario: Read or time budget is exhausted
- **WHEN** the output root holds files whose total size exceeds the per-step read budget, or observation reaches its time budget
- **THEN** observation stops, the step continues with its normal outcome, and its `step_end` marks written-file coverage as truncated with the exhausted budget as the reason

#### Scenario: Symbolic link in the output root
- **WHEN** a step creates a symbolic link under the output root that points outside it
- **THEN** Runner does not read the link target, and the link is not recorded as written content

#### Scenario: Retention cannot be written
- **WHEN** Runner cannot write retained evidence, for example because the disk is full
- **THEN** the step's outcome is unchanged and its `step_end` marks the affected written-file evidence as unavailable with a reason

#### Scenario: Run recorded before this capability
- **WHEN** a run's `step_end` events have no written-file record
- **THEN** consumers treat its written-file evidence as unavailable because it was not recorded, and not as an empty list
