## Context

`internal/config/config.go` currently models config as `Config { Profiles: map[string]*Profile }`, where `Profile` is a CLI/model/mode bundle. Global and project YAML files are unmarshaled into that flat shape and merged at the profile-name level. Workflow steps reference profiles by name via `agent: <name>` (`internal/runner/runner.go`).

This change reshapes the on-disk format and the in-memory model, renames the bundle concept from "profile" to "agent", introduces a selectable top-level profile-set layer, and rejects the old shape outright. It's a pre-release, breaking schema change — there is no auto-migration.

## Goals / Non-Goals

**Goals:**
- A single YAML shape that supports multiple named profile sets and lets a project pick one.
- Keep all existing agent-level features (inheritance, base completeness, merge precedence across files) intact, just scoped to the active profile set.
- Leave workflow syntax (`agent: <name>`) unchanged so existing workflows keep working.
- Fail loudly on the legacy flat shape so users are never silently mis-configured.

**Non-Goals:**
- No runtime profile switching, no CLI flag for overriding `active_profile`, no per-workflow profile selection.
- No automated migration of existing config files; users edit manually.
- No nested profile sets or profile-set inheritance.

## Approach

**In-memory model.** Restructure `config.Config` to:

```go
type Config struct {
    ActiveProfile string                    // empty ⇒ fall back to "default"
    Profiles      map[string]*ProfileSet    // top-level named sets
    // retains a resolved view for workflow execution:
    ActiveAgents  map[string]*Agent         // = Profiles[selected].Agents after merge
}

type ProfileSet struct {
    Agents map[string]*Agent `yaml:"agents"`
}

type Agent struct { /* exact fields of today's Profile */ }
type ResolvedAgent struct { /* exact fields of today's ResolvedProfile */ }
```

Rename `Profile` → `Agent` and `ResolvedProfile` → `ResolvedAgent` throughout (`internal/config`, `internal/runner`, `internal/model`, `internal/cli`, any test helpers). The exported `ProfileStore` field on the runner can stay named for now (it holds the `*Config`) but its internals change.

**Two-pass YAML load.**
1. Unmarshal each file into a permissive intermediate struct that captures `profiles:` as `map[string]yaml.Node` plus `active_profile` as `string`.
2. **Legacy-shape check** (run per file): for each value under `profiles:`, if the mapping contains any of `default_mode`, `cli`, `model`, `effort`, `system_prompt`, or `extends` as a direct key (and does *not* contain `agents:`), fail with a message naming the file and pointing at the offending entry. This is the single source of the "restructure your config" error.
3. Unmarshal the validated structure into the typed model.
4. Enforce `active_profile` presence rules: allowed in project, disallowed in global (reject at load with clear error).

**Merging.** `mergeConfigs(global, project)` now walks profile set names instead of agent names:
- For each profile set name present in either file, take the union of the two `agents:` maps; when an agent appears in both, keep the project version entirely (existing rule, one level deeper).
- Profile set names only in one file pass through untouched.
- `active_profile` is read from `project` only (global's is rejected earlier).

**Selection + fallback.** After merging:
- Determine the selected profile set: `cfg.ActiveProfile` if set, else `"default"`.
- If the selected profile set is absent, fail with an error explaining both paths out (define `default`, or set `active_profile`).
- Populate `cfg.ActiveAgents` from `cfg.Profiles[selected].Agents`. Agent resolution (`Resolve`, cycle detection, `extends` walking) runs against this map only.

**Validation.** Still validate all agents in all profile sets (not just active) — invalid YAML anywhere in the file should block load so mistakes are caught early.

**Auto-generation.** `defaultConfig()` now returns the nested shape with `default` as the only profile set, containing the five existing agents. The file written to disk uses the same structure.

**Callers.** `ProfileStore *config.Config` on the runner is unchanged as a pointer, but any code that walks `cfg.Profiles[name]` to resolve an agent must switch to `cfg.ActiveAgents[name]` (or an accessor method). Grep targets to update: `cfg.Profiles`, `Profile`, `ResolvedProfile`, `Resolve(`.

## Decisions

- **Rename "profile" → "agent" at the per-bundle level, keep "profile" for the new grouping.** The word "profile" is reused because the new concept is what most users will think of as "their profile" (the set of tools they use). Decided with user up front; the biggest ambiguity in the change.
- **`active_profile` is project-only.** Rejected in global to keep global configs portable across machines/projects — a global `active_profile` would silently override every project that didn't set one. Cleaner to force the selector to live with the project.
- **Fallback is literal-name `default`, not implicit first/only.** "Literal `default`" is predictable and self-documenting; the auto-generated config already creates it. "First" is order-dependent in YAML; "only" is fragile once users add a second set.
- **No auto-migration.** Pre-release; migration code would outlive its usefulness. A clear error with the fix is less work and less mystery than silent rewriting.
- **Legacy-shape detection is key-sniffing, not strict type-checking.** We look for bundle-shaped fields (`default_mode`, `cli`, etc.) at the wrong level. This catches the realistic mistake (someone using the old shape) without false-positiving on legitimately empty profile sets.
- **`extends` stays within a single profile set.** Cross-profile-set extends would couple independent bundles and make "switch profile" stop being a clean swap. Cleaner to require each profile set to be self-contained.
- **Non-active profile sets are still validated.** Catches typos and invalid fields early; the marginal cost is zero.

## Risks / Trade-offs

- **User with existing config breaks on upgrade.** Mitigation: the error message must be actionable (include the file path and a one-line example of the fix). This is called out explicitly in `config-profiles` requirements.
- **Legacy-shape detection heuristic is a heuristic.** A user who genuinely names a profile set `cli` (and has no `agents:` under it) would not trip the heuristic — but a profile set with no agents is useless, and the field-key match is on bundle-specific fields. Low risk.
- **"Profile" becomes overloaded in conversation** (profile set vs. the old meaning). The rename makes the code unambiguous; docs and error messages should prefer "profile set" when ambiguity matters, and "profile" when context is clear.
- **Cross-file merge inside a profile set doubles the surface area of the existing merge rule.** The rule itself (project agent fully replaces global agent of the same name) is unchanged, just nested one layer deeper. Tests need to cover both "same profile set name, disjoint agents" and "same profile set name, overlapping agents".
