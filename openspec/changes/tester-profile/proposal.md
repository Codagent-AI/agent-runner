## Why

Acceptance testing currently reuses the general-purpose `reviewer` profile, so users cannot independently select a testing-optimized CLI and model family. Native setup also configures only planner and implementor despite workflows relying on additional review roles; recommending independent model families for creation and evaluation roles reduces correlated blind spots without requiring users to design that configuration themselves.

## What Changes

- Add a dedicated `tester` agent profile for autonomous acceptance testing.
- Extend native profile setup to recommend concrete CLI and model selections for `planner`, `implementor`, `reviewer`, and `tester` using maintained, role-specific CLI and model-family precedence data and the choices available on the host.
- Prefer different model families for planner versus reviewer and implementor versus tester when the available choices permit; fall back deterministically and explain the limitation when they do not.
- Let users accept the complete four-role recommendation or customize each role's CLI and model individually.
- Preserve required autonomous-backend and permission-mode choices on both setup paths, recommend the headless backend for new setup selections, and retain interactive backend choices without making billing claims that depend on external provider policy.
- Write direct, independently configurable entries for all four roles while preserving unrelated agents and configuration.
- Route both initial acceptance testing in `implement-change2` and targeted re-acceptance testing in `accept-change` through the `tester` profile; keep proposal, specification, design, task, and code review work on `reviewer`.
- **BREAKING** Change the built-in `reviewer` profile from interactive to autonomous by default, aligning it with the direct profile written by setup. Built-in review workflows already force autonomous execution and are unaffected; user-authored workflows that rely on `reviewer`'s implicit interactive mode must add an explicit mode override.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-profile-editor`: Recommend and configure the standard four role profiles, including diversity-aware fallback behavior and accept-all or per-role customization paths.
- `agent-profiles`: Document the existing built-in `reviewer` role, add the built-in autonomous `tester` role, and make `reviewer` autonomous by default.
- `native-setup`: Start profile configuration with a host-informed four-role recommendation and branch into either immediate acceptance or sequential customization.
- `native-setup-ux`: Present recommendation loading, summary, fallback, and customization states with accurate explanatory copy and progress accounting.

## Impact

- Native setup TUI state, adapter/model discovery, recommendation logic, explanatory copy, collision handling, and profile-writing inputs will expand from two configured roles to four.
- Built-in profile defaults and agent-profile documentation will gain `tester`; user-authored workflows that relied on `reviewer` defaulting to interactive will need an explicit mode override.
- The embedded `openspec:implement-change2` and `openspec:accept-change` workflows will require the `tester` profile. The built-in `default` profile set provides it automatically, while custom active profile sets must define or inherit `tester`.
- Recommendation logic will maintain model-family classification and precedence data that must evolve as supported CLIs expose new model names and families.
- Existing configuration files remain readable; setup continues to preserve unmanaged agents and unrelated profile sets. Existing hand-authored `reviewer` or `tester` entries become setup-managed collisions and are replaced only after the user confirms overwrite.
