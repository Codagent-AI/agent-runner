## Why

The current first-run experience embeds welcome, setup, dismissal, and completion behavior inside `onboarding:welcome`, which makes the UI feel like a live workflow run instead of a native application flow. Moving setup into the TUI creates a cleaner first-run path while preserving workflow-based onboarding demos for the parts that benefit from showing real workflow steps.

## What Changes

- Add a mandatory native first-run setup flow that runs before onboarding demos when setup has not been completed.
- Move the setup UI and setup completion tracking out of `workflows/onboarding/welcome.yaml` and into Agent Runner TUI functionality.
- Track native setup state separately from onboarding demo completion so setup can gate first-run behavior without depending on workflow run state.
- Keep an `onboarding` builtin workflow after setup completes, but remove the welcome/setup/dismissal shell and have it focus on the step-types demo sub-workflow.
- Rename or reshape the remaining onboarding workflow YAML so it represents the demo workflow sequence rather than the full first-run setup flow.
- Preserve the existing setup semantics: choose adapters and models, choose global or project config scope, confirm overwrites, write profile config atomically, support cancellation/interruption without marking setup complete.
- Keep onboarding demo skip/defer/dismiss controls in the onboarding workflow, under demo-oriented copy rather than the old setup welcome framing.
- Remove workflow-specific resume/dismissal assumptions for the old setup path; interrupted native setup should return the user to the native setup flow on the next eligible TUI entry.
- **BREAKING**: `agent-runner run onboarding:welcome` will no longer represent the full welcome/setup/dismissal flow. The first-run setup surface becomes native TUI behavior, and workflow invocation remains for onboarding demos.

## Capabilities

### New Capabilities

- `native-setup`: Mandatory native first-run setup flow, including setup presentation, completion tracking, and handoff into the normal app/onboarding-demo experience.

### Modified Capabilities

- `onboarding-workflow`: Change onboarding from the owner of first-run setup into a workflow-based demo that runs after native setup, owns demo defer/dismiss behavior, and can continue to host current and future onboarding sub-workflows.
- `agent-profile-editor`: Change the setup entry point from a workflow sub-flow to native TUI functionality while preserving the profile shape, adapter/model selection, scope choice, overwrite confirmation, and atomic write requirements.
- `builtin-workflows`: Update the embedded onboarding namespace to remove or rename the old `welcome`/`setup-agent-profile` workflow contract and keep the demo-oriented onboarding workflow assets.
- `user-settings-file`: Add or adjust settings keys for native setup tracking, distinct from demo workflow state, using the existing atomic settings write path.
- `workflow-bundled-scripts`: Remove onboarding setup script assumptions now that setup adapter/model discovery and profile writing move into native Go code.

## Impact

- Affected code: CLI startup dispatch in `cmd/agent-runner`, native TUI packages for first-run/setup screens, settings helpers in `internal/usersettings`, embedded workflow resolution in `workflows/`, and tests around onboarding dispatch.
- Affected workflows: `workflows/onboarding/welcome.yaml` is removed, renamed, or reduced to demo orchestration; `workflows/onboarding/setup-agent-profile.yaml` no longer owns native setup; `workflows/onboarding/step-types-demo.yaml` remains available and may become the primary onboarding workflow entry.
- Affected specs: existing requirements in `onboarding-workflow`, `agent-profile-editor`, `builtin-workflows`, and `user-settings-file` need deltas because they currently describe setup as workflow-driven.
- No new external dependencies are expected; the implementation should reuse existing TUI, settings, adapter detection/model discovery, and profile writing infrastructure where practical.
