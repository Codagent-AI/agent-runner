## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

Unit tests carry most of the load: `runretention.Plan` is a pure function (classes, limits, groups,
count ordering, grace), and settings parsing, trash naming, and throttle arithmetic are isolated
logic. The obligations below target what unit tests cannot prove: behavior against a real filesystem
with real permissions and real run locks, interleaving with the real resume path, cross-process
single ownership, and the end-to-end wiring from a real `agent-runner` process start through the
exit-time notice.

All tests below must sandbox `HOME` to a temporary directory. No test may read or write the
developer's real `~/.agent-runner`.

## Integration Tests

### INT-001: Sweep over a realistic multi-project run tree
- Covers: run-retention — Retained run scope, Run activity age, Run classification, Age-based removal, Per-project count limit, Audit-linked groups, Read-only content removal, Removal is atomic (trash finishing)
- Boundary: `internal/runretention` sweep (classify → plan → remove) against the real filesystem, real `runlock`, real `stateio` state files, and `internal/runs` listing
- Setup: temp `HOME` with two projects under `.agent-runner/projects/*/runs/`, each seeded with fixture run directories whose `audit.log`/`state.json`/directory mtimes are set with `os.Chtimes`: finished (31 d and 5 d), resumable (45 d and 91 d), stateless legacy (31 d), damaged `state.json` (100 d), unreadable lock (100 d, lock chmod `000`), a finished source + finished audit pair (40 d) with a `0500`/`0400` sealed `audit-snapshots/<id>/`, a finished source (60 d) whose audit run is resumable and recent, a source with a dangling audit link, a run containing a symlink to a file outside the tree, 103 recent finished runs in one project, a leftover `.pruning-<id>` directory containing sealed content, `~/.agent-runner/onboarding/runs` with an old run, a third project whose `runs` is a symlink to an external directory of old run-shaped directories, and a fourth project directory that is itself a symlink to an external tree. A variant runs the same fixture with `~/.agent-runner` itself a symlink to another temp directory. Injected clock; activation state 8 days old.
- Action: run one sweep synchronously and wait for completion.
- Assertions: exact set of surviving run directory names per project; the 3 least recently active of the 103 recent runs are gone; the sealed pair is fully gone; the resumable-audit group and damaged/unreadable-lock runs survive and appear in the returned warnings by path; the symlink target file still exists; no `.pruning-*` remains; onboarding run untouched; both symlinked-parent targets are byte-for-byte unchanged and each skipped path is named in a warning; the relocated-root variant removes the same runs as the baseline; `runs.ListForDir` returns only surviving runs; the sweep returns no error.
- Execution: `go test ./internal/runretention -run TestSweepIntegration` (part of `make test`)

### INT-002: Resume and pruning exclusion across interleavings
- Covers: run-lock — Lock acquisition is exclusive; Resume requires the run to still exist under its lock; Active lock refuses concurrent run (retention holds the lock); run-retention — Exclusion with concurrent runs
- Boundary: real `runner.PrepareResume` / `initRunState` and `runretention` removal sharing a real run directory and `runlock`
- Setup: temp `HOME` with an abandoned resumable run (91 d) of a shell-only fixture workflow; a test seam that pauses resume after its initial state read and a seam that pauses removal after acquiring the lock and before renaming.
- Action: (e) with the run's lock file holding a dead PID, run a resume in one OS process and a removal in another, both paused (via helper env vars) after observing the stale lock and released together, repeated many times; (a) pause resume after its state read, remove the run, release resume; (b) pause removal while holding the lock, invoke resume, then release removal; (c) start resume holding the lock (paused at first step), then run removal; (d) mark the run completed between resume's state read and lock.
- Assertions: (a) resume returns `ErrSessionNotFound`, executes no steps, and no directory exists at the original path; (b) resume fails with the "run already in progress (PID …)" error and the run is then removed; (c) removal skips the run, it remains, resume completes; (d) resume returns `ErrAlreadyCompleted` and executes no steps; (e) in every repetition exactly one side proceeds — either the run is removed and resume fails with no steps executed, or resume runs and the run survives — never both.
- Execution: `go test ./internal/runner -run TestResumeRetentionExclusion` (part of `make test`)

### INT-003: Single sweeper and throttling across processes
- Covers: run-retention — Sweep trigger and throttling (concurrent fresh runs, daily throttle, interrupted sweep retry)
- Boundary: two real OS processes (test binary re-exec via a helper env var) sharing `~/.agent-runner/retention/` lock and state
- Setup: temp `HOME` with eligible runs and activation older than 7 days; a deletion hook in the helper that blocks until signaled so the sweeps overlap.
- Action: start both helpers' background sweeps concurrently; then start a third after completion; then simulate an incomplete sweep (kill a helper mid-sweep) and start helpers at +30 min and +2 h with an injected clock.
- Assertions: exactly one helper performs removals and neither blocks waiting; third helper does not sweep (completed within 24 h); after the kill, the +30 min helper does not sweep and the +2 h helper does, finishing any `.pruning-*` left behind.
- Execution: `go test ./internal/runretention -run TestSweepAcrossProcesses` (part of `make test`; POSIX only, skipped on Windows like existing fake-CLI tests)

### INT-004: Audit linkage writers racing group removal
- Covers: run-retention — Audit-linked groups (membership frozen during removal; Replay racing group removal)
- Boundary: real `devaudit.Replay` (and link-state update path) with a stub launcher, sharing the source directory's audit-linkage claim with `runretention` group removal; real filesystem and locks
- Setup: temp `HOME` with an eligible finished source run carrying metrics for one execution session and an existing finished audit sibling, both aged past the limit; test seams pausing Replay (i) before taking the claim and (ii) after creating the audit sibling but before appending the source link; a seam pausing removal after re-discovery.
- Action: (i) start removal, pause after it holds the claim; start Replay; release removal. (ii) start Replay, pause after creating the sibling; run a sweep; release Replay. (iii) pause removal after re-discovery, attempt a link-state update for the existing audit; release.
- Assertions: (i) Replay fails with a not-found error, no new audit directory exists, the source directory is not recreated; (ii) the sweep skips the group (membership changed or claim held), and after Replay completes the new audit run's source still exists; (iii) the update fails without recreating the source; no audit run ever exists whose recorded source directory is missing.
- Execution: `go test -tags dev_audit ./internal/devaudit -run TestReplayRetentionRace` (part of `make test`)

### INT-005: Malformed settings preserve an opt-out
- Covers: run-retention — Retention policy configuration (malformed settings file removes nothing)
- Boundary: real `settings.yaml` on disk → `usersettings.LoadRunRetention` → sweep
- Setup: temp `HOME`, retention activated 8 days ago, eligible old runs; `settings.yaml` successively containing `run_retention: {enabled: false}` then invalid YAML, a scalar root, `run_retention: 7`, and an unreadable file (mode `000`); plus a file with an invalid `autonomous_backend` alongside `run_retention.enabled: false`.
- Action: run a due sweep for each variant.
- Assertions: no run removed in any variant; malformed/unreadable variants emit a warning that the retention policy could not be read; the invalid-`autonomous_backend` variant honors `enabled: false` without warning about retention.
- Execution: `go test ./internal/runretention -run TestMalformedSettingsFailClosed` (part of `make test`)

## End-to-End Tests

### E2E-001: Upgrade notice, grace period, then cleanup via the real CLI
- Covers: run-retention — Upgrade grace period and activation notice; Sweeps never impede the new run; Sweep trigger; Retention policy configuration (defaults); audit-log-storage — Log persistence (log removed with run)
- Surface: built `agent-runner` binary, headless (`AGENT_RUNNER_NO_TUI=1`), running a shell-only workflow in a temp project, following the pattern of `cmd/agent-runner/smoke_headless_integration_test.go`
- Setup: temp `HOME` with no `settings.yaml`; the temp project's run directory seeded with 5 finished runs aged 40 d, one resumable run aged 45 d, one resumable run aged 100 d, and 2 recent finished runs.
- Journey: (1) run the workflow; (2) run it again immediately; (3) rewrite `~/.agent-runner/retention/state.json` so activation is 8 days old and the last completed sweep 25 h old; run the workflow a third time; (4) `--resume <id>` for a removed run.
- Assertions: (1) exit 0; stderr contains exactly one activation notice naming the 30/90/100 policy, a count of 6, and `run_retention.enabled: false`; no seeded run removed; (2) no notice; nothing removed; (3) exit 0; the five 40 d runs and the 100 d resumable run (including their `audit.log` files) are gone; the 45 d resumable run, the 2 recent runs, and all three new runs remain; the process exits no later than the 2 s bound plus a generous CI scheduling margin (e.g. 5 s total) after the workflow's last step finishes; (4) exits non-zero with "session not found".
- Execution: `go test -tags dev_audit ./cmd/agent-runner -run TestRunRetentionE2E` (part of `make test`; POSIX only)

### E2E-002: Opt-out and non-triggering entry points
- Covers: run-retention — Retention disabled; Disabled before activation; Explicit session directory does not sweep; Resume does not sweep; Invalid value falls back to default
- Surface: built `agent-runner` binary, headless
- Setup: temp `HOME` seeded with old finished runs and activation older than 7 days.
- Journey: (1) with `run_retention.enabled: false`, run the workflow; (2) remove the setting, run with `--session-dir <tmp>`; (3) resume an unfinished run; (4) with `run_retention.max_age_days: -5`, run the workflow; (5) in a fresh `HOME` with `enabled: false`, run the workflow.
- Assertions: (1)–(3) no seeded run removed, no retention output; (4) exit 0, stderr contains a run-retention warning naming `max_age_days`, and 40 d runs are removed under the 30-day default; (5) no notice and no `retention/state.json` activation recorded.
- Execution: `go test -tags dev_audit ./cmd/agent-runner -run TestRunRetentionOptOutE2E` (part of `make test`; POSIX only)

## Acceptance Testing Envelope

- Environments and sandboxes: local macOS/Linux checkout; `./dev.sh` or a built binary with `HOME` pointed at a scratch directory (e.g. `mktemp -d`); shell-only fixture workflows and the repo's fake agent CLI stubs; Python `pty.fork` + `pyte` to drive the live TUI and verify the notice/warnings print only after the TUI releases the terminal (see CLAUDE.md "Exercising the TUI From an Agent Session"). The retention state file may be edited directly to move activation/throttle times.
- Credentials and secrets: none required.
- Authorized effects: creating, aging (`touch -t`/`os.Chtimes`), sealing (`chmod`), and deleting run directories inside the scratch `HOME`; spawning short-lived local processes (e.g. `sleep`) to hold run locks. Clean up the scratch directory afterward (restore write permission first).
- Off limits: the real `~/.agent-runner` of the machine's user (it holds live run history); any real agent CLI invocation that bills; GitHub/remote operations; `make build` output replacing an installed binary.
- Permitted substitutes: fake agent CLIs and shell-only workflows in place of real agents; editing `retention/state.json` in place of waiting 24 h / 7 d.
- Known risk areas: lock-then-rename races with resume (the proposal review's main finding); the reworked OS-lock `Acquire` (stale takeover, crash release, descriptor lifetime, `--resume` refusal messages still naming a PID); dev-audit Replay/Reconcile/completion now waiting on the audit-linkage claim (exercise with a `-tags dev_audit` build); audit-group ordering and sealed `0500`/`0400` snapshots; runs from older versions without `state.json`; the `.pruning-*` hidden names leaking into any run enumeration other than `runs.ListForDir` (list TUI, debug, devaudit status, inspect); stderr output colliding with the TUI; the 2 s exit wait on very large deletions (accepted limitation: work carries over); PID reuse making a stale lock look active (accepted: fail closed); resumable runs with all steps done but no `Completed` flag kept until 90 d (accepted).

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| run-retention: Retention policy configuration | INT-005 | E2E-001, E2E-002 | — |
| run-retention: Retained run scope | INT-001 | — | — |
| run-retention: Run activity age | INT-001 | — | — |
| run-retention: Run classification for retention | INT-001 | — | — |
| run-retention: Age-based removal | INT-001 | E2E-001 | — |
| run-retention: Per-project count limit | INT-001 | — | — |
| run-retention: Audit-linked groups | INT-001, INT-004 | — | — |
| run-retention: Exclusion with concurrent runs | INT-002 | — | — |
| run-retention: Removal is atomic from the user's view | INT-001 | E2E-001 | — |
| run-retention: Read-only content removal | INT-001 | — | — |
| run-retention: Sweep trigger and throttling | INT-003 | E2E-001, E2E-002 | — |
| run-retention: Sweeps never impede the new run | — | E2E-001, E2E-002 | — |
| run-retention: Upgrade grace period and activation notice | — | E2E-001, E2E-002 | — |
| audit-log-storage: Log persistence | — | E2E-001 | — |
| run-lock: Active lock refuses concurrent run (retention holds lock) | INT-002 | — | — |
| run-lock: Lock acquisition is exclusive | INT-002 | — | — |
| run-lock: Resume requires the run to still exist under its lock | INT-002 | E2E-001 | — |
