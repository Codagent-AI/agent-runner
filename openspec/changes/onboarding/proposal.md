## Why

New users land in Agent Runner with no guided path: they must read docs, hand-author `config.yaml`, and infer the workflow model from examples. The result is high drop-off before users ever run a real workflow. We have an opportunity to onboard users *with the system itself* — using a workflow to teach workflows — so first-run users finish with a configured environment and a working mental model. This change establishes the foundation: a first-run intro and an interactive agent-profile setup. Later phases (step-types demo, guided task, validator) build on the same primitives.

## What Changes

This change covers **Phase 1 (Intro)** and **Phase 2 (Setup / Agent Profiles)** from `docs/agent_runner_onboarding_workflow_spec.md`. Phases 3–7 are explicitly deferred.

- Add a single new step mode `ui` for rendering structured TUI screens. The primitive supports two screen kinds within the same step type: an *informational* screen (title, body, action buttons) and an *input* screen (title, body, a small set of typed input fields plus action buttons). All later onboarding phases use this one primitive.
- Add an embedded `onboarding` builtin workflow namespace whose initial sub-workflows cover Phase 1 (welcome screen) and Phase 2 (agent-profile setup).
- Add an interactive agent-profile editor: detects locally available CLI adapters and exposes a curated per-adapter model list, lets the user assemble a single agent profile per editor session, and writes the user-confirmed profile to either `~/.agent-runner/config.yaml` (global) or `.agent-runner/config.yaml` (project). Re-running the editor adds additional profiles.
- Add first-run dispatch keyed on settings content, not file existence: the CLI offers to run the onboarding workflow when `~/.agent-runner/settings.yaml` does not have `onboarding.completed_at` or `onboarding.dismissed` set. Both values are written to settings on completion / explicit dismissal.
- Name the post-dismissal re-entry path: `agent-runner run onboarding:welcome` (the same builtin workflow the first-run dispatcher invokes). Listview / main-menu integration remains deferred.
- Extend `StepMode` validation to accept `ui` and define which fields are valid on UI steps.

## Capabilities

### New Capabilities

- `ui-step`: A new step mode (`mode: ui`) that renders a TUI screen. Supports two screen kinds in one primitive: *informational* (title, body, action buttons) and *input* (title, body, typed input fields, action buttons). Captured input field values are exposed to subsequent steps via the existing capture mechanism. The executor blocks on user input and resolves to a step outcome that downstream `skip_if`/`break_if` logic can read. This is the only UI primitive introduced by this change and is the foundation for all instructional and configuration screens, now and in later phases.
- `onboarding-workflow`: The embedded onboarding workflow itself — its phase structure, content for Phases 1 and 2, lifecycle behavior (resume via standard workflow state, dismiss, completion record), the first-run dispatch rule (gated on `onboarding.completed_at` / `onboarding.dismissed` in settings), and the explicit re-entry invocation `agent-runner run onboarding:welcome`.
- `agent-profile-editor`: A user-initiated, interactive flow that detects installed CLI adapters and curated models per adapter, lets the user assemble one agent profile per editor session (re-runnable to add more), and writes the result to global or project config. Defines the *user-initiated* write path that does not violate the existing "never auto-generate config" rule in `agent-profiles`.

### Modified Capabilities

- `step-model`: Add `ui` to the `StepMode` enum; specify which existing step fields are valid/invalid on UI steps (e.g., `prompt`, `agent`, `cli`, `model`, `capture`, session strategy).
- `builtin-workflows`: Require the binary to embed an `onboarding` namespace alongside the existing `core`, `openspec`, and `spec-driven` namespaces.

## Technical Approach

The change adds one new step primitive and reuses everything else. Onboarding *is* a normal workflow — same loader, same state, same resume — so we avoid one-off code paths.

```
┌──────────────────────────────────────────────────────────────────┐
│                   agent-runner CLI startup                       │
│                                                                  │
│   settings.onboarding.{completed_at,dismissed} unset?            │
│       ── yes ──► offer "run onboarding?"                         │
│       ── no  ──► proceed normally                                │
│                                                                  │
│   manual re-entry: `agent-runner run onboarding:welcome`         │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│           onboarding:welcome  (embedded builtin workflow)        │
│                                                                  │
│   step: intro      mode: ui (informational)                      │
│   step: setup      workflow: setup-agent-profile.yaml            │
│                                                                  │
│   on completion → settings.onboarding.completed_at = <ts>        │
│   on dismissal  → settings.onboarding.dismissed   = <ts>         │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│         setup-agent-profile.yaml (sub-workflow, Phase 2)         │
│                                                                  │
│   mode: ui (informational) → "we'll set up an agent profile"     │
│   mode: ui (input)         → detect adapters → pick adapter,     │
│                              model, scope (global vs project)    │
│   mode: ui (informational) → confirm write to selected file      │
└──────────────────────────────────────────────────────────────────┘
```

**Key technical decisions:**

- **One UI primitive, two screen kinds.** `mode: ui` covers both informational and input screens within a single step type. We deliberately do not split into `mode: ui` + `mode: ui-form` — a single primitive keeps the step model coherent and avoids cross-cutting duplication in validation, capture, and TUI rendering. Field-rendering for the input variant is configured in step body content, not a new mode.
- **`mode: ui` lives next to `interactive`/`headless`, not as a separate top-level construct.** It reuses step-flow control (`skip_if`, `break_if`), workflow state, and validation. The executor lives in `internal/exec/` like every other step type.
- **Reuse the existing TUI stack** (Bubble Tea, `internal/tuistyle/`, patterns from `internal/listview/` and `internal/runview/`) for UI screens. We do not introduce a new UI framework.
- **Adapter/model detection is bounded.** We detect adapters by probing for their CLI binaries on `$PATH` (matches what `internal/cli/` adapters know how to invoke). Models are a *static curated list per adapter* shipped with the binary, not a runtime probe. This keeps Phase 2 deterministic and fast; runtime model discovery is deferred.
- **Profile-editor writes are user-initiated and confirmed.** This preserves the existing rule from `agent-profiles` that the runner "SHALL NOT create config files automatically." The editor is an explicit user action, not startup behavior — the spec for the new `agent-profile-editor` capability will make this distinction precise.
- **First-run gating is content-based, not file-based.** The dispatcher reads `~/.agent-runner/settings.yaml` and triggers onboarding when neither `onboarding.completed_at` nor `onboarding.dismissed` is set. A user with an unrelated settings file (e.g., `theme:`) still sees onboarding on a true first run, and a user who dismissed will not be re-prompted because their file already records `dismissed`.
- **Settings shape is closed for this change.** The new `onboarding` key has exactly two fields: `completed_at` (RFC3339 timestamp) and `dismissed` (RFC3339 timestamp). The `user-settings-file` capability's existing forward-compat rule covers any future additions.
- **Onboarding state = normal workflow state.** Resume, mid-phase exit, and re-entry use the existing resume-by-session-id machinery. We do not invent an onboarding-specific state file.

**Risk areas:**

- *Input-variant schema shape.* `mode: ui` input screens need a schema for fields (kind, label, default, validation). Picking the right minimum schema — small enough to ship Phase 2 without over-design, broad enough that later phases don't need a new primitive — is a design-phase concern.
- *Cross-platform TUI behavior.* Existing TUI code is exercised, but new screens add surface area. Manual verification on macOS and Linux terminals is part of the rollout.

## Out of Scope

- Phases 3–7: step-types demo, guided real workflow, validator setup, validation run, advanced + help screens, tutorial agent, help agent.
- "Onboarding" main-menu / tab integration in the listview (replay UI, "continue onboarding" entry). The CLI invocation `agent-runner run onboarding:welcome` is the only re-entry path until that work lands.
- Runtime model discovery (querying an adapter for its supported models). Phase 2 uses a static curated list per adapter.
- Multi-profile-per-session and `extends` editing in the UI editor. One editor session writes one profile; the user re-runs the editor to add more, and complex inheritance editing stays in the YAML for now.
- Step-internal branching on `mode: ui` actions beyond what `skip_if` / `break_if` reading the step outcome already supports. Rich conditional flows (multiple distinct outcome values, in-step routing) are deferred until a later phase needs them.
- Telemetry on onboarding completion or drop-off.

## Impact

- **`internal/model/step.go`**: extend `StepMode` with `ui`; update `Step.Validate` to accept `ui` and constrain which fields are legal on UI steps.
- **`internal/exec/`**: new UI-step executor (`ui.go`) plus tests; integrates with the existing TUI machinery.
- **`internal/cli/`**: small adapter-availability helper used by the profile editor (probe binary on `$PATH`); curated per-adapter model list lives here too.
- **`internal/config/`**: new write path for user-initiated profile updates (atomic write, mode `0o600`, parent-dir creation matching the settings-file pattern).
- **`internal/usersettings/`**: add the `onboarding` key with two fields, `completed_at` and `dismissed`. Reuse the existing atomic-write infra and the package's "ignore unknown keys" rule.
- **`cmd/agent-runner/`**: first-run check at startup that reads `settings.onboarding.{completed_at,dismissed}` and offers to launch the onboarding workflow when neither is set; routes to `onboarding:welcome` on confirmation.
- **`workflows/onboarding/`** (new): YAML files for the welcome workflow and the agent-profile setup sub-workflow, embedded via the existing `workflows/` embed mechanism.
- **`openspec/specs/`**: new `ui-step`, `onboarding-workflow`, `agent-profile-editor` capabilities; deltas to `step-model` and `builtin-workflows`.
- **Docs**: a short pointer in user docs noting the onboarding workflow exists and how to re-run it after dismissal.
