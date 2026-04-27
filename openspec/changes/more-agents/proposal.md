## Why

Agent Runner currently supports four CLI backends, but two of them (Copilot, Cursor) reject interactive mode at runtime, and OpenCode — a popular open-source coding agent — has no adapter at all. This forces users onto Claude or Codex whenever a workflow step needs an interactive session, and locks out OpenCode users entirely. Closing these gaps lets workflows mix and match models and providers freely without restructuring around backend limitations.

## What Changes

- Add interactive-mode argument construction to the Copilot adapter and remove its `InteractiveRejector` implementation.
- Add interactive-mode argument construction to the Cursor adapter and remove its `InteractiveRejector` implementation.
- Add session-ID discovery for Cursor interactive sessions (the existing `stream-json` channel is `-p`-only).
- Introduce a new `OpenCode` adapter supporting both headless and interactive modes, with session creation, resume, and session-ID discovery.
- Register `opencode` in the CLI adapter registry and accept `cli: opencode` in agent-profile and workflow configuration.
- Update spec coverage for the three affected/new CLI backends.

**Observable behavior change:** Copilot and Cursor steps in interactive mode currently fail at runtime with a documented "interactive mode not supported" error. After this change those steps run instead. Any workflow, test, or external tooling depending on those error messages will need to adapt. No existing headless behavior changes.

## Capabilities

### New Capabilities
- `opencode-cli-support`: how the runner integrates with the OpenCode CLI as an agent backend in headless and interactive modes — invocation args, session lifecycle, session-ID discovery, registry/config wiring, and model/effort/system-prompt/disallowed-tools handling to the extent OpenCode's CLI exposes equivalent flags (inputs without an equivalent are silently ignored, matching the established Cursor pattern).

### Modified Capabilities
- `copilot-cli-support`: remove the "interactive mode rejected at runtime" requirement and add an "interactive invocation" requirement plus interactive session-ID discovery rules.
- `cursor-cli-support`: remove the "interactive mode rejected at runtime" requirement, add an "interactive invocation" requirement, and add an interactive session-ID discovery requirement (separate from the existing headless `stream-json` path).
- `cli-adapter`: extend the registry/validation requirements so `opencode` resolves alongside `claude`, `codex`, `copilot`, and `cursor` (including fixing the registry example list at `openspec/specs/cli-adapter/spec.md:9` which currently omits Cursor), and extend the system-prompt-capability requirement to declare both Cursor's stance (currently absent — pre-existing drift this change will fix) and OpenCode's stance.

## Technical Approach

The `cli.Adapter` abstraction in `internal/cli/adapter.go` was built for this — adding a backend or a mode is a localized change. No runner, executor, or workflow code needs to change beyond the adapter layer and the config validator.

```
                        ┌───────────────────────────────┐
                        │   internal/exec/agent.go      │
                        │   (mode-agnostic dispatch)    │
                        └───────────────┬───────────────┘
                                        │
                              cli.Adapter interface
                                        │
        ┌─────────────┬─────────────┬───┴──────────┬─────────────┬─────────────┐
        ▼             ▼             ▼              ▼             ▼             ▼
     claude         codex        copilot         cursor       opencode    (registry)
   (h + i now)   (h + i now)   (h now,         (h now,       (NEW —
                                add i)          add i)        h + i)
```

**Per-backend approach:**

- **Copilot interactive** — invoke `copilot` without `-p`/`-s`, keep `--allow-all`/`--autopilot` for autonomy parity, and reuse the existing filesystem-scan session-ID discovery (which is mode-agnostic). Remove `InteractiveModeError()` so the existing `InteractiveRejector` check in `agent.go:158` no longer fires.
- **Cursor interactive** — invoke `agent` without `-p` (which also drops `--output-format stream-json` and `--trust`, both of which are `-p`-only). Session-ID discovery via stream-json no longer applies in interactive mode; replace it with a filesystem scan of Cursor's session storage directory (pattern modeled on the Copilot implementation).
- **OpenCode (new)** — implement the full `Adapter` interface, register it, and add a `opencode-cli-support` spec. Investigation of OpenCode's actual CLI flags, session storage layout, and reasoning/effort surface area is deferred to the design phase. **Design-phase risk:** if OpenCode lacks one of the primitives this proposal commits to (e.g., no headless flag, no on-disk session state, no resume mechanism), the new capability's scope will need to shrink accordingly and this proposal must be revisited.

**Key technical decisions:**

1. **No new optional adapter interfaces.** The existing set (`OutputFilter`, `StdoutWrapper`, `HeadlessResultFilter`, `SessionStore`, etc.) covers what the new modes need; OpenCode can opt in to whichever subset its output format requires.
2. **Interactive session-ID discovery for Cursor follows the Copilot pattern.** Filesystem scan of session-state directories matched on CWD and spawn time. Avoids new abstractions; the Copilot implementation is the reference.
3. **`InteractiveRejector` stays in the abstraction.** Removing the rejector implementations from Copilot and Cursor leaves the interface in place for future backends that genuinely don't support interactive mode.

## Out of Scope

- Changing the `Adapter` interface itself or introducing new optional interfaces beyond what already exists.
- Changing how the runner attaches to or renders interactive sessions (PTY, TUI styling, suspend/resume hooks) — Cursor and Copilot interactive sessions go through the same path Claude/Codex already use.
- Reasoning-effort, model-selection, or tool-restriction surface area for OpenCode beyond what its CLI natively exposes; if OpenCode lacks an equivalent flag, the adapter ignores the input (the established pattern for Cursor's effort/disallowed-tools handling).
- Any change to built-in workflow YAML in `workflows/` to use the new backends.
- Concurrent-session disambiguation improvements for filesystem-scan session discovery (the Copilot implementation already logs a warning when ambiguous; matching behavior is sufficient).

## Impact

- **Code:** `internal/cli/copilot.go`, `internal/cli/cursor.go`, `internal/cli/adapter.go` (registry), new `internal/cli/opencode.go`, plus tests alongside each. `internal/config/config.go` `validCLI` map and the related error message at line 441.
- **Specs:** new `openspec/specs/opencode-cli-support/spec.md`, modified `openspec/specs/copilot-cli-support/spec.md`, modified `openspec/specs/cursor-cli-support/spec.md`, modified `openspec/specs/cli-adapter/spec.md`.
- **Dependencies:** none expected. OpenCode session-state parsing may need YAML/JSON depending on its on-disk format; both are already in `go.mod`.
- **External requirements:** the `opencode` binary must be on `$PATH` for users who select it (mirrors the existing requirement for `claude`, `codex`, `copilot`, and `agent`).
- **User-visible:** `cli: opencode` becomes a valid value in profiles and workflows; `cli: copilot` and `cli: cursor` no longer fail at runtime when used in interactive steps.
