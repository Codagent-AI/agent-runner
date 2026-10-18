- [ ] Implement the change described by these files using TDD, as `CLAUDE.md` requires: write each failing test before the code that makes it pass. Satisfy every spec scenario in `specs/` and every `INT-*` / `E2E-*` obligation in `test-plan.md`. `design.md` is the technical source of truth; the bullets below summarize it.
  - [proposal.md](proposal.md), [design.md](design.md), [test-plan.md](test-plan.md), [decisions.md](decisions.md)
  - [specs/agent-context-size/spec.md](specs/agent-context-size/spec.md), [specs/agent-usage-collection/spec.md](specs/agent-usage-collection/spec.md), [specs/interactive-claude-usage/spec.md](specs/interactive-claude-usage/spec.md), [specs/run-metrics-artifact/spec.md](specs/run-metrics-artifact/spec.md)

  **Model** (`internal/model/usage.go`)
  - Add `ContextStatus` (`available` | `partial` | `unavailable`) and `ContextMeasurement` with `status`, `reason`, `source`, `last_request_input_tokens`, `peak_context_tokens`, `context_window_limit_tokens` (pointer values; nulls, never zeros).
  - Add `UsageRecord.Context *ContextMeasurement` (`json:"context,omitempty"`).
  - Add reasons `unsupported`, `context-unclassified-update`, `codex-rollout-not-found`, `codex-rollout-unsupported`, `context-not-recorded`; reuse existing reasons where the cause is the same.

  **Shared builders** (`internal/cli`, `internal/exec`)
  - `buildContext(source, obs, window, finalMissing)`: adapters drop positively excluded updates (subagent, compaction, synthetic, pre-invocation) before calling it; observations are eligible or unclassified. Peak = max eligible; last = final observation's tokens only when it is eligible and `finalMissing` is false; status per `design.md` Data model.
  - `mergeContext(turns)` for external-user steps, separate from `buildContext` and from the counter merge in `mergeExternalUsage`: max non-null turn peak (including partial turns), final turn's last, status/reason/window per `design.md` External-user steps. Set it in `externalTurnUsage.Add`.

  **Claude headless** (`internal/cli/claude.go`)
  - In the existing `ExtractUsage` scan, collect main-thread (`parent_tool_use_id` empty), non-`<synthetic>` `assistant` messages by `message.id` in first-seen order, keeping the last usage per ID. Context size = `input_tokens + cache_read_input_tokens + cache_creation_input_tokens`; a missing category makes the message unclassified.
  - Window = `result.modelUsage[<observed main model>].contextWindow`. Source `claude:stream-json`. Add fixtures for repeated message, subagent event, synthetic, missing cache field, `modelUsage.contextWindow`, and `compact_boundary`.

  **Claude autonomous-interactive** (`internal/cli/claude_interactive_usage.go`, `cmd/agent-runner/statusline_cmd.go`)
  - Build observations from `claudeInteractiveSpan.messages` in transcript order; damaged or unreadable span entries set `finalMissing`; an unestablished span copies the usage record's unavailable reason. Source `claude:session-transcript`.
  - Add `ClaudeStatusReport.ContextWindowSize` from `context_window.context_window_size`; window = latest report for the step's session at or after the span start.

  **Codex headless** (`internal/cli/codex_context.go`, new; `internal/cli/codex.go`)
  - Implement `ContextualUsageExtractor` on `CodexAdapter`: `ExtractUsage` unchanged, then attach context.
  - Session from `thread.started.thread_id`. Codex home = `CODEX_HOME` from `UsageContext.Env`, else `~/.codex`; find `sessions/**/rollout-*-<thread_id>.jsonl`, newest date directories first.
  - Add `UsageContext.StartedAt`, set in `internal/exec/invocation.go` just before launch; ignore rollout lines with earlier `timestamp`.
  - Use `token_usage_record` lines with `thread_id == session`; exclude records whose `response_id` matches a `compacted.compaction_response_id`; a record that is the file's last line is unclassified; context size = `usage.input_tokens`. Never take values from `token_count`; window = last non-null `token_count.info.model_context_window`. No `token_usage_record` but non-zero `token_count` → `codex-rollout-unsupported`; no file → `codex-rollout-not-found`. Source `codex:rollout`.
  - Context errors never fail extraction or alter the usage record.

  **Other paths** (`internal/cli/cursor.go`, `copilot.go`, `opencode.go`, `internal/exec/agent.go`)
  - Cursor, Copilot, OpenCode, adapters without `UsageExtractor`, and autonomous-interactive steps on CLIs without an interactive collector: `unavailable` / `unsupported`. Parse-failure fallback: `unavailable` / `parse-failure`. Human-interactive: `unavailable` with the usage record's reason. Non-agent steps carry no `Context`.

  **Metrics collector** (`internal/metrics/collector.go`)
  - `cloneUsage` deep-copies `Context`; `attribute` and `attributeTurns` pass it through unchanged; totals, coverage indicators, and `SessionRollup` ignore it.
  - Add `Artifact.AgentSessionContext` (`agent_session_context`: `sessions[]`, `unattributed_attempts`) recomputed in `refreshAggregatesLocked`. Include agent-step and agent-call records with `AgentInvoked`; key by CLI plus `StepRecord.SessionID`; empty session ID increments `unattributed_attempts`; `Context == nil` counts as `context-not-recorded`. Entry fields and coverage rules per `design.md` and the `run-metrics-artifact` spec; order by first appearance.
  - Keep `SchemaVersion` unchanged.

  **Tests from `test-plan.md`**
  - INT-001 (`internal/cli`): Codex rollout through the private Codex home from `prepareCodexRunnerHome`, with fixtures derived from a real Codex 0.160+ rollout.
  - INT-002 (`internal/metrics`): resume over an artifact whose agent records lack `context`.
  - E2E-001 (`cmd/agent-runner`): built binary, fake `claude` and `codex` on `PATH`, workflow of Claude new, Claude resume, Codex, shell.
  - E2E-002 (`cmd/agent-runner`): extend `TestInteractiveClaudeUsageE2E001` with statusline window size, inherited session, and damaged-transcript case.
  - E2E-003: add context assertions to `TestClaudeHeadlessRealAgentE2E` and `TestCodexHeadlessRealAgentE2E`, launching them with `isolatedRealAgentCallEnv`-style isolation (temp `HOME`, temp `CODEX_HOME` with copied `auth.json`); skip with reason when a CLI cannot authenticate under isolation. Do not add CI jobs.

  **Done when**
  - `make fmt`, `make lint`, and `make test` pass.
  - `openspec validate feature-280-26fd2f2d --strict` passes.
