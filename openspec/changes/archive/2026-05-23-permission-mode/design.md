## Context

Agent Runner orchestrates autonomous CLI agents (Claude, Codex, Copilot, Cursor, OpenCode). Autonomous workflows like `implement-task` and `implement-change` only work if the agent can actually run the tools the workflow expects: shell, file edits, git, tests. Each underlying CLI has its own permission model. Today the runner picks a per-CLI default that "works" — except for Cursor, where the adapter passes `--trust` (which trusts the workspace) but not `--force` (which actually allows tool execution), so every shell call is silently rejected. This is filed as issue #33.

The narrow fix would be to add `--force` to the Cursor adapter. The user pushed back: that handles this one symptom but leaves the underlying decision ("how much authority does Agent Runner pre-grant to unattended agents?") implicit and per-CLI. The next missing flag on the next CLI will recreate the bug. The user wants a single, visible setting that names the trade-off and lets adapters honor it consistently.

This setting is orthogonal to `autonomous_backend`:

- `autonomous_backend` decides *where/how* autonomous work runs (headless process vs interactive backend vs interactive-for-Claude).
- `autonomous_permission_mode` decides *how much authority* the agent has once it's running.

A user can sensibly want headless-with-conservative, interactive-with-yolo, or any other combination.

## Goals / Non-Goals

**Goals:**

- Give users one explicit, cross-CLI knob for autonomous agent permission breadth.
- Make the trade-off visible during setup (when the user's mental model is being formed) and editable later (reversible without re-doing setup).
- Make the default safe (conservative).
- Bound CLI adapter behavior: each adapter's autonomous flags are governed by this setting, not by ad-hoc per-CLI decisions buried in adapter code.
- Resolve issue #33 by giving Cursor users a documented path to enable autonomous shell execution.

**Non-Goals:**

- Per-tool allowlists (e.g., "allow git but block curl"). Users who want finer control configure their CLI directly.
- Per-step or per-workflow overrides.
- Changing `autonomous_backend` semantics.
- Changing interactive (human-supervised) behavior. The `cli-adapter` "no permission loosening in interactive mode" rule is unaffected.
- A third mode (`strict` / "no permission flags at all"). Conservative is already "today's working baseline"; adding a third value to express "even less" trades user clarity for a use case nobody asked for. Easy to add later if needed.

## Approach

### Setting

Add `autonomous_permission_mode` as a top-level key in `~/.agent-runner/settings.yaml`. Values:

- `conservative` — default. Each adapter emits the per-CLI permission flags it emits today for autonomous contexts. No additional broad-authority flags are added.
- `yolo` — each adapter MAY additionally emit its CLI's broadest-authority flag.

Parser, validator, marshaller, and "preserve unrelated keys" behavior all mirror `autonomous_backend` (see `internal/usersettings/settings.go`). Same atomic-write semantics and same forward-compat treatment of unknown keys.

### Setup UI

A new screen in `internal/onboarding/native/native.go` immediately after the Autonomous Backend screen, before the skills install step. Two options (Conservative, YOLO). The YOLO option additionally carries risk copy. Conservative is pre-selected.

#### Mockup — Conservative focused (default)

```
╭──────────────────────────────────────────────────────────────────────────╮
│                                                                          │
│                  [██████████░░░░░░░░░░░░░░] Step 5 of 8                  │
│                                                                          │
│  Autonomous Permission Mode                                              │
│                                                                          │
│  Choose how much authority autonomous agent steps have when they run.    │
│  This controls whether the runner pre-approves shell, file, and          │
│  network actions for unattended work.                                    │
│                                                                          │
│  Choose the permission mode for autonomous steps.                        │
│                                                                          │
│  ▶ Conservative - Use each CLI's default permission flags. Some          │
│    commands may not work unless you have separately given the CLI        │
│    access to all necessary tools.                                        │
│    YOLO - Allow autonomous agents to run shell, file, and network        │
│    actions without per-command approval. Recommended only when running   │
│    inside an external sandbox such as Docker.                            │
│                                                                          │
╰──────────────────────────────────────────────────────────────────────────╯
```

#### Mockup — YOLO focused

```
╭──────────────────────────────────────────────────────────────────────────╮
│                                                                          │
│                  [██████████░░░░░░░░░░░░░░] Step 5 of 8                  │
│                                                                          │
│  Autonomous Permission Mode                                              │
│                                                                          │
│  Choose how much authority autonomous agent steps have when they run.    │
│  This controls whether the runner pre-approves shell, file, and          │
│  network actions for unattended work.                                    │
│                                                                          │
│  Choose the permission mode for autonomous steps.                        │
│                                                                          │
│    Conservative - Use each CLI's default permission flags. Some          │
│    commands may not work unless you have separately given the CLI        │
│    access to all necessary tools.                                        │
│  ▶ YOLO - Allow autonomous agents to run shell, file, and network        │
│    actions without per-command approval. Recommended only when running   │
│    inside an external sandbox such as Docker.                            │
│                                                                          │
╰──────────────────────────────────────────────────────────────────────────╯
```

The risk copy on YOLO carries two ideas: (a) the broader authority being granted (shell/file/network without per-command approval), and (b) the recommended mitigation (running inside an external sandbox such as Docker). The Conservative copy sets the expectation that the user may need to configure tool access per CLI themselves — this is honest about the trade-off the user is accepting by staying on the safer default.

Final wording is the implementer's call as long as the two ideas above are present on YOLO and the per-CLI tool-access caveat is present on Conservative. The settings editor reuses the same labels (with whatever rendering differences the editor's bordered overlay imposes).

### Editor UI

Add a third field to `internal/settingseditor/editor.go` after Autonomous Backend. The keyboard model already treats fields as a flat option list with wrap; the change is only adding the field's two options to the list and updating the wrap target.

### Adapter plumbing

`BuildArgsInput` gains a `PermissionMode` field. The runner populates it from `Settings.AutonomousPermissionMode` on every autonomous step. Adapters branch on it inside `BuildArgs`:

| CLI | Conservative (today's baseline) | YOLO (additional broader flag) |
|-----|---------------------------------|--------------------------------|
| Claude | `--permission-mode acceptEdits` | `--permission-mode bypassPermissions` (replaces `acceptEdits`) |
| Codex | `--sandbox workspace-write` | `--sandbox danger-full-access` (replaces `workspace-write`) |
| Copilot | `--allow-tool=write --autopilot` | adds `--allow-all-tools` (or verified equivalent) |
| Cursor | `--trust` only | adds `--force` |
| OpenCode | (no permission flag) | (no broader flag known — emit nothing, behave identically) |

The implementer SHALL verify each CLI's exact yolo-mode flag name against the current CLI version before committing to it. Replacements (Claude, Codex) emit one flag in either mode; additions (Copilot, Cursor) emit baseline + yolo flag in yolo mode.

### Resolution at call time

The runner reads the setting once at workflow start and passes the resolved value through `BuildArgsInput` on every step. If the user changes the setting mid-run via the editor, the change applies to subsequent steps started after save (matches existing editor "applies without restart" semantics for theme and backend). No mid-step re-spawn.

## Decisions

### Default is conservative, knowing Cursor stays blocked

The user picked `conservative` as default knowing that Cursor's autonomous shell execution remains blocked out of the box. The reasoning: a conservative default is the safer trade-off across the user base, and the explicit setup screen surfaces the choice immediately. Users who want implement-task to work on Cursor will see the YOLO option during setup with risk copy and can opt in. This is preferable to silently flipping every user into a broader-authority mode on first run.

Implication for documentation: the implement-task workflow docs should note that running it under Cursor requires `autonomous_permission_mode: yolo`. The proposal does not commit to writing those docs — that's a follow-on if the team wants it.

### Two modes, not three

A `strict` value (emit no permission flags) was considered and rejected as out of scope. The conservative mode is already each CLI's working baseline; "less than working baseline" is not a user-requested case and would break the very workflows the runner ships. Two modes keep the editor and setup screens simple. A third value can be added later by extending the enum without breaking existing settings files.

### Replace vs add (per-CLI)

Claude and Codex express permission mode as a single flag with an enum value (`acceptEdits` vs `bypassPermissions`; `workspace-write` vs `danger-full-access`). For these, yolo mode replaces the conservative flag rather than appending. Copilot and Cursor use additive flags (`--allow-all-tools` is added on top of `--allow-tool=write`; `--force` is added on top of `--trust`). The adapter code naturally handles both shapes; the spec describes observable args, not the construction mechanism.

### Setting is autonomous-only

Interactive context is excluded for two reasons:

1. The existing `cli-adapter` requirement ("no permission loosening in interactive mode") already forbids permission-grant flags there. Changing that would be a meaningful security posture change for a different problem.
2. Interactive context has a human at the keyboard to answer prompts. The setting solves a problem (unattended workflow blocked on a prompt) that doesn't exist there.

### Autonomous-interactive also honors the setting

When the user runs with `autonomous_backend: interactive-claude`, the resulting step is still unattended (the agent is supposed to work autonomously inside the interactive backend and signal completion). The same permission-flag logic applies — there's no human to answer prompts even though the underlying CLI is in interactive mode. This is consistent with the existing cli-adapter rule that both autonomous-headless and autonomous-interactive MAY emit permission-grant flags.

### Setting name and value labels

`autonomous_permission_mode` parallels `autonomous_backend` for shelf-readability and grep-ability. Values `conservative` and `yolo` were chosen because "yolo" is the unambiguous, widely-used label for this class of mode in this ecosystem (Claude `--dangerously-skip-permissions`, Codex `--yolo`, etc.) — using it sets the right expectations rather than dressing the trade-off up in neutral language.

## Risks / Trade-offs

- **Yolo means yolo.** Users who flip the setting are explicitly granting broad authority. The risk copy in setup and the editor must make this clear. Documentation/changelog should call out the new setting prominently.
- **Per-CLI yolo flag names may drift.** Claude, Codex, Copilot, and Cursor each evolve their CLI flags independently. The spec talks about "broadest-authority flag where the CLI provides one" rather than naming flags, so when a CLI renames a flag the cli-support spec for that one CLI gets updated without churning the cross-cutting requirement.
- **Conservative-default-leaves-Cursor-blocked is a known UX trade-off.** The first-time Cursor user who runs `implement-task` will see it fail. The mitigation is the setup screen showing the YOLO option at install time. If we discover users still get blindsided, we can revisit the default — the setting is just a value change, no schema migration needed.
- **`BuildArgsInput` plumbing is a touch point across all adapters.** Adding a field to the existing input struct is mechanical but easy to forget for one adapter. Tests for each adapter must cover both modes.
- **No mid-step re-evaluation.** Changing the setting during a long-running run does not retroactively affect in-flight steps. This matches existing theme/backend behavior; if it surprises users we can revisit, but adding it is unnecessary scope here.

## References

- Issue #33: bug: Cursor headless mode missing --force flag, shell commands blocked
- `openspec/specs/cli-adapter/spec.md` — existing "No permission loosening in interactive mode" requirement (preserved)
- `openspec/specs/user-settings-file/spec.md` — `autonomous_backend` setting (precedent for parser/marshaller shape)
- `openspec/specs/native-setup/spec.md` — Autonomous backend selection screen (precedent for setup UI)
