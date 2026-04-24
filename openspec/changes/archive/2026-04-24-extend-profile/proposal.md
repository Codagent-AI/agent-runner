## Why

Today, each profile set is a closed bundle: a project that wants to share agents across teams or reuse a user-level base has to copy every agent into its own profile set. There is no way to say "this project's `copilot` profile set is everything from the `team_base` set, plus or minus a few agents." This change reverses the earlier "no cross-profile-set extends" non-goal and adds profile-set-level inheritance so users can keep shared agents in one place (typically the global config) and specialize them per project.

## What Changes

- Add an optional `extends: <profile_set_name>` field to profile set definitions in `.agent-runner/config.yaml` and `~/.agent-runner/config.yaml`.
- When a profile set declares `extends`, its effective agents map is the parent's effective agents map, with child agents added (new names) or overriding wholesale (same name replaces — no field-level merge across the set boundary, matching the existing global/project rule).
- Single-parent only. Chains are allowed (A extends B extends C). Cycles and missing parents are rejected at config load.
- `extends` is allowed in both the global and project files. When both files declare `extends` on the same profile set name, the project value wins (same precedence rule as agent-level overrides).
- Resolution order: profile sets are first merged across files by name (existing behavior), then each merged profile set's `extends` chain is resolved.
- Agent-level `extends` within a child profile set can reference agents inherited from the parent set, because those agents are part of the child's effective agents map after resolution. This naturally relaxes the previous "only same active profile set" scope for agent-level extends.

## Capabilities

### Modified Capabilities
- `config-profiles`: adds profile-set-level `extends` — syntax, resolution ordering, cycle/missing-parent validation, cross-file precedence.
- `agent-profiles`: updates the "Profile resolution" requirement so agent-level `extends` resolves against the profile set's *effective* agents map (which may include inherited agents), and retires the "extending agents in non-active profile sets is not supported" clause.

## Out of Scope

- Multi-parent (list-valued) `extends` on profile sets.
- Agent-level cross-profile references like `extends: default.headless_base`. Cross-set reuse is only via profile-set `extends`.
- Field-level merging for same-name agents across the parent/child set boundary (child replaces parent wholesale).
- CLI UX for listing or diffing resolved profile sets.

## Impact

- `internal/config/config.go`: parsing (`ProfileSet` gains `Extends`), `buildConfig`/`mergeProfileSets` resolution ordering, new cycle detection at the profile-set level, validation pass updated to run against each set's effective agents map.
- `internal/config/config_test.go`: add coverage for profile-set extends (single-level, chains, overrides, cycles, missing parent, cross-file precedence, agent-level extends pulling in inherited agents).
- Specs under `openspec/specs/config-profiles/spec.md` and `openspec/specs/agent-profiles/spec.md`.
- No workflow-YAML or step-model changes; runtime agent resolution stays identical from the caller's point of view.
