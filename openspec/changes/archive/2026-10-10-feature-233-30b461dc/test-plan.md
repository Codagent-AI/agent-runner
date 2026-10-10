## Coverage Strategy

The specifications are the source of unit-test requirements. This plan records only the
additional integration and end-to-end obligations, the acceptance testing envelope, and the
exceptional human-only check.

Unit tests are expected to cover most of the change. They exercise these pieces against in-memory
or temporary fixtures:

- span parsing and message dedupe;
- reason selection;
- cost computation, covering the baseline, stale reports, session mismatches, and negative deltas;
- settings-layer resolution;
- `extractAgentUsage` gating (non-Claude and human-interactive steps keep `interactive-context`);
- metrics coverage for the `claude:session-transcript` source.

The obligations below cover the places where several real components have to work together:

- real transcript files with byte offsets;
- the built recorder subcommand with a real shell delegate;
- the direct runner's bounded wait against a live child process;
- one full run through the built binary under a PTY.

All automated tests use fake Claude processes. None calls the real Claude CLI or spends money.

## Integration Tests

### INT-001: Transcript span collection on real files
- **Covers:**
  - `interactive-claude-usage`: "Transcript usage for autonomous-interactive Claude steps", "Each assistant message counted once with its final usage", "Invocation span by transcript position", and "Unusable transcript evidence is explicit".
  - `claude-subagent-usage`: "Attribution to the spawning invocation", for positional spans.
- **Boundary:** `ClaudeAdapter.PrepareInteractiveUsage` → appends to the files → `ExtractInteractiveUsage`, reading through `os.Root` with real offsets, `CLAUDE_CONFIG_DIR` resolution, a shortened project directory, and subagent sidecars.
- **Setup:** a temporary Claude config directory holding a session transcript in a shortened project directory, plus subagent `.jsonl` and `.meta.json` files.
- **Action:** each case runs Prepare, appends entries, then Extract. The cases are:
  - a fresh session where the file is absent at Prepare;
  - a resumed session with existing entries and an earlier spawn;
  - two sequential invocations on one session, the inherit case;
  - a span with a malformed line;
  - a span with only a prompt and no assistant message;
  - a truncated file, where size is less than the start offset;
  - a session present in two project directories.

  Subagent lifecycle cases use transcript shapes captured from Claude 2.1.296:
  - a foreground spawn finished by a non-async `tool_result`;
  - a background spawn (`toolUseResult.status: "async_launched"`) finished by a `<task-notification>` with `<status>completed</status>`, in both its `queued_command` attachment form and its `queue-operation` form;
  - a background spawn with a valid transcript but no notification;
  - a background spawn whose subagent records usage after its last completion notification, meaning it was re-activated;
  - a finished subagent whose nested subagent has no finish evidence;
  - unrecognized entry types such as `last-prompt`.
- **Assertions:**
  - The token categories and canonical totals equal the expected per-message sums: the streamed duplicate counts once, at its final value.
  - Pre-span messages and spawns are excluded, and the two invocations' totals sum to the whole file once.
  - Each subagent allocation carries its observed model.
  - The source is `claude:session-transcript`.
  - Each partial or unavailable case carries the reason given in the design, never zero.
  - `RawCumulativeCostUSD` is nil.
  - Proven-finished subagents are complete.
  - Unfinished, re-activated, and nested-unfinished subagents keep their usage, with allocation reason `subagent-still-running`, and subagent collection and attempt completeness are `partial`.
  - Unrecognized entry types do not make the span invalid.
- **Execution:** an `internal/cli` package test in the `test` CI job (`go test -tags dev_audit ./...`).

### INT-002: Status-line recorder subcommand with a real delegate
- **Covers:**
  - `interactive-claude-usage`: "Claude-reported cost for autonomous-interactive Claude steps", specifically the status-line content guarantees and the recording that cost depends on.
- **Boundary:** the built `agent-runner internal statusline-record` binary, invoked as Claude would invoke it: payload on stdin, `COLUMNS` and `LINES` in the environment, a real `sh -c` delegate.
- **Setup:** build the binary once (reuse `buildAgentRunner`), create a temporary report path, and use a delegate script that echoes its stdin length and `$COLUMNS`, then exits with code 3.
- **Action:**
  1. Run the recorder with a full payload fixture that includes paths, repo, and rate limits, with and without `--delegate`.
  2. Run it again with a delegate that sleeps, and send SIGTERM to the recorder.
- **Assertions:**
  - The recorder's stdout and exit code equal the delegate's.
  - Without a delegate, stdout is empty and the exit code is 0.
  - The report gains exactly one line per invocation. That line holds only `recorded_at`, `session_id`, `total_cost_usd`, and `current_usage`; no paths or rate limits.
  - The report file mode is 0600.
  - The line exists even when the delegate is killed.
  - The SIGTERM case leaves no delegate process running.
- **Execution:** a `cmd/agent-runner` test, POSIX only, in the `test` CI job.

### INT-003: Bounded final-report wait in the direct runner
- **Covers:**
  - `interactive-claude-usage`: "Claude-reported cost …", for the bounded wait after durability; scenarios "Cost report never arrives" and "Final cost report captured".
- **Boundary:** `interactive.DirectRunner` `finishDirectCompletion` → durability → `WaitForFinalReport` → `Terminate`, using the existing helper-process child (`TestDirectRunnerHelperProcess`) and a real control channel.
- **Setup:** a helper child that writes a committed final assistant message to a temporary transcript, requests completion, and then, by mode:
  - (a) appends a matching report line after 200 ms;
  - (b) never writes a matching line;
  - (c) writes a matching line, then a later assistant message.

  `DirectOptions` sets a short bound, such as 500 ms, through the new option.
- **Assertions:**
  - (a) Terminate happens after the matching line and before the bound.
  - (b) Terminate happens no later than the bound plus the termination grace.
  - `DirectResult.Completed` is true and the outcome is unchanged in every mode.
  - The wait is skipped when durability fails or the context is cancelled.
  - The wait is skipped when the plan has reporting disabled.
- **Execution:** an `internal/interactive` package test in the `test` CI job.

### INT-004: Cost eligibility from recorded reports and transcripts
- **Covers:**
  - `interactive-claude-usage`: "Claude-reported cost for autonomous-interactive Claude steps", including the scenarios for baseline evidence, stale reports, the subagents-settled rule, and session mismatch.
- **Boundary:** `ExtractInteractiveUsage` reading a real report file, as the recorder writes it, together with a real transcript tree on disk.
- **Setup:** report and transcript fixtures modeled on the gate's observations. Each case:
  - (a) fresh: a startup report with no `prompt_id` and cost 0, intermediate streaming reports, and a final report matching `F`;
  - (b) resumed, with the startup report carrying prior cost and no `prompt_id`;
  - (c) startup report missing, and the first report has a `prompt_id` and null `current_usage` (post-compaction);
  - (d) startup report missing, and the first report is a mid-stream tuple;
  - (e) no report carries a `prompt_id` at all;
  - (f) the first `prompt_id` differs from the span's first user `promptId`;
  - (g) a background subagent's notification is written after `F`;
  - (h) a background subagent records usage after its notification;
  - (i) a report names another session;
  - (j) a final cost lower than the baseline.
- **Assertions:**
  - (a) and (b): `EstimatedCostUSD` equals the final cost minus the startup cost (for (b), not the raw final value), and tokens include the subagent usage.
  - (c) to (j): cost is nil, the design's reason is reported (`cost-baseline-missing`, `cost-subagent-unsettled`, `cost-session-mismatch`, or `counter-reset`), and token usage is unchanged.
  - (i): main-thread usage is `partial` with `session-switched`.
- **Execution:** an `internal/cli` package test in the `test` CI job.

### INT-005: Status-line resolution follows Claude's settings locations
- **Covers:**
  - `interactive-claude-usage`: "Claude-reported cost …", for the guarantees about preserving the user's status line, with scenarios "Project-local status line at the repository root", "Unreadable Claude settings", and "User's status line preserved".
- **Boundary:** `PrepareInteractiveUsage` and `BuildArgsWithError` against real temporary git repositories, run through real `git`, with the step's effective `HOME` and `CLAUDE_CONFIG_DIR`.
- **Setup:** a temporary repository with conflicting `statusLine` definitions (different commands and `padding`) in:
  - the root `.claude/settings.local.json`;
  - a legacy subdirectory `.claude/settings.local.json`;
  - the subdirectory's `.claude/settings.json`;
  - the user settings under `CLAUDE_CONFIG_DIR`.

  Plus a linked worktree created with `git worktree add`, and a non-repository directory.
- **Action:** prepare and build arguments for these working directories:
  - the repository root;
  - a subdirectory;
  - the linked worktree;
  - the non-repository directory;
  - a case with an unparseable candidate file;
  - a case where a `git` query fails.
- **Assertions:**
  - The injected `statusLine` delegates to the command Claude would load: the root local file over the legacy file, and the main checkout's local file for the worktree. All of its display fields are preserved, and the `Stop` hook is still present.
  - The unparseable and `git`-failure cases inject no `statusLine`, and the plan reports `cost-report-unavailable`.
- **Execution:** an `internal/cli` package test in the `test` CI job. It needs `git`, which is available on CI runners.

## End-to-End Tests

### E2E-001: Autonomous-interactive Claude run records tokens and cost
- **Covers:**
  - `interactive-claude-usage`: "Transcript usage for autonomous-interactive Claude steps", "Invocation span by transcript position", and "Claude-reported cost for autonomous-interactive Claude steps".
  - `agent-usage-collection`: "Unavailable usage is explicit".
  - `claude-subagent-usage`: "Subagent usage collection for Claude steps".
  - `cost-capture`: "CLI-reported cost captured verbatim".
  - `run-metrics-artifact`: "Partial Claude transcript collection affects coverage".
- **Surface:** the built `agent-runner` binary running a workflow under a PTY, with `autonomous_backend: interactive-claude`. It extends the harness in `cmd/agent-runner/smoke_interactive_integration_test.go`.
- **Setup:**
  - Temporary `HOME` and a workdir whose `.claude/settings.json` defines a user `statusLine` (a script that prints `USER-LINE` plus padding).
  - A two-step workflow: step 2 uses `session: inherit`.
  - A fake `claude` fixture that does the following:
    1. Records its argv.
    2. Reads `statusLine.command` from the `--settings` JSON.
    3. Pipes a startup payload with cost 0 to that command.
    4. Appends main-thread assistant messages to the session transcript, one of them streamed twice, plus one subagent transcript and sidecar.
    5. Pipes a final payload whose `current_usage` matches the last message and whose cost is 0.42 (step 1) or 0.30 (step 2).
    6. Completes through the control channel.
  - A second variant in which the fake never emits the final payload.
- **Journey:** `agent-runner --profile smoke_test <workflow>` in the PTY, until the run completes.
- **Assertions:**
  - `run-metrics.json` shows both steps with usage available, source `claude:session-transcript`, per-step tokens that exclude each other's messages, a subagent allocation, and `estimated_api_cost_usd` 0.42 and 0.30.
  - The run cost totals 0.72, and cost coverage and usage coverage are `complete`.
  - The PTY output contains `USER-LINE`. The fixture's recorded `--settings` keeps the user's `padding` and still contains the `Stop` hook.
  - In the variant: the run succeeds, the step cost is null, cost coverage is `none`, tokens are still available, the audit step-end event has a `cost_unavailable_reason`, and the step completes within the bound plus slack.
- **Execution:** a `cmd/agent-runner` test, POSIX only, in the `test` CI job. It must use the existing PTY helpers and stable assertions on files, not on screen layout.

No further E2E is planned. Non-Claude and human-interactive gating, settings-parse failure, and
session-mismatch cases are fully observable at the unit or integration layer.

## Acceptance Testing Envelope

- **Environments and sandboxes:**
  - Paul's Mac, with the repository checkout and `./dev.sh`.
  - Temporary working directories under `/tmp`, used as Claude project directories.
  - The real Claude Code CLI, version 2.1.296 at design time.
  - A synthetic PTY (Python `pty.fork`) for driving Claude or `agent-runner` runs. Per `CLAUDE.md`, launching runs from the TUI browser does not work under a synthetic PTY. Direct CLI invocation of a workflow may be attempted.
- **Credentials and secrets:** Claude Code is logged in on the Mac through the user's keychain. There are no other secrets.
  - To run autonomous-interactive steps without touching global Agent Runner settings, use a temporary `HOME` containing:
    - symlinks `~/.claude` → the real `~/.claude`;
    - `~/Library` → the real `~/Library`, for keychain access;
    - a copy of `~/.claude.json`;
    - its own `.agent-runner/settings.yaml` with `autonomous_backend: interactive-claude`.

    The design's feasibility gate used this setup successfully. Child processes must not inherit `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, or `CLAUDE_CODE_ENTRYPOINT`: Runner strips them, and manual instrumentation must use a clean environment, or Claude writes no transcripts.
- **Authorized effects:**
  - **Spend:** real Claude sessions on the cheapest model (`haiku`), with short prompts that spawn at most one or two subagents. The total is under $1.
  - **Cleanup:** remove the test project directories created under `~/.claude/projects/` (for example `-private-tmp-…`), any run directories created under the real `~/.agent-runner/projects/`, the temporary `HOME`, and the `/tmp` work directories.
- **Off limits:**
  - The user's `~/.claude/settings.json` and other global Claude configuration. Put status-line settings in the temporary project's `.claude/settings.json` instead.
  - The real `~/.agent-runner/settings.yaml`. Live factory runs share it.
  - Live Agent Factory runs and claims.
  - Other repositories.
  - Pushing, PRs, or GitHub mutations.
- **Readiness obligation:** before merge, the acceptance pass must run the built binary through a direct workflow invocation under a PTY, with real Claude in the autonomous-interactive context. It needs a fresh step and a resumed step, each with a subagent that finishes before the step completes. Both steps must record available tokens with subagent allocations, and a non-null `estimated_api_cost_usd` equal to the final minus the baseline report. A null cost in these ordinary cases is a defect, not an acceptable fallback.
- **Permitted substitutes:** none for the readiness obligation; the design's gate proved that it can be driven. The fake-Claude harness (E2E-001) remains the regression guard for the null-fallback paths.
- **Known risk areas:**
  - The status-line payload shape drifts across Claude versions. A mismatch should yield null cost, never a wrong cost.
  - A post-completion turn, such as a background-task notification, makes cost null; this is an accepted limitation.
  - Footer keyboard hints are hidden when the user has no status line; this is an accepted trade-off.
  - Settings-layer resolution may not match Claude's for plugin-provided status lines.
  - Transcripts are assumed to be append-only.
  - Subagent lifecycle shapes (`queued_command` and `queue-operation` task notifications) are observed, not documented. Background subagents are the norm in Claude 2.1.296, even when the prompt asks the agent to wait.
  - Settings resolution from subdirectories and worktrees.
  - The existing headless `total_cost_usd` baseline is cleared after an interactive step on the same session.

## Human-Only Testing

### HT-001: Real-terminal orchestrated run shows usage and cost
- **Reason:** whether the hidden footer hints are acceptable in a real terminal during long orchestrated steps is a subjective call. The gate showed that an agent can launch direct-CLI runs under a PTY, so mechanical verification belongs to the acceptance pass.
- **Prerequisites:**
  - E2E-001 and INT-001 to INT-005 pass in CI.
  - The acceptance pass has confirmed recorder and cost behavior against real Claude, by direct run or substitute.
- **Instructions:**
  1. With `autonomous_backend: interactive-claude`, run a workflow containing an autonomous-interactive Claude step that spawns subagents, such as `orchestrated-implement-change` on a small change, at a real terminal.
  2. Watch the status line during the step.
  3. Open the run's `run-metrics.json`.
- **Required decision or observation:**
  - Confirm that the step's usage is available with subagent allocations and a non-null `estimated_api_cost_usd`. A null cost counts as a failure unless the audit's `cost_unavailable_reason` names an intended fallback that actually occurred, such as a background subagent still running at completion.
  - Confirm that your own status line, if configured, looked unchanged.
  - Decide whether the missing footer hints are acceptable.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| interactive-claude-usage: Transcript usage for autonomous-interactive Claude steps | INT-001 | E2E-001 | HT-001 |
| interactive-claude-usage: Each assistant message counted once with its final usage | INT-001 | E2E-001 | — |
| interactive-claude-usage: Invocation span by transcript position | INT-001 | E2E-001 | — |
| interactive-claude-usage: Unusable transcript evidence is explicit | INT-001 | — | — |
| interactive-claude-usage: Claude-reported cost for autonomous-interactive Claude steps | INT-002, INT-003, INT-004, INT-005 | E2E-001 | HT-001 |
| agent-usage-collection: Unavailable usage is explicit | — | E2E-001 | — |
| claude-subagent-usage: Subagent usage collection for Claude steps | INT-001 | E2E-001 | HT-001 |
| claude-subagent-usage: Missing or incomplete subagent evidence is explicit (autonomous-interactive lifecycle) | INT-001 | — | — |
| claude-subagent-usage: Attribution to the spawning invocation | INT-001 | — | — |
| cost-capture: CLI-reported cost captured verbatim | — | E2E-001 | — |
| run-metrics-artifact: Partial Claude transcript collection affects coverage | — | E2E-001 | — |
