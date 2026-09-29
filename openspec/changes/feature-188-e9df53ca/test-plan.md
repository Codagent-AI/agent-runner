## Coverage Strategy

The specifications are the source of unit-test requirements. That includes the per-case Claude
fixture tables in `internal/cli` and the coverage and native-projection rules in `internal/metrics`
that `design.md` lists under Testing. This plan records only the additional integration and
end-to-end obligations, the acceptance testing envelope, and exceptional human-only obligations.

The main risks sit at seams that unit tests do not cross:

- the working directory and environment must be passed from the executor into the adapter's
  transcript lookup;
- the new `UsageRecord` fields must survive the audit event and metrics collector into
  `run-metrics.json`;
- a step that inherits a session must not pick up an earlier step's subagents once the whole
  pipeline runs.

## Integration Tests

### INT-001: Executor-to-collector wiring for subagent usage
- Covers:
  - `claude-subagent-usage`: subagent usage collection, attribution to the spawning invocation,
    and overridden Claude config directory.
  - `agent-usage-collection`: main-thread and subagent usage allocations.
  - `run-metrics-artifact`: subagent usage in the artifact.
- Boundary: The real `internal/exec` agent invocation path (`invocation.go` →
  `extractAgentUsage` → `ClaudeAdapter.ExtractUsageWithContext`), the real filesystem transcript
  lookup, and the real `metrics.Collector` that receives the step-end event.
- Setup:
  - A stub process runner returns a fixture Claude `stream-json` stdout, with `session_id`,
    UUIDs, and one `Agent` tool use.
  - A `t.TempDir()` Claude config tree holds the parent transcript and
    `subagents/agent-*.jsonl` plus `.meta.json` files. The subagent runs on a different model
    from the main thread.
  - The invocation environment sets `CLAUDE_CONFIG_DIR` to that tree. The workdir is a temp
    directory.
- Action: Execute a headless Claude agent step through the executor with the collector attached.
- Assertions:
  - The collected step record's `usage.allocations` contains a `main` allocation (main-thread
    model) and a `subagent` allocation (subagent model, agent type, tool-use ID).
  - `usage.tokens` and `usage.token_totals` equal the sum of the two allocations.
  - `subagent_collection` is `complete`.
  - `estimated_api_cost_usd` equals the fixture's `total_cost_usd`.
  - The step's native measurement has version 2, lists both allocations with distinct observed
    identities, and has null `unallocated_usage`.
- Execution: `internal/exec` test (for example `agent_subagent_usage_test.go`), `make test`.

### INT-002: Partial subagent collection reaches run and session aggregates
- Covers:
  - `run-metrics-artifact`: partial subagent collection affects coverage, and other partial
    records keep their existing coverage.
  - `agent-usage-collection`: a partial subagent subtotal keeps the known subtotal.
  - `cost-capture`: Claude step cost covers subagent spend.
- Boundary: The same executor-to-collector path as INT-001, through the collector's
  `run-metrics.json` write.
- Setup:
  - Same as INT-001, but stdout and the parent transcript show two spawns, and only one
    subagent transcript exists.
  - The collector also receives a nested Validator measurement whose projection is `partial`,
    in a second execution session.
- Action: Run the step, finalize the collector, and read the written `run-metrics.json`.
- Assertions:
  - `totals.tokens` and `totals.token_totals` equal main + collected subagent.
  - `totals.usage_coverage` and `totals.token_total_coverage` are `partial`, and
    `totals.cost_coverage` is `complete`.
  - The Claude step's session rollup reports `partial` coverage. The session containing only the
    Validator attempt reports `complete` usage coverage.
  - The missing subagent appears as an unavailable allocation with reason
    `subagent-transcript-missing`.
  - Available native attempt token fields for the Claude step have `availability: "partial"`.
  - The step outcome is `success`.
- Execution: `internal/exec` or `internal/metrics` integration test, `make test`.
- Additional sub-cases, in the same test:
  - **Span unavailable.** Stdout has no spawns and no UUIDs that match the parent transcript.
    Assert `subagent_collection` is `partial` with reason `subagent-span-unavailable` in the
    written `run-metrics.json`, and that there are no subagent allocations.
  - **Invalid parent transcript line.** A malformed line and an unknown `tool_use` shape sit
    inside an otherwise established span. Assert that the known subtotal is kept, that the
    reason is `subagent-parent-transcript-invalid`, and that run usage coverage is `partial`.
  - **Allocation costs.** In every sub-case, each allocation's `cost` (main and subagent) is
    unavailable with a reason, and never zero.

## End-to-End Tests

### E2E-001: Headless run records subagent usage without double counting in an inherited session
- Covers:
  - `claude-subagent-usage`: attribution to the spawning invocation (inherited session does not
    double count), and a spawn missing from stdout is still collected.
  - `run-metrics-artifact`: subagent usage visible per step, and run totals include subagents
    once.
  - The issue's acceptance: the step measurement shows subagent tokens and model.
- Surface: The built `agent-runner` binary run with `--headless` against a project workflow,
  following the pattern of `cmd/agent-runner/repair_inline_e2e_test.go`.
- Setup:
  - Temp `HOME`, project dir, and smoke profile config (reuse `writeSmokeProfileConfig`,
    `writeProjectSmokeConfig`, and `buildAgentRunner`).
  - A fake POSIX `claude` on `PATH`. It emits `stream-json` for a fixed session ID and appends
    to that session's parent transcript under `$HOME/.claude/projects/<dir>/`.
    - Invocation 1 writes one subagent transcript plus sidecar.
    - Invocation 2 resumes the session and writes a second subagent. Its `Agent` tool use is
      only in the transcript, not on stdout.
  - The project directory name is deliberately not the adapter's encoding of the workdir, so
    the glob fallback is exercised.
  - The workflow has two agent steps; the second uses `session: inherit`.
- Journey: Run the workflow headlessly to completion, then read the run's `run-metrics.json`.
- Assertions:
  - Step 1 has exactly one subagent allocation with the fixture's subagent model and tokens.
  - Step 2 has exactly one subagent allocation: its own subagent, not step 1's.
  - `totals.tokens` equals the sum of both steps' main and subagent tokens, each counted once.
  - Both steps report `subagent_collection: complete`, and usage coverage is `complete`.
  - The run exits 0.
- Execution: `cmd/agent-runner/claude_subagent_usage_e2e_test.go`, `make test`. Skipped on
  Windows, like the existing fake-CLI E2E tests.

## Acceptance Testing Envelope

- **Environments and sandboxes:**
  - The local checkout, running the CLI from source with `./dev.sh`.
  - Temporary project directories and a temporary `HOME` or `CLAUDE_CONFIG_DIR` for isolated
    runs.
  - The fake-`claude` pattern from the E2E tests, for deterministic scenarios.
  - Historical factory artifacts under `~/.agent-factory/artifacts/*/attempt-*/` and real
    transcripts under `~/.claude/projects/`. These are **read-only** reference data for
    comparing the new extraction against real stdout and transcripts (for example, re-running
    extraction on a past `simplify` step's captured `.out` file).
- **Credentials and secrets:** The operator's existing Claude Code login on this machine. No
  other secrets are needed or permitted.
- **Authorized effects:**
  - At most three small real headless Claude runs through `./dev.sh`. Each is a one-step
    workflow whose prompt asks for a single cheap subagent (for example, an Explore agent
    listing one directory), with an estimated total spend under $2.
  - These runs may write their run directories under `~/.agent-runner/projects/...` and
    transcripts under `~/.claude/projects/...`. Leave them in place, or remove only the run
    directories this pass created.
- **Off limits:**
  - Starting or modifying agent-factory runs.
  - Pushing, publishing, or opening PRs.
  - Editing or deleting existing transcripts, factory artifacts, or other runs' directories.
  - Changing the user's Claude settings or `~/.claude` configuration.
  - Long or open-ended real agent sessions.
- **Permitted substitutes:** If the Claude login is unavailable or the budget is exhausted, use
  the fake `claude` executable with fixture transcripts, plus offline replay of historical
  captured stdout and transcripts. Record that real-CLI evidence was not obtained.
- **Known risk areas:**
  - The undocumented Claude Code transcript and sidecar formats (`Agent` vs `Task` tool name,
    streamed duplicate entries, `<synthetic>` model entries).
  - Truncated or hashed project directory names for long working directories.
  - Inherited-session attribution.
  - Main-thread model mislabeling by trailing subagent events.
  - Coverage regressions for runs containing nested Validator attempts.
  - Accepted limitations:
    - background subagents killed at CLI exit are under-reported and flagged `partial`;
    - allocation-level cost is always unavailable;
    - interactive Claude steps collect nothing.

## Human-Only Testing

### HT-001: Real factory run with `/simplify` shows subagent usage
- **Reason:** The issue requires evidence from a real agent-factory run. Launching that run
  requires the merged build installed in the factory environment and an operator-triggered
  factory pipeline, which is outside this repository and outside the acceptance envelope's
  authority and budget.
- **Prerequisites:**
  - INT-001, INT-002, and E2E-001 pass.
  - The acceptance pass has shown non-zero subagent allocations on a real or substitute
    headless Claude run.
  - The change is merged and released or installed where the factory runs.
- **Instructions:**
  1. Run any factory feature workflow that reaches the `verify` → `simplify` step.
  2. Open that attempt's `agent-runner-session/run-metrics.json`.
  3. Find the `simplify` step record.
- **Required decision or observation:** Confirm that the `simplify` step's `usage.allocations`
  include one or more `subagent` allocations with non-zero tokens and an observed model, and
  that `usage.tokens` exceeds the main-thread allocation's tokens.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| claude-subagent-usage: Subagent usage collection for headless Claude steps | INT-001 | E2E-001 | HT-001 |
| claude-subagent-usage: Attribution to the spawning invocation | INT-001 | E2E-001 | — |
| claude-subagent-usage: Missing or incomplete subagent evidence is explicit (config-dir override, missing transcript, invalid parent transcript) | INT-001, INT-002 | — | — |
| agent-usage-collection: Main-thread and subagent usage allocations | INT-001, INT-002 | E2E-001 | — |
| run-metrics-artifact: Subagent usage in the artifact | INT-001 | E2E-001 | HT-001 |
| run-metrics-artifact: Partial subagent collection affects coverage (including the visible collection reason) | INT-002 | — | — |
| cost-capture: Claude step cost covers subagent spend | INT-001, INT-002 | — | — |
