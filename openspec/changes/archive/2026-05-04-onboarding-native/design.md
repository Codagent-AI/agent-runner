## Context

The current first-run path is implemented as the builtin `onboarding:welcome` workflow. That workflow owns the setup welcome screen, setup profile collection, setup dismissal/completion writes, and the step-types demo handoff. This keeps orchestration uniform, but it forces setup into generic workflow UI steps inside the live run view.

The new requirements split the first-run path into two phases:
- mandatory native setup, tracked by `settings.setup.completed_at`;
- optional workflow-based onboarding demo, started as `onboarding:onboarding` and tracked by `settings.onboarding.completed_at` / `settings.onboarding.dismissed`.

Setup cannot be skipped or dismissed. Demo defer/dismiss behavior remains workflow-owned, but the copy and workflow name must no longer present it as the setup welcome.

## Goals / Non-Goals

**Goals:**
- Implement mandatory setup as native TUI functionality outside workflow execution.
- Keep the startup path deterministic: setup gate first, onboarding demo gate second, then the normal TUI entry point.
- Move reusable adapter/model discovery, collision detection, target resolution, and profile writing out of shell scripts and into Go services.
- Preserve the current four-agent profile write semantics and atomic settings/config writes.
- Keep `onboarding:onboarding` and `onboarding:step-types-demo` as builtin workflows for demo behavior.

**Non-Goals:**
- Persisting partial setup wizard state.
- Reusing `mode: ui` workflow screens as the native setup UI.
- Changing the step-types demo behavior beyond adding the new demo intro/handoff wrapper.
- Adding external dependencies.

## Approach

Use a dedicated native setup package, with `cmd/agent-runner` only responsible for first-run orchestration.

```text
bare/list TUI entry
  |
  v
ensureThemeForTUI
  |
  v
ensureFirstRunForTUI
  |
  +-- settings.setup.completed_at unset? --> run native setup TUI
  |                                         |
  |                                         +-- completed: write setup.completed_at
  |                                         +-- cancelled/failed: go home, write nothing
  |
  +-- setup completed and onboarding not completed/dismissed? --> run onboarding:onboarding
  |
  v
normal list/home TUI
```

Create a package such as `internal/onboarding/native` for the setup flow. It should expose a small entry point along these lines:

```go
type Result int

const (
    ResultCompleted Result = iota
    ResultCancelled
)

type Deps struct {
    Settings SettingsStore
    Detector AdapterDetector
    Models ModelDiscoverer
    Profiles ProfileWriter
    Clock func() time.Time
    Cwd func() (string, error)
}

func Run(deps Deps) (Result, error)
```

The package owns the Bubble Tea model and all setup-specific state. It should keep transient choices in memory only: interactive CLI/model, headless CLI/model, scope, target path, and overwrite decision. Cancel, Ctrl-C, or process interruption does not persist wizard progress, so the next eligible launch starts from the beginning.

Move reusable setup operations into Go behind interfaces:
- Adapter detection: `exec.LookPath` over the existing supported adapter list (`claude`, `codex`, `copilot`, `cursor`, `opencode`).
- Model discovery: subprocess wrappers for current supported commands (`claude models list`, `codex debug models`, `opencode models`) with the same best-effort behavior as today: unsupported/empty discovery returns no model choices and writes adapter default.
- Collision detection: parse the selected YAML config with `yaml.v3` and inspect `profiles.default.agents` for `interactive_base`, `headless_base`, `planner`, and `implementor`.
- Profile writing: extract the existing `writeProfile`/merge/atomic-write logic out of `cmd/agent-runner/internal_cmd.go` into an internal package used by both native setup and `agent-runner internal write-profile`.
- Settings writes: extend `internal/usersettings` with `Setup.CompletedAt` while preserving `Onboarding.CompletedAt` and `Onboarding.Dismissed`.

Replace the old workflow shell with a demo workflow:
- Remove `workflows/onboarding/welcome.yaml` and `setup-agent-profile.yaml` as builtin entry points.
- Add `workflows/onboarding/onboarding.yaml`.
- The first step is a UI demo intro with outcomes `continue`, `not_now`, and `dismiss`.
- `continue` invokes `step-types-demo.yaml`, then writes `settings.onboarding.completed_at`.
- `dismiss` writes `settings.onboarding.dismissed`.
- `not_now` writes nothing.
- `step-types-demo.yaml` remains directly runnable.

## Decisions

### Dedicated native setup package

Use a dedicated package rather than adding setup internals to `cmd/agent-runner` or the top-level switcher.

Alternatives considered:
- Reusing `internal/uistep`: less code, but keeps setup constrained by generic workflow-step UI.
- Adding setup as another `switcher` mode: smoother in-process transitions, but couples setup to list/run navigation.

Rationale: setup is a product surface with its own state machine and services. A package boundary makes it testable and prevents `main.go` from becoming the implementation.

### Native setup runs before home as its own Bubble Tea program

Run native setup before constructing the listview switcher, similar to existing theme/onboarding gates.

Alternatives considered:
- Integrate setup into the switcher.
- Run setup through workflow/liverun machinery.

Rationale: the requirement explicitly removes setup from live workflow UI. Running setup as a pre-home TUI keeps the startup order simple and avoids mixing mandatory setup with run navigation.

### Move setup primitives from shell scripts to Go

Adapter/model discovery, collision detection, and profile writing should be Go services.

Alternatives considered:
- Keep invoking bundled shell scripts from native setup.

Rationale: native setup needs direct error handling, fakes for tests, and structured data. Keeping shell scripts as the primary implementation would preserve the old workflow-shaped architecture. Existing scripts can be removed if no longer referenced by remaining workflows, or left only if a workflow asset still needs them.

### Keep demo optionality in workflow

The onboarding demo remains workflow-based and owns `not_now`/`dismiss` through its intro UI step.

Alternatives considered:
- Make demo skip/dismiss native too.

Rationale: the user explicitly wants skip/defer controls to remain in onboarding workflow. This also keeps the demo as a real workflow example while making mandatory setup native.

## Risks / Trade-offs

- Native setup duplicates some visual patterns from `uistep` -> Use shared `tuistyle` and keep UI helpers small; do not import workflow UI semantics into setup.
- Subprocess model discovery can be slow or unavailable -> Run discovery at the point the adapter is selected, treat command failure as empty model list, and write adapter default.
- Moving profile writing out of `cmd` may disturb internal command tests -> Extract without changing the external `agent-runner internal write-profile` contract, then update tests to cover the shared package and the command wrapper.
- Removing builtin `onboarding:welcome` and `onboarding:setup-agent-profile` is breaking -> Update builtin workflow tests, loader tests, docs, and any references to point to `onboarding:onboarding` or native setup.
- Existing users with `settings.onboarding.dismissed` from the old setup flow will skip the new demo -> Accept this as demo dismissal state; mandatory setup is controlled only by `settings.setup.completed_at`.

## Migration Plan

1. Add `setup.completed_at` support to `internal/usersettings` while preserving existing `onboarding.completed_at` and `onboarding.dismissed`.
2. Extract profile-writing logic from `cmd/agent-runner/internal_cmd.go` into a reusable internal package; keep the internal command as a wrapper.
3. Add Go services for adapter detection, model discovery, target path resolution, and collision detection.
4. Implement `internal/onboarding/native` with a Bubble Tea model and tests for completion, cancellation, no-skip behavior, collision confirmation, and write failure.
5. Replace `ensureOnboardingForTUI` with first-run orchestration that runs mandatory setup first and `onboarding:onboarding` second.
6. Replace onboarding builtin workflow files: add `onboarding.yaml`, keep `step-types-demo.yaml`, remove/stop exposing `welcome.yaml` and `setup-agent-profile.yaml`.
7. Update tests and docs that reference old workflow names.

Rollback is straightforward before release: restore `welcome.yaml` / `setup-agent-profile.yaml`, restore `ensureOnboardingForTUI` to launch `onboarding:welcome`, and leave the unused `setup.completed_at` setting ignored. Since this is pre-release, no compatibility migration is required beyond preserving unknown settings keys.

## Open Questions

- Exact native setup screen layout and copy are left to implementation, constrained by the requirements: setup is mandatory, cannot be skipped, and cancellation/interruption writes no setup state.
