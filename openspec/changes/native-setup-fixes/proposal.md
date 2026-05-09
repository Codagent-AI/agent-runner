## Why

The native setup feature shipped with several UX issues (no explanatory copy, ugly layout, unnecessary screens), bugs (codex model discovery broken, setup.completed_at not persisting), and an over-engineered profile shape (four agents when only two are needed). The onboarding demo prompt also belongs inside the native setup flow rather than as a separate workflow step.

## What Changes

- Remove the welcome/intro screen — native setup starts directly at CLI selection
- Remove the confirmation screen — proceed from scope to write (or collision check)
- Remove `interactive_base` and `headless_base` agents everywhere — native setup writes only `planner` and `implementor` with direct `default_mode`/`cli`/`model` fields (no `extends`)
- Move the onboarding demo prompt (Continue / Not now / Dismiss) into native setup as a post-write screen
- Add a startup trigger to re-show the demo prompt when setup is complete but onboarding is neither completed nor dismissed
- Fix codex model discovery: `parseCodexModels` does not handle the `{"models": [...]}` wrapper format returned by `codex debug models`
- Fix setup.completed_at persistence: the setting is not persisted after completing the full setup flow
- UX overhaul: welcoming explanatory copy on every screen, centered layout (with top-left fallback for small terminals), smooth scroll-up transitions using harmonica

## Capabilities

### Modified Capabilities
- `native-setup`: Remove welcome and confirm screens, integrate demo prompt, add re-show trigger, fix persistence bug
- `agent-profile-editor`: Remove _base agents, write planner/implementor directly, fix codex model discovery
- `onboarding-workflow`: Remove intro and set-dismissed steps from workflow; demo prompt ownership moves to native setup

### New Capabilities
- `native-setup-ux`: Centered layout with animation, explanatory copy on each screen

## Out of Scope

- Back/previous navigation within the wizard
- Keyboard-accessible re-run of the editor outside native setup
- Changes to the step-types-demo content or instructional flow
- Migration tooling for existing user configs that reference `interactive_base`/`headless_base`

## Impact

- `internal/onboarding/native/native.go` — major rewrite of stages, view, and write logic
- `internal/onboarding/native/native_test.go` — update all tests
- `internal/profilewrite/profilewrite.go` — write 2 agents instead of 4
- `internal/profilewrite/profilewrite_test.go` — update expected output
- `internal/config/config.go` — remove _base from built-in defaults
- `internal/config/config_test.go` — update all _base references
- `internal/usersettings/` — investigate/fix persistence bug
- `cmd/agent-runner/main.go` — add demo-prompt re-show trigger in `ensureFirstRunForTUI`
- `cmd/agent-runner/internal_cmd_test.go` — update profile shape assertions
- `workflows/onboarding/onboarding.yaml` — remove intro and set-dismissed steps
- `openspec/specs/native-setup/spec.md` — update requirements
- `openspec/specs/agent-profile-editor/spec.md` — update four-agent → two-agent
- `openspec/specs/onboarding-workflow/spec.md` — remove intro ownership
- `openspec/specs/agent-profiles/spec.md` — update _base references
- `openspec/specs/config-profiles/spec.md` — update _base scenario names
- `testdata/valid-workflow.yaml` — update agent reference
- `.agent-runner/workflows/smoke-test-*.yaml` — update agent references
