## Why

Today, agent profiles and user workflows are configured per-project under `.agent-runner/`, which forces users to redeclare the same base profiles (e.g. `headless_base`, `interactive_base`) and copy reusable workflows into every repo. A global config layer at `~/.agent-runner/` lets users define shared profiles and workflows once and have every project pick them up automatically, while still allowing per-project overrides.

## What Changes

- Load a global config from `~/.agent-runner/config.yaml` if present, in addition to the existing project-local `.agent-runner/config.yaml`.
- Merge profiles by name across the two files. The project file is authoritative on conflict: a project profile fully replaces (not field-merges) a global profile of the same name.
- Resolve `extends:` against the merged profile set, so a project profile MAY extend a profile defined only in the global file (and vice versa). Cycles and missing parents are detected against the merged graph.
- Resolve bare workflow names against `~/.agent-runner/workflows/` as a fallback when no project-local match exists. Project workflows shadow global workflows of the same relative path. Namespaces (`<ns>:<name>`) remain builtin-only and do not consult the global directory.
- Do not auto-generate `~/.agent-runner/config.yaml`. Project auto-generation behavior is unchanged.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-profiles`: adds the global config file as a second profile source, defines merge precedence, and clarifies that `extends:` resolves across the merged set.
- `workflow-name-resolution`: adds `~/.agent-runner/workflows/` as a fallback search path for bare workflow names, with project files taking precedence.

## Out of Scope

- A `setup` / `init` subcommand or interactive wizard for creating `~/.agent-runner/config.yaml`. Auto-generation of the global file is intentionally deferred; a future change will add an opt-in setup flow that lets the user choose where defaults live (global vs project) at first run. For now, users who want a global config create the file by hand.
- XDG Base Directory support (`$XDG_CONFIG_HOME`). The path is fixed at `~/.agent-runner/config.yaml`.
- Field-level merging of profiles across files. The project file replaces the whole profile on a name conflict; users who want partial overrides should use `extends:`.
- A `user:` (or any other) namespace for accessing global workflows. Bare names are the only entry point; namespaced names continue to resolve only against embedded builtins.
- Any change to how the runner discovers `.agent-runner/` itself (still rooted at the current working directory).

## Impact

- **Code**: `internal/config/` — config loader gains a global-file load step and a profile merge pass before validation/cycle detection. `internal/loader/` (or wherever workflow file resolution lives) — bare-name resolution gets a second lookup directory.
- **CLI surface**: no new flags or commands. Behavior is purely additive: existing projects without a global file behave exactly as today.
- **Specs**: `agent-profiles` and `workflow-name-resolution` updated.
- **Backwards compatibility**: fully backwards compatible. A user who never creates `~/.agent-runner/config.yaml` or `~/.agent-runner/workflows/` sees no change.
