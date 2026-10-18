# agent-context-size Specification

## Purpose
TBD - created by archiving change feature-280-26fd2f2d. Update Purpose after archive.
## Requirements
### Requirement: Context size of a model request

Agent Runner SHALL define the context size of one model request as the sum of the request's uncached input tokens, cache-read input tokens, and cache-creation input tokens, as reported by the CLI for that request. Output tokens SHALL NOT be included. When the CLI reports some but not all of these input categories for a request, the context size SHALL be computed only if the missing categories are known to be absent for that CLI; otherwise the observation SHALL be treated as not eligible.

#### Scenario: All input categories reported
- **WHEN** a request reports 1,200 uncached input tokens, 80,000 cache-read tokens, 3,000 cache-creation tokens, and 900 output tokens
- **THEN** its context size is 84,200 tokens

#### Scenario: CLI without cache categories
- **WHEN** a CLI whose usage format has no cache categories reports 40,000 input tokens and 500 output tokens for a request
- **THEN** its context size is 40,000 tokens

### Requirement: Per-attempt context measurement

For every agent execution attempt whose CLI Agent Runner launched (agent steps and called agents), Agent Runner SHALL record a context measurement for the attempt's main session containing:

- **last observed request input**: the context size of the last eligible request observed in the attempt;
- **peak context**: the largest context size among the attempt's eligible requests;
- **context window limit**: the model's context window in tokens, only when the CLI reported it for the attempt;
- a **status** of `available`, `partial`, or `unavailable`;
- a **reason** whenever the status is not `available`;
- the measurement **source** that supplied the observations.

The status SHALL be `available` when every request the source reports for the attempt's main session is either eligible or positively identified as excluded, and at least one eligible request was observed. It SHALL be `partial` when at least one eligible request was observed but the source also contained updates that could not be classified, or when the final observation of the attempt could not be measured. It SHALL be `unavailable` when no eligible request was observed. A known peak SHALL be retained when the last observed request input is unavailable, and vice versa. Missing values SHALL be explicit nulls, never zeros.

#### Scenario: Several requests in one attempt
- **WHEN** a headless agent attempt makes three main-session requests with context sizes 20,000, 45,000, and 38,000 tokens
- **THEN** its context measurement is `available` with last observed request input 38,000 and peak context 45,000

#### Scenario: Window limit reported
- **WHEN** the CLI reports a 200,000-token context window for the attempt's model
- **THEN** the measurement's context window limit is 200,000

#### Scenario: Window limit not reported
- **WHEN** the CLI reports per-request usage but no context window
- **THEN** the measurement's context window limit is null and its status is unaffected

#### Scenario: Unclassifiable trailing update
- **WHEN** an attempt has eligible requests with peak 150,000 tokens, and its final usage update cannot be told apart from a compaction or synthetic update
- **THEN** the measurement is `partial` with peak context 150,000, a null last observed request input, and a reason naming the unclassifiable update

#### Scenario: No eligible request
- **WHEN** an attempt's CLI output contains no per-request usage
- **THEN** the measurement is `unavailable` with a reason, and both values are null

### Requirement: Eligible observations

Only usage the CLI reported for an ordinary request of the attempt's main conversation, made during the current invocation, SHALL count as an eligible observation. The following SHALL NOT be eligible: requests made by subagents or other child sessions; context-compaction or summarization requests; synthetic or placeholder usage updates that do not correspond to a model request (for example a value filled to the window size on context overflow, or a CLI-generated synthetic message); and usage recorded before the current invocation began. When a source reports an update that cannot be classified as eligible or excluded, Agent Runner SHALL NOT use its value and SHALL mark the measurement `partial` or `unavailable` with a limitation reason rather than report a possibly false value.

#### Scenario: Subagent requests excluded
- **WHEN** a Claude attempt's main thread peaks at 60,000 context tokens and a subagent it spawned makes a request with 150,000 context tokens
- **THEN** the attempt's peak context is 60,000

#### Scenario: Compaction request excluded
- **WHEN** an attempt's main session grows to 180,000 context tokens, the CLI runs a compaction request, and the next ordinary request has 30,000 context tokens
- **THEN** the peak context is 180,000, the last observed request input is 30,000, and the compaction request's usage contributes to neither value

#### Scenario: Synthetic overflow value excluded
- **WHEN** the CLI records a synthetic usage update filled to the context window after a context-overflow failure, and that update is identifiable as synthetic
- **THEN** the update contributes to neither value, and the last observed request input is the last ordinary request before it

#### Scenario: Prior session usage excluded
- **WHEN** an attempt resumes an agent session whose earlier requests were made by a previous step
- **THEN** the attempt's measurement reflects only requests made during this attempt's invocation

### Requirement: Context size is a gauge

Context measurements SHALL be point-in-time values, not counters. Agent Runner SHALL NOT sum them into run totals, per-category token totals, or execution-session rollups, and SHALL NOT apply cumulative-counter delta attribution to them. Each attempt SHALL report the values observed during that attempt, so successive attempts that share an agent session show the session's size step by step.

#### Scenario: Resumed session reports its own sizes
- **WHEN** step A runs in agent session S and peaks at 50,000 context tokens, and step B later resumes S and peaks at 90,000
- **THEN** step A's peak context is 50,000, step B's peak context is 90,000, and neither value is a difference between the two

#### Scenario: Run totals unchanged
- **WHEN** a run's agent attempts record context measurements
- **THEN** the run's token totals and cost are identical to what they would be without context measurement

### Requirement: Context sources by CLI

Agent Runner SHALL obtain context observations from the CLI's own per-request usage reports and SHALL NOT estimate context size by tokenizing prompts, transcripts, or output.

- **Claude headless**: main-thread assistant messages' per-request usage in the captured stream output; the context window limit from the run result when Claude reports it.
- **Claude autonomous-interactive**: as defined by `interactive-claude-usage`.
- **Codex headless**: the per-response usage records in the session rollout of the attempt's session, written during the current invocation by the main thread, with compaction responses (those the rollout identifies as compaction results) excluded; the window limit from the rollout's reported model context window. Codex's aggregate token-count updates, which include synthetic post-compaction values, and its session-cumulative turn-completion usage SHALL NOT be used as context observations. A rollout without per-response usage records (older Codex versions) yields `unavailable`.
- **External-user steps**: combined from the step's per-turn measurements: peak is the largest turn peak, last observed request input is the last turn's value, and any turn that is not `available` makes the step `partial` or `unavailable`.
- **Cursor, Copilot, OpenCode, and autonomous-interactive Codex**: `unavailable` with reason `unsupported`. (Cursor and Copilot report no per-request input usage; OpenCode's run output does not identify compaction steps.)

#### Scenario: Claude headless attempt
- **WHEN** a headless Claude attempt's stream output contains main-thread assistant messages with usage
- **THEN** its context measurement is computed from those messages and its source identifies the Claude stream output

#### Scenario: Repeated stream entries for one message
- **WHEN** the Claude stream output repeats the same assistant message's usage across several entries
- **THEN** that message counts as one request whose context size is taken from its last reported usage

#### Scenario: Codex rollout available
- **WHEN** a headless Codex attempt's session rollout contains per-response usage records written during this invocation by the main thread
- **THEN** the context measurement is computed from those records and carries the reported model context window

#### Scenario: Codex compaction response excluded
- **WHEN** a Codex attempt's rollout records a compaction response with 215,000 input tokens followed by a synthetic token-count update, and the next ordinary response has 18,000 input tokens
- **THEN** neither the compaction response nor the synthetic update contributes, and the last observed request input is 18,000

#### Scenario: Codex resumed session
- **WHEN** a headless Codex attempt resumes a session whose rollout already holds records from an earlier step
- **THEN** only records written after this attempt's launch contribute

#### Scenario: Older Codex rollout
- **WHEN** a Codex attempt's rollout has token-count updates but no per-response usage records
- **THEN** the context measurement is `unavailable` with a reason saying the rollout format is unsupported

#### Scenario: Codex rollout not locatable
- **WHEN** a Codex attempt's session rollout cannot be located
- **THEN** the context measurement is `unavailable` with a reason saying the rollout was not found, and the attempt's token usage is unaffected

#### Scenario: CLI without per-request usage
- **WHEN** an attempt runs on Cursor, Copilot, or OpenCode
- **THEN** the context measurement is `unavailable` with reason `unsupported`, and the attempt's token usage is unaffected

#### Scenario: External-user step over several turns
- **WHEN** an external-user step runs two headless Claude turns with peaks 40,000 and 75,000 and last observed request inputs 39,000 and 72,000
- **THEN** the step's context measurement is `available` with peak context 75,000 and last observed request input 72,000

#### Scenario: No estimation
- **WHEN** a CLI reports only session-cumulative usage
- **THEN** no context size is derived from that usage or from the prompt text, and the measurement is `unavailable`

### Requirement: Context measurement does not affect execution or usage

A failure to collect or parse context observations SHALL NOT change the attempt's outcome, delay its completion, or alter its token usage or cost record. Context measurement SHALL be attempted regardless of the attempt's exit code, and observations from a failed attempt SHALL be retained.

#### Scenario: Context parse failure
- **WHEN** an attempt's token usage is extracted successfully but its context observations cannot be parsed
- **THEN** the attempt's usage and outcome are unchanged and its context measurement is `unavailable` with a parse-failure reason

#### Scenario: Failed attempt retains context
- **WHEN** an agent attempt exits nonzero after making main-session requests with usage
- **THEN** its context measurement contains those requests' values

