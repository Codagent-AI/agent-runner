## Why

Starting a new run today requires dropping out of the TUI to invoke `agent-runner run <workflow> key=value ...` on the CLI — users must already know which workflows exist, remember parameter names, and type them correctly. The main TUI surfaces existing runs but offers no path to discover workflows or launch one, so the tool feels like a run viewer bolted onto a CLI launcher rather than an integrated workflow runner.

## What Changes

- Add a new **"new" tab** to the main list TUI alongside the existing current-dir / worktrees / all tabs, listing available workflows grouped by scope (project → user → builtin).
- Add per-row keybindings on the new tab: one to **start a run** directly, one to **open the workflow** in a read-only definition view.
- Add a **workflow definition view** — a new mode of the run view that renders a workflow's steps, parameters, and metadata without being tied to a run instance. This view also exposes the "start run" action.
- Add a **parameter form view** that appears when starting a run from the TUI. It prompts for each declared `Param`, respecting `Required` and `Default`, and validates before launch.
- Wire the TUI start action into the existing workflow execution path so runs launched from the TUI behave identically to CLI-launched runs.
- Change the **default TUI entry point**: running `agent-runner` with no subcommand opens the list TUI focused on the new "new" tab (workflows), rather than a run-oriented tab.
- Preserve `agent-runner --resume` (no arg) as the "runs for current dir" entry point — it continues to open the TUI on the current-dir runs tab exactly as it does today.
- Leave tab-selection on entry as a concern for the spec stage to finalize across all other TUI entry points (e.g., worktree context, explicit `--all`, deep-links from other commands).

## Capabilities

### New Capabilities

- `workflow-discovery`: Enumerates workflows available to the user, grouping results by scope (builtin / user / project) with source-path metadata. Consumed by the new tab's list and the workflow definition view.
- `workflow-definition-view`: Read-only TUI view that renders a workflow's steps, parameters, and metadata from its definition (not from a run instance), and surfaces a "start run" action.
- `workflow-param-form`: TUI form that collects parameter values interactively before a run starts, honoring each param's `Required` and `Default` fields, validating input, and returning the resulting param map to the launch path.

### Modified Capabilities

- `list-runs`: Gains a fourth tab ("new") that lists workflows instead of runs, with its own keybindings for start / inspect actions, and a search filter for narrowing the workflow list. The default focused tab on TUI entry changes based on how the TUI was opened: bare `agent-runner` focuses "new"; `--resume` focuses the current-dir runs tab. Existing tab-switching keybindings are unchanged.
- `view-run`: Gains a new entry mode for viewing a workflow definition (no run instance attached). Existing `FromList`, `FromInspect`, and `FromLiveRun` modes are unaffected.

## Out of Scope

- **Editing workflows** from the TUI — the definition view is read-only.
- **Creating or scaffolding new workflow YAML files** from the TUI.
- **Parameter type richness beyond strings** — the existing `Param` model is string-valued; richer types (enums, booleans, files) are a separate change.
- **Persisting or recalling prior parameter values** across runs.
- **Advanced search** (fuzzy matching, regex) — the filter is a simple case-insensitive substring match.
- **Non-interactive / scripted launches** — the CLI path remains the contract for automation.

## Impact

- **New code**: new `internal/discovery/` package (workflow enumeration), new `internal/paramform/` component (parameter form TUI).
- **Modified code**: `internal/listview/` (new tab + workflow list rendering), `internal/runview/` (workflow-definition mode).
- **Touched code**: `cmd/agent-runner/main.go` run-launch path must be reachable from the TUI without duplicating parameter parsing from `parseParams` / `matchParams`.
- **Reused infrastructure**: `builtinworkflows.Resolve`, `loader` package, and `.agent-runner/workflows/` + `~/.agent-runner/workflows/` discovery logic — no changes to the resolution order.
- **Specs**: Three new spec files, two delta spec files (see Capabilities).
- **No changes** to workflow YAML schema, run state format, or the CLI `run` command contract.
- **CLI entry-point semantics change**: bare `agent-runner` now opens the TUI on the new tab instead of its prior default. `--resume` behavior is preserved. Other TUI entry points will be enumerated and resolved during the spec stage.
