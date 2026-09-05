## Why

Agent Runner's rigorous workflows are intentionally more expensive and complex than a single agent session, but there is not yet enough systematic evidence to show which workflow steps deliver unique value, which defects occur in real executions, or whether the added cost is justified. Simplifying or strengthening workflows before collecting that evidence would rely on intuition and isolated anecdotes.

The first priority is therefore instrumentation: automatically evaluate ordinary runs and accumulate comparable evidence about cost, step contribution, and Agent Runner correctness without immediately changing workflows in response. Deterministic run artifacts can establish what executed and what it cost, while attributed commits show what a step actually changed. Model-based judgment is still needed to assess whether a change was correct, novel, consequential, or duplicated elsewhere and to recognize orchestration defects that deterministic checks do not encode.

Workflow-value observation and correctness auditing serve different purposes and need different outputs. Value observations should accumulate as lightweight qualitative and quantitative records for later analysis. Confirmed Agent Runner defects should instead become actionable, evidence-backed GitHub issues. Neither path should automatically modify code or workflow policy.

## What Changes

- Add a hidden development-only audit workflow that can be invoked explicitly for a recorded run and launched automatically after eligible top-level OpenSpec and spec-driven workflow executions.
- Compile the workflow, automatic hook, and local audit commands only into binaries produced by the repository's local development build paths. Local development-audit builds enable the hook automatically and inject the Agent Runner checkout that built them; production builds expose no audit option or embedded workflow.
- Launch automatic auditing asynchronously as soon as the source workflow reaches a terminal outcome and its durable evidence is finalized, independently of the run-view lifecycle. Keep the source completion view available, allow the audit to continue after that view exits, and preserve the source outcome and exit status regardless of audit or reporting failure.
- Audit each successful, failed, or stopped execution session that Agent Runner can finalize. Link resumed execution sessions to the same source run and record overlap or supersession so repeated evidence is not counted as independent observations.
- Add a deterministic evidence-preparation stage before model judgment. It creates a compact per-step index and bounded default evidence package with explicit coverage, omissions, and links back to the complete local evidence.
- Allow both model-driven audit steps to inspect detailed local evidence selectively when deeper verification is needed, including Runner-owned outputs, diffs, artifacts, and any native agent-session material that is discoverable without adding a new transcript subsystem. Record what they consulted, make unavailable categories explicit, and keep all detailed evidence local.
- Capture each step's starting and ending Git revision plus working-tree, index, and untracked-file state. Use those deltas to recognize work before a commit exists, and use the workflow commit prefix `[<step_id>]` as direct commit attribution. Record explicit no-change outcomes and leave ambiguous or unprefixed commits unattributed rather than guessing.
- Treat attributable working-tree changes and attributed commits and diffs as the primary evidence of value delivered by a step. Mark changes committed by a later bulk commit as deferred from the earlier step so they are not credited twice. Use the step's findings, available session evidence, artifacts, tests, validator results, and downstream outcomes as supporting evidence.
- Extend run instrumentation where necessary to capture per-step repository and artifact checkpoints, changed-file identity, and trustworthy usage, cost, and duration where available. Preserve measurements that Runner cannot observe, especially interactive-step usage and cost, as explicitly unknown.
- Give the audit workflow two separate model-driven stages, both resolved through the existing `crosscheck` role using the source run's recorded profile-set name and current profile configuration, with the resolved agent/model provenance frozen into the audit, and with different contracts:
  - a workflow-value audit that writes high-level qualitative and quantitative observations to a lightweight longitudinal dataset and never opens GitHub issues;
  - an Agent Runner correctness audit that verifies suspected defects against run evidence and current code, checks for existing issues, and files an evidence-backed GitHub issue for each confirmed new problem.
- Keep value records intentionally lightweight: source workflow, run, execution session, step, attributed commit, important cost and outcome metrics, categorical value judgments, basic judge/model identification, coarse confidence, and whether important evidence was unavailable.
- Persist the complete validated audit report locally so audits can be inspected, retried, rerun against historical run IDs, and reassessed before any code or workflow change is made.
- Add a simple reporting stage that writes value observations to one existing Google Sheet through a protected, user-scoped development integration record. Do not build a hosted service or generalized storage platform.
- Never write transcripts, transcript summaries, prompts, responses, tool calls, command output, source code, diffs, artifact contents, evidence excerpts, local paths, or other detailed run material to the lightweight external dataset.
- Define a versioned row or record format so observations remain comparable over time. Failed writes retain the complete local report for retry and surface only a non-blocking warning.
- Keep audit workflows hidden from ordinary workflow discovery, link audit runs to their source runs for inspection, tolerate linked audit runs in ordinary run history, and prevent an audit workflow from auditing itself.
- Define ordering with existing intake routing so the audit launches promptly and only one post-run transition owns the foreground process at a time.

## Capabilities

### New Capabilities

- `automatic-run-audit`: Development-build-only, runner-managed launch of a linked audit workflow after eligible OpenSpec and spec-driven execution sessions.
- `run-audit-replay`: Explicit invocation of the same audit workflow for a recorded run, enabling historical backfill and repeated evaluation without rerunning the source workflow.
- `audit-evidence-preparation`: Bounded, coverage-aware indexing of durable run evidence with selective local access to the underlying details.
- `workflow-value-observation`: Commit-first model judgment of each workflow step's cost, changes, correctness, unique contribution, duplication, regressions, and likely downstream value.
- `workflow-correctness-audit`: Model-assisted verification of Agent Runner orchestration defects and deduplicated GitHub issue filing for confirmed problems.
- `lightweight-audit-reporting`: Versioned reporting of high-level value observations to one existing Google Sheet while detailed evidence remains local.
- `development-audit-availability`: Build-time exclusion from production, automatic local-development activation, build-source injection, and protected Google connection state.

### Modified Capabilities

- `audit-log-lifecycle`: Record source-to-audit linkage, audit launch and completion, reporting warnings, and the Git revision evidence needed for step attribution.
- `run-metrics-artifact`: Expose execution-session identity, evidence coverage, and additional trustworthy step-level measurements needed by the audit workflow.

## Technical Approach

Treat automatic auditing as a runner lifecycle extension rather than appending an ordinary step to every workflow. A private build tag compiles the audit provider and hidden workflow only into binaries produced by `make build` and `dev.sh`; release and ordinary untagged builds do not register the capability. The local build injects its Agent Runner source root and Git provenance, eliminating an audit enablement setting and a separate repository setting. Once an eligible top-level execution session records its terminal outcome and closes or flushes its durable evidence, the runner starts a separate linked headless audit run. This launch does not wait for the user to leave the completed source run view, and the linked audit continues independently if that view exits. Explicit audit identity prevents recursion, while lifecycle ordering lets an existing intake route proceed without surrendering the audit launch.

The same audit workflow accepts an explicit source run identifier for manual replay and historical backfill. Each observation identifies both the stable source run and the particular Agent Runner execution session that triggered it. When a run is resumed, later observations retain lineage to earlier ones and identify overlapping or superseded step evidence rather than presenting every cumulative audit as an independent sample.

Before either model step runs, a deterministic preparation stage creates an immutable audit-launch snapshot of the finalized source-run evidence and indexes audit events, metrics, captured outputs, workflow artifacts, validator evidence, and Git state into a bounded default package. Step-boundary working-tree deltas associate uncommitted work with the step that produced it; revision intervals and `[<step_id>]` commit prefixes associate commits without crediting a later bulk commit twice. The package records missing or truncated evidence and points back to the snapshotted local sources. Model auditors begin with this package but retain read-only access to detailed local evidence that is actually available for targeted investigation; the workflow records the scope of evidence consulted.

The workflow-value stage treats attributed commits and their diffs as the primary account of delivered value, then compares them with the step's stated findings and downstream validation. It emits one small structured record for each executed leaf workflow step, aggregating attempts, iterations, and agent-internal child usage owned by that leaf. Basic rubric and judge identification, confidence, and evidence availability make the record interpretable without creating an elaborate finding-management or human-adjudication system. Individual value findings remain hypotheses until the underlying code and evidence are independently verified.

The correctness stage separately examines the source run for Agent Runner behavior that is inconsistent with current code, specifications, or workflow intent. It snapshots the injected Agent Runner checkout at audit launch, treats that snapshot as authoritative for whether the behavior remains a current defect, and records both build-time and launch-time revision/dirty provenance as diagnostic context. Before filing, it verifies the suspected defect against repository evidence and searches existing GitHub issues to avoid duplicates. Each confirmed new defect produces a focused issue whose title begins `[auto-audit]` and includes enough redacted evidence to reproduce and diagnose it. Filing an issue does not authorize a fix, and the correctness stage never edits the repository.

The complete report and detailed evidence stay local. A final reporting stage writes only the approved high-level value fields to one existing Google Sheet using the Sheets API directly. A one-time local command imports compatible existing Google OAuth material and the destination into protected Agent Runner user storage; runtime behavior has no dependency on Hermes or another source application. Agent Runner will not introduce or operate a network service or generalized sink framework for this experiment.

This workflow-based approach keeps model judgment observable and separately costed using Agent Runner's existing orchestration model. A deterministic collector alone cannot make the qualitative judgments being tested, while a separate offline tool would duplicate run resolution, agent selection, metrics, and lifecycle behavior. A simple semi-structured sink is preferred over a purpose-built service because one operator is collecting an exploratory dataset and the schema is expected to evolve.

Audit, issue-filing, and dataset-write failures remain observable but non-blocking. The source run retains its original result, resumability, and diagnostic details; the audit run has its own status and cost. Audit cost is reported separately from the workflow being evaluated so the measurement system does not obscure its own overhead.

## Out of Scope

- Simplifying, removing, reordering, or automatically modifying workflow steps based on collected observations.
- Applying code fixes from correctness findings; repository evidence must be verified again before any later change.
- Creating GitHub issues from workflow-value observations. Only the correctness stage may file issues for confirmed, deduplicated defects.
- Uploading or copying detailed run evidence into the lightweight external dataset.
- Building or operating a hosted reporting service, credential service, generalized storage abstraction, or analytics UI.
- Public enablement documentation, onboarding, consent prompts, settings-editor controls, or any production audit option while the feature remains private to development builds.
- Auditing workflow families other than OpenSpec and spec-driven workflows.
- Auditing nested sub-workflows independently from their top-level execution.
- Automatically launching an audit after a process is terminated before Agent Runner can finalize the execution session; surviving partial runs may still be audited explicitly.
- Proving causal workflow uplift from individual observational runs; controlled comparisons remain a later evaluation phase.

## Impact

- Runner finalization and lifecycle orchestration must launch one non-recursive linked audit run without waiting for run-view exit or mutating the completed source result.
- `make build` and `dev.sh` gain a private build tag and injected source provenance; untagged and release builds exclude the audit provider, workflow asset, and commands.
- A new hidden development audit workflow, local-only replay/setup entry points, evidence-preparation contract, and structured local output are added.
- Audit logs gain source/audit linkage, execution-session lineage, lifecycle events, and step-boundary Git evidence. `run-metrics.json` advances to schema v3 with explicit execution-session identity and migration from v2.
- Existing source run views continue to show the source workflow's completed state. Linked audits may appear as ordinary run-history entries, remain identifiable by kind and reciprocal linkage, and require no new TUI navigation.
- The correctness audit requires read-only repository inspection plus permission to search and create GitHub issues, but never permission to modify code.
- One direct Google Sheets integration and a small versioned value-record format are implemented using protected user-scoped connection state.
- Eligible local-development runs incur additional model cost, latency, local storage, GitHub activity when defects are confirmed, and reporting writes; the audit's own cost is recorded separately.
