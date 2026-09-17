## MODIFIED Requirements

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
