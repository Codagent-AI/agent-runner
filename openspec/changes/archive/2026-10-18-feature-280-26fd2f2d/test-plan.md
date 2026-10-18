## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

Unit tests carry most of this change: the context-measurement builder's status rules, Claude stream
and transcript observation rules, Codex rollout classification, external-user turn merging, collector
pass-through, and summary grouping (see `design.md` Testing). The obligations below cover the places
where those units meet real boundaries: the Codex rollout on disk behind Runner's private Codex home,
the statusline command feeding the interactive collector, artifact persistence across resume, and the
built binary producing `run-metrics.json`. The main residual risk is format drift in real CLI output,
which only real-agent runs detect; that is covered by extending the existing opt-in real-agent tests,
not by a new suite.

## Integration Tests

### INT-001: Codex rollout located and classified through the private Codex home
- Covers: `agent-context-size` Context sources by CLI (Codex scenarios), Eligible observations (compaction and synthetic exclusion), Per-attempt context measurement (unclassifiable trailing update)
- Boundary: `CodexAdapter.ExtractUsageWithContext` reading a rollout from the real filesystem through the `sessions` symlink that `prepareCodexRunnerHome` creates in the private `CODEX_HOME`.
- Setup: temp source Codex home with `sessions/YYYY/MM/DD/rollout-<ts>-<id>.jsonl` fixtures derived from a real Codex 0.160+ rollout (session meta, per-response `token_usage_record` lines, a compaction record followed by `compacted`, synthetic zero-input `token_count` updates, a subagent thread's records); a second, older-format rollout with only `token_count` events; a private home built by the real helper; `UsageContext.Env` carrying the private `CODEX_HOME`; `StartedAt` set between earlier and later lines to model a resumed session.
- Action: extract usage and context for stdout fixtures whose `thread.started` names each session, plus one naming a session with no rollout.
- Assertions: last observed request input and peak equal the eligible records' `input_tokens` after `StartedAt`; compaction, synthetic, pre-start, and subagent records contribute nothing; window limit equals `model_context_window`; trailing record yields `partial`; older format yields `unavailable` / `codex-rollout-unsupported`; missing rollout yields `unavailable` / `codex-rollout-not-found`; the usage record is byte-identical to the one produced without the rollout present.
- Execution: `internal/cli` package test, `go test -tags dev_audit ./...` (CI `test` job).

### INT-002: Metrics artifact over resume and legacy records
- Covers: `run-metrics-artifact` Agent-session context summary (inherited session spans a resume, coverage), Artifacts without context measurements, Context measurement excluded from totals, Schema version unchanged
- Boundary: metrics collector writing `run-metrics.json` to disk, reloading it in a new execution session, and recomputing aggregates.
- Setup: a run directory holding a `run-metrics.json` written in the current schema whose agent records have no `context` (the pre-change shape), including one agent session S; audit step-end events for a resumed execution session that adds attempts in S (with context), a fresh session, an attempt without a session ID, and a shell step.
- Action: load the collector for the resumed execution session, record the events, and read the artifact back from disk.
- Assertions: legacy records are unchanged; S has one summary entry spanning both execution sessions with coverage `partial` and a high-water mark from the new attempts only; the fresh session has its own entry; unattributed count is 1; the shell step has no context and no entry; execution-session rollups and all totals equal those computed with context removed from the events; schema version equals the pre-change constant.
- Execution: `internal/metrics` package test, CI `test` job.

## End-to-End Tests

### E2E-001: Headless workflow records per-step context and session summary
- Covers: `agent-context-size` Per-attempt context measurement, Context size is a gauge, Context sources by CLI (Claude headless, Codex headless); `agent-usage-collection` Context observations collected with usage, Non-agent steps carry no context measurement; `run-metrics-artifact` Context measurement in step records, Agent-session context summary
- Surface: built `agent-runner` binary running a workflow from the CLI.
- Setup: follow `cmd/agent-runner/claude_subagent_usage_e2e_test.go`: temp `HOME`, project, profile config, and POSIX fake `claude` and `codex` scripts on `PATH`. Fake Claude emits stream-json with several main-thread assistant messages (one repeated), a subagent assistant event, and a `result` with `modelUsage.<model>.contextWindow`; on resume it emits only new messages. Fake Codex emits `thread.started`/`turn.completed` on stdout and writes a rollout under `$HOME/.codex/sessions` including a compaction. Workflow: Claude step (new session), Claude step resuming it, Codex step, shell step.
- Journey: run the workflow to completion and wait for the top-level `run_end` audit event.
- Assertions: each agent step record in `run-metrics.json` has the expected last observed request input, peak, and window; the resumed Claude step reports its own values, not differences; the subagent and compaction usage are absent from context values; the shell step has no `context`; `agent_session_context` has one Claude entry with attempt count 2 and one Codex entry, both coverage `complete`; run token totals match the totals expected from the fixtures' usage alone.
- Execution: `cmd/agent-runner` test in the CI `test` job.

### E2E-002: Autonomous-interactive Claude step records transcript context and statusline window
- Covers: `interactive-claude-usage` Transcript context for autonomous-interactive Claude steps, Context window limit from the statusline report, Unusable transcript evidence limits context
- Surface: built binary plus the `statusline-record` command, as in the existing `TestInteractiveClaudeUsageE2E001`.
- Setup: extend that test's fixture process so its statusline payloads include `context_window.context_window_size`, its transcript holds an inherited session with earlier messages before the step's span, and a second case ends with an unparseable entry.
- Journey: run the workflow's autonomous-interactive step to completion.
- Assertions: the step's context is computed only from the span's main-thread messages, synthetic message ignored, window limit equals the statusline value; the damaged case is `partial` with the readable peak and null last value; usage and cost assertions already in the test still hold.
- Execution: `cmd/agent-runner` test in the CI `test` job.

### E2E-003: Real Claude and Codex output yields available context
- Covers: `agent-context-size` Context sources by CLI against current real CLI formats (design risk: undocumented Codex rollout records, Claude `modelUsage.contextWindow`)
- Surface: existing opt-in `TestClaudeHeadlessRealAgentE2E` and `TestCodexHeadlessRealAgentE2E`.
- Setup: existing real-agent setup with authenticated CLIs; no new suite. Native session storage must be isolated: the two tests launch the CLIs with the environment from `isolatedRealAgentCallEnv` (`cmd/agent-runner/real_agent_e2e_test.go`): a temp `HOME`, and for Codex a temp `CODEX_HOME` holding a copy of `auth.json`, so Claude transcripts and Codex rollouts are written under the temp directory and the user's `~/.claude` and `~/.codex/sessions` are only read for credentials. If a CLI cannot authenticate under that isolation, the test skips with that reason and the fake-CLI coverage in E2E-001 stands.
- Journey: the existing headless real-agent runs.
- Assertions: each agent step's context status is `available` with non-null positive peak and last values, peak ≥ last, and non-null window limit; Codex source is the rollout.
- Execution: `./.validator/go-offline.sh go test -count=1 -timeout 30m -tags e2e_agents ./cmd/agent-runner -run '^(TestClaudeHeadlessRealAgentE2E|TestCodexHeadlessRealAgentE2E)$' -v` (opt-in, not in CI; run before acceptance when credentials exist). Do not run the whole `make test-e2e-headless-agents` target, which runs every headless real-agent test.

## Acceptance Testing Envelope

- Environments and sandboxes: local macOS checkout; temp directories for workflow runs; `./dev.sh` to run the CLI from source; fake CLIs from the E2E fixtures.
- Credentials and secrets: the machine's existing Claude Code and Codex CLI authentication (Paul's accounts), used only through the CLIs; no other secrets.
- Authorized effects: E2E-003's two scoped tests plus a few small real headless Claude and Codex workflow runs in a temp project (well under $1 total model cost), every one with isolated native session storage (temp `HOME`, temp `CODEX_HOME` with a copied `auth.json`, as in `isolatedRealAgentCallEnv`); temp directories may be deleted afterwards.
- Off limits: editing or deleting anything under `~/.claude`, `~/.codex/sessions`, or other users' runs (read-only inspection is allowed); Agent Factory artifacts and live factory runs; pushing, opening PRs, or changing GitHub state.
- Permitted substitutes: fake CLI scripts when a real CLI is unavailable, unauthenticated, or cannot authenticate under isolated session storage, provided the report states that real-format verification was not performed. Autonomous-interactive steps cannot be launched under a synthetic PTY (see `CLAUDE.md`), so interactive behavior may be verified through E2E-002 evidence and by running the interactive collector over copies of real transcripts.
- Known risk areas: Codex rollout format drift (`token_usage_record`, `compacted`) and the end-of-file unclassified-record rule; Claude compaction assumed not to emit a main-thread assistant event; timestamp-based Codex invocation span; prior defect clusters in cumulative usage attribution on resumed sessions and in interactive transcript span handling. Accepted limitations: Cursor, Copilot, OpenCode, and autonomous-interactive Codex report `unsupported`; artifacts rewritten by an older binary lose context fields.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| agent-context-size: Per-attempt context measurement | INT-001 | E2E-001, E2E-003 | — |
| agent-context-size: Eligible observations | INT-001 | E2E-001 | — |
| agent-context-size: Context size is a gauge | — | E2E-001 | — |
| agent-context-size: Context sources by CLI | INT-001 | E2E-001, E2E-003 | — |
| agent-usage-collection: Context observations collected with usage | INT-001 | E2E-001 | — |
| agent-usage-collection: Non-agent steps carry no context measurement | INT-002 | E2E-001 | — |
| interactive-claude-usage: Transcript context | — | E2E-002 | — |
| interactive-claude-usage: Context window limit from the statusline report | — | E2E-002 | — |
| interactive-claude-usage: Unusable transcript evidence limits context | — | E2E-002 | — |
| run-metrics-artifact: Context measurement in step records | — | E2E-001 | — |
| run-metrics-artifact: Agent-session context summary | INT-002 | E2E-001 | — |
| run-metrics-artifact: Context measurement excluded from totals | INT-002 | E2E-001 | — |
| run-metrics-artifact: Artifacts without context measurements | INT-002 | — | — |
