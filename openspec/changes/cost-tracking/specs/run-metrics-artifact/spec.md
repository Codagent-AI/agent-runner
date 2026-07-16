# run-metrics-artifact Specification (delta)

## ADDED Requirements

### Requirement: Artifact location

Agent Runner SHALL write a machine-readable metrics artifact named `run-metrics.json` in the run session directory, alongside `audit.log` and `state.json`:

```text
~/.agent-runner/projects/{encoded-path}/runs/{run-id}/run-metrics.json
```

This artifact is the supported boundary for external consumers (Agent Evals and others); consumers SHALL NOT need to reconstruct metrics from audit internals or CLI transcripts.

#### Scenario: Artifact created in run directory
- **WHEN** a workflow run completes its first step
- **THEN** `run-metrics.json` exists in that run's session directory

### Requirement: Versioned schema

`run-metrics.json` SHALL carry a top-level schema version field. Backward-incompatible changes to the artifact's structure SHALL increment the version. Consumers can rely on a given version's structure remaining stable across Agent Runner releases.

<!-- deferred-to-design: exact field names of the schema (including the version field's name
     and numbering scheme) are fixed in the design document before implementation. -->

#### Scenario: Version field present
- **WHEN** any `run-metrics.json` is written
- **THEN** it contains the schema version identifying its structure

### Requirement: Artifact content

`run-metrics.json` SHALL contain one record per executed step and a run-level aggregate. Each step record SHALL include the step's identifier and nesting prefix, step type, outcome, duration in milliseconds, the usage record (token categories plus provenance and completeness, per `agent-usage-collection`), and `estimated_api_cost_usd` (per `cost-capture`). The run-level aggregate SHALL include the run's duration, per-category token totals, and the cost total with its coverage indicator. Unavailable usage and absent cost SHALL appear as explicit null/unavailable states, never as zeros.

#### Scenario: Agent step record content
- **WHEN** an autonomous-headless agent step completes with usage and cost collected
- **THEN** its record in `run-metrics.json` carries the step identifier, prefix, type, outcome, duration, token categories with provenance, and the reported cost

#### Scenario: Run aggregate content
- **WHEN** a run ends
- **THEN** the artifact's run-level aggregate carries the run duration, per-category token totals across all steps, and the cost total with coverage

#### Scenario: Unavailable data explicit in artifact
- **WHEN** a step's usage is unavailable
- **THEN** the artifact represents that step's usage as an explicit unavailable state and its cost as null, not as zeros

### Requirement: Incremental atomic writes

Agent Runner SHALL update `run-metrics.json` after each step completes and finalize it at run end. Each write SHALL be atomic (write-then-rename), so the file is always well-formed JSON. A run that is interrupted or crashes SHALL leave an artifact containing the metrics of every step that completed before the interruption.

#### Scenario: Interrupted run leaves valid partial artifact
- **WHEN** a run is killed after two steps completed and a third was in progress
- **THEN** `run-metrics.json` is valid JSON containing the two completed steps' records

#### Scenario: Reader never sees a torn file
- **WHEN** an external consumer reads `run-metrics.json` while a run is writing it
- **THEN** the consumer sees either the previous complete version or the new complete version, never a partial write

### Requirement: Cumulative aggregation across resume sessions

When a run is resumed, `run-metrics.json` SHALL accumulate: step records from earlier sessions of the run are retained, new steps are appended, and run-level aggregates cover all sessions, so the artifact always describes the whole run.

#### Scenario: Resumed run accumulates metrics
- **WHEN** a run executes two steps, is interrupted, and is later resumed to execute two more
- **THEN** the final `run-metrics.json` contains all four step records and run totals spanning both sessions
