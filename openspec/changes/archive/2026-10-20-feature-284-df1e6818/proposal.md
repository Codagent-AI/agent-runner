## Why

The development audit's value judge rates each workflow step on its cost against its contribution.
Today it sees only the step's metrics: duration, cost, outcome, and Git change counts. It does not
see what the step said or what the step produced. In every sampled audit, all four content categories
were missing from every leaf: `artifact_unavailable`, `narrative_unavailable`,
`native_session_unavailable`, and `validation_unavailable`. That was 44 of 44 leaves in fix audit
`audit-a315bda27174e7d7` and 53 of 53 in and-scene eval audit `audit-b729df160ca7ebbc`.

A first pass over 182 audits (September 8 to October 19) found about 25% of judge notes empty. The
judge rarely sees findings from the audit, acceptance, test-flows, review, and triage steps, so it
infers credit from which steps ran next to each other. The step that found a problem and the step
that fixed it often both get rated "unique". Until the judge sees what each step produced, its
ratings cannot rank steps by cost against value. That ranking is the input to the planned continuous
loop that proposes and evaluates workflow improvements. Factory post-run audits are off for now (#191,
agent-factory#60), but and-scene evals still replay an audit for every run, so the replay path is the
one producing data today.

Three causes, from reading `internal/devaudit/value_audit.go` and `internal/devaudit/lifecycle.go`:

- **Replay audits throw away all content.** `snapshotReplayEvidenceAt` copies only `run-metrics.json`,
  because older runs could not prove which execution session produced the files in the run
  directory. Preparation then adds explicit "unavailable" references for validation, artifact,
  narrative, and native_session. The sampled audits are all replays. Yet `audit.log` already records
  each agent step's final output (`stdout` on `step_end`) and each shell step's output (capped at 4 KiB),
  and every event carries `execution_session_id`. Session ownership of those outputs is already
  durable. Replay just doesn't use it.
- **Automatic audits attribute files by name guessing.** `referencesForLeaf` links a snapshot file to
  a step only when the file's path contains the step's record id, and `evidenceCategory` classifies
  files by name patterns. `audit.log` itself is discovered but never attached to any leaf. Files a
  step writes under `{{session_dir}}/output` are attributed only when their names happen to contain
  the step id.
- **Nothing records which files a step wrote, other than repository paths.** Git checkpoints record
  the repository paths a step changed, with fingerprints, but not their content. Nothing at all
  observes writes to the run's session directory, where workflows put the acceptance ledger, verify
  evidence, and evidence-problem files.

## What Changes

- **Step output reaches the judge.** Evidence preparation reads `step_end` events from the
  snapshotted `audit.log` and attaches each value leaf's own output to that leaf: an agent step's
  final response, or a shell step's recorded output, including validator check output. Attribution
  uses the event's step path and execution session, not filename matching.
- **Replay audits keep ownership-proven content.** The replay snapshot includes `audit.log` events
  whose `execution_session_id` belongs to the selected session or an earlier one in its lineage.
  Later-session events are excluded. Old runs get step output on replay with no re-run needed.
- **The runner records which workflow output files each step wrote, and what it changed in them.**
  The observed boundary is the workflow output root, `{{session_dir}}/output/`, where workflows put
  findings, ledgers, and evidence. Runner bookkeeping (`audit.log`, `run-metrics.json`, `state.json`,
  validator metrics), the new evidence store, audit snapshots, and `{{session_dir}}/scratch/` are
  excluded. Runner logs keep reaching the judge through their own channels, such as step output from
  `audit.log`. Each step's end record gains a manifest of output files it created or modified. Each
  entry links the version before the step (or "created") to the version after it, and the runner
  stores a bounded delta between them. An append to a cumulative ledger such as
  `acceptance-assumptions.md` is therefore credited as the appended section only, not as the whole
  ledger. Bounded whole-file excerpts of the end version are kept only as context, labelled as
  possibly inherited. Observation is bounded and best-effort: there are caps on files enumerated,
  bytes read, and bytes stored per step. Hitting a cap or an I/O error records truncated or
  unavailable coverage and never fails or noticeably slows the step. Runs recorded after this change
  get this evidence in both automatic and replay audits.
- **Repository files a step wrote reach the judge as changes, not as end content.** For commits
  attributed to a step, the snapshot exports a bounded patch. For uncommitted working-tree changes,
  the runner keeps a bounded patch from the step's start checkpoint to its end checkpoint. It does
  this alongside the existing fingerprints, which hash Git diff streams for tracked files rather than
  file bytes, so they cannot be compared with snapshot-time content. Snapshot-time file content,
  when captured, is context only. Any portion whose authorship cannot be resolved is marked
  unresolved rather than credited to the step.
- **Attribution replaces guessing.** The record-id substring match and filename-pattern categories
  no longer decide which files belong to a step. Each leaf's coverage list then reports
  `narrative`, `artifact`, and `validation` as available only when attributed evidence exists. A step
  that genuinely produced nothing still reports them as unavailable, with a reason.
- **Content is guaranteed a share of each leaf's budget.** The existing 32 KiB per-leaf and
  256 KiB per-package limits stay. Within each leaf, a fixed reserved share goes to the step's
  output excerpt and its contribution excerpts (written-file deltas and repository patches). Other
  evidence can no longer crowd it out. Today the leaf embeds full metric step records, and trimming
  drops whole trailing references, which removes narrative first. Instead, metrics become compact
  facts that remain the quantitative authority, and verbose metric revisions, Git detail, and
  whole-file context are each trimmed separately. Every cut leaves a category-specific truncation
  marker and a stable reference to the full local evidence, which the judge can drill into
  read-only.
- The value rubric prompt tells the judge to use step outputs and each step's contribution deltas.
  Detection goes to the step whose output or delta reported a problem, and correction to the step
  whose delta fixed it. Content marked inherited or unresolved earns no credit.

No breaking change. The `audit.log` and step-record additions are additive. Runs recorded before this
change still yield step output on replay. They report written run-directory files as unavailable,
with a reason that says ownership was not recorded.

## Capabilities

### New Capabilities
- `step-written-files`: the runner records, for each step, the files it created or modified under the
  workflow output root (`{{session_dir}}/output/`, excluding Runner bookkeeping and scratch), with
  before/after linkage and bounded deltas kept for audit evidence. It also keeps bounded working-tree
  patches for the step's repository changes.

### Modified Capabilities
- `audit-evidence-preparation`: leaf evidence includes the step's own output and its contributions
  to the files it wrote, attributed by step path and execution session instead of filename
  heuristics. Each leaf has a reserved content allocation within its existing bound, and truncation
  is category-specific. Coverage classification reflects attributed evidence.
- `run-audit-replay`: the replay snapshot includes `audit.log` events and step-written-file records
  whose execution-session ownership is durable, in place of excluding all detailed outputs.
- `workflow-value-observation`: the value judge credits detection and correction from each step's
  own output and contribution deltas, and does not credit content marked inherited or unresolved.

Automatic-snapshot contents are specified under `audit-evidence-preparation`, which already owns the
audit-launch snapshot. No `automatic-run-audit` requirement changes.

## Technical Approach

The change has three layers, and each keeps today's boundaries:

1. **Recording (runner, `internal/exec` and `internal/audit`).** For executable leaf steps only, the
   runner observes the workflow output root at step start and end, beside the Git checkpoints, with
   the exclusions and caps above. It adds created and modified paths, with before and after
   content hashes, to `step_end`. Bounded deltas, plus the version baselines needed to compute them,
   go to a content-addressed evidence store in the run directory. Because the store is inside the
   run directory, `copyEvidenceTree` picks it up automatically, and because it sits outside the
   observed root, it never observes itself. Working-tree patches are stored there in the same way.
   Nested steps (loops, groups, sub-workflows) are not observed separately, so parents don't
   double-count.
2. **Snapshotting (`internal/devaudit/lifecycle.go`).** The automatic snapshot already copies the run
   directory. It also exports bounded content for Git-attributed repository paths, with fingerprints,
   and patches for attributed commits next to `git-evidence.json`. The replay snapshot adds a
   filtered `audit.log` and the evidence-store entries referenced by allowed sessions' `step_end`
   records, using the same session allow-list it already applies to
   metrics. It still excludes everything without durable ownership.
3. **Preparation (`internal/devaudit/value_audit.go`).** A parser maps `step_end` events to value
   leaves by logical step path and execution session, and builds `narrative` references (step
   output), `artifact` references (written files), and `validation` references (validator steps'
   output and validator files). Leaf bounding changes from dropping trailing references to a reserved
   content share plus category-specific trimming. Unattributed snapshot files remain in the index for drill-in but are no longer attached to leaves
   by name.

Key decisions:

- **Ownership of a version is not authorship of its content.** Evidence credits a step with the
  delta between the versions before and after it, not with the whole file it last touched. The
  delta is what lets the judge tell detector from fixer without double-crediting.
- **Observe the output root, not the whole session directory.** Runner bookkeeping changes during
  every step, and scratch trees hold clones and build output. Observing the whole session directory
  would give every step false artifact evidence and unbounded scan cost. Files that workflows write
  elsewhere in the session directory stay unattributed, but they remain in the index for drill-in.
- **Where to keep content: in the run directory or only in the snapshot.** Keeping versions and
  deltas in the run directory is the only way replays can see files that later steps overwrite, such
  as round-by-round acceptance files. The cost is more disk per run, capped per file and per step.
  Design sets the caps.
- **Repository changes are kept as patches, not copies.** Bounded patches for commits and for
  working-tree deltas show what the step changed, without duplicating the working tree into every
  run.
- **Privacy is unchanged.** Step output and file contents are model input and local evidence only.
  The value dataset row (Sheets) keeps its schema, and existing judge-note safety checks still
  apply.

Risks:

- Agent `stdout` exists only for headless steps. Interactive steps get no recorded output and must
  report it as unavailable, not as missing by mistake.
- A large `audit.log` makes preparation slower. Parsing is a single streaming pass.
- Output-root observation adds work at every step boundary. The caps and best-effort semantics keep
  that cost from failing or dominating a step.
- Workflows that write evidence outside `{{session_dir}}/output/` get no artifact credit until they
  move it there or a follow-up adds declared evidence paths.
- Package batching produces more batches, so judge calls cost more. The existing bounds limit the
  growth.

## Out of Scope

- Files written outside the repository and the workflow output root, such as the factory's
  `{{artifact_dir}}`, `{{session_dir}}/scratch/`, and gitignored repository files such as
  `validator_logs/`. They need
  workflow-declared evidence paths, which is a follow-up.
- Native agent-session transcripts. The existing spec rules out a cross-adapter transcript-capture
  interface, and `native_session` stays unavailable unless an adapter already exposes it.
- Re-enabling factory post-run audits (#191, agent-factory#60).
- Changes to the correctness audit's prompt or findings. It reads the same improved evidence index,
  but its behavior and rubric don't change here.
- Changes to the value dataset schema or Sheets delivery.
- Backfilling written-file manifests for runs recorded before this change.

## Impact

- **Code:** `internal/exec` (step-end recording for agent, shell, and nested steps), `internal/audit`
  (session-directory observation alongside Git checkpoints), `internal/devaudit/lifecycle.go`
  (automatic and replay snapshot contents), and `internal/devaudit/value_audit.go` (attribution,
  coverage, prompt).
- **Persisted data:** additive fields on `step_end` audit events and a new content-addressed evidence
  store in run directories. The evidence index and value packages gain attributed references, so the
  evidence schema version increases. Existing readers ignore the new fields.
- **Specs:** `audit-evidence-preparation`, `run-audit-replay`, and `workflow-value-observation`
  change, and `step-written-files` is new.
- **Users:** and-scene eval replays and future factory audits produce value ratings grounded in what
  each step reported and produced. Run directories grow modestly, within fixed caps.
- **Tests:** unit tests in `internal/devaudit` and `internal/exec`, and existing integration tests
  (`value_audit_integration_test.go`, `cli_e2e_test.go`) extended with fixtures that have outputs,
  written files, and multi-session replays.
