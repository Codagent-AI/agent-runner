## Context

The codebase currently uses "headless" to mean autonomous agent execution. Mode values (`interactive` / `headless`) flow from profile config through step resolution to adapter arg construction via a `Headless bool` on `BuildArgsInput`. The settings editor has a single theme field. There is no mechanism for users to choose between headless and interactive invocation for autonomous steps.

Key current architecture:
- `StepMode` is a string type in `internal/model/step.go` with constants `ModeInteractive` and `ModeHeadless`
- `internal/config/config.go` validates `default_mode` against `validDefaultMode = {"interactive": true, "headless": true}` and defines built-in profiles (`headless_base`, `implementor`, `summarizer`)
- `internal/cli/adapter.go` defines `BuildArgsInput` with a `Headless bool` field consumed by all five adapters
- `internal/exec/agent.go` resolves step mode via `ResolveAgentStepMode()`, then sets `headless := mode == model.ModeHeadless` to build adapter input
- `internal/usersettings/settings.go` stores `Theme` and lifecycle tracking; the settings editor in `internal/settingseditor/editor.go` is hardcoded to a single theme field

## Goals / Non-Goals

**Goals:**
- Rename the mode concept from headless to autonomous throughout the codebase
- Add a user setting (`autonomous_backend`) controlling invocation style for autonomous steps
- Expose the setting in the settings editor and native setup flow
- Route autonomous steps through the correct invocation backend
- Align permission flags across all five CLI adapters

**Non-Goals:**
- Changing the default autonomous backend away from headless (future consideration)
- Adding new CLI adapters
- Backward compatibility for `mode: autonomous` in workflow YAML (pre-release, clean break)

## Approach

Four layers of change, each building on the previous:

### Layer 1: Rename (model + config)

Rename `ModeHeadless` to `ModeAutonomous` in `internal/model/step.go`. Update `validDefaultMode` in `internal/config/config.go` to `{"interactive": true, "autonomous": true}`. Rename the built-in profile `headless_base` to `autonomous_base` with `default_mode: "autonomous"`. Update `implementor` to extend `autonomous_base`. Update `summarizer` to `default_mode: "autonomous"`. Update all workflow YAML files that reference `mode: headless`.

### Layer 2: Settings (usersettings + settingseditor + native setup)

**Settings struct:** Add `AutonomousBackend` field to `internal/usersettings/settings.go` as a named string type with three constants:
```
BackendHeadless          = "headless"
BackendInteractive       = "interactive"
BackendInteractiveClaude = "interactive-claude"
```
The YAML node-based load/save already preserves unknown fields, so adding a new recognized field follows the existing theme pattern. When absent from the file, `Load()` returns `BackendHeadless` as the default. Invalid values produce a validation error at load time.

**Settings editor:** Replace the single `selected usersettings.Theme` cursor with a flat integer index over all options across both fields. Define fields as an ordered slice of structs, each with a label and options list:
- Field 0 "Theme": options [Light, Dark]
- Field 1 "Autonomous Backend": options [Headless, Interactive, Interactive for Claude]

The `move(delta int)` function uses `(cursor + delta + total) % total` to wrap. Rendering iterates the field slice, inserting a label before each option group. The cursor maps to concrete field values via index arithmetic.

**Native setup:** After the implementor CLI selection (where the billing disclosure is shown), the setup presents an "Autonomous Backend" selection screen with the three options and a one-sentence explanation of each. `interactive-claude` is pre-selected as the recommended default. The chosen value is written to `settings.yaml` when setup completes (alongside `setup.completed_at`).

### Layer 3: Invocation context (cli adapter)

Define `InvocationContext` as a named string type in `internal/cli/adapter.go`:
```go
type InvocationContext string

const (
    ContextInteractive           InvocationContext = "interactive"
    ContextAutonomousHeadless    InvocationContext = "autonomous-headless"
    ContextAutonomousInteractive InvocationContext = "autonomous-interactive"
)

func (c InvocationContext) IsInteractive() bool { return c == ContextInteractive }
func (c InvocationContext) IsAutonomous() bool  { return c != ContextInteractive }
func (c InvocationContext) IsHeadless() bool    { return c == ContextAutonomousHeadless }
```

Replace `Headless bool` in `BuildArgsInput` with `Context InvocationContext`. Each adapter's `BuildArgs` method switches on the context:

- **Print/exec mode flag** (e.g., Claude's `-p`, Copilot's `-p`): emitted when `input.Context.IsHeadless()`, omitted for interactive and autonomous-interactive.
- **Permission flags**: emitted when `input.Context.IsAutonomous()`, omitted for interactive. Specific flags per adapter:
  - Claude: `--permission-mode acceptEdits`
  - Codex: `--sandbox workspace-write` (replaces `--dangerously-bypass-approvals-and-sandbox`)
  - OpenCode: no permission flags (remove `--dangerously-skip-permissions`; default behavior suffices)
  - Copilot: `--allow-tool='write'` (replaces `--allow-all`)
  - Cursor: keep `--trust`, remove `--force`
- **Autonomy flags**: emitted when `input.Context == ContextAutonomousInteractive`. Currently only Copilot has one: `--autopilot`. Other adapters may add these in the future.
- **AskUserQuestion blocking**: the runner includes `AskUserQuestion` in `DisallowedTools` when `input.Context.IsAutonomous()`, regardless of backend.

### Layer 4: Routing (executor)

Add `AutonomousBackend string` field to `ExecutionContext` in `internal/model/context.go`, set from loaded user settings at run start in the runner.

In `internal/exec/agent.go`, after resolving the step mode to `ModeAutonomous`, compute the `InvocationContext`:

1. If mode is `interactive` → `ContextInteractive`
2. If mode is `autonomous`:
   a. Determine desired backend from `ctx.AutonomousBackend`:
      - `"interactive"` → wants interactive for all adapters
      - `"interactive-claude"` → wants interactive only if adapter is Claude
      - `"headless"` (or empty) → wants headless
   b. If backend wants interactive AND `term.IsTerminal(os.Stdin.Fd())` → `ContextAutonomousInteractive`
   c. If backend wants interactive but no TTY → `ContextAutonomousHeadless` + log warning
   d. Otherwise → `ContextAutonomousHeadless`

**System prompt enrichment:** Rename `headlessPreamble` to `autonomyPreamble`. For `ContextAutonomousHeadless`, prepend the preamble as today. For `ContextAutonomousInteractive`, prepend the preamble plus continuation signal instructions (telling the agent to signal `<<DONE>>` when finished, matching the existing interactive step continuation mechanism). For `ContextInteractive`, no preamble.

## Decisions

1. **InvocationContext as named string type with methods.** Single field on `BuildArgsInput` eliminates invalid state. Helper methods (`IsInteractive()`, `IsAutonomous()`, `IsHeadless()`) give adapters clean boolean checks without string comparisons. Alternatives considered: two-field approach (Autonomous bool + Backend string) was rejected because it allows invalid state combinations.

2. **AutonomousBackend on ExecutionContext.** The setting is run-level state that affects execution, same category as session IDs and variables. Set once at run start, read by the executor. No new imports in the exec package. Alternative: passing through function parameters would require changing signatures through the call chain for a single value.

3. **Flat-list editor navigation.** Single integer cursor over all options across both fields (0=Light, 1=Dark, 2=Headless, 3=Interactive, 4=Interactive for Claude). Wrap at boundaries. This matches the spec requirement for Up/Down moving through all options across fields. Alternative: tab-between-fields navigation was rejected — the spec settled on flat-list.

4. **Permission flags are per-adapter constants.** Each adapter hardcodes its permission flags for autonomous contexts. No config-driven flag mechanism. The flags were manually tested against all five CLIs.

5. **TTY detection via `term.IsTerminal`.** Uses `golang.org/x/term` (already a dependency) at the point where invocation context is computed in the executor. Per-step, not global — each step checks at dispatch time.

6. **Setup pre-selects interactive-claude.** The billing disclosure for Claude is shown immediately before the autonomous backend choice. Pre-selecting `interactive-claude` guides users toward the cost-avoiding option. The runtime default remains `headless` for existing users who skip setup.

## Risks / Trade-offs

- **Permission flag breakage** — Tighter permission flags may cause adapters to stall on operations they could previously auto-approve. → Mitigation: flags were manually tested. Tests will verify exact flag sets per adapter.
- **Interactive autonomous is new territory** — Running an agent interactively with autonomy instructions is untested in production. → Mitigation: default backend is `headless`, users opt in. The continuation mechanism already works for interactive steps.
- **No migration for `mode: autonomous`** — Users with custom workflow YAML get a validation error. → Mitigation: pre-release project, clear error message.
- **Setup vs runtime default mismatch** — Setup pre-selects `interactive-claude` but the runtime default is `headless`. Users who complete setup get `interactive-claude`; users who skip setup get `headless`. → This is intentional: setup users made an informed choice; non-setup users (CI, Docker) should get the safe default.
