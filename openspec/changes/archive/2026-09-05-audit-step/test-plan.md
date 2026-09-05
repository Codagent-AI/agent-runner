## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional integration, end-to-end, agent-acceptance, and exceptional human-only obligations.

The highest-risk failures are audit code leaking into production builds, launching before source evidence is durable, blocking or changing the source result, double-counting resumed or nested work, exporting detailed evidence, duplicating external writes, and allowing unvalidated model output to mutate GitHub. Automated tests use isolated homes, temporary Git repositories, fake agent executables, controlled HTTP servers, and a stateful fake `gh` executable. They must not use the operator's real Google or GitHub accounts.

Normal CI continues to run the untagged suite. A dedicated development-audit job runs `go build -tags dev_audit ./cmd/agent-runner`, `go test -tags dev_audit ./...`, `golangci-lint run --build-tags dev_audit ./...`, `gosec -tags dev_audit ./...`, and `govulncheck -tags dev_audit ./...`, plus the normal formatting check, with timeouts appropriate to those checks. Stable fake-based end-to-end tests run in that tagged job. Real-agent and real-service flows remain acceptance obligations because credentials, cost, and external effects make them unsuitable for pull-request CI.

The plan intentionally omits:

- a unit-test inventory, because the specifications and implementation-time TDD govern isolated validation and edge cases;
- a real-agent matrix across every supported CLI, because the workflow resolves one existing `crosscheck` agent and adapter behavior already has its own real-agent suite;
- live Google or GitHub calls from CI;
- synthetic issues in `Codagent-AI/agent-runner`; and
- new TUI navigation or audit-step rendering, because ordinary run history may show audit runs but receives no dedicated presentation.

## Integration Tests

### INT-001: Development and production build boundary

- Covers: development-only availability, local build-source injection, hidden workflow injection, and production exclusion.
- Boundary: `Makefile`, `dev.sh`, Go build constraints, linker provenance, command registration, and workflow catalog assembly.
- Setup: Build tagged and untagged binaries into temporary paths. Inject two distinct temporary Agent Runner checkout roots into separate tagged builds. Build the untagged binary with production-style linker flags, and provide a normal runs root containing a linked audit run created by the tagged binary.
- Action: Inspect help, explicit audit command routing, ordinary and hidden workflow discovery, ordinary run list/view behavior, reported build provenance, and binary content for a unique hidden-workflow marker.
- Assertions: `make build` and `dev.sh` use the development tag and source provenance; tagged binaries register local audit commands and inject exactly one hidden audit workflow; each tagged binary reports its own injected checkout; untagged and production-style binaries reject audit commands, never list or resolve the workflow, contain no workflow marker, cannot be enabled by configuration, and safely list/view pre-existing audit-run records without gaining audit capability.
- Constraints: The test builds only temporary binaries and does not overwrite `bin/agent-runner`. Exact release archive packaging remains covered by the same untagged build contract plus GoReleaser configuration validation.
- Execution: Go integration tests in the tagged provider and `cmd/agent-runner` packages, run in both normal and `dev_audit` CI jobs as applicable.

### INT-002: Durable post-finalization launch and process independence

- Covers: eligible outcome handling, finalization ordering, exactly-once reservation, recursion prevention, source authority, detached continuation, reciprocal linkage, and intake-route ordering.
- Boundary: runner finalization, state and metrics persistence, run locks, source-side audit lifecycle metadata, atomic launch records, subprocess creation, and frozen intake routing.
- Setup: Use temporary run directories and a helper child process that records start and completion sentinels. Inject success, failure, stopped, duplicate-terminal, launch-error, audit-kind, unrelated-workflow, nested-workflow, and frozen-intake cases.
- Action: Finalize source execution sessions and allow the real coordinator/process abstraction to reserve and start the helper where eligible.
- Assertions: The hook observes flushed terminal state, metrics, and checkpoints; while finalization still owns the source run it reserves one audit ID and freezes an immutable session snapshot; only then is the source released for resume and the child started; duplicate handling reuses the reservation; excluded runs start nothing; the source result and exit semantics never change; the child survives parent completion; reciprocal identifiers agree; launch failure is a warning; and the audit launch attempt precedes foreground intake transfer without blocking that transfer.
- Constraints: Process-detachment assertions run on supported Darwin/Linux implementations. Unsupported-platform behavior is covered at the platform adapter's lowest reliable layer.
- Execution: Go integration tests beside the runner coordinator and CLI launch adapter in the `dev_audit` CI job.

### INT-003: Session metrics, Git checkpoints, and leaf attribution

- Covers: metrics schema v3 migration, execution-session identity, resume separation, source/audit metric separation, trustworthy unknowns, leaf-step aggregation, and conservative working-tree/commit attribution.
- Boundary: audit event pipeline, metrics collector, resolved workflow tree, temporary Git repository, and persisted `audit.log`/`run-metrics.json` artifacts.
- Setup: Execute a fixture containing nested groups, a loop with repeated attempts, a sub-workflow, an autonomous agent leaf with child-call metrics, and an interactive leaf with no native usage record. In a temporary Git repository, create uncommitted step deltas, a later bulk commit of earlier deltas, matching prefixed commits, unprefixed commits, a prefix/boundary disagreement, no-change steps, and an unavailable closing checkpoint. Provide schema v2 fixtures with both unambiguous and ambiguous legacy session attribution, then resume the run in a second execution session.
- Action: Process lifecycle events, write checkpoints and metrics, and build per-session leaf rollups and attribution facts.
- Assertions: v2 migrates to v3 without record loss; deterministic legacy session IDs are assigned; ambiguous legacy record attribution stays unknown with limited history coverage; every new event and metric record has the correct execution session; resume produces a distinct session rollup under the same run; structural containers have no value row; attempts, iterations, and agent-internal child usage aggregate once into the full leaf path; uncommitted deltas are `working_tree`; later packaging is `deferred_commit` and counted once; matching boundary-plus-prefix commits are attributed; ambiguous commits are not; interactive usage/cost stay unknown; and audit overhead never enters source totals.
- Constraints: Commit contents and changed-file identities remain inside the temporary repository and local artifacts.
- Execution: Go integration tests beside audit, metrics, and evidence preparation, run in normal CI for shared instrumentation and tagged CI for audit projection.

### INT-004: Evidence preparation and model-output containment

- Covers: immutable session-scoped intake, 256 KiB package bounds, 32 KiB per-leaf detail bounds, deterministic batching with fresh model sessions, evidence priority and omissions, optional native-session consultation, fixed value rubric, snapshot immutability, and rejection of unsafe or fabricated model fields.
- Boundary: realistic persisted run fixtures, immutable filesystem evidence workspace, Git exports, model input/output JSON schemas, deterministic merge, and snapshot/output-boundary fingerprints.
- Setup: Construct source-run fixtures with multiple sessions, oversized Runner-owned outputs and diffs, one discoverable native-session reference, one adapter with no native-session material, missing metrics, inherited artifacts, validation results, sensitive-looking content, and enough leaf facts to require more than one package. Supply valid and adversarial value/correctness model outputs.
- Action: Freeze the evidence intake, prepare it twice, mutate the live source run after freezing, invoke a fresh fake model session per batch, validate and merge all outputs, record detailed-evidence consultations, and separately simulate mutation of the frozen snapshot during model inspection.
- Assertions: Repeated preparation is equivalent; later legitimate source-run changes do not enter or invalidate the audit; every covered leaf appears in exactly one processed batch; each batch uses a fresh session; limits and priority order hold; omissions and unavailable native-session material reduce coverage; measured identity/cost/Git fields cannot be replaced by model text; enum and completeness errors fail validation; unsafe notes and unknown evidence references are rejected; consultations record identifiers/categories without copied contents; and mutation of the frozen snapshot prevents Sheets or GitHub publication.
- Constraints: Tests assert byte limits on encoded UTF-8 JSON, not approximate token counts. They do not judge whether a model's qualitative opinion is correct.
- Execution: Go integration tests in the evidence, report, and validation packages in tagged CI.

### INT-005: Google connection import and OAuth independence

- Covers: one-time credential import, user-only storage, source-application independence, destination freezing, and non-blocking missing or unusable connection state.
- Boundary: setup CLI, installed-application client JSON, authorized-user token JSON, atomic filesystem storage, OAuth2 refresh transport, and frozen audit delivery metadata.
- Setup: Use isolated homes, synthetic compatible credential files, a controlled OAuth token server, malformed and insufficient-scope variants, and connection files whose source paths are later removed.
- Action: Run setup, inspect permissions and non-secret metadata, remove the source files, authenticate through the stored copy, change the current destination, and retry a report frozen against the earlier destination.
- Assertions: Parent storage is `0700`, the connection record is `0600`, only required credential values are copied, source paths and secrets never enter project/run/model artifacts, refresh works without Hermes or source files, malformed or insufficient credentials produce reporting warnings, connection presence never controls audit launch, and retry uses the frozen destination unless explicitly migrated.
- Constraints: The controlled token server is a contract substitute for Google authorization in CI; live credential compatibility is covered by AT-003.
- Execution: Go integration tests beside the development Google setup and credential store in tagged CI.

### INT-006: Google Sheets projection and retry contract

- Covers: exact `step_value_v1` header validation, allowlisted high-level projection, append-only delivery, replay identity, retry idempotency, ambiguous responses, and local-first failure handling.
- Boundary: validated local audit report, OAuth HTTP client, stateful Sheets API test server, reporter lock, and delivery state.
- Setup: Serve correct, missing, reordered, and unsupported headers; existing observation IDs; rate-limit/auth failures; and an append endpoint that commits a row before dropping its response. Include local report fields containing transcripts, paths, URLs, diffs, filenames, secrets, unsafe notes, and unknown numeric metrics.
- Action: Report automatic and replay observations, retry definite and ambiguous failures, and run concurrent reporters for the same sheet/tab.
- Assertions: The reporter sends exactly the ordered schema columns; `project` is a sanitized `owner/repository` slug or root basename and never a URL/path; prohibited local fields never appear in any request; unknown numbers are blank; unsafe notes prevent the row from being sent; header mismatch writes nothing and changes no structure; new audits append; replays use new IDs; retries reuse existing IDs; ambiguous success does not duplicate a row; concurrent delivery is serialized; and the complete local report predates every write attempt and remains retryable after failure.
- Constraints: No real Google network call occurs in automated tests. Request bodies are retained only in test memory.
- Execution: Go integration tests beside the Sheets reporter in tagged CI.

### INT-007: GitHub issue publication and deduplication contract

- Covers: confirmed-defect publication, semantic duplicate handling, stable finding markers, redaction, grouped symptoms, closed recurrence, failure retention, and the prohibition on value-stage GitHub mutations.
- Boundary: validated correctness candidate, injected stateful `gh` executable, issue search results, deterministic publisher, and local finding state.
- Setup: Provide confirmed, inconclusive, duplicate, closed-duplicate, multi-symptom, unsafe-content, and publication-failure candidates. The fake `gh` executable records argv/stdin and simulates issue state across retries.
- Action: Search and publish through the same command boundary used in development, then repeat each ambiguous or failed operation.
- Assertions: Only validated confirmed non-duplicates can create an issue; the target is exactly `Codagent-AI/agent-runner`; every created title begins `[auto-audit]`; issue bodies contain observed/expected behavior, verification guidance, redacted evidence, and the stable marker; symptoms sharing a cause create one issue; open duplicates receive neither issue nor comment; closed recurrence may create one linked issue; exact-marker retries are idempotent; failures remain local; value outputs cannot invoke the publisher; and no repository file or commit changes.
- Constraints: Automated tests never authenticate to GitHub and never create a real issue. Semantic duplicate quality from a real model is exercised in acceptance, while the mutation contract is authoritative here.
- Execution: Go integration tests beside the correctness publisher in tagged CI.

## End-to-End Tests

### E2E-001: Automatic audit completes independently after a successful headless run

- Covers: the primary automatic journey from eligible source completion through linked audit, value observations, local report, and reporting.
- Surface: Tagged `agent-runner` CLI invoked headlessly from an isolated project.
- Setup: Build a temporary `dev_audit` binary with injected source provenance. Create an isolated home, a temporary Git project, an eligible OpenSpec/spec-driven fixture with nested leaf steps and attributable working-tree changes/commits, a recorded profile set whose `crosscheck` role resolves to a deterministic fake agent, a delayed audit completion sentinel, and controlled Google/GitHub endpoints.
- Journey: Run the source workflow, observe its process return while the delayed linked audit remains active, wait for audit completion, inspect source/audit state, and retry the already successful report.
- Assertions: The source succeeds without waiting for audit completion; one linked non-recursive audit freezes source evidence and starts before view/intake release boundaries; reciprocal links and execution-session IDs agree; the hidden workflow freezes the actual resolved `crosscheck` CLI/model/effort; exactly one row exists per executed leaf with owned child usage aggregated; source and audit metrics remain separate; the local report is complete; the Sheet request contains only allowlisted data; no GitHub issue originates from value output; and retry adds no rows or model calls.
- Execution: Stable fake-based CLI E2E in `cmd/agent-runner`, run by tagged CI on Linux.

### E2E-002: Failed execution, resume, replay, and report retry preserve lineage

- Covers: failed/stopped source authority, resumed execution sessions, overlap lineage, explicit replay, reporting retry, and source immutability.
- Surface: Tagged `agent-runner` run, resume, audit replay, status, and report-retry CLI entry points.
- Setup: Use an isolated source workflow that fails after one leaf on its first invocation and succeeds after resume, deterministic fake `crosscheck` output, a controlled Sheet endpoint that fails its first delivery after recording the request, and snapshots of source state and repository content.
- Journey: Run and audit the failed session, resume and audit the successful session, explicitly replay one selected session, then retry the failed delivery without rerunning the model.
- Assertions: Failure exit/status remain authoritative and resumable; the first audit continues against its frozen snapshot while resume creates a new execution-session ID and linked snapshot under the same source run; revisited leaf observations identify overlap rather than independent new evidence; replay requires an unambiguous session, gets a distinct audit/observation set, and never reruns source work; report retry keeps its original IDs and model-call count; and source state, commits, and working tree remain unchanged by replay.
- Execution: Stable fake-based CLI E2E in `cmd/agent-runner`, run by tagged CI on Linux.

## Agent Acceptance Tests

### AT-001: Real crosscheck agents produce a safe local audit

- Classification: Required
- Covers: real model prompt usability, concrete Git-first value judgment, fixed rubric output, selective available detailed-evidence access, distinction between project failure and Runner defect, and informational-only value behavior.
- Actor and surface: Acceptance agent using the tagged headless CLI and the operator's working `crosscheck` agent profile.
- Setup: An isolated small eligible workflow fixture with two executed leaves, one attributable commit, one useful no-commit planning artifact, downstream validation, and an ordinary project-caused failure. Disable real Sheets delivery for this flow and place a recording non-mutating `gh` substitute on `PATH` so an unexpected candidate cannot create an issue.
- Steps: Run the workflow, allow both real model stages to finish, inspect the local evidence index, consultations, observations, correctness findings, audit metrics, and snapshot/output-boundary fingerprints.
- Expected: One observation exists per leaf; attributable working-tree or commit diffs lead the implementation judgment without double counting a later bulk commit; the planning leaf is not assigned no value merely for lacking a commit; every rubric field validates; any available deep-evidence consultation is recorded locally and unavailable categories are explicit; the ordinary project failure is not confirmed as a Runner defect; no issue call or repository mutation occurs; and audit cost is separate from source cost.
- Evidence: Sanitized CLI output, source and audit IDs, local report schema/field summary, consultation categories, metric totals, and before/after Git status and HEAD. Do not copy transcript or diff contents into acceptance evidence.
- Effects and cleanup: Authorized real model usage and cost for one small audit. Remove temporary project/run data after recording sanitized evidence. No Google or GitHub mutation is authorized.
- Permitted substitutes: A different supported real agent may be used only by redefining the existing `crosscheck` role through normal profile configuration. A fake model is not a substitute.

### AT-002: TUI completion remains visible while audit runs independently

- Classification: Conditional: Darwin
- Covers: launch before TUI exit, unchanged source completion display, background continuation after exit, and hidden audit presentation.
- Actor and surface: Acceptance agent driving the tagged CLI's live run view through a PTY and reconstructing frames with a terminal emulator.
- Setup: An eligible fast source fixture and a deterministic audit agent that waits on a sentinel after the linked audit starts. Launch the workflow directly into the live run view so no synthetic `r` keypress is required.
- Steps: Wait for the source completion frame, confirm persisted audit launch while leaving that frame open, capture the reconstructed screen, exit the run view, release the sentinel, and inspect audit completion through the local status command.
- Expected: The source completion state stays visible and is not replaced by audit steps; audit launch occurs before TUI exit; the hidden workflow does not appear in ordinary discovery; exiting the view does not stop the audit; and the audit later reaches its own terminal state.
- Evidence: Reconstructed terminal screenshot showing source completion, timestamps or lifecycle records proving the linked audit had already started, and final local audit status.
- Effects and cleanup: No external services or model cost. Remove the isolated home and fixture after evidence capture.
- Permitted substitutes: None. Raw PTY byte scraping without terminal reconstruction is not acceptable visual evidence.

### AT-003: Imported Google credentials deliver and deduplicate real rows

- Classification: Conditional: applies once compatible existing Google credentials have been imported and the operator-provided spreadsheet is accessible.
- Covers: real OAuth compatibility without Hermes, exact header validation, allowlisted external projection, append behavior, and retry idempotency.
- Actor and surface: Acceptance agent using the tagged Google setup and report-retry CLI commands plus the Google Sheets UI or API for observation.
- Setup: Create a temporary `acceptance` tab in the operator-provided spreadsheet with the exact `step_value_v1` header. Temporarily point protected development-audit connection state at that tab. Prepare a validated local report containing representative known and unknown metrics plus local-only detailed evidence.
- Steps: Import/copy the existing compatible credentials through the product setup command, deliver the report, inspect the appended rows, retry the same report, and inspect the tab again.
- Expected: OAuth refresh and Sheets access work after the source application/files are unavailable; each leaf produces one row in exact column order; unknown numbers are blank; no transcript, summary, prompt, response, tool call, output, source, diff, artifact content, excerpt, filename/path, private URL, or secret appears; retry creates no duplicate observation ID; and secrets do not appear in local run artifacts.
- Evidence: Sanitized setup output, a screenshot or range read of header plus appended high-level rows, row count and observation IDs before/after retry, and protected-file permission checks. Redact the spreadsheet ID and OAuth material from published evidence.
- Effects and cleanup: Authorized real Google API calls and temporary rows. After evidence capture, restore the intended `step_value_v1` destination and delete the temporary `acceptance` tab using ordinary Google administration outside Agent Runner.
- Permitted substitutes: None once the condition applies. A controlled HTTP server proves the contract but does not replace this live compatibility flow.

### AT-004: A genuine confirmed Runner defect creates one real issue

- Classification: Conditional: applies only when an audit identifies a genuine, confirmed, non-duplicate Agent Runner defect.
- Covers: real semantic verification, duplicate search, redacted issue publication, reciprocal finding linkage, and repository non-mutation.
- Actor and surface: Acceptance agent reviewing the local correctness finding and the resulting `Codagent-AI/agent-runner` issue.
- Setup: Preserve the genuine source run, build provenance, authoritative launch-time checkout snapshot, validated finding, and current open/closed issue search results. Do not manufacture a defect or synthetic issue for acceptance.
- Steps: Confirm that the condition applies, allow deterministic publication, inspect the issue and local finding, then retry publication once.
- Expected: Exactly one focused issue is created with an `[auto-audit]` title, observed/expected behavior, reproduction or verification guidance, concise redacted evidence, and a stable finding marker; the local report records its URL; retry creates no issue or comment; and neither the source project nor Agent Runner checkout changes.
- Evidence: Issue URL, sanitized issue body, duplicate-search result, local finding ID/link, retry result, and before/after Git status and HEAD.
- Effects and cleanup: Authorized creation of one genuine issue in `Codagent-AI/agent-runner`. The issue remains open for ordinary triage; acceptance does not close, edit, comment on, or fix it.
- Permitted substitutes: None when a genuine finding exists. If no genuine confirmed non-duplicate defect exists, this flow is not applicable and no synthetic issue is created.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | AT | HT |
| --- | --- | --- | --- | --- |
| Development-only capability and production exclusion | INT-001 | E2E-001 | — | — |
| Automatic post-finalization audit lifecycle and source authority | INT-002 | E2E-001, E2E-002 | AT-002 | — |
| Execution-session identity, resume lineage, and metric separation | INT-003 | E2E-002 | AT-001 | — |
| Git checkpoints, conservative attribution, and leaf aggregation | INT-003 | E2E-001, E2E-002 | AT-001 | — |
| Deterministic bounded evidence and selective local inspection | INT-004 | E2E-001 | AT-001 | — |
| Fixed value rubric, safe model output, and informational-only behavior | INT-004 | E2E-001 | AT-001 | — |
| Injected Agent Runner source and repository non-mutation | INT-001, INT-004 | E2E-001, E2E-002 | AT-001, AT-004 | — |
| Protected Google import and Hermes-independent OAuth | INT-005 | — | AT-003 | — |
| Allowlisted Google Sheets reporting and idempotent retry | INT-006 | E2E-001, E2E-002 | AT-003 | — |
| Correctness classification and confirmed-defect issue publication | INT-004, INT-007 | E2E-001 | AT-001, AT-004 | — |
| Explicit historical replay and source immutability | INT-002, INT-004 | E2E-002 | — | — |
| Run-view independence and hidden-workflow inspection | INT-001, INT-002 | E2E-001 | AT-002 | — |
