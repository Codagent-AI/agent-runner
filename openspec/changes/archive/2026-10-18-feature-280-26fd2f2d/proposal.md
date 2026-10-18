## Why

`run-metrics.json` records each agent step's token totals (input, cached input, cache writes, output) and cost, but not how large the agent session's context was. Token totals are the sum over every model request in a step, so they mix two different things: how many requests the agent made and how large each request was. They cannot answer "did this session's context grow large?"

That question matters now because workflow designs are being compared on cost and quality. In the and-scene eval, the audited workflow's single implementor build session read 7 to 11M cached tokens, against about 5M across the default workflow's per-task sessions. Without context size per step, nobody can tell whether the long session costs more because its context grew (each turn re-reads a large prefix) or because it simply did more turns, nor whether quality drops as context nears the model's window. Paul asked directly: "are / can we measure context window size of each agent after each step?" Today the answer is no, though the data already reaches Runner: Claude's stream-json and transcripts carry per-request usage, Runner's Claude statusline report already receives `context_window.current_usage`, and Codex session rollouts record per-turn token counts.

Verdict: **go**. The data is already in sources Runner parses, the change is additive to the metrics artifact, and it directly answers a live eval question. Caveat: coverage is per CLI; CLIs without per-request usage report context size as unavailable, never zero or estimated.

## What Changes

- Each agent step attempt record in `run-metrics.json` gains a **context size** measurement for the agent's main session:
  - **last observed request input**: the context tokens of the last eligible main-conversation model request observed in the step (uncached input + cache read + cache creation). This is the last measured prompt size, not a claim about the exact context a resumed step inherits, since final output and later compaction change that state;
  - **peak context**: the largest such value across the step's eligible requests (they differ when the CLI compacts context mid-step). A known peak is kept even when the final observation is unavailable;
  - **context window limit**, when the CLI reports it (for example Codex's `model_context_window`, or Claude's statusline `context_window_size`), so size can be read as a fraction of the window.
- Context size is measured for the step's main session only. Subagent sessions have their own contexts and are not added to the parent's figure.
- Context size is a point-in-time gauge, not a counter: it is not summed into run totals, and per-step attribution of cumulative counters (resumed sessions) does not apply to it. Each step reports the size observed in that step, so a resumed session shows its growth step by step.
- Only eligible observations count: usage the CLI reported for a main-conversation model request within the current invocation. Compaction requests, synthetic counter updates (for example a value filled to the window size on overflow), and updates that cannot be told apart from those are not treated as request measurements; when a source cannot make that distinction, the measurement records an explicit limitation rather than a possibly false value.
- A run-level **agent-session context summary** groups steps by agent CLI session (CLI plus agent `session_id`) across the whole run, not by Runner invocation: ten fresh agent sessions in one invocation yield ten entries, and one inherited session spanning a Runner resume yields one. Each entry's high-water mark is the maximum of its steps' peak context; the maximum last observed request input is kept as a separately named statistic. When any of a session's step observations are unavailable or limited, the summary marks its coverage as partial instead of presenting the known maximum as complete. Steps without an agent session identity stay explicitly unattributed and are never merged. Existing execution-session rollups keep their current semantics.
- When context size cannot be observed (unsupported CLI, no per-request usage, parse failure, shell steps), it is recorded as unavailable with a reason, following the existing unavailable-is-explicit rule.
- Coverage by CLI:
  - **Claude headless**: from main-thread `assistant` events' `message.usage` in the captured stream-json.
  - **Claude autonomous-interactive**: from the transcript usage the interactive collector already reads, with the statusline report as a source of the window limit.
  - **Codex**: from the session rollout's `token_count` events (`last_token_usage`, `model_context_window`) when the rollout file can be located for the step's session and its updates can be attributed to the current invocation's main-conversation requests; otherwise unavailable or limited. Codex also records compaction-response usage and synthetic fill-to-window values through the same `token_count` path, so Codex coverage is conditional on design demonstrating that those updates can be distinguished. Codex `exec --json` `turn.completed` usage is session-cumulative and cannot provide context size.
  - **Cursor, Copilot, OpenCode, external-user**: unavailable unless their existing output already carries per-request usage; the design step confirms each.

## Capabilities

### New Capabilities
- `agent-context-size`: what "context size" means for an agent step, which sources supply it per CLI, main-thread versus subagent scope, eligible observations (main-conversation requests, excluding compaction and synthetic updates), gauge semantics (last observed request input and peak, not summed), and explicit unavailability.

### Modified Capabilities
- `run-metrics-artifact`: step attempt records carry the context size measurement, and a new agent-session context summary keyed by CLI and agent session ID reports each session's high-water mark and coverage; execution-session rollups and run totals do not aggregate it.
- `agent-usage-collection`: adapters extract context size alongside token usage for headless and autonomous-interactive steps.
- `interactive-claude-usage`: the transcript and statusline collectors supply context size and window limit for autonomous-interactive Claude steps.

## Technical Approach

The measurement rides the existing usage-extraction path. `UsageRecord` (`internal/model/usage.go`) gains an optional context measurement (last observed request input, peak, optional window limit, status, reason or limitation, source). Each adapter's `ExtractUsage` already scans every stream event; the Claude adapter (`internal/cli/claude.go`) additionally tracks main-thread (`parent_tool_use_id` empty) assistant `message.usage` while scanning, and the interactive collector (`internal/cli/claude_interactive_usage.go`) does the same over transcript `assistant` entries it already parses. Codex needs a new read of the session rollout's `token_count` events, located by the session ID Runner already resolves. That carries two risks the design must settle before Codex coverage is promised: how the rollout is found for headless runs, and how compaction-response usage and synthetic fill-to-window updates (Codex `compact.rs` `run_compaction_stream` and `TokenUsageInfo::fill_to_context_window`) are told apart from ordinary request usage. Where either fails, the measurement is unavailable or limited.

The metrics collector (`internal/metrics/collector.go`) copies the measurement onto the step record unchanged, deliberately bypassing cumulative-delta attribution and run-total aggregation, and builds the agent-session context summary by grouping step records on CLI plus agent `session_id` across the run; `SessionRollup`, which groups by Runner invocation, is left unchanged. The new fields are optional and additive, so older artifacts load unchanged; whether the additive fields warrant a `schema_version` bump follows the artifact's existing versioning rules and is a design decision.

Defined context size as uncached input + cache read + cache creation of one request because that is the full prompt the model processed on that request, matching what Claude's statusline reports as current context usage. Output tokens are excluded; they enter the next request's input.

## Out of Scope

- Changing workflow behavior based on context size (for example forcing a new session or compacting when context is large).
- Displaying context size in the TUI run view, run list, or run complete screen; this change records it in `run-metrics.json` only.
- Per-turn context history within a step; only last observed request input and peak are recorded.
- Estimating context size by tokenizing prompts or transcripts for CLIs that do not report per-request usage.
- Subagent context sizes and nested-tool (Agent Validator) context sizes.
- Backfilling context size into existing historical run artifacts.

## Impact

- **Code**: `internal/model/usage.go`, `internal/cli/claude.go`, `internal/cli/claude_interactive_usage.go`, `internal/cli/codex.go` (rollout read), possibly the other adapters; `internal/metrics/collector.go` for step records and the agent-session context summary; adapter and collector tests.
- **Artifact**: `run-metrics.json` step records gain optional fields and the artifact gains an agent-session context summary. Additive; existing consumers that ignore unknown fields are unaffected.
- **Specs**: new `agent-context-size`; deltas to `run-metrics-artifact`, `agent-usage-collection`, `interactive-claude-usage`.
- **Users**: eval and workflow authors can compare context growth across workflow designs from the metrics artifact.
- **Dependencies**: none new.
