# claude-subagent-usage Specification

## Purpose
TBD - created by archiving change feature-188-e9df53ca. Update Purpose after archive.
## Requirements
### Requirement: Attribution to the spawning invocation

Agent Runner SHALL attribute a subagent's usage only to the step whose invocation spawned it. The set of subagents spawned by an invocation SHALL be established from the Task tool-use IDs that appear in either of two sources: the invocation's captured stdout, when there is one, or the parent session transcript within the portion written by that invocation. Each subagent transcript SHALL be matched to a spawn through the spawning tool-use ID that Claude Code records for it. Subagents spawned by a subagent (nested at any depth) SHALL be discovered from the spawning subagent's transcript and attributed to the same step. A subagent SHALL be counted at most once per run. Subagent transcripts in the same session that belong to other invocations SHALL NOT be attributed to the step.

For an autonomous-headless invocation, the invocation's portion of the parent session transcript SHALL be bounded by the first and last transcript entries whose identifiers also appear in the invocation's captured stdout. An autonomous-interactive invocation has no captured stdout. Its portion SHALL be the entries written after the transcript position recorded before launch, as specified by `interactive-claude-usage`, and its spawns SHALL come only from that portion and from nested subagent transcripts.

#### Scenario: Earlier spawn in the inherited session is outside the span
- **WHEN** a parent session transcript contains a subagent spawn written before the first entry that the current step's stdout also reports
- **THEN** that subagent is not attributed to the current step

#### Scenario: Inherited session does not double count
- **WHEN** step A spawns two subagents in session S, and step B later inherits session S and spawns one more subagent
- **THEN** step A's measurement includes only its two subagents, step B's measurement includes only its one subagent, and run-level totals count each of the three subagents once

#### Scenario: Spawn missing from stdout is still collected
- **WHEN** a Task tool use appears in the parent session transcript within the step's invocation but not in the step's captured stdout, and the matching subagent transcript is readable
- **THEN** that subagent's usage is included in the step's measurement

#### Scenario: Nested subagent attributed to the originating step
- **WHEN** a subagent spawned by a step itself spawns a second-level subagent
- **THEN** the second-level subagent's usage is included in the same step's measurement

#### Scenario: Invocation boundary cannot be established
- **WHEN** Agent Runner cannot determine which portion of the parent session transcript belongs to the step's invocation
- **THEN** usage from any subagents matched through stdout is retained, and the step's subagent collection and usage completeness are `partial` with a reason. An absence of spawns in stdout is not treated as proof of zero subagents.

#### Scenario: Autonomous-interactive spawns come from the positional span
- **WHEN** an autonomous-interactive Claude step resumes session S, which already contains one subagent spawn from an earlier step, and the step's invocation spawns two more
- **THEN** only the two subagents spawned after the step's recorded transcript position are attributed to the step

#### Scenario: Autonomous-interactive background subagent proven finished
- **WHEN** an autonomous-interactive Claude step spawns a background subagent, and the parent transcript within the step's portion records the subagent's completion for its spawning tool-use ID
- **THEN** the subagent's allocation is complete

#### Scenario: Autonomous-interactive subagent without finish evidence
- **WHEN** an autonomous-interactive Claude step's subagent transcript holds valid usage, but neither its parent transcript nor its spawning subagent's transcript records that it finished
- **THEN** the subagent's known usage is retained, its allocation and the step's usage completeness are `partial` with a reason that it may still be running, and the step does not wait for it

#### Scenario: Autonomous-interactive nested subagent without finish evidence
- **WHEN** an autonomous-interactive Claude step's subagent finished, but a subagent it spawned has no finish evidence in the spawning subagent's transcript
- **THEN** the nested subagent's allocation and the step's usage completeness are `partial`, and run-level and session-level usage coverage are `partial`

#### Scenario: Autonomous-interactive span cannot be established
- **WHEN** an autonomous-interactive Claude step's portion of the parent session transcript cannot be established
- **THEN** no subagents are attributed to the step, and its subagent collection and usage completeness are `partial` or unavailable with a reason, never complete

### Requirement: Streamed transcript entries counted once

A transcript can record the same assistant message several times as it streams. Agent Runner SHALL count each assistant message's usage exactly once per subagent, using the message's final recorded usage.

#### Scenario: Repeated message entries
- **WHEN** a subagent transcript contains three entries for the same assistant message ID with output token counts 8, 120, and 640
- **THEN** that message contributes 640 output tokens to the subagent's usage

### Requirement: Model identity is scoped to its source

The main-thread observed model SHALL be taken only from parent-scoped telemetry. For autonomous-headless steps, that is stdout events without a parent tool-use ID, and the `result` event. For autonomous-interactive steps, it is the main-thread assistant messages in the invocation's portion of the parent session transcript. Each subagent's observed model SHALL be taken only from that subagent's own transcript. Subagent events that appear in stdout SHALL NOT change the main-thread model identity. When one subagent's transcript records several models, each SHALL remain a distinct observed identity.

#### Scenario: Trailing subagent event does not relabel main-thread tokens
- **WHEN** a headless Claude step's main thread runs on `claude-opus-5-5`, and a subagent's `claude-haiku-4-5` assistant event appears in stdout after the last main-thread assistant event
- **THEN** the main-thread allocation's observed model is `claude-opus-5-5` and the subagent allocation's observed model is `claude-haiku-4-5`

#### Scenario: Same model on both threads remains separate
- **WHEN** a step's main thread and its subagents all run on `claude-opus-5-5`
- **THEN** the measurement still contains distinct main-thread and subagent allocations, each with observed model `claude-opus-5-5`

#### Scenario: Interactive main-thread model comes from the parent transcript
- **WHEN** an autonomous-interactive Claude step's main thread writes assistant messages on `claude-opus-5-5` and its subagent's transcript records `claude-haiku-4-5`
- **THEN** the main-thread allocation's observed model is `claude-opus-5-5` and the subagent allocation's observed model is `claude-haiku-4-5`

### Requirement: Missing or incomplete subagent evidence is explicit

When a spawned subagent's transcript is missing, unreadable, unparseable, or records no usage, or when the step's portion of the parent session transcript contains an entry that cannot be parsed or recognized and could hide a spawn, Agent Runner SHALL retain all usage it could collect, mark that subagent's allocation unavailable with a reason, and mark the step's usage completeness `partial`. A subagent that is still running when the parent CLI exits SHALL be recorded from whatever its transcript holds at collection time, and SHALL be marked partial. Agent Runner SHALL NOT wait for it. Subagent collection failures SHALL NOT change the step's outcome, output, or main-thread usage, and SHALL never be represented as zero tokens.

Agent Runner SHALL locate transcripts under the Claude configuration directory named by `CLAUDE_CONFIG_DIR` in the step's effective environment when it is set, and under `~/.claude` otherwise. It SHALL find the session's transcript even when Claude Code shortens the project directory name for a long working directory. A spawn whose tool result is an error and for which no transcript exists SHALL be treated as never started. It SHALL NOT make collection partial.

#### Scenario: Overridden Claude config directory
- **WHEN** a headless Claude step runs with `CLAUDE_CONFIG_DIR` set to a custom directory that holds the session's transcripts
- **THEN** subagent usage is collected from that directory

#### Scenario: Failed spawn without a transcript
- **WHEN** a step's Task tool use returns an error result and no subagent transcript exists for it
- **THEN** no allocation is recorded for that spawn and subagent collection completeness is unaffected

#### Scenario: Malformed parent transcript entry inside the invocation
- **WHEN** a step's portion of the parent session transcript is established, but a line within it cannot be parsed or contains a tool use in an unrecognized shape
- **THEN** usage found on valid lines is retained, and the step's subagent collection and usage completeness are `partial` with a reason identifying the invalid parent transcript

#### Scenario: Malformed parent transcript line outside the invocation
- **WHEN** a parent session transcript contains an unparseable line outside the step's portion
- **THEN** the line does not affect the step's subagent collection completeness

#### Scenario: Missing subagent transcript
- **WHEN** a step's invocation spawned two subagents, one transcript is readable, and the other does not exist
- **THEN** the step's measurement contains the readable subagent's usage and an unavailable allocation for the missing one with a reason, the step's usage completeness is `partial`, and the step's outcome is unchanged

#### Scenario: Background subagent outlives the parent
- **WHEN** a subagent spawned in the background is still running when the Claude CLI process exits
- **THEN** Agent Runner records the usage present in its transcript at collection time, marks that allocation and the step's usage as `partial`, and does not delay the step's completion

#### Scenario: Unparseable transcript line
- **WHEN** a subagent transcript contains a line that is not valid JSON
- **THEN** that subagent's allocation is `partial` with a reason, known usage from valid lines is retained, and the step does not fail

### Requirement: Subagent usage collection for Claude steps

After an autonomous-headless or autonomous-interactive Claude agent step's CLI process exits, Agent Runner SHALL collect token usage and observed model identity for every subagent that the step's invocation spawned. It SHALL read them from the subagent transcripts that Claude Code writes for the step's session. Collection SHALL be attempted regardless of the step's exit code. Collection SHALL NOT be attempted for human-interactive Claude steps, whose usage remains unavailable with reason `interactive-context`, or for agent steps that use other CLIs.

#### Scenario: Subagent tokens and model appear in the step measurement
- **WHEN** a headless Claude step spawns one subagent through the Task tool, and that subagent's transcript records assistant messages on model `claude-haiku-4-5` with input, cache-read, cache-write, and output usage
- **THEN** the step's measurement contains a subagent allocation with the summed token categories from that transcript and observed model `claude-haiku-4-5`

#### Scenario: Parallel subagents are all collected
- **WHEN** a headless Claude step spawns four subagents in parallel and all four transcripts are readable
- **THEN** the step's measurement includes usage from all four subagents and its usage completeness is `complete`

#### Scenario: Failed step still collects subagent usage
- **WHEN** a headless Claude step spawns a subagent and then exits with a nonzero code
- **THEN** the subagent's usage is collected and included in the step's measurement and in run-level aggregates

#### Scenario: Interactive Claude step is not scanned
- **WHEN** a Claude agent step runs in a human-interactive context
- **THEN** no subagent transcripts are read and the step's usage remains unavailable with reason `interactive-context`

#### Scenario: Autonomous-interactive Claude step collects subagents
- **WHEN** an autonomous-interactive Claude step's lead spawns three subagents through the Task tool, and all three transcripts are readable
- **THEN** the step's measurement contains a main-thread allocation from the session transcript and subagent allocations for all three, attempt totals count each token once, and its usage completeness is `complete`

#### Scenario: Step without subagents is unchanged
- **WHEN** a headless Claude step completes, and its invocation's span in the parent session transcript is established and contains no Task tool uses
- **THEN** the step's measurement contains no subagent allocations, its tokens equal the main-thread usage, and its completeness reflects main-thread collection only

