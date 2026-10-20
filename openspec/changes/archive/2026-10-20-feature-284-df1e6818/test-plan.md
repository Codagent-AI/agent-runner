## Coverage Strategy

The specifications remain the source of unit-test requirements. That unit coverage includes:

- line deltas;
- the Runner-owned output-name rule;
- observer windows and caps;
- step-event joins;
- per-leaf reference building;
- validator classification;
- budgeted bounding.

This plan records only the additional integration and end-to-end obligations, the acceptance testing envelope, and any exceptional human-only obligations.

The risk is concentrated at three boundaries:

1. **Recording.** The real Runner, executors, and Git together must produce correct `step_end` written-file records and evidence-store blobs.
2. **Snapshot boundary.** The automatic and replay snapshots must carry the right session-owned content into the audit.
3. **CLI replay journey.** This is the path and-scene evals actually use. It must deliver content-grounded packages to the judge.

Automatic auditing is paused (#191). Its CLI journey is therefore covered at the snapshot boundary (INT-004) instead of through a new E2E that would skip.

All tests run in the existing CI `test` job (`go test -tags dev_audit -timeout 300s ./...`). The E2E test uses the existing `devaudit_e2e` build-tag harness inside that job. No new jobs or suites are added.

## Integration Tests

### INT-001: Runner records step-written files and repository patches on `step_end`
- **Covers:** `step-written-files`:
  - Steps record the workflow output files they wrote (created, modified, deleted, and the empty list; Runner-maintained files and scratch excluded; containers don't double-record; repair check runs claim their own writes);
  - Step-written file versions and deltas are retained durably (later overwrite, append delta, binary file, size bound);
  - Step repository changes are retained as bounded patches (uncommitted edit; a later step edits the same file).
- **Boundary:** real `runner` execution of a workflow, through the shell, script, loop, and repair-check executors, `metrics.Pipeline`, `audit.CheckpointLogger`, the audit file logger, and real Git in a temporary repository. Agent steps are omitted here because E2E-001 covers them.
- **Setup:**
  - A temporary Git repository with one committed tracked file.
  - A test workflow under `testdata/` with these steps:
    - a step that creates `output/findings.md`;
    - a later step that appends a section to it;
    - a loop whose iteration writes `output/round.md`, which a second iteration overwrites;
    - a step that deletes an output file;
    - a step that writes only under `{{session_dir}}/scratch/`;
    - a check with inline repair: the failing initial check run appends `A` to `output/ledger.md`, the repair agent appends `B`, and the internal check run appends `C`. A stub agent CLI on `PATH` serves the repair agent;
    - a step that edits a ledger's heading and footer and leaves an earlier finding between them unchanged;
    - a step that appends to an output ledger whose earlier content exceeds 1 MiB;
    - a step that writes a binary file and one larger than the per-file retention bound;
    - two consecutive steps that edit the same tracked repository file without committing;
    - a step that writes nothing.
  - A pre-seeded Runner-owned name, `output/_x_.out`, that one step touches.
- **Action:** run the workflow headless to completion, then parse the run's `audit.log`.
- **Assertions:**
  - Each executable leaf's `step_end` has `output_writes` with exactly the expected paths and change kinds, before and after identities, and deltas that resolve to blobs under `step-evidence/blobs/sha256/`.
  - The append step's contribution is `append` and contains only the appended section.
  - The large-ledger append is still an exact `append`.
  - `A`, `B`, and `C` are each recorded exactly once, on the owning check (segment 0), the repair agent, and the internal run respectively.
  - The scattered-edit contribution is a `diff` whose unchanged finding appears only on ` ` context lines.
  - Iteration 1's retained version and delta still resolve after iteration 2.
  - The binary entry has `delta.unavailable`, and the oversized entry is marked truncated.
  - The no-write and scratch-only steps record `files: []`.
  - The loop and iteration end events carry no written-file entries, and the owning check carries no entry for `B` or `C`.
  - `_x_.out` never appears.
  - Each repository-editing step's `repository_patch` shows only its own edit.
- **Execution:** `internal/runner` package test in the CI `test` job.

### INT-002: Written-file observation failures never change outcomes and never become "none produced"
- **Covers:**
  - `step-written-files`: Written-file observation is best-effort and bounded (enumeration bound; read or time budget exhausted; symbolic link; retention cannot be written);
  - `audit-evidence-preparation`: Leaf evidence includes the step's written-file contributions (observation failed with an empty list).
- **Boundary:** real `runner` execution with the real `CheckpointLogger` and filesystem, then `snapshotEvidenceForProject` and `PrepareEvidence` on the resulting run directory.
- **Setup:** runs of a two-step shell workflow, one per condition:
  1. The enumeration bound lowered through a test hook, with more output files than the bound.
  2. The read budget and observation deadline lowered through test hooks, with an injected clock. The output root holds several files larger than the read budget and a nested tree deeper than the depth bound.
  3. `{{session_dir}}/step-evidence` pre-created read-only (`0500`), so retention fails.
  4. A step that writes nothing while observation is forced unavailable.

  One step also creates a symbolic link under `output/` pointing outside it.
- **Action:** run each workflow headless to completion.
- **Assertions:**
  - Both runs complete with every step `success`, and later steps run.
  - Runs 1 and 2 have `output_writes.status` `truncated`, with the bound or the exhausted budget as the reason.
  - In run 3, the affected entries or the whole record are `unavailable` with a non-empty reason.
  - No error event is emitted for the observation failure.
  - The symbolic link's target is never read, and the link is not recorded as written content.
  - After snapshot and preparation, no leaf from these runs reports artifact `none_produced` or `available` without contributions. Each leaf reports `unavailable` or `partial` with its recorded reasons, so coverage is never `complete`. That includes run 4's leaf, whose file list is empty.
- **Execution:** `internal/runner` package test in the CI `test` job. Skipped when running as root, where permission bits are not enforced.

### INT-003: Replay snapshot carries session-owned output and contributions into prepared leaves
- **Covers:**
  - `run-audit-replay`: Historical replay excludes evidence without durable session ownership (pre-change run; later session's output excluded; overwritten round retained).
  - `audit-evidence-preparation`: Leaf evidence includes the step's own output; Leaf evidence includes the step's written-file contributions; Leaf evidence attribution is explicit.
- **Boundary:** `snapshotReplayEvidenceAt`, `exportGitEvidence` against a real Git repository, `sealSnapshot`, and `PrepareEvidence` reading the real snapshot.
- **Setup:**
  - A source run directory produced through `metrics.Pipeline` with the real `CheckpointLogger`, across three execution sessions:
    - Session 1 runs these steps:
      - `audit`: its output reports two findings, and it appends a ledger section.
      - `audit-fix`: edits and commits with the `[audit-fix]` prefix.
      - `implement`: edits a file without committing.
      - `commit-all`: a commit-only step that commits `implement`'s edit together with one new file of its own.
      - an interactive-mode agent step without output.
      - a validator script step.
    - Session 2 re-runs `audit` with different output and overwrites the ledger.
    - Session 3 adds an unrelated step.
  - A second, legacy source run directory whose `step_end` events have no `output_writes`.
- **Action:** replay session 1 of the first run and session 1 of the legacy run, then run `PrepareEvidence` on each snapshot.
- **Assertions:**
  - The snapshot's `audit.log` contains only session-1 lines, plus lines from earlier lineage where present.
  - Only session-1-referenced blobs are copied.
  - `git-patches/<sha>.patch` exists for the `[audit-fix]` commit.
  - The `audit` leaf's narrative is session 1's response and its artifact contribution is session 1's ledger delta. Nothing from session 2 appears.
  - The `audit-fix` leaf has a `commit_patch` contribution and none of `audit`'s output.
  - `implement`'s contribution is its working-tree patch, plus a `commit_patch_confirmation` holding only its deferred path.
  - `commit-all`'s `commit_patch` contribution contains only its own new file's section.
  - The interactive leaf reports narrative unavailable with the interactive reason.
  - The validator step's output is classified `validation`.
  - Run-directory files without a written-file record are absent from every leaf, but the index remains complete.
  - Legacy run: leaves carry recorded output, and artifact is reported unavailable with the "not recorded" reason.
- **Execution:** `internal/devaudit` test (`dev_audit` tag) in the CI `test` job.

### INT-004: Automatic audit snapshot carries contribution evidence through bounded packages
- **Covers:**
  - `audit-evidence-preparation`: Automatic audit snapshots carry step contribution evidence; Default model input is bounded and coverage-aware (an evidence-heavy leaf keeps its output; a large output is excerpted).
  - `workflow-value-observation`: the inputs that let the judge separate detection from correction.
- **Boundary:** `snapshotEvidenceForProject` (copying the real run directory and exporting Git evidence), `sealSnapshot`, `PrepareEvidence`, and `buildValuePackages`.
- **Setup:** a run directory produced through the real pipeline in a temporary Git repository. It contains:
  - one leaf with 50 attempts, large Git detail, 2,000 written files (the enumeration bound), and a short final output;
  - one leaf whose final output exceeds the reserved share;
  - a detector and fixer pair as in INT-003.
- **Action:** take the automatic snapshot, mutate the source run directory afterwards, then prepare evidence.
- **Assertions:**
  - The snapshot contains `step-evidence/` blobs and commit patches, unaffected by the later mutation.
  - Every package is at most 256 KiB, every leaf at most 32 KiB, and every schema at most 96 KiB.
  - The evidence-heavy leaf's package entry contains its final output excerpt, at least one contribution excerpt, and at most 8 shells per category. It has a `summarized` reference per category with the omitted count, and the reference resolves to the complete list in `evidence-index.json`. It has `truncated_<category>` markers for the categories that were cut.
  - The large-output leaf's narrative is marked truncated and carries an `evidence/...` `ref` that resolves inside the model workspace.
  - The detector and fixer leaves carry their own output and `delta` or `commit_patch` contributions.
  - `evidence-index.json` has schema version 2.
- **Execution:** `internal/devaudit` test (`dev_audit` tag) in the CI `test` job.

## End-to-End Tests

### E2E-001: CLI replay delivers content-grounded value packages to the judge
- **Covers:** the and-scene journey, a headless run followed by `audit replay`. It exercises:
  - `audit-evidence-preparation`: the step's own output, written-file contributions, and none produced;
  - `run-audit-replay`: ownership-based inclusion;
  - `workflow-value-observation`: the judge receives content, and the rubric version is recorded.
- **Surface:** the tagged `agent-runner` binary: `--headless <workflow>`, then `audit replay <run> --session <id>`.
- **Setup:**
  - Extend the existing `cliAuditFixture` in `internal/devaudit/cli_e2e_test.go` (fake `codex`, fake `gh`, fake Sheets server, isolated `HOME`).
  - Register a second test-only workflow, `spec-driven:audit-e2e-content`, in `e2e_fixture_enabled.go`, and leave the existing `audit-e2e` fixture unchanged. The new workflow has:
    - a headless agent step, served by the fake `codex` when the prompt carries a source-step marker, that prints a final response and appends a section to `{{session_dir}}/output/ledger.md`, which an earlier shell step created;
    - a shell step that edits and commits a project file with its `[<step_id>]` prefix;
    - a step that writes nothing.
- **Journey:** run the workflow headless to completion, then replay its execution session.
- **Assertions:**
  - The replay audit directory's `value-packages.json` gives:
    - the agent leaf: its final response as narrative and only the appended ledger section as its `delta` contribution;
    - the commit step: a `commit_patch` contribution;
    - the no-write step: an artifact entry of `none_produced`.
  - No leaf reports `narrative_unavailable` or `artifact_unavailable` when it has recorded content.
  - The fake judge's recorded prompt input contains the agent's final response.
  - The delivered Sheets row has `rubric_version` `value-rubric-v3`.
  - The source repository's Git state is unchanged by the replay.
- **Execution:** `internal/devaudit` (`dev_audit` with a `devaudit_e2e` build of the binary) in the CI `test` job, alongside the existing E2E tests. It does not depend on `automaticAuditEnabled`.

## Acceptance Testing Envelope

- **Environments and sandboxes:**
  - The local checkout, run through `./dev.sh`, which builds with `dev_audit`.
  - Throwaway Git repositories and run directories under `{{session_dir}}/scratch/acceptance-test`.
  - An isolated `HOME` for any run that reaches the audit stages.
  - `scripts/sandbox-run.sh --dev-audit` and `scripts/docker-dev-audit-smoke.sh`, to confirm that Linux-confined judges can read the new snapshot content. Both need Docker.
- **Credentials and secrets:**
  - Real agent CLIs (Claude, Codex) authenticated on the host, which the pass may use for small headless source runs and for judge stages.
  - No Google Sheets or GitHub credentials may be used for audit delivery.
- **Authorized effects:**
  - Small real-agent headless runs and audit replays: a few runs, a few US dollars total in model cost.
  - Local files under scratch and the isolated `HOME`, removed after the pass.
  - Docker images and containers the smoke creates and removes itself.
- **Off limits:**
  - Delivering rows to the real development-audit Sheets dataset (`~/.agent-runner/development-audit-connection.json`).
  - Correctness-audit GitHub issue filing or commenting on real repositories.
  - Modifying or replaying audits in `~/.agent-factory/artifacts` or other real run directories in place.
  - Re-enabling automatic factory audits (#191).

  Use an isolated `HOME` without a connection file, plus the fake `gh` approach or `--no`-delivery equivalents.
- **Permitted substitutes:**
  - The fake `codex` judge and fake Sheets server from the E2E harness, when real-agent cost or confinement prerequisites are unavailable.
  - Copies of existing real run directories (for example a sampled and-scene run) as replay sources, provided the copy is replayed and the original is not.
- **Known risk areas:**
  - Runner-owned stream file names drifting from `RunnerOwnedOutputName`.
  - Nested executable windows (repair checks) double-claiming writes.
  - mtime-granularity hash-cache staleness on same-size rewrites.
  - Replay filtering of legacy `audit.log` lines that have no `execution_session_id` (accepted: they are dropped).
  - Non-append edits to output files over 1 MiB resolving to unresolved context (accepted).
  - Segmented nesting during repair cycles, and commit-patch splitting for mixed deferred commits.
  - Observation budgets degrading to truncated coverage under large output trees.
  - Workflows writing evidence outside `{{session_dir}}/output/` getting no artifact credit (accepted).
  - Shell steps without `capture` reporting no output (accepted).
  - Package batching producing more judge batches, which adds judge cost.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| step-written-files: Steps record the workflow output files they wrote | INT-001 | E2E-001 | — |
| step-written-files: Step-written file versions and deltas are retained durably | INT-001 | E2E-001 | — |
| step-written-files: Step repository changes are retained as bounded patches | INT-001 | — | — |
| step-written-files: Written-file observation is best-effort and bounded | INT-002 | — | — |
| audit-evidence-preparation: Leaf evidence includes the step's own output | INT-003 | E2E-001 | — |
| audit-evidence-preparation: Leaf evidence includes the step's written-file contributions | INT-002, INT-003, INT-004 | E2E-001 | — |
| audit-evidence-preparation: Leaf evidence attribution is explicit | INT-003 | — | — |
| audit-evidence-preparation: Automatic audit snapshots carry step contribution evidence | INT-004 | — | — |
| audit-evidence-preparation: Default model input is bounded and coverage-aware (modified) | INT-004 | — | — |
| run-audit-replay: Historical replay excludes evidence without durable session ownership (modified) | INT-003 | E2E-001 | — |
| workflow-value-observation: Value credit separates detection from correction | INT-004 | E2E-001 | — |
| Journey: headless run, then CLI audit replay, then the judge receives content | — | E2E-001 | — |
