# run-retention Specification

## Purpose
TBD - created by archiving change feature-109-6cb620c4. Update Purpose after archive.
## Requirements
### Requirement: Retention policy configuration

Agent Runner SHALL apply a run retention policy read from the `run_retention` mapping in `~/.agent-runner/settings.yaml`. The mapping SHALL support these keys, each optional:

| Key | Meaning | Default |
|-----|---------|---------|
| `enabled` | Master switch for automatic run cleanup | `true` |
| `max_age_days` | Maximum age, in days of inactivity, of a finished run | `30` |
| `resumable_max_age_days` | Maximum age, in days of inactivity, of a resumable run | `90` |
| `max_runs_per_project` | Maximum number of finished runs kept per project | `100` |

A value of `0` for `max_age_days`, `resumable_max_age_days`, or `max_runs_per_project` SHALL disable that limit. An absent key, an absent `run_retention` mapping, or an absent settings file SHALL use the default for each affected key. When the settings file exists but cannot be read, is not valid YAML, has a root that is not a mapping, or has a `run_retention` value that is not a mapping, the policy SHALL be treated as unknown: the sweep SHALL remove nothing and SHALL report a non-fatal warning, so that a previously configured opt-out is never lost to a malformed file. A present key whose value is not of the expected type, or is a negative number, SHALL use the default for that key and SHALL be reported as a non-fatal warning; it SHALL NOT fail the run.

#### Scenario: No retention settings
- **WHEN** `settings.yaml` has no `run_retention` key
- **THEN** the effective policy is enabled, finished runs expire after 30 days, resumable runs expire after 90 days, and each project keeps at most 100 finished runs

#### Scenario: Custom limits
- **WHEN** `settings.yaml` sets `run_retention.max_age_days: 7` and `run_retention.max_runs_per_project: 20`
- **THEN** finished runs expire after 7 days of inactivity and each project keeps at most 20 finished runs, while resumable runs keep the 90-day default

#### Scenario: Limit disabled with zero
- **WHEN** `settings.yaml` sets `run_retention.max_runs_per_project: 0`
- **THEN** no run is removed because of the per-project count, and the age limits still apply

#### Scenario: Retention disabled
- **WHEN** `settings.yaml` sets `run_retention.enabled: false`
- **THEN** no run directory is ever removed by retention, no retention notice is shown, and runs accumulate as before this change

#### Scenario: Malformed settings file removes nothing
- **WHEN** retention was activated more than 7 days ago, the user had set `run_retention.enabled: false`, and `settings.yaml` is then edited into invalid YAML
- **THEN** a due sweep removes no run and a non-fatal warning states that the retention policy could not be read

#### Scenario: Invalid value falls back to default
- **WHEN** `settings.yaml` sets `run_retention.max_age_days: -5`
- **THEN** the finished-run age limit is 30 days, a non-fatal warning identifies the invalid value, and the new run proceeds normally

### Requirement: Retained run scope

Retention SHALL only consider run directories that are direct children of `~/.agent-runner/projects/<encoded-project>/runs/`, across every project directory under `~/.agent-runner/projects/`. It SHALL NOT remove or modify anything outside those `runs/` directories, including project `meta.json` files, caller-supplied session directories, and `~/.agent-runner/onboarding/runs`. It SHALL NOT follow symbolic links out of the run directory being removed. The projects root `~/.agent-runner/projects` SHALL be resolved once, following symbolic links, so a user may place Agent Runner storage on another volume. Beneath that resolved root, a project directory, its `runs/` directory, or a run directory that is a symbolic link (or not a directory) SHALL be skipped without following it and SHALL be reported as a non-fatal warning.

#### Scenario: Runs from another project are cleaned
- **WHEN** a fresh run starts in project A, a sweep is due, and project B (not visited since) has finished runs older than the finished-run age limit
- **THEN** those project B runs are removed

#### Scenario: Onboarding runs are untouched
- **WHEN** a sweep runs and `~/.agent-runner/onboarding/runs` contains runs older than every limit
- **THEN** those onboarding runs remain on disk

#### Scenario: Symlinked project or runs directory is not followed
- **WHEN** `~/.agent-runner/projects/<p>/runs` (or `~/.agent-runner/projects/<p>`) is a symbolic link to a directory outside the projects root containing directories older than every limit
- **THEN** nothing in the link target is removed or modified, and a non-fatal warning names the skipped path

#### Scenario: Relocated storage root
- **WHEN** `~/.agent-runner` is a symbolic link to a directory on another volume
- **THEN** runs under the linked storage are evaluated and removed normally

#### Scenario: Symlink inside a run directory
- **WHEN** a run directory being removed contains a symbolic link to a file or directory outside that run directory
- **THEN** the link itself is removed and its target is left unchanged

### Requirement: Run activity age

A run's age SHALL be measured from its last activity, defined as the most recent modification time among the run's `audit.log`, its `state.json`, and the run directory itself, falling back to the start time encoded in the run ID when none of those can be read. For an audit-linked group, the group's last activity SHALL be the most recent last activity of any member.

#### Scenario: Old run with recent activity is kept
- **WHEN** a finished run started 60 days ago but its `audit.log` was last modified 5 days ago, with default limits
- **THEN** the run is not removed by the age limit

#### Scenario: Group age follows its newest member
- **WHEN** a source run was last active 40 days ago and its linked audit run was last active 10 days ago, with default limits
- **THEN** neither run is removed by the age limit

### Requirement: Run classification for retention

Retention SHALL classify each run directory as exactly one of the following, and SHALL decide eligibility from this classification rather than from run-list display status:

- **active**: its run lock names a live process;
- **lock-unknown**: its lock file exists but cannot be read or inspected;
- **damaged**: it has a `state.json` that cannot be read or parsed;
- **finished**: it has a readable `state.json` recording that the workflow completed;
- **resumable**: it has a readable `state.json` that does not record completion;
- **stateless**: it has no `state.json`, and no lock or only a stale lock.

Active, lock-unknown, and damaged runs SHALL be protected: they SHALL never be removed. Damaged and lock-unknown runs SHALL be reported as non-fatal warnings naming the run directory, so the user can repair or remove them manually.

#### Scenario: Active run is never removed
- **WHEN** a run older than every limit holds a lock naming a live process
- **THEN** the run is not removed

#### Scenario: Damaged state is protected and reported
- **WHEN** a run older than every limit has a `state.json` containing invalid JSON
- **THEN** the run is not removed and a non-fatal warning names its run directory

#### Scenario: Unreadable lock is protected
- **WHEN** a run older than every limit has a lock file that cannot be read
- **THEN** the run is not removed and a non-fatal warning names its run directory

#### Scenario: Failed run is resumable
- **WHEN** a run stopped on a failed step and its `state.json` does not record completion
- **THEN** the run is classified as resumable

### Requirement: Age-based removal

Once the upgrade grace period has elapsed, retention SHALL remove:

- a finished run whose group's last activity is older than `max_age_days`;
- a resumable run whose group's last activity is older than `resumable_max_age_days`;
- a stateless run whose last activity is older than `max_age_days`.

A disabled limit (value `0`) SHALL remove nothing.

#### Scenario: Expired finished run removed
- **WHEN** a finished run's last activity was 31 days ago, with default limits and the grace period elapsed
- **THEN** the run directory is removed

#### Scenario: Resumable run survives the finished-run limit
- **WHEN** a resumable run's last activity was 45 days ago, with default limits
- **THEN** the run is not removed and can still be resumed

#### Scenario: Abandoned resumable run removed
- **WHEN** a resumable run's last activity was 91 days ago, its lock is stale or absent, with default limits and the grace period elapsed
- **THEN** the run directory is removed

#### Scenario: Legacy stateless run removed by age
- **WHEN** a run directory from an older Agent Runner version has no `state.json` and no lock, and its last activity was 31 days ago, with default limits and the grace period elapsed
- **THEN** the run directory is removed

### Requirement: Per-project count limit

Once the upgrade grace period has elapsed, when a project has more finished units than `max_runs_per_project`, retention SHALL remove the least recently active finished units until the project has at most `max_runs_per_project` finished units. A finished unit is a single finished run, or an audit-linked group whose members are all finished; a group counts as one unit. Resumable, stateless, and protected runs, and groups containing any of them, SHALL NOT be removed by the count limit and SHALL NOT count toward it.

#### Scenario: Oldest finished runs evicted
- **WHEN** a project has 103 finished runs, all active within the last day, with default limits and the grace period elapsed
- **THEN** the 3 least recently active finished runs are removed and 100 remain

#### Scenario: Resumable runs are not count-evicted
- **WHEN** a project has 100 finished runs and 20 resumable runs, all active within the last day, with default limits
- **THEN** no run is removed

#### Scenario: Stateless runs are not count-evicted
- **WHEN** a project has 100 finished runs and 5 recent stateless run directories, with default limits
- **THEN** no run is removed

### Requirement: Audit-linked groups

Retention SHALL treat a source run and every development-audit run linked to it as one group. Linkage SHALL be discovered from both sides: an audit run's recorded source run and a source run's recorded audit links. A link that names a run directory that no longer exists SHALL be ignored. If any member of a group is protected, or is not itself eligible for removal under the rules for its own classification, no member of the group SHALL be removed.

Group membership SHALL be frozen during removal: every operation that creates an audit run for a source run or adds or updates a source run's audit linkage (automatic audit launch, replay, reconciliation, and audit completion) and group removal SHALL hold the source run's audit-linkage claim. Retention SHALL take the source's claim after its members' run locks, re-discover the group's membership while holding it, and skip the group if membership changed. An operation that obtains the claim after the source run has been removed SHALL fail without creating an audit run or recreating the source directory.

When removing a group, retention SHALL remove all audit runs before the source run. If removal is interrupted, every surviving combination SHALL be a source run with some or none of its audit runs; retention SHALL never leave an audit run whose source run has been removed by retention.

#### Scenario: Active audit protects its source
- **WHEN** a finished source run is 60 days old and a linked audit run is currently active
- **THEN** neither the source run nor the audit run is removed

#### Scenario: Group removed together
- **WHEN** a finished source run and its finished audit run were both last active 40 days ago, with default limits and the grace period elapsed
- **THEN** both run directories are removed

#### Scenario: Interrupted group removal
- **WHEN** a group removal is interrupted after the audit run was removed but before the source run was removed
- **THEN** the source run remains a normal, inspectable run and a later sweep evaluates it on its own

#### Scenario: Replay racing group removal
- **WHEN** a replay starts for a source run while a sweep is removing that source's group
- **THEN** either the replay completes first and the sweep skips the group because its membership changed, or the sweep completes first and the replay fails with an error without leaving an audit run for the removed source

#### Scenario: Dangling audit link
- **WHEN** a finished source run records an audit link to a run ID whose directory no longer exists
- **THEN** the source run is evaluated as if the missing audit run were not linked

### Requirement: Exclusion with concurrent runs

Before removing a run, retention SHALL take that run's run lock and SHALL re-read the run's classification while holding it. If the lock is held by a live process, or the re-read classification is no longer eligible, the run (and its group) SHALL be skipped for this sweep. For a group, retention SHALL take and re-check every member's lock before removing any member. A run being resumed and a run being removed SHALL never both proceed. Lock acquisition SHALL be exclusive even when several processes simultaneously take over the same stale lock (see `run-lock`).

#### Scenario: Resume wins the race
- **WHEN** a sweep selects an abandoned resumable run and the user resumes that run before the sweep takes its lock
- **THEN** the sweep skips the run and the resume proceeds normally

#### Scenario: Simultaneous stale-lock takeover
- **WHEN** a resume and a sweep both find the same abandoned run's stale lock and try to take it over at the same moment
- **THEN** exactly one of them proceeds; either the run is resumed and not removed, or it is removed and the resume fails without executing steps

#### Scenario: Run changes before removal
- **WHEN** a sweep selects a run from its first scan and, by the time it holds the run's lock, the run's last activity is within the limits
- **THEN** the run is not removed

### Requirement: Removal is atomic from the user's view

A run directory being removed SHALL disappear from its original path in a single step before its contents are deleted, so run listings, `--resume`, and `--inspect` never observe a partially deleted run. Partially deleted remains from an interrupted removal SHALL NOT appear as runs in any run listing and SHALL be deleted by a later sweep.

#### Scenario: Interrupted removal is hidden and finished later
- **WHEN** the process exits while deleting a removed run's contents
- **THEN** the run does not appear in the run list, cannot be addressed by `--resume` or `--inspect`, and a later sweep deletes the remaining contents

### Requirement: Read-only content removal

Retention SHALL remove run directories containing read-only files and directories, including sealed development-audit snapshots with directory mode `0500` and file mode `0400`, without failing on permission errors caused by those modes.

#### Scenario: Sealed audit snapshot removed
- **WHEN** an eligible run contains `audit-snapshots/<audit-id>/` sealed with directories `0500` and files `0400`
- **THEN** the entire run directory is removed

### Requirement: Sweep trigger and throttling

A retention sweep SHALL be started only when a fresh run starts whose session directory Agent Runner allocates under `~/.agent-runner/projects/<encoded-project>/runs/`, after the fresh run holds its own run lock. Runs started with `--session-dir`, onboarding runs, development-audit runs, resuming, inspecting, listing, and validating SHALL NOT start a sweep. The fresh run SHALL never be selected for removal. A sweep SHALL NOT start if a sweep completed within the previous 24 hours. After a sweep that started but did not complete, another sweep SHALL NOT start until at least 1 hour after the incomplete sweep started. At most one sweep SHALL run at a time across all Agent Runner processes for the same user; a process that finds another sweep in progress SHALL skip its sweep without waiting.

#### Scenario: Daily throttle
- **WHEN** a sweep completed 3 hours ago and a new fresh run starts
- **THEN** no sweep starts

#### Scenario: Due sweep on fresh run
- **WHEN** the last completed sweep was 25 hours ago and a new fresh run starts
- **THEN** a sweep starts

#### Scenario: Explicit session directory does not sweep
- **WHEN** no sweep has completed in the last 24 hours and the user starts a run with `--session-dir /tmp/run1`
- **THEN** no sweep starts

#### Scenario: Resume does not sweep
- **WHEN** no sweep has completed in the last 24 hours and the user resumes a run
- **THEN** no sweep starts

#### Scenario: Interrupted sweep is retried later
- **WHEN** a sweep started 2 hours ago, never completed because its process exited, and a new fresh run starts
- **THEN** a sweep starts

#### Scenario: Concurrent fresh runs
- **WHEN** two fresh runs start at the same time and a sweep is due
- **THEN** exactly one of them performs the sweep and neither is delayed waiting for the other

### Requirement: Sweeps never impede the new run

A sweep SHALL run without delaying the start or execution of the fresh run that triggered it. Sweep errors (including permission, I/O, and settings errors) SHALL NOT change the fresh run's outcome or exit code. Retention warnings SHALL be written to standard error, prefixed `agent-runner: warning: run retention:`, when the triggering process exits (after any TUI has released the terminal), for both TUI and headless runs. When the triggering process is ready to exit while its sweep is still in progress, it SHALL wait at most 2 seconds for the sweep, print any warnings collected so far, and then exit, leaving unfinished work to a later sweep. If the settings file exists but cannot be read, the sweep SHALL be skipped and a warning reported.

#### Scenario: Removal failure does not fail the run
- **WHEN** a sweep cannot remove an eligible run because of an I/O error
- **THEN** the fresh run completes with the outcome and exit code it would otherwise have had, and on exit standard error shows a run-retention warning identifying the run that could not be removed

#### Scenario: Short run exits with sweep in progress
- **WHEN** the fresh run finishes while a large sweep is still deleting runs
- **THEN** the process exits no more than 2 seconds after the run finishes, and the next due sweep continues the cleanup

### Requirement: Upgrade grace period and activation notice

The first time a sweep is due with retention enabled on an installation where retention has never been activated, Agent Runner SHALL show a notice on standard error when the triggering process exits, and SHALL record the activation time only once the notice has been written. The notice SHALL state the effective retention policy, the number of existing runs that the policy would remove once the grace period ends, and how to change or disable retention in `~/.agent-runner/settings.yaml`. No run SHALL be removed by retention until 7 days after the recorded activation time. The notice SHALL NOT be shown again once activation is recorded. If the process ends before the notice is written, activation is not recorded and the notice is shown by a later due sweep.

#### Scenario: First run after upgrade
- **WHEN** an existing installation with 400 runs older than 30 days starts its first fresh run with retention enabled
- **THEN** when the process exits, standard error shows a notice stating the effective policy, that about 400 runs will be removed after the grace period, and how to disable retention, and no run is removed

#### Scenario: Deletion during the grace period
- **WHEN** a sweep is due 3 days after activation and runs exceed the limits
- **THEN** no run is removed

#### Scenario: Deletion after the grace period
- **WHEN** a sweep is due 8 days after activation and runs exceed the limits
- **THEN** eligible runs are removed

#### Scenario: Notice shown once
- **WHEN** retention was activated earlier and a later fresh run starts
- **THEN** no activation notice is shown

#### Scenario: Disabled before activation
- **WHEN** a user sets `run_retention.enabled: false` before their first fresh run after upgrading
- **THEN** no notice is shown, no activation is recorded, and no run is removed

