## Context

Autonomous-interactive Claude steps hand the terminal to `claude` through the direct interactive
runner (`internal/interactive/runner.go`, called from `runAgentProcess` in `internal/exec/agent.go`).
No stdout is captured, so `extractAgentUsage` short-circuits on `!invocationContext.IsHeadless()` and
records `interactive-context` with no cost.

Pieces that already exist and are reused:

- **Transcript reading.** `internal/cli/claude_subagents.go` holds `claudeTranscriptPaths`, which
  locates `<config>/projects/<project>/<session>.jsonl` while honoring `CLAUDE_CONFIG_DIR`, `HOME`,
  and shortened project directories, and reports ambiguity. The same file holds
  `scanClaudeParentSpan` (spawns in a span), `collectClaudeSubagents` and `readClaudeSubagent`
  (deduplicated subagent usage, nested spawns, partial reasons), and `sumClaudeAllocations`
  (attempt totals over allocations).
- **Checkpoints.** `internal/cli/durability.go` has `fileCheckpoint`, which returns the offset just
  past the last complete JSONL line, and the Claude `TurnDurabilityProbe`.
- **Completion.** `finishDirectCompletion` waits for turn durability, which is a committed final
  assistant record, and then calls `supervisor.Terminate`.
- **Settings injection.** `ClaudeAdapter.BuildArgsWithError` already passes `--settings` JSON
  containing a `Stop` hook (`agent-runner internal turn-committed`) for autonomous-interactive
  completion.
- **Cost attribution.** `metrics.Collector.attributeCost` turns `RawCumulativeCostUSD` into a delta
  per session. A record without it clears the session's baseline and keeps the event's own
  `estimated_api_cost_usd`.
- **Environment.** Runner already drops `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, and
  `CLAUDE_CODE_ENTRYPOINT` from the child environment. The feasibility check confirmed this matters:
  with those variables inherited, Claude 2.1.296 writes no session transcript at all.

### Feasibility gate result (required by the proposal)

The gate ran on 2026-10-10 against real Claude Code 2.1.296 (`claude-haiku-5-5`) in two stages.

**Stage 1: through Runner's actual lifecycle (decisive).** A locally built `agent-runner` ran a
two-step workflow under a PTY. The workflow had a `session: new` step and then a `session: resume`
step, each instructed to spawn one Task subagent and then complete. The profile used
`default_mode: autonomous`, and Runner's settings set `autonomous_backend: interactive-claude`.

- The run used a temporary `HOME`, with `~/.claude` and `~/Library` symlinked so that Claude could
  authenticate. The user's global `~/.agent-runner/settings.yaml` was not touched.
- A project `.claude/settings.json` installed a recording status-line command. Runner's own
  `--settings` supplied only the completion `Stop` hook, so Runner's control, durability, and
  termination path ran unmodified. The audit log shows `completion_requested`,
  `completion_acknowledged`, `turn_committed`, and terminal reclaim for both steps.
- The same workflow also ran once in human-interactive mode, through the same `DirectRunner` path,
  with the same results.

**Stage 2:** plain `claude` runs (fresh, and then `--resume`) with hook instrumentation.

| Question | Observation | Consequence |
|---|---|---|
| Does a final payload arrive inside Runner's lifecycle? | Yes, in all four Runner-driven steps. The payload whose `context_window.current_usage` equals the final main-thread transcript message arrived 0.33–0.46 s after `turn_committed`. Runner terminated the CLI about 1.0–1.15 s after `turn_committed`. In the plain runs, it arrived 0.30–0.32 s after the final `Stop` hook. | Final cost is obtainable through Runner's real path. The bounded wait adds margin; it is not what makes cost possible. |
| Is the payload causally tied to the final turn? | Every payload's `current_usage` equals a specific API response's usage. Intermediate streaming payloads also exist: the same message is reported first with a partial output count and later with its final one. | Freshness is proven by matching the final tuple. Intermediate payloads never match a final tuple unless the counts coincide. |
| Counter scope across `--resume` | The scope is not stable. In both Runner-driven resumed steps, the resumed process's startup payload carried the previous process's final cost (0.01179 and 0.01586). In the plain `--resume` run, it started at 0. | A per-process baseline is mandatory. Neither "starts at 0" nor "session-cumulative" can be assumed. |
| What proves that a report predates the step's work? | Startup payloads (fresh and resumed) carried no `prompt_id`. Every payload after the step's prompt carried a `prompt_id` equal to the `promptId` of the user entries in the transcript. | `prompt_id` is the positive baseline witness (AR-001). |
| Does it include subagents? | Yes. In the fresh Runner step, the final aggregate (0.01179) equaled the next process's carried startup value, and that step's cost jumps line up with the subagent's work. Subagents ran in the background (`isAsync: true`, `status: "async_launched"`). Their completion is recorded in the parent transcript as `<task-notification>` content with `<tool-use-id>` and `<status>completed</status>`, both in an `attachment` of type `queued_command` and in `queue-operation` entries. Each notification was written before the main-thread message that consumed it. | Subagent completion is provable from the parent transcript (AR-002 and AR-003). |
| Session identity | Every payload carried the step's `session_id`. | Payloads can confirm or contradict the span's session. |

**Environment note.** If `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, or `CLAUDE_CODE_ENTRYPOINT` are
inherited, Claude writes no session transcript. Runner already strips these variables, and the
acceptance pass must as well.

**Side effect confirmed in Claude's docs.** When any `statusLine` is configured, Claude hides most
footer keyboard hints, such as `esc to interrupt`. Injecting a recorder therefore hides those hints
for users who have no status line of their own. See Decisions.

## Goals / Non-Goals

**Goals:**
- Available main-thread and subagent token usage for autonomous-interactive Claude steps, from the
  session transcript, with explicit partial and unavailable states.
- Claude-reported USD cost for those steps when a payload provably reflects the final turn, and
  null otherwise.
- No change to step outcomes, no unbounded delay to completion, and no visible change to a user's
  configured status line.

**Non-Goals:**
- Other CLIs, human-interactive steps, pricing math, historical backfill, and new TUI layouts.
- A schema version bump for `run-metrics.json`. All changes are additive enum values or the
  existing fields.

## Approach

```
exec.ExecuteAgentStep (autonomous-interactive, claude)
  ├─ before launch: adapter.PrepareInteractiveUsage(session, workdir, env)
  │     → InteractiveUsagePlan{TranscriptPath?, StartOffset, ReportPath, StatusLine settings}
  ├─ BuildArgs: --settings {hooks.Stop, statusLine: recorder(+delegate)}
  ├─ interactive.DirectRunner.Run
  │     └─ finishDirectCompletion: durability ✓ → WaitForFinalReport(bound) → Terminate
  └─ after exit: adapter.ExtractInteractiveUsage(plan, usageContext)
        → main allocation (transcript span) + subagent allocations + cost
```

### 1. Adapter capability (`internal/cli`)

Add an optional interface beside `ContextualUsageExtractor`:

```go
type InteractiveUsageCollector interface {
    PrepareInteractiveUsage(sessionID string, resume bool, uc UsageContext) (InteractiveUsagePlan, error)
    WaitForFinalReport(ctx context.Context, plan InteractiveUsagePlan) // bounded by ctx
    ExtractInteractiveUsage(plan InteractiveUsagePlan, uc UsageContext) UsageExtraction
}
```

`InteractiveUsagePlan` is plain data:

- `SessionID`;
- `TranscriptPath`, set when the transcript already exists;
- `StartOffset`;
- `ReportPath`;
- `ReportEnabled`, plus a reason when it is disabled;
- `PrepareErr`, the span-preparation reason.

Only `ClaudeAdapter` implements the interface. `extractAgentUsage` now works as follows:

- For a non-headless context, if the context is `ContextAutonomousInteractive` and the adapter
  implements `InteractiveUsageCollector`, return `ExtractInteractiveUsage(...)`.
- Otherwise keep `defaultAgentUsage` with `interactive-context`.

Human-interactive (`ContextInteractive`) steps never reach the collector.

### 2. Span start (before launch)

`PrepareInteractiveUsage` resolves the transcript with `claudeTranscriptPaths(session, uc)`:

- **Exists:** `StartOffset = fileCheckpoint(path).Offset`, and `TranscriptPath` is recorded.
- **Not found:** a fresh `--session-id` or a session not yet written. `StartOffset = 0`, and the path
  is resolved again after exit.
- **Ambiguous:** `PrepareErr = transcript-ambiguous`.

The plan is computed in `internal/exec` immediately before `runAgentProcess` and threaded through
`directInvocation` so that the direct runner can call `WaitForFinalReport`.

### 3. Main-thread transcript usage (after exit)

`ExtractInteractiveUsage` takes these steps:

1. **Resolve the transcript.**
   - Resolve it again. If the plan had a path and the new one differs, or `size < StartOffset`, the
     main allocation is unavailable with `transcript-span-unavailable`.
   - If no transcript exists, the reason is `transcript-missing`.
   - If the location is ambiguous, the reason is `transcript-ambiguous`.
2. **Stream the span** from `StartOffset` to EOF. For each line:
   - **Invalid JSON:** note `transcript-invalid` and continue.
   - **Main-thread assistant entry:** an entry with `type == "assistant"` and `isSidechain` false
     or absent. Keep the last usage per `message.id` (the existing dedupe rule) along with
     `message.model`. A message without `usage` notes `no-usage-event` (partial).
   - **Task tool uses:** collect them for spawn discovery, using the same parsing as
     `claudeTools`, so unrecognized shapes mark the span invalid exactly as
     `scanClaudeParentSpan` does.
3. **Build the main allocation.**
   - Sum the deduplicated usage into a main allocation with `Source: "claude:session-transcript"`
     and `claudeTokenTotals`.
   - Several models stay distinct: the main allocation is split per model, and the observed
     identities list each one.
   - A span with zero main-thread assistant messages makes the main allocation unavailable with
     `no-usage-event`, never zero.
4. **Collect subagents with transcript-derived lifecycle.** There is no stdout, so the running set
   cannot come from `task_started` or `task_notification` stream events as it does for headless
   steps. Instead, the scan records completion evidence for each spawn while it reads the span (and,
   for nested spawns, the spawning subagent's transcript):
   - **Foreground spawn:** a `tool_result` for the spawn's tool-use ID that is not an async-launch
     acknowledgement. An acknowledgement has `toolUseResult.isAsync == true` or
     `status == "async_launched"`.
   - **Background spawn:** a `<task-notification>` whose `<tool-use-id>` equals the spawn's ID and
     whose `<status>` is terminal (`completed`, `failed`, or `killed`). It may appear in an
     `attachment` with `type == "queued_command"` (its `prompt`) or in a `queue-operation` entry
     (its `content`).
     - **Unknown statuses** are not treated as terminal.
     - **Re-activation:** a notification fires each time an agent stops, and an agent can be
       resumed with SendMessage. The proof therefore uses the last terminal notification, and it
       holds only if the subagent transcript has no assistant entry timestamped after that
       notification.
   - Lines whose types the scan does not recognize (for example `queue-operation` and
     `last-prompt`) are skipped. They do not mark the span invalid, as long as they contain no
     tool use.
   - A spawn without completion evidence goes into the `running` set passed to
     `collectClaudeSubagents`, which records the known usage and marks the allocation
     `subagent-still-running` (partial), as it does today. This applies at every nesting depth.

   Each spawn also gets a **settled position**: the transcript position of its completion evidence,
   projected to the top-level span. A nested spawn settles at its top-level ancestor's evidence, and
   only if all its descendants are proven complete. Cost eligibility uses this position.
5. **Total the attempt.** Call `sumClaudeAllocations`. Any noted reason makes `Completeness`
   `partial`. Main-thread reasons are recorded on the main allocation, and subagent reasons go to
   `SubagentCollection` and `SubagentCollectionReason` as today. When the main allocation is
   unavailable and no subagent tokens exist, the record is `UsageUnavailable` with the main reason.
6. **Leave the cumulative cost empty.** `RawCumulativeCostUSD` stays nil, because transcript usage
   is per-message. As a result, the collector clears the session's cumulative-cost baseline, which
   is the existing conservative behavior for invocations that report no cumulative cost.

To share span scanning, refactor `scanClaudeParentSpan` to take a span selector: the existing UUID
bounds for headless steps, or `[StartOffset, EOF)` for interactive steps. Headless behavior must not
change.

### 4. Status-line recorder (cost channel)

**Injection.** When `PrepareInteractiveUsage` succeeds, `BuildArgsWithError` merges a `statusLine`
into the same `--settings` object as the `Stop` hook:

- With a user status line, Runner copies the user's `statusLine` object and keeps `padding`,
  `refreshInterval`, `hideVimModeIndicator`, and any other fields. Only `command` is replaced, with
  `'<agent-runner>' internal statusline-record --report '<ReportPath>' --delegate '<original command>'`.
- With no user status line: `{"type":"command","command":"'<agent-runner>' internal statusline-record --report '<ReportPath>'"}`.

Runner's `--settings` outranks every layer except managed settings, so Runner must resolve the
`statusLine` Claude would otherwise load. It follows Claude's documented locations: the "Where
Claude Code keeps the local file in a git repository" rules, valid since v2.1.211. The candidate
layers, highest first, are:

1. **Project local, root file.** In a git repository, `<root>/.claude/settings.local.json`, where
   `<root>` is:
   - the main checkout's root for a linked worktree (the parent of `git rev-parse
     --git-common-dir`);
   - otherwise the repository top level (`git rev-parse --show-toplevel`).

   The file stays at the working directory instead (Claude's exceptions) when:
   - the working directory is outside a git repository;
   - the root is the user's home directory;
   - the root, or its `.git` or `.claude` entry, isn't owned by the current user.
2. **Project local, legacy file.** `<workdir>/.claude/settings.local.json`, when it differs from the
   root file. Claude still reads it, and the root file's value wins for the same key.
3. **Shared project.** `<workdir>/.claude/settings.json`. Claude reads it from the primary working
   directory, not the repository root.
4. **User.** `<CLAUDE_CONFIG_DIR or HOME/.claude>/settings.json`, using the step's effective
   environment.

The first layer that defines `statusLine` wins, and its whole object is copied. Capture is disabled
(`ReportEnabled=false`, reason `cost-report-unavailable`) when:

- any candidate file exists but cannot be parsed;
- a `git` query fails inside what appears to be a repository;
- ownership cannot be determined.

That leaves the user's display untouched, and cost is null. Runner never guesses when it cannot
resolve safely.

Managed settings outrank `--settings`. If they define a status line, no payloads arrive and cost is
null. The same happens when `disableAllHooks` disables status lines.

**Recorder subcommand** (`cmd/agent-runner`, `internal statusline-record`):

1. Read stdin (the payload).
2. Immediately append one compact JSON line to `--report` with a single `O_APPEND` write:
   `{"recorded_at": ..., "session_id", "prompt_id", "total_cost_usd", "current_usage"}`. The line
   holds only these fields; paths, git data, and rate limits are not persisted. Writing before delegation
   matters because Claude cancels an in-flight status-line command when a new update arrives.
3. If `--delegate` is given, run it with `sh -c`, with the same stdin bytes and environment
   (Claude sets `COLUMNS` and `LINES`). Copy its stdout to stdout and exit with its code. Kill its
   process group if the recorder receives SIGTERM or SIGINT.
4. Without a delegate, print nothing and exit 0.

Errors writing the report are ignored for display purposes: delegation still runs.

`ReportPath` is a run-owned file, `<run state dir>/usage/<audit-prefix>-<attempt>.statusline.jsonl`,
which is created empty, with mode 0600, before launch.

**Bounded final wait.** In `finishDirectCompletion`, after `AwaitTurnDurability` returns success and
before `supervisor.Terminate`, the runner calls `WaitForFinalReport` with a context bounded by
`DefaultFinalReportTimeout = 3s` (an option on `DirectOptions`). It returns as soon as:

- the transcript's current final main-thread assistant message, scanned from `StartOffset`, has a
  usage tuple equal to the `current_usage` of some report line recorded after that message's
  first appearance;

or when the context expires.

The wait runs only when the plan has reporting enabled. It never changes `DirectResult` or the
outcome, and it is skipped on cancellation or failed durability. The observed latency is about
0.3–0.46 s, so 3 s leaves a wide margin while capping the worst-case added completion time.

**Cost computation** happens in `ExtractInteractiveUsage`, after exit, using the final transcript
and all report lines. Each failed check yields a null cost with the reason in parentheses.

1. **Final message (`F`).** Let `F` be the final main-thread assistant message in the span at
   collection time, with its last recorded usage.
2. **Final report (`P`).** Let `P` be the last report line whose `current_usage` equals `F`'s usage
   tuple (input, cache-write, cache-read, output). (`cost-report-stale`)
3. **Baseline (`B`).** `B` must be proven to predate the step's input. That requires all of the
   following (`cost-baseline-missing`):
   - `B` is the first report line recorded for this attempt;
   - `B` has no `prompt_id`;
   - some later report line carries a `prompt_id`, which proves this Claude version supplies the
     field;
   - the first `prompt_id` reported equals the `promptId` of the first user entry in the span,
     which proves that the first prompt-bearing report belongs to the step's own prompt.

   As a result, a lost or cancelled startup report, a first report taken after compaction (null
   `current_usage`), and a first report taken mid-stream all produce a null cost, never an
   undercounted delta. A null `current_usage` or a tuple that matches nothing is not accepted as
   evidence.
4. **Subagents settled (AR-003).** Every attributed subagent, at every depth, must have a settled
   position (§3, step 4) before `F` in the span. That means its last activity ended before the
   request that produced `F`, so the report matching `F` already includes its spend.
   (`cost-subagent-unsettled`) This covers background subagents that finish, or resume and
   continue, after `F`, while the status line stays quiet.
5. **Session.** Every report line must have `session_id` equal to the step's session.
   (`cost-session-mismatch`) Main-thread usage also becomes `partial` with `session-switched`,
   because a `/clear` or session switch moved work out of the transcript being read.
6. **Cost.** The cost is `P.total_cost_usd − B.total_cost_usd`. (`counter-reset` if negative or
   either value is missing)
7. **Output.** The result goes to `UsageExtraction.EstimatedCostUSD`. When the cost is null, the
   reason is emitted as `cost_unavailable_reason` in the step-end audit event data.
   `run-metrics.json` has no step-level cost-reason field, and none is added.

Allocation cost stays unavailable, as required by `cost-capture`.

### 5. Metrics and coverage (`internal/metrics`)

- `totalsForRecords` and the native projection already treat `SubagentCollection == partial` as
  partial coverage. Extend that check so a record whose main allocation is `partial` or
  unavailable, while it still has a known subtotal (the interactive transcript source), also counts
  as partial. Detect it with `Source == "claude:session-transcript" && Completeness == partial`,
  not by a generic `Completeness`, so that other partial records keep their existing treatment
  (spec: "Other partial records keep existing coverage").
- Cost coverage needs no change. A step with a non-null `estimated_api_cost_usd` counts as priced.

### 6. New reason values (`internal/model/usage.go`)

Usage reasons: `transcript-missing`, `transcript-invalid`, `transcript-span-unavailable`, and
`session-switched`. Existing reasons are reused: `transcript-ambiguous` and `no-usage-event`.

Cost reasons (audit only): `cost-report-unavailable`, `cost-report-stale`, `cost-baseline-missing`,
`cost-subagent-unsettled`, `cost-session-mismatch`, and `counter-reset`.

## Decisions

- **Status line as the cost channel.** This was verified by the feasibility gate. Hook payloads
  and transcripts carry no USD. OTLP was rejected because it needs a receiver and overrides the
  user's telemetry. A pricing catalog was rejected because it is forbidden by `cost-capture`.
- **Freshness by usage-tuple match, not time.** Timing alone cannot prove the payload includes
  the final turn: the debounce, cancellation, and post-completion turns all interfere. The usage
  tuple identifies the API response the payload reflects. A collision between two different
  messages with identical four-field usage is theoretically possible. The design accepts it,
  because any such message is in the same span and the cost difference would be the last tiny
  message.
- **Per-process baseline instead of a session-cumulative delta.** The gate observed both behaviors:
  carried cost in the Runner-driven resumes and a restart at 0 in a plain resume. Taking the
  process's own first payload as the baseline is correct either way. The baseline is accepted
  only with positive `prompt_id` evidence that it predates the step's input. It also avoids depending on the collector's cross-step baseline. Because
  the interactive record sets no `RawCumulativeCostUSD`, the cumulative baseline for later
  headless steps on that session is cleared, as it is today.
- **Inject a status line even when the user has none, accepting hidden footer hints.**
  - **Gain:** cost for the most expensive steps.
  - **Loss:** Claude's keyboard hints, such as `esc to interrupt`, are hidden during autonomous
    steps that nobody is typing into.
  - No status-line content is added.
  - The spec is revised to say this explicitly, instead of the earlier "none SHALL be added".
- **Do not inject when settings cannot be parsed.** This avoids silently replacing a user's status
  line that Runner failed to read. Cost is null in that case.
- **The wait runs after durability and before Terminate, bounded to 3 s.** This preserves
  completion reliability (spec: "Cost report never arrives").
- **The report file holds a minimal projection.** Payloads contain paths, repository identity, and
  rate limits. Only the five fields needed are persisted, in a run-owned file with mode 0600.
- **Cost requires every subagent settled before `F`.** Matching `F` proves only that main-thread
  usage is fresh. Background subagents can keep spending while the status line is quiet, so cost is
  accepted only when all attributed subagents provably finished before `F` was requested.
- **Subagent lifecycle comes from the parent transcript.** Foreground `tool_result` entries and
  background `<task-notification>` terminal statuses are the completion evidence, as observed in
  the gate. Anything unproven is partial. Absence of evidence is never treated as completion.
- **Claude's own settings-location rules decide which status line to delegate to.** If the rules
  cannot be applied safely, capture is disabled rather than risking a shadowed status line.

## Risks / Trade-offs

- **Claude Code payload drift.** If `current_usage` or `total_cost_usd` changes shape, matching
  fails and cost becomes null with `cost-report-stale` or `cost-baseline-missing`. Tokens are
  unaffected. Unit fixtures pin the current shape. The failure mode is null, not wrong.
- **Undocumented transcript shapes.** Subagent lifecycle evidence (`queued_command` and
  `queue-operation` task notifications, `toolUseResult.status`) is observed, not documented. If the
  shapes drift, subagents read as unproven: tokens become partial and cost becomes null. Results
  are never presented as complete. Fixtures pin the 2.1.296 shapes.
- **Turns after completion.** As observed, a background task notification can trigger another
  turn after the first `Stop`. Messages written before termination are counted in tokens. If they
  postdate the matched payload, `F` changes and cost becomes null. That is correct, but it lowers
  cost coverage for runs that use background subagents.
- **The settings-layer resolution must track Claude.** The resolution implements the documented
  local (root and legacy), shared, and user rules. If Claude adds layers, such as plugin-provided
  status lines, Runner might shadow one. That risk is accepted; managed settings already win over
  Runner's settings.
- **Reads after the CLI exits.** Background subagents may still be writing. Without completion
  evidence, they are partial and cost is null.
- **Span by offset assumes append-only transcripts.** Shrinkage is detected and makes the span
  unavailable. A rewrite of the same or a larger size is not detectable. Claude Code appends in
  all observed versions.

## Migration Plan

There is no data migration. Old `run-metrics.json` files keep `interactive-context` records. New
runs emit the new source and reasons.

Rollback is a code revert. The recorder subcommand is internal, and stale report files under run
state are harmless.

## Open Questions

None that block implementation. The post-merge real-terminal run of `orchestrate-implementation`
remains the end-to-end confirmation.
