# Decisions

## proposal: verdict go with caveats
- **Decision:** Proceed. Subagent token usage is missing from Claude step measurements. Subagent cost is already inside `total_cost_usd` but cannot be separated.
- **Alternatives:** No-go because cost is already captured (rejected: tokens and model are still missing, and cost cannot be separated as the issue requires).
- **Decision-bearing:** yes

## proposal: no pricing catalog
- **Decision:** Do not compute USD from tokens. The step cost keeps using the attributed delta of Claude's `total_cost_usd`. Checked against real runs, this already includes subagent calls priced at each model's own rates. Allocation-scoped cost is recorded only when Claude telemetry establishes it, and is otherwise unavailable.
- **Alternatives:** Add a per-model pricing catalog to price subagent allocations (rejected: contradicts the `cost-capture` spec, duplicates Claude Code's own pricing, and drifts over time).
- **Decision-bearing:** yes. The issue's "price at its own model's rates" is satisfied by Claude's reported cost, not by Runner-side pricing.

## proposal: transcripts as usage source
- **Decision:** Read subagent usage from `subagents/agent-*.jsonl`, deduplicated by message ID with the final entry kept. Use stdout only to identify spawned Task tool-use IDs.
- **Alternatives:** Use `parent_tool_use_id` assistant events in stdout (rejected: in a real run they held 38 of 59 messages and mid-stream output counts). Use `modelUsage` deltas (rejected: cannot separate main-thread from subagent usage when both use the same model).
- **Decision-bearing:** yes

## proposal: attribution by tool-use ID
- **Decision:** Attribute a subagent to the step whose stdout contains its spawning Task tool-use ID, matched to the sidecar's `toolUseId`. Nested subagents are attributed through their parent subagent.
- **Alternatives:** Attribute every subagent transcript in the session (rejected: double counts when steps inherit a session). Use timestamp windows (rejected: fragile).
- **Decision-bearing:** no

## proposal: representation
- **Decision:** Record a separate main-thread allocation plus subagent allocations grouped by model, using the existing allocation and identity vocabulary. Attempt totals and compatibility `tokens` and `token_totals` include subagents exactly once.
- **Alternatives:** A separate top-level subagent record per step (rejected: would be counted as an extra attempt). Keep compatibility totals main-thread only (rejected: the issue says totals must include subagents).
- **Decision-bearing:** no

## proposal: scope limits
- **Decision:** Claude headless only. No historical backfill. Do not wait for background subagents that outlive the CLI; record them as partial.
- **Alternatives:** Cover other CLIs as well (rejected: the issue is Claude-specific and other CLIs lack an equivalent transcript source).
- **Decision-bearing:** no

## proposal-review: PR-1 spawn discovery boundary (applied)
- **Decision:** Build the spawn set by reconciling stdout with the parent session transcript's direct Task tool uses inside the invocation's span (bounded by the message IDs and UUIDs emitted on stdout), then recurse into subagent transcripts. If the span cannot be established, keep known tokens and mark subagent collection `partial` with a reason. An empty stdout spawn set is never proof of zero subagents.
- **Alternatives:** Use stdout-only spawn IDs (rejected: stdout was shown to omit subagent events, so a missed spawn would silently report complete). Use every transcript in the session (rejected: double counts across inherited sessions).
- **Decision-bearing:** no

## proposal-review: PR-2 partial collection in aggregates (applied)
- **Decision:** Make partial subagent collection an explicit aggregate contract. Keep the known subtotal in `tokens` and `token_totals`; mark affected canonical fields and run- and session-level usage and canonical-total coverage `partial`; keep cost coverage independent. `totalsForRecords` and the native projection must honor `Completeness`. Add a missing-subagent fixture that asserts both the subtotal and `partial` coverage.
- **Alternatives:** Leave the aggregate logic as is (rejected: it would report complete coverage for a partial subtotal, contrary to the `agent-usage-collection` partial-value semantics).
- **Decision-bearing:** no

## proposal-review: PR-3 model identity scoping (applied)
- **Decision:** Read the main-thread model only from parent-scoped telemetry (events without `parent_tool_use_id`, plus the `result` event). Read each subagent's model only from its own transcript. Add a mixed-model fixture with subagent events after the last parent model event.
- **Alternatives:** Keep the current last-seen-model tracking in `ExtractUsage` (rejected: a trailing subagent event can mislabel main-thread tokens).
- **Decision-bearing:** no

## spec: delta style for modified capabilities
- **Decision:** Express changes to `agent-usage-collection`, `run-metrics-artifact`, and `cost-capture` as ADDED requirements that refine the existing ones, not by rewriting the large existing requirement blocks. No existing scenario is contradicted.
- **Alternatives:** MODIFIED blocks copying the full "Artifact content" and cost requirements (rejected: large copies with a high risk of accidental drift, for no behavioral gain).
- **Decision-bearing:** no

## spec: partial-coverage rule scoped to subagent collection
- **Decision:** Partial subagent collection makes run- and session-level usage and canonical-total coverage `partial`. Whether existing partial records from other sources (such as a missing expected category) should also flip coverage is deferred to design, because it changes reports for other adapters that are outside this issue.
- **Alternatives:** Apply the rule to every `partial` usage record now (deferred: broader behavioral change beyond issue #188).
- **Decision-bearing:** no

## spec: step without subagents
- **Decision:** A step whose invocation span is established and contains no Task tool uses records no subagent allocations and keeps its current completeness. Subagent evidence is never fabricated as zero.
- **Alternatives:** Always emit an empty subagent allocation (rejected: noise, and it could read as an observed zero).
- **Decision-bearing:** no

## spec: background and unparseable subagent transcripts
- **Decision:** Do not wait for background subagents. Record whatever their transcripts hold at collection time and mark it `partial`. For an invalid transcript line, retain usage from valid lines and mark the allocation `partial` without failing the step.
- **Alternatives:** Wait for background subagents to finish (rejected: it would delay step completion by an unbounded amount). Discard the whole transcript on a bad line (rejected: loses known usage).
- **Decision-bearing:** no

## spec: main thread unavailable but subagents collected
- **Decision:** Retain subagent usage and mark the step `partial`, with the main-thread allocation unavailable (`no-usage-event`).
- **Alternatives:** Mark the whole step unavailable (rejected: discards known consumption).
- **Decision-bearing:** no

## spec: deferred-to-design items
- **Decision:** Defer to design:
  - how the invocation's span in the parent transcript is bounded;
  - transcript directory resolution when Claude's config directory is overridden;
  - whether `modelUsage` cost deltas qualify as allocation-scoped cost evidence;
  - whether the partial-coverage rule generalizes to other sources.
- **Alternatives:** Decide these now in the specs (rejected: they are architectural choices).
- **Decision-bearing:** no

## design: invocation span bounded by stdout UUIDs
- **Decision:** The invocation's span is the range from the first to the last parent-transcript entry whose `uuid` appears in stdout. On real data this isolated the `simplify` step's four spawns from an earlier step's spawn in the same inherited session. With no match, subagent collection is `partial` (`subagent-span-unavailable`). The spec now states this.
- **Alternatives:** Timestamp windows (rejected: fragile). Message-ID bounds (rejected: message IDs matched entries across the whole file).
- **Decision-bearing:** no

## design: transcript location
- **Decision:** Use `CLAUDE_CONFIG_DIR` from the child environment, else `~/.claude`. Look under the encoded working directory first, then glob `projects/*/<session>.jsonl`, because long paths are truncated and hashed. The spec now covers the config override. A failed spawn with no transcript is treated as never started.
- **Alternatives:** Use the encoded working directory only (rejected: misses truncated directories observed in real runs).
- **Decision-bearing:** no

## design: no allocation-scoped cost
- **Decision:** Allocation cost is always unavailable for Claude main-thread and subagent allocations. The `modelUsage` idea is dropped. The `cost-capture` spec is updated to say so.
- **Alternatives:** Per-model `modelUsage` deltas (rejected: session-cumulative, would need new baselines, keys vary with `[1m]`, and cannot separate main from subagent on the same model, which is the common case).
- **Decision-bearing:** no. The full step cost still includes subagent spend.

## design: partial-coverage rule not generalized
- **Decision:** Only `SubagentCollection == partial` turns usage and canonical-total coverage `partial`. Other partial records keep their current treatment. The spec now states this.
- **Alternatives:** Honor every `Completeness: partial` record (rejected: nested Validator projections are always partial, so every run with Validator attempts would flip to `partial`).
- **Decision-bearing:** no

## design: optional ContextualUsageExtractor interface
- **Decision:** Add `ExtractUsageWithContext(rawStdout, UsageContext{Workdir, Env})`, implemented only by Claude and preferred by `extractAgentUsage`.
- **Alternatives:** Change `ExtractUsage`'s signature for all adapters (rejected: churns four unaffected adapters).
- **Decision-bearing:** no

## design: allocation granularity
- **Decision:** Record one allocation per subagent per model. This supersedes the proposal-time "grouped by model" wording and matches the spec's per-subagent unavailable allocation.
- **Alternatives:** Group allocations by model (rejected: cannot represent one missing subagent explicitly).
- **Decision-bearing:** no

## design: native measurement schema version 2
- **Decision:** Bump `native_measurement_schema_version` to 2, adding `allocations` and allowing attempt tokens that include subagents. Existing v1 measurements are retained unchanged.
- **Alternatives:** Keep version 1 with an optional field (rejected: the meaning of the tokens changes silently).
- **Decision-bearing:** no

## test-plan: automated obligations
- **Decision:** Two integration tests that cross the executor, filesystem, and collector boundary (INT-001 wiring, INT-002 partial coverage including the Validator non-regression), and one built-binary E2E with a fake `claude` covering inherited-session attribution, a spawn missing from stdout, and the glob fallback (E2E-001). Per-case extraction logic stays in unit tests.
- **Alternatives:** An E2E test for every failure mode (rejected: duplicates cheaper unit and integration coverage). No E2E (rejected: inherited-session double counting only shows up across real step sequencing).
- **Decision-bearing:** no

## test-plan: acceptance envelope
- **Decision:** Authorize at most three small real headless Claude runs through `./dev.sh`, using the existing login, under $2 in total. Historical artifacts and transcripts are read-only. Factory runs, pushes, and edits to `~/.claude` are off limits. The fake `claude` plus offline replay is the permitted substitute.
- **Alternatives:** No real Claude runs (rejected: the transcript format is undocumented, so real evidence has high value at negligible cost). An unbounded budget (rejected).
- **Decision-bearing:** no

## test-plan: HT-001 real factory /simplify run
- **Decision:** The issue's real-factory acceptance check is a human-only post-merge obligation, because it needs an operator-triggered factory run with the merged build, which is outside this repository.
- **Alternatives:** Have the acceptance pass trigger a factory run (rejected: outside repository authority and budget).
- **Decision-bearing:** no

## approach-review: AR-1 invalid parent-transcript entries (applied)
- **Decision:** An unparseable or truncated line, or an undecodable or unrecognized `tool_use` entry, inside the span (or immediately after its last matched entry) makes subagent collection `partial` with `subagent-parent-transcript-invalid`, while valid-line spawns and usage are kept. Invalid lines outside the span are ignored. A fixed precedence decides which reason is recorded when several apply. Added spec scenarios, `parent-invalid-line` and `parent-invalid-tail` extractor fixtures, and an INT-002 sub-case that asserts the subtotal and `partial` coverage in the artifact.
- **Alternatives:** Treat any parent-transcript parse error as unavailable for the whole step (rejected: discards known usage). Skip bad lines silently (rejected: can hide a spawn while still reporting complete).
- **Decision-bearing:** no

## approach-review: AR-2 step-level collection reason (applied)
- **Decision:** Add `UsageRecord.SubagentCollectionReason` and the native attempt-level `subagent_collection: {completeness, reason}`, and use the specific reason on partial token fields instead of the generic `subagent_collection_partial`. Added a run-metrics spec scenario for the no-allocation case, plus a `span-unavailable` fixture assertion and an INT-002 sub-case.
- **Alternatives:** Reuse `UsageRecord.Reason` for collected-but-partial records (rejected: that field means "unavailable" across existing consumers).
- **Decision-bearing:** no

## approach-review: AR-3 per-allocation cost evidence (applied)
- **Decision:** Every native allocation, main and subagent, carries a `cost` value that is `unavailable` with reason `allocation_cost_not_reported`. The measurement-level `allocation_cost_unavailable` limitation is dropped, and the full USD cost stays at attempt scope. Added a cost-capture scenario for the main allocation and assertions in the design tests and INT-002.
- **Alternatives:** Keep a measurement-level limitation and relax the spec (rejected: it does not identify each allocation's cost state).
- **Decision-bearing:** no

## tasks: single implementation task
- **Decision:** `tasks.md` contains one task that links every definition artifact and requires TDD, all spec scenarios, the INT-001, INT-002, and E2E-001 obligations, and passing `make fmt`, `make test`, and `make lint`. HT-001 is a post-merge human check and is not an implementation task.
- **Alternatives:** Split into extractor, model, collector, and E2E tasks (rejected: the step instructions require exactly one task).
- **Decision-bearing:** no
