# interactive-claude-usage Specification

## Purpose
TBD - created by archiving change feature-233-312c4a4a. Update Purpose after archive.
## Requirements
### Requirement: Transcript usage for autonomous-interactive Claude steps

After an autonomous-interactive Claude agent step's CLI process exits, Agent Runner SHALL collect the step's main-thread token usage and observed model identity from the Claude session transcript of the session the step ran in. The usage SHALL come from the assistant messages that the step's invocation wrote to that transcript. Collection SHALL be attempted regardless of the step's exit code or completion outcome, including when Agent Runner terminated the CLI after a completion request. The resulting usage record SHALL identify the session transcript as its measurement source. It SHALL be distinct from the headless `result`-event source. Its reason SHALL NOT be `interactive-context`.

Transcripts SHALL be located under the same rules that apply to subagent transcripts: the Claude configuration directory named by `CLAUDE_CONFIG_DIR` in the step's effective environment when it is set, `~/.claude` otherwise, including project directory names that Claude Code shortens.

#### Scenario: Fresh session reports main-thread usage
- **WHEN** an autonomous-interactive Claude step starts a new session, and the agent writes three assistant messages on model `claude-opus-5-5` whose usage reports input, cache-read, cache-write, and output tokens
- **THEN** the step's usage record is available, its token categories equal the sum of those three messages' usage, its observed model is `claude-opus-5-5`, and its measurement source is the session transcript

#### Scenario: Terminated after completion still collects
- **WHEN** an autonomous-interactive Claude step requests completion and Agent Runner terminates the CLI after the turn is durable
- **THEN** the step's usage record contains the usage of every assistant message the invocation wrote

#### Scenario: Step fails but usage is retained
- **WHEN** an autonomous-interactive Claude step's CLI exits with a nonzero code after writing assistant messages with usage
- **THEN** the step's usage record contains those messages' usage, and the usage is included in run-level aggregates

#### Scenario: Several models in one invocation
- **WHEN** the main thread of one autonomous-interactive Claude invocation writes assistant messages on two different models
- **THEN** both models are recorded as distinct observed identities for the main thread, and no tokens are counted twice

#### Scenario: Overridden Claude config directory
- **WHEN** an autonomous-interactive Claude step runs with `CLAUDE_CONFIG_DIR` set to a custom directory that holds the session transcript
- **THEN** usage is collected from that directory

### Requirement: Each assistant message counted once with its final usage

Claude Code can record the same assistant message several times as it streams. Agent Runner SHALL count each assistant message's usage exactly once within the invocation, using that message's last recorded usage in the transcript. Transcript usage SHALL be treated as per-message usage, not as a cumulative session counter: it SHALL be attributed directly without a prior baseline, including when the step resumes a session whose earlier usage was never recorded in this run.

#### Scenario: Streamed message entries
- **WHEN** the invocation's portion of the transcript contains three entries for the same assistant message ID with output token counts 8, 120, and 640
- **THEN** that message contributes 640 output tokens to the step's usage

#### Scenario: Resumed session without a prior record
- **WHEN** an autonomous-interactive Claude step resumes a session created outside this run, and the invocation writes assistant messages with usage
- **THEN** the step's usage record is available and contains only those messages' usage, not the session's earlier usage

### Requirement: Invocation span by transcript position

Agent Runner SHALL attribute to an autonomous-interactive Claude step only the transcript entries that its invocation wrote. Before handing the terminal to the CLI, it SHALL record the position of the session transcript's end. If the transcript does not exist yet, that position is the start of the file. The step's portion of the transcript SHALL be the entries written after that position, up to the end of the transcript when collection runs. Entries written before the recorded position SHALL NOT be attributed to the step, even when the session is shared with earlier steps through `inherit` or `resume`.

#### Scenario: Inherited session does not double count
- **WHEN** step A runs autonomous-interactive in session S and writes 4 assistant messages, and step B later inherits session S and writes 2 more
- **THEN** step A's usage contains only its 4 messages, step B's usage contains only its 2 messages, and run-level totals count all 6 messages once

#### Scenario: Headless and interactive steps share a session
- **WHEN** a headless Claude step and a later autonomous-interactive Claude step run in the same session
- **THEN** the autonomous-interactive step's usage excludes the messages the headless step wrote, and run-level totals count each message once

### Requirement: Unusable transcript evidence is explicit

When the step's transcript evidence is missing, ambiguous, or damaged, Agent Runner SHALL retain any usage it could attribute to the invocation and SHALL record why the rest is missing. Missing evidence SHALL never be represented as zero tokens. A transcript collection failure SHALL NOT change the step's outcome or delay its completion.

- When no usage can be attributed, the record SHALL be unavailable with a reason.
- When some usage is attributed but the evidence is incomplete, the record SHALL keep the known subtotal and be `partial` with a reason.
- When the invocation's span is established but contains no assistant messages, the record SHALL be unavailable with a reason. It SHALL NOT be zero, because a failed model call might not be recorded in the transcript.

#### Scenario: Transcript never written
- **WHEN** an autonomous-interactive Claude step's CLI exits before the session transcript exists
- **THEN** the step's usage record is unavailable with a reason and records no zero counts

#### Scenario: Span without assistant messages
- **WHEN** the invocation's portion of the transcript contains only the step's prompt and no assistant message
- **THEN** the step's usage record is unavailable with a reason and records no zero counts

#### Scenario: Transcript shrank or was replaced
- **WHEN** the session transcript is shorter at collection time than the position recorded before launch
- **THEN** the step's usage record is unavailable with a reason, because the invocation's portion cannot be established

#### Scenario: Ambiguous transcript location
- **WHEN** the session transcript exists in more than one Claude project directory
- **THEN** the step's usage record is unavailable with a reason identifying the ambiguity

#### Scenario: Malformed line inside the span
- **WHEN** the invocation's portion of the transcript contains valid assistant messages with usage and one line that is not valid JSON
- **THEN** the step's usage keeps the valid messages' usage and is `partial` with a reason identifying the invalid transcript line

#### Scenario: Assistant message without usage
- **WHEN** an assistant message in the invocation's portion of the transcript has no usage data
- **THEN** the step's usage keeps the other messages' usage and is `partial` with a reason

#### Scenario: Agent switched sessions during the step
- **WHEN** a cost report recorded during the step names a session other than the step's session, for example after the session was cleared
- **THEN** the step's usage keeps what was attributed from the original session and is `partial` with a reason, and it is never presented as complete

#### Scenario: Session switch without cost reports
- **WHEN** no cost reports were recorded during the step, so no session switch can be observed
- **THEN** the step's usage reflects only the original session's transcript

### Requirement: Claude-reported cost for autonomous-interactive Claude steps

Agent Runner SHALL record an autonomous-interactive Claude step's `estimated_api_cost_usd` only from a USD cost that Claude Code itself reports to its status line during the step's process. It SHALL record the value without price math. The step's cost SHALL be the final report's cost minus the cost in the process's first report. It SHALL be computed this way whether Claude's counter starts at zero or carries cost from before the process. It SHALL NOT depend on cost recorded for earlier steps on the same session.

The first report SHALL be used as the baseline only when there is positive evidence that Claude Code made it before processing the step's prompt. The absence of a matching last-response usage SHALL NOT count as such evidence. Without that evidence, the step's cost SHALL be null.

A report SHALL be accepted as final only when both of the following hold:

- It reflects the step's final main-thread assistant message, as shown by the report's last-response token usage equaling that message's usage.
- Every subagent attributed to the step, at any depth, has transcript evidence that it finished before that final message was requested. A subagent without such evidence, or one that recorded further usage after its last finish evidence, makes the cost null, because the report cannot be shown to include its spend.

All reports SHALL name the step's session. Otherwise the step's cost SHALL be null, never zero, and never computed from token counts. When cost is null, the reason SHALL be recorded in the step's audit event.

Waiting for a final cost report SHALL begin only after the step's final turn is durable and SHALL be bounded. When the bound expires, the step SHALL complete with a null cost. Its outcome and token usage SHALL NOT change.

Capturing cost reports SHALL NOT change the content of the user's status line:

- When the user has configured a status line, its output and display options SHALL still be used.
- When the user has not, no status-line content SHALL be added. Claude's footer keyboard hints may be hidden while the step runs.
- The user's status line SHALL be the one Claude Code itself would load for the step's working directory, including project-local settings that Claude reads from the repository root or a linked worktree's main checkout.
- When Agent Runner cannot read the user's Claude settings, or cannot determine with certainty which status line Claude would load, it SHALL NOT capture cost reports for that step, and the step's cost SHALL be null.

#### Scenario: Final cost report captured
- **WHEN** an autonomous-interactive Claude step starts a new session, the process's first report shows $0.00, and the report reflecting the step's final message shows $3.40
- **THEN** the step's `estimated_api_cost_usd` is 3.40, and the run-level cost total includes it once

#### Scenario: Resumed process counter starts at zero
- **WHEN** an earlier step in session S recorded a cost of $1.00, and a later autonomous-interactive step resumes S in a new process whose first report shows $0.00 and whose final report shows $1.50
- **THEN** the later step's `estimated_api_cost_usd` is 1.50

#### Scenario: Resumed process counter carries prior cost
- **WHEN** an autonomous-interactive step resumes session S in a new process whose first report, made before the step's first model response, shows $1.00 and whose final report shows $2.50
- **THEN** the step's `estimated_api_cost_usd` is 1.50

#### Scenario: First report already includes the step's work
- **WHEN** the first report recorded for the step's process already reflects one of the step's own model responses
- **THEN** the step's `estimated_api_cost_usd` is null, because no baseline predates the step

#### Scenario: Startup report lost and first saved report follows compaction
- **WHEN** the process's startup report was not recorded, and the first recorded report was made after a compaction, so its last-response usage is empty even though it includes the step's spend
- **THEN** the step's `estimated_api_cost_usd` is null, its token usage is still recorded, and cost coverage is not complete

#### Scenario: First saved report is mid-stream
- **WHEN** the first recorded report was made while one of the step's messages was streaming, so its last-response usage matches no final message usage
- **THEN** the step's `estimated_api_cost_usd` is null rather than a delta that omits earlier spend

#### Scenario: Background subagent still working at the final report
- **WHEN** the final report matches the step's final main-thread message, but a background subagent attributed to the step finishes, or records more usage, after that message
- **THEN** the step's `estimated_api_cost_usd` is null, its token usage keeps the subagent's recorded usage, and cost coverage is not complete

#### Scenario: Subagents finished before the final message
- **WHEN** every subagent the step spawned, including nested ones, has finish evidence written before the step's final main-thread message, and the final report matches that message
- **THEN** the step's `estimated_api_cost_usd` is the final report's value minus the baseline, and it includes the subagents' spend

#### Scenario: Cost includes subagent spend
- **WHEN** an autonomous-interactive Claude step's lead spawns subagents, and Claude's final report reflects their spend
- **THEN** the step's `estimated_api_cost_usd` includes that spend once, and subagent allocations' own cost remains unavailable

#### Scenario: Cost report never arrives
- **WHEN** an autonomous-interactive Claude step completes and no report reflecting its final message arrives within the bound
- **THEN** the step completes without further delay, its `estimated_api_cost_usd` is null, and its token usage is still recorded

#### Scenario: Stale cost report
- **WHEN** the latest report recorded during the step reflects a message earlier than the step's final main-thread message, for example because another turn ran after completion was requested
- **THEN** the step's `estimated_api_cost_usd` is null rather than the earlier subtotal

#### Scenario: Cost report from another session
- **WHEN** a report recorded during the step names a session other than the step's session
- **THEN** the step's `estimated_api_cost_usd` is null

#### Scenario: User's status line preserved
- **WHEN** the user has configured a Claude status line with display options and an autonomous-interactive Claude step runs
- **THEN** the user's status line output is displayed during the step with the same options as it would be without Agent Runner

#### Scenario: Project-local status line at the repository root
- **WHEN** an autonomous-interactive Claude step runs in a subdirectory of a git repository, or in a linked worktree, and the project-local settings at the repository root or the main checkout define a status line that differs from other settings layers
- **THEN** the status line displayed during the step is the one from those project-local settings, with its display options

#### Scenario: No user status line
- **WHEN** the user has not configured a Claude status line and an autonomous-interactive Claude step runs
- **THEN** no status-line text is shown during the step, and the step's cost can still be captured

#### Scenario: Unreadable Claude settings
- **WHEN** a Claude settings file that could define the user's status line exists but cannot be parsed
- **THEN** Agent Runner does not alter the status line, the step's `estimated_api_cost_usd` is null, and its token usage is still collected

#### Scenario: Cost channel overridden by managed settings
- **WHEN** a managed Claude setting prevents Agent Runner from receiving cost reports
- **THEN** the step's `estimated_api_cost_usd` is null and its token usage is still collected from the transcript

