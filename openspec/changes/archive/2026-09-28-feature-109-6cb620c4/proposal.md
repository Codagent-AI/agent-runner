## Why

Agent Runner keeps every run directory under `~/.agent-runner/projects/<project>/runs/<run-id>/` forever. Session state, step output, archived output, audit logs, and development-audit snapshots accumulate until a user notices a full disk and deletes them by hand. On the maintainer's workstation the tree held 827 run directories (6.8 GiB) *after* a manual cleanup; before it, audit snapshots alone had reached 41.8 GiB and nearly filled the disk (#104).

#104 shrank each snapshot but did not bound how many runs accumulate. The only existing cleanups are narrow: `newRunSessionCleanup` removes a session directory when fresh-run setup fails, and `pruneOutputArchives` trims archived step output inside one run. Nothing removes whole runs. As Agent Runner is increasingly driven by unattended automation (factory runs, development audits), growth is proportional to usage and silent, so it matters now rather than after the next disk-full incident.

**Verdict: go with caveats.** The problem is real, recurring, and cheap to solve relative to its cost; the alternatives (documenting manual `rm -rf`, an external cron job) push the burden onto every user and fail on sealed read-only snapshots. The caveats are that automatic deletion of user data must be conservative about what it touches — active runs, resumable work, and audit runs that still depend on a source run's snapshot — and must never make starting a run slower or less reliable.

## What Changes

- Agent Runner automatically deletes whole run directories that fall outside a retention policy, opportunistically when a fresh run starts, without a separate job or command.
- A retention policy with defaults:
  - finished runs whose last activity is older than a maximum age (default **30 days**) are removed;
  - each project keeps at most a maximum number of finished runs (default **100**), evicting the least recently active first;
  - resumable (inactive, not completed) runs use a separate, longer abandonment age (default **90 days**) and are never count-evicted.
  Each limit can be disabled.
- The policy is user-configurable through a new `run_retention` key in `~/.agent-runner/settings.yaml` (finished-run maximum age, resumable-run abandonment age, maximum finished runs per project, and a master off switch).
- **Rollout on upgrade.** The first time an installation runs with retention, it records an activation time and prints a one-time notice stating the effective policy, how many existing runs it will make eligible, and how to change or disable it. Nothing is deleted until a **7-day grace period** after activation has elapsed, so an existing user can inspect or opt out before the backlog is removed.
- Safety rules (fail closed):
  - a run holding a live run lock, or whose lock status cannot be determined, is never removed;
  - count eviction applies only to runs with a successfully parsed `state.json` recording a definitive terminal status and resolvable audit linkage;
  - a run whose `state.json` exists but is unreadable, inconsistent, or partially written is protected and reported, not deleted;
  - a run with no `state.json` and no lock (partially created runs, or runs from older versions that removed state on completion) is eligible only by age, never by count;
  - a source run and its linked development-audit runs are retained or removed together; a group is protected if any member is protected, so an audit run never loses the snapshot it reads from the source run directory.
- Resume no longer recreates a missing run directory and re-verifies the run's state after taking the run lock, so a resume can never race a pruner into running against a deleted run.
- Pruning handles sealed read-only content (audit snapshots chmodded `0500`/`0400` by `sealSnapshot`) instead of failing with permission denied.
- Pruning failures are reported as non-fatal warnings; they never fail, block, or noticeably delay the new run.
- **Behavior change:** the `audit-log-storage` guarantee that audit logs are "never automatically deleted" is replaced — a run's `audit.log` is deleted along with its run directory when the run is pruned. Users who want indefinite retention can disable retention.

## Capabilities

### New Capabilities
- `run-retention`: retention policy, defaults and configuration, upgrade grace period and notice, fail-closed eligibility (active, resumable, stateless, damaged, audit-linked runs), group deletion ordering, sweep execution and throttling, handling of read-only content, and failure reporting.

### Modified Capabilities
- `audit-log-storage`: the "never automatically deleted" retention requirement changes so audit logs are removed together with runs pruned by `run-retention`.
- `run-lock`: lock acquisition becomes exclusive even under simultaneous stale-lock takeover (OS advisory lock, released on crash); resuming a run requires its directory and state to still exist after the lock is acquired; resume never recreates a missing run directory.

## Technical Approach

- **Placement.** A new focused package (e.g. `internal/runretention`) owns discovery, eligibility, and deletion. It uses `internal/runlock` for liveness and `internal/usersettings` for configuration. It reads run state itself with strict, fail-closed classification rather than reusing the display-oriented classification in `internal/runs`, which treats a directory with no state and no lock as completed and tolerates unreadable state. The runner invokes it from the fresh-run path in `internal/runner` after the new run's own lock is held, so the new run can never select itself.
- **Exclusion with resume.** The run lock inside the run directory remains the exclusion point. The pruner takes a candidate's lock with `runlock.Acquire`, re-reads its state under the lock, and only then renames it to a hidden trash name inside the same `runs/` directory. Resume is changed so it (a) does not `MkdirAll` its session directory — only fresh runs create one — and (b) re-reads and validates `state.json` after acquiring the lock. Every interleaving is then safe: if the pruner holds the lock, resume sees an active lock and refuses; if the pruner has renamed the directory, resume cannot acquire a lock in a directory that no longer exists; if resume holds the lock, the pruner skips the run.
- **Group deletion protocol.** For an audit-linked group, the pruner acquires and rechecks every member's lock before deleting anything, then removes audit runs first and the source run last. Every intermediate state after a crash is therefore a valid state — a source with fewer (or no) audit runs — never an audit run whose source snapshot is gone. Linkage is discovered from both sides (`Audit.SourceRunID` on audit runs, `Audit.Links` on source runs), and dangling links to already-deleted audit runs are tolerated. The next sweep re-evaluates survivors normally.
- **Execution model.** A sweep covers every project under `~/.agent-runner/projects/*/runs/`, so projects that are never revisited still get cleaned. It runs as an in-process background worker started at fresh-run start, so the new run proceeds immediately. A global pruner lock under `~/.agent-runner/` guarantees a single owner across concurrent Agent Runner processes. The daily throttle marker records sweep *completion*, not launch; retries after an interrupted sweep are rate-limited separately (e.g. at most hourly). On exit the CLI waits a short bounded grace for an in-flight sweep, then abandons it. All work is idempotent: trash directories left by an interrupted deletion are finished by the next sweep, and discovery (reading a few hundred small `state.json` files) is cheap relative to deletion.
- **Read-only content.** Deletion restores owner write/execute permission on directories as it walks, then removes contents; it does not follow symlinks and never operates outside the `runs/` directory being swept.
- **Reporting.** The one-time activation notice goes to the user's terminal. Sweep failures and protected-but-damaged runs are collected and surfaced as non-fatal warnings (e.g. an audit event on the triggering run); exact surfaces are left to design.

Detailed data flow, throttling intervals, and reporting mechanics belong in `design.md`.

## Out of Scope

- A manual `prune`/`clean` CLI command or TUI action, and exposing retention in the settings editor UI (configuration is via `settings.yaml`; the activation notice explains how).
- Retention for run directories outside `~/.agent-runner/projects/*/runs/` — e.g. caller-supplied session directories and `~/.agent-runner/onboarding/runs`.
- Size-based (bytes) quotas, reporting reclaimable bytes, per-project or per-workflow retention overrides, and compressing or archiving old runs instead of deleting them.
- Automatic repair of runs with damaged state; they are protected and reported only.
- Changing the per-run content that is written (snapshot size is #104's concern) or `pruneOutputArchives`.
- Pruning on resume, list, or other non-run-start entry points.

## Impact

- **Code:** new `internal/runretention` package; `internal/runlock` `Acquire` reworked onto an OS advisory lock plus a source-level audit-linkage claim used by `internal/devaudit` (automatic launch, Replay, Reconcile, link updates); hook in `internal/runner/runner.go` fresh-run setup; resume path changes in `internal/runner/resume.go` / `PrepareRun` (no directory creation, state revalidation under lock); new `run_retention` settings in `internal/usersettings`; CLI exit path waits briefly for an in-flight sweep; possible small helpers from `internal/runlock`.
- **Users:** after the 7-day grace period, finished runs older than 30 days, finished runs beyond the 100 most recent per project, and resumable runs idle for over 90 days disappear from the run list and cannot be resumed or inspected; debug/replay/audit tooling can no longer target them. Existing installations see a one-time notice first and can disable retention before anything is deleted.
- **Persisted data:** `settings.yaml` gains an optional, additive key (unknown keys are already ignored by older binaries); `~/.agent-runner/` gains small activation, throttle, and pruner-lock files. No existing format changes.
- **Specs:** new `run-retention` spec; deltas to `audit-log-storage` and `run-lock`.
- **Dependencies:** none new.
