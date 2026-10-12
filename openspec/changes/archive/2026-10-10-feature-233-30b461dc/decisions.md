# Decisions

## proposal: verdict go with caveats
- **Decision:** Proceed. Autonomous-interactive steps record `interactive-context` and drop what is often a run's most expensive step from tokens and cost. Claude session transcripts hold per-message usage and model, and the existing subagent transcript code already parses them.
- **Alternatives:** No-go because the current behavior matches the spec (rejected: the issue explicitly asks for the spec to change).
- **Decision-bearing:** yes

## proposal: tokens from the Claude session transcript
- **Decision:** Read main-thread usage from `<session-id>.jsonl`, counting each assistant message ID once with its final usage. Treat the counts as per-message, not cumulative.
- **Alternatives:** Capture a PTY tee of the terminal (rejected: the TUI output carries no structured usage). OpenTelemetry export (rejected for tokens: the transcript is already a supported source).
- **Decision-bearing:** no

## proposal: invocation span by transcript position
- **Decision:** Before launch, record the parent transcript's position. The invocation's span is everything appended after it. If the span cannot be established, usage is partial or unavailable with a reason, never zero.
- **Alternatives:** Count the whole transcript (rejected: double counts sessions shared through `inherit` or `resume`). Use timestamp windows (rejected: fragile).
- **Decision-bearing:** no

## proposal: cost via injected Claude status line
- **Decision:** Capture Claude's own `cost.total_cost_usd` from a status-line command that Runner injects through its existing `--settings` payload. The command delegates to the user's configured status line. The cost is recorded verbatim and attributed by delta under the existing baseline rules. It is null when it is stale, from another session, missing, or has no trusted baseline.
- **Alternatives:**
  - Tokens only, cost null (rejected: the issue asks for cost).
  - A pricing catalog (rejected: contradicts `cost-capture`).
  - An OTLP receiver with Claude telemetry environment variables (rejected: heavy, and overrides the user's telemetry configuration).
- **Decision-bearing:** yes. It changes what runs in the user's Claude status line during autonomous-interactive steps. Freshness and resume semantics are left to `design.md`.

## proposal: subagents included
- **Decision:** Extend `claude-subagent-usage` to autonomous-interactive Claude steps, discovering spawns from the transcript span.
- **Alternatives:** Main thread only (rejected: the motivating step's cost is dominated by sub-agents).
- **Decision-bearing:** no

## proposal: scope limited to autonomous-interactive Claude
- **Decision:** Only autonomous-interactive Claude steps change. Other CLIs and human-interactive steps keep `interactive-context`.
- **Alternatives:**
  - Cover Codex, Cursor, Copilot, and OpenCode too (deferred: each needs its own transcript source and attribution rules; the issue's evidence and the `interactive-claude` backend are Claude).
  - Include human-interactive steps (deferred: users can clear or switch sessions, so attribution is less reliable, and the issue targets autonomous-interactive).
- **Decision-bearing:** yes. This is the minimum useful scope; follow-up issues can extend it.

## proposal-review: PR-001 cost channel feasibility (applied)
- **Decision:** Applied. Cost-channel feasibility is now a gate at the start of design, run against real Claude through Runner's completion path for a fresh session and a resumed session, both with subagents. It must establish three things: causal freshness (the payload is tied to the final accounted turn, not just written after the Stop hook), the counter scope of `total_cost_usd` across `--resume`, and a bounded collection window between turn durability and `Terminate` in `finishDirectCompletion`, with cost null on timeout. The "usually cost" claim was removed, and feasibility evidence moved ahead of implementation instead of post-merge. If the gate fails, design stops for a direction decision, likely to stage tokens first with cost as a follow-up.
- **Alternatives:** Keep the status-line approach and leave freshness to design (rejected: an unverified feasibility dependency could leave cost null most of the time or accept a stale subtotal). Drop cost now and ship tokens only (rejected at this step: it departs from the issue's request for cost, so it is a direction decision taken only if the gate fails).
- **Decision-bearing:** yes. Whether cost ships in this change depends on the gate's outcome.

## spec: capability split
- **Decision:** New `interactive-claude-usage` spec covering transcript usage, per-message dedupe, positional span, explicit gaps, and Claude-reported cost. Deltas to `agent-usage-collection` ("Unavailable usage is explicit"), `claude-subagent-usage` (renamed to "Subagent usage collection for Claude steps", plus attribution and model-scope changes), `cost-capture` ("CLI-reported cost captured verbatim"), and `run-metrics-artifact` (renamed to "Partial Claude transcript collection affects coverage").
- **Alternatives:** Fold everything into `claude-subagent-usage` (rejected: main-thread transcript usage and cost are not subagent behavior). Leave `run-metrics-artifact` unchanged (rejected: a partial main-thread transcript would otherwise count as complete coverage).
- **Decision-bearing:** no

## spec: span with no assistant messages is unavailable, not zero
- **Decision:** If the invocation's span is established but has no assistant messages, usage is unavailable with a reason.
- **Alternatives:** Report available zero (rejected: a failed model call might not be recorded in the transcript, and the specs never present missing usage as zero).
- **Decision-bearing:** no

## spec: transcript usage is per-message, no baseline
- **Decision:** Transcript usage is attributed directly, even for a resumed session with no prior record in the run. The cumulative no-baseline rule does not apply.
- **Alternatives:** Treat it as cumulative (rejected: transcript usage is per message).
- **Decision-bearing:** no

## spec: cost scenarios deferred to design
- **Decision:** Cost requirements are written but marked `deferred-to-design`, pending the feasibility gate from PR-001. Cost is null on timeout, staleness, a mismatched session, or managed-settings override. The user's status line must be preserved.
- **Alternatives:** Omit cost requirements until the gate passes (rejected: the issue asks for cost, and the null-fallback behavior can be specified now).
- **Decision-bearing:** yes. If the gate fails, the cost requirement is withdrawn through a direction decision.

## spec: mid-step session switch
- **Decision:** If Runner has evidence that the agent switched sessions mid-step, usage is partial with a reason. Which evidence counts is deferred to design.
- **Alternatives:** Follow session switches (rejected: out of scope, and attribution is unreliable).
- **Decision-bearing:** no

## design: cost-channel feasibility gate passed
- **Decision:** Gate passed on real Claude Code 2.1.296, with a fresh session and a `--resume`, each spawning a subagent:
  - **Freshness:** the final status-line payload arrived about 0.3 s after the final Stop hook, and its `context_window.current_usage` exactly matched the final transcript message.
  - **Subagent spend:** cost includes it.
  - **Counter scope:** `total_cost_usd` restarts at 0 in a resumed process.
  - **Limitation:** measured with hook instrumentation, not a full Runner run, because a run can't be launched under a synthetic PTY. Runner's path only moves the wait's start later.
- **Alternatives:** Stop for a direction decision to ship tokens only (not needed, because the gate passed).
- **Decision-bearing:** yes

## design: freshness by usage-tuple match
- **Decision:** A payload counts as final only when its `current_usage` equals the final main-thread transcript message's usage. Cost is null otherwise, including when a later turn ran after completion.
- **Alternatives:** Accept the latest payload after a time delay (rejected: debounce, cancellation, and post-completion turns make timing unreliable).
- **Decision-bearing:** no

## design: per-process cost baseline
- **Decision:** Step cost = final payload cost − the process's first payload cost. The first payload must predate the span's first model response, or cost is null. `RawCumulativeCostUSD` stays unset, so the collector clears the session's cumulative baseline as today.
- **Alternatives:** The session-cumulative delta from `cost-capture` (rejected: the observed counter is process-scoped). Assume the counter starts at 0 (rejected: the scope changed across Claude versions).
- **Decision-bearing:** no. This revised the spec's deferred delta scenario.

## design: inject status line even when the user has none
- **Decision:** Inject a recorder that prints nothing when the user has no status line. Per Claude's docs, any configured status line hides most footer keyboard hints (`esc to interrupt`) during autonomous-interactive steps. The spec was revised from "none SHALL be added" to "no status-line content is added; footer hints may be hidden".
- **Alternatives:** Capture cost only for users who already have a status line (rejected: cost coverage would depend on an unrelated personal setting). Never inject (rejected: no cost).
- **Decision-bearing:** yes. This is a small user-visible change during unattended steps.

## design: unreadable Claude settings skip injection
- **Decision:** If a settings layer that could define `statusLine` exists but can't be parsed, don't inject. Cost is null.
- **Alternatives:** Inject anyway (rejected: could silently replace the user's status line).
- **Decision-bearing:** no

## design: bounded wait placement and bound
- **Decision:** In `finishDirectCompletion`, after durability succeeds and before Terminate, wait up to 3 s (`DefaultFinalReportTimeout`) for a matching payload. The wait never changes the outcome.
- **Alternatives:** No wait, reading only after exit (rejected: termination about 0 s after durability would usually beat the ~0.3 s debounce). A longer bound (rejected: no evidence it's needed, and it delays every step).
- **Decision-bearing:** no

## design: minimal report persistence
- **Decision:** The recorder appends only `recorded_at`, `session_id`, `total_cost_usd`, and `current_usage` to a mode-0600 run-owned file, before delegating to the user's command.
- **Alternatives:** Store full payloads (rejected: they contain paths, repository identity, and rate-limit data). Keep only the latest payload with atomic replace (rejected: concurrent or cancelled invocations could leave a stale latest, and the baseline needs the first payload).
- **Decision-bearing:** no

## design: cost-null reasons in audit only
- **Decision:** Cost-null reasons go in step-end audit data (`cost_unavailable_reason`). No new `run-metrics.json` field.
- **Alternatives:** Add a step-level cost reason to the artifact (rejected: a schema change beyond scope).
- **Decision-bearing:** no

## test-plan: automated obligations
- **Decision:** Three integration tests and one end-to-end test:
  - INT-001: transcript span collection on real files;
  - INT-002: the built recorder subcommand with a real `sh` delegate;
  - INT-003: the direct runner's bounded wait with the helper child;
  - E2E-001: a two-step `interactive-claude` run through the built binary under a PTY, with a fake `claude` that drives the injected status line, plus a no-final-payload variant.

  All are added to the existing `test` CI job, with no real Claude.
- **Alternatives:**
  - Real-Claude tests in CI (rejected: cost, credentials, and flakiness).
  - Extra E2E tests for non-Claude and human-interactive gating, or for settings-parse failure (rejected: fully covered at the unit level).
- **Decision-bearing:** no

## test-plan: acceptance envelope and HT-001
- **Decision:**
  - **Authorized:** real Claude acceptance on haiku, under $1, using temporary project directories with cleanup.
  - **Off limits:** global Claude settings, live factory runs, and GitHub mutations.
  - **Substitute:** hook-instrumented real Claude plus the fake harness, if a real run can't be driven under a synthetic PTY.
  - **HT-001:** a real-terminal orchestrated run. `CLAUDE.md` says real run launches need a human, and whether hidden footer hints are acceptable is a subjective call.
- **Alternatives:**
  - No HT, relying on the substitute (rejected: the real Runner completion path with real Claude would never be exercised).
  - Allow editing `~/.claude/settings.json` to test delegation (rejected: user-global state; project-level settings exercise the same code).
- **Decision-bearing:** no

## approach-review: AR-001 baseline needs positive evidence (applied)
- **Decision:** Applied. The baseline report is accepted only when all of these hold:
  - it is the attempt's first recorded report and has no `prompt_id`;
  - a later report carries a `prompt_id`;
  - the first `prompt_id` reported equals the span's first user `promptId`.

  A null or unmatched `current_usage` is no longer accepted as evidence. This is grounded in the re-run gate: startup payloads lacked `prompt_id`, and every later payload carried the transcript's `promptId`. The recorder now persists `prompt_id`. A spec scenario covers each of the lost-startup/post-compaction and mid-stream first reports. INT-004 covers these cases.
- **Alternatives:** A transcript-size witness recorded with each report (rejected: the cost counter can increment before the transcript write, so size alone can't order them). Keep the null-or-unmatched rule (rejected: it accepts false baselines and undercounts).
- **Decision-bearing:** no

## approach-review: AR-002 subagent lifecycle from transcripts (applied)
- **Decision:** Applied. Without stdout, completion evidence comes from the parent transcript (or, for nested spawns, the spawning subagent's transcript):
  - **Foreground spawn:** a non-async `tool_result`.
  - **Background spawn:** a `<task-notification>` with the spawn's `<tool-use-id>` and a terminal `<status>`, in a `queued_command` attachment or `queue-operation` entry. These shapes were captured in the gate.

  Unproven or re-activated subagents feed the existing `running` set, so they become `subagent-still-running` partial. Unrecognized entry types are skipped, not marked invalid. INT-001 and the `claude-subagent-usage` scenarios cover this.
- **Alternatives:** Use the `.meta.json` sidecar or file quiescence (rejected: neither proves completion). Wait for subagents (rejected: the spec forbids waiting).
- **Decision-bearing:** no

## approach-review: AR-003 cost requires settled subagents (applied)
- **Decision:** Applied. Cost is eligible only when every attributed subagent, at every depth, has a settled position (its completion evidence) before `F` in the span, with no usage after its last completion. Otherwise cost is null with `cost-subagent-unsettled`, tokens are kept, and cost coverage is not complete. Spec scenarios and INT-004 cases (g) and (h) cover this.
- **Alternatives:** Timestamp comparison between subagent entries and the recorder's clock (rejected: mixes clocks; transcript ordering is causal). Accept the matched report regardless (rejected: can present a stale aggregate as complete).
- **Decision-bearing:** no

## approach-review: AR-004 Claude's settings-location rules (applied)
- **Decision:** Applied. Status-line resolution now follows Claude's documented rules (v2.1.211+):
  - **Local file:** at the repository root, or the main checkout root for linked worktrees. Claude's exceptions apply: outside a repository, root equal to the home directory, or not owned by the user.
  - **Legacy file:** the working-directory local file, with the root file winning.
  - **Shared and user settings:** shared settings from the working directory, and user settings from `CLAUDE_CONFIG_DIR` or `HOME`, using the step's environment.

  Capture is disabled when resolution is uncertain (parse failure, `git` failure, unknown ownership). There is a spec scenario and INT-005.
- **Alternatives:** Keep workdir-only resolution (rejected: shadows the real status line in subdirectories and worktrees).
- **Decision-bearing:** no

## approach-review: AR-005 gate through Runner's real path (applied)
- **Decision:** Applied. The gate was re-run through Runner's actual lifecycle:
  - **Setup:** a locally built `agent-runner` ran a fresh-plus-resume workflow under a PTY with real Claude 2.1.296 (haiku). It used a true autonomous-interactive context (`autonomous_backend: interactive-claude`) via a temporary `HOME` with symlinked Claude config, and the global settings were untouched. A human-interactive run was also made.
  - **Timing:** in all four steps, the final matching payload arrived 0.33–0.46 s after `turn_committed`, before Runner's termination at about 1.0–1.15 s.
  - **New finding:** resumed processes carried prior cost, which confirms the per-process baseline is required.
  - **Plan changes:** the test plan now requires a pre-merge acceptance run with non-null final cost for ordinary completed-subagent fresh and resumed steps, with no substitute. HT-001 treats a null cost as failure unless an intended fallback actually occurred.
  - **Cleanup:** temporary files and run directories were removed. The real `~/.claude.json` keeps a folder-trust entry for the deleted `/private/tmp/f233b/work`, which is harmless.
- **Alternatives:** Keep the hook-only evidence (rejected: it omits Runner's control, durability, and termination lifecycle).
- **Decision-bearing:** yes. Cost stays in scope on direct evidence.

## tasks: single implementation task
- **Decision:** `tasks.md` has exactly one checkbox. It links every artifact and groups the work into adapter and gating, span and main-thread usage, subagent lifecycle, recorder and injection, bounded wait and cost, metrics and model, and tests.
- **Alternatives:** Split into several tasks (rejected: the step requires exactly one).
- **Decision-bearing:** no
