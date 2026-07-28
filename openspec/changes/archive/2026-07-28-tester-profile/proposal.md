## Why

Acceptance testing currently reuses a planning-review profile, so users cannot independently select a testing-optimized CLI and model family. The existing `planner` and `reviewer` names also obscure that one role leads user-facing work while the other independently stress-tests planning artifacts; configuring four clearly named, diversity-aware roles reduces correlated blind spots and role confusion.

## What Changes

- Add a dedicated `tester` agent profile for autonomous acceptance testing.
- Rename `planner` to `lead` and `reviewer` to `crosscheck` across built-in profiles and workflows. Continue accepting the legacy names with undated deprecation warnings, and reject same-layer configurations that define both names from one alias pair.
- Define Crosscheck as an autonomous second planning perspective that challenges Lead's reasoning and checks proposals, specifications, designs, and tasks for omissions. Agent Validator remains responsible for implementation-code review.
- Extend native profile setup to recommend concrete CLI and model selections for `lead`, `crosscheck`, `implementor`, and `tester` using maintained, role-specific CLI and model-family precedence data and the choices available on the host.
- Prefer different model families for lead versus crosscheck and implementor versus tester when the available choices permit; fall back deterministically and explain the limitation when they do not.
- Select Claude flagship/balanced aliases and the newest discovered GPT Sol/Terra model using boundary-safe, version-flexible matching. Prefer the CLI default over guessing from an unclassified model.
- Let users accept the complete four-role recommendation or customize each role's CLI and model individually.
- Preserve required autonomous-backend and permission-mode choices on both setup paths, recommend the headless backend for new setup selections, and retain interactive backend choices without making billing claims that depend on external provider policy.
- Write direct, independently configurable entries for all four roles while preserving unrelated agents and configuration.
- Route both initial acceptance testing in `implement-change2` and targeted re-acceptance testing in `accept-change` through the `tester` profile; keep planning-artifact scrutiny on `crosscheck`.
- Document all four roles, the difference between Crosscheck and Agent Validator, recommendation order and rationale, worked examples, and legacy-name compatibility.
- **BREAKING** The canonical planning-role names become `lead` and `crosscheck`, and Crosscheck defaults to autonomous. Legacy `planner` and `reviewer` references continue to resolve with warnings, but configurations that define both a canonical and legacy alias in the same profile layer must remove one; user-authored workflows relying on Reviewer's implicit interactive mode must add an explicit mode override.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-profile-editor`: Recommend and configure the standard four role profiles, including version-flexible model selection, diversity-aware fallback behavior, legacy-entry migration, and accept-all or per-role customization paths.
- `agent-profiles`: Establish canonical Lead and Crosscheck roles with legacy aliases, add the built-in autonomous Tester role, and make Crosscheck autonomous by default.
- `native-setup`: Start profile configuration with a host-informed four-role recommendation and branch into either immediate acceptance or sequential customization.
- `native-setup-ux`: Present recommendation loading, summary, fallback, and customization states with accurate explanatory copy and progress accounting.

## Impact

- Native setup TUI state, adapter/model discovery, recommendation logic, explanatory copy, collision handling, and profile-writing inputs will expand from two configured roles to four.
- Config loading and agent resolution will canonicalize `planner` to `lead` and `reviewer` to `crosscheck`, surface deduplicated deprecation warnings, and reject ambiguous same-layer alias pairs.
- Built-in profile defaults and agent-profile documentation will use `lead`, `crosscheck`, and `tester`; user-authored workflows that relied on Reviewer's implicit interactive mode will need an explicit mode override.
- The embedded `openspec:implement-change2` and `openspec:accept-change` workflows will require the `tester` profile. The built-in `default` profile set provides it automatically, while custom active profile sets must define or inherit `tester`.
- Recommendation logic will live in a pure internal package with small role-order, family, and tier policies instead of a comprehensive model catalog.
- Existing legacy configuration files remain readable and are not rewritten on load. Setup preserves unmanaged agents and unrelated profile sets; canonical or legacy managed entries are replaced and legacy aliases removed only after the user confirms overwrite.
