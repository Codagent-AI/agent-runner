## Context

Agent Runner needs longitudinal evidence about whether the steps in its rigorous workflows justify their cost and whether the orchestrator misbehaves in real executions. The source evidence is already mostly local: workflow state, audit events, metrics, agent output, artifacts, validation results, and Git history. Deterministic code can establish what ran and what changed, but model judgment is needed to assess qualitative value and distinguish an Agent Runner defect from an ordinary project failure.

This is a private development instrument for the maintainer, not a product feature. It will be used from locally built Agent Runner binaries on a small number of machines. A production build must not expose an audit switch, command, workflow, or dormant embedded asset. Local builds should require no audit enablement setting and should automatically inspect the Agent Runner checkout from which that binary was built.

The workflow has two distinct outcomes. The value stage produces one lightweight observation per executed leaf step for a longitudinal Google Sheet. The correctness stage may create a GitHub issue for a confirmed, non-duplicate Agent Runner defect. Detailed run evidence remains local and neither stage changes source code or workflow policy.

## Goals / Non-Goals

### Goals

- Automatically launch a linked audit immediately after every finalized top-level OpenSpec or spec-driven execution session in a development-audit build.
- Keep the source run's terminal state, exit status, TUI completion view, and intake routing independent from audit success or failure.
- Produce one value observation per executed leaf workflow step, aggregating attempts, loop iterations, and agent-internal child usage owned by that leaf.
- Ground value judgment primarily in per-step working-tree deltas and commits/diffs that can be conservatively attributed without double-counting later bulk commits.
- Let both model stages inspect detailed local evidence when the compact package is insufficient.
- Report only an allowlisted set of high-level metrics and judgments to one existing Google Sheet.
- Verify suspected Agent Runner defects against a launch-time snapshot of the injected checkout and create focused, deduplicated GitHub issues when warranted.
- Make automatic launch, model output validation, Sheets retry, and issue creation durable and idempotent.
- Exclude the concrete audit implementation and workflow asset from production builds.

### Non-Goals

- A user-facing audit setting, consent flow, onboarding path, settings-editor control, or public documentation.
- A configurable audit profile or a separately configured Agent Runner repository.
- A production or generally supported audit command.
- A reporting service, generic sink interface, analytics UI, or spreadsheet-creation workflow.
- Full transcripts, transcript summaries, source code, diffs, paths, filenames, or evidence excerpts in the external dataset.
- Automatic workflow changes, code fixes, or GitHub issues based on value judgments.
- Independent rows for structural workflow containers, attempts, loop iterations, or agent-internal subagents.
- Strong causal claims from individual observations.

## Approach

### 1. Development-only build boundary

Introduce a private `dev_audit` Go build tag. The tagged implementation lives behind a small provider boundary; the untagged provider reports that no audit capability exists. `make build` and `dev.sh` add the tag and inject the absolute repository root and build Git provenance with linker variables. GoReleaser, ordinary untagged `go build`, and production packaging omit the tag.

The hidden workflow asset belongs to the tagged implementation rather than the existing `workflows/` embedded filesystem, whose `all:*` pattern would otherwise place it in every binary. The tagged provider embeds and injects the workflow into the loader only for a development-audit build. It also registers the post-run hook and local replay/setup commands. The untagged build registers none of them, so production help and workflow discovery contain no audit surface and runtime configuration cannot enable it.

The injected provenance contains the absolute checkout root, Git revision when available, and whether the build observed a dirty tree. This gives each machine's local build its own inspection source without a repository setting. At audit launch the coordinator verifies that the path is still the Agent Runner module, records its current revision and dirty state, and snapshots that checkout for read-only correctness inspection. The launch snapshot is authoritative for whether a defect is current; the build provenance remains useful diagnostic context. A build-to-launch mismatch reduces confidence where relevant but does not by itself block a finding verified against the launch snapshot. A missing path, wrong module, or incomplete launch snapshot prevents source-verified publication unless independent matching repository evidence is available.

### 2. Post-finalization coordinator and linked audit process

Add a runner post-finalization hook that receives a read-only terminal execution summary after state, metrics, terminal audit events, and step checkpoints have been durably flushed. While finalization still owns the source run, the coordinator atomically reserves the audit and copies or exports the selected execution-session evidence into an immutable launch snapshot. It then releases the source run for later resume and starts the detached child from that snapshot. The CLI supplies the tagged audit coordinator; the runner package remains unaware of the concrete audit implementation.

For a top-level run whose canonical workflow is in the `openspec` or `spec-driven` namespace, the coordinator atomically reserves an audit run ID and persists a launch record keyed by source run ID plus execution-session ID. Duplicate terminal handling reuses that reservation rather than starting another audit. Audit runs identify their kind explicitly, so they cannot trigger the hook recursively.

The coordinator starts the current executable with a private tagged audit subcommand, an immutable request/snapshot path, redirected standard streams, and platform-appropriate process detachment. It does not invoke a shell. The child process is released after a successful start and owns the hidden audit run. A reciprocal link is written into the audit state; launch and later completion/reporting state are appended to source-side post-run lifecycle metadata without reopening or rewriting the snapshotted source evidence.

The launch attempt happens inside `ExecuteFromHandle` after finalization but before that call returns. Consequently, a live TUI can continue displaying the completed source run while the audit runs, and a successful frozen intake route is not given foreground ownership until the audit launch attempt has completed. Only process creation is awaited; audit execution is not.

### 3. Execution-session and leaf-step instrumentation

Create a durable execution-session UUID for every new or resumed invocation. Add it to run and step lifecycle events, metric records, and per-session metric rollups while retaining the stable run ID. A thin audit-logger wrapper captures Git checkpoints on executable step start and end, avoiding Git instrumentation in each executor.

A value observation corresponds to an executed leaf in the resolved workflow tree:

- its identity is the full logical path, so identically named nested steps remain distinct;
- structural group, loop, dispatch, and sub-workflow containers receive no row when their work is represented by descendant leaves;
- multiple attempts or iterations of the same leaf path in one execution session are aggregated;
- agent-internal child calls and their trustworthy usage are rolled into the owning workflow leaf rather than becoming separate rows; and
- a later resume creates a new observation for a revisited leaf and records overlap with the earlier session.

The checkpoint records include HEAD plus index, working-tree, and untracked-file state sufficient to derive aggregate deltas, with explicit unavailability reasons. A concrete change first observed across a leaf boundary is `working_tree` evidence for that leaf even before commit. Direct commit attribution requires both membership in the leaf's recorded revision interval and an exact `[<step_id>]` subject prefix. When a later bulk commit packages changes already associated with an earlier leaf, that earlier evidence becomes `deferred_commit` and the later commit-only step does not receive duplicate change statistics or contribution credit. Ambiguous, shared, mismatched, and unprefixed commits remain unattributed. No commit is an explicit Git state, not an automatic no-value judgment.

`run-metrics.json` advances to schema v3. Existing `sessions[]` entries gain deterministic legacy execution-session identities, and metric records gain execution-session attribution only where durable v2 evidence makes it unambiguous; otherwise the record remains preserved with unknown attribution and limited history coverage. New records always identify their execution session. Interactive steps keep duration and any adapter-provided native usage, but usage and cost remain unknown when direct terminal handoff makes them unobservable. Human-interaction count is omitted rather than inferred from traffic Runner intentionally does not capture.

### 4. Deterministic evidence preparation

The post-finalization coordinator freezes the selected execution session's durable evidence before releasing source-run ownership. The first audit stage consumes only that immutable launch snapshot and produces local, versioned artifacts:

- `evidence-index.json`, containing normalized per-leaf facts and references to all available local evidence;
- bounded model packages containing identity, outcomes, trustworthy metrics, Git attribution, commit summaries, validation/downstream results, and explicit omissions;
- a source-provenance record covering both the Agent Runner build provenance and its audit-launch checkout snapshot; and
- an audit workspace with separate read-only evidence and writable model-output directories.

Each model package is limited to 256 KiB of UTF-8 JSON, with no more than 32 KiB of detailed default evidence allocated to one leaf. The compact fact record for every leaf is retained. If those records and prioritized evidence cannot fit in one package, the preparer emits deterministic batches and the value stage processes all batches before merge. Selection priority is: identity/outcome, metrics and coverage, Git attribution and change statistics, commit summaries, downstream validation, produced artifacts, and narrative output. Every omitted or truncated category is recorded.

Detailed Runner-owned artifacts, captured output, full diffs, and relevant repository files remain locally addressable through the evidence index. Native agent-session material is included only when an existing adapter or durable reference makes it discoverable; unsupported or unavailable categories are explicit. This change does not introduce terminal capture or a cross-adapter transcript interface. Auditors may inspect available detail when necessary and must return the identifiers and categories consulted. The audit report records those references, never their contents, in its consultation ledger.

Audit agents run from the audit workspace and write only structured candidate output there. They are not given a source-repository working directory. The immutable intake contains the durable source-run evidence and relevant Git material; later resume activity cannot change or invalidate it. Pre/post fingerprints cover the audit's read-only snapshot and writable-output boundary, so mutation of snapshotted evidence invalidates publication without treating legitimate later source-run changes as tampering. This containment plus explicit read-only agent instructions protects the source while acknowledging that not every supported agent CLI offers an enforceable filesystem sandbox.

### 5. Hidden audit workflow

The tagged provider injects one versioned hidden workflow with these stages:

1. **Prepare evidence** — deterministic collection, batching, provenance checks, and observation skeleton generation.
2. **Value audit** — a deterministic loop invokes a fresh model session for each evidence batch; each invocation fills only the fixed qualitative judgments, bounded note, confidence, coverage, and consultation references for its leaf skeletons, after which code merges the complete batch set.
3. **Validate value** — deterministic schema, enum, completeness, note-safety, and evidence-reference validation; measured identity, cost, and Git fields cannot be overwritten by the model.
4. **Correctness audit** — a separate model agent investigates possible Runner defects, reads the injected Runner source snapshot as needed, searches existing issues, and emits structured candidate findings.
5. **Validate and publish correctness** — deterministic validation/redaction and GitHub issue creation for confirmed non-duplicates.
6. **Assemble local report** — atomically commits the complete report and delivery state under the audit run.
7. **Report value observations** — deterministic, retry-safe Google Sheets delivery.

Both model stages resolve the existing `crosscheck` role using the source run's recorded profile-set name and the profile configuration available at audit launch. The audit freezes the resolved CLI, model, and reasoning effort it actually invokes; it does not claim that the profile-set name alone reproduces an earlier agent definition. There is no audit-profile setting and neither stage changes the source run's profile. Failure to resolve `crosscheck` fails the audit diagnostically but remains non-blocking for the source.

Model failures preserve partial local evidence and do not prevent later deterministic stages from recording why an output is absent. Correctness publication and Sheets delivery never consume unvalidated model text.

### 6. Value record and Google Sheets projection

The local observation ID is a deterministic hash of audit run ID, execution-session ID, and full leaf path. Retrying the same report therefore preserves IDs; an explicit replay has a new audit run ID and produces new observations.

The initial worksheet schema is `step_value_v1`, with this exact ordered header:

`schema_version`, `observation_id`, `observed_at_utc`, `project`, `workflow`, `source_run_id`, `execution_session_id`, `audit_run_id`, `trigger`, `source_outcome`, `step_id`, `step_outcome`, `lineage`, `duration_ms`, `cost_usd`, `total_tokens`, `source_models`, `git_attribution`, `commit_shas`, `files_changed`, `lines_added`, `lines_deleted`, `overall_value`, `change_effect`, `unique_contribution`, `downstream_evidence`, `confidence`, `evidence_coverage`, `judge_model`, `rubric_version`, `note`.

`project` is a sanitized Git hosting `owner/repository` slug without host, credentials, or URL material; when no suitable remote exists it is the repository root basename. Model and SHA lists are deterministically sorted and joined. Unknown numeric values are blank, never zero. `lineage` is `new`, `overlap`, or `supersedes`; Git attribution is `attributed`, `working_tree`, `deferred_commit`, `no_change`, `ambiguous`, or `unavailable`. The judgment enums come from the specification. The optional note is single-line, at most 280 Unicode characters, and rejected if it contains URLs, path-like values, secret-like values, or evidence excerpts.

The reporter constructs rows from an explicit field allowlist rather than serializing the local report. It reads and exactly validates the header before writing. A user-scoped lock serializes this reporter per spreadsheet/tab. Before append, it reads existing `observation_id` values; after an ambiguous response it repeats that check. New audits append; retries never duplicate an existing observation.

### 7. Google connection setup

The tagged local build exposes a one-time setup command that imports a compatible Google installed-application client credential and authorized-user token, plus the existing spreadsheet ID and worksheet tab. It copies only the OAuth fields required for refresh and records the destination in one private development-audit connection record under `~/.agent-runner/`. Parent directories are mode `0700` and the record is atomically written with mode `0600`. Source application paths are not retained.

This connection record is integration state, not an enablement or layered project setting. Its presence does not control whether the local audit runs. When it is absent or unusable, evidence and model auditing still complete locally and Sheets delivery records a retryable warning. The reporter calls the Sheets REST API directly through OAuth2 and has no Hermes runtime or code dependency.

The destination identity and schema version are frozen into each completed local audit report before delivery; secrets are resolved only at delivery time and never copied into runs, logs, prompts, or reports. Reconfiguration affects future audits unless the operator explicitly migrates a pending report.

### 8. Correctness verification and issue publication

The correctness agent classifies suspected behavior against run evidence, current specifications, and the authoritative audit-launch snapshot of the injected Agent Runner source. It emits a compact issue candidate containing observed/expected behavior, reproducibility, affected component, evidence references, confidence, duplicate-search result, and a normalized defect key. Unverified suspicions remain local.

The deterministic publisher accepts only confirmed candidates that passed provenance and redaction checks. It derives a stable finding ID from the normalized defect key, prefixes every created issue title with `[auto-audit]`, and includes a hidden marker in the issue body. Before creating an issue in `Codagent-AI/agent-runner`, it checks both the model-selected semantic duplicate and an exact marker search. An open duplicate is linked without a new issue or comment. A closed match may be referenced when current evidence confirms recurrence. Issue creation uses the operator's existing GitHub CLI authentication; failure remains retryable local audit state.

No model stage receives authority to edit either repository. The publisher is the only stage with a GitHub mutation, and its capability is limited to creating a validated issue.

### 9. Local artifacts, replay, and inspection

Audit state records its kind, trigger (`automatic` or `replay`), source run ID, source execution-session ID, originating build and launch provenance, and delivery states. The source run holds an append-only reciprocal link keyed by execution session. Audit runs use the normal runs root and may appear in ordinary run history, identified by kind and reciprocal linkage; version one adds no TUI navigation or separate audit hierarchy. Untagged binaries tolerate these stored run records while exposing no audit launch or replay capability. Existing source run views continue to show the source completion state, and tagged audit status/replay commands provide the focused inspection path.

Explicit replay resolves one historical execution session and launches the same hidden workflow without rerunning the source. It is available only in a development-audit build. Every replay gets a new audit ID and new Sheet observations; a reporting retry reuses the original report and observation IDs without invoking either model.

## Decisions

### Use a build tag instead of a runtime setting

A tag is the only reliable way to make the capability absent from production rather than merely disabled. It also keeps private workflow assets and integration commands out of release artifacts. The trade-off is that local builds must go through `make build` or `dev.sh`; an arbitrary untagged `go build` intentionally behaves like production.

### Inject the build checkout instead of configuring a repository

The local build already supplies the intended source root. Capturing it at build time removes configuration drift across machines; snapshotting it at audit launch gives the correctness auditor a stable view of the code that is current when the audit runs. Build and launch revision/dirty provenance make their difference visible without making the routinely changing build snapshot an automatic publication blocker.

### Use the source profile's `crosscheck` agent

Reusing an existing role avoids a private audit-profile schema and keeps model selection within the current profile mechanism. The source run contributes its recorded profile-set name; the audit resolves that name at launch and freezes the actual CLI/model/effort used, giving truthful provenance without claiming that only the name reproduces past configuration.

### Keep lifecycle integration generic

The runner gains a generic post-finalization hook and execution-session identity, while the tagged CLI layer supplies all audit-specific behavior. This avoids importing dev-only Google, GitHub, or workflow concerns into the runner core and makes the untagged implementation inert.

### Use a linked workflow rather than an in-process evaluator

Agent Runner's workflow machinery already supplies separate model sessions, metrics, artifacts, failure handling, and inspectability. A linked run keeps audit overhead distinct and makes the qualitative stages visible without delaying or changing the source result.

### Keep measurement deterministic and judgment model-owned

Identity, execution facts, metrics, Git attribution, and delivery are produced by code. Models supply only the judgments deterministic code cannot make. This prevents model output from fabricating quantitative evidence and gives validators a narrow schema.

### Use Google Sheets directly

One operator needs a lightweight longitudinal table, not a storage platform. Direct OAuth2 REST calls minimize dependencies and permit reuse of an existing Google OAuth application without coupling Agent Runner to Hermes.

### Separate correctness judgment from GitHub mutation

The model investigates and proposes; deterministic code validates, redacts, deduplicates by stable marker, and creates the issue. This provides a narrow, retry-safe mutation boundary while preserving semantic duplicate judgment.

## Risks / Trade-offs

- **The checkout can change after compilation.** Build and launch provenance expose the mismatch. The launch snapshot answers whether the defect is current; the mismatch affects confidence only where it weakens the relationship to the observed run.
- **Dirty builds cannot be reconstructed from a commit alone.** The audit records dirty provenance and snapshots the current checkout contents at launch rather than pretending a revision identifies them fully.
- **Detached launch can fail between reservation and process start.** The durable launch record has reserved, started, and failed states; retries reuse the same reservation and observation identities.
- **Agent CLIs do not all enforce read-only filesystems.** Audit agents work in a separate workspace against copied/exported evidence and are surrounded by snapshot/output-boundary fingerprints. Detected snapshot mutation invalidates publication; legitimate later source-run changes do not.
- **Detailed local evidence may contain secrets.** It never enters the Sheet projection, and issue publication applies explicit redaction. Local audit artifacts inherit protected run-directory permissions.
- **A model may make an incorrect value judgment.** Rows carry rubric/model/confidence/coverage, remain informational, and never authorize action.
- **A model may falsely confirm a defect.** Current-source inspection, structured evidence references, semantic duplicate search, deterministic validation, and conservative publication reduce but cannot eliminate this risk. Maintainers still verify code before fixing.
- **Sheets append is not transactional.** Stable observation IDs plus preflight and retry reads provide practical idempotency. An unrecoverable ambiguity is retained locally rather than blindly appended.
- **The fixed worksheet schema will evolve.** Exact headers and a schema version force an explicit migration instead of silently mixing incompatible rows.
- **Automatic local audits add cost and background load.** Their metrics remain in the linked audit run, and no source outcome waits for them. There is intentionally no runtime disable switch in this private experiment.
- **Using `crosscheck` couples audit availability to the source profile.** Missing role resolution is an audit failure only. A later change can introduce a dedicated role if evidence shows this is limiting.

## Migration Plan

1. Advance `run-metrics.json` to schema v3, migrate v2 session history conservatively, and add execution-session identities, per-session rollups, and step Git checkpoints without changing existing run IDs or cumulative totals.
2. Add the generic post-finalization hook with an inert default implementation.
3. Add the tagged development provider, hidden workflow asset, build provenance injection, local commands, and `make build`/`dev.sh` flags. Confirm an untagged/release build contains no registered audit surface, and add tagged build, test, lint, `gosec`, and `govulncheck` CI coverage.
4. Add deterministic evidence preparation, model schemas, validators, local reports, and reciprocal linkage.
5. Add correctness publication through existing GitHub CLI authentication.
6. Add the protected Google connection import and exact `step_value_v1` reporter after the operator creates the spreadsheet.
7. Exercise automatic launch from both TUI and headless paths, failed/stopped runs, resumes, replay, report retry, duplicate issue handling, and production-build exclusion.

Existing runs remain readable through the v2-to-v3 metrics migration. Historical records whose execution session cannot be derived remain explicitly unknown, and historical runs without step checkpoints may be replayed only with limited coverage; the audit does not synthesize missing attribution. No configuration migration is needed because the feature adds no audit configuration fields.

Rollback consists of building without `dev_audit`. Existing local audit runs and Sheet rows remain historical data; source runs require no conversion.

## Open Questions

- The exact compatible Hermes credential source files will be confirmed during implementation; import must validate their structure and scopes without depending on Hermes at runtime.
