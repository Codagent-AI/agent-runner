## ADDED Requirements

### Requirement: Transcript context for autonomous-interactive Claude steps

For an autonomous-interactive Claude step, Agent Runner SHALL compute the context measurement defined by `agent-context-size` from the same main-thread assistant messages it attributes to the invocation for usage: those written after the recorded transcript position, each counted once with its last recorded usage. Messages the CLI marks as synthetic SHALL NOT be eligible. The last observed request input SHALL be the context size of the last such message in transcript order. The measurement source SHALL identify the session transcript.

#### Scenario: Interactive step context from transcript
- **WHEN** an autonomous-interactive Claude step's invocation writes main-thread assistant messages with context sizes 30,000, 70,000, and 65,000 tokens
- **THEN** the step's context measurement is `available` with last observed request input 65,000 and peak context 70,000, sourced from the session transcript

#### Scenario: Inherited session counts only this invocation
- **WHEN** step A writes messages up to 120,000 context tokens in session S, and autonomous-interactive step B inherits S and writes messages peaking at 140,000
- **THEN** step B's peak context is 140,000 and its last observed request input comes from B's own last message, and A's messages are not part of B's measurement

#### Scenario: Synthetic message ignored
- **WHEN** the invocation's last transcript entry is a synthetic assistant message after an ordinary message with 90,000 context tokens
- **THEN** the last observed request input is 90,000

#### Scenario: Streamed duplicates use final usage
- **WHEN** the same assistant message appears three times with growing usage
- **THEN** it is one request whose context size uses its last recorded usage

### Requirement: Context window limit from the statusline report

When Claude's statusline report for the step's session reports the context window size during the invocation, Agent Runner SHALL record it as the step's context window limit. When no such report exists, the limit SHALL be null and the context measurement's status SHALL be unaffected.

#### Scenario: Statusline reports window size
- **WHEN** the statusline report received during an autonomous-interactive Claude step reports a context window size of 1,000,000 tokens
- **THEN** the step's context window limit is 1,000,000

#### Scenario: No statusline report
- **WHEN** no statusline report was recorded for the step's session
- **THEN** the context window limit is null and the measurement is still computed from the transcript

### Requirement: Unusable transcript evidence limits context

When the step's transcript evidence is missing, ambiguous, or damaged, the context measurement SHALL follow the same evidence the usage collection could attribute: `unavailable` when no assistant message can be attributed, and `partial` when some attributed messages could be read but entries were damaged or unreadable. Human-interactive Claude steps SHALL NOT carry a collected context measurement.

#### Scenario: Transcript missing
- **WHEN** the step's session transcript cannot be found
- **THEN** the context measurement is `unavailable` with a reason matching the usage record's

#### Scenario: Damaged entry after readable messages
- **WHEN** the invocation's portion of the transcript contains readable assistant messages followed by an unparseable entry
- **THEN** the context measurement is `partial`, keeps the peak from the readable messages, and has a null last observed request input

#### Scenario: Human-interactive step
- **WHEN** a human-interactive Claude step completes
- **THEN** its context measurement is `unavailable` with the same reason as its usage record
