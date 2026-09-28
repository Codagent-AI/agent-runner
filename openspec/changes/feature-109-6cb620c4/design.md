## Context

Every top-level run lives in `~/.agent-runner/projects/<encoded-project>/runs/<run-id>/` and is never removed. Relevant current code:

- **Fresh runs.** `cmd/agent-runner/main.go` `prepareFreshRun` → `runner.PrepareRun` → `initRunState` (`internal/runner/runner.go`). When `Options.SessionDir == ""`, `freshSessionLocation` allocates `projects/<encoded>/runs/<workflow>-<timestamp>`; the directory is created with `MkdirAll` and locked with `runlock.Acquire`. `prepareFreshRun` is used by the CLI `run` path, the live TUI `prepare` closure, the headless (`AGENT_RUNNER_NO_TUI=1`) path, and the intake launch route. `--session-dir`, onboarding runs (`~/.agent-runner/onboarding/runs`), and development-audit runs pass an explicit `SessionDir`.
- **Resume.** `runner.PrepareResume` reads `state.json`, then calls `PrepareRun` with `SessionDir = filepath.Dir(stateFile)`. `initRunState` then runs `MkdirAll(sessionDir)` and `runlock.Acquire`, so a resume that read state just before the directory disappeared would recreate an empty directory and proceed.
- **Locks.** `internal/runlock`: `Acquire` (atomic create; stale locks replaced), `Delete`, `Check`/`CheckPID` (collapse read errors to `LockStale`), unexported `checkPID` (returns the error). The stale path is racy: `Acquire` checks the PID, `os.Remove`s the lock, then re-creates it, so two processes that both observed the same stale lock can each remove the other's fresh lock and both "acquire" (runlock.go:92–106). Callers: `runner.initRunState`, `runner.newRunSessionCleanup`/`finalizeRun` (`Delete`), `cmd/agent-runner/metrics_cmd.go`.
- **Run listing.** `internal/runs.ListForDir` enumerates every directory under `runs/`; status is display-oriented (no state + no lock ⇒ "completed"; unreadable state tolerated). Unexported helpers `lastUpdateTime` and `parseStartTime` compute activity.
- **Audit linkage.** An audit run is a sibling directory `runs/<audit-run-id>/` (`devaudit.auditSessionDir`). Its `state.json` has `RunKind: "audit"` and `Audit.SourceRunID`; the source run's `state.json` has `Audit.Links[].AuditRunID`. The audit reads `runs/<source>/audit-snapshots/<audit-id>/`, sealed by `sealSnapshot` to `0500`/`0400`. Linkage is created or updated outside the source run lock by several `dev_audit`-tagged paths: automatic launch in the post-finalization hook (source lock held), `Replay` (creates the audit sibling via `createAuditRun`, then appends the source link; no source lock), `Reconcile` (checks the source lock, then takes `audit-reconcile.lock`), and audit completion/failure updates via `appendSourceLink` (`provider_enabled.go:203,240`). `devaudit` already has a flock helper, `acquireDeliveryLock` (Unix `syscall.Flock`, Windows `LockFileEx`).
- **Settings.** `internal/usersettings.Load` parses `~/.agent-runner/settings.yaml`; unknown keys are ignored and preserved by `Save` (raw-node round trip). `Load` returns an error for some invalid enum values (e.g. `autonomous_backend`).
- **Warnings convention.** Runner warnings are `log.Printf("agent-runner: warning: ...")`. In TUI mode the run's logger is not reliably visible, and the audit logger is closed at `finalizeRun`.

## Goals / Non-Goals

**Goals:**
- Implement the `run-retention` spec: policy, fail-closed classification, audit groups, lock-based exclusion, atomic removal, read-only removal, throttled single-owner background sweeps, upgrade grace period and one-time notice.
- Implement the `run-lock` delta: resume never recreates a missing run directory and revalidates state under the lock.
- Keep `internal/runner` free of retention logic; the sweep must not change run latency or outcome.

**Non-Goals:**
- Manual prune command, settings-editor UI, byte accounting, onboarding/custom session directories (per proposal).
- Changing how runs, snapshots, or archives are written.

## Approach

### Components

```
cmd/agent-runner
  prepareFreshRun ──(after PrepareRun succeeds, req.SessionDir == "")──► runretention.StartBackground(cfg)
  main() ──(after run() returns)──► runretention.Finish(os.Stderr, 2s) ──► os.Exit(code)

internal/runretention            (new)
  policy.go      Policy, defaults, from usersettings.RunRetention
  classify.go    Classify(runDir) → Run{Class, LastActivity, SourceRunID, AuditRunIDs}
  plan.go        Plan(projectRuns []Run, policy, now) → []Unit to remove (pure)
  remove.go      removeUnit: lock + recheck all members, rename to trash, delete (audits first)
  sweep.go       StartBackground / Finish / sweep loop, throttle + activation state, sweeper lock
  report.go      Report{Notice, Warnings, Removed, Skipped}

internal/usersettings  + RunRetention (lenient parse) + LoadRunRetention()
internal/runlock       Acquire reworked onto an OS advisory lock; + Inspect(sessionDir) (LockStatus, pid, error);
                       + ClaimLinkage(sourceDir) (release func, error) — audit-linkage claim
internal/devaudit      Replay / Reconcile / automatic launch / completion hold ClaimLinkage(source)
internal/runs          + LastActivity, StartTimeFromID exported; ListForDir skips dot-prefixed entries
internal/runner        + Options.Resume; resume path: no MkdirAll, re-read state under lock
cmd/agent-runner       reject dot-prefixed run IDs in resume/inspect resolution
```

`internal/runretention` depends on `runlock`, `runs`, `stateio`, `model`, `usersettings`. `internal/runner` does not import it.

### Exclusive run locks (`internal/runlock`)

`Acquire(sessionDir)` keeps its signature and the PID-bearing `lock` file, but mutual exclusion moves to an OS advisory lock held for the holder's lifetime:

1. `open(sessionDir/lock, O_RDWR|O_CREATE, 0600)` — no truncation, no removal. `ENOENT` on the directory is returned wrapping `fs.ErrNotExist`.
2. Non-blocking exclusive lock on the descriptor (`flock(LOCK_EX|LOCK_NB)` on Unix; `LockFileEx` with `LOCKFILE_FAIL_IMMEDIATELY` on Windows, mirroring `devaudit/sheets_lock_*.go`). Would-block ⇒ read the recorded PID and return it as `activePID` (if unreadable, return an error "lock held by another process").
3. After locking, compare the descriptor with a fresh `Lstat` of the path (`os.SameFile`). If the path now names a different file or is gone (a holder unlinked it while we waited, or the directory was renamed), close and retry (bounded; then return the not-exist error or "lock changed during acquisition").
4. `Truncate(0)`, write the PID, `Sync`. Record the open descriptor in a process-wide registry keyed by the cleaned directory.

`Delete(sessionDir)` looks up the registry: if the path still names the held file, unlink it; then unlock and close the descriptor. If the registry has no entry (legacy callers, crashed-attempt cleanup), it keeps today's best-effort unlink. A crash releases the OS lock automatically; the stale PID file left behind is harmless because the next `Acquire` locks it rather than removing it. Because stale takeover never removes a file, two concurrent takeovers contend for the same inode and exactly one wins. Descriptors are `O_CLOEXEC` (Go default), so agent subprocesses never inherit the lock; code holding a lock must not `syscall.Exec` (today only the pre-run TUI re-execs).

`Check`/`CheckPID`/`Inspect` stay read-only and PID-based: they never take the OS lock, so a status check can never cause a concurrent `Acquire` to be refused. Classification is advisory; the authority is `Acquire` plus re-classification under the lock.

### Audit-linkage claim

`runlock.ClaimLinkage(sourceDir)` takes a blocking (bounded wait, e.g. 30 s) OS lock on `sourceDir/audit-linkage.lock` using the same open → lock → `SameFile` revalidation protocol, then verifies `sourceDir/state.json` still exists; if the source directory is gone or renamed it returns an `fs.ErrNotExist` error and never creates the directory (the lock file is opened without `MkdirAll`). Holders:

- `devaudit` automatic launch (post-finalization), `Replay`, `Reconcile`, and every `appendSourceLink`/source lifecycle update take the claim around "create audit sibling + append source link" and around link-state updates. `Replay` takes it *before* `createAuditRun`, so an audit sibling is never created for a source that retention has removed. `Reconcile` keeps its existing `audit-reconcile.lock` inside the claim.
- The pruner takes it for a group's source after acquiring all member run locks (fixed order: member run locks by name, then the source claim; devaudit paths take only the claim, or the source run lock first when they already hold it, so no lock-order cycle exists).

### Settings

`usersettings.RunRetention` holds `Enabled *bool`, `MaxAgeDays`, `ResumableMaxAgeDays`, `MaxRunsPerProject *int`, and `Invalid []string` (messages for keys present with a wrong type or a negative value). `parseSettingPair` gains a lenient `run_retention` case that never returns an error. `LoadRunRetention()` reads the file and parses only the `run_retention` mapping, so an unrelated invalid key (which makes `Load` fail) cannot switch retention back to defaults. It returns `(RunRetention, known bool, err)`: missing file or missing `run_retention` key ⇒ defaults, known. An existing file that cannot be read, is not valid YAML, has a non-mapping root, or has a non-mapping `run_retention` value ⇒ unknown: the sweep removes nothing (it may still finish `.pruning-*` trash, which is already committed removal) and reports a warning. This deliberately differs from generic `usersettings.Load`, which treats malformed YAML as empty: a destructive feature must not lose a user's opt-out to a typo. Per-key invalid scalars (wrong type, negative) still fall back to that key's default with a warning — an explicit, spec-level choice, since they cannot flip `enabled`. `Save` needs no change; the raw round trip preserves the key.

`runretention.Policy{Enabled bool; MaxAge, ResumableMaxAge time.Duration; MaxRunsPerProject int}` with defaults `true/30d/90d/100`; `0` disables a limit.

### Classification (`Classify`)

Discovery resolves `~/.agent-runner/projects` once with `filepath.EvalSymlinks` (relocated storage is supported). Beneath that root, each project directory and its `runs/` directory must be a real directory by `Lstat`; a symlink or non-directory is skipped with a warning and never opened. Every path the pruner renames or deletes is built from the resolved root plus `Lstat`-verified components, and `deleteTree` refuses any target whose parent is not the verified `runs/` directory.

For each direct child of a `runs/` directory whose name does not start with `.`, and which is a real directory (`Lstat`; symlinks are skipped with a warning):

1. `runlock.Inspect(dir)`: read error other than not-exist ⇒ `LockUnknown`; `LockActive` ⇒ `Active`.
2. `state.json` absent ⇒ `Stateless`. Present but `stateio.ReadState` fails ⇒ `Damaged`.
3. `state.Completed` ⇒ `Finished`, else `Resumable`. (Runs whose remaining steps are all done but `Completed` is unset are treated as `Resumable` — conservative, avoids loading workflow files.)
4. `LastActivity = runs.LastActivity(dir, runs.StartTimeFromID(name))`.
5. Linkage: `state.Audit.SourceRunID` (audit side) and every `state.Audit.Links[].AuditRunID` (source side).

`LockUnknown` and `Damaged` produce warnings naming the directory.

### Planning (`Plan`, pure and table-tested)

Per project `runs/` directory:

1. **Group** with union-find over directory names using both link directions; links to names not present are dropped.
2. **Group activity** = max member `LastActivity`.
3. **Protected groups**: any member `Active`, `LockUnknown`, or `Damaged`.
4. **Age eligibility** (unprotected group, grace elapsed): every member must satisfy its own class rule against the group activity — `Finished` and `Stateless` older than `MaxAge`; `Resumable` older than `ResumableMaxAge`; a disabled (`0`) limit makes its class ineligible.
5. **Count eligibility**: finished units = unprotected groups whose members are all `Finished`. Sort by group activity descending (ties: newest start time first); units beyond index `MaxRunsPerProject` are eligible.
6. Result = union of age- and count-eligible units, oldest first.

`Plan` also runs during the grace period (and on activation) to compute the "would remove" count; it simply ignores the grace flag.

### Removal (`removeUnit`)

1. For each member (deterministic name order), `runlock.Acquire(dir)`. If any returns an active PID or error: `runlock.Delete` the locks already taken, skip the unit (no warning for an active PID; warning for an error).
2. For an audit-linked unit, take `runlock.ClaimLinkage(source)`; failure ⇒ release and skip.
3. Re-`Classify` every member under the locks (treating our own locks as not-active), **re-discover the group** from both link directions (rescan the `runs/` directory for audit runs naming this source, plus the source's links), and re-run eligibility with fresh activity. If membership changed or the unit is no longer eligible, release everything and skip; the next sweep re-plans.
4. Remove audit members first, the source last. For each: `os.Rename(runs/<id>, runs/.pruning-<id>)` — the atomic disappearance point. If the rename fails, release this and remaining locks, warn, stop the unit (already-removed audit members are fine: the surviving source is a valid run).
5. `deleteTree(runs/.pruning-<id>)`: `filepath.WalkDir` (does not follow symlinks) chmods each directory to `0700` before its entries are read, then `os.RemoveAll`. Symlinks are unlinked, never followed. Errors ⇒ warning; the trash is retried next sweep.

Every sweep first calls `deleteTree` on any existing `runs/.pruning-*` entries across all projects (finishing interrupted removals).

**Why exclusion holds** (with the resume change below): pruner holds lock ⇒ resume's `Acquire` sees a live PID and refuses; pruner renamed ⇒ resume's `Acquire` cannot create a lock in a missing directory and resume reports not-found (it no longer `MkdirAll`s); resume holds lock ⇒ pruner's `Acquire` sees a live PID and skips. The pruner never writes a lock into a directory it did not first find. With the reworked `Acquire`, these hold even when both sides race to take over the same stale lock: both open the same inode and only one obtains the OS lock. For groups, the linkage claim freezes membership: a `Replay` that holds the claim first finishes linking, and the pruner's re-discovery sees the new member and skips; a `Replay` that waits for the claim finds the source gone after the pruner releases and fails without creating an audit sibling.

### Resume change (`internal/runner`)

- `Options.Resume bool`, set by `PrepareResume`.
- In `initRunState`, when `Resume`: skip `MkdirAll`; map an `Acquire` error wrapping `fs.ErrNotExist` to a new exported `ErrSessionNotFound` (message `session not found: <id>`). After acquiring, re-read `state.json`: missing ⇒ release lock, `ErrSessionNotFound`; `resumeAlreadyCompleted` ⇒ release lock, `ErrAlreadyCompleted` (existing callers already open completed runs for inspection). The directory is never removed on this path (existing `newRunSessionCleanup` only removes self-allocated directories).
- `cmd/agent-runner` `resolveResumeStatePath` and the inspect resolver reject run IDs beginning with `.`, so trash directories cannot be addressed; `runs.ListForDir` skips dot-prefixed names.

### Sweep lifecycle (`sweep.go`)

State lives in `~/.agent-runner/retention/`:
- `lock` — sweeper lock via `runlock.Acquire("~/.agent-runner/retention")` (reuses PID liveness/stale handling).
- `state.json` — `{activated_at, last_started_at, last_completed_at}` written atomically (temp + rename, `0600`).

`StartBackground(cfg)` (process-wide singleton; subsequent calls in the same process are no-ops) launches a goroutine:

1. Load policy. Disabled ⇒ done (no activation, no notice).
2. Take the sweeper lock; active elsewhere ⇒ done silently.
3. Read state. **Due** iff `now − last_completed ≥ 24h` and (`last_started ≤ last_completed` or `now − last_started ≥ 1h`). Not due ⇒ release, done.
4. Write `last_started_at = now`.
5. Finish `.pruning-*` trash in every project.
6. If `activated_at` is unset: classify + plan all projects, build the notice (effective policy, would-remove count, how to disable via `run_retention.enabled: false` in `~/.agent-runner/settings.yaml`), hand it to the report, and **do not** record activation yet (see Finish). Otherwise, if `now < activated_at + 7d`: plan only (nothing removed). Otherwise: for each project, classify, plan, `removeUnit` each unit.
7. Write `last_completed_at = now`, release the sweeper lock.

`Finish(w io.Writer, bound time.Duration)` is called once by `main` after `run()` returns and before `os.Exit`, when the terminal is restored from any TUI. It waits up to `bound` (2 s) for the goroutine. It prints the activation notice if one was produced, then records `activated_at = now` (so the grace clock starts only once the user has actually been shown the notice). It prints collected warnings (`agent-runner: warning: run retention: ...`). If the sweep has not finished, it prints any warnings collected so far and returns; the process exit abandons the goroutine, the sweeper lock becomes stale, and the next due sweep (≥ 1 h later) resumes the work.

### Failure behavior

All sweep errors become report warnings; none propagate to the run. A panic in the goroutine is recovered into a warning. The triggering run's exit code is unchanged.

## Decisions

1. **Start the sweep from `cmd` after `PrepareRun`, not inside `internal/runner`.** The run lock is already held (the new run classifies as `Active`), only CLI fresh runs with an Agent-Runner-allocated directory trigger it, and runner/devaudit/test callers of `RunWorkflow` never touch real user storage. Alternative: a hook in `initRunState` — rejected because every test and audit run would need to opt out.
2. **Report at process exit on stderr.** The run's logger is invisible in TUI mode and the audit logger may be closed before the sweep ends; printing after `run()` returns works identically for TUI and headless. Alternative: audit events on the triggering run — rejected because the sweep outlives the logger and would pollute run-specific evidence with global housekeeping.
3. **Record activation only after the notice is printed.** Guarantees the grace period starts from what the user saw; a crash before printing re-shows the notice next time.
4. **Rename to `runs/.pruning-<id>` within the same directory.** Same filesystem ⇒ atomic rename; dot prefix hides it from listings; deterministic name lets later sweeps finish it.
5. **Reuse `runlock` for both per-run exclusion and the sweeper lock.** Avoids a second locking primitive; with the OS-lock rework (decision 9) both get crash-safe, race-free takeover.
6. **Dedicated lenient `run_retention` parsing.** Retention must not be re-enabled by an unrelated settings error, and invalid retention values must not fail runs.
7. **`Plan` is a pure function over classified runs.** All eligibility rules (groups, classes, count vs age, grace) are table-testable without a filesystem.
8. **Exit wait bound of 2 s.** Scanning ~1k `state.json` files is well under that; large deletions continue in later sweeps via trash finishing.
9. **OS advisory lock for run-lock exclusivity (AR-001).** Fixes the pre-existing stale-takeover race for resume-vs-resume too. Alternatives: rename-based stale takeover with content verification (still has a check-then-act window); a parent-level claim file (second namespace every path must honor). flock is already used in `devaudit` and releases on crash.
10. **Source-level audit-linkage claim (AR-002).** One primitive shared by all linkage writers and the pruner, with membership re-discovery under it. Alternative: requiring devaudit paths to take the source *run* lock — rejected because the source run lock is held by the running source during automatic launch and is conceptually "this run is executing", so Replay on a finished run would appear as an active run in listings.
11. **Unknown policy on malformed settings (AR-003).** Fail closed for destructive behavior.
12. **Lstat-verified project/runs parents under a once-resolved root (AR-004).** Supports relocated storage while preventing redirection below the root.

## Risks / Trade-offs

- **Abandoned goroutine mid-deletion** leaves a partially deleted `.pruning-*` directory. Hidden from users and finished next sweep. Acceptable.
- **PID reuse** could make a stale lock look active in `Check`/`Inspect` classification (display and planning only); the effect is only a skipped run (fail closed). Exclusion itself no longer depends on PID liveness.
- **Mixed binaries.** An older binary using the O_EXCL/remove protocol could still remove a new binary's lock file if it judged the PID dead; since the new holder's PID is live, this requires PID confusion. Accepted for a pre-release tool; noted in the changelog.
- **Holding a descriptor per locked run** for the process lifetime; one fd per concurrently held run. Negligible.
- **Linkage claim wait.** `Replay`/completion may wait up to the bound while a pruner deletes a large group; on timeout they fail with a retryable error rather than proceeding unlinked.
- **Brief "active" display** of a run while the pruner holds its lock; lasts milliseconds before the rename.
- **Resumable runs with all steps done** but no `Completed` flag are kept until the 90-day limit rather than counted as finished. Conservative by design.
- **Only fresh CLI runs trigger sweeps.** A user who only resumes never cleans up; acceptable, since fresh runs are the growth source.
- **First sweep after grace may delete many GiB** and exceed the 2 s bound; work continues across subsequent sweeps (hourly retry after an incomplete sweep).

## Migration Plan

No data migration. On upgrade, the first fresh run records nothing until the notice is printed at exit, then starts the 7-day grace period. Users can set `run_retention.enabled: false` at any time; an already-activated installation stops removing runs immediately. Rollback: older binaries ignore `run_retention` and `~/.agent-runner/retention/`; leftover `.pruning-*` directories would appear in an older binary's run list as broken runs, so rollback users may delete them manually (documented in `docs/`).

## Testing Strategy

- `runretention.Plan`: table tests for each class, disabled limits, group rules (active audit protects source, dangling links, mixed classes), count ordering and ties, grace flag.
- `Classify`: temp-dir fixtures for each class, unreadable lock (chmod `000`), invalid JSON state, symlinked entries skipped.
- `removeUnit`: sealed snapshot fixture (`0500`/`0400`) removed; symlink target outside run untouched; lock held by a live foreign PID (a helper subprocess) ⇒ skipped; audit removed before source (inject a rename failure after the first member via a function variable) ⇒ source survives intact.
- Sweep lifecycle with an injected clock and home dir: due/not-due/incomplete-retry, activation notice then no deletion during grace, deletion after grace, disabled ⇒ no activation, concurrent `StartBackground` in two processes ⇒ one sweeps, `Finish` bound honored with a blocked deleter.
- `usersettings`: `run_retention` parsing defaults, zeros, negatives/wrong types → `Invalid`; `Save` round-trip preserves the key; `LoadRunRetention` works when another key is invalid.
- `runner` resume: directory removed between `PrepareResume`'s read and lock ⇒ `ErrSessionNotFound` and no directory recreated; completed-under-lock ⇒ `ErrAlreadyCompleted`; lock held by another live PID ⇒ existing refusal.
- `runs.ListForDir` skips `.pruning-*`; resume/inspect reject dot-prefixed IDs.
- `runlock`: two processes paused after both observe the same stale lock then both attempt takeover ⇒ exactly one wins; SIGKILLed holder ⇒ next `Acquire` succeeds; `Delete` after the directory was renamed does not unlink a different file at the original path; `Check` never blocks or refuses a concurrent `Acquire`.
- `devaudit` (`-tags dev_audit`): `Replay` paused between claim-wait and audit creation while a sweep removes the source ⇒ `Replay` fails, no audit sibling created; `Replay` holding the claim while the sweep re-discovers ⇒ group skipped.
- Settings: activated opt-out whose file becomes malformed ⇒ nothing removed, warning emitted.
- Symlinked project dir and symlinked `runs/` pointing outside the root ⇒ targets untouched, warnings emitted; `~/.agent-runner` itself a symlink ⇒ normal operation.

## Open Questions

None.
