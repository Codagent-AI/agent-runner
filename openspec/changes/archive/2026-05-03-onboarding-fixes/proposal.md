## Why

User testing of the just-shipped onboarding feature surfaced bugs and UX gaps spanning the welcome flow, the `mode: ui` renderer, the dispatcher's interaction with run state, and visual styling. This change batches them into a single fix pass so onboarding can ship reliably.

This change depends on the `onboarding` branch landing on `dev` first, since the deltas modify capabilities (`onboarding-workflow`, `ui-step`) introduced by that change.

## What Changes

### Bug fixes
- **Dispatcher restarts onboarding from scratch** after a Ctrl-C mid-flow instead of resuming the incomplete run. Verify whether `agent-runner -resume` is also broken for *other* workflows; if it is, fix the general regression in scope.
- **`not_now` writes `onboarding.completed_at`**, so the dispatcher correctly thinks onboarding is done and never fires again. The `skip_if` on `set-completed` should already gate this — needs tracing.
- **Dispatcher fires on every startup** despite `completed_at` being written. Likely the same root cause as above (wrong key, wrong format, or a read/write mismatch).
- **`set-dismissed` errors with `workflow "internal" not found`**. The step uses `command: agent-runner internal write-setting …` which should run as a shell step; either the runner is misrouting it or the embedded `internal` subcommand isn't wired.
- **`q` on the run view goes to the home screen** instead of quitting. Both `live-run-view` and `view-run` specs already say `q` exits the program — this is impl drift.
- **`not_now` / `dismiss` leave the user on the run-view** instead of returning to the home screen.
- **Up/down on the adapter selector loses focus**, so Enter no longer fires.
- **`mode: ui` body text gets cut off** instead of wrapping.

### UX / rendering
- **`mode: ui` renders standalone**, not inside the live-run-view chrome. Mockups required workflow name + breadcrumbs at the top and the step list in the sidebar.
- **Adapter screen splits the input and the Continue action across two screens**, instead of rendering them together as one form.
- **Action buttons read as a list of items**; they should look like buttons (focus-aware, padded, themed via `lipgloss`).
- **Apply theme colors** (`internal/tuistyle` palette) to UI step titles and field labels for visual consistency with the rest of the TUI.
- **Add glyphs** for the two new step types in the run-view step list:
  - `script:` → glyph in `tuistyle.InactiveAmber` (same color as shell — they are similar primitives).
  - `mode: ui` → distinct glyph in a different theme color.

### Spec additions
- `onboarding-workflow`: dispatcher SHALL resume an incomplete prior onboarding run instead of starting fresh.
- `onboarding-workflow`: when a dispatcher-launched onboarding run ends (any outcome), the runner SHALL proceed to its normal entry point (the home screen / listview).
- `ui-step`: when a UI step has both `inputs` and `actions`, they SHALL render together on a single screen with focus traversable between them.
- `live-run-view`: when the active step is `mode: ui`, the live-run-view SHALL render the UI step inside its chrome (sidebar with step list, breadcrumb header, status panel).

### Consistency
- Align `kind: single_select` between the spec, the loader, and the bundled YAML files. Today the model accepts both `single_select` and `single-select`, the spec only documents the underscore form, and the bundled `setup-agent-profile.yaml` uses the hyphen form. Pick the underscore form everywhere; reject the hyphen form in validation; update bundled YAML.

## Capabilities

### Modified Capabilities
- `onboarding-workflow`: add dispatcher-resume and post-exit navigation requirements.
- `ui-step`: add form-layout requirement (inputs + actions on one screen).
- `live-run-view`: add UI-step-inside-chrome rendering requirement.

## Out of Scope

- Generalised form/input primitives beyond what onboarding needs (e.g., multi-select, free-text, validated text fields).
- New step types beyond `script:` and `mode: ui`.
- Onboarding content/copy changes (welcome wording, body text, etc.) — only structural and rendering issues are in scope here.
- Adding a `huh` dependency. The button-look improvement uses `lipgloss` styling on top of the existing `bubbles` widgets already in `go.mod`.

## Impact

- **Workflows**: `workflows/onboarding/welcome.yaml`, `workflows/onboarding/setup-agent-profile.yaml`.
- **CLI**: `cmd/agent-runner/internal_cmd.go`, `cmd/agent-runner/main.go` (dispatcher entry, post-onboarding handoff to listview).
- **UI / runtime**: `internal/exec/ui.go` (renderer), `internal/runview/view.go` (glyphs, chrome integration), `internal/liverun/` (chrome wraps `mode: ui`), `internal/tuistyle/` (palette usage).
- **Loader / model**: `internal/model/step.go` (tighten `kind` validation), `internal/loader/`.
- **Settings**: `internal/usersettings/settings.go` (verify the `onboarding.completed_at` / `onboarding.dismissed` read path matches `internal write-setting`).
- **Specs**: three capability deltas; no new capabilities.
