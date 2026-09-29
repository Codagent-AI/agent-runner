## Why

Claude agent steps often delegate work to Task-tool subagents. For example, `/simplify` fans out four
parallel reviewers. `run-metrics.json` records only the main thread's token usage for these steps.
In the Sep 21–28 factory analysis, subagents accounted for about $100 of $453 in instrumented Claude
spend (about 22%). That share was only visible after someone read the transcripts by hand. The
`simplify` step alone hid $23.50 of subagent work.

Investigating the current behavior found two things:

- **Tokens are missing.** The Claude adapter reads the `usage` object from the final `result`
  event, and that object covers only the main thread. For a real `simplify` step, the main thread
  reported about 11.6M processed tokens. Its five subagent transcripts held about 3.1M more, and
  none of those reached the step's measurement. The subagent model is not recorded anywhere.
- **Cost is already included but cannot be separated.** Claude's cumulative `total_cost_usd`
  equals the sum of `modelUsage[*].costUSD`, and `modelUsage` covers both main-thread and subagent
  calls. In one verified session, main-thread `usage` plus the subagent transcripts equals the
  `modelUsage` cache-read and cache-write counts exactly. The step's `estimated_api_cost_usd` is
  derived by delta from `total_cost_usd`, so it already includes subagent spend. Reports still
  cannot say how much of it came from subagents. Because the numbers are mixed, the step's
  cost-per-token also looks wrong.

Recording subagent usage makes token totals match the cost that is already reported. It also lets
reports show main-thread and subagent consumption side by side, which factory cost analysis needs
now that fan-out skills are common.

**Verdict: go with caveats.** The gap is real and well bounded, and the measurement vocabulary
already models per-model allocations and unallocated evidence. The caveat is cost. Agent Runner
will not add a pricing catalog (see Technical Approach). Subagent cost stays included in the step's
full reported cost, which Claude Code prices at each model's own rates. Per-allocation USD is
recorded only where Claude's own telemetry establishes it. Otherwise it is explicitly unavailable.

## What Changes

- After a headless Claude agent step exits, Agent Runner collects token usage and model identity
  for the subagents that the invocation spawned. It reads them from Claude Code's subagent
  transcripts (`<session transcript dir>/<session-id>/subagents/agent-*.jsonl` and their
  `.meta.json` sidecars).
- Subagent usage is attributed to the step whose invocation spawned it, by matching the spawning
  Task tool-use ID against each sidecar's `toolUseId`. Several steps can share one inherited
  session, so this matching keeps each subagent from being counted twice.
  - The set of spawn IDs comes from reconciling two sources: the invocation's captured stdout,
    and the direct Task tool uses in the parent session transcript within this invocation's span.
    The span is bounded by the message IDs and UUIDs the invocation emitted on stdout.
  - Nested subagents (spawn depth > 1) are discovered recursively from each subagent's own
    transcript and attributed through their spawning subagent.
  - An empty spawn set is not proof of zero subagents. If the invocation's span in the parent
    transcript cannot be established, known tokens are kept but subagent collection is `partial`
    with a reason.
- The step's measurement keeps main-thread and subagent usage separate. Each has its own
  allocation with its own observed model identity. The step's attempt-level token totals include
  both, and aggregates count each token once.
  - The main-thread model comes only from parent-scoped telemetry: events without
    `parent_tool_use_id`, plus the `result` event.
  - Each subagent's model comes only from its own transcript. Subagent events in stdout must never
    relabel main-thread tokens, which the current `ExtractUsage` model tracking allows.
- Cost:
  - The step's `estimated_api_cost_usd` keeps its current source: the attributed delta of Claude's
    `total_cost_usd`, which already includes subagents.
  - Allocation-scoped cost evidence is recorded only when Claude telemetry establishes it
    unambiguously. Otherwise it is unavailable with a reason, never computed from tokens.
- Collection is best-effort, and gaps are explicit:
  - If stdout shows subagent spawns but their transcripts are missing or unreadable, the subagent
    allocation is unavailable with a reason and the step's usage completeness becomes `partial`.
  - A transcript failure never changes the step's outcome.
  - Partial collection is an explicit aggregate contract:
    - The known subtotal is kept in `tokens` and `token_totals` and is never presented as
      complete.
    - Affected canonical fields and run- and session-level usage and canonical-total coverage
      report `partial`.
    - Cost coverage stays independent.
    - Today, `totalsForRecords` in `internal/metrics/collector.go` counts every collected step as
      fully reported without checking `Completeness`. The native projection likewise emits
      partial values as available. Both must honor partial completeness.
- Run-level aggregates, the compatibility step views (`tokens`, `token_totals`), and the TUI and
  report views that read them include subagent tokens.

No **BREAKING** changes. `run-metrics.json` gains subagent allocation data in the existing
allocation and unallocated vocabulary. For Claude steps that spawned subagents, existing token
fields report larger (now complete) values.

## Capabilities

### New Capabilities
- `claude-subagent-usage`: how headless Claude steps discover their subagent transcripts,
  attribute them to the spawning invocation, map their usage and model, and behave when
  transcripts are missing, partial, nested, or shared across steps.

### Modified Capabilities
- `agent-usage-collection`: a step's usage record can combine several sources (the main-thread
  `result` event plus subagent transcripts). It keeps them as distinct allocations and makes
  attempt totals and completeness reflect both.
- `run-metrics-artifact`: native Claude measurements carry subagent allocations. Aggregates and
  compatibility views include subagent tokens exactly once.
- `cost-capture`: records that Claude's reported step cost already covers subagent spend, and how
  allocation-scoped cost is recorded or marked unavailable without a pricing catalog.

## Technical Approach

- **Source of truth: transcripts, not stdout.** Headless `stream-json` stdout does include subagent
  `assistant` events (those with `parent_tool_use_id` set), but the data is incomplete. In the
  sampled `simplify` run it held 38 of 59 subagent messages, and its output-token counts were
  mid-stream values (340 compared with 40,132 in the transcripts). The transcripts are
  authoritative for usage and model. Spawn IDs come from reconciling stdout with the parent
  session transcript's direct Task tool uses inside the invocation's span, then recursing into
  each subagent transcript. Stdout alone is not trusted to list every spawn.
- **Adapter-owned.** The Claude adapter already resolves the transcript location for
  `SessionExists`. Transcript discovery and usage mapping stay in `internal/cli` behind the
  adapter's usage extraction, so other adapters are unaffected. Usage from repeated streaming
  entries is deduplicated by message ID, keeping the final entry, before it is summed.
- **Measurement fit.** The native measurement projection (`internal/metrics`) currently records a
  single observed identity and falls back to unallocated usage. It will emit:
  - one allocation for the main thread;
  - subagent allocations grouped by observed model, keeping subagent identity such as agent type
    and tool-use ID as evidence.

  This reuses the allocation, identity, and cost-scope structures already defined for Validator
  measurements. It does not introduce a new schema concept.
- **No pricing catalog.** The `cost-capture` spec forbids token-times-rate math. That is unnecessary
  here, because Claude Code already prices subagent calls at their own model's rates inside
  `total_cost_usd`. Adding and maintaining a catalog would be redundant and would drift.
  - When a subagent's model is the only user of its `modelUsage` entry across the invocation's
    delta, that entry's cost may serve as allocation-scoped evidence.
  - `design.md` settles whether that is reliable enough to ship. If not, allocation cost is
    unavailable.
- **Timing.** Transcripts are read after the CLI process exits, when foreground subagents have
  finished writing. A background subagent still running at exit is recorded as partial and is not
  waited for.

## Out of Scope

- A pricing catalog, or computing USD from token counts.
- Subagent usage for non-Claude CLIs (Codex, Cursor, Copilot, OpenCode) and for interactive or
  autonomous-interactive Claude steps. Those steps already report `interactive-context` usage.
- Backfilling subagent usage into historical `run-metrics.json` files.
- New TUI or report layouts beyond making existing totals include subagents. Views that want to
  show a main-versus-subagent breakdown can read the allocations later.
- Waiting for background subagents that outlive the parent CLI process.

## Impact

- **Code:**
  - `internal/cli/claude.go` and `usage.go`: transcript discovery, tool-use ID extraction, and
    usage mapping.
  - `internal/model`: allocation-capable usage records.
  - `internal/metrics/native_measurements.go` and `collector.go` (`totalsForRecords`):
    allocations, totals, and partial-completeness-aware coverage.
  - Possibly `internal/measurements` helpers.
- **Tests:** fixtures under `internal/cli/testdata` with Claude streams, parent transcripts,
  subagent transcripts, and sidecars:
  - Basic Task tool uses. This covers the issue's acceptance test.
  - A mixed-model case where subagent events follow the last parent model event. It asserts
    distinct main-thread and subagent identities and allocations.
  - A missing-transcript case. It asserts both the retained numeric subtotal and `partial`
    coverage in `run-metrics.json`.
  - A spawn missing from stdout but present in the parent transcript.
- **Data:** `run-metrics.json` for Claude steps with subagents shows larger token totals and new
  allocation entries. Cost totals are unchanged.
- **Filesystem:** Runner reads additional files under the Claude transcript directory after
  headless Claude steps. The reads are read-only, and a failure only degrades completeness.
- **Acceptance:** after merge, a real factory run with `/simplify` must show non-zero subagent
  usage for the `simplify` step. This needs a human-triggered factory run and is tracked as
  post-merge verification.
