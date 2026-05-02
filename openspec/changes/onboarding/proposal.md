## Why

New users land in Agent Runner with no guided path: they must read docs, hand-author `config.yaml`, and infer the workflow model from examples. The result is high drop-off before users ever run a real workflow. We can onboard users *with the system itself* — using a workflow to teach workflows — so first-run users finish with a configured environment and a working mental model. This change establishes the foundation: a first-run intro and an interactive agent-profile setup.

## What Changes

This change covers **Phase 1 (Intro)** and **Phase 2 (Setup / Agent Profiles)** from `docs/agent_runner_onboarding_workflow_spec.md`. Phases 3–7 are explicitly deferred.

- Add a `mode: ui` step type for interactive TUI screens (informational and input variants in one primitive).
- Add a `workflow-bundled-scripts` primitive: a workflow YAML may reference sibling script files via a step-level `script: <path>` field.
- Add an embedded `onboarding` builtin workflow namespace covering Phases 1 and 2.
- Add an interactive agent-profile editor expressed as a sub-workflow composed of bundled-script steps and `mode: ui` steps.
- Add a first-run dispatch path keyed on settings content; persist completion and explicit dismissal in `~/.agent-runner/settings.yaml`.
- Extend `StepMode` to accept `ui` and recognize the new `script:` step.
- Extend captures from string-only to typed values so map and list captures are first-class.

## Capabilities

### New Capabilities

- `ui-step`: A `mode: ui` step that renders informational or input TUI screens, with named action outcomes and single-select inputs whose options may come from a static list or a runtime-resolved value.
- `workflow-bundled-scripts`: A step-level `script:` field that invokes a script bundled alongside the workflow YAML, with declared inputs and structured-output capture.
- `onboarding-workflow`: The embedded onboarding workflow itself — phase structure, lifecycle, first-run dispatch, and re-entry.
- `agent-profile-editor`: A sub-workflow that detects available adapters, lets the user assemble one agent profile per session, and persists it to global or project config.

### Modified Capabilities

- `step-model`: Add `ui` to the `StepMode` enum and recognize the new `script:` step field.
- `builtin-workflows`: Embed the `onboarding` namespace and extend the embed mechanism to include non-YAML files (executable scripts) inside namespace subdirectories.
- `output-capture`: Extend captures from string-only to typed values (`string | list<string> | map<string,string>`), backward-compatible by default; typed shapes are produced only when explicitly opted into.

## Technical Approach

The change adds two new step primitives (`mode: ui` and `script:`) and reuses everything else. Onboarding *is* a normal workflow — same loader, same state, same resume — so we avoid one-off code paths.

```
┌──────────────────────────────────────────────────────────────────┐
│                   agent-runner CLI startup                       │
│   settings.onboarding.{completed_at,dismissed} unset?            │
│       ── yes ──► offer "run onboarding?"                         │
│       ── no  ──► proceed normally                                │
│   manual re-entry: `agent-runner run onboarding:welcome`         │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│           onboarding:welcome  (embedded builtin workflow)        │
│   intro      mode: ui (informational)                            │
│   setup      workflow: setup-agent-profile.yaml                  │
│   completion / dismissal recorded in settings.yaml               │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│         setup-agent-profile.yaml (sub-workflow, Phase 2)         │
│         + bundled scripts: detect-adapters.sh, write-profile.sh  │
│   intro             mode: ui (informational)                     │
│   detect            script: detect-adapters.sh → captured list   │
│   choose            mode: ui (input, options from captured list) │
│   confirm           mode: ui (informational)                     │
│   write             script: write-profile.sh                     │
└──────────────────────────────────────────────────────────────────┘
```

**Shape-level decisions:**

- **One UI primitive, two screen kinds.** `mode: ui` covers both informational and input screens. We deliberately do not split into `ui` + `ui-form`.
- **Runtime values come from bundled scripts, not the executor.** UI input options that depend on the host environment are produced by a preceding `script:` step and consumed via interpolation. The UI executor never probes the environment; the editor never owns adapter-detection logic.
- **First-run gating is content-based, not file-based.** The dispatcher reads `~/.agent-runner/settings.yaml` and triggers onboarding when neither `onboarding.completed_at` nor `onboarding.dismissed` is set. An unrelated existing settings file (e.g., `theme:`) does not suppress onboarding.
- **Onboarding state = normal workflow state.** Resume, mid-phase exit, and re-entry use the existing resume-by-session-id machinery.
- **Profile-editor writes are user-initiated.** This preserves the existing rule from `agent-profiles` that the runner does not auto-create config files. The editor is an explicit user action.

**Risk areas:**

- *Typed captures (central risk).* Moving captures from string-only to typed values (`string | list<string> | map<string,string>`) touches workflow state, audit logs, interpolation, loop capture, resume, and pre-validation. The migration plan keeps string capture behavior exactly compatible by default and produces typed values only when opted in. This is the largest implementation surface in the change.
- *Cross-platform TUI behavior.* New screens add surface area; manual verification on macOS and Linux terminals is part of the rollout.

## Out of Scope

- Phases 3–7: step-types demo, guided real workflow, validator setup, validation run, advanced + help screens, tutorial agent, help agent.
- Listview / main-menu integration for onboarding (replay UI, "continue onboarding" entry). `agent-runner run onboarding:welcome` is the only re-entry path until that lands.
- Full model-catalog integration (paginated model lists, model metadata). Phase 2 queries adapters for their model list at runtime but does not support pagination or model details beyond the name.
- Multi-profile-per-session and `extends` editing in the UI editor. One editor session writes one profile.
- Step-internal branching on `mode: ui` actions beyond what `skip_if` / `break_if` already supports.
- Secrets in UI inputs (no redaction in this version).
- Telemetry on onboarding completion or drop-off.

## Impact

- `internal/model/step.go`: extend `StepMode` with `ui`; recognize and validate the `script:` step type.
- `internal/exec/`: new UI-step executor and script-step executor.
- `internal/loader/`: carry the workflow's source location through to executors.
- `workflows/embed.go`: include non-YAML files alongside YAML in embedded namespaces.
- `workflows/onboarding/` (new): welcome workflow, setup sub-workflow, and bundled scripts.
- `internal/config/`: profile-writer logic invoked by the editor's writer step.
- `internal/usersettings/`: add the `onboarding` key (`completed_at`, `dismissed`).
- `cmd/agent-runner/`: first-run dispatch at startup; new internal subcommand consumed by the editor's writer script.
- `openspec/specs/`: new `ui-step`, `workflow-bundled-scripts`, `onboarding-workflow`, `agent-profile-editor`; deltas to `step-model`, `builtin-workflows`, `output-capture`.
- Docs: short pointer to the onboarding workflow and re-entry command.
