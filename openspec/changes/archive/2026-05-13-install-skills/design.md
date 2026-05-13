## Context

Agent Runner already ships profile setup via `internal/onboarding/native/native.go`. The setup TUI walks the user through CLI/model selection for `planner` and `implementor`, prompts for scope (`user` vs `project`), writes the profile via `internal/profilewrite/`, then writes `settings.setup.completed_at`.

`.goreleaser.yaml` has been updated to add `formula: agent-plugin` as a Homebrew cask dependency, so the `agent-plugin` binary is expected to be on PATH for users installing via `brew install agent-runner`. (The corresponding tap formula is out of scope for this change.)

A near-identical integration was shipped in `agent-validator.dev` at commit `4fb0bdf`. The reference TypeScript implementation lives in `src/plugin/agent-plugin-cli.ts` and `src/commands/init.ts` of that repo, and shows the command shape we'll mirror in Go:

```
agent-plugin add Codagent-AI/agent-skills \
  --agent <name> [--agent <name>]... \
  [--project] \
  [--yes] \
  [--dry-run]
```

Agent Runner already uses adapter names that match agent-plugin's expectations (`claude`, `codex`, `copilot`, `cursor`, `opencode`), so the `github-copilot` → `copilot` mapping the reference impl performs is not needed here.

## Goals / Non-Goals

**Goals:**
- Drive Codagent skill installation during native setup without leaving the TUI.
- Reuse the configured CLI surface so we install for what the user will actually run.
- Make the failure modes predictable: missing binary blocks setup; per-CLI failures warn but unblock.

**Non-Goals:**
- Creating the `agent-plugin` Homebrew formula.
- A standalone skill-install command outside of setup.
- Updating already-installed skills (that's `agent-plugin update`, separate flow).
- Cross-CLI name mapping (Agent Runner adapter names already match agent-plugin's).

## Approach

### New package: `internal/agentplugin/`

Parallel to `internal/profilewrite/`. Owns:

- `Resolve(req *Request) (Plan, error)` — assembles the CLI list and resolves the binary, returns a `Plan` describing the invocation that will run.
- `DryRun(plan Plan) (Preview, error)` — runs `agent-plugin ... --dry-run` and returns parsed output (or raw stdout for display).
- `Install(plan Plan) (Result, error)` — runs the real invocation with `--yes` and returns a `Result` containing per-CLI success/failure entries plus any aggregate error.
- `ErrBinaryMissing` — sentinel error returned by `Resolve` when `agent-plugin` is absent from PATH (use `exec.LookPath`).

The package is small and testable in isolation using a fake `exec` interface — keep the same pattern `internal/profilewrite/` uses, with no mocking framework.

### CLI-set derivation helper

In `internal/config/` (or a sibling helper file), add `EnumerateCLIs(globalPath, projectPath string) ([]string, error)`. It loads each file independently via the existing parser, walks every profile, collects every non-empty `Agent.CLI` (following `Extends` if present), and returns the deduplicated sorted list. The just-written profile must be reflected, so this helper is called *after* `profilewrite.Write` returns.

Notes:
- A missing file is treated as an empty layer (no error).
- The helper must not require an `active_profile` — it scans every profile in both files.
- Order is deterministic so tests can compare against a fixed list.

### TUI integration

Extend the `stage` enum in `native.go` with two new states between `stageOverwrite` and the completion write:

```
stagePluginPreview    // dry-run running / showing preview
stagePluginConfirm    // user confirms or cancels
```

Wire them into `Model.write()` so the sequence becomes:

1. `profilewrite.Write` — same as today.
2. `agentplugin.Resolve` — if `ErrBinaryMissing`, call `m.fail(err)` (no `completed_at`).
3. Transition to `stagePluginPreview`, run `agentplugin.DryRun` async; render result.
4. Transition to `stagePluginConfirm`, user picks "Install" or "Cancel".
   - Cancel → `m.cancel()` (no `completed_at`).
   - Install → `agentplugin.Install`. Always render per-CLI warnings if any.
5. Write `settings.setup.completed_at`.
6. Continue to `stageDemoPrompt` (or skip per `OnboardingCompleted`).

Use the existing `setStageAnimated` helper and `Deps` struct so the new package can be injected as a `PluginInstaller` interface for testing — same pattern as `ProfileWriter`, `Models`, etc.

### Scope mapping

The existing `stageScope` already captures `user` vs `project` into `m.scope`. Pass that string straight into `agentplugin.Request.Scope`. `Resolve` translates `project` → append `--project`, anything else → omit the flag.

### Binary resolution

`exec.LookPath("agent-plugin")` from `Resolve`. No `AGENT_PLUGIN_BIN` env-var override (the reference impl needs one because Node's `require.resolve` is the default; in Go, PATH is enough). Allow tests to swap the lookup via a package-level variable.

## Decisions

### Fail setup on missing binary, warn-and-continue on per-CLI failure

A missing `agent-plugin` binary means Homebrew dependency wiring is broken — the user's machine is in a state we explicitly designed against. Letting setup complete masks the bug and leaves the user with no skills. Treating it as a setup failure means the next launch re-offers setup, giving the user a chance to fix the install.

A per-CLI failure means agent-plugin couldn't write to one CLI's plugin tree (permission, missing dir, unsupported CLI). The user has a working profile and probably skills for the other CLIs; forcing them to redo setup adds friction. Warn loudly, continue.

### Dry-run + confirm instead of silent install

Matches the reference impl, gives the user a chance to see what's about to change in their CLIs (especially relevant when union-of-configs surfaces CLIs they don't actively use), and keeps blast radius visible.

### CLI source = union across both config files, not just the just-written profile

The user's answer: a project-level setup may write only a `default` profile, but the user-level config may already list other CLIs (or vice versa). The merged set is what `agent-runner` will use across all working directories, so it's what we should install for.

### No name mapping for agent-plugin CLI names

Reference impl maps `github-copilot` → `copilot` because its adapter set differs. Agent Runner's adapter names (`claude`, `codex`, `copilot`, `cursor`, `opencode`) already align with agent-plugin's `--agent` values. If that ever drifts, add a single translation table in `internal/agentplugin/`.

## Risks / Trade-offs

- **Empty CLI list:** if the merged config somehow yields zero CLI values (unlikely after the profile write), the runner has nothing to install for. Treat as success (no-op), do not invoke `agent-plugin`.
- **`agent-plugin` output format:** the dry-run preview render assumes the binary produces human-readable output. If output is structured, the preview render may need a parser — defer until the formula and the binary's output format are pinned.
- **Slow installs:** `agent-plugin add` can fetch from a remote repo; the dry-run mitigates this somewhat but the real install still takes time. The reference impl uses a 120 s timeout; use the same default and surface as a per-CLI warning on timeout.
- **Future drift:** if agent-plugin renames `add` to something else, or changes the `--project` flag shape, the wrapper package centralizes the fix.
- **Reset story:** clearing `settings.setup.completed_at` is currently the only way to re-trigger this flow. That's consistent with how setup as a whole works today; if it becomes a UX problem, add a dedicated re-install command outside this change.
