## MODIFIED Requirements

### Requirement: Unavailable usage is explicit

When usage cannot be collected for an agent step, Agent Runner SHALL record an explicit unavailable state with the reason. Missing usage SHALL never be represented as zero tokens. Situations that produce an unavailable record include: interactive invocation contexts where the agent CLI owns the terminal and no transcript collector applies, reported as `interactive-context`; structured-output parse failures, missing usage events in otherwise valid output, and adapters that do not support extraction.

The `interactive-context` reason SHALL apply to human-interactive agent steps on every CLI, and to autonomous-interactive agent steps whose CLI has no session-transcript usage collector. Autonomous-interactive Claude steps are not an `interactive-context` case: their usage SHALL be collected from the Claude session transcript as specified by `interactive-claude-usage`. When that collection fails, the record SHALL be unavailable or partial with a transcript-specific reason, not `interactive-context`.

An interactive step running in external-user mode is not an interactive invocation context for usage purposes, because no CLI owns a terminal. Agent Runner SHALL collect each of its turns' usage as it does for autonomous-headless invocations, and SHALL report the step attempt's usage as the aggregate of its turns, under the existing per-turn versus cumulative semantics. A turn whose usage cannot be collected SHALL make the aggregate partial, with the reason. It SHALL never be counted as zero.

#### Scenario: Interactive agent step reports unavailable
- **WHEN** an agent step runs in a human-interactive context (the CLI owns the terminal, so no stdout is captured)
- **THEN** the step's usage record is an explicit unavailable state with reason `interactive-context`

#### Scenario: Autonomous-interactive step without a transcript collector reports unavailable
- **WHEN** an agent step on a CLI other than Claude runs in an autonomous-interactive context
- **THEN** the step's usage record is an explicit unavailable state with reason `interactive-context`

#### Scenario: Autonomous-interactive Claude step reports transcript usage
- **WHEN** a Claude agent step runs in an autonomous-interactive context and its session transcript records assistant messages with usage
- **THEN** the step's usage record is available with the transcript usage, and its reason is not `interactive-context`

#### Scenario: Human-interactive Claude step is not collected
- **WHEN** a Claude agent step runs in a human-interactive context
- **THEN** no session transcript is read for usage and the step's usage record is unavailable with reason `interactive-context`

#### Scenario: External-user interactive step reports turn usage
- **WHEN** an interactive step runs in external-user mode for three turns, and each turn reports usage
- **THEN** the step attempt's usage record is available and aggregates all three turns, and its reason is not `interactive-context`

#### Scenario: External-user turn without usage
- **WHEN** one of several external-user turns ends without the event that carries usage data
- **THEN** the step attempt's usage is partial, keeps the other turns' known totals, and records why the missing turn is unavailable

#### Scenario: Parse failure reports unavailable
- **WHEN** an autonomous-headless agent step completes but its stdout cannot be parsed as the expected structured format
- **THEN** the step's usage record is an explicit unavailable state, the step's outcome is otherwise unaffected, and no zero counts are recorded

#### Scenario: Missing usage event reports unavailable
- **WHEN** an autonomous-headless agent step's structured output is otherwise valid but ends without the event that carries usage data
- **THEN** the step's usage record is an explicit unavailable state
