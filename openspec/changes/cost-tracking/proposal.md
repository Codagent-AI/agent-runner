## Why

Agent Runner records `duration_ms` on `step_end` and `run_end` events but has no visibility into how many tokens a run consumed or what it cost. Users can't see what a workflow spends, and Agent Evals has no stable metrics artifact to consume, so cost and usage are currently invisible to both humans and downstream tooling.

## What Changes

- Extract per-step token usage from each agent CLI's structured output, starting with autonomous Claude and Codex (headless and autonomous-interactive collection first; interactive-session collection is a later phase).
- Distinguish token categories where the CLI provides them: input, cached input, cache writes, output, and reasoning output. Do not collapse to a single total-tokens number.
- Embed a release-versioned default pricing catalog and compute `estimated_api_cost_usd` per step and per run from category-level rates. The number is an API-price equivalent, not the user's actual bill, since Runner often uses subscription-authenticated CLIs.
- Add usage, cost, and existing timing fields to agent `step_end` events, and aggregated totals to `run_end` events.
- Write a versioned, machine-readable `run-metrics.json` artifact in each run directory as the supported boundary for Agent Evals and other consumers.
- Represent unavailable usage or pricing as explicit `null`/unavailable states, never as zero tokens or zero cost. Non-agent steps (shell, etc.) report zero token usage while retaining their duration.
- Surface per-step and per-run usage, cost, and timing in the run views (live and inspect).

## Capabilities

### New Capabilities

- `agent-usage-collection`: How Agent Runner extracts structured token usage from an agent CLI's output after a step completes, which token categories it captures, which CLIs/contexts are covered, and how it represents unavailable usage. Covers the adapter-level extraction seam and carrying structured usage through the agent process result.
- `cost-estimation`: The embedded release-versioned pricing catalog, how category-level token counts map to `estimated_api_cost_usd`, how unknown/aliased models and missing pricing are handled, and the separation of raw usage from valuation (provider, actual model, token categories, pricing snapshot/catalog version, measurement source, completeness).
- `run-metrics-artifact`: The versioned `run-metrics.json` schema and file, its location in the run directory, per-step and per-run aggregation, and its guarantees as the stable consumer boundary.

### Modified Capabilities

- `audit-log-entries`: Agent `step_end` entries gain token-usage and estimated-cost fields; `run_end` entries gain aggregated usage and cost totals. (Timing/`duration_ms` already specified.)
- `cli-adapter`: The adapter contract gains an optional usage-extraction capability so each adapter can surface structured usage from its CLI's output.
- `view-run`: Per-step and per-run usage, cost, and timing appear in the inspect run view.
- `live-run-view`: Per-step and per-run usage, cost, and timing appear in the live run view.

## Technical Approach

Agent Runner owns measurement end to end: it collects usage and durations at the adapter boundary, values them against an embedded catalog, aggregates by step and run, and writes a stable artifact. It never fetches live prices during a run.

**Extraction seam (adapter level).** The `cli.Adapter` interface already uses optional capability interfaces (`OutputFilter`, `StdoutWrapper`, `HeadlessResultFilter`) that adapters implement when their CLI emits structured JSONL. Usage extraction follows the same pattern: a new optional interface (e.g. `UsageExtractor`) that Claude and Codex implement to parse category-level token counts from their output after the process exits. Adapters that don't implement it yield an explicit "unavailable" usage record rather than zeros. The two backends start from different baselines: Codex already carries a full `usage` object (input, cached input, output, reasoning output) in its `turn.completed` JSONL that the runner parses but currently discards, so extraction is a small extension. Claude's headless invocation uses plain text (`-p`) and emits no usage today, so this change also adds `--output-format json` to Claude's arg construction and parses the `usage` block (input, output, cache-creation, cache-read) from its result event. Interactive/PTY runs capture no stdout, which is why autonomous/headless collection comes first.

```
agent step runs CLI
   → adapter extracts raw usage (categories, provider, actual model, source, completeness)
      → structured usage carried through the agent process result
         → cost-estimation values it against the embedded catalog (catalog version recorded)
            ├─ step_end audit event  (usage + estimated_api_cost_usd)
            ├─ run_end audit event   (aggregated totals)
            └─ run-metrics.json      (per-step + per-run, versioned schema)
                  ↑
            run view (live + inspect) reads/renders the same metrics
```

**Persistence.** `run-metrics.json` lives alongside `audit.log` in the existing run directory (`~/.agent-runner/projects/{encoded-path}/runs/{run-id}/`). It is the declared boundary for consumers; Agent Evals reads this artifact rather than reconstructing metrics from audit internals or CLI transcripts.

**Raw usage vs. valuation.** Each usage record keeps its raw measurement (provider, actual model, per-category token counts, measurement source, completeness) separate from the derived valuation (`estimated_api_cost_usd`, pricing snapshot/catalog version). This keeps historical records re-valuable if the catalog changes and makes missing data explicit at each layer.

**Key decisions carried from the handoff:** (1) Runner owns measurement, not consumers. (2) `run-metrics.json` is the supported boundary. (3) The monetary field is `estimated_api_cost_usd` (API-price equivalent). (4) Missing usage/pricing is `null`/unavailable, never zero. (5) Autonomous Claude + Codex first; interactive collection is a deferred phase.

**Schema-first sequencing.** The versioned usage and `run-metrics.json` schemas are defined before adapter changes, since audit events, the artifact, and the UI all depend on them.

## Out of Scope

- Interactive-session usage collection (deferred second phase; requires adapter-specific post-run transcript extraction).
- Adapters other than Claude and Codex (Copilot, Cursor, OpenCode) — they report unavailable usage until a later phase adds extraction.
- Eval-side aggregation, reporting, or dashboards built on `run-metrics.json` — that is Agent Evals' concern.
- Live price fetching or a networked pricing service — the catalog is embedded and release-versioned.
- Reporting the user's actual billed cost or reconciling against subscription billing.

## Impact

- **Code:** `internal/cli/adapter.go` (new optional usage interface), `internal/cli/claude.go`, `internal/cli/codex.go` (extraction), `internal/exec/agent.go` (carry usage through the result), `internal/audit/types.go` (new fields), `internal/runner/runner.go` (aggregation + artifact write), `internal/runview/` (UI). A new pricing-catalog component and `run-metrics.json` writer.
- **Specs:** New `agent-usage-collection`, `cost-estimation`, `run-metrics-artifact`; deltas to `audit-log-entries`, `cli-adapter`, `view-run`, `live-run-view`. Possibly `audit-log-storage` if the artifact's location is specified there.
- **Consumers:** Agent Evals gains a documented `run-metrics.json` contract; its version must be stable across releases.
- **Dependencies:** No new runtime service dependencies; pricing data ships in the binary.
