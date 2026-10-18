## Context

Agent usage flows through one path today:

1. `internal/exec/invocation.go` launches the CLI, then calls `extractAgentUsage` (`internal/exec/agent.go`) with the captured stdout and a `cli.UsageContext` (workdir, effective env, interactive plan).
2. `extractAgentUsage` dispatches to the adapter: `ContextualUsageExtractor.ExtractUsageWithContext` when implemented (Claude), otherwise `UsageExtractor.ExtractUsage`; autonomous-interactive Claude goes to `InteractiveUsageCollector.ExtractInteractiveUsage`.
3. The resulting `model.UsageRecord` rides on `AgentInvocationResult.Usage`, is persisted in the step-end audit event, and the metrics collector (`internal/metrics/collector.go`) clones it onto `StepRecord.Usage`, applies cumulative-delta attribution (`attribute`, `attributeTurns`), and recomputes aggregates in `refreshAggregatesLocked`.

Facts established for this design (checked against real CLI output on 2026-10-18):

- **Claude headless stream-json**: each main-thread `assistant` event carries `message.id` and `message.usage` with `input_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`, `output_tokens`; subagent events carry a non-empty `parent_tool_use_id`. The same message can repeat across events. The `result` event's `modelUsage.<model>.contextWindow` reports the window (for example `1000000` for `claude-opus-5-5`). Stream output only contains the current invocation's messages, even when resuming. Auto-compaction emits a `system` `compact_boundary` event; the summarization request is not emitted as a main-thread `assistant` event.
- **Claude transcripts** (autonomous-interactive): `claudeInteractiveSpan` in `internal/cli/claude_interactive_usage.go` already isolates the invocation's main-thread assistant messages (sidechain and `<synthetic>` excluded), deduplicated by message ID with final usage, in transcript order, and records damage.
- **Claude statusline**: `recordClaudeStatusline` (`cmd/agent-runner/statusline_cmd.go`) stores `ClaudeStatusReport{SessionID, PromptID, TotalCostUSD, CurrentUsage}`; the incoming payload also carries `context_window.context_window_size`, which is currently dropped.
- **Codex** (`exec --json`): stdout gives `thread.started.thread_id` (the session ID) and session-cumulative `turn.completed.usage` only. The session rollout `<codex-home>/sessions/YYYY/MM/DD/rollout-<ts>-<session-id>.jsonl` (Runner's private `CODEX_HOME` symlinks `sessions` to the user's Codex home) contains, from Codex 0.160:
  - one `token_usage_record` line per model response: `{thread_id, turn_id, response_id, usage{input_tokens, cached_input_tokens, cache_write_input_tokens, output_tokens, ...}, turn_token_usage, thread_token_usage}`, with a top-level `timestamp`;
  - a `compacted` line after each compaction whose payload names `compaction_response_id`; the compaction response also has its own `token_usage_record` (written immediately before the `compacted` line);
  - `event_msg` `token_count` events with `last_token_usage` and `model_context_window`, including synthetic post-compaction updates with all input categories zero and only `total_tokens` set.
  - Codex `input_tokens` already includes `cached_input_tokens` (OpenAI semantics; the existing adapter documents this).
  - Older rollouts have `token_count` events but no `token_usage_record` lines.
- **Cursor** (`result.usage` once per run), **Copilot** (per-message `outputTokens` only): no per-request input usage. **OpenCode** emits per-step `step_finish.tokens`, but its run JSON does not identify compaction steps.
- **External-user** steps collect one `UsageRecord` per headless turn (`externalTurnUsage`, `internal/exec/external_user.go`) and the collector attributes `Usage.Turns`.
- Additive JSON fields are backward compatible: `run-metrics.json` loading uses `encoding/json`, which ignores unknown fields, and the `Versioned schema` requirement bumps the version only for backward-incompatible changes.

## Goals / Non-Goals

**Goals:**
- Attach a context measurement to every launched agent attempt, computed from the CLI's own per-request usage, without changing usage, cost, or outcomes.
- Support Claude headless, Claude autonomous-interactive, Codex headless (rollout with per-response records), and external-user steps over those CLIs.
- Publish a run-level agent-session context summary keyed by CLI plus agent session ID.

**Non-Goals:**
- Context for Cursor, Copilot, OpenCode, autonomous-interactive Codex, and Agent Validator nested records.
- Adding context to the v4 native measurement heads (`internal/measurements` contract, shared with Agent Validator).
- Any TUI display.

## Approach

### Data model (`internal/model/usage.go`)

```go
type ContextStatus string // "available" | "partial" | "unavailable"

type ContextMeasurement struct {
    Status              ContextStatus     `json:"status"`
    Reason              UnavailableReason `json:"reason,omitempty"`
    Source              string            `json:"source"`
    LastRequestInput    *int64            `json:"last_request_input_tokens"`
    PeakContext         *int64            `json:"peak_context_tokens"`
    ContextWindowLimit  *int64            `json:"context_window_limit_tokens"`
}

// on UsageRecord:
Context *ContextMeasurement `json:"context,omitempty"`
```

`UsageRecord` is the carrier because it already travels from adapter to audit event to `StepRecord.Usage` intact. Shell and other non-agent records keep `Context == nil` (omitted). A helper builds measurements from an ordered list of observations:

```go
type contextObservation struct{ tokens int64; unclassified bool }
func buildContext(source string, obs []contextObservation, window *int64, finalMissing bool) *model.ContextMeasurement
```

Adapters drop positively excluded updates (subagent, compaction, synthetic, pre-invocation) before calling the builder; they never reach it. Each remaining observation is either eligible (`unclassified == false`, `tokens` set) or unclassified (`unclassified == true`, `tokens` ignored). `finalMissing` is set when the source has explicit evidence that the invocation's final request could not be read (for example a damaged trailing transcript entry).

Rules (one implementation, used by every adapter):
- peak = max `tokens` over eligible observations, or null when there are none;
- last = `tokens` of the final observation when it is eligible and `finalMissing` is false; null when the final observation is unclassified or `finalMissing` is true. An unclassified observation earlier in the list does not affect last;
- `available` when at least one observation is eligible, none is unclassified, and `finalMissing` is false; `partial` when at least one is eligible but some observation is unclassified or `finalMissing` is true; `unavailable` when none is eligible;
- values are never zero-filled.

So an ordinary request followed only by an identified compaction response or synthetic update is `available` with that ordinary request as last, as the spec's synthetic-exclusion scenarios require; only an unclassified trailing update or explicit missing-final evidence nulls the last value.

External-user steps need a different reduction, because their inputs are turn measurements that each carry a separate peak and last. `mergeContext(turns []*model.ContextMeasurement) *model.ContextMeasurement` (see External-user steps) is defined separately and does not go through `buildContext`.

New reasons in `model.UnavailableReason`: `unsupported` (`UnavailableContextUnsupported`, the literal the spec uses), `context-unclassified-update`, `codex-rollout-not-found`, `codex-rollout-unsupported`, `context-not-recorded`. Existing reasons (`parse-failure`, `no-usage-event`, `transcript-*`) are reused where the cause is the same.

### Claude headless (`internal/cli/claude.go`)

Extend the existing single scan in `ExtractUsage`. For each `assistant` event with empty `parent_tool_use_id` and model not `<synthetic>`: record `message.id → usage` in first-seen order, overwriting usage on repeats. Context size = `input_tokens + cache_read_input_tokens + cache_creation_input_tokens`; a message whose usage lacks any of the three is unclassified. Read `result.modelUsage[<observed main model>].contextWindow` as the window. `ExtractUsageWithContext` inherits it unchanged. Source `claude:stream-json`.

### Claude autonomous-interactive (`internal/cli/claude_interactive_usage.go`, `cmd/agent-runner/statusline_cmd.go`)

Build observations from `claudeInteractiveSpan.messages` in transcript order (already deduplicated, main thread only, synthetic excluded). Damaged or unreadable entries in the span mark the final observation missing, yielding `partial` with the readable peak, matching the spec. When the span cannot be established, the measurement copies the usage record's unavailable reason. Add `ContextWindowSize *int64 json:"context_window_size,omitempty"` to `ClaudeStatusReport`, populated from `context_window.context_window_size`; the window limit is the latest report for the step's session recorded at or after the span start. Source `claude:session-transcript`. Human-interactive steps get `unavailable` with the usage record's reason.

### Codex headless (`internal/cli/codex_context.go`, new)

`CodexAdapter` implements `ContextualUsageExtractor`: call `ExtractUsage`, then attach `codexContext(stdout, uc)`.

1. **Session**: `thread.started.thread_id` from stdout; none → `unavailable` / `no-usage-event`.
2. **Locate rollout**: Codex home = `CODEX_HOME` from `uc.Env` if set (private home's `sessions` symlink resolves to the shared directory), else `~/.codex`. Walk `sessions/` date directories newest first and pick the file whose name ends in `-<thread_id>.jsonl`. Not found or unreadable → `unavailable` / `codex-rollout-not-found`.
3. **Invocation span**: add `StartedAt time.Time` to `cli.UsageContext`, set in `invocation.go` immediately before launch. Lines with `timestamp` earlier than `StartedAt` are excluded. Codex writes the rollout on the same host clock; runs in a session are sequential, so a timestamp boundary is sufficient. (Claude interactive uses byte offsets because its transcript is located before launch; Codex fresh sessions have no file before launch, so offsets do not apply.)
4. **Classify**: collect `token_usage_record` lines in the span with `thread_id == session` (other threads are subagents) in file order, and the set of `compacted.compaction_response_id` values in the span. A record whose `response_id` is in that set is excluded. A record that is the last line of the file is unclassified (a compaction response is followed by its `compacted` line, so a trailing record could be one interrupted mid-compaction). All others are eligible with context size `usage.input_tokens`. `token_count` events never contribute values; if the span has `token_count` events with non-zero usage but no `token_usage_record` lines, the rollout predates per-response records → `unavailable` / `codex-rollout-unsupported`.
5. **Window**: `model_context_window` from the last `token_count` event in the span with non-null `info`.

Source `codex:rollout`. The rollout read happens after process exit, in the existing post-exit extraction call; it adds no wait.

### Other adapters

Cursor, Copilot, and OpenCode return their existing extraction plus `Context{Status: unavailable, Reason: unsupported}`. `extractAgentUsage` sets the same for adapters without `UsageExtractor` and for autonomous-interactive steps on CLIs without an interactive collector (for example Codex); its parse-failure fallback sets `unavailable` / `parse-failure`. Human-interactive steps set `unavailable` with the usage record's reason. A context extraction error inside an adapter never returns an error from extraction; it degrades the context measurement only, leaving usage untouched.

### External-user steps (`internal/exec/external_user.go`)

Each turn's `UsageRecord` already carries its own `Context`. `externalTurnUsage.Add` computes the step context with `mergeContext` over all turns' measurements, independently of the token-counter merge in `mergeExternalUsage` (which copies or sums counters and must not touch `Context`):
- peak = max of every non-null turn peak, including peaks from `partial` turns;
- last = the final turn's last value (null when the final turn's last is null);
- status = `available` when every turn is `available`; `unavailable` when no turn has a non-null peak or last; `partial` otherwise; reason = the final turn's reason when it is not `available`, else the first non-`available` turn's reason;
- window = the last non-null turn window.

Example: a `partial` first turn with peak 180,000 and null last, then an `available` turn with peak 30,000 and last 25,000, yields peak 180,000, last 25,000, status `partial`. An unavailable final turn after a known peak yields that peak, null last, status `partial`. The merged `Context` is set on the step record's usage; `attributeTurns` in the collector carries it through unchanged and ignores per-turn `Context` when attributing counters.

### Metrics collector (`internal/metrics/collector.go`)

- `cloneUsage` deep-copies `Context`. `attribute` and `attributeTurns` pass it through unchanged: it is never baselined, differenced, or summed. `totalsLocked`, `totalsForRecords`, and `SessionRollup` ignore it.
- New artifact field, recomputed in `refreshAggregatesLocked`:

```go
type AgentSessionContextSummary struct {
    Sessions             []AgentSessionContext `json:"sessions"`
    UnattributedAttempts int                   `json:"unattributed_attempts"`
}
type AgentSessionContext struct {
    CLI                     string `json:"cli"`
    SessionID               string `json:"session_id"`
    HighWaterMark           *int64 `json:"high_water_mark_tokens"`
    MaxLastRequestInput     *int64 `json:"max_last_request_input_tokens"`
    ContextWindowLimit      *int64 `json:"context_window_limit_tokens"`
    AttemptCount            int    `json:"attempt_count"`
    Coverage                string `json:"coverage"` // complete | partial | none
}
// Artifact.AgentSessionContext AgentSessionContextSummary `json:"agent_session_context"`
```

- Eligible records: agent-step and agent-call records with `AgentInvoked`; nested-agent records, iterations, and non-agent steps are skipped. Key = `(Usage.CLI or Tool, StepRecord.SessionID)`; empty session ID increments `UnattributedAttempts`. A record with `Usage.Context == nil` (written before this change) counts as `unavailable` / `context-not-recorded` for coverage. Entries are ordered by first appearance in `Steps` for stable output.

### Data flow

```
CLI stdout / transcript / rollout
        │  adapter extraction (post-exit)
        ▼
UsageRecord{..., Context}
        │  step-end audit event
        ▼
StepRecord.Usage.Context ──► agent_session_context (recomputed each write)
        ╳  totals, rollups, cumulative attribution, native measurements
```

## Decisions

- **Carrier is `UsageRecord.Context`**, not a new `StepRecord` field: it reuses the audit-event path, external-user turns, and resume replay with no new plumbing. Alternative (separate audit data key) duplicates persistence logic.
- **Codex source is `token_usage_record`, not `token_count`**: per-response records carry `response_id`, which joins to `compacted.compaction_response_id`, and synthetic post-compaction updates exist only as `token_count` events. This is what makes Codex eligibility demonstrable. Older Codex versions get an explicit unsupported reason instead of a heuristic.
- **Codex context size = `input_tokens`**: Codex input already includes cached and cache-write input; adding them would double count. This equals the spec's uncached + cache read + cache creation sum.
- **Codex invocation span by timestamp**: rollout files do not exist before a fresh launch and stdout carries no turn ID, so a pre-launch byte offset is unavailable; Runner's sequential session use makes `timestamp >= StartedAt` exact in practice.
- **OpenCode unsupported**: it reports per-step usage, but `opencode run` output cannot distinguish compaction steps, so every update would be unclassified; reporting `unsupported` is clearer than a permanently `partial` value. Follow-up once OpenCode marks compaction in its output.
- **No schema version bump**: the fields are additive and optional; older readers ignore them; this artifact's versioning rule bumps only for incompatible changes. A newer artifact rewritten by an older binary loses context fields, which then read as `context-not-recorded` — an acceptable, explicit degradation for a pre-release tool.
- **Not in native measurement heads**: the v4 measurement contract is shared with Agent Validator and versioned separately; context is a Runner-side gauge and stays in the compatibility step view and summary.

## Risks / Trade-offs

- **Codex rollout format drift**: `token_usage_record` and `compacted` are undocumented rollout internals. Mitigation: strict parsing that degrades to `codex-rollout-unsupported` or `context-unclassified-update`, never to a guessed value; fixtures copied from real 0.160+ rollouts (with the compaction sequence) in `internal/cli/testdata/usage/`.
- **Claude compaction assumption**: the design relies on Claude not emitting the summarization request as a main-thread `assistant` event. If it ever does, its usage would look ordinary. Mitigation: a fixture containing `compact_boundary` pins the expected stream shape, so a format change surfaces as a test failure when fixtures are refreshed.
- **Rollout search cost**: walking `~/.codex/sessions` (low thousands of files) once per Codex step after exit is negligible; newest-first ordering stops early for fresh sessions.
- **Clock skew between Runner and Codex**: same host clock; a session is never used by two concurrent invocations.

## Testing

- `internal/cli`: table tests for `buildContext` status rules, including ordinary requests followed by identified (dropped) synthetic or compaction updates (`available`, last = ordinary request) versus an unclassified trailing update (`partial`, null last); Claude stream fixtures (single message, repeated message, subagent event, synthetic, missing cache field, `modelUsage.contextWindow`, `compact_boundary`); transcript span tests reusing interactive fixtures (inherited session, damaged trailing entry, statusline window); Codex rollout fixtures under a temp `CODEX_HOME` (fresh session, resumed session with earlier lines before `StartedAt`, compaction record excluded, synthetic `token_count` ignored, trailing record unclassified, pre-0.160 rollout, missing rollout, subagent thread).
- `internal/exec`: extraction fallbacks set context reasons; `mergeContext` cases for peak differing from last, a `partial` turn holding the largest peak followed by a valid final turn, and an unavailable final turn after a known peak.
- `internal/metrics`: context passes through attribution untouched; totals unchanged with and without context; summary grouping (fresh sessions, inherited across resume, same ID on two CLIs, unattributed, legacy record without context, coverage values).

## Migration Plan

No migration step. Existing artifacts load as-is; their agent records have no `context` and are reported as `context-not-recorded` in the summary. Rollback is a code revert; older binaries ignore the new fields.

## Open Questions

None.
