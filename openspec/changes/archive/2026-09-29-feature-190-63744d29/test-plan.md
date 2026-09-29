## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

Unit tests carry most of the load. They cover snapshot parsing, per-window delta rules and reasons,
baseline selection, precision and limitations, and rollup arithmetic, all against in-memory inputs or
small fixture files.

The integration tests below exist because the risky failures in this change sit at boundaries unit
tests cannot see:

- the real Codex-home filesystem layout, including the private `CODEX_HOME` symlinks;
- evidence travelling from `InvokeAgent` through the audit terminal event into the collector and the
  persisted `run-metrics.json`, and surviving resume;
- the agent-call record path;
- the Validator measurement-head refresh cycle that rebuilds nested projections.

The end-to-end obligations extend the existing opt-in real-agent tests. They are the only way to prove
that real Codex output and the real session-log format produce evidence. That is the issue's
"a real run shows per-step limit deltas" acceptance.

## Integration Tests

### INT-001: Reader resolves and reads real Codex-home layouts
- Covers:
  - `codex-rate-limit-capture`: Rate-limit evidence comes only from Codex-reported state;
    Attempt-bounded snapshots; Pre-launch start baseline with provenance (same-thread and
    cross-thread, including account, freshness, and date-directory search); Capture failures are
    isolated.
- Boundary: `cli.CodexRateLimitReader` together with the shared Codex-home resolver, on a real
  filesystem. Includes a private Runner home created by `prepareCodexRunnerHome`, which symlinks
  `sessions/`.
- Setup: a temp source `CODEX_HOME` holding `sessions/YYYY/MM/DD/rollout-<local-ts>-<id>.jsonl`
  files. Fixtures are copied from the observed codex-cli 0.157.1 shape (`session_meta` with
  `creator_account_id`, and `event_msg`/`token_count` with `rate_limits`). Cases:
  - a resumed thread with pre-launch and in-attempt snapshots;
  - a fresh thread plus a second same-account session within 10 minutes;
  - a different-account session;
  - a session older than 10 minutes;
  - a pair of sessions that straddles local midnight, so it spans two date directories;
  - a missing log;
  - a log with one malformed line.
- Action: resolve the home both through `$CODEX_HOME` and through the private home. Read evidence for
  fixed `StartedAt`/`EndedAt` values.
- Assertions:
  - the issue fixture gives start 40, end 43, primary delta 3 (`approximate`, `same-thread`,
    limitations `account-wide` and `coarse-precision`);
  - a cross-thread case gives provenance `cross-thread` with the exact `baseline_gap_ms` and
    limitation `unobserved-gap`;
  - different-account and stale candidates give `no-baseline`;
  - the midnight case finds the baseline in the previous day's directory;
  - a missing log gives `session-log-unavailable`;
  - a malformed line is skipped and the valid snapshots are kept;
  - no account identifier appears anywhere in the serialized evidence;
  - the real fixture files are unchanged after reading (byte comparison).
- Execution: `go test ./internal/cli -run CodexRateLimit` (part of `make test` and CI).

### INT-002: Native Codex step evidence reaches `run-metrics.json` and survives resume
- Covers:
  - `codex-rate-limit-capture`: Rate-limit evidence for every measured Codex attempt (headless);
    Capture failures are isolated.
  - `run-metrics-artifact`: Codex rate-limit evidence on attempt records; Run-level Codex rate-limit
    rollup.
- Boundary: workflow runner → agent executor → `InvokeAgent` → Codex adapter plus the real reader →
  `step_end` audit data → `metrics.Collector` → atomic artifact write → rehydrate on resume.
- Setup: a temp run directory and a temp `CODEX_HOME`. Use the existing stub process-runner pattern,
  or a fake `codex` script on `PATH`. The stub emits `thread.started`/`turn.completed` stdout and, as a
  side effect, appends `token_count` events stamped inside the attempt to that thread's rollout file.
  The workflow has four steps:
  - a fresh Codex step;
  - a Codex step that resumes the same session;
  - a Claude step using the stub Claude fixture;
  - a shell step.

  A second workflow variant makes the Codex log unreadable for one step. A third, resumed variant
  switches the synthetic `creator_account_id` between the pre-interrupt and post-resume Codex steps.
  It also advances the primary `resets_at` for a later step on the second account, so that the run
  crosses both an account switch and a reset.
- Action: run the workflow. Interrupt it after the first Codex step, resume it, and let it complete.
- Assertions:
  - both Codex records carry `codex_rate_limits`;
  - the fresh step without a qualifying baseline shows an end snapshot with delta reason
    `no-baseline`;
  - the resumed step shows a `same-thread` start and an available delta;
  - the Claude and shell records carry no evidence;
  - the rollup has `measured_attempts` 2, partial coverage, a sum equal to the resumed step's delta,
    and it persists after resume;
  - the schema version is still 4 and every pre-existing field is unchanged compared with a run
    without the stub log;
  - in the unreadable-log variant, the step outcome, usage and cost match the readable run and the
    evidence reason is `session-log-unavailable`;
  - in the account-switch variant:
    - the rollup has distinct window groups for each (account scope, reset) pair;
    - no sum or run span combines groups;
    - the pre- and post-resume attempts on the first account share one scope, which stays stable
      across the resume;
    - neither raw account ID appears anywhere in `run-metrics.json` or the audit log.
- Execution: `go test ./internal/runner` or `./internal/exec` (CI via `make test`).

### INT-003: Codex agent-call evidence stays on the call record
- Covers:
  - `codex-rate-limit-capture`: agent call scenario.
  - `run-metrics-artifact`: Codex rate-limit evidence on attempt records (for `agent-call` records);
    the rollup counts calls separately.
- Boundary: `AgentCallHandler` → child `InvokeAgent` (Codex) → `agent_call_end` data → collector.
- Setup: the existing agent-call test runner (`callTestRunner`-style), with a Claude parent calling a
  Codex child. The child stub writes rollout events to a temp `CODEX_HOME`. Add a variant with two
  concurrent Codex calls whose attempt intervals overlap.
- Action: execute the parent step, which makes the calls.
- Assertions:
  - the `agent-call` record carries evidence for the child's thread and the parent record carries
    none;
  - an idempotent retry does not duplicate evidence or double-count it in the rollup;
  - the overlapping variant makes the group sum unavailable with reason and limitation `overlapping-attempts`.
- Execution: `go test ./internal/exec -run AgentCall` (CI).

### INT-004: Nested Validator enrichment survives refresh, revision, conflict, and resume
- Covers:
  - `run-metrics-artifact`: Durable rate-limit enrichment for nested Validator attempts.
  - `codex-rate-limit-capture`: nested attempt bounded by lifecycle.
- Boundary: `Collector.IncorporateValidator` with real contract validation
  (`measurements.ValidateRecord`), the injected reader, `refreshMeasurementsLocked`, and
  persist/rehydrate through `NewCollector` on the same directory.
- Setup: valid Validator `model_attempt` records built from existing measurement test fixtures, with
  adapter `codex`. Cases:
  - a record carrying a `provider_native_usage` row `provider_session_id` that points at a temp
    rollout, including events just outside the lifecycle bounds (1s before start, 1s and 3s after
    end);
  - a record without `provider_session_id`;
  - a later completion revision with a new `ended_at`;
  - a same-revision record with a different digest (conflict).
- Additional cases:
  - a second completion revision whose `provider_session_id` log has been deleted;
  - a head with an unsupported measurement version in the current scope.
- Action:
  - import the records;
  - import an unrelated record to force a refresh;
  - import the revision (reader installed);
  - import the revision whose log is missing (reader installed);
  - rehydrate a new collector with no reader, then import another changed-bounds revision;
  - import the conflicting record and the unsupported-scope head.
- Assertions:
  - in-bounds events are used: the event 1s after end is included and the events 1s before start and
    3s after end are excluded;
  - evidence survives the unrelated refresh;
  - the stored `MeasurementHead.Record` bytes are identical before and after enrichment;
  - the revision triggers an immediate recapture against the new bounds;
  - the missing-log recapture shows `session-log-unavailable` and none of the earlier captured values;
  - after rehydrate without a reader, the changed-bounds revision shows `stale-enrichment` and none of
    the earlier values;
  - the record without a provider session shows `session-unidentified` and counts in coverage;
  - the conflicting head shows `excluded-measurement`, is excluded from sums, and counts in coverage;
  - the unsupported-scope head has no projection, no evidence, and is not counted.
- Execution: `go test ./internal/metrics -run RateLimit` (CI).

## End-to-End Tests

### E2E-001: Real headless Codex run records per-step limit evidence
- Covers: the issue acceptance "a real run shows per-step limit deltas for its Codex steps", i.e.
  `codex-rate-limit-capture` for headless attempts and `run-metrics-artifact` for records and the
  rollup.
- Surface: the built `agent-runner --headless` binary running a catalog workflow. Extends
  `TestCodexHeadlessRealAgentE2E` (fresh step plus resume step).
- Setup: the existing real-agent E2E harness with a real, authenticated Codex CLI and the harness's
  process-local environment.
- Journey: run the fresh and resume Codex steps to completion.
- Assertions (always):
  - both Codex step records in `run-metrics.json` carry `codex_rate_limits` with valid vocabulary;
  - the run-level rollup exists with `measured_attempts` 2;
  - the run completes exactly as it does today.
- Assertions (when the account reports rate limits, detected by the fresh step's evidence being
  `captured`), which must all hold:
  - both steps are `captured` with end snapshots;
  - the resume step has a `same-thread` start;
  - the resume step's primary delta is available;
  - end `used_percent` ≥ start, in the same reset window.

  A resume step that yields only an unavailable delta fails the test. The one exception is
  `window-reset` when the two snapshots genuinely straddle a reset: that run is retried once, and
  fails if it straddles again.
- When the environment reports no rate limits (`no-snapshots`, e.g. API-key auth), the test asserts
  the explicit reason and then skips with "delta acceptance unverified: account reports no
  rate_limits". That outcome does NOT satisfy the issue's real-run acceptance. Acceptance must then
  be re-run on a subscription account and must not be reported as verified.
- Execution: `E2E_AGENTS=codex make test-e2e-headless-agents` (opt-in, real credentials; not in CI).

### E2E-002: Real interactive Codex step records evidence or an explicit reason
- Covers: `codex-rate-limit-capture` for interactive Codex steps (identified or unidentified session),
  and the Codex-home resolver change in interactive discovery.
- Surface: extends `TestCodexInteractiveRealAgentE2E`.
- Setup: the existing PTY-driven real-agent harness.
- Journey: run the existing interactive Codex scenario.
- Assertions: the interactive Codex step record carries `codex_rate_limits`, either captured or
  unavailable with `session-unidentified`/`no-snapshots`. Session discovery and resume behave exactly
  as before, which guards against a regression from the resolver change.
- Execution: `E2E_AGENTS=codex make test-e2e-interactive-agents` (opt-in; not in CI).

## Acceptance Testing Envelope

- **Environments and sandboxes:**
  - this local checkout via `./dev.sh`, with the Runner's normal run directories under
    `~/.agent-runner/projects/...`;
  - temp directories used as synthetic `CODEX_HOME`;
  - the real-agent E2E harness environment.
- **Credentials and secrets:** the user's authenticated Codex CLI (ChatGPT subscription, which reports
  `rate_limits`; observed plan `prolite`) in `~/.codex`, and the Claude CLI credentials already used
  by the E2E harness. No new secrets are needed.
- **Authorized effects:**
  - a small number of short real Codex invocations (trivial prompts, at most about 10 in total). Each
    consumes a sliver of the user's shared usage limit, and nothing needs cleaning up;
  - writing run directories and temp files, which are removed when temp.
- **Off limits:**
  - editing, moving, or deleting anything in the real `~/.codex` (sessions, auth, config). Read access
    only;
  - long or looped Codex runs that would materially consume the usage limit;
  - the Agent Validator repository and any other repo;
  - pushing, releases, and production artifacts.
- **Permitted substitutes:** if Codex is unauthenticated or its account reports no `rate_limits`, use
  a fake `codex` executable on `PATH` that writes synthetic rollout logs into a temp `CODEX_HOME`, to
  explore behavior. The issue's real-run delta acceptance must then be reported as unverified rather
  than passed. Use
  synthetic Validator records carrying `provider_session_id` in place of a real Validator run, because
  the real Validator does not emit the field yet.
- **Known risk areas:**
  - Codex session-log format drift, since it is an undocumented format;
  - local-time date directories versus UTC event timestamps around midnight;
  - resumed threads leaking pre-launch snapshots into the end snapshot;
  - private-`CODEX_HOME` symlinks, and the interactive-discovery home resolution change (a regression
    risk for existing session discovery);
  - parallel Codex steps and calls, whose overlapping rollup sums are unavailable;
  - `used_percent` coarseness, which makes zero deltas possible and accepted;
  - fresh threads with no baseline show `no-baseline` by design;
  - nested Validator attempts show `session-unidentified` until Validator exports
    `provider_session_id`. This is an accepted limitation, not a defect.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| codex-rate-limit-capture: Rate-limit evidence for every measured Codex attempt | INT-002, INT-003, INT-004 | E2E-001, E2E-002 | — |
| codex-rate-limit-capture: Rate-limit evidence comes only from Codex-reported state | INT-001 | E2E-001 | — |
| codex-rate-limit-capture: Attempt-bounded snapshots | INT-001, INT-004 | E2E-001 | — |
| codex-rate-limit-capture: Pre-launch start baseline with provenance | INT-001, INT-002 | E2E-001 | — |
| codex-rate-limit-capture: Capture failures are isolated | INT-001, INT-002 | — | — |
| run-metrics-artifact: Codex rate-limit evidence on attempt records | INT-002, INT-003 | E2E-001 | — |
| run-metrics-artifact: Run-level Codex rate-limit rollup | INT-002, INT-003 | E2E-001 | — |
| run-metrics-artifact: Durable rate-limit enrichment for nested Validator attempts | INT-004 | — | — |
| Issue acceptance: real run shows per-step limit deltas | — | E2E-001 | — |
