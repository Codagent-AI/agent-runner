## Context

Agent configuration (mode, CLI backend, model) is currently scattered across individual workflow step definitions. Each step independently sets `mode`, `model`, and `cli` with no reuse or consistency guarantees. The runner has two CLI adapters (Claude, Codex) registered in a hard-coded registry, per-step model/cli overrides, and an engine enrichment system that injects step-specific context into prompts.

This design introduces named agent profiles loaded from a project-level config file, with inheritance and session-aware resolution.

## Goals / Non-Goals

**Goals:**
- Centralize agent configuration into named, reusable profiles
- Support profile inheritance to reduce duplication
- Auto-generate sensible defaults on first run
- Clean up workflow files by removing redundant mode/session declarations
- Add effort level support to CLI adapters

**Non-Goals:**
- Runtime profile switching or parameterized profile selection
- Profile-to-CLI compatibility validation (e.g., checking model availability)
- Environment-specific profile sets (dev/staging/prod)
- Unifying with `.validator/config.yml` adapter configuration

## Approach

```text
.agent-runner/config.yaml          internal/config/
+-----------------------+          +----------------------+
| profiles:             |  load    | Config struct        |
|   interactive_base:   | -------> |   Profiles map       |
|     default_mode: ... |          | Profile struct       |
|   planner:            |  resolve |   DefaultMode, CLI,  |
|     extends: ...      | -------> |   Model, Effort,     |
|                       |          |   SystemPrompt,      |
+-----------------------+          |   Extends            |
                                   +----------+-----------+
                                              |
                               passed into    |
                               RunWorkflow    |
                                              v
                                   ExecutionContext
                                   +- ProfileStore (resolved profiles)
                                   +- SessionProfiles map[stepID]string
                                              |
                               ExecuteAgentStep reads profile
                               merges step overrides (mode, model, cli)
                               prepends system_prompt to fullPrompt
                               adds effort to BuildArgsInput
                                              |
                                              v
                                   CLI Adapter (BuildArgs)
                                   +- Effort flag added
                                   +- Everything else unchanged
```

### New package: `internal/config`

Dedicated package for runner-level configuration.

- **`Config` struct** with a `Profiles map[string]*Profile` field.
- **`Profile` struct**: `DefaultMode`, `CLI`, `Model`, `Effort`, `SystemPrompt`, `Extends` (all strings, optional via `yaml:",omitempty"`).
- **`LoadOrGenerate(path string) (*Config, error)`**: reads `.agent-runner/config.yaml` if it exists, otherwise writes the default config file and returns it. Validates base profile completeness (must have `default_mode` and `cli`), detects inheritance cycles, and resolves `extends` references.
- **`(c *Config) Resolve(name string) (*ResolvedProfile, error)`**: walks the `extends` chain, merges fields (child overrides parent), returns a `ResolvedProfile` with all fields materialized. Uses a visited set for cycle detection.
- **`ResolvedProfile` struct**: same fields as `Profile` but guaranteed to have `DefaultMode` and `CLI` populated. `Model`, `Effort`, `SystemPrompt` may be empty (meaning "not set, don't pass to CLI").

### Changes to `model.Step`

- **Add** `Agent string` field (`yaml:"agent,omitempty"`).
- **Keep** `Mode` field but it becomes an optional override, no longer a type discriminator.
- **`StepType()`** detection changes: agent steps identified by `step.Prompt != "" || step.Agent != ""` instead of checking `step.Mode == ModeInteractive/ModeHeadless`.
- **`isAgentContext()`** updated similarly.
- **Remove** `ModeShell` constant. `Mode` enum is just `ModeInteractive` and `ModeHeadless`, used only as step-level overrides.
- **`ApplyDefaults()`** updated for session strategy: first agentic step (has `prompt`) in a workflow defaults to `session: new`; all subsequent agentic steps default to `session: resume`. This requires the workflow-level `ApplyDefaults()` to track whether the first agentic step has been seen.
- **Validation** updated:
  - `agent` required when session=new on agent steps, forbidden when session=resume/inherit.
  - `agent` forbidden on shell steps.
  - `mode` values restricted to interactive/headless (no more "shell").
  - `mode: shell` on `command` steps is a validation error.

### Changes to `ExecutionContext`

- **Add** `ProfileStore interface{}` field (typed as `*config.Config` at runtime, stored as `interface{}` to avoid circular imports, following the `EngineRef` pattern).
- **Add** `SessionProfiles map[string]string` — maps session-originating step ID to profile name. Populated when a new-session step stores its session ID. Read by resume/inherit steps to determine their profile.
- **Persisted** in `state.json` via `writeStepState` so profiles survive resume-after-restart.

### Changes to `ExecuteAgentStep`

New `resolveStepProfile()` function:
1. For `session: new` steps: reads `step.Agent`, calls `ProfileStore.Resolve(name)`.
2. For `session: resume/inherit` steps: looks up the profile name from `SessionProfiles[ctx.LastSessionStepID]`, then resolves it.
3. Applies step-level overrides: `step.Mode` overrides `resolved.DefaultMode`, `step.Model` overrides `resolved.Model`, `step.CLI` overrides `resolved.CLI`.

Prompt construction changes:
- If the resolved profile has `system_prompt`, it is prepended to the `fullPrompt` before the step prompt and engine enrichment. Order: `[profile system_prompt] [step prompt] [engine enrichment]`.

BuildArgsInput changes:
- `Effort` field populated from the resolved profile's effort (after override merge).

### Changes to `cli.BuildArgsInput` and adapters

- **Add** `Effort string` field to `BuildArgsInput`.
- **Claude adapter**: when `Effort` is non-empty, appends the appropriate effort flag.
- **Codex adapter**: when `Effort` is non-empty, appends the appropriate effort flag.

### Changes to `DispatchStep`

Agent detection in the dispatch switch changes from:
```go
step.Mode == model.ModeInteractive || step.Mode == model.ModeHeadless
```
to:
```go
step.Agent != "" || step.Prompt != ""
```

### Workflow file updates

All 5 workflow files updated mechanically:
- Shell steps: remove `mode: shell` (was redundant).
- First agentic step: add `agent: <profile_name>`, remove `session: resume` (defaults to `new`).
- Subsequent agentic steps: remove `mode:` and `session: resume` (both defaulted). Add `mode: headless` only where overriding the profile's `default_mode`.
- No changes to shell steps, loops, or sub-workflow references.

## Decisions

| Decision | Choice | Rationale | Alternatives considered |
|----------|--------|-----------|------------------------|
| Profile field name | `default_mode` not `mode` | Mode is overridable per-step; naming makes the default nature explicit | `mode` — ambiguous whether it's a default or mandate |
| System prompt delivery | Prepend to fullPrompt, same channel | No new delivery mechanism. Works uniformly for all CLI adapters. Profile identity goes first, then step prompt, then engine enrichment | Separate `--append-system-prompt` — only works for Claude, adds Codex fallback complexity |
| Session-to-profile tracking | `SessionProfiles` map in ExecutionContext | O(1) lookup for resume steps. Persisted in state.json for resume-after-restart | Walk back through workflow to find originating step — requires traversal, doesn't survive restart without extra state |
| Config auto-generation | Generate on missing, never modify existing | Avoids surprising users who've customized. Generated file serves as schema documentation | Fail on missing — unfriendly first-run experience |
| `mode: shell` removal | Shell steps identified by `command` field only | Already redundant in `StepType()`. Simplifies mode enum to interactive/headless only | Keep for explicit typing — unnecessary noise in workflow files |
| Unknown profile ref timing | Fail at workflow validation (load time) | Config loaded before workflows; all profile names known. Catches errors before any step executes | Fail at step execution time — delays error discovery |
| Config package location | New `internal/config` | Config is a separate concern from workflow types. Will grow beyond profiles | Types in `model`, loading in `loader` — mixes concerns |

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Breaking all workflow files at once | Mechanical transformation. Existing test infrastructure covers workflow loading and execution. |
| `SessionProfiles` not persisted on crash | `writeStepState` already persists after every step. Add `SessionProfiles` to the state struct alongside `SessionIDs`. |
| Config auto-generation creates file user didn't ask for | File is small, well-commented, only created when missing. Users can delete and it regenerates. |
| `ProfileStore` as `interface{}` in ExecutionContext | This follows the existing `EngineRef` pattern. Type assertions are done at usage sites, and this can be improved later with a shared interface package. |

## Migration Plan

1. Add `internal/config` package: Profile types, LoadOrGenerate, Resolve, cycle detection, auto-generation
2. Add `Effort` to `cli.BuildArgsInput`, update Claude and Codex adapters to emit effort flags
3. Add `Agent` field to `model.Step`, update `StepType()`, `isAgentContext()`, remove `ModeShell`
4. Update `ApplyDefaults()` for session strategy defaults (first agentic step -> new, rest -> resume)
5. Update validation: `agent` required/forbidden rules, `mode` restricted to interactive/headless
6. Add `ProfileStore` and `SessionProfiles` to `ExecutionContext`, persist in state.json
7. Update `ExecuteAgentStep`: profile resolution, system_prompt prepend, effort pass-through
8. Update `DispatchStep` agent detection
9. Update all 5 workflow files
10. Update tests throughout

No rollback strategy needed — this is pre-release software. All changes are in-tree.

## Open Questions

None — all architectural decisions resolved during design.
