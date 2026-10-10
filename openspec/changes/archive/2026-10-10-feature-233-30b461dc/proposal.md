## Why

Autonomous-interactive agent steps record no usage. When the autonomous backend is `interactive`
or `interactive-claude`, Agent Runner hands the terminal to the agent CLI and captures no stdout.
`extractAgentUsage` in `internal/exec/agent.go` therefore records every non-headless step as
`{"status": "unavailable", "reason": "interactive-context"}`, and the step's
`estimated_api_cost_usd` is null.

These are often the most expensive steps in a run. The `orchestrate-implementation` step in
`orchestrated-implement-change` (PR #209) runs with `autonomous_backend: interactive`. In run
`orchestrate-change-2026-10-09T02-36-12-193439Z` (agent-evals), its lead session ran for about
1h50m and spawned many sub-agents. All of that spend was missing from `run-metrics.json`. The
run-level cost coverage was `partial`, and its total cost and tokens undercounted the run by what
was probably its largest step. Cost analysis of orchestrated workflows cannot be trusted while
this gap exists.

The gap is in the spec, not a bug. `agent-usage-collection` ("Unavailable usage is explicit",
scenario "Interactive agent step reports unavailable") requires `interactive-context` for both
interactive and autonomous-interactive steps, and `claude-subagent-usage` forbids scanning
subagent transcripts for them. That is why the earlier issues about this behavior (#125 and #126,
about the old `pty-context` reason) were closed as matching the spec. This change revises those
requirements.

The data now exists. Agent Runner already reads Claude Code's session transcripts
(`<config>/projects/<project>/<session-id>.jsonl` and `<session-id>/subagents/`) to collect
subagent usage for headless steps. A current transcript records each assistant message's model
and its full `usage` object (input, cache-read, cache-write, and output tokens). Agent Runner also
knows an autonomous-interactive Claude step's session ID before launch, because it passes
`--session-id` or `--resume`.

**Verdict: go with caveats.**

- **Tokens:** a clear go. The transcript source, the parser, the dedupe-by-message-ID rule, and
  the allocation model all exist already. Most of the work is reusing them with a different way to
  find the invocation's span.
- **Cost is the caveat.** Transcripts record tokens but no USD cost, and `cost-capture` forbids
  computing cost from tokens. A step's cost is collected only through a separate channel in which
  Claude Code reports its own cost (see Technical Approach). That channel has not yet been shown
  to deliver a final value within Runner's completion lifecycle. Design must therefore start with
  a real-Claude feasibility check, before the cost architecture is committed. If the check fails,
  the design step stops for a direction decision instead of shipping a cost path that is null
  most of the time. Cost is null whenever the channel cannot give a trustworthy value. Token
  collection does not depend on it.

## What Changes

- **Main-thread usage for autonomous-interactive Claude steps.**
  - After the CLI exits, Agent Runner reads the step's session transcript.
  - It sums usage from the assistant messages that this invocation appended. Each message ID is
    counted once, using its final recorded usage.
  - It records the observed model or models from those messages.
  - The result is a normal usage record from a new measurement source. It is no longer
    `interactive-context`.
- **Invocation span by transcript position.** There is no stdout to bound the span, so Agent
  Runner records the parent transcript's position before launch. Entries written after that
  position belong to the invocation. This keeps a session that is shared through `inherit` or
  `resume` from being counted twice. If the span cannot be established (for example, the file is
  missing, shrank, or the session ID changed during the step), the record keeps any known usage and
  is `partial` or unavailable with a reason. It is never zero.
- **Subagents included.** The existing `claude-subagent-usage` collection is extended to
  autonomous-interactive Claude steps:
  - spawns are found in the invocation's transcript span instead of in stdout;
  - nested subagents, partial evidence, and still-running subagents are handled under the same
    rules as headless steps;
  - the main thread and each subagent remain separate allocations, and attempt totals count each
    token once.
- **Cost from Claude's own report.**
  - Agent Runner injects a status-line command into the settings it already passes to Claude
    with `--settings` for the completion Stop hook. Claude Code passes that command a payload
    that includes its own `cost.total_cost_usd`.
  - The command records the payload in a run-owned location. It then runs the user's configured
    status line, if there is one, so its content looks the same. When the user has none, Claude's
    footer keyboard hints are hidden during the step (see `design.md`).
  - The step's `estimated_api_cost_usd` is the reported value with no price math, attributed
    against the process's own first report. The design's feasibility check showed the counter
    is process-scoped.
  - Completion waits a bounded time for a payload that is causally tied to the final accounted
    turn. Cost is null if that wait times out, or if no payload was recorded, the payload is stale
    or belongs to another session, or no baseline can be trusted. A cost timeout never delays
    completion beyond its bound or changes the step's outcome.
  - This bullet is conditional on the feasibility gate in Technical Approach.
- **Run-level aggregates** include these steps. Token and cost coverage become `complete` when
  every step was collected.
- Autonomous-interactive steps on other CLIs (Codex, Cursor, Copilot, OpenCode) and
  human-interactive steps on any CLI still report `interactive-context`.

No **BREAKING** changes. `run-metrics.json` uses its existing usage, allocation, and cost
vocabulary and gains a new measurement-source value. For affected steps, `unavailable` records
become available or partial ones.

## Capabilities

### New Capabilities
- `interactive-claude-usage`: how autonomous-interactive Claude steps get usage from session
  transcripts. Covers establishing the invocation span by transcript position, which entries
  count, behavior when the span or transcript is unusable, and capturing Claude-reported cost
  through the injected status line, including baseline attribution and staleness.

### Modified Capabilities
- `agent-usage-collection`: autonomous-interactive Claude steps are no longer an
  `interactive-context` case. `interactive-context` remains for human-interactive steps and for
  autonomous-interactive steps on CLIs that have no transcript collector.
- `claude-subagent-usage`: subagent collection applies to autonomous-interactive Claude steps,
  with spawns discovered from the transcript span instead of stdout.
- `cost-capture`: Claude-reported cost may come from the status-line payload as well as from
  the headless `result` event. It is still verbatim and attributed by delta, with no price math.
- `run-metrics-artifact`: partial main-thread transcript collection for autonomous-interactive
  Claude steps makes usage and canonical-total coverage partial, as partial subagent collection
  already does.

## Technical Approach

- **Reuse the subagent transcript machinery.** `internal/cli/claude_subagents.go` already locates
  transcripts (honoring `CLAUDE_CONFIG_DIR`, `HOME`, and shortened project directories), reads
  them through `os.Root`, deduplicates streamed messages, and builds allocations. The new path
  adds:
  - a main-thread reader for the parent transcript;
  - a span definition based on a byte position captured before launch, instead of stdout UUIDs.

  This stays inside the Claude adapter behind the existing `ContextualUsageExtractor`.
  `extractAgentUsage` stops short-circuiting autonomous-interactive steps whose adapter supports
  transcript collection. All other adapters keep the current fallback.
- **Pre-launch snapshot.** `UsageContext` gains the information needed to bound the span. The
  executor records it before it hands off the terminal. A new session (`--session-id`) starts
  with an empty span. A resumed session starts at the file's current end.
- **Per-message usage, not cumulative.** Transcript usage is reported per message. It is
  attributed directly, with no baseline, under "Attribution follows source counter semantics".
- **Cost via status line.** Claude Code's status-line payload is the only Claude-reported USD
  cost available in an interactive session. Hook payloads carry none, and transcripts carry none.
  The payload also carries `session_id` and `transcript_path`, which can confirm the span's
  session.

  **Feasibility gate (before design commits to this channel).** The final payload is not yet
  shown to be capturable:
  - `finishDirectCompletion` (`internal/interactive/runner.go`) waits only for turn durability.
    That is a committed final assistant record (`WaitForCommittedTurn` in
    `internal/cli/durability.go`), not cost readiness. It then terminates the CLI immediately.
  - Claude's status-line updates are asynchronous and debounced, and an in-flight command can be
    cancelled.
  - A recorder write after the Stop hook does not prove that the payload included the final
    turn's usage.

  The first design task is therefore a small real-Claude check through Runner's actual
  completion path. It covers a fresh session and a resumed session, both with subagents, and
  establishes:
  - **Causal freshness.** Some payload field must tie the payload to the final accounted turn,
    for example by matching the payload's reported token or context usage against the final
    transcript message. Write time alone does not count.
  - **Counter scope across `--resume`.** Whether `total_cost_usd` carries prior process cost. If
    it is still unknown, the existing no-baseline rule yields null.
  - **A bounded collection window.** Completion waits a short, bounded time between durability
    and `Terminate` for a fresh payload, and cost is null on timeout. Completion reliability
    comes first.

  If the check shows that a fresh final payload is not reliably obtainable, design stops for a
  direction decision. The likely outcome is to ship transcript tokens in this change and track
  cost as a follow-up. That would depart from the issue's request for cost, so it is not decided
  here. Remaining design risks:
  - **Cumulative semantics across `--resume`.** Whether `total_cost_usd` carries prior process
    cost determines the baseline. When it is unknown, the existing no-baseline rule yields null.
  - **Preserving the user's status line.** The user's command must be resolved from Claude's
    settings layers and delegated to. Managed settings that override `statusLine` simply leave
    cost null.

  The alternatives were rejected:
  - a pricing catalog contradicts `cost-capture`;
  - an OpenTelemetry exporter would mean running an OTLP receiver and overriding the user's
    telemetry environment;
  - shipping tokens without cost would leave the issue's cost gap open.
- **Timing.** Transcripts are read after the CLI exits, as for headless steps. A subagent that is
  still writing is recorded as partial and is not waited for.

## Out of Scope

- Transcript-based usage for autonomous-interactive steps on other CLIs (for example, Codex
  rollout files). They keep `interactive-context` and are candidates for follow-up issues.
- Human-driven `interactive` steps. Users can switch or clear sessions in those, so attribution is
  less reliable. They keep `interactive-context`.
- A pricing catalog, or any USD computed from tokens.
- Lifting the rule that output capture forces headless execution (the TODO in
  `resolveAutonomousInvocationContext`).
- Backfilling historical `run-metrics.json` files.
- New TUI or report layouts. Existing views show the newly available values.

## Impact

- **Code:**
  - `internal/cli/claude_subagents.go` and `claude.go`: main-thread transcript reader, span by
    position, status-line injection and recording, and cost attribution.
  - `internal/cli/adapter.go`: `UsageContext` fields.
  - `internal/exec/agent.go`: the pre-launch snapshot and the `extractAgentUsage` gating.
  - Possibly a small `agent-runner` internal subcommand for the status-line recorder, alongside
    the existing completion hook command.
  - `internal/metrics`: only if provenance or measurement-source mapping needs it.
- **Tests:**
  - transcript fixtures for new and resumed sessions with and without subagents;
  - an inherited session shared by two steps, checked for no double counting;
  - span failures (missing or truncated transcript, changed session);
  - status-line payloads that are fresh, stale, from another session, or missing;
  - an `extractAgentUsage` test proving that non-Claude interactive steps keep
    `interactive-context`.
- **Data:** autonomous-interactive Claude steps in `run-metrics.json` show tokens and
  allocations. They show cost when the feasibility gate passes and the bounded window captures a
  fresh payload. Run-level totals and coverage rise accordingly.
- **User-visible:** the status line Claude shows during autonomous-interactive steps runs through
  Agent Runner's recorder but should look the same.
- **Acceptance:** the cost-channel feasibility check ran before implementation, through Runner's
  real autonomous-interactive completion path with real Claude, on fresh and resumed sessions with
  subagents (see `design.md`).
  - Before merge, the acceptance pass repeats that run with the built feature and must show
    non-null final cost.
  - A human real-terminal run of an `orchestrated-implement-change` step remains only to judge
    the hidden-footer-hint trade-off and confirm end-to-end behavior.
