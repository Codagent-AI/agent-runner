# Decisions

## propose: verdict
- **Decision**: Go. Context size data already reaches Runner through sources it parses (Claude stream-json and transcripts, Claude statusline, Codex rollouts); the change is additive to `run-metrics.json`.
- **Alternatives considered**: No-go (answer Paul's question with "estimate from cached-input totals"); rejected because totals mix request count and request size and cannot show context growth.
- **Decision-bearing**: yes

## propose: definition of context size
- **Decision**: Context size of a request = uncached input + cache read + cache creation tokens of that model request; output tokens excluded. Matches what Claude's statusline reports as current context usage.
- **Alternatives considered**: Including output tokens; using the CLI's own "percent of window" figure only (not available for most CLIs).
- **Decision-bearing**: yes

## propose: which values per step
- **Decision**: Record end-of-step context (last main-thread request) and peak context in the step, plus the window limit when the CLI reports it. Session rollups expose the maximum end-of-step context per execution session. (Superseded by PR-001, PR-002, PR-003 below.)
- **Alternatives considered**: End-of-step only (misses mid-step compaction); full per-turn history (larger artifact, beyond what the issue asks).
- **Decision-bearing**: yes

## propose: scope of session
- **Decision**: Measure the step's main session only; subagent and nested-tool contexts are excluded and not summed into the parent.
- **Alternatives considered**: Summing subagent contexts; rejected because they are separate context windows, so a sum is not any session's context size.
- **Decision-bearing**: no

## propose: gauge semantics
- **Decision**: Context size is not summed into run totals and bypasses cumulative-delta attribution; each step reports what it observed.
- **Alternatives considered**: Treating it like token counters; rejected because context size is a point-in-time value.
- **Decision-bearing**: no

## propose: CLI coverage
- **Decision**: Claude headless and autonomous-interactive required; Codex via session rollout `token_count` events when locatable, otherwise unavailable; other CLIs unavailable unless their existing output carries per-request usage (design confirms). Unavailable is explicit, never zero or estimated.
- **Alternatives considered**: Claude only (issue names Codex as a known source); tokenizer-based estimation for all CLIs (inaccurate, new dependency).
- **Decision-bearing**: yes

## propose: artifact compatibility
- **Decision**: New fields are optional and additive; not a breaking change to the persisted format. Whether to bump `schema_version` is left to design under the artifact's existing versioning rules.
- **Alternatives considered**: A separate context-size artifact; rejected because the issue asks for it in run metrics and it belongs on the step attempt record.
- **Decision-bearing**: no

## propose: out of scope
- **Decision**: No TUI display, no behavior driven by context size, no backfill of historical artifacts.
- **Alternatives considered**: Showing context size in the run view; deferred because the issue asks only that a run record it.
- **Decision-bearing**: no

## proposal-review: PR-001 (rollup groups by Runner invocation, not agent session)
- **Decision**: Applied. Replaced the execution-session rollup maximum with a run-level agent-session context summary keyed by CLI plus agent `session_id`; execution-session rollups keep their semantics; steps without a session identity stay unattributed.
- **Alternatives considered**: Keep it on `SessionRollup` (wrong grouping per run-metrics-artifact spec); keyed entries inside each execution-session rollup (still splits a resumed agent session).
- **Decision-bearing**: yes

## proposal-review: PR-002 (session summary drops the peak)
- **Decision**: Applied. The session high-water mark is the maximum of per-step peaks; maximum last observed request input is kept as a separate statistic; unavailable or limited step observations make the summary's coverage partial.
- **Alternatives considered**: Maximum end-of-step only (misses pre-compaction peaks, defeating the purpose).
- **Decision-bearing**: yes

## proposal-review: PR-003 (Codex token_count includes compaction and synthetic updates)
- **Decision**: Applied. Added an eligibility contract (attributable main-conversation request usage within the current invocation; compaction and synthetic updates excluded; explicit limitation when indistinguishable), kept a known peak independent of an unavailable final value, renamed the end gauge to "last observed request input", and made Codex coverage conditional on design demonstrating the distinction.
- **Alternatives considered**: Drop Codex from scope (issue names it as a known source; conditional coverage keeps it without risking false values); last-event parsing as originally proposed (can report compaction or synthetic values).
- **Decision-bearing**: yes

## spec: measurement status values
- **Decision**: Per-attempt context measurement has status `available`, `partial`, or `unavailable`; `partial` covers unclassifiable updates and an unmeasurable final observation; a known peak survives an unavailable last value.
- **Alternatives considered**: Binary available/unavailable (loses the known peak PR-003 asked to keep); a separate limitation flag alongside binary status (two fields expressing one state).
- **Decision-bearing**: no

## spec: which executions carry a measurement
- **Decision**: Agent steps and called agents whose CLI was launched carry a measurement. Shell steps, iteration records, and agent steps that never launched carry none and are excluded from context coverage (instead of "unavailable" as the proposal's list implied), since there is no context to miss.
- **Alternatives considered**: Recording shell steps as unavailable (would wrongly reduce coverage); excluding called agents (they are agent CLI sessions the issue's "every session" covers).
- **Decision-bearing**: no

## spec: summary fields and keys
- **Decision**: Summary entries keyed by CLI name plus agent session ID; fields are high-water mark (max peak), max last observed request input, largest reported window limit, attempt count, coverage complete/partial/none; attempts without a session ID go into an unattributed count.
- **Alternatives considered**: Listing unattributed attempts individually (adds no session insight); keying by session ID alone (collides across CLIs).
- **Decision-bearing**: no

## spec: older artifacts on resume
- **Decision**: Attempts recorded before context measurement existed are kept unchanged, treated as unavailable ("not recorded"), and make their session's summary coverage partial. Schema version increment left to design (deferred marker).
- **Alternatives considered**: Ignoring older attempts in the summary (would report complete coverage falsely).
- **Decision-bearing**: no

## spec: Claude headless window limit and duplicate stream entries
- **Decision**: Headless Claude window limit comes from the result event when Claude reports it; repeated stream entries for one assistant message count as one request using its last usage. Interactive window limit comes from the statusline report.
- **Alternatives considered**: Hard-coding window sizes per model (estimation, excluded by the proposal).
- **Decision-bearing**: no

## spec: deferred items
- **Decision**: Codex rollout location and compaction/synthetic discrimination, per-CLI confirmation for Cursor/Copilot/OpenCode/external-user, and schema version bump are marked `deferred-to-design`.
- **Alternatives considered**: Deciding now without design evidence.
- **Decision-bearing**: no

## design: Codex context source
- **Decision**: Use rollout `token_usage_record` lines (one per model response, with `response_id`) as Codex observations; exclude responses named by `compacted.compaction_response_id`; never use `token_count` values (they include synthetic post-compaction updates); a trailing record at end of file is unclassified; rollouts without `token_usage_record` (pre-0.160) are `unavailable` / `codex-rollout-unsupported`. Verified against real rollouts on this machine (Codex 0.160–0.162).
- **Alternatives considered**: `token_count.last_token_usage` with heuristics for zero-input synthetic values (cannot identify compaction responses); dropping Codex (issue names it).
- **Decision-bearing**: yes

## design: Codex rollout location and invocation span
- **Decision**: Locate `<CODEX_HOME or ~/.codex>/sessions/**/rollout-*-<thread_id>.jsonl` after exit (newest date first); attribute lines with `timestamp >= invocation StartedAt` (new `UsageContext.StartedAt`), main thread only (`thread_id == session`). Context size = Codex `input_tokens` (already includes cached input).
- **Alternatives considered**: Pre-launch byte offset (no file exists before a fresh launch); turn IDs (not exposed on stdout).
- **Decision-bearing**: no

## design: carrier and summary placement
- **Decision**: `UsageRecord.Context` (`context`, omitted for non-agent records) carries the measurement through audit events; artifact gains `agent_session_context {sessions[], unattributed_attempts}` recomputed with aggregates. Not added to v4 native measurement heads (contract shared with Agent Validator).
- **Alternatives considered**: New `StepRecord` field with separate audit data key (duplicate plumbing); extending native measurements (cross-repo contract change).
- **Decision-bearing**: no

## design: unsupported CLIs
- **Decision**: Cursor, Copilot, OpenCode, and autonomous-interactive Codex report `unavailable` / `unsupported`. OpenCode has per-step usage but its run output does not mark compaction steps; spec updated to name these CLIs. External-user steps combine per-turn measurements.
- **Alternatives considered**: OpenCode with every update `partial` (permanently unclassified, misleading).
- **Decision-bearing**: no

## design: schema version
- **Decision**: No schema version bump; fields are additive and the versioning requirement bumps only for incompatible changes. Spec scenario added.
- **Alternatives considered**: Bump to v5 (forces a migration stage for no structural change).
- **Decision-bearing**: no

## design: claude window limit and compaction
- **Decision**: Headless window from `result.modelUsage[<main model>].contextWindow`; interactive window from a new `ClaudeStatusReport.ContextWindowSize` captured from the statusline payload's `context_window.context_window_size`. Claude compaction summarization requests are not main-thread assistant events, so no extra exclusion logic; pinned by a `compact_boundary` fixture.
- **Alternatives considered**: Per-model window table (estimation).
- **Decision-bearing**: no

## test-plan: obligations and envelope
- **Decision**: Two integration obligations (Codex rollout through the private Codex home; artifact over resume with legacy records), two CI E2E tests through the built binary with fake CLIs (headless Claude+Codex+shell workflow; extension of the existing interactive Claude usage E2E), and context assertions added to the existing opt-in real-agent headless tests to catch real format drift. No human-only testing. Acceptance may spend under $1 on small real headless runs; `~/.claude`, `~/.codex/sessions`, factory artifacts, and GitHub are off limits for writes.
- **Alternatives considered**: A new real-agent CI job (forbidden by repo rules); fake-only coverage (cannot detect drift in undocumented Codex rollout records); a human interactive check (agent can verify via E2E-002 and real transcript copies).
- **Decision-bearing**: no

## approach-review: AR-001 (external-user merge cannot reuse the observation builder)
- **Decision**: Applied. Added a separate `mergeContext` reducer for external-user turns: max of every non-null turn peak (including partial turns), final turn's last, status/reason from all turns, window from the last non-null turn; independent of the counter merge and carried unchanged through `attributeTurns`. Added the reviewer's three test cases.
- **Alternatives considered**: Feeding turn summaries into `buildContext` (cannot hold separate peak and last; drops partial-turn peaks).
- **Decision-bearing**: no

## approach-review: AR-002 (excluded final observation nulled the last value)
- **Decision**: Applied. Adapters drop positively excluded updates before the builder; the builder only sees eligible or unclassified observations; only an unclassified final observation or explicit missing-final evidence nulls last. Matches the spec's synthetic-exclusion scenarios and the existing interactive collector's synthetic skip. Added tests for both endings.
- **Alternatives considered**: Builder-side exclusion flag (equivalent, but one more state for every adapter to set correctly).
- **Decision-bearing**: no

## approach-review: AR-003 (real-agent tests not isolated; make target too broad)
- **Decision**: Applied. E2E-003 and acceptance real runs use `isolatedRealAgentCallEnv`-style isolation (temp `HOME`, temp `CODEX_HOME` with copied `auth.json`); execution scoped to `TestClaudeHeadlessRealAgentE2E` and `TestCodexHeadlessRealAgentE2E` via `-run`; skip with reason and rely on fake coverage when a CLI cannot authenticate under isolation.
- **Alternatives considered**: Relaxing the off-limits rule to allow writes to real session stores (pollutes the user's history); dropping E2E-003 (loses the only live-format check).
- **Decision-bearing**: no

## tasks: single implementation task
- **Decision**: One task covering model, builders, Claude headless and interactive, Codex rollout, other adapters, collector summary, and every INT/E2E obligation, with `design.md` as the technical source of truth and `make fmt`/`lint`/`test` plus strict OpenSpec validation as done criteria.
- **Alternatives considered**: Per-component tasks (the step requires exactly one task).
- **Decision-bearing**: no
