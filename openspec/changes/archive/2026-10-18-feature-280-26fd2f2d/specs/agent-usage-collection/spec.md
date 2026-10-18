## ADDED Requirements

### Requirement: Context observations collected with usage

When Agent Runner extracts token usage from an autonomous-headless agent step's captured CLI output, it SHALL also extract the step's context measurement, as defined by `agent-context-size`, from the same output and in the same collection pass. Context extraction SHALL be attempted whenever usage extraction is attempted, including for failed steps. The context measurement SHALL be independent of the usage record's status: available usage MAY accompany unavailable context, and available context MAY accompany partial usage.

#### Scenario: Usage and context from one output
- **WHEN** a headless Claude step completes and its stream output contains main-thread assistant messages with usage and a result event with usage
- **THEN** the step's usage record is taken from the result event as today, and its context measurement is taken from the assistant messages

#### Scenario: Usage available, context unavailable
- **WHEN** a headless step's adapter reports session-cumulative usage but no per-request usage
- **THEN** the step's usage record is available and its context measurement is `unavailable`

#### Scenario: Adapter without usage extraction
- **WHEN** a headless step's adapter does not support usage extraction
- **THEN** both its usage record and its context measurement are unavailable

### Requirement: Non-agent steps carry no context measurement

Shell steps and other non-agent steps, iteration records, and agent steps that never invoked their CLI SHALL NOT carry a context measurement and SHALL NOT count toward any context coverage.

#### Scenario: Shell step
- **WHEN** a shell step completes
- **THEN** its record has no context measurement, and it does not appear in the agent-session context summary

#### Scenario: Agent step skipped before launch
- **WHEN** an agent step is skipped by `skip_if` before its CLI is launched
- **THEN** its record has no context measurement and it does not reduce any context coverage
