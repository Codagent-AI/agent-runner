## Why

Agent Runner records `duration_ms` on `step_end` and `run_end` events but has no visibility into how many tokens a run consumed or what it cost. Users can't see what a workflow spends, and Agent Evals has no stable metrics artifact to consume, so cost and usage are currently invisible to both humans and downstream tooling.

## What Changes

- Extract per-step token usage — and, where the CLI reports it, cost — from each agent CLI's structured stdout on autonomous-headless steps, for all five supported CLIs: Claude, Codex, OpenCode, Copilot, and Cursor (best-effort; its usage output is undocumented). PTY-backed contexts (interactive and autonomous-interactive) capture no stdout, so their collection is a later phase.
- Distinguish token categories where the CLI provides them: input, cached input, cache writes, output, and reasoning output. Do not collapse to a single total-tokens number.
- Capture cost only as reported by the CLI itself, in USD (`estimated_api_cost_usd`). Agent Runner computes no prices: there is no pricing catalog, no price fetching, and no token-times-rate math. Claude and OpenCode report USD cost; Codex and Cursor report none; Copilot reports only GitHub AI Credits, which are not converted. The number is the CLI's own API-price estimate, not the user's actual bill.
- Add usage and cost fields to agent `step_end` events, and aggregated totals to `run_end` events.
- Write a versioned, machine-readable `run-metrics.json` artifact in each run directory as the supported boundary for Agent Evals and other consumers. It is updated incrementally as steps complete and accumulates across resume sessions.
- Represent unavailable usage or cost as explicit `null`/unavailable states, never as zero tokens or zero cost. Run-level cost totals sum only the steps that reported USD cost and carry an explicit coverage indicator (complete/partial/none). Non-agent steps (shell, etc.) report zero token usage while retaining their duration.
- Surface per-step and per-run usage, cost, and timing in the run views (live and inspect).
- Add a "run complete" screen shown when a workflow run finishes: wall-clock time and reported API cost for each step, with nested steps (loops, groups, sub-workflows) rolling up into their parent and top-level totals for the run.

## Capabilities

### New Capabilities

- `agent-usage-collection`: How Agent Runner extracts structured token usage from an agent CLI's stdout after a headless step completes, which token categories it captures per CLI, which CLIs/contexts are covered, and how it represents unavailable usage. Covers the adapter-level extraction seam and carrying structured usage through the agent process result.
- `cost-capture`: Recording the CLI's own reported cost verbatim (USD only), null-when-absent semantics, run-level cost aggregation with the coverage indicator, and the separation of raw usage from the cost record (provider, actual model, token categories, measurement source, completeness).
- `run-metrics-artifact`: The versioned `run-metrics.json` schema and file, its location in the run directory, incremental per-step writes, cumulative aggregation across resume sessions, and its guarantees as the stable consumer boundary.
- `run-complete-screen`: The end-of-run summary screen: per-step wall-clock time and reported API cost, nested-step rollup into parent steps (loops, groups, sub-workflows), and run-level totals.

### Modified Capabilities

- `audit-log-entries`: Agent `step_end` entries gain token-usage and cost fields; `run_end` entries gain aggregated usage and cost totals. (Timing/`duration_ms` already specified.)
- `cli-adapter`: The adapter contract gains an optional usage-extraction capability so each adapter can surface structured usage (and CLI-reported cost) from its CLI's output.
- `view-run`: Per-step and per-run usage, cost, and timing appear in the inspect run view.
- `live-run-view`: Per-step and per-run usage, cost, and timing appear in the live run view.

## Technical Approach

Agent Runner owns measurement end to end: it collects usage, CLI-reported cost, and durations at the adapter boundary, aggregates them by step and run, and writes a stable artifact. It performs no valuation of its own — cost exists only where the CLI reported it. No OTLP receiver or OTEL collection is involved; every source is the CLI's structured stdout, which research confirmed carries equal or better data than the CLIs' OTEL exports for headless runs, with none of the flush-reliability risk.

**Extraction seam (adapter level).** The `cli.Adapter` interface already uses optional capability interfaces (`OutputFilter`, `StdoutWrapper`, `HeadlessResultFilter`) that adapters implement when their CLI emits structured JSONL. Usage extraction follows the same pattern: a new optional interface (e.g. `UsageExtractor`) each adapter implements to parse token counts and any reported cost from its CLI's output after the process exits. Per-CLI sources:

- **Claude**: headless invocation gains `--output-format stream-json --verbose`; the final `result` JSONL event carries `usage` (input, output, cache-creation, cache-read) and `total_cost_usd` / per-model `costUSD` (Claude Code's own client-side estimate), while intermediate events preserve live output.
- **Codex**: the `turn.completed` JSONL event's `usage` object (input, cached input, output, reasoning) — already parsed and currently discarded. No cost. On multi-turn threads the value is cumulative, which extraction must account for.
- **OpenCode**: `--format json` (already passed) emits `step_finish` events with `tokens` (input, output, reasoning, cache read/write) and `cost` in USD (models.dev rates). A known race can drop the final stdout event; extraction treats a missing event as unavailable.
- **Copilot**: `--output-format json` JSONL carries per-model token fields (`inputTokens`, `outputTokens`, `cachedInputTokens`, `cacheWriteTokens`, `reasoningTokens`). Its `cost` field is GitHub AI Credits, not USD, so cost is recorded as unavailable.
- **Cursor**: `--output-format stream-json` (already passed) may include a `usage` object (`inputTokens`, `outputTokens`) in recent versions; extraction is best-effort and reports unavailable when the object is absent. No cost.

PTY-backed runs (interactive and autonomous-interactive contexts) capture no stdout, which is why autonomous-headless collection comes first.

```
agent step runs CLI (headless)
   → adapter extracts raw usage + any CLI-reported cost
     (categories, provider, actual model, source, completeness)
      → structured usage carried through the agent process result
         ├─ step_end audit event  (usage + estimated_api_cost_usd)
         ├─ run_end audit event   (aggregated totals + cost coverage)
         └─ run-metrics.json      (per-step + per-run, versioned schema,
                                   updated incrementally per step)
               ↑
         run view (live + inspect) reads/renders the same metrics
```

**Run-complete screen.** When a run finishes, the TUI presents a summary screen built from the same aggregated metrics: each step's wall-clock time and reported cost, with nested structures (loop iterations, groups, sub-workflow steps) rolled up into their parent rows and totals at the top level. It renders from the same step-node tree the live and inspect views use, so no separate metrics pipeline is needed.

**Persistence.** `run-metrics.json` lives alongside `audit.log` in the existing run directory (`~/.agent-runner/projects/{encoded-path}/runs/{run-id}/`). It is rewritten atomically as each step completes and finalized at run end, so interrupted runs still leave metrics for completed steps; resumed runs accumulate into the same file so totals describe the whole run. It is the declared boundary for consumers; Agent Evals reads this artifact rather than reconstructing metrics from audit internals or CLI transcripts.

**Raw usage vs. cost record.** Each usage record keeps its raw measurement (provider, actual model, per-category token counts, measurement source, completeness) separate from the captured cost (`estimated_api_cost_usd`, present only when the CLI reported USD). Missing data is explicit at each layer — `null`, never zero. Run-level cost totals carry a coverage indicator (complete/partial/none) reflecting how many contributing agent steps reported cost.

**Key decisions:** (1) Runner owns measurement, not consumers. (2) `run-metrics.json` is the supported boundary. (3) The monetary field is `estimated_api_cost_usd`, captured verbatim from the CLI (API-price estimate, USD only). (4) Agent Runner does no pricing: no embedded catalog, no overrides, no live fetching. (5) Missing usage/cost is `null`/unavailable, never zero; partial run cost is flagged, not silently summed. (6) All five CLIs get extraction in this change (Cursor best-effort); PTY-backed collection is a deferred phase. (7) Collection is structured-stdout only; no OTLP receiver.

**Schema-first sequencing.** The versioned usage and `run-metrics.json` schemas are defined before adapter changes, since audit events, the artifact, and the UI all depend on them.

## Out of Scope

- Usage collection for PTY-backed steps (interactive and autonomous-interactive contexts) — deferred second phase; requires adapter-specific post-run transcript/session-file extraction or OTEL.
- Any OTLP receiver or OTEL-based collection.
- Computing cost from token counts: no pricing catalog, no price overrides, no models.dev or API price lookups, no credits-to-USD conversion for Copilot.
- Eval-side aggregation, reporting, or dashboards built on `run-metrics.json` — that is Agent Evals' concern.
- Reporting the user's actual billed cost or reconciling against subscription billing.

## Impact

- **Code:** `internal/cli/adapter.go` (new optional usage interface), `internal/cli/claude.go` (add `--output-format stream-json --verbose` + extraction), `internal/cli/codex.go`, `internal/cli/opencode.go`, `internal/cli/copilot.go`, `internal/cli/cursor.go` (extraction), `internal/exec/agent.go` (carry usage through the result), `internal/audit/types.go` (new fields), `internal/runner/runner.go` (aggregation + incremental artifact write), `internal/runview/` (metrics in run views plus the new run-complete screen). A `run-metrics.json` writer following the `stateio` atomic pattern.
- **Specs:** New `agent-usage-collection`, `cost-capture`, `run-metrics-artifact`, `run-complete-screen`; deltas to `audit-log-entries`, `cli-adapter`, `view-run`, `live-run-view`. `audit-log-storage` needs no delta — it governs only the audit log file, and the `run-metrics-artifact` spec owns the new artifact's location.
- **Consumers:** Agent Evals gains a documented `run-metrics.json` contract; its version must be stable across releases.
- **Behavioral side effect:** Claude headless steps switch from plain-text to `stream-json` JSONL output, so the adapter must now stream assistant text live and extract the plain-text response from the final `result` event for capture variables and display (as Codex already does with its JSONL).
- **Dependencies:** No new runtime dependencies; no network access during runs.
