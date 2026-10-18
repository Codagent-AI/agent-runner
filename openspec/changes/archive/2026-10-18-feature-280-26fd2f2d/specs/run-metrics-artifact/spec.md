## ADDED Requirements

### Requirement: Context measurement in step records

Each agent execution attempt record in `run-metrics.json` whose CLI was launched SHALL include the attempt's context measurement as defined by `agent-context-size`: last observed request input, peak context, context window limit, status, reason, and source. Unavailable values SHALL appear as explicit nulls with a status and reason, never as zeros. Re-executed steps SHALL carry each attempt's own measurement in that attempt's record. Records of non-agent steps, iteration records, and agent steps that never launched their CLI SHALL NOT include a context measurement.

#### Scenario: Agent attempt record content
- **WHEN** an agent step attempt completes with an available context measurement
- **THEN** its record in `run-metrics.json` contains the last observed request input, peak context, context window limit or null, status `available`, and source

#### Scenario: Re-executed step
- **WHEN** a step is executed twice in a run, with peak context 40,000 on the first attempt and 70,000 on the second
- **THEN** the two attempt records carry 40,000 and 70,000 respectively

#### Scenario: Unavailable context in artifact
- **WHEN** an agent attempt's context cannot be measured
- **THEN** its record shows null values with status `unavailable` and a reason

### Requirement: Agent-session context summary

`run-metrics.json` SHALL contain a run-level agent-session context summary with one entry per distinct agent CLI session, keyed by CLI name plus the agent session ID recorded on the step records, across every execution session of the run. Each entry SHALL include:

- the CLI name and agent session ID;
- the **high-water mark**: the maximum peak context across the entry's attempts;
- the **maximum last observed request input** across the entry's attempts, as a separately named value;
- the largest context window limit reported for the entry's attempts, or null;
- the number of attempts in the entry;
- a **coverage** indicator: `complete` when every attempt in the entry has an `available` measurement, `partial` when at least one attempt contributed a value but some are `partial` or `unavailable`, and `none` when no attempt contributed a value.

Attempts that launched their CLI but have no recorded agent session ID SHALL be reported in an explicit unattributed count and SHALL NOT be merged into any keyed entry or with each other. Execution-session rollups SHALL keep their existing semantics and SHALL NOT be regrouped by agent session. The summary SHALL be recomputed whenever the artifact is written, so it stays consistent with the step records after resume.

#### Scenario: Several fresh sessions in one invocation
- **WHEN** one Runner invocation runs ten agent steps that each start a new agent session
- **THEN** the summary contains ten entries, one per agent session

#### Scenario: Inherited session spans a resume
- **WHEN** an agent session S is used by a step before a Runner interruption and inherited by another step after the run resumes
- **THEN** the summary contains one entry for S covering both attempts, while the execution-session rollups still show two invocations

#### Scenario: High-water mark uses peak
- **WHEN** a session's single attempt reaches peak context 180,000, compacts, and ends with last observed request input 30,000
- **THEN** the session's high-water mark is 180,000 and its maximum last observed request input is 30,000

#### Scenario: Partial session coverage
- **WHEN** a session has two attempts, one `available` with peak 90,000 and one `unavailable`
- **THEN** the entry's high-water mark is 90,000 and its coverage is `partial`

#### Scenario: Same session ID on different CLIs
- **WHEN** a Claude attempt and a Codex attempt report the same session ID string
- **THEN** they appear as two separate entries

#### Scenario: Missing session identity
- **WHEN** two agent attempts launched their CLI but have no recorded agent session ID
- **THEN** the summary's unattributed count is 2 and no keyed entry includes them

### Requirement: Context measurement excluded from totals

Context measurements SHALL NOT contribute to run totals, per-category token totals, canonical totals, cost, usage or cost coverage indicators, or execution-session rollup totals. Adding context measurement SHALL NOT change any existing aggregate value.

#### Scenario: Aggregates unaffected
- **WHEN** a run's attempts carry context measurements
- **THEN** every existing run-level and execution-session aggregate equals the value computed from usage and cost alone

### Requirement: Artifacts without context measurements

When Agent Runner resumes a run whose existing `run-metrics.json` records attempts without context measurements, it SHALL retain those attempts unchanged, treat their context as unavailable with a reason indicating it was not recorded, and include them in the agent-session context summary's coverage accordingly. It SHALL NOT invent context values for them.

#### Scenario: Resume over an older artifact
- **WHEN** a run started by a binary without context measurement is resumed by one with it, and an inherited agent session spans both
- **THEN** the earlier attempts keep their original records, the session's summary entry has coverage `partial`, and its high-water mark comes only from the newer attempts

#### Scenario: Schema version unchanged
- **WHEN** an artifact with context measurements is written
- **THEN** its schema version is the same as an artifact without them, because the fields are additive and backward compatible
