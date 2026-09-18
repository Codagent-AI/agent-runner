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

Usage records SHALL represent token counts in distinct categories — input, cached input, cache writes, output, and reasoning output — as provided by the CLI. A category the CLI does not report SHALL remain absent in raw categories and explicitly unavailable in the common field envelope, not zero. Agent Runner SHALL NOT collapse categories into a single total at collection time.

The canonical v1 vocabulary SHALL include `input_total`, `input_uncached`, `cache_read`, `cache_write`, `output`, `reasoning`, `provider_total`, and `normalized_total`. Each mapped value SHALL retain available/partial/unavailable state, known value or null, reason, observed/derived origin, exact/approximate precision, source, derivation, and inclusion relationships where established. Adapters SHALL map only source-supported semantics; absence of a cache category MUST NOT become observed zero. A partial value SHALL retain its known subtotal rather than being discarded or presented as complete. Preserve allowlisted native usage evidence independently of canonical mapping.

#### Scenario: Categories preserved as reported
- **WHEN** a CLI reports input, cached-input, output, and reasoning-output counts
- **THEN** the usage record stores each category separately with its reported value

#### Scenario: Unreported category is absent, not zero
- **WHEN** a CLI's output provides no count for a token category (for example, no cache-write count)
- **THEN** the usage record marks that category as absent rather than recording `0`

#### Scenario: Partial native value
- **WHEN** native telemetry establishes only a partial token subtotal
- **THEN** the common measurement retains its numeric subtotal, reason, and precision and the aggregate remains partial

#### Scenario: Observed zero differs from unavailable
- **WHEN** a source explicitly reports zero cache-read tokens but omits cache-write usage
- **THEN** cache-read is available zero and cache-write is unavailable

### Requirement: Canonical processed-token totals

In addition to preserving provider-reported categories, an adapter SHALL report canonical processed-token totals for input, output, and overall tokens when the CLI reports those totals or the adapter can derive them without double-counting according to that CLI's accounting semantics. The adapter SHALL own this normalization. It SHALL NOT blindly add raw category fields whose relationships may overlap. When a reliable overall total cannot be obtained, the canonical totals SHALL be absent rather than fabricated.

Native normalization SHALL preserve category inclusion relationships and explicit unavailable canonical fields when a reliable total cannot be established. Normalization and aggregate views SHALL never sum overlapping cache/reasoning categories or sum allocation rows again alongside their attempt total. Existing per-turn versus cumulative semantics, no-baseline protection, and reset protection SHALL apply before projecting common measurements. Native identity/measurement migration SHALL not fabricate missing historical evidence.

#### Scenario: Adapter derives non-overlapping totals
- **WHEN** a CLI reports cache or reasoning categories separately and its documented accounting semantics establish how they relate to input and output
- **THEN** the adapter records canonical input, output, and overall totals that count each token exactly once while preserving the original categories

#### Scenario: Uncertain accounting leaves totals absent
- **WHEN** a CLI reports token categories but their overlap is not reliably known
- **THEN** the categories remain available and canonical totals are absent

#### Scenario: Cumulative canonical totals are attributed per step
- **WHEN** a cumulative CLI reports canonical totals for a resumed session
- **THEN** the collector attributes the totals against the prior cumulative baseline using the same no-baseline and counter-reset protections as the raw categories

#### Scenario: Native and nested totals share semantics
- **WHEN** a workflow has a native parent, a called child, and Validator model attempts
- **THEN** common aggregates count each attempt once with independent field coverage, preserve partial values, and exclude child usage from the parent's own measurement

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

Every native step and called-agent measurement SHALL distinguish requested CLI/model/effort, launch-resolved identity, and telemetry-observed model identities using the common versioned measurement vocabulary. Resolution MUST NOT establish observation. Observed identities SHALL retain stable references when supported; multiple observed models SHALL remain distinct within one attempt. Usage SHALL preserve measurement source, field availability, derivation, inclusion, precision, and independent collection/attribution completeness. Producer/build, adapter/parser mapping, CLI, and source-format versions SHALL be retained or explicitly unavailable. Native provenance SHALL identify Runner and its actual source, never fabricate Validator producer/session identity.

#### Scenario: Provenance recorded with usage
- **WHEN** a usage record is collected from a completed agent step
- **THEN** the record includes distinct requested, resolved, and observed identity with identity provenance, the measurement source, and a completeness indicator

#### Scenario: Observed model preserves requested alias
- **WHEN** a user requests Claude model alias `sonnet` and terminal telemetry reports `claude-sonnet-5`
- **THEN** requested model remains `sonnet`, observed model is `claude-sonnet-5`, resolved launch identity is retained separately, and observed model provenance is telemetry

#### Scenario: Invocation identity fills omitted telemetry model
- **WHEN** Runner invokes Codex model `gpt-5.6-terra` and `turn.completed` omits a model field
- **THEN** requested and resolved model identify `gpt-5.6-terra`, while observed model remains unavailable with an explicit reason

#### Scenario: Partial measurement flagged
- **WHEN** a CLI reports only a subset of the token categories its adapter expects it to provide
- **THEN** the record's completeness indicator reflects a partial measurement

#### Scenario: Source version is unavailable
- **WHEN** a captured native event does not identify the provider CLI or event-format version
- **THEN** the measurement records those version fields as unavailable rather than guessing them from the current installation

#### Scenario: One dispatch observes multiple models
- **WHEN** native telemetry provides multiple model identities or allocations for one dispatch
- **THEN** the measurement preserves supported identities, stable allocation references, unallocated evidence, and partial attribution without inventing additional attempts or distributing unknown usage

### Requirement: Non-agent step usage

Non-agent steps (shell, UI, and other step types that invoke no agent CLI) SHALL report zero token usage while retaining their measured duration. Zero here is a true measurement — no tokens were consumed — and is distinct from the unavailable state.

#### Scenario: Shell step reports zero usage
- **WHEN** a shell step completes
- **THEN** its metrics carry zero token usage and the step's duration in milliseconds

### Requirement: Attribution follows source counter semantics

Agent Runner SHALL distinguish per-turn reports from cumulative session counters. A per-turn report SHALL be recorded directly for every invocation, including resumed sessions; it MUST NOT be subtracted from a prior turn. When an adapter explicitly identifies a report as cumulative, the existing baseline/delta safeguards apply and missing baselines or resets remain unavailable rather than fabricated.

#### Scenario: Resumed Codex turn is recorded directly
- **WHEN** a resumed Codex invocation emits `turn.completed.usage`
- **THEN** the complete reported snapshot is attributed to that invocation without comparing it to or subtracting the preceding turn

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

