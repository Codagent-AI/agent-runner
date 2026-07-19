# agent-usage-collection Specification

## Purpose
TBD - created by archiving change cost-tracking. Update Purpose after archive.
## Requirements
### Requirement: Usage collection on autonomous-headless agent steps

After an autonomous-headless agent step's CLI process exits, Agent Runner SHALL obtain a token-usage record for that step by extracting structured usage data from the CLI's captured stdout via the step's CLI adapter. Extraction SHALL be attempted for every autonomous-headless agent step regardless of which CLI it uses and regardless of the step's exit code: usage and cost successfully extracted from a failed step's output SHALL be retained and included in run-level aggregates, since those tokens were consumed regardless of outcome.

#### Scenario: Adapter supports usage extraction
- **WHEN** an autonomous-headless agent step completes, its adapter supports usage extraction, and the CLI's structured output contains usage data
- **THEN** the step's usage record contains the token counts the CLI reported

#### Scenario: Adapter does not support usage extraction
- **WHEN** an autonomous-headless agent step completes and its adapter does not support usage extraction
- **THEN** the step's usage record is recorded as unavailable (not zero)

#### Scenario: Failed step retains extracted usage
- **WHEN** an autonomous-headless agent step exits with a nonzero code but its structured output contains valid usage data
- **THEN** the step's usage record contains the reported counts (and any reported cost is captured per `cost-capture`), and the step's usage and cost are included in run-level aggregates

### Requirement: Distinct token categories

Usage records SHALL represent token counts in distinct categories — input, cached input, cache writes, output, and reasoning output — as provided by the CLI. A category the CLI does not report SHALL be recorded as absent, not as zero. Agent Runner SHALL NOT collapse categories into a single total at collection time.

#### Scenario: Categories preserved as reported
- **WHEN** a CLI reports input, cached-input, output, and reasoning-output counts
- **THEN** the usage record stores each category separately with its reported value

#### Scenario: Unreported category is absent, not zero
- **WHEN** a CLI's output provides no count for a token category (for example, no cache-write count)
- **THEN** the usage record marks that category as absent rather than recording `0`

### Requirement: Canonical processed-token totals

In addition to preserving provider-reported categories, an adapter SHALL report canonical processed-token totals for input, output, and overall tokens when the CLI reports those totals or the adapter can derive them without double-counting according to that CLI's accounting semantics. The adapter SHALL own this normalization. It SHALL NOT blindly add raw category fields whose relationships may overlap. When a reliable overall total cannot be obtained, the canonical totals SHALL be absent rather than fabricated.

#### Scenario: Adapter derives non-overlapping totals
- **WHEN** a CLI reports cache or reasoning categories separately and its documented accounting semantics establish how they relate to input and output
- **THEN** the adapter records canonical input, output, and overall totals that count each token exactly once while preserving the original categories

#### Scenario: Uncertain accounting leaves totals absent
- **WHEN** a CLI reports token categories but their overlap is not reliably known
- **THEN** the categories remain available and canonical totals are absent

#### Scenario: Cumulative canonical totals are attributed per step
- **WHEN** a cumulative CLI reports canonical totals for a resumed session
- **THEN** the collector attributes the totals against the prior cumulative baseline using the same no-baseline and counter-reset protections as the raw categories

### Requirement: Unavailable usage is explicit

When usage cannot be collected for an agent step, Agent Runner SHALL record an explicit unavailable state with the reason. Missing usage SHALL never be represented as zero tokens. Situations that produce an unavailable record include: PTY-backed invocation contexts (interactive and autonomous-interactive), structured-output parse failures, missing usage events in otherwise valid output, and adapters that do not support extraction.

#### Scenario: PTY-backed agent step reports unavailable
- **WHEN** an agent step runs in an interactive or autonomous-interactive context (no stdout captured)
- **THEN** the step's usage record is an explicit unavailable state with a reason indicating the invocation context

#### Scenario: Parse failure reports unavailable
- **WHEN** an autonomous-headless agent step completes but its stdout cannot be parsed as the expected structured format
- **THEN** the step's usage record is an explicit unavailable state, the step's outcome is otherwise unaffected, and no zero counts are recorded

#### Scenario: Missing usage event reports unavailable
- **WHEN** an autonomous-headless agent step's structured output is otherwise valid but ends without the event that carries usage data
- **THEN** the step's usage record is an explicit unavailable state

### Requirement: Usage record provenance

Every usage record SHALL retain provenance alongside the token counts: the CLI (adapter) that produced it, the provider where available, the actual model reported by the CLI where available, the measurement source (e.g. which output event supplied the data), and a completeness indicator distinguishing full, partial, and unavailable measurements.

#### Scenario: Provenance recorded with usage
- **WHEN** a usage record is collected from a completed agent step
- **THEN** the record includes the CLI name, the provider and reported model (when the CLI provides them), the measurement source, and a completeness indicator

#### Scenario: Partial measurement flagged
- **WHEN** a CLI reports only a subset of the token categories its adapter expects it to provide
- **THEN** the record's completeness indicator reflects a partial measurement

### Requirement: Non-agent step usage

Non-agent steps (shell, UI, and other step types that invoke no agent CLI) SHALL report zero token usage while retaining their measured duration. Zero here is a true measurement — no tokens were consumed — and is distinct from the unavailable state.

#### Scenario: Shell step reports zero usage
- **WHEN** a shell step completes
- **THEN** its metrics carry zero token usage and the step's duration in milliseconds

### Requirement: Per-step attribution for cumulative usage sources

When a CLI reports cumulative session totals rather than per-invocation usage, the usage recorded for a step that resumes an existing session SHALL reflect only that step's consumption, not the session's lifetime total. Attribution SHALL never produce a negative or fabricated token count: when the reported cumulative total is lower than the session's previously recorded total (e.g. a counter reset), the step's usage SHALL be recorded as unavailable. When a session is resumed but no previously recorded total for it exists within the run, the step's usage SHALL be recorded as unavailable rather than attributing the session's lifetime total to the step; the reported cumulative value SHALL be retained in provenance and SHALL serve as the prior total for subsequent invocations of that session. A token category present in the prior total but absent from the current report SHALL produce no value for that category (absent, never negative); a category absent from the prior total SHALL be attributed from zero.

#### Scenario: Resumed session step records its own usage
- **WHEN** an agent step resumes a session whose earlier step already consumed tokens, and the CLI reports cumulative session totals
- **THEN** the resumed step's usage record reflects only the tokens consumed by that step's invocation

#### Scenario: Resumed session without prior total yields unavailable
- **WHEN** an agent step resumes a session whose earlier consumption was never recorded in this run, and the CLI reports cumulative session totals
- **THEN** the step's usage record is an explicit unavailable state, the reported cumulative value is retained in provenance, and a subsequent step on the same session is attributed relative to that retained value

#### Scenario: Cumulative counter reset yields unavailable
- **WHEN** a step's reported cumulative total is lower than the total previously recorded for that session
- **THEN** the step's usage record is an explicit unavailable state; no negative counts are recorded; subsequent attribution for that session is based on the newly reported total

#### Scenario: Category disappears from cumulative reporting
- **WHEN** a token category present in the session's previously recorded total is absent from the current report
- **THEN** the step's usage record contains no value for that category (absent, never negative)

#### Scenario: Category appears mid-session
- **WHEN** the current report contains a token category absent from the session's previously recorded total
- **THEN** that category's attributed value equals the newly reported value (attributed from zero)
