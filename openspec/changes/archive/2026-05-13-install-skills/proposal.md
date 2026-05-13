## Why

Agent Runner ships with profile setup but does nothing to install the Codagent skills repo into the agent CLIs the user just configured. With `agent-plugin` now declared as a Homebrew cask dependency, native setup can drive that installation directly, getting users from `brew install agent-runner` to a working agent skill experience in one flow.

## What Changes

- During native setup, between the profile write and the `settings.setup.completed_at` write, Agent Runner invokes `agent-plugin add Codagent-AI/agent-skills` for the union of `cli` values across user-level and project-level `config.yaml`.
- A new TUI stage runs a dry-run preview against `agent-plugin` and asks the user to confirm before the real install.
- A missing `agent-plugin` binary is treated as a setup failure — completion is not recorded.
- A per-CLI install failure is treated as a non-fatal warning — completion is still recorded so the user is not blocked.
- The install scope (`--project` vs user) is aligned with the scope the user selected for the profile config.

## Capabilities

### Modified Capabilities
- `native-setup`: adds skill installation as a required setup action, with new completion-tracking semantics for binary-missing vs per-CLI failure cases.

## Out of Scope

- Creating the `agent-plugin` Homebrew formula in `Codagent-AI/homebrew-tap`. Tracked separately in the handoff.
- A standalone command/menu entry to re-run skill installation outside of setup. Users can re-run setup by clearing `settings.setup.completed_at`.
- Updating already-installed skills. Update flow is the agent-plugin `update` subcommand, not exercised by setup.

## Impact

- `internal/onboarding/native/native.go`: new stage between `stageOverwrite`/profile write and `settings.setup.completed_at` for dry-run preview, confirmation, and install.
- New `internal/agentplugin/` package wrapping `agent-plugin add` invocation, CLI-set derivation, and result parsing — parallel to `internal/profilewrite/`.
- `internal/config/`: a helper to enumerate `cli` values across all profiles in user + project configs without forcing active-profile resolution.
- `.goreleaser.yaml`: already includes `formula: agent-plugin` (no change needed).
- Tests in `internal/onboarding/native/` and the new `internal/agentplugin/` package.
