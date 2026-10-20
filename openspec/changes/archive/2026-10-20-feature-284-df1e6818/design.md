## Context

The development audit's value stage (`internal/devaudit/value_audit.go`, build tag `dev_audit`) rates each value leaf from a bounded package that `PrepareEvidence` builds out of an immutable snapshot. Today, that package carries no step content:

- **Replay snapshots** (`snapshotReplayEvidenceAt` in `internal/devaudit/lifecycle.go`) contain only a session-filtered `run-metrics.json`, plus `git-evidence.json` from `exportGitEvidence`. `PrepareEvidence` then adds `replay-*-unavailable` references for `audit_log`, `validation`, `artifact`, `narrative`, and `native_session`.
- **Automatic snapshots** (`snapshotEvidenceForProject`) copy the whole run directory. But `referencesForLeaf` attaches a file to a leaf only when the file's path contains `record.ID`, and `evidenceCategory` guesses categories from file names. `audit.log` is discovered as `audit_log` but never attached to a leaf.
- **Bounding** (`boundLeafDetail`) drops whole trailing references until the leaf fits in 32 KiB. The metrics reference embeds every full `StepRecord` and sorts first, so narrative, which sorts last, is the first thing dropped.

Facts this design relies on:

- `audit.log` lines have the form `<ts> [<prefix>] <type> <json>`. They are parsed by `audit.parseAuditLine` and `visitAuditLines` (`internal/audit/summary.go`). Every event carries `execution_session_id`, added by `metrics.Pipeline.Emit`.
- An agent `step_end` carries `stdout`, which is the adapter's final response (`invocation.Response`), not the raw stream. It also carries `identity` (`execution_session_id`, `step_id`, `prefix`, `attempt`, `step_type`). A script `step_end` always carries `stdout`, capped at 4 KiB by `truncateForAudit`. A shell `step_end` carries stdout only when `capture` is set.
- `audit.CheckpointLogger.Decorate` runs synchronously inside `metrics.Pipeline.Emit`. It observes Git at every executable `step_start` (data has `command`, `script`, `title`, or `mode`) and at the matching `step_end`, keyed by event prefix. This is the existing boundary hook.
- Tracked-file `GitFileStat.Fingerprint` values hash `git diff` output, not file bytes (`fingerprintDiffStats`), so they cannot be compared with file content.
- Runner writes its own files into `{{session_dir}}/output/`. These are per-step stream files (`liverun.SanitizeOutputPrefix(prefix)+suffix+".out"/".err"`, whose names always begin with `_` because prefixes begin with `[`), their `*.bak-<ts>-<pid>-<seq>` archives, and `persistence-warnings.log`.
- Validator measurement projections in `run-metrics.json` are `StepRecord`s with `Tool: "agent-validator"`, whose `Prefix` is the owning leaf's logical path.
- The model workspace exposes the snapshot read-only as `evidence/` (`prepareModelWorkspace`).

## Goals / Non-Goals

**Goals:**
- Attach each value leaf's own recorded output and its file contributions (output-root deltas, working-tree patches, commit patches), attributed by step path and execution session, in both automatic and replay audits.
- Durably retain what each step changed under `{{session_dir}}/output/`, so replays of later runs see overwritten rounds.
- Guarantee that content reaches the judge's default package within the existing 32 KiB per-leaf and 256 KiB per-package bounds.
- Make the value rubric credit detection and correction from that content.

**Non-Goals:**
- Recording uncaptured shell stdout, capturing native transcripts, observing paths outside `{{session_dir}}/output/` and the repository, or recording gitignored repository files.
- Changing the correctness audit's rubric, the Sheets dataset columns, or `run-metrics.json`'s schema.
- Backfilling written-file records for runs recorded before this change.

## Approach

```
step_start / step_end ──► metrics.Pipeline.Emit
                            └─ audit.CheckpointLogger.Decorate
                                 ├─ observeGit (existing)
                                 ├─ outputObserver   → step_end["output_writes"]
                                 └─ repoPatcher       → step_end["repository_patch"]
                                        │ both write blobs to
                                        ▼
                         {{session_dir}}/step-evidence/blobs/sha256/<hex>

audit launch ──► automatic: copyEvidenceTree (already copies step-evidence/)
             └─► replay:    run-metrics.json (existing filter)
                            + audit.log filtered by execution_session_id
                            + blobs referenced by included step_end events
             └─► both:      exportGitEvidence + commit patches (git-patches/<sha>.patch)

PrepareEvidence ──► stepEvents index from snapshot audit.log
                   ──► per leaf: narrative/validation (output) + artifact/validation (contributions)
                   ──► budgeted bounding (reserved content share)
```

### 1. Evidence store (`internal/audit/stepevidence.go`)

The store lives at `{{session_dir}}/step-evidence/blobs/sha256/<hex>`. It is content-addressed by the SHA-256 of the stored bytes and written with `0600` permissions through write-to-temp-then-rename. An existing blob is never rewritten. The store is outside `output/`, so it is never observed, and `copyEvidenceTree` already copies it into automatic snapshots.

New types, serialized into `step_end` data:

```go
type FileVersion struct {
    SHA256           string `json:"sha256,omitempty"`  // full-content hash; empty when IdentityComplete is false
    Size             int64  `json:"size"`
    MtimeNS          int64  `json:"mtime_ns"`
    IdentityComplete bool   `json:"identity_complete"` // whole file hashed within the per-file hash bound
    Binary           bool   `json:"binary,omitempty"`  // NUL byte or invalid UTF-8 in the read window
    Blob             string `json:"blob,omitempty"`    // retained copy, store-relative
    BlobComplete     bool   `json:"blob_complete,omitempty"` // retained copy holds the entire file
}
type Retained struct {
    Kind        string `json:"kind"`                  // append | diff | created | unresolved | unavailable
    Blob        string `json:"blob,omitempty"`        // contribution bytes (append/created) or unified diff (diff)
    Context     string `json:"context,omitempty"`     // after-version excerpt blob, for unresolved only
    Bytes       int64  `json:"bytes"`
    Truncated   bool   `json:"truncated,omitempty"`
    Reason      string `json:"reason,omitempty"`      // why unresolved or unavailable
}
type FileWrite struct {
    Path    string       `json:"path"`             // relative to output root, or to repo top for repository_patch
    Change  string       `json:"change"`           // created | modified | deleted
    Segment int          `json:"segment,omitempty"`// 0-based observation segment of this step (see §2)
    Before  *FileVersion `json:"before,omitempty"`
    After   *FileVersion `json:"after,omitempty"`
    Contribution *Retained `json:"contribution,omitempty"`
}
type WriteRecord struct {
    Status string      `json:"status"`           // observed | truncated | unavailable
    Reason string      `json:"reason,omitempty"`
    Files  []FileWrite `json:"files"`            // always non-nil; [] means "wrote nothing"
}
```

`step_end` gains `output_writes` (a `WriteRecord`) on every executable leaf. It gains `repository_patch` (a `WriteRecord`) whenever both Git checkpoints are available and the step has a dirty-path delta. When a checkpoint is unavailable, `repository_patch` is `{status: unavailable, reason}`.

**Contribution computation** (`audit.Contribution(before, after FileVersion, beforeBytes, afterReader)`) is shared by output files and repository paths. It is evaluated in this order:

1. **Binary:** either version is binary. Result: `unavailable`, reason `binary content`.
2. **`created`:** the contribution is the whole new content, capped by the per-contribution bound.
3. **`append`:** `before.IdentityComplete` and `after.Size >= before.Size`, and the SHA-256 of the first `before.Size` bytes of the after file equals `before.SHA256` (one streaming read of the after file). The contribution is exactly bytes `[before.Size, after.Size)`, capped. This needs only the before hash, not the before bytes, so appends to ledgers of any size up to the hash bound stay exact.
4. **`diff`:** both versions are completely retained (`BlobComplete`) and each is at most 1 MiB. The contribution is `github.com/rogpeppe/go-internal/diff.Diff`, an anchored multi-hunk unified diff. That package is already a direct dependency. Unchanged lines appear only as ` ` context lines, never as `+`. The diff output is capped.
5. **`unresolved`:** every other case. Examples are an incomplete identity, a missing baseline, a version over 1 MiB, or a failed read. Result: reason set and `Context` = a capped excerpt of the after version. Preparation presents it as possibly inherited context and never as a contribution.

`deleted` keeps no contribution.

**Identity and change detection.** A file is hashed in full only when it is at most 16 MiB. Above that, `IdentityComplete = false` and no hash is recorded. Two observations are equal when both identities are complete with equal hashes. For an incomplete identity, they are equal only when size, `mtime_ns`, and inode are all unchanged. Otherwise the file is `modified`, and its contribution resolves to `unresolved`. A partial hash is never recorded or compared.

**Bounds** are package constants with tests at the edges:

| Bound | Value |
|---|---|
| Files recorded in the output root per observation | 2,000 |
| Directory entries visited per step (both boundaries plus repository baselines) | 10,000 |
| Directory depth below `output/` | 16 |
| Bytes read per step (hashing, append checks, baselines, diffs; shared by both boundaries and both records) | 64 MiB |
| Observation wall time per boundary | 2 s (checked between files and every 1 MiB of reads) |
| Hash bound per file | 16 MiB (above that, `IdentityComplete = false`) |
| Retained version copy per file | 1 MiB (`BlobComplete` when the whole file fits) |
| Retained contribution or context per file | 64 KiB |
| Bytes newly stored per step (both records) | 8 MiB |
| Store size per run | 512 MiB (beyond that, new retention is `unavailable: "evidence store full"`) |

A single `observeBudget` value carries the read, entry, and stored-byte counters and the deadline. It is created at the step's start boundary and reused at its end boundary and by `repoPatcher`. Tests inject a fake clock and a read-accounting filesystem, so no multi-gigabyte fixtures are needed.

Traversal uses `os.Lstat` and `os.OpenRoot`-relative opens. It opens only regular files and never follows symbolic links, which are skipped and not recorded. When a budget is exhausted, observation stops at a deterministic point: lexical path order, depth first. Unobserved paths are not reported as unchanged. The record's `Status` becomes `truncated` with reason `read budget exhausted`, `entry budget exhausted`, or `time budget exhausted`. A file whose start-time version was not observed and that appears at the end is reported with an `unresolved` contribution.

Reaching a per-file bound marks the affected entry truncated, and reaching the enumeration bound marks the record's `Status = truncated`. A filesystem error marks the entry, or the whole record, `unavailable` with the error text. Nothing is returned to the step, which follows the existing `CheckpointLogger` rule: never return errors or change outcomes.

### 2. Output-root observation (`outputObserver` inside `CheckpointLogger`)

`NewCheckpointLogger` and `metrics.NewExecutionPipeline` gain a `sessionDir` argument, which `runner.go:532` passes from the resolved run options. An empty `sessionDir` disables observation, and `output_writes` becomes `{status: unavailable, reason: "session directory unavailable"}`.

- **At an executable `step_start`:** enumerate `output/`, skipping Runner-owned names (`audit.RunnerOwnedOutputName`: top-level names beginning with `_` that end in `.out` or `.err` or contain `.out.bak-` or `.err.bak-`, plus `persistence-warnings.log`). Compute a `FileVersion` for each file. A per-logger cache keyed by `(path, size, mtime_ns, inode)` avoids rehashing unchanged files. Store a baseline blob, up to 1 MiB, for any version not already in the store. Push a window `{prefix, segment: 0, baseline map[path]FileVersion, recorded []FileWrite}` onto a stack.
- **Segmented windows for nesting:** when a nested executable `step_start` opens while a window is open, the observation taken for the child's start also closes the parent's current segment. The parent's changes since its baseline are computed, given `Segment = n`, and appended to `parent.recorded`. The parent is then suspended. When the child's `step_end` closes the child window, the observation taken for the child's end becomes the parent's new baseline, and the parent's segment counter increments. At the parent's own `step_end`, its final segment is computed and `output_writes.files` is all of `parent.recorded` plus that segment, in segment order. Every write is therefore recorded once, by the window that was active when it happened.

  Example: in a failing check, the initial check run appends A (parent segment 0), the repair agent appends B (child), the internal check run appends C (child), and nothing happens afterwards (parent segment 1 is empty). Each of A, B, and C is recorded exactly once, under its owner.
- **At the matching `step_end` of a non-nested window:** re-enumerate, compute created, modified, and deleted paths against the baseline, store the after-version blobs (up to 1 MiB), compute and store contributions, and emit `output_writes`.
- **Called agents** (`agent_call_*`) open no window, so their writes land in the calling leaf's window, as the spec requires.

`audit.RunnerOwnedOutputName` lives in `internal/audit`. A test in `internal/liverun` asserts that every name `openOutputFileWithSuffix` and the archive renamer can produce satisfies it, so the two cannot drift.

### 3. Repository patches (`repoPatcher` inside `CheckpointLogger`)

- **At an executable `step_start`:** after `observeGit`, compute `FileVersion`s and store blobs (up to 1 MiB each) for every path that is dirty at the start (index, worktree, or untracked, within the existing 256 and 256 path caps), reusing the hash cache and charging the shared `observeBudget`. Keep a `path → FileVersion` map in the window.
- **At `step_end`:** take `audit.ChangedDirtyPaths(start, end)` (the same paths that feed `GitEvidence.ChangedPaths`). For each path, the before version is the start `FileVersion` and blob when the path was dirty at the start. Otherwise it comes from `git cat-file -s` and `git cat-file blob <start.HEAD>:<path>`, read through the budget. A committed blob is always complete when it is within the bounds, and absent means created. The after content is the file on disk, and absent means deleted. Contributions use the same `audit.Contribution` rules. Nothing beyond blobs and contributions is stored, and unchanged repository files are never copied. Repository patches are not segmented: they keep the existing Git checkpoint nesting behavior.

### 4. Snapshots (`internal/devaudit/lifecycle.go`)

- **`exportGitEvidence`** (shared by automatic and replay) additionally runs `git show --no-ext-diff --no-textconv --format=%H%n%s --patch <sha>` for each boundary commit. It writes `git-patches/<sha>.patch`, capped at 64 KiB with a trailing truncation marker line, and records `patch` and `patch_truncated` on each `snapshottedGitCommit`. A failure sets `patch_unavailable` for that commit only, so commit metadata stays available.
- **`snapshotReplayEvidenceAt`** additionally streams the source `audit.log` through `visitAuditLines` and writes only lines whose `execution_session_id` is in the existing `allowed` set, dropping lines that have none. It then copies each blob referenced by an included `step_end`'s `output_writes` or `repository_patch` into `snapshot/step-evidence/blobs/...`. A missing blob is left absent, and preparation reports it unavailable. Both writes complete before `sealSnapshot`.
- **Automatic snapshots** need no copy changes. `audit.log` and `step-evidence/` are already copied.

### 5. Preparation (`internal/devaudit/value_audit.go`, new `step_events.go`)

- **`readStepEvents(snapshot)`** streams `audit.log` once. For each `step_end`, it keeps `{sessionID, logicalPath(identity.prefix, identity.step_id), attempt, stepType, mode (from the matching step_start), stdout, stdoutTruncated, outputWrites, repositoryPatch}`. The result is keyed by `(sessionID, logicalPath)`. Events without `identity` or `execution_session_id` are skipped. A missing `audit.log` returns "not snapshotted".
- **`leafFromRecords`** joins on `(request.ExecutionSessionID, path)`, the same key `buildLeaves` already groups by. It then builds the following references, each with a new `Ref string json:"ref,omitempty"` set to `evidence/<snapshot-relative path>` (`audit.log#<line>` for output). That gives the judge a stable drill-in target:
  - **`narrative`** (or `validation` for validator leaves): one reference per attempt, with the final attempt first. When there is no stdout, the reference is `{status: unavailable, detail: reason}`. Reasons are `interactive step records no final response` (mode is interactive), `shell step without capture records no output`, `audit.log not in snapshot`, or `no output recorded`.
  - **`artifact`** (or `validation` for validator leaves): one reference per `output_writes` entry and per `repository_patch` entry:
    - `append`, `created`, and `diff` contributions: `Detail` is the retained contribution, with `Contribution` set to that kind.
    - `unresolved`: `Detail` is the context excerpt, with `Contribution: "context_possibly_inherited"` and the reason.
    - `unavailable`: no content, with the reason.

    Commit patch references are built **after** `applyDeferredCommitAttribution`, from the finalized attribution (see below).
  - **Commit patches after deferral:**
    - A leaf whose final attribution is `attributed` gets one `artifact` reference per directly attributed commit, with `Contribution: "commit_patch"`.
    - When an earlier leaf has `deferred_commit` provenance for a commit, the committing leaf's patch is split by `diff --git` file sections. Sections for paths in any earlier leaf's `DeferralPaths` are removed from the committing leaf's contribution. The remaining sections stay as its `commit_patch` contribution, which is empty when every path was deferred.
    - The earlier leaf keeps its own working-tree patch as its contribution. It gains a `commit_patch_confirmation` reference, which is context with no credit and holds only its deferred paths' sections.
    - A committing leaf marked `no_change` whose patch has no remaining sections gets no commit contribution.
  - **Artifact coverage, decided per leaf in this order:**

    | Condition | Artifact status |
    |---|---|
    | Any attempt's `output_writes` is absent | `unavailable` — `written files not recorded for this run` |
    | Any attempt's `output_writes` or `repository_patch` is `unavailable` | `unavailable` — the recorded reasons |
    | Any attempt's `output_writes` or `repository_patch` is `truncated`, or any contribution is unresolved, unavailable, or truncated | `partial` — the recorded reasons, alongside the available references |
    | All records are `observed` and at least one contribution is available | `available` |
    | All `output_writes` are `observed` with `files: []`, both Git checkpoints are available, there is no working-tree delta, and there is no attributed or deferred commit | `none_produced` |
    | Otherwise (for example, Git checkpoints unavailable with an empty file list) | `unavailable` — `repository change unknown` |

    `missingEvidenceCategories` treats `available` and `none_produced` as satisfying `artifact`. A `partial` status adds `partial_artifact` to `OmittedCategories`, so coverage is never `complete`.
- **Validator leaf:** a leaf is a validator leaf when the artifact has any `StepRecord` with `Tool == "agent-validator"` and `Prefix == path`, or when its `step_start` `script`/`command` invokes `agent-validator`.
- **Removed heuristics:** `referencesForLeaf`'s substring match, and the replay `replay-*-unavailable` injection for `narrative`, `artifact`, and `validation`, are removed. Leaf-level references now state availability per leaf. `evidenceCategory` remains only to index unattributed snapshot files for drill-in.
- **Schema versions:** `evidenceSchemaVersion` becomes 2 and `rubricVersion` becomes `value-rubric-v3`. The Sheets row already carries `rubric_version`, so ratings from before and after the change stay distinguishable.

### 6. Budgeted bounding (replaces `boundLeafDetail`)

Each leaf has 32 KiB of encoded JSON, measured with `encodedJSONBytes`. The budget is allocated in order:

The complete per-leaf reference list always stays in the local `evidence-index.json`. The package carries a bounded projection of it:

1. **Fixed facts (at most 6 KiB):** skeleton, attempts, and omitted categories. The skeleton is already compact, and `ChangedPaths` is capped at 64 entries, with a count of the omitted entries.
2. **Bounded reference shells (at most 4 KiB, at most 8 per category):** shells (id, category, status, contribution kind, ref) are selected deterministically:
   - narrative or validation output: the final attempt first, then earlier attempts from newest to oldest;
   - contributions: in path then segment order, preferring available contributions over unresolved ones;
   - then the other categories.

   Each category with omitted shells gets one `summary` reference `{category, status: "summarized", omitted: <n>, ref: "evidence-index.json#leaf=<observation_id>&category=<c>"}`. Shells are measured as encoded JSON, including summaries and truncation markers, before any content is allocated.
3. **Reserved content share (12 KiB):** first the final attempt's output (up to 6 KiB, plus whatever contributions do not need), then contribution excerpts for the selected shells in selection order. At least the first selected contribution gets up to 2 KiB before the output takes more than 6 KiB. A share that one side does not use passes to the other. A share that neither uses is released to step 4.
4. **Remaining budget, at least 10 KiB, in spec priority order:** metrics detail (trimmed as text rather than dropped), Git detail, commit summaries, validation, artifact context excerpts, and earlier-attempt narrative.

Steps 1 to 3 total at most 22 KiB, so the final output and the first contribution always fit, whatever the cardinality of attempts and written files.

Each `Detail` is cut at a UTF-8 boundary to its allotment and sets `Truncated: true`. The leaf's `OmittedCategories` gains `truncated_<category>` instead of the generic `truncated_default_evidence`. A reference that receives no bytes keeps its shell and `ref`. Package batching (`buildValuePackages`) is unchanged.

### 7. Rubric prompt

`valueAuditPrompt` adds the rule from the spec. The step's own output and its `append`, `created`, `diff` (`+`/`-` lines only; ` ` lines are context), and `commit_patch` contributions are primary evidence of what it reported or changed. `context_possibly_inherited`, `commit_patch_confirmation`, and unresolved content earn no credit. Detection goes to the step whose output or delta first reported a problem, and correction to the step whose delta fixed it. Never rate both `unique` for one outcome. Missing output lowers confidence and coverage instead of inferring from adjacency.

## Decisions

- **The hook point is `CheckpointLogger`, not each executor.** It already brackets every executable leaf, runs before both the metrics projection and the durable sink, and has never-fail semantics. Per-executor hooks would duplicate this across agent, shell, script, and repair paths.
- **Records live in `audit.log` `step_end`, not `run-metrics.json`.** `audit.log` events already carry durable session ownership and are the source of step output, so one parser serves both content kinds. `run-metrics.json` stays quantitative, and its external consumers see no schema change.
- **The store is content-addressed under `{{session_dir}}/step-evidence/`.** Deduplication makes repeated baselines cheap. Overwrites cannot destroy earlier evidence. Placing it outside `output/` means it is never observed itself.
- **Contributions combine an append check by prefix hash with an anchored multi-hunk diff.** The prefix-hash append check stays exact for cumulative ledgers of any retained size. The diff, from `rogpeppe/go-internal/diff` (already a direct dependency), never presents unchanged interior lines as added. Anything else is unresolved context. Shelling out to `git diff --no-index` was rejected because it would make output-root evidence depend on Git.
- **Repository before-content comes from start blobs or `git show <startHEAD>:path`.** This avoids comparing diff-stream fingerprints with bytes, and copies only paths that are dirty at step start.
- **Segmented windows.** A nested executable step suspends its parent's window. The parent records its segments before and after the child, so each write has exactly one owner and the owning check's own earlier writes are kept. Existing Git attribution and repository patches keep their current nesting behavior.
- **Commit contributions follow the finalized deferral.** This keeps content credit consistent with the quantitative attribution that `applyDeferredCommitAttribution` already applies, and stops a commit-only step from receiving correction credit for an earlier step's edit.
- **One shared observation budget per step.** Reads, entries, stored bytes, and time are bounded across both boundaries and both records, so the worst-case cost of a synchronous `Decorate` call is fixed regardless of file count.
- **Validator classification uses metrics attribution first.** It is deterministic, with a narrow command fallback for validator scripts that have no measurements.
- **The 12 KiB reserved share** fits a typical final response (a few KiB) plus several small deltas. It leaves at least 20 KiB for facts and the other categories, and is easy to tune.

## Risks / Trade-offs

- **Boundary overhead.** Each executable step observes the output root twice. The cache keyed by size, mtime, and inode makes unchanged files cost one `stat`. The shared budget (64 MiB of reads, 10,000 entries, 2 s per boundary) caps the worst case, and exhaustion degrades to truncated coverage rather than delay.
- **Mtime-granularity cache misses.** A same-size rewrite within the filesystem's mtime resolution could reuse a stale hash. Including the inode, and rehashing files whose mtime is within 2 s of the observation time, mitigates this. Those rehashes are charged to the shared budget.
- **Disk growth per run.** This is bounded at 8 MiB per step and 512 MiB per run. Typical runs store kilobytes. Retention of run directories is unchanged.
- **Workflows writing evidence elsewhere** (`{{artifact_dir}}`, `scratch/`) get no artifact credit. This is accepted per the proposal and is visible in coverage.
- **Edits to files over 1 MiB** that are not appends resolve to unresolved context. That is accepted: such files are rare in workflow output, and honest coverage beats guessed authorship.
- **Replay `audit.log` filtering drops legacy lines without `execution_session_id`.** That is correct under the ownership rule, but very old runs get no output.

## Migration Plan

All changes are additive. Old runs replay with step output and `written files not recorded` coverage. Runs recorded during the rollout gain records automatically. In-flight audits prepared with schema v1 are loaded rather than re-prepared, so they finish unchanged. Rollback means reverting the change. Older binaries ignore the extra `step_end` fields and the `step-evidence/` directory.

## Verification

- **`internal/audit`** (unit, table-driven, using temporary directories and a temporary Git repo):
  - `Contribution`: append, append beyond 1 MiB of earlier content, scattered edit with an unchanged middle, create, an edit beyond 1 MiB, a same-size change beyond the 16 MiB hash bound (via a lowered test bound), a missing baseline, binary, and CRLF.
  - `RunnerOwnedOutputName`.
  - Observer windows: created, modified, deleted, empty, segmented nesting (A, B, C ledger appends), Runner-owned skip, symlinks skipped, per-file and enumeration caps.
  - Budget exhaustion with an injected clock and read accounting: many large files, a deep tree, and cache misses.
  - Store errors never surface to the step.
  - Repository-patch before-content sources.
- **`internal/liverun`**: generated stream and archive names satisfy `RunnerOwnedOutputName`.
- **`internal/devaudit`** (unit, `dev_audit` tag):
  - `readStepEvents` joins.
  - Per-leaf references for agent, script, interactive, and uncaptured-shell steps, `audit` vs `audit-fix`, retries, validator leaves, and pre-change runs.
  - Every row of the artifact coverage table.
  - Commit-patch splitting after deferral, including mixed commits.
  - Replay filtering of `audit.log` and blobs across three sessions.
  - Commit patch export.
  - Budget allocation with 2,000 written files and 50 attempts: the final output and first contribution survive, summaries carry counts, and the leaf, package, and schema bounds hold.
- **Integration:** extend `value_audit_integration_test.go` and `cli_e2e_test.go` with a fixture workflow whose steps write and append to `output/` files and commit. Assert that the packages carry the deltas and output in both the automatic and the replay paths.

## Open Questions

None.
