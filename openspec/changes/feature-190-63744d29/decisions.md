# Decisions — feature-190-63744d29

## propose

1. **Verdict: go, with caveats.** Alternatives: no-go, because account-wide deltas are noisy.
   Rejected: the issue explicitly asks us to record the concurrency limitation, not drop the data.
   Decision-bearing: yes.
2. **Source: read `rate_limits` from the Codex session log (`$CODEX_HOME/sessions/**/rollout-*-<thread_id>.jsonl`), not from stdout.**
   Alternatives: parse `codex exec --json` stdout; query the Codex app-server. Checked against
   codex-cli 0.157.1: exec JSON stdout has no `rate_limits`, and the app-server adds a new integration
   surface. The Runner already reads this directory for interactive session discovery.
   Decision-bearing: yes.
3. **(Superseded by design decision 1.) Bound the attempt with a pre-launch size marker on the log, so a resumed thread only gets the
   events it appended.** Alternative: timestamp filtering. The design may add timestamps as a fallback.
   Decision-bearing: no.
4. **(Superseded by PR-2 below.) Choose the start value in this order: the same thread's pre-launch snapshot, then the latest
   pre-launch snapshot for the same `limit_id`, then the first snapshot inside the attempt. The record
   stores which rule it used.** Alternative: always use the first in-attempt snapshot. Rejected because
   it understates single-request attempts. Decision-bearing: no; the design refines this.
5. **Compute deltas per window, only when both endpoints share `window_minutes`/`resets_at`. Otherwise
   the delta is unavailable with a reason, and every delta is marked approximate.** Alternative: clamp
   to 0 after a reset. Rejected because the specs forbid showing unavailable values as zero.
   Decision-bearing: no.
6. **Add fields to schema v4 without bumping the version.** Alternative: bump to v5. The spec requires
   a bump only for backward-incompatible changes. Decision-bearing: no.
7. **Run rollup: sum of attempt deltas, run-span change, and coverage.** Alternative: sum only. A group
   with parallel attempts leaves the sum unavailable because adding its deltas would double-count.
   Decision-bearing: no.
8. **(Extended by PR-1 below to include agent calls.) Scope: headless and interactive Codex agent steps, plus nested Validator Codex attempts through
   the existing `provider_session_id`, with no Validator changes.** Alternatives: native steps only,
   which under-delivers the issue's "every Codex attempt"; or changing the Validator contract, which
   would require work outside this repo. Decision-bearing: yes.
9. **Showing the data in the UI or audit report is out of scope.** The issue asks only for
   `run-metrics.json`. Decision-bearing: no.

## proposal-review

- **PR-1 (structural): applied.** Codex agent calls launch their own Codex CLI process and write their
  own measured call record (`internal/exec/agent_call.go`), so they are now in scope. The evidence goes
  on the call record and stays separate from the parent step's. Alternative: leave calls out and report
  them as a coverage gap. Rejected because the issue asks for every measured Codex attempt.
  Decision-bearing: yes; it widens scope within the issue's stated intent.
- **PR-2 (significant): applied.** This replaces decision 4. A first in-attempt snapshot is now only an
  end-only observation, and the delta is unavailable (`no-baseline`) unless a pre-launch baseline
  qualifies. Two baselines qualify: the same thread's pre-launch snapshot, or a cross-thread snapshot
  whose `limit_id`, `plan_type`, `window_minutes` and `resets_at` all match and that is newer than a
  freshness bound. For a cross-thread baseline, the unobserved gap before launch is recorded as a
  limitation. Alternative kept from the original proposal: fall back to the first in-attempt snapshot.
  Rejected because it records a false zero for single-request attempts. Trade-off accepted: fresh
  threads with no qualifying recent snapshot report an unavailable delta. Decision-bearing: yes; the
  design sets the freshness bound.
- **PR-3 (significant): applied.** Confirmed: `refreshMeasurementsLocked`
  (`internal/metrics/measurements.go`) drops and rebuilds nested Validator step projections from the
  measurement heads, so evidence written onto them would be lost. Validator rate-limit evidence now
  lives in a durable, Runner-owned enrichment. It is keyed by producer store plus model-attempt
  identity, kept apart from the accepted producer record, and rebuilt into projections and the rollup.
  Default for revised or conflicting heads: an enrichment whose attempt identity or lifecycle bounds no
  longer match its head is marked stale and shown as unavailable; the design refines this. Validator
  does not change. Decision-bearing: no; this is an implementation-placement correction within scope.
- No finding is direction-level, so no stop.

## spec

1. **Add the `run-metrics-artifact` changes as new ADDED requirements, not MODIFIED copies of "Artifact
   content".** Alternative: rewrite the existing large requirement blocks. The new evidence is
   additive, and separate requirements keep the change reviewable. Decision-bearing: no.
2. **Non-Codex attempts carry no rate-limit evidence and don't count in coverage.** Alternative: every
   attempt carries an unavailable block. Rejected: that adds noise and distorts coverage.
   Decision-bearing: no.
3. **Fixed vocabulary for unavailable reasons:** `session-unidentified`, `no-snapshots`,
   `session-log-unavailable`, `unparseable`, `no-baseline`, `window-reset`, `window-not-reported`,
   `inconsistent`, `stale-enrichment`. Limitations: `account-wide`, `coarse-precision`,
   `unobserved-gap`, `overlapping-attempts`. Alternative: free-text reasons. Rejected because they
   aren't testable. Decision-bearing: no.
4. **A decrease within the same window is `inconsistent` (unavailable), not a negative delta.**
   Alternative: clamp to 0 or report the negative number. Both would misreport. Decision-bearing: no.
5. **Rollup windows are keyed by limit identity + role (primary/secondary) + window length. Each window
   reports the sum, the run-span change and coverage; an overlapping group's sum is unavailable with
   reason `overlapping-attempts`.** Alternative: sum only. Rejected: parallel attempts double-count.
   Decision-bearing: no.
6. **The rollup is run-level only; there is no per-execution-session rate-limit rollup.**
   Alternative: mirror the per-execution-session rollups. The issue asks only for a per-run rollup, and
   per-attempt records already carry execution-session identity. Decision-bearing: no.
7. **Deferred to design:** the freshness bound and which local session logs are searched for a
   cross-thread baseline; the timestamp tolerance for nested lifecycle bounds; whether a stale
   enrichment is recaptured eagerly. Decision-bearing: no.
8. **Interactive and autonomous-interactive Codex steps are covered when their session is
   identified.** Otherwise the reason is `session-unidentified`. Consistent with proposal decision 8.
   Decision-bearing: no.

## design

1. **Bound attempts by event timestamps in `[StartedAt, EndedAt]`, not a pre-launch byte offset.**
   This supersedes proposal decision 3. Alternative: a byte offset, which needs the log path before
   launch (it doesn't exist for fresh threads) and can't serve nested attempts. Decision-bearing: no.
2. **A cross-thread baseline requires the same `creator_account_id` in both logs' `session_meta`,
   compared in memory and never stored.** Alternative: match on limit, plan and window only. Observed
   logs use the generic `limit_id` `codex`, so that isn't enough to rule out another account. Spec
   updated. Decision-bearing: no; this tightens proposal review PR-2.
3. **Freshness bound: 10 minutes.** Only local-date directories spanning the bound and files with
   mtime in the bound are searched. Alternatives: 1 minute (too strict for normal gaps between steps)
   or unbounded (unbounded unobserved consumption). Spec updated. Decision-bearing: no.
4. **Nested lifecycle bounds: strict at start, +2s tolerance at end.** Alternative: symmetric
   tolerance. Rejected: it could pull in a previous turn's snapshot. Spec updated. Decision-bearing: no.
5. **Recapture nested enrichment eagerly at revision import. `stale-enrichment` appears only when
   recapture is impossible.** Alternative: lazy recapture on refresh. Rejected: it does file IO on
   every refresh. Spec updated. Decision-bearing: no.
6. **Added reason `excluded-measurement` for conflicting or unsupported heads.** Spec updated.
   Decision-bearing: no.
7. **Inject the reader into `metrics.Collector` (`SetCodexRateLimitReader`) to avoid a `metrics → cli`
   import.** `exec` calls the reader directly for native attempts. Decision-bearing: no.
8. **Agent Validator's Codex adapter does not currently emit `provider_session_id`, so nested Codex
   attempts will report `session-unidentified` until it does.** Alternatives: heuristic cwd/time log
   matching, rejected because it misattributes parallel reviews; or requiring a Validator change,
   which is outside this repo and not needed for the issue's acceptance. The issue scopes Validator
   reviews "once those are measured". Recorded as a follow-up, not a stop. Decision-bearing: yes.
9. **Rollup lives at the artifact top level as `codex_rate_limits`, recomputed from steps on every
   refresh. Schema stays v4 and all fields are `omitempty`.** Decision-bearing: no.
10. **`discoverCodexInteractiveSession` uses the shared `$CODEX_HOME`/`~/.codex` resolver.** This
    keeps interactive discovery and the reader consistent. Decision-bearing: no.

## test-plan

1. **Four integration tests:**
   - INT-001, reader on a real filesystem, including private-home symlinks and midnight date
     directories;
   - INT-002, native step through to the artifact and resume;
   - INT-003, agent-call record;
   - INT-004, Validator enrichment lifecycle.

   Snapshot parsing and delta/rollup arithmetic are left to unit tests from the specs. Alternative:
   one large runner-level integration test. Rejected: it would hide which boundary failed.
   Decision-bearing: no.
2. **End-to-end coverage extends the existing opt-in real-agent tests (`TestCodexHeadlessRealAgentE2E`,
   `TestCodexInteractiveRealAgentE2E`) rather than adding a new CI E2E.** Real Codex credentials and
   usage are not available in CI. Assertions accept both captured and `no-snapshots` outcomes so they
   hold for API-key accounts. Decision-bearing: no.
3. **Acceptance envelope:**
   - authorizes at most about 10 short real Codex invocations against the user's subscription;
   - allows only read access to the real `~/.codex`;
   - allows a fake `codex` on `PATH` and synthetic Validator records as substitutes, because the real
     Validator doesn't emit `provider_session_id`.

   Alternative: forbid real Codex use. Rejected: the issue's acceptance explicitly requires a real
   run. Decision-bearing: no.
4. **Human-only testing: none.** Every check is observable in `run-metrics.json`. Decision-bearing: no.

## approach-review

- **AR-1 (high): applied.** The rollup is now partitioned by (opaque account scope, limit, role,
  window length, reset time). The account scope is `acct-` + a run-salted SHA-256 prefix of
  `creator_account_id`, which stays stable across resume and never exposes the raw ID. It is
  `unverified` when the ID is absent, and that group's sum and span are unavailable
  (`account-unverified`). No total is computed across accounts or resets.
  - Alternatives: a run-local ordinal, rejected because staying stable across resume would require
    persisting the raw ID; or marking the whole run unavailable after any account change, rejected
    because it loses valid per-account data.
  - Updated:
    - run-metrics spec: rollup requirement plus account-switch and account-not-established scenarios;
    - capture spec: account-scope sentence and scenario;
    - design: types, rollup rules, decision 8;
    - proposal: What Changes;
    - test plan: INT-002 account-switch and reset variant.

  Decision-bearing: yes; this changes the shape of the rollup.
- **AR-2 (high): applied.** E2E-001 now requires, whenever the account reports rate limits:
  - the resume step has a `same-thread` start;
  - it has an available primary delta with end ≥ start in the same window;
  - a genuine `window-reset` straddle is retried once.

  With no `rate_limits` in the environment, the test skips with "delta acceptance unverified" and does
  not satisfy the issue. The acceptance envelope now says a substitute run leaves acceptance
  unverified. Design testing section updated. Decision-bearing: no.
- **AR-3 (medium): applied, using the finding's alternative for unsupported heads.**
  - Conflicting heads, which are projected today, show `excluded-measurement` and count in coverage.
  - `unsupported_current_scope` heads stay unprojected, as `refreshMeasurementsLocked` does today,
    carry no evidence, and are not counted.
  - Rejected: adding minimal projections for unsupported heads. That would change an established
    exclusion for little value.
  - Spec, design (decision 10) and INT-004 updated.

  Decision-bearing: no.
- **AR-4 (medium): applied.**
  - A recapture whose read fails replaces the enrichment with the reader's own reason, bound to the
    revised head (`session-log-unavailable`, `unparseable`, `no-snapshots`).
  - `stale-enrichment` now means only that no reader was available when a changed-bounds revision was
    imported.
  - Old values are never shown in either case.
  - Spec scenarios split into "Recapture read fails" and "No reader available at revision import".
  - INT-004 now rehydrates without a reader before importing a changed-bounds revision, and adds a
    missing-log recapture case.
  - Design decision 9 added.

  Decision-bearing: no.
- No finding is direction-level, so no stop.

## tasks

1. **Exactly one implementation task, linking every definition artifact.** It follows the format of
   archived change `feature-109-6cb620c4`. The E2E obligations extend the existing opt-in
   `e2e_agents` tests: they must compile, and running them needs real Codex credentials, which CI
   doesn't have. Alternative: split the work into reader, native capture, enrichment and rollup
   tasks. Rejected because the instructions require a single task. Decision-bearing: no.
