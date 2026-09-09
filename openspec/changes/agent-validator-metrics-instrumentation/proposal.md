## Why

Runner's current Validator bridge expects JSONL records with one effective model and deduplicates them within a parent attempt. Validator now publishes richer, revisioned measurements through a durable CLI export/acknowledgment protocol. Without the companion integration, Runner loses nested usage or misattributes replayed work, and downstream consumers cannot account for partial or multi-model evidence correctly.

## What Changes

- Replace the declared Validator JSONL bridge with correlated launch, bounded export, verified durable incorporation, acknowledgment, and recovery.
- Introduce workflow metrics schema v4 with original attribution, current producer measurement heads, independent completeness and delivery gaps, and lossless accepted records.
- Align native measurements with the common identity, token, allocation, cost, and provenance vocabulary; preserve existing source-counter safeguards and historical uncertainty.
- Update the post-run local audit to consume the same v4 artifact, attribute Validator and called-agent measurements to their owning leaves, preserve coverage/gaps, and snapshot after bounded final delivery with later recovery available through explicit replay.
- Update bundled Validator scripts and integration documentation. Pin producer contracts and exercise real delivery through the local feature-bearing Validator CLI.

## Capabilities

### New Capabilities

- `validator-metrics-delivery`: Correlated Validator launches, strict protocol ingestion, durable receipt handling, bounded recovery, and failure isolation.

### Modified Capabilities

- `run-metrics-artifact`: Schema v4, revision replacement, lossless measurement storage, original attribution, migration, and honest aggregates.
- `agent-usage-collection`: Common measurement semantics and distinct requested, launch-resolved, and telemetry-observed identities for native agent steps and calls.
- `task-compliance-activation`: Preserve task-file activation while permitting independent Validator metrics correlation arguments.
- `workflow-value-observation`: Include current attributable child measurements once per owning leaf with honest local quantitative evidence.
- `audit-evidence-preparation`: Supply v4 metrics and delivery gaps to both audit stages, and order the automatic snapshot after bounded final delivery.
- `run-audit-replay`: Filter all v4 quantitative evidence by original execution session and expose later recovery only through a new replay snapshot.

## Out of Scope

Changes to Agent Validator or Agent Evals implementations; changes to audit eligibility, the value rubric, correctness issue policy, or external value dataset columns; pricing catalogs or currency conversion; new paid provider captures; a TUI redesign; additional telemetry producers; background delivery daemons; automatic deletion/discard of pending evidence; automatic storage relocation; support or conversion for measurement versions beyond v1. Ordinary existing metrics displays must continue to render supported values and uncertainty.

## Impact

Affects `internal/model`, `internal/cli`, `internal/exec`, `internal/metrics`, `internal/devaudit`, Runner lifecycle and recovery command wiring, durable metrics persistence, bundled Validator scripts, tests, and usage documentation. External artifact consumers must explicitly support v4; this branch supplies the contract and fixtures without claiming that Evals already supports it. Existing artifacts migrate without losing recorded attempts. Telemetry failures continue to leave workflow and validation outcomes unchanged.

The full Runner companion scope was approved in the plan conversation. The user also approved updating the post-run local audit as a consumer of the same measurements. The user additionally requires integration tests to use the local Agent Validator CLI rather than the globally installed release. See `design.md` for the verified local invocation and contract pin.
