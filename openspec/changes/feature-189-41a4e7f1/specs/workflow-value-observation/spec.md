## MODIFIED Requirements

### Requirement: Local value records contain approved high-level fields

Each local step observation SHALL contain the following high-level field groups:

- identity: schema version, stable observation identity, observation timestamp, project, workflow, source run, execution session, audit run, automatic or replay trigger, source outcome, full leaf-step identity, lineage, and step outcome;
- cost: duration, cost, total tokens, and source model identity where each is trustworthy;
- change: Git attribution state, attributed commit SHA or SHAs, changed-file count, additions, and deletions;
- judgment: every fixed rubric dimension, judge model identity, rubric version, and the optional short note.

The local report SHALL also contain, once per audit rather than per observation, the audit-level judge usage summary defined by `audit-judge-usage`. That summary covers the judge identity, per-attempt usage records, and aggregate tokens and cost with coverage. Judge usage SHALL NOT be folded into any observation's cost field group.

Unknown quantitative values SHALL remain explicitly unknown rather than becoming zero. The complete local report MAY retain additional detailed evidence and diagnostic fields that are prohibited from the external value dataset.

Local metric evidence SHALL preserve v4 field-level known subtotals, availability, precision, original model identity provenance, and independent measurement, attribution, history, and delivery coverage, including Validator evidence. Partial usage or cost SHALL remain usable as explicitly partial evidence. A scalar total-token or USD-cost field that cannot express partial coverage SHALL remain unknown unless the complete leaf value is established; the known subtotal and reasons SHALL still be retained in detailed local evidence and supplied to the auditors. Multiple models and allocation-scoped costs SHALL not be flattened into a fabricated single model or complete attempt cost. Model aliases and launch-resolved identities SHALL remain distinguishable from telemetry-observed identities.

This change SHALL preserve the existing external value dataset's approved columns and privacy boundary. The only additions are the trailing audit-level judge usage columns defined by `lightweight-audit-reporting`. Detailed measurements and delivery diagnostics SHALL remain in the local report and bounded audit evidence; any existing scalar projection SHALL preserve unknown values instead of publishing a partial subtotal as a complete total.

#### Scenario: Cost is unavailable
- **WHEN** a source step reports duration and tokens but no trustworthy monetary cost
- **THEN** the observation retains duration and tokens and marks cost unknown

#### Scenario: Several models contribute to one logical step
- **WHEN** a step and its declared child agents use more than one source model
- **THEN** the local observation preserves their basic model identities without copying prompts or responses

#### Scenario: Observation is ready for reporting
- **WHEN** the value stage completes validation of an observation
- **THEN** the approved high-level fields and stable observation identity can be projected to the external dataset while detailed local fields remain excluded

#### Scenario: Partial Validator usage remains useful
- **WHEN** a leaf's Validator attempt reports partial output usage with a known subtotal
- **THEN** the local report and model evidence retain that subtotal, precision, and partial coverage while the scalar leaf token total remains unknown if completeness cannot be established

#### Scenario: Validator cost lacks a full USD total
- **WHEN** a confirmed nested dispatch reports only allocation-scoped, partial, or overlapping costs
- **THEN** the audit retains the scoped evidence and cost-coverage limitation without treating it as a full leaf USD cost

#### Scenario: Delivery gap is visible to value judgment
- **WHEN** the snapshot records a blocked, missing, or discarded Validator delivery relevant to a leaf
- **THEN** the audit exposes the gap, does not classify affected evidence as complete, and does not substitute zero usage or cost

#### Scenario: Local report carries judge usage once
- **WHEN** an audit with several step observations assembles its local report
- **THEN** the report contains one audit-level judge usage summary, and no observation's duration, cost, or total-token values include judge usage
