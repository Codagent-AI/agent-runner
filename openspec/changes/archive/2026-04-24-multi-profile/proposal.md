## Why

Today the config file has a single flat `profiles:` map (`planner`, `implementor`, `headless_base`, …). Users can't keep multiple swappable sets of these bundles — e.g., a "default" set that uses Claude everywhere and a "copilot" set that uses GitHub Copilot everywhere — and switch between them per project without editing every entry. This change introduces a named, selectable top-level grouping so a project can opt into one bundle of agents, while also freeing up the word "profile" for that grouping by renaming the current per-agent bundle concept.

## What Changes

- **Config file schema** (both global `~/.agent-runner/config.yaml` and project `.agent-runner/config.yaml`):
  - Top-level `profiles:` is now a map of named **profile sets** (e.g., `default`, `copilot`, `work`), each containing an `agents:` map.
  - The existing per-agent bundle concept (what was called "profile": `planner`, `implementor`, `headless_base`, etc.) is **renamed to "agent"** and lives under `profiles.<name>.agents.<agent_name>`.
- **Project config** gains a new top-level field `active_profile: <name>` selecting which profile set to use.
  - `active_profile` is **project-only**; setting it in the global config is an error.
  - If the project config omits `active_profile`, the runner falls back to the profile literally named `default`. Missing `default` → config load error.
- **Merging semantics** (global + project):
  - Profile sets with the same name in both files merge their inner `agents:` maps. Within a matching profile set, the existing rule still applies: a project agent entirely replaces a global agent of the same name (no field-level merge). Profile sets whose name appears in only one file pass through as-is.
  - Only the profile set selected by `active_profile` is made available to the running workflow; other profile sets are loaded but not used for resolution.
- **Workflow step field** `agent: <name>` is **unchanged**. It now references an entry in the selected profile's `agents:` map.
- **Auto-generation**: when no project config exists, the runner generates the new nested shape (`profiles.default.agents:` populated with `interactive_base`, `headless_base`, `planner`, `implementor`, `summarizer`).
- **BREAKING** — legacy flat shape (top-level `profiles:` containing agent bundles directly) is rejected at config load with an error telling the user to restructure. No auto-migration. (Pre-release; no compatibility guarantees.)

## Capabilities

### New Capabilities
- `config-profiles`: named top-level profile sets, `active_profile` selection, fallback to `default`, profile-set merging across global and project configs, rejection of the legacy flat shape.

### Modified Capabilities
- `agent-profiles`: renames the concept ("profile" → "agent" for the per-CLI/model/mode bundle); auto-generated config produces the new nested shape; merge precedence and cross-file extends now operate on the agents map inside the matching profile set; agent resolution is scoped to the active profile's agents.

## Out of Scope

- Switching the active profile at runtime, via CLI flag, or per-workflow. The active profile is fixed per project at config load time.
- Automatic migration of existing config files. Users with the legacy shape must edit their files manually.
- Changes to the workflow YAML syntax (`agent:` keyword stays; no new step fields).
- Any interactive setup/wizard flow for generating the global config.
- Nested profiles (profiles extending other profiles). `extends` remains an agent-level concept only.

## Impact

- **Code** — `internal/config/config.go` is substantially reshaped: new top-level `Config` type (map of profile sets), new loader two-pass (resolve `active_profile`, then load agents from the selected set), legacy-shape detection + rejection, updated merge logic, updated `defaultConfig()`. `config_test.go` needs rewritten test fixtures.
- **Callers** — `internal/runner/runner.go` passes `ProfileStore` around; the type it uses (`*config.Config`) changes. Any code that walks `cfg.Profiles` directly must now walk through the active profile's `Agents`. Grep targets: `cfg.Profiles`, `ProfileStore`, `config.Config`, `config.Profile`, `config.ResolvedProfile`, `Resolve(`.
- **User configs** — existing files (including the user's current `~/.agent-runner/config.yaml`) break on next load until restructured. Error message must tell them exactly what to do.
- **Workflows** — no changes. `agent: planner` still works unchanged.
- **Specs** — `openspec/specs/agent-profiles/spec.md` is modified; new `openspec/specs/config-profiles/spec.md` is added.
