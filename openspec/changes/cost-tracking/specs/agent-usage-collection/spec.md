# agent-usage-collection Specification (delta)

## ADDED Requirements

### Requirement: Usage collection on autonomous-headless agent steps

After an autonomous-headless agent step's CLI process exits, Agent Runner SHALL obtain a token-usage record for that step by extracting structured usage data from the CLI's captured stdout via the step's CLI adapter. Extraction SHALL be attempted for every autonomous-headless agent step regardless of which CLI it uses.

#### Scenario: Adapter supports usage extraction
- **WHEN** an autonomous-headless agent step completes, its adapter supports usage extraction, and the CLI's structured output contains usage data
- **THEN** the step's usage record contains the token counts the CLI reported

#### Scenario: Adapter does not support usage extraction
- **WHEN** an autonomous-headless agent step completes and its adapter does not support usage extraction
- **THEN** the step's usage record is recorded as unavailable (not zero)

### Requirement: Distinct token categories

Usage records SHALL represent token counts in distinct categories — input, cached input, cache writes, output, and reasoning output — as provided by the CLI. A category the CLI does not report SHALL be recorded as absent, not as zero. Agent Runner SHALL NOT collapse categories into a single total at collection time.

#### Scenario: Categories preserved as reported
- **WHEN** a CLI reports input, cached-input, output, and reasoning-output counts
- **THEN** the usage record stores each category separately with its reported value

#### Scenario: Unreported category is absent, not zero
- **WHEN** a CLI's output provides no count for a token category (for example, no cache-write count)
- **THEN** the usage record marks that category as absent rather than recording `0`

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

Every usage record SHALL retain provenance alongside the token counts: the CLI (adapter) that produced it, the actual model reported by the CLI where available, the measurement source (e.g. which output event supplied the data), and a completeness indicator distinguishing full, partial, and unavailable measurements.

#### Scenario: Provenance recorded with usage
- **WHEN** a usage record is collected from a completed agent step
- **THEN** the record includes the CLI name, the reported model (when the CLI provides one), the measurement source, and a completeness indicator

#### Scenario: Partial measurement flagged
- **WHEN** a CLI reports only a subset of the token categories its adapter expects it to provide
- **THEN** the record's completeness indicator reflects a partial measurement

### Requirement: Non-agent step usage

Non-agent steps (shell, UI, and other step types that invoke no agent CLI) SHALL report zero token usage while retaining their measured duration. Zero here is a true measurement — no tokens were consumed — and is distinct from the unavailable state.

#### Scenario: Shell step reports zero usage
- **WHEN** a shell step completes
- **THEN** its metrics carry zero token usage and the step's duration in milliseconds

### Requirement: Per-step attribution for cumulative usage sources

When a CLI reports cumulative session totals rather than per-invocation usage, the usage recorded for a step that resumes an existing session SHALL reflect only that step's consumption, not the session's lifetime total.

<!-- deferred-to-design: mechanism for per-step attribution — delta against the previously
     recorded cumulative total for the session vs. recording the raw cumulative value with a
     source marker. Depends on where per-session usage state is kept across steps.
     (Known cumulative-reporting CLI at research time: Codex.) -->

#### Scenario: Resumed session step records its own usage
- **WHEN** an agent step resumes a session whose earlier step already consumed tokens, and the CLI reports cumulative session totals
- **THEN** the resumed step's usage record reflects only the tokens consumed by that step's invocation
