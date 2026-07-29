# launch-profile

## Why

Profile-set selection is currently a project-config-only decision. The only way to run a workflow
against a different profile set is to edit `active_profile` in `.agent-runner/config.yaml`, run, and
edit it back. That is awkward for one-off runs (trying a workflow with a different CLI or model
family, comparing two profile sets, reproducing someone else's setup) and it dirties the working tree
of a project whose config is checked in.

There is no launch-time way to say "run this workflow with that profile set."

## What Changes

- Add a `--profile <name>` flag that selects the profile set for a single invocation. It takes
  precedence over `active_profile` and over the implicit `default`, and it writes nothing to config.
- Honor the flag on fresh runs and on `--validate`, so a workflow can be validated against the same
  profile set it will run with, and report which profile set validation used.
- Record the resolved profile set in `state.json` so `--resume` continues with the profile set the run
  started with. An explicit `--profile` on resume overrides it for the remaining steps.
- Surface the resolved profile set: in the run-view breadcrumb when it is not `default`, and in the
  `run_start` audit entry along with where it came from.
- Fail fast, before any step or TUI, when the named profile set does not exist, listing what is
  available.

## Capabilities

### New Capabilities

- `launch-profile-flag`: the `--profile` CLI surface — flag name and usage text, empty and unknown
  value handling, allowed and rejected flag combinations, and fail-fast timing.

### Modified Capabilities

- `config-profiles`: active-profile selection gains a launch-time override as the highest-precedence
  input, above `active_profile` and above the implicit `default` fallback. `active_profile` itself
  stays project-only.
- `workflow-pre-validation`: fresh-run and `--validate` agent resolution draw from the overridden
  profile set, and the structured error reports that set name.
- `recursive-state`: the state file records the profile set a run resolved at launch; resume reuses it
  when no override is given.
- `audit-log-entries`: `run_start` carries the resolved profile set name and its source.
- `live-run-view`: the breadcrumb shows the profile set when it is not `default`.

## Out of Scope

- Persisting the flag's value back into config. `--profile` never writes `active_profile`; changing the
  durable default stays a config edit or a settings-editor action.
- Relaxing the rule that `active_profile` is rejected in the global config.
- Selecting individual agents, CLIs, models, or efforts from the command line. The flag selects a whole
  profile set only.
- A profile picker in the TUI, or a short `-p` alias.
- Changing profile-set merge, `extends` resolution, or validation semantics.

## Impact

- `cmd/agent-runner/main.go`: new flag, usage text, flag-combination validation, threading the value
  into the dispatch path.
- `internal/config`: profile-set selection accepts an override input.
- `internal/runner`, `internal/prevalidate`: pass the override through to config loading.
- `internal/model`: `RunState` gains a recorded profile-set field; older state files without it stay
  loadable.
- `internal/audit`, `internal/runview`: report the resolved profile set.
- `docs/agent-profiles.md`: document the flag alongside `active_profile`.
- No config-file format change.

Two deliberate behavior changes affect invocations that pass no `--profile` flag, so the change is not
purely additive:

- A flagless `--resume` now prefers the profile set recorded in the run's state file over the project
  config's current `active_profile`. This is the point of recording it: a run finishes with the profiles it
  started with even if the config changed underneath it. Runs whose state files predate this change keep the
  old fallback behavior.
- Layered config is now loaded for every run rather than only for workflows that reference agents, so that a
  profile set is always resolved, validated, recorded, and displayable. A malformed config file therefore
  fails a shell-only run that previously succeeded without reading it. That is the intended tradeoff for
  dropping the special case; a missing config file is still fine, since the built-in defaults supply
  `default`.
