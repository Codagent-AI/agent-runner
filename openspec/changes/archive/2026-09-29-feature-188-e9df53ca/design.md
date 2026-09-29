## Context

Headless Claude steps run `claude -p --output-format stream-json`. Agent Runner extracts usage in
`internal/exec/invocation.go` → `extractAgentUsage` → `cli.UsageExtractor.ExtractUsage(rawStdout)`.
`ClaudeAdapter.ExtractUsage` (`internal/cli/claude.go`) has three problems today:

- It reads token counts only from the final `result` event's `usage`, which covers the main thread
  only.
- It takes `total_cost_usd` as `RawCumulativeCostUSD`. The metrics collector turns that into a
  per-step cost by delta.
- It sets the model from the last event carrying one, including subagent events.

The metrics collector (`internal/metrics`) stores the `model.UsageRecord` on a `StepRecord` and
builds three things from it:

- `NativeMeasurement` (`native_measurements.go`), with one observed identity and an
  `unallocated_usage` fallback;
- run and session totals (`totalsForRecords` in `collector.go`);
- per-field `measurement_totals` (`accumulateFields`).

Facts verified against real factory runs (Claude Code with Opus 5.5, Sep 2026):

- **Where subagent transcripts live.** Subagent transcripts are at
  `<config>/projects/<encoded-cwd>/<session-id>/subagents/agent-<id>.jsonl`. Each has a sidecar
  `agent-<id>.meta.json` containing `agentType`, `description`, `toolUseId`, and `spawnDepth`.
- **What transcripts contain.** Transcript `assistant` entries carry `message.id`,
  `message.model`, and `message.usage`. The same message can be written several times as it
  streams, and the last entry holds the final usage.
- **How subagents are spawned.** The main thread spawns subagents through a `tool_use` content
  block named `Agent` (older CLI versions use `Task`) in the parent transcript
  `<config>/projects/<encoded-cwd>/<session-id>.jsonl`.
- **Stdout and parent-transcript UUIDs match.** Stdout events carry `uuid` values that equal the
  parent transcript entries' `uuid`s. In the sampled `simplify` step:
  - 97 stdout UUIDs matched transcript entries 933–1120 of 1378;
  - the step's four `Agent` tool uses were entries 942–945;
  - an earlier step's `Agent` tool use at entry 357, in the same inherited session, fell outside
    that range.
- **Stdout tracks subagent tasks.** Stdout carries `system` `task_started`, `task_updated`, and
  `task_notification` events keyed by `tool_use_id`, from which subagent completion can be
  determined.
- **Long working directories are truncated.** The encoded project directory is truncated and
  suffixed with a hash for long working directories (for example `...-38829-d83hg1`). The
  `/`, `.`, `_` → `-` encoding used by `SessionExists` therefore does not always find the
  directory.
- **Cost already includes subagents.** `total_cost_usd` equals the sum of
  `modelUsage[*].costUSD`, which includes subagent calls. `modelUsage` is session-cumulative and
  keyed by model only; the keys include context-window variants such as `claude-opus-5-5[1m]`.
- **Validator projections are always partial.** Nested Validator step projections always set
  `Completeness: partial`, even when the Validator's collection was complete. They count toward
  coverage as reported today.

## Goals / Non-Goals

**Goals:**
- Collect subagent tokens and models for headless Claude steps, attribute them to the spawning
  invocation, and keep them as separate allocations from main-thread usage.
- Include subagent tokens once in step compatibility fields, run and session totals, and
  `measurement_totals`.
- Surface partial subagent collection as partial usage and canonical-total coverage without
  changing coverage semantics for other sources.
- Leave cost values unchanged.

**Non-Goals:**
- Any pricing, or allocation-scoped USD cost.
- Other CLIs, interactive contexts, and historical backfill.
- Fixing `SessionExists` for truncated project directories. The new resolver can be reused there
  later.

## Approach

### 1. Context-aware usage extraction (`internal/cli`)

Add an optional adapter capability next to `UsageExtractor` in `adapter.go`:

```go
// UsageContext carries invocation facts that some adapters need to locate
// out-of-band usage evidence.
type UsageContext struct {
    Workdir string   // invocation working directory ("" = process cwd)
    Env     []string // effective child environment (input.Env over os.Environ, minus DropEnv)
}

type ContextualUsageExtractor interface {
    ExtractUsageWithContext(rawStdout string, uc UsageContext) (UsageExtraction, error)
}
```

`extractAgentUsage` gains a `cli.UsageContext` argument. It prefers `ContextualUsageExtractor`
when the adapter implements it and the context is headless, and otherwise falls back to
`ExtractUsage`. `invocation.go` passes `input.Workdir` and the effective environment it already
computes for the process.

`ClaudeAdapter` changes:

- **`ExtractUsage` (stdout only).**
  - The main-thread model is taken only from events whose `parent_tool_use_id` is absent or null.
    This covers the `system/init` `model` field and parent `assistant` `message.model`. The
    result event does not carry a model.
  - It also collects `session_id`, the stdout UUID set, spawn tool-use IDs, and task lifecycle
    state into an internal `claudeStream` struct.
  - Its behavior is otherwise unchanged.
- **`ExtractUsageWithContext`.** It runs the stdout parse, builds the main-thread allocation from
  the result event, then calls `collectClaudeSubagents` (new file `claude_subagents.go`).

### 2. Transcript location

`claudeTranscriptPaths(sessionID, uc)` resolves the paths as follows:

1. **Config root.** Use `CLAUDE_CONFIG_DIR` from `uc.Env` (last occurrence wins) if set.
   Otherwise use `~/.claude`.
2. **Project directory.** Try `<root>/projects/<encoded(abs(workdir))>/<session>.jsonl` first. If
   that is absent, glob `<root>/projects/*/<session>.jsonl`. Session IDs are UUIDs, so exactly
   one match is expected. More than one match makes collection partial with reason
   `transcript-ambiguous`.
3. **Subagents directory.** The subagents directory is `<projectDir>/<session>/subagents`.

Tests inject the root through `CLAUDE_CONFIG_DIR` in `uc.Env`, so no home-directory stubbing is
needed.

### 3. Spawn discovery and attribution (`collectClaudeSubagents`)

```text
stdout ──► session_id, stdout UUIDs, stdout spawn IDs, task states
   │
parent transcript ──► span = [first entry whose uuid ∈ stdout UUIDs,
   │                          last entry whose uuid ∈ stdout UUIDs]
   │                  spawn IDs += Agent/Task tool_use ids in span (non-sidechain entries)
   │                  failed spawns = tool_result is_error for those ids
   ▼
sidecars (*.meta.json) ──► toolUseId → transcript file
   ▼
BFS over spawn IDs:
  for each matched transcript: parse, dedupe by message.id (last wins),
  sum usage per model, then enqueue the Agent/Task tool_use ids found in that transcript
  (nested spawns)
```

- **Span.** The span runs from the first to the last parent-transcript entry whose `uuid`
  appears in stdout.
  - If no stdout UUID matches, or the parent transcript is missing or unreadable, the span is not
    established. In that case, spawns found through stdout are still collected, and subagent
    collection is `partial` (`subagent-span-unavailable`).
  - An empty stdout spawn set is never treated as proof of zero subagents.
- **Invalid parent-transcript entries.** Line positions are known even for lines that cannot be
  parsed. Two cases make subagent collection `partial` with reason
  `subagent-parent-transcript-invalid`, because either could hide a spawn:
  - a line inside the span, or immediately after its last matched entry, that is not valid JSON
    or is truncated;
  - an assistant entry in the span whose `message.content` cannot be decoded, or whose
    `tool_use` block has no `id` or an unrecognized shape.

  Spawns found on valid lines, and all usage, are still collected. Invalid lines outside the span
  are ignored, which covers a partially flushed tail written by a later process. When several
  partial reasons apply, the first in this order is recorded: `subagent-span-unavailable`,
  `subagent-parent-transcript-invalid`, `subagent-transcript-missing`,
  `subagent-transcript-invalid`, `subagent-still-running`. Per-allocation reasons remain on
  their allocations.
- **Spawn set.** The spawn set is the union of the stdout spawn IDs and the span spawn IDs.
  - Stdout spawn IDs come from parent-scoped `assistant` `tool_use` blocks named `Agent` or
    `Task`, plus `task_started` or `task_notification` `tool_use_id`s.
  - A spawn whose `tool_result` is an error and which has no sidecar is dropped as never started.
    It does not make collection partial.
- **Missing transcript.** A spawn with no sidecar produces an unavailable allocation (reason
  `subagent-transcript-missing`) and makes collection `partial`.
- **Incomplete transcript.** A transcript containing an unparseable line keeps the usage from its
  valid lines, and its allocation is `partial` (`subagent-transcript-invalid`). The same applies
  to a transcript that has no usage-bearing assistant entries.
- **Running subagent.** A subagent whose stdout `task_started` has no later terminal
  `task_updated` or `task_notification` status is `partial` (`subagent-still-running`). Its usage
  so far is kept, and Agent Runner does not wait for it.
- **Ignored entries.** Entries with model `<synthetic>` (locally generated error messages) are
  skipped. Entries without `message.id` are counted individually.
- **Counting once.** The `visited` set on tool-use IDs keeps each subagent from being counted
  twice within one step. Transcripts spawned outside this invocation's span are never reached, so
  steps sharing an inherited session cannot double count.

Subagent usage maps to the same canonical categories as the main thread: `input_tokens`,
`cache_read_input_tokens`, `cache_creation_input_tokens`, and `output_tokens`. Canonical totals
per allocation use the existing Claude formula: input + cache read + cache write, then output.

### 4. Usage record shape (`internal/model/usage.go`)

```go
type UsageAllocation struct {
    ID            string            `json:"allocation_id"`   // "main", "subagent:<toolUseId>[:<model>]"
    Kind          string            `json:"kind"`            // "main" | "subagent"
    Status        UsageStatus       `json:"status"`
    Reason        UnavailableReason `json:"reason,omitempty"`
    Model         string            `json:"model,omitempty"`
    Tokens        TokenCounts       `json:"tokens,omitempty"`
    TokenTotals   *TokenTotals      `json:"token_totals,omitempty"`
    Completeness  Completeness      `json:"completeness,omitempty"`
    AgentType     string            `json:"agent_type,omitempty"`
    ToolUseID     string            `json:"tool_use_id,omitempty"`
    ParentToolUseID string          `json:"parent_tool_use_id,omitempty"` // nested spawns
    SpawnDepth    int               `json:"spawn_depth,omitempty"`
}

// on UsageRecord:
Allocations         []UsageAllocation `json:"allocations,omitempty"`
SubagentCollection       Completeness      `json:"subagent_collection,omitempty"`        // "" when not attempted
SubagentCollectionReason UnavailableReason `json:"subagent_collection_reason,omitempty"` // set when partial
```

- **Allocations.** There is one allocation per subagent and model. A subagent whose transcript
  shows several models gets one allocation per model.
- **Attempt-level fields on the `UsageRecord`:**
  - `Tokens` is the category-wise sum of collected allocations.
  - `TokenTotals` is the sum of the allocations that have totals. It is nil when none do.
  - `Model` and `Identity` stay main-thread.
  - `Completeness` is `partial` if the main thread is partial or unavailable, or if
    `SubagentCollection` is `partial`.
  - `Status` is `collected` when any allocation is collected. The main thread unavailable with
    subagents collected is therefore still `collected`, with `partial` completeness.
  - `RawCumulativeCostUSD` is unchanged.
- **No subagents.** When no subagents were found and the span was established, `Allocations`
  holds only `main` and `SubagentCollection` is `complete`. The record's values then equal
  today's.
- **Stdout parse failure.** Subagent collection is not attempted, because the session ID is
  unknown. The existing `parse-failure` unavailable record is kept.

`SubagentCollectionReason` carries the step-level cause when `SubagentCollection` is `partial`.
It is needed because a span failure can occur with no allocation to hold the reason (for example,
zero spawns on stdout). `UsageRecord.Reason` stays reserved for unavailable records.

Add the reasons `subagent-transcript-missing`, `subagent-transcript-invalid`,
`subagent-still-running`, `subagent-span-unavailable`, `subagent-parent-transcript-invalid`, and
`transcript-ambiguous` to the `UnavailableReason` set.

### 5. Metrics collector (`internal/metrics`)

- **Pass-through.** Claude usage is attributed per invocation (no cumulative baseline), so
  `Allocations`, `SubagentCollection`, and `SubagentCollectionReason` pass through. Any usage clone helper must copy them.
- **`totalsForRecords`.**
  - A step with `Usage.SubagentCollection == partial` counts as partially reported for
    `UsageCoverage` and `TokenTotalCoverage`, while its known subtotal is still summed.
  - `coverage(agents, full, partial)` returns:
    - `none` when `full + partial == 0`;
    - `complete` when `full == agents && partial == 0`;
    - `partial` otherwise.
  - Other `Completeness: partial` records keep today's behavior. This avoids flipping every run
    that contains Validator projections to `partial`.
  - Cost coverage is untouched.
  - Session rollups reuse `totalsForRecords`, so they inherit the rule.
- **`nativeMeasurement`.**
  - Bump `native_measurement_schema_version` to 2 and add
    `Allocations []NativeAllocation \`json:"allocations"\``. Each allocation carries
    `allocation_id`, `kind`, `observed_identity_ref`, subagent evidence (agent type, tool-use ID,
    parent tool-use ID, spawn depth), availability and reason, a per-allocation `tokens` map in
    the common `Value` vocabulary, and a `cost` `Value`. The `cost` is always `unavailable` with
    reason `allocation_cost_not_reported`, for the main and every subagent allocation.
  - Add attempt-level `subagent_collection: {completeness, reason}` (null when not attempted),
    carrying `SubagentCollectionReason` verbatim so the specific cause survives into
    `run-metrics.json`.
  - Each distinct allocation model gets its own `observed_identities` entry (`observed-main`,
    `observed-<n>`). Allocations with the same model still keep separate allocation IDs.
  - When allocations exist, `unallocated_usage` is null and the `model_allocation_unavailable`
    limitation is not added.
  - Attempt-level `tokens` are projected from the summed `UsageRecord` as today. When
    `SubagentCollection` is `partial`, each available attempt field becomes
    `availability: "partial"`, with the reason set to the `SubagentCollectionReason` value
    converted to snake_case (for example `subagent_span_unavailable`). `accumulateFields`
    already counts these fields as partial in `measurement_totals`.
  - Allocation token values are evidence only. Aggregations continue to read attempt-level
    `tokens` only, so allocations are never summed again.
- **`collectReportedCost`.** The step's full `attempt`-scope cost is unchanged. No
  allocation-scope entries are added to `provider_reported_costs`. Allocation cost state lives
  only in each allocation's unavailable `cost` value, which is never zero and never
  token-derived.

### 6. Consumers

`runview` and report code that read `usage.tokens`, `usage.token_totals`, or run totals pick up
subagent tokens automatically. No UI change is included.

## Decisions

- **Span bounding by stdout UUIDs.** This is deterministic, needs no clocks, and was verified on
  real data to isolate one invocation within an inherited session. Timestamp windows were
  rejected: clock skew and concurrent writes make them fragile. Message-ID bounding was rejected:
  in the sample, message IDs matched entries across the whole file (4–1368), so they do not bound
  the span.
- **Transcript directory resolution.** Use `CLAUDE_CONFIG_DIR` from the child environment, then
  the encoded working directory, then a glob on the session ID. This handles config overrides and
  the truncated encoding observed for long paths.
- **No allocation-scoped cost.** `modelUsage` is session-cumulative per model. Attributing it
  would need new per-model baselines, and the keys vary (`[1m]` variants). It also cannot
  separate main-thread from subagent cost when both use the same model, which is the common
  `simplify` case. The full step cost already includes subagent spend, so allocation cost is
  unavailable. The `cost-capture` spec is updated to say so plainly.
- **Partial coverage scoped to `SubagentCollection`.** Generalizing to every
  `Completeness: partial` record would flip coverage for all runs containing Validator nested
  attempts, whose projections are always partial. That is a regression outside this issue.
- **Contextual extractor interface.** An optional interface keeps the four other adapters and
  their tests untouched. Changing `ExtractUsage`'s signature for all adapters was rejected.
- **Allocation per subagent (and model).** This matches the spec requirement that a missing
  subagent has its own unavailable allocation. It supersedes the proposal-time "grouped by model"
  wording. Reports can still group allocations by model.
- **Native schema version 2.** The meaning of attempt `tokens` changes (it now includes
  subagents), and `allocations` is new. The version makes that explicit for consumers. Existing
  v1 measurements already in an artifact are retained as-is.

## Risks / Trade-offs

- **Undocumented Claude Code file formats.** The sidecar, transcript, and tool names are not a
  public API. Mitigations:
  - Unknown shapes degrade to `partial` with a reason, never to a failure or a silent zero.
  - Both `Agent` and `Task` tool names are accepted.
  - Fixtures pin the current format.
- **Read cost.** Parent transcripts can be several MB. They are read once per headless Claude
  step, with streaming line scanning and a large buffer (reusing `newStreamScanner`). The added
  latency is acceptable after an agent step.
- **Background subagents.** If they are killed at CLI exit, their usage is under-reported and
  flagged `partial`. That is acceptable and explicit.
- **Main-thread model change.** The fix to main-thread model tracking can change the observed
  model for existing Claude steps that spawned subagents on another model. This corrects a
  mislabel.

## Testing

All fixtures live under `internal/cli/testdata/usage/claude-subagents/<case>/`. Each case holds
a `stdout.jsonl` and a `config/projects/<enc>/` tree containing the parent transcript and a
`subagents/` directory. `CLAUDE_CONFIG_DIR` points at the fixture copy in `t.TempDir()`.

- **`internal/cli` table tests for `ExtractUsageWithContext`.** These cover the acceptance
  fixture and each failure mode:
  - `basic`, the issue's acceptance fixture: one subagent on a different model. Asserts the
    tokens and model in the allocation, plus the summed totals.
  - `parallel-four`: four parallel subagents.
  - `inherited-session`: an earlier `Agent` use outside the span is excluded.
  - `spawn-missing-from-stdout`.
  - `nested`: a depth-2 subagent.
  - `streamed-duplicates`: message-ID dedupe.
  - `mixed-model-trailing-subagent-event`.
  - `missing-transcript`.
  - `span-unavailable`: zero spawns on stdout; asserts `subagent_collection_reason`.
  - `parent-invalid-line`: valid bounding UUIDs with a malformed line and an unknown `tool_use`
    shape inside the span. Asserts the known subtotal and `partial` with
    `subagent-parent-transcript-invalid`.
  - `parent-invalid-tail`: a malformed line outside the span is ignored.
  - `invalid-line`.
  - `still-running`.
  - `failed-spawn`: an errored tool_result with no sidecar is not partial.
  - `truncated-project-dir`: glob fallback.
  - `no-subagents`: output identical to today apart from the `main` allocation.
- **`internal/metrics` tests.**
  - Totals include subagent tokens once.
  - Partial subagent collection gives `partial` usage and canonical-total coverage, `complete`
    cost coverage, the known subtotal, and a partial session rollup.
  - Validator partial projections do not change coverage.
  - The native measurement lists allocations with observed identities, and each allocation's
    `cost` is unavailable with a reason (main and subagent), never zero.
  - `subagent_collection.reason` is preserved.
  - `measurement_totals` counts partial fields.
- **`internal/exec`.** `extractAgentUsage` passes the working directory and environment through,
  and prefers the contextual extractor.
- **Post-merge acceptance.** A real factory run with `/simplify` shows non-zero subagent
  allocations on the `simplify` step. This needs a human-triggered run.

## Migration Plan

This is additive. Old `run-metrics.json` files and v1 native measurements are read unchanged.
Rolling back removes the new fields, and old readers already ignore unknown fields.

## Open Questions

None.
