## Context

The multi-profile change landed a closed-bundle model: each named profile set is a self-contained `agents:` map, and the only inheritance was at the agent level within a single profile set. The design doc for that change explicitly listed cross-profile-set extends as a non-goal, arguing it would couple bundles and make "switch profile" stop being a clean swap.

In practice, users who want to share a small set of base agents across profile sets (e.g., a `headless_base` defined globally, reused in both a `default` project profile and a `copilot` project profile) have to copy agents. The pain point is acute for users who maintain a global `~/.agent-runner/config.yaml` and want per-project variations.

This change reverses the non-goal. The triggering use case is explicit: a **project** profile set should be able to `extends` a **global** profile set and specialize agents by adding or wholesale-overriding them.

## Goals / Non-Goals

**Goals:**
- Let a profile set declare a single parent profile set via `extends: <name>`.
- Allow the child to both add new agents and replace parent agents by name.
- Make the feature work cleanly across the existing global/project file layering so the natural pattern — "global defines a base, project extends it" — works out of the box.
- Preserve all existing error surfaces: base completeness, effort/mode/cli validation, agent-level cycle detection, and unknown-parent errors.

**Non-Goals:**
- Multi-parent (`extends: [a, b]`) inheritance. One parent per profile set.
- Agent-level cross-profile references (e.g., `extends: default.headless_base`). Cross-set reuse is exclusively at the profile set boundary.
- Field-level merging for same-name agents across the parent/child set boundary. Same-name agents in the child replace the parent's agent wholesale, matching the existing global/project merge rule at the agent level.
- CLI ergonomics for inspecting or diffing resolved profile sets. A follow-up change can add `agent-runner config show` if useful.

## Approach

Config loading gains one new resolution step, sandwiched between the existing layer merge and per-agent validation:

1. **Parse files.** Defaults (in code), global (`~/.agent-runner/config.yaml`), project (`.agent-runner/config.yaml`). Each file contributes a map of profile sets, where each profile set now has an optional `extends: string` alongside `agents:`.
2. **Merge profile sets by name across layers (unchanged).** For each profile set name, union the agents maps (project agents replace same-named global/defaults agents wholesale). For the new `extends` field, project value wins if set, otherwise global, otherwise defaults.
3. **Resolve profile set `extends` chains (new).** For each merged profile set, walk its `extends` chain to produce its *effective agents map*. At each link, the child's agents override the parent's agents by name wholesale.
4. **Agent-level validation (unchanged API, runs against effective maps).** Base completeness, value constraints, and agent-level cycle detection run against each profile set's effective agents map.
5. **Active profile selection (unchanged).** `active_profile` is still project-only; falls back to `default`.

### Data model change

```go
type ProfileSet struct {
    Extends string             `yaml:"extends,omitempty"` // NEW
    Agents  map[string]*Agent  `yaml:"agents"`
}
```

No change to `Config`, `Agent`, or `ResolvedAgent`. The `ActiveAgents` field continues to expose a flat agent map; the only difference is that it now reflects the post-extend effective map.

### Cycle detection

The existing `detectAgentCycle` pattern ports cleanly. A new `detectProfileSetCycle` traverses the `extends` chain and emits an error naming every set on the cycle path. Self-reference (`extends: a` on profile set `a`) is a special case of the same logic.

### Missing parent

If a profile set's `extends` names a parent that does not exist in the merged-by-name map, loading fails at the resolution step with an error identifying both the child and the missing parent. This happens *before* agent-level validation, so the user sees the structural error first.

### Ordering: merge-then-extend vs. extend-then-merge

We chose **merge-then-extend** over resolving each file's `extends` locally and then merging the resulting effective sets. Rationale:

- **The triggering use case works naturally.** Project declares `extends: team_base`, `team_base` lives only in the global file. Under merge-then-extend, the project's `extends` sees the global `team_base` because merging happened first. Under extend-then-merge, the project would fail to resolve its parent and the user would need to either duplicate the parent or invent a cross-file syntax.
- **Precedence stays consistent with the existing rule.** Today, project agents replace global agents of the same name. With merge-then-extend, the same rule governs what the child profile set sees when it walks its parent chain: the parent's effective agents map has already reconciled global-vs-project.
- **Cycles and parent existence are resolved against one unambiguous graph** (the merged map), so error messages are never per-file.

The trade-off is that a malformed global parent can surface errors through a well-formed project profile set. That matches the existing pattern where agent-level errors in non-active profile sets still block loading.

### Interaction with agent-level `extends`

Because the child profile set's effective agents map includes inherited agents after step 3, agent-level `extends` inside the child naturally resolves against the full effective map. No new syntax is needed. The `agent-profiles` spec's "only agents within the active profile set" clause relaxes to "only agents visible in the containing set's effective map," which includes inherited ones.

## Decisions

- **Single parent, string-valued `extends` only.** Keeps the mental model identical to agent-level extends. Multi-parent would force conflict-resolution rules we don't need to design today.
- **Wholesale override for same-name agents across the set boundary.** Consistent with the existing global/project merge. A child that wants field-level inheritance can still use agent-level `extends` to the parent's agent name (which resolves naturally because the parent's agents are inherited into the child's effective map).
- **`extends` allowed in both files, project wins.** Parallels `active_profile` considered but rejected: `active_profile` is project-only because it is a selection, not a contribution. Profile-set `extends` is a structural declaration of the set itself, so both files should be able to declare it, with the same project-over-global precedence as every other contribution.
- **Merge-then-extend ordering.** See above.
- **Validation runs against every profile set's effective map, not just the active one** (preserves existing behavior and catches structural errors early).

## Risks / Trade-offs

- **Error messages become slightly less local.** An agent defined only in a parent profile set can cause validation errors in a child. Mitigation: error messages always name the profile set in which the offending agent lives, not only the chain that surfaced it.
- **Chains could grow deep and slow.** In practice, cycle detection bounds traversal, and configs rarely exceed 2–3 levels. Not worth engineering for now.
- **Reversing a documented non-goal.** The previous rationale — that profile sets should be clean swaps — still holds for *runtime* behavior (the active profile determines the agent pool). Profile-set extends is a *config-time* composition feature; at runtime, the active profile's effective agents map is still a single, self-contained pool.
