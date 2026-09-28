# Decisions — feature-109-6cb620c4

| Step | Decision | Alternatives considered | Decision-bearing |
|------|----------|-------------------------|------------------|
| propose | Verdict: go with caveats (conservative deletion, never blocks run start). | No-go and document manual cleanup; external cron job. | Yes |
| propose | Configure retention via a new optional `run_retention` key in `~/.agent-runner/settings.yaml`. | `~/.agent-runner/config.yaml` (profile config); per-project `.agent-runner/config.yaml`; env vars. Settings file is per-user, already ignores unknown keys, so the change is additive. | Yes |
| propose | Defaults: max age 30 days (by last activity) and max 100 runs per project; each limit can be disabled. | Age-only; count-only; no default (opt-in). Issue asks for a sensible default; opt-in would not prevent the disk-full incident. | Yes |
| propose | Resumable runs are exempt from the count limit and removed only past max age ("clearly abandoned"); active (locked) runs are never removed. | Treat all non-active runs alike; never remove resumable runs. | Yes |
| propose | Source runs and linked audit runs are retained/removed as a group. | Prune independently (would break audit runs whose snapshot lives in the source run dir). | No |
| propose | Prune on fresh run start, sweeping all projects, throttled to once per day, off the critical path. | Current project only (abandoned projects never cleaned); every run start unthrottled; prune on resume/list too. | No |
| propose | Take candidate's run lock before deletion, rename to trash inside `runs/`, then delete, restoring owner write permission on sealed dirs. | Direct `os.RemoveAll` (fails on sealed snapshots, races resume, leaves half-deleted runs visible). | No |
| propose | Replace the `audit-log-storage` "never automatically deleted" requirement. | Keep audit logs forever while deleting the rest of the run (contradicts the issue's goal). | Yes |
| propose | Out of scope: manual prune command, settings-editor UI, onboarding/custom session dirs, size quotas. | Include them now. | No |

## Proposal review findings

| Step | Finding | Disposition | Decision | Alternatives considered | Decision-bearing |
|------|---------|-------------|----------|-------------------------|------------------|
| proposal-review | PR-001 rename moves the lock; a resume that read state earlier can `MkdirAll` the old path and run against deleted evidence | Applied (variant) | Keep the in-directory run lock as the exclusion point, but change resume to never create its session directory and to re-read/validate `state.json` after acquiring the lock; pruner re-reads state under the lock before renaming. Adds `run-lock` as a modified capability. | Reviewer's parent-level per-run claim checked before state read (a second lock namespace every resume path must honor; the resume-side change closes the same race with less new mechanism). | Yes |
| proposal-review | PR-002 group deletion on individual dirs can leave an audit run with a broken snapshot; other members unlocked | Applied (variant) | Acquire and recheck every group member's lock before deleting; delete audit runs first and the source last so every crash state is valid; discover links from both sides and tolerate dangling links. | Durable group-deletion marker with restart completion (unnecessary once ordering makes every intermediate state valid). | No |
| proposal-review | PR-003 background sweep has no lifetime guarantee; marker semantics undefined | Applied | In-process background worker, global pruner lock for single ownership, throttle marker records completion, separately rate-limited retries, bounded wait at CLI exit, idempotent trash finishing. | Detached pruner subprocess (adds a hidden CLI entry point and process management); fully synchronous sweep (delays run start). | No |
| proposal-review | PR-004 `internal/runs` classification treats stateless dirs as completed and tolerates unreadable state | Applied (partially) | Strict fail-closed classification in the pruner: count eviction only for parsed terminal state with resolvable linkage; damaged/unreadable state and unknown lock status protected and reported. Dirs with no `state.json` and no lock stay eligible by age only. | Protect all stateless dirs (rejected: older versions removed `state.json` on completion, so the existing backlog would never be cleaned, defeating the issue's goal). | Yes |
| proposal-review | PR-005 first run after upgrade can irreversibly delete a large backlog with no chance to intervene; age alone doesn't prove a resumable run is abandoned | Applied | Record activation time, print a one-time notice with effective policy, eligible run count, and how to disable; delete nothing until 7 days after activation. Resumable runs get a separate 90-day abandonment age. Add a master off switch. | Opt-in retention (would not prevent the disk-full incident the issue cites); reporting eligible bytes (requires walking GiBs; left out of scope). | Yes |

Updated defaults supersede the earlier propose row: finished runs 30 days / 100 per project; resumable runs 90 days; 7-day upgrade grace period.

## Specification decisions

| Step | Decision | Alternatives considered | Decision-bearing |
|------|----------|-------------------------|------------------|
| spec | `run_retention` keys: `enabled`, `max_age_days`, `resumable_max_age_days`, `max_runs_per_project`; `0` disables a limit; invalid/negative values fall back to the per-key default with a non-fatal warning. | Nested durations (`30d` strings); failing the run on invalid settings; treating invalid as disabled. | Yes |
| spec | Last activity = newest mtime of `audit.log`, `state.json`, run dir, falling back to run-ID start time (matches the run list's existing notion); a group's activity is its newest member. | Start time only; recursive newest mtime (expensive on large snapshots). | No |
| spec | Classification: active, lock-unknown, damaged (protected); finished (Completed=true); resumable (state present, not completed — includes failed runs); stateless (no state, no/stale lock). | Treat failed runs as finished (would count-evict runs the list offers to resume). | No |
| spec | Count limit counts finished units only; an audit-linked group counts as one unit; groups containing any non-finished member are neither counted nor count-evicted. | Count every run dir including audit runs individually. | No |
| spec | Throttle: no sweep within 24h of a completed sweep; incomplete sweep retried no sooner than 1h after it started; single sweeper across processes, others skip without waiting. | Sweep every fresh run; retry immediately. | No |
| spec | Activation notice shown once on the first due sweep, even with zero eligible runs; disabling retention before first use suppresses notice and activation. | Show notice only when eligible runs exist (users would not learn deletion is enabled). | No |
| spec | Resume after lock acquisition that finds the run completed is handled as a completed run (no steps executed). | Error out. | No |
| spec | Warning surface and exit-wait bound deferred to design (marked `deferred-to-design`). | Fix them in spec. | No |

## Design decisions

| Step | Decision | Alternatives considered | Decision-bearing |
|------|----------|-------------------------|------------------|
| design | Start the sweep from `cmd/agent-runner` `prepareFreshRun` after `PrepareRun` succeeds, only when Agent Runner allocated the session dir; `internal/runner` does not import retention. Spec refined: `--session-dir`, onboarding, and dev-audit runs do not trigger sweeps. | Hook inside `initRunState` (every test/audit/onboarding run would touch real user storage or need opt-out). | No |
| design | Retention warnings and the activation notice are printed to stderr when the process exits (after TUI teardown); exit waits at most 2 s for an in-flight sweep. Resolves both spec deferred-to-design markers. | Run logger (invisible in TUI); audit events on the triggering run (logger closed before sweep ends; mixes global housekeeping into run evidence). | No |
| design | Activation time recorded only after the notice is written; a crash before printing re-shows it later. Spec updated accordingly. | Record at scan time (grace could elapse unseen). | No |
| design | Trash name `runs/.pruning-<id>` (same dir ⇒ atomic rename); listings skip dot-prefixed entries; resume/inspect reject dot-prefixed IDs. | Global trash dir under `~/.agent-runner` (cross-filesystem rename risk). | No |
| design | Reuse `runlock.Acquire` for per-run exclusion and for a global sweeper lock at `~/.agent-runner/retention/lock`; sweep state in `~/.agent-runner/retention/state.json`. | New flock-based primitive. | No |
| design | Resume gets `Options.Resume`: no `MkdirAll`; `Acquire` ENOENT ⇒ new `ErrSessionNotFound`; state re-read under lock (missing ⇒ not found, completed ⇒ `ErrAlreadyCompleted`). | Parent-level claim file (rejected at proposal review). | No |
| design | Dedicated lenient `run_retention` parsing (`LoadRunRetention`) so an unrelated invalid settings key cannot re-enable retention; settings I/O errors skip the sweep (fail closed). | Reuse `usersettings.Load` (fails wholesale on unrelated invalid enum values). | No |
| design | Runs with all steps done but `Completed` unset are classified resumable (90-day rule), avoiding workflow loading during classification. | Load recorded workflows to detect completion (slow, fails on missing files). | No |
| design | Rollback note: older binaries would list leftover `.pruning-*` dirs; documented for manual removal. | Place trash outside `runs/`. | No |

## Test-plan decisions

| Step | Decision | Alternatives considered | Decision-bearing |
|------|----------|-------------------------|------------------|
| test-plan | Three INT obligations (real-FS multi-project sweep; resume/prune interleavings via test seams; cross-process single sweeper via test-binary re-exec) and two headless E2E obligations through the built binary, following `smoke_headless_integration_test.go`. | TUI-driven E2E (PTY flakiness; stderr-at-exit is identical in headless); E2E for count limits and groups (cheaper INT-001 proves them). | No |
| test-plan | All automated and acceptance testing sandboxes `HOME`; the real `~/.agent-runner` is off limits. | — | No |
| test-plan | Throttle/grace times advanced by editing `retention/state.json` or an injected clock, not by waiting. | Real-time waits. | No |
| test-plan | No human-only testing; TUI notice timing is covered by the exploratory pass using `pty.fork` + `pyte`. | HT for TUI notice visibility. | No |
| test-plan | E2E exit-time assertion uses the 2 s bound plus a CI margin (5 s total) to avoid flakiness. | Strict 2 s assertion. | No |

## Approach review findings

| Step | Finding | Disposition | Decision | Alternatives considered | Decision-bearing |
|------|---------|-------------|----------|-------------------------|------------------|
| approach-review | AR-001 `runlock.Acquire` stale takeover (check → remove → create) lets two processes both acquire, so resume/prune exclusion fails | Applied | Rework `Acquire` onto an OS advisory lock (flock / LockFileEx) on a never-removed-on-takeover lock file, held for the holder's lifetime, with `SameFile` revalidation; PID still recorded for display; `Delete` only unlinks the file it holds. New `run-lock` requirement "Lock acquisition is exclusive"; INT-002 case (e) with two OS processes racing a stale takeover. | Rename-based takeover with content verification (still a check-then-act window); parent-level claim file. | Yes — changes locking behavior repo-wide (in-repo, no public interface or persisted format change). |
| approach-review | AR-002 Replay/Reconcile/completion create or update audit linkage without the source run lock, so group membership is not frozen during deletion | Applied | Add `runlock.ClaimLinkage(sourceDir)` (OS lock on `audit-linkage.lock`, never creates the dir, fails if source gone). All devaudit linkage writers hold it (Replay before `createAuditRun`); the pruner takes it after member run locks and re-discovers membership under it, skipping on change. Spec scenarios added; new INT-004 (`-tags dev_audit`). | Make devaudit paths take the source run lock (would mark finished sources "active" during replay and conflicts with post-finalization semantics). | Yes |
| approach-review | AR-003 Malformed/non-mapping settings silently revert to enabled defaults, losing an opt-out | Applied | Existing-but-unreadable, invalid YAML, non-mapping root, or non-mapping `run_retention` ⇒ unknown policy: remove nothing, warn. Absent file/key ⇒ defaults. Per-key invalid scalars keep per-key default fallback (explicit; cannot flip `enabled`). Spec, design, INT-005 updated. | Keep generic `usersettings.Load` semantics. | Yes |
| approach-review | AR-004 Symlinked project or `runs/` parents could redirect deletion outside the retention root | Applied | Resolve `~/.agent-runner/projects` once with `EvalSymlinks` (relocated storage supported); project dirs, `runs/` dirs, and run dirs beneath must be real dirs by `Lstat`, else skipped with a warning; deletion targets must have the verified `runs/` as parent. Spec scenarios and INT-001 fixtures added. | Refuse any symlink including the root (would break users who relocated `~/.agent-runner` to another volume). | No |

## Task planning

| Step | Decision | Alternatives considered | Decision-bearing |
|------|----------|-------------------------|------------------|
| tasks | Single implementation task referencing all artifacts, per the step instruction and the repo's single-task format (e.g. `async-mcp`, `testing-evidence-fixes`). | Split into runlock rework, retention package, resume change, devaudit claim, CLI wiring. | No |
