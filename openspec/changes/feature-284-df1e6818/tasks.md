- [x] Implement the change described by these files using TDD, as `CLAUDE.md` requires: write each failing test before the code that makes it pass. Satisfy every spec scenario in `specs/` and every `INT-*` / `E2E-*` obligation in `test-plan.md`. `design.md` is the technical source of truth; the bullets below summarize it.
  - [proposal.md](proposal.md), [design.md](design.md), [test-plan.md](test-plan.md), [decisions.md](decisions.md)
  - [specs/step-written-files/spec.md](specs/step-written-files/spec.md), [specs/audit-evidence-preparation/spec.md](specs/audit-evidence-preparation/spec.md), [specs/run-audit-replay/spec.md](specs/run-audit-replay/spec.md), [specs/workflow-value-observation/spec.md](specs/workflow-value-observation/spec.md)

  **Evidence store and contributions** (`internal/audit/stepevidence.go`, new)
  - Content-addressed blobs at `{{session_dir}}/step-evidence/blobs/sha256/<hex>`: `0600` permissions, written to a temporary file then renamed, never rewritten.
  - Types `FileVersion`, `Retained`, `FileWrite`, and `WriteRecord`, with the JSON fields in `design.md` §1. `files` is always non-nil.
  - Identity: a full SHA-256 only for files up to 16 MiB. Larger files get `IdentityComplete=false`, and equality falls back to size, `mtime_ns`, and inode. Never record or compare a partial hash.
  - `Contribution`, evaluated in order:
    1. binary → `unavailable`;
    2. `created` → the whole new content;
    3. `append` → the before hash matches the after file's first `before.Size` bytes, so the contribution is the exact appended bytes;
    4. `diff` → both versions are fully retained and at most 1 MiB, so use `github.com/rogpeppe/go-internal/diff.Diff`, with unchanged lines only as context;
    5. otherwise `unresolved`, with a reason and an after-version context excerpt.

    `deleted` has no contribution.
  - Bounds as constants:

    | Bound | Value |
    |---|---|
    | Files recorded per observation | 2,000 |
    | Directory entries visited per step | 10,000 |
    | Directory depth | 16 |
    | Bytes read per step | 64 MiB |
    | Time per boundary | 2 s |
    | Hash bound per file | 16 MiB |
    | Retained version per file | 1 MiB |
    | Contribution or context per file | 64 KiB |
    | Bytes stored per step | 8 MiB |
    | Store size per run | 512 MiB |

    Read, entry, and stored-byte counts and the deadline share one `observeBudget` per step, across both boundaries and both records. Inject the clock and read accounting for tests.
  - Open only regular files, relative to the output root (`os.Lstat`, `os.OpenRoot`). Never follow symbolic links, and do not record them.
  - Exhausting a budget stops deterministically and sets `truncated` with a reason. Errors set `unavailable`. Nothing propagates to the step.
  - `RunnerOwnedOutputName`: top-level `_*.out` and `_*.err`, their `.out.bak-*` and `.err.bak-*` archives, and `persistence-warnings.log`.

  **Recording hook** (`internal/audit/checkpoint.go`, `internal/metrics/pipeline.go`, `internal/runner/runner.go`)
  - `NewCheckpointLogger` and `NewExecutionPipeline` take a `sessionDir`, which `runner.go` passes. An empty value gives `output_writes` the status `unavailable` with reason `session directory unavailable`.
  - At executable `step_start` and `step_end` (the existing `executableStart` rule), observe `output/` and skip Runner-owned names. Use a hash cache keyed by `(path, size, mtime_ns, inode)` that rehashes files with a recent mtime. Retain the baseline and after-version blobs.
  - Segmented windows: a nested executable `step_start` closes the parent's current segment, and the nested step's end starts a new parent baseline. The parent's `output_writes.files` is all of its segments, in order.
  - `repository_patch`: at step start, retain versions of the paths that are dirty then. At step end, for `audit.ChangedDirtyPaths(start, end)`, take the before content from the start blob, or from `git cat-file blob <start.HEAD>:<path>` when the path was clean at the start. Compute contributions with the same rules. When a checkpoint is unavailable, `repository_patch` is `{status: unavailable, reason}`. Repository patches are not segmented.
  - Emit `output_writes` on every executable leaf's `step_end`. Emit `repository_patch` when the step has a Git delta, or as `unavailable` when a checkpoint is missing.

  **Snapshots** (`internal/devaudit/lifecycle.go`)
  - `exportGitEvidence`: write `git-patches/<sha>.patch` for each boundary commit using `git show --no-ext-diff --no-textconv --patch`. Cap each patch at 64 KiB with a marker line. Add `patch`, `patch_truncated`, and `patch_unavailable` per commit, so that one patch failure does not drop the commit metadata.
  - `snapshotReplayEvidenceAt`:
    - Stream `audit.log` through `visitAuditLines`, keeping only lines whose `execution_session_id` is in `allowed` and dropping lines that have none.
    - Copy the blobs referenced by included `step_end` events' `output_writes` and `repository_patch`. A missing blob stays absent.
    - Finish both before `sealSnapshot`.
  - The automatic snapshot already copies `audit.log` and `step-evidence/`. Only verify this.

  **Preparation** (`internal/devaudit/value_audit.go`, new `internal/devaudit/step_events.go`)
  - `readStepEvents` streams the snapshot's `audit.log`, keyed by `(execution_session_id, logicalPath(identity.prefix, identity.step_id))`, with per-attempt stdout, mode, `output_writes`, and `repository_patch`.
  - Narrative (or validation): one reference per attempt, final attempt first. When there is no output, give an unavailable reason: interactive, shell without capture, `audit.log` not in snapshot, or no output recorded.
    - A leaf is a validator leaf when metrics attribute an `agent-validator` `StepRecord` to its path, or when its script or command invokes `agent-validator`.
  - Artifact (or validation) contribution references:
    - `append`, `created`, and `diff` contributions are presented as contributions;
    - `unresolved` is presented as `context_possibly_inherited`;
    - `unavailable` carries no content.

    Add `EvidenceReference.Ref` set to `evidence/<snapshot path>`, or `audit.log#<line>` for step output.
  - Build commit references after `applyDeferredCommitAttribution`:
    - an `attributed` leaf gets `commit_patch` contributions;
    - deferred paths' `diff --git` sections are removed from the committing leaf's patch;
    - the earlier leaf gets a `commit_patch_confirmation`, which is context only.
  - Apply the artifact coverage table in `design.md` §5. `none_produced` requires all of these:
    - every attempt is `observed` with `files: []`;
    - both Git checkpoints are available;
    - there is no working-tree delta;
    - there is no attributed or deferred commit.

    `truncated` gives `partial`, which adds `partial_artifact` to `OmittedCategories`. `unavailable` and absent records give unavailable, with their reasons kept.
  - Remove `referencesForLeaf`'s record-id substring match. Remove the replay `replay-*-unavailable` injection for narrative, artifact, and validation. `evidenceCategory` stays only for indexing unattributed files.
  - Set `evidenceSchemaVersion = 2` and `rubricVersion = "value-rubric-v3"`.

  **Bounding** (replaces `boundLeafDetail`)
  - Budget 32 KiB of encoded JSON per leaf, allocated in order:
    1. fixed facts, at most 6 KiB, with `ChangedPaths` capped at 64 plus a count of the rest;
    2. at most 8 reference shells per category and 4 KiB in total, chosen deterministically, plus one `summarized` reference per category with the omitted count and `ref: evidence-index.json#leaf=<observation_id>&category=<c>`;
    3. a 12 KiB reserved share: final output up to 6 KiB, with the first contribution guaranteed up to 2 KiB, unused share passing between the two and the rest released to step 4;
    4. the remainder, in spec priority order, each category cut at a UTF-8 boundary with `Truncated` and a `truncated_<category>` marker.
  - The full reference list stays in `evidence-index.json`. `buildValuePackages` is unchanged.

  **Rubric prompt** (`valueAuditPrompt`)
  - Add the detection and correction crediting rule from `specs/workflow-value-observation/spec.md`. Treat `append`, `created`, `diff` (`+` and `-` lines only), and `commit_patch` as contributions. Give no credit for `context_possibly_inherited`, `commit_patch_confirmation`, or unresolved content. When output is missing, lower confidence and coverage.

  **Tests from `test-plan.md`**
  - Unit tests per `design.md` Verification, in `internal/audit`, `internal/liverun` (the generated names satisfy `RunnerOwnedOutputName`), and `internal/devaudit`.
  - INT-001 and INT-002 in `internal/runner`, using a `testdata/` workflow, a temporary Git repo, and a stub agent CLI for the repair agent. INT-002 continues through `snapshotEvidenceForProject` and `PrepareEvidence`, and skips when running as root.
  - INT-003 and INT-004 in `internal/devaudit` (`dev_audit` tag), using real Git and pipeline-produced run directories.
  - E2E-001 in `internal/devaudit/cli_e2e_test.go`: register a separate `spec-driven:audit-e2e-content` fixture in `e2e_fixture_enabled.go`, extend the fake `codex` to serve the source agent step, and leave `audit-e2e` unchanged. It must not depend on `automaticAuditEnabled`.
  - Do not add CI jobs. Everything runs under `go test -tags dev_audit ./...`.

  **Done when**
  - `make fmt`, `make lint`, and `make test` pass.
  - `openspec validate feature-284-df1e6818 --strict` passes.
