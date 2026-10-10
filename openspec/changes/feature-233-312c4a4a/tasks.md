- [ ] Implement the change described by these files using TDD, as `CLAUDE.md` requires. Satisfy every spec scenario and every `INT-*` and `E2E-*` obligation in the test plan. Finish with `make fmt`, `make test`, and `make lint` passing:
  - [proposal.md](proposal.md)
  - [specs/interactive-claude-usage/spec.md](specs/interactive-claude-usage/spec.md)
  - [specs/agent-usage-collection/spec.md](specs/agent-usage-collection/spec.md)
  - [specs/claude-subagent-usage/spec.md](specs/claude-subagent-usage/spec.md)
  - [specs/cost-capture/spec.md](specs/cost-capture/spec.md)
  - [specs/run-metrics-artifact/spec.md](specs/run-metrics-artifact/spec.md)
  - [design.md](design.md)
  - [test-plan.md](test-plan.md)
  - [decisions.md](decisions.md)

  **Adapter capability and gating**
  - Add `InteractiveUsageCollector` and `InteractiveUsagePlan` to `internal/cli/adapter.go`, and implement them on `ClaudeAdapter` only.
  - In `extractAgentUsage` (`internal/exec/agent.go`), route `ContextAutonomousInteractive` steps whose adapter implements the collector to `ExtractInteractiveUsage`.
  - Human-interactive steps, and autonomous-interactive steps on other CLIs, keep `interactive-context`.

  **Span and main-thread usage**
  - Compute the plan immediately before `runAgentProcess`, and thread it through `directInvocation`:
    - resolve the transcript with `claudeTranscriptPaths`;
    - take `StartOffset` from `fileCheckpoint`, or 0 when the transcript is absent;
    - create the 0600 report file under the run state directory.
  - In `ExtractInteractiveUsage`, take these steps:
    - Stream `[StartOffset, EOF)`.
    - Deduplicate main-thread assistant messages by message ID, keeping the last usage.
    - Keep distinct models.
    - Emit source `claude:session-transcript`.
    - Report the design's reasons: `transcript-missing`, `transcript-invalid`, `transcript-span-unavailable`, `transcript-ambiguous`, `no-usage-event`, and `session-switched`. Never report zero.
    - Leave `RawCumulativeCostUSD` nil.
  - Refactor `scanClaudeParentSpan` to accept a span selector. Headless UUID-bounded behavior must not change.

  **Subagent lifecycle**
  - Derive completion evidence from the transcripts:
    - for foreground spawns, a non-async `tool_result`;
    - for background spawns, a `<task-notification>` with the spawn's `<tool-use-id>` and a terminal `<status>`, in a `queued_command` attachment or a `queue-operation` entry;
    - for nested spawns, the same evidence in the spawning subagent's transcript.
  - Feed unproven or re-activated spawns into the `running` set of `collectClaudeSubagents`.
  - Record each spawn's settled position.
  - Skip unrecognized entry types instead of treating them as invalid.

  **Status-line recorder and injection**
  - Add the `agent-runner internal statusline-record --report <path> [--delegate <cmd>]` subcommand:
    - Append `recorded_at`, `session_id`, `prompt_id`, `total_cost_usd`, and `current_usage` to the report before delegating.
    - Run the delegate with `sh -c`, passing the same stdin and environment through.
    - Pass the delegate's stdout and exit code through.
    - On a signal, kill the delegate's process group.
  - Resolve the user's effective `statusLine` with Claude's location rules:
    - the root or main-checkout local file, subject to Claude's exceptions;
    - the legacy workdir local file;
    - the workdir shared settings;
    - the user settings under `CLAUDE_CONFIG_DIR` or `HOME`.
  - Disable capture (`cost-report-unavailable`) on a parse error, a `git` error, or an ownership the code cannot determine.
  - Merge the recorder `statusLine` into the existing `--settings` JSON alongside the `Stop` hook, preserving the user object's display fields.

  **Bounded wait and cost**
  - In `finishDirectCompletion` (`internal/interactive/runner.go`), after durability succeeds and before `Terminate`, call `WaitForFinalReport`, bounded by `DefaultFinalReportTimeout = 3s`. Make the bound a `DirectOptions` option for tests.
    - Skip the wait on failure, on cancellation, or when reporting is disabled.
    - The wait must never change the outcome.
  - Compute cost per the design. Each check fails to a null cost with the reason shown:
    - the final report `P` matches `F`'s usage tuple (`cost-report-stale`);
    - the baseline `B` has `prompt_id` evidence (`cost-baseline-missing`);
    - all subagents settled before `F` (`cost-subagent-unsettled`);
    - all reports name the step's session (`cost-session-mismatch`);
    - the delta is non-negative (`counter-reset`).
  - Set `EstimatedCostUSD`.
  - Emit `cost_unavailable_reason` in the step-end audit data.

  **Metrics and model**
  - Add the new usage reasons to `internal/model/usage.go`.
  - In `internal/metrics`, count `claude:session-transcript` records with partial completeness as partial for usage and canonical-total coverage. Other partial records keep their current treatment.

  **Tests**
  - Write unit tests for every spec scenario.
  - Add INT-001 to INT-005 and E2E-001 as described in `test-plan.md`, in the existing `test` CI job. Add no new CI jobs.
  - Model transcript and report fixtures on the Claude 2.1.296 shapes recorded in `design.md`.
