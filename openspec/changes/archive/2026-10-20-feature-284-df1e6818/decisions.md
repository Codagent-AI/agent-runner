# Decisions

## propose

### Verdict: go with caveats
- **Decision:** Build it. The problem is confirmed in code. Replay snapshots keep only `run-metrics.json`, and leaf attribution is a record-id substring match. Step output already exists in `audit.log` with durable `execution_session_id` ownership.
- **Alternatives considered:** no-go, on the grounds that audits are off in the factory; prompt-only changes that tell the judge to drill into the snapshot (the replay snapshot has no content to drill into).
- **Decision-bearing:** yes

### Replay includes `audit.log` events filtered by execution session
- **Decision:** The replay snapshot includes `audit.log` events from the selected session and earlier sessions in its lineage, and excludes events from later sessions.
- **Alternatives considered:** keep excluding `audit.log` (leaves replays, the only path producing data today, without output); include the whole log unfiltered (breaks the replay ownership rule).
- **Decision-bearing:** yes. It modifies `run-audit-replay`, but stays consistent with that spec's principle that evidence is included only when session ownership is durable, so this is not a stop.

### "Files the step wrote" means files in the repository working tree and the run's session directory
- **Superseded in part by PR-001 and PR-002:** the run-side boundary is now `{{session_dir}}/output/` only, and repository contributions are patches rather than snapshot-time content.
- **Decision:** The runner records step-written files under `{{session_dir}}` with bounded end-of-step copies. Repository files use the existing Git attribution plus snapshot-time content with a fingerprint freshness check. External directories (for example the factory `{{artifact_dir}}`) and gitignored repository files are out of scope.
- **Alternatives considered:** watch the whole filesystem (not feasible or portable); workflow-declared evidence paths (a larger interface change, left as a follow-up); copy repository files at every step end (duplicates the working tree).
- **Decision-bearing:** yes. The issue's examples (audit findings, acceptance ledger, verify evidence, validator output) are written mostly to the run's session directory or appear as step output in eval workflows, so this reading covers the issue's intent. Recorded as an assumption, not a stop.

### End-of-step copies live in the run directory
- **Superseded by PR-002:** the run directory now stores versions and bounded deltas in a content-addressed store, not whole end-of-step copies.
- **Decision:** Bounded copies of session-directory files go to a runner-owned per-step evidence directory, so replays can see content that later rounds overwrote.
- **Alternatives considered:** fingerprint only and read content at snapshot time (loses overwritten round files and cannot prove ownership on replay).
- **Decision-bearing:** yes (persisted run-directory layout; the change is additive)

### Remove filename-heuristic attribution from leaves
- **Decision:** Leaves receive only evidence attributed by step path and execution session. Unattributed files stay in the index for drill-in.
- **Alternatives considered:** keep the heuristic as a fallback (it produces misattributions such as `audit` matching `audit-fix`).
- **Decision-bearing:** no

### Correctness audit and value dataset schema unchanged
- **Decision:** Scope is the value judge's evidence. The correctness audit benefits from the same index, but its rubric and the Sheets dataset row do not change.
- **Alternatives considered:** extend the change to correctness prompts and dataset columns.
- **Decision-bearing:** no

## proposal-review

### PR-001: the recording boundary conflates changed-during-step with produced-by-step (applied)
- **Decision:** Applied. Checked against the code: Runner writes `audit.log` and `run-metrics.json` into the session directory during steps, and verify-change puts clones and build output under `{{session_dir}}/scratch/`. Observation now covers only the workflow output root `{{session_dir}}/output/`. It excludes Runner bookkeeping, the evidence store, audit snapshots, and scratch. It is bounded and best-effort, records truncated or unavailable coverage, and never fails the step. Runner logs reach the judge through their own channels.
- **Alternatives considered:** observe the whole session directory with an exclusion list (more fragile, and it still scans scratch trees); observe only workflow-declared paths (an interface change, kept as a follow-up).
- **Decision-bearing:** yes. Workflows that write evidence elsewhere in the session directory get no artifact credit. This is recorded as a risk.

### PR-002: owning a captured version is not authoring its content (applied)
- **Decision:** Applied. Checked against the code: tracked-file fingerprints hash `git diff` streams (`internal/audit/checkpoint.go`), and the acceptance assumptions ledger is cumulative. Step-written files now record before/after version linkage with bounded deltas in a content-addressed store, including baselines for pre-existing files. Repository contributions are bounded commit patches plus a runner-kept working-tree patch from start to end checkpoint. Whole-file content is context only, marked inherited or unresolved. Freshness no longer compares fingerprints with snapshot-time bytes.
- **Alternatives considered:** keep end-version content with a freshness check (cannot separate inherited content from new content, and fingerprints are not content hashes).
- **Decision-bearing:** yes (persisted evidence-store layout; the change is additive)

### PR-003: existing priority and truncation can drop all content (applied)
- **Decision:** Applied. Checked against the code: `referencesForLeaf` embeds full metric records, narrative has the lowest priority, and `boundLeafDetail` drops trailing references. Each leaf now reserves a fixed share of its 32 KiB for step output and contribution excerpts. Metrics become compact facts, and verbose metric revisions, Git detail, and whole-file context are trimmed separately, with category-specific truncation markers and stable local references. Package limits are unchanged.
- **Alternatives considered:** raise the per-leaf bound (conflicts with the existing package and schema limits); rely on drill-in (does not ground the default judgments).
- **Decision-bearing:** no (stays within the existing spec bounds; the exact reserved size is a design decision)

## spec

### Capability set: workflow-value-observation replaces automatic-run-audit
- **Decision:** The detector/fixer crediting rule goes into `workflow-value-observation`, which owns the value rubric. Automatic-snapshot contents go into `audit-evidence-preparation`, which already owns the audit-launch snapshot. No `automatic-run-audit` requirement changes, and the proposal's capability list is updated to match.
- **Alternatives considered:** add a snapshot requirement to `automatic-run-audit` (that spec covers launch and lifecycle, not snapshot contents).
- **Decision-bearing:** no

### Step output scope: use what `step_end` already records
- **Decision:** A leaf's output is the agent's final response (`step_end` stdout is the adapter's final response, not the raw stream) and shell or script output where `step_end` already records it. A shell step without `capture` keeps recording no stdout, so the `audit-log-entries` contract is unchanged, and its narrative is reported unavailable.
- **Alternatives considered:** retain every shell step's uncaptured stdout as step evidence (changes the "no stdout without capture" contract, and the issue asks for agent steps' output); read Runner's per-step `.out` stream files (they have no durable session ownership and a re-run overwrites them).
- **Decision-bearing:** yes. Shell steps without capture get no narrative evidence.

### Written-file record details
- **Decision:** Change kinds are `created`, `modified`, and `deleted`. An explicit empty list means "wrote nothing", and an absent record means "not recorded" (pre-change runs). Container steps don't record writes, and writes by called agents belong to the calling leaf. Binary files get identity and size only. Runner's own per-step stream files under `output/` are excluded. Observation and retention failures never change a step's outcome.
- **Alternatives considered:** record only created and modified (hides deletions); treat an absent record as empty (would report old runs as having produced nothing).
- **Decision-bearing:** no

### "None produced" differs from "unavailable"
- **Decision:** A step with an empty written-file list and no repository change reports that no artifact was produced. This is distinct from unavailable, so honest no-output steps don't lower coverage for the wrong reason.
- **Alternatives considered:** keep reporting `artifact_unavailable` for every step without files (the current behavior, which the issue identifies as noise).
- **Decision-bearing:** no

### Reserved content share: size deferred to design
- **Decision:** The spec requires a fixed reserved share of each leaf's 32 KiB for output and contribution excerpts, unused share released to other evidence, and per-category trimming with markers. The exact size is left to design.
- **Alternatives considered:** fix a number in the spec (premature before measuring typical output sizes).
- **Decision-bearing:** no

## design

### Hook at `audit.CheckpointLogger`
- **Decision:** Output-root observation and repository patches run in `CheckpointLogger.Decorate`, beside the existing Git checkpoints. It already brackets every executable leaf synchronously and never fails a step.
- **Alternatives considered:** hooks in each executor (duplicated across agent, shell, script, and repair paths).
- **Decision-bearing:** no

### Records go on `audit.log` `step_end`, not `run-metrics.json`
- **Decision:** `output_writes` and `repository_patch` are added to `step_end` data. `run-metrics.json` is unchanged.
- **Alternatives considered:** project the records into `StepRecord` (changes an artifact that external consumers read, and preparation would still need `audit.log` for step output).
- **Decision-bearing:** yes (additive persisted audit-log fields)

### Content-addressed store at `{{session_dir}}/step-evidence/blobs/sha256/`
- **Decision:** Blobs are named by the hash of their bytes, written atomically, and never rewritten. Bounds: 2,000 files enumerated, 16 MiB hashed per file, 256 KiB per retained version, 64 KiB per delta, 8 MiB per step, 512 MiB per run.
- **Alternatives considered:** per-step directories (no deduplication, so baselines are rewritten at every step).
- **Decision-bearing:** yes (persisted run-directory layout; additive)

### Own line delta instead of a diff library or `git diff --no-index`
- **Decision:** A single prefix/suffix-trimmed hunk, which is exact for appends.
- **Alternatives considered:** add a diff dependency; shell out to Git (would make output-root evidence depend on Git).
- **Decision-bearing:** no

### Nested executable windows claim their own writes
- **Decision:** A closing inner window advances its enclosing windows' baselines, so a repair check's internal run and the owning check don't both record the same write. A spec scenario was added for this.
- **Alternatives considered:** let both record it (double credit).
- **Decision-bearing:** no

### Runner-owned output names
- **Decision:** `audit.RunnerOwnedOutputName` matches `_*.out` and `_*.err`, their `.bak-*` archives, and `persistence-warnings.log`. A liverun test keeps it in sync with the names liverun generates.
- **Alternatives considered:** a registry file written by liverun (more moving parts).
- **Decision-bearing:** no

### Validator leaf classification
- **Decision:** A leaf is a validator leaf when metrics attribute an `agent-validator` record to its path, or when its script or command invokes `agent-validator`.
- **Alternatives considered:** filename patterns (the heuristic this change removes).
- **Decision-bearing:** no

### Reserved content share of 12 KiB, and version bumps
- **Decision:** 12 KiB of each leaf's 32 KiB is reserved for output and contributions, with the final attempt's output first. `evidenceSchemaVersion` becomes 2 and `rubricVersion` becomes `value-rubric-v3`.
- **Alternatives considered:** 8 KiB (too small for typical final responses plus deltas); 16 KiB (squeezes metrics and Git facts).
- **Decision-bearing:** no

## test-plan

### Automatic path covered at the snapshot boundary, not by a new E2E
- **Decision:** Automatic auditing is paused (#191), and the existing automatic CLI E2E tests skip. INT-004 exercises `snapshotEvidenceForProject` and preparation directly. The CLI E2E covers the replay journey that and-scene evals use.
- **Alternatives considered:** add assertions to the skipped automatic E2E tests (they would never run while auditing is paused).
- **Decision-bearing:** no

### New E2E fixture workflow instead of extending `audit-e2e`
- **Decision:** Register a separate test-only `spec-driven:audit-e2e-content` workflow. Existing E2E assertions (for example a single observation) stay untouched.
- **Alternatives considered:** extend `audit-e2e` (breaks existing observation-count assertions).
- **Decision-bearing:** no

### Acceptance envelope keeps audit delivery off real services
- **Decision:** The acceptance pass may spend a few dollars on real-agent runs and replays, but must use an isolated `HOME` without the Sheets connection, must not file GitHub issues, and must replay only copies of real run directories.
- **Alternatives considered:** allow delivery to the real dataset (would pollute the value data used for workflow decisions).
- **Decision-bearing:** no

### No human-only testing
- **Decision:** Every obligation can be checked by an agent with the tools and fakes available.
- **Alternatives considered:** a human review of judge note quality (subjective, and outside the spec's observable contract).
- **Decision-bearing:** no

## approach-review

### AR-001: reference shells can overflow the 32 KiB leaf (applied)
- **Decision:** Applied. Checked against the code: `buildValuePackages` errors when a single leaf exceeds a package. The full reference list stays in the local `evidence-index.json`. The package carries at most 8 shells per category (4 KiB total, measured as encoded JSON), chosen deterministically, plus a `summarized` reference per category with the omitted count and a pointer to the full list. Fixed facts are capped at 6 KiB (`ChangedPaths` capped at 64 entries). Steps 1 to 3 total at most 22 KiB, so the final output and the first contribution always fit. Spec scenario and INT-004 updated (50 attempts, 2,000 files).
- **Alternatives considered:** allow more batches (cannot shrink an oversized leaf); drop shells silently (violates coverage honesty).
- **Decision-bearing:** no

### AR-002: partial baselines and partial hashes (applied)
- **Decision:** Applied. Identity is a full-content hash only, up to 16 MiB. Above that the identity is incomplete: no partial hash is recorded, and equality requires size, mtime, and inode to be unchanged. Contributions are computed in order: binary, created, then append by prefix hash (exact without the before bytes, so appends to large ledgers stay exact), then a diff when both versions are completely retained (up to 1 MiB each), and otherwise unresolved context. Retained versions are now up to 1 MiB. Spec scenarios added for large-ledger appends, edits beyond the bound, same-size changes beyond the hash bound, and baseline failure.
- **Alternatives considered:** keep a fixed-size tail of every file (complex, and still not exact for edits); retain everything (unbounded).
- **Decision-bearing:** no

### AR-003: nested windows erased the parent's earlier writes (applied)
- **Decision:** Applied. Checked against the code: `internal/exec/repair.go` runs the initial check inside the owning window before the nested repair and internal runs. Windows are now segmented: a child's start closes the parent's current segment, and the child's end starts a new parent baseline. The parent records all its segments. Spec scenario and INT-001 updated (A, B, C ledger appends).
- **Alternatives considered:** keep baseline advancement (loses A); let both windows record everything (double credit).
- **Decision-bearing:** no

### AR-004: a single-hunk delta presented unchanged lines as added (applied)
- **Decision:** Applied. Contributions use `rogpeppe/go-internal/diff`, an anchored multi-hunk unified diff that is already a direct dependency, so unchanged lines appear only as context. The append fast path is exact. Anything beyond the bounds is unresolved context with no credit. Spec scenario and INT-001 updated.
- **Alternatives considered:** write our own Myers diff (unnecessary given the existing dependency); mark single-hunk middles as possibly inherited (loses real edits).
- **Decision-bearing:** no

### AR-005: "none produced" could be inferred from failed observation (applied)
- **Decision:** Applied. The design now has a status-aware artifact coverage table. `none_produced` requires every attempt to be `observed` with an empty list, available Git checkpoints, no working-tree delta, and no attributed or deferred commit. A `truncated` or `unavailable` record yields `partial` or `unavailable` with the recorded reasons preserved. Spec requirement and scenario updated, and INT-002 extended through `PrepareEvidence`.
- **Alternatives considered:** none; the previous rule contradicted the spec.
- **Decision-bearing:** no

### AR-006: commit patches ignored deferred-commit attribution (applied)
- **Decision:** Applied. Checked against the code: `applyDeferredCommitAttribution` runs after references are built. Commit references are now built after deferral. A deferred step keeps its working-tree patch as its contribution and gets a `commit_patch_confirmation` (context, no credit). The committing step keeps only the patch sections for paths not deferred from earlier steps. Spec requirement and scenario updated, and INT-003 extended with an `implement`/`commit-all` pair.
- **Alternatives considered:** whole-commit credit to the committing step (double credit); drop mixed commits entirely (loses the committing step's own new files).
- **Decision-bearing:** no

### AR-007: no aggregate read or time budget (applied)
- **Decision:** Applied. One shared `observeBudget` per step, across both boundaries and both records: 64 MiB of reads, 10,000 entries, depth 16, and 2 s per boundary. Only regular files are opened, symbolic links are never followed, and paths open relative to the output root. Exhaustion stops deterministically with `truncated` coverage and a reason. Tests use an injected clock and read accounting. Spec scenarios added (budget exhausted, symbolic link), and INT-002 extended.
- **Alternatives considered:** asynchronous observation off the step path (complicates durability and ordering of `step_end`).
- **Decision-bearing:** no

## tasks

### One implementation task, in the established factory format
- **Decision:** `tasks.md` has a single checkbox task in the format used by the archived `feature-280-26fd2f2d` change. It requires TDD, links every artifact and spec, and summarizes the design by area, with the test-plan obligations and "done when" checks (`make fmt`, `make lint`, `make test`, `openspec validate --strict`).
- **Alternatives considered:** split into per-layer tasks (the request asked for exactly one).
- **Decision-bearing:** no
