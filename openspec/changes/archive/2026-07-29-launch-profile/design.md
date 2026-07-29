# Design: launch-profile

## Context

Profile-set selection lives entirely inside `internal/config`. `buildConfig` reads `active_profile` from
the project layer, falls back to `default`, and returns a `*config.Config` whose `ActiveAgents` is the
selected set's agents (`internal/config/config.go:275-303`). The only entry point is
`config.Load(path string)` (`internal/config/config.go:111`), which takes a project config path and
nothing else.

Three call sites reach that code, and all three matter here:

1. `internal/runner/runner.go:290` — `initRunState` lazily loads config into `Options.ProfileStore` when
   the workflow has agent steps. If a caller pre-populates `ProfileStore`, no load happens.
2. `internal/prevalidate/pipeline.go:131` — `Options.withDefaults` installs a `LoadConfig` closure that
   calls `config.Load` for both fresh-run pre-validation and `--validate`.
3. `cmd/agent-runner/main.go` — parses flags and dispatches, but never touches config itself.

So the override has to travel from flag parsing to `internal/config`, through two independent seams that
tests already substitute (`ProfileStore` and `LoadConfig`). The rest of this document pins how, plus the
state round-trip that makes resume behave.

## Approach

### Threading the override

Add the override as an explicit input to config loading rather than a package-level variable or an
environment variable. Two concrete requirements shape the choice: `config.Load` has many existing
callers in tests, and `internal/prevalidate` swaps the loader wholesale.

Introduce an options-style variant alongside the existing function:

- Keep `config.Load(path string)` as a thin wrapper that delegates with no override, so existing callers
  and tests compile unchanged.
- Add `config.LoadWithProfile(path string, override ProfileOverride)` (or an equivalent options struct)
  that carries the override into `buildConfig`. The override pairs the profile set name with an origin
  label, because the same input arrives from two places — the `--profile` flag and a resumed run's state
  file — and the two produce different error text and different audit source values. Keeping the label
  with the name means `internal/config` never has to know about flags or state files; it just echoes back
  what the caller told it.

`buildConfig` then applies the precedence chain (override → `active_profile` → `default`) in one place,
which keeps the selection rule and its error messages from being duplicated across runner and
pre-validation.

`*config.Config` gains the resolved name and its source so callers can report them without recomputing
precedence. The existing `ActiveProfile` field holds the raw `active_profile` value from the project
layer and is consumed by `internal/prevalidate/pipeline.go:558` (`activeProfileName`), which reimplements
the fallback to `default`. Add distinct fields for the resolved outcome rather than overloading
`ActiveProfile`, then point `activeProfileName` at the resolved field so pre-validation errors report the
set actually in use.

The override reaches those two seams as follows:

- Runner: `runner.Options` gains a profile-override field. `initRunState` passes it to the new loader.
  Callers that pre-populate `ProfileStore` are unaffected, since they have already resolved a config.
- Pre-validation: `prevalidate.Options` gains the same field, and the default `LoadConfig` closure passes
  it through. Substituted loaders in tests keep working.

`cmd/agent-runner/main.go` validates the flag (non-empty after trimming, not combined with `--list` or
`--inspect`) and puts the value on `commandFlags`, which already carries `validate`, `resume`, `inspect`,
`list`, and `onboardingFrom` to `dispatchRunCommand`.

One path needs explicit attention because it crosses a process boundary. A bare `--resume` with no session ID
falls through to `handleList` (`cmd/agent-runner/main.go:439`); when the user picks a run, the list re-execs
the binary via `execRunnerResume` (`:1053`), which builds `args := []string{"--resume", runID}` (`:1060`) and
replaces the process. An override held only in the first process's memory is lost there. `execRunnerResume`
must therefore append `--profile <name>` when an override is in effect, which means the switcher/list handoff
has to carry the value from `dispatchRunCommand` through to the re-exec. The same applies to the other
`execRunnerResume` call sites (`:730`, `:883`, `:901`), which pass an empty run ID and re-exec into the list.

### Fail-fast placement

Unknown-profile-set detection is a natural consequence of routing selection through `buildConfig`: the
existing lookup already fails when the selected name is absent. The change is the error text, which must
attribute the name to the flag and list available sets in sorted order.

The `launch-profile-flag` spec requires that this failure produce no run, no state file, and no audit
entry. For fresh runs that falls out of ordering: pre-validation loads config before
`runner.PrepareRun`/`RunWorkflow` and before the TUI (`workflow-pre-validation`, "When pre-validation
runs"). The gap is workflows that skip pre-validation via the path-based skip rule. For those, the run path
must still surface the error before creating run artifacts, so the override is resolved early in the launch
path rather than relying on `initRunState`'s lazy load.

There is deliberately no special case for workflows with no agent steps. `workflowNeedsAgentProfiles`
(`internal/runner/runner.go:270`) currently gates the config load in `initRunState`
(`internal/runner/runner.go:289`) on the workflow referencing a prompt, an agent, or a sub-workflow. Keeping
that gate would mean a shell-only workflow resolves no profile set, which in turn would need carve-outs in
the state, audit, and breadcrumb rules, and would let `--profile bogus` pass silently. Instead config is
loaded for every run and a profile set is always resolved. The cost is that a malformed config now fails a
shell-only run; a missing config file is unaffected, because `loadFileOptional` treats absence as an empty
layer and the built-in defaults supply `default`.

Resume needs its own ordering care. `handleResumeWithOptions` (`cmd/agent-runner/main.go:538`) calls
`requireTTY`, then `ensureThemeForTUI` (`:566`), and only then `runner.PrepareResume` (`:570`). Resolving the
effective profile set inside `PrepareResume` would let an unknown name surface after a theme prompt, and
after `PrepareRun` has begun touching the run's state. The effective profile set must therefore be resolved
and validated in the resume path before `ensureThemeForTUI`.

### Resume and state

`model.RunState` (`internal/model/state.go:73`) gains a `profileSet` field, camelCase to match the existing
keys in that struct. It stays `omitempty`: not because this version ever omits it, but because a state file
written before this change decodes to `""`, which is exactly the fallback case the `recursive-state` spec
requires. No migration step is needed. The field is populated in `initialRunState`, whose result is written at
`internal/runner/runner.go:729` specifically so concurrent readers (the live TUI, `--inspect` on a fresh run)
can see it immediately, which is what makes the breadcrumb work from the first frame.

Resume precedence is override → recorded state → `active_profile` → `default`. This differs from the
fresh-run chain by the one extra level, and it is why the resolved-source enum has a `state` value: the
audit entry needs to distinguish "reused what the run started with" from "the config says so".

When resume applies an override, it rewrites the recorded field. Practically that means the resume path
computes the effective profile set before the first step executes and persists it with the next state
write, so a second resume with no flag continues with the overridden set.

### Reporting

Both consumers read the resolved name from state rather than recomputing selection:

- The run view already reads `state.json` for run-level metadata. `recordedVersion` is derived from it at
  `internal/runview/model.go:209` and rendered as a dim `· <value>` segment right after the top crumb
  (`internal/runview/breadcrumb.go:38`). The profile segment borrows that styling and that state-backed
  source, which also means `--inspect` of a finished run shows it without touching the audit log.

  It must **not** borrow `recordedVersion`'s entry-mode gate. That assignment is wrapped in
  `if entered == FromList || entered == FromInspect` (`internal/runview/model.go:208`), so the version segment
  never appears during a live run. Copying that condition would hide the profile segment for the entire run,
  which is the opposite of the intended behavior. The underlying data is available in every mode: `state` is
  read unconditionally at `internal/runview/model.go:191`. Populate the profile name for all entry modes,
  including `FromLiveRun`.
- The audit `run_start` entry records the name and source at run start, as `profile_set` and `profile_source`
  to match the snake_case keys already used in `emitRunStart` (`internal/runner/runner.go:480`).

## Decisions

| Decision | Rationale |
| --- | --- |
| New loader function, keep `config.Load` | Avoids churn across many existing callers and tests while giving the override an explicit, testable input. |
| Precedence resolved once in `buildConfig` | Runner and pre-validation must never disagree about which set is active; duplicating the chain is how they would. |
| Resolved name and source as new `Config` fields | `ActiveProfile` means "the raw `active_profile` value" to existing code; overloading it would silently change `activeProfileName` and any other reader. |
| Override carries an origin label | The flag and the state file feed the same selection slot but need different error text and different audit sources; the label keeps `internal/config` ignorant of both. |
| Source enum includes `state` | Resume reusing a recorded set is operationally different from the config selecting it, and the audit log is where that distinction is worth having. |
| `omitempty` state field, no migration | Absent field naturally decodes to the documented fallback behavior. |
| Run view reads state, not the audit log | State is already the run view's source for run-level metadata, and it works identically for live runs and `--inspect`. |
| Breadcrumb rule keys on the resolved name | "Not `default`" is a rule a user can predict from what they see; "source is the flag" would make `--profile default` render differently from an unflagged default run for no visible reason. |
| Always resolve a profile set; no shell-only special case | The alternative needed carve-outs in three specs and would have let `--profile bogus` pass silently on a shell-only workflow. One rule everywhere, at the cost of loading config for runs that do not consult agents. |
| Serialized keys pinned in the specs | `state.json` is camelCase and audit data is snake_case; leaving the keys to the implementer invites an inconsistency that is awkward to change once runs exist on disk. |

## Risks and Trade-offs

- **Two loader entry points.** `config.Load` and the override-aware variant can drift. Mitigated by making
  the former call the latter with an empty override, so there is one implementation.
- **Early resolution duplicates a load.** Failing fast for workflows that skip pre-validation may mean
  loading config in the launch path and again in `initRunState`. Passing the resolved config through as
  `Options.ProfileStore` avoids the second load; that is worth doing, but the correctness requirement is
  the fail-fast ordering, not the load count.
- **Resume with a different profile set produces a mixed run.** Early steps ran under one set, later steps
  under another. This was chosen deliberately over rejecting the combination; the audit log records the
  switch, and the `recursive-state` spec makes explicit that completed steps are not re-executed.
- **Breadcrumb width.** The chrome line already competes with the logo and the elapsed/status suffix. The
  profile segment must truncate with existing breadcrumb behavior rather than introduce wrapping.
- **Config is now loaded for every run.** Shell-only workflows that never touched `.agent-runner/config.yaml`
  now do. A malformed config fails such a run where it previously succeeded. Accepted deliberately: the
  project is pre-release and prefers a coherent rule over accidental compatibility, and failing loudly on a
  broken config is defensible on its own. Missing config files are unaffected.
- **The override crosses a process boundary on bare `--resume`.** The list-selection path re-execs the binary,
  so the override has to be re-serialized onto the new argv. This is easy to miss and silently degrades to
  "flag ignored" rather than failing, which makes it worth a dedicated test.
