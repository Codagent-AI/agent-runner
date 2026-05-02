# Task: Onboarding End-to-End

## Goal

Ship the complete onboarding experience: the `agent-runner internal write-profile` subcommand, the embedded onboarding workflow YAML files, the bundled shell scripts, and the first-run dispatcher that triggers onboarding on fresh installs.

## Background

You MUST read these files before starting:
- `openspec/changes/onboarding/design.md` — full design (particularly "Onboarding Workflow Structure", "Internal write-profile Subcommand", "First-Run Dispatcher", and "UI Mockups" sections)
- `openspec/changes/onboarding/specs/onboarding-workflow/spec.md` — workflow lifecycle requirements
- `openspec/changes/onboarding/specs/agent-profile-editor/spec.md` — editor flow requirements
- `openspec/changes/onboarding/specs/builtin-workflows/spec.md` — embedded namespace requirements

This task assumes that `mode: ui` steps, `script:` steps, typed captures, interpolation extension, settings refactor (with onboarding keys), and bundled asset materialization are all already implemented and working. You are building ON those primitives.

### Internal write-profile Subcommand

Add `agent-runner internal write-profile` as a hidden but tested subcommand. Intercept `args[0] == "internal"` in the `run()` function of `cmd/agent-runner/main.go` before flag parsing, dispatching to a `handleInternal(args[1:])` function.

The `write-profile` subcommand:
- Reads JSON from stdin: `{"interactive_cli": "...", "interactive_model": "...", "headless_cli": "...", "headless_model": "...", "target_path": "..."}`
- Model fields may be empty string (when the CLI doesn't support model selection)
- Loads the existing file (if any) at `target_path` as a `yaml.Node`
- Merges four agents into `profiles.default.agents`:
  - `interactive_base`: `{default_mode: interactive, cli: <chosen>, model: <chosen>}` (omit model key if empty)
  - `headless_base`: `{default_mode: headless, cli: <chosen>, model: <chosen>}` (omit model key if empty)
  - `planner`: `{extends: interactive_base}`
  - `implementor`: `{extends: headless_base}`
- Preserves all other agents, profile sets, and top-level keys in the file
- Writes atomically (temp file + rename in same directory)
- File mode 0o600; creates missing parent directories with mode 0o755
- Exits 0 on success, non-zero with stderr message on failure

Place the implementation in a new file like `cmd/agent-runner/internal_cmd.go`. Test it like a real command with both unit tests (JSON parsing, yaml merge logic) and integration-style tests (round-trip: write then read back and verify structure).

### Onboarding Workflow YAML

Create `workflows/onboarding/` directory with:

**welcome.yaml** — the top-level workflow invoked by the dispatcher or by `agent-runner run onboarding:welcome`:
1. `welcome` — UI step: informational body explaining what onboarding does, with 3 actions: `{label: "Continue", outcome: continue}`, `{label: "Not now", outcome: not_now}`, `{label: "Dismiss", outcome: dismiss}`. Uses `outcome_capture: user_action`.
2. `set-dismissed` — shell step: writes `settings.onboarding.dismissed` timestamp. Use `skip_if: 'sh: [ "{{user_action}}" != "dismiss" ]'`. Command invokes the settings write (e.g., `agent-runner internal write-setting onboarding.dismissed $(date -u +%Y-%m-%dT%H:%M:%SZ)` — or use a simpler approach if an internal subcommand for settings already exists; otherwise a direct shell write to the settings file following the atomic pattern).
3. `setup` — sub-workflow step: `workflow: setup-agent-profile.yaml`. Use `skip_if: 'sh: [ "{{user_action}}" != "continue" ]'`.
4. `set-completed` — shell step: writes `settings.onboarding.completed_at` timestamp. Same skip pattern — only runs after successful setup return. Use `skip_if: 'sh: [ "{{user_action}}" != "continue" ]'`.

**setup-agent-profile.yaml** — the Phase 2 sub-workflow:
1. `detect-adapters` — script step: `script: detect-adapters.sh`, `capture: detected_adapters`, `capture_format: json`
2. `pick-interactive-cli` — UI step: body explains interactive agent, single-select input with `options: {{detected_adapters}}`, `capture: interactive_choices`
3. `models-interactive` — script step: `script: models-for-cli.sh`, `script_inputs: {adapter: "{{interactive_choices.cli}}"}`, `capture: interactive_models`, `capture_format: json`
4. `count-interactive-models` — shell step: `command: echo {{interactive_models}} | jq length`, `capture: interactive_model_count`. (The `script_inputs` JSON-encodes the list for the script; this shell step uses a helper to get the count as a string for skip_if.)
5. `pick-interactive-model` — UI step: body shows chosen CLI, single-select input with `options: {{interactive_models}}`, `capture: interactive_model_choice`. Use `skip_if: 'sh: [ "{{interactive_model_count}}" = "0" ]'`
6. `detect-adapters-hl` — script step: same as step 1 (re-detect for headless selection)
7. `pick-headless-cli` — UI step: body explains headless agent, single-select input with `options: {{detected_adapters}}`, `capture: headless_choices`
8. `models-headless` — script step: same pattern as step 3 for headless CLI
9. `count-headless-models` — shell step: same count pattern as step 4
10. `pick-headless-model` — UI step: same pattern as step 5 for headless model. Same skip logic using headless model count.
11. `pick-scope` — UI step: body explains scope, single-select with options `[global, project]`, `capture: scope_choice`
12. `check-collisions` — script step: `script: check-collisions.sh`, `script_inputs` with target path, `capture: collisions`, `capture_format: json`. Outputs JSON array of colliding agent names (empty array if none).
13. `count-collisions` — shell step: count collisions list length, `capture: collision_count`
14. `confirm-overwrite` — UI step: body shows colliding entries, actions: `{label: "Overwrite", outcome: overwrite}`, `{label: "Cancel", outcome: cancel}`, `outcome_capture: overwrite_action`. Use `skip_if: 'sh: [ "{{collision_count}}" = "0" ]'`
15. `confirm` — UI step: body shows all chosen values (interpolated from captures using `{{var.field}}`), actions: `{label: "Confirm", outcome: confirm}`, `{label: "Cancel", outcome: cancel}`, `outcome_capture: confirm_action`. Use `skip_if: 'sh: [ "{{overwrite_action}}" = "cancel" ]'` (skip if user already cancelled at overwrite screen).
16. `write-profile` — script step: `script: write-profile.sh`, `script_inputs` with all chosen values + resolved target path. Use `skip_if: 'sh: [ "{{confirm_action}}" != "confirm" ]'`.

Note: The skip_if conditions use string captures from shell count steps to avoid interpolating list-typed captures in string contexts (which would be a type error per the typed capture rules).

### Bundled Scripts

Create in `workflows/onboarding/`:

**detect-adapters.sh**:
- Check `$PATH` for known CLI binaries: `claude`, `codex`, `copilot`, `cursor`, `opencode`
- Output a JSON array of found adapter names to stdout (e.g., `["claude","codex"]`)
- Exit 0 always (empty array if nothing found — the UI step will fail gracefully with "no adapters")

**models-for-cli.sh**:
- Reads JSON from stdin to get the adapter name
- Queries the CLI for available models. Strategy per adapter:
  - `claude`: try `claude --version` or a known subcommand that lists models (research what's available)
  - For adapters without a list-models command: output `[]`
- Output a JSON array of model name strings to stdout
- Exit 0 always (empty array means model selection will be skipped)

**check-collisions.sh**:
- Reads JSON from stdin to get the target config path
- If the file exists, parses it and checks for any of `interactive_base`, `headless_base`, `planner`, `implementor` under `profiles.default.agents`
- Outputs a JSON array of colliding agent names (e.g., `["planner"]` or `[]` if no collisions)
- Exit 0 always

**write-profile.sh**:
- Reads script_inputs from stdin (JSON with all profile values + target path)
- Constructs the JSON payload expected by `agent-runner internal write-profile`
- Pipes it to `agent-runner internal write-profile`
- Exits with the subcommand's exit code

### First-Run Dispatcher

Add `ensureOnboardingForTUI` function in `cmd/agent-runner/main.go`:
- Called after `ensureThemeForTUI` in `handleListBare` and `handleList` only (NOT in handleInspect or handleResume)
- Loads settings via `usersettings.Load()`
- Checks: `settings.Onboarding.CompletedAt == "" && settings.Onboarding.Dismissed == ""`
- Also checks both stdin and stdout are TTYs (use `term.IsTerminal` or equivalent)
- When all conditions met: launches `onboarding:welcome` via the standard handleRun path using `builtinworkflows.Resolve("onboarding:welcome")`
- Returns exit code from the workflow run

Follow the `ensureThemeForTUI` pattern for testability: use a deps struct so the dispatcher can be unit-tested without actually launching workflows.

### Key Constraints

- The dispatcher SHALL NOT fire on `--inspect`, `--resume`, `-version`, `-validate`, or explicit `agent-runner run <workflow>` invocations
- The `not_now` action exits without modifying settings (dispatcher fires again next time)
- The `dismiss` action writes `settings.onboarding.dismissed` timestamp (dispatcher never fires again)
- Successful completion writes `settings.onboarding.completed_at` timestamp
- Cancellation or failure inside setup leaves settings unchanged
- Re-entry is always available via `agent-runner run onboarding:welcome` regardless of settings state
- The onboarding workflow runs through the same loader/runner/audit pipeline as any other workflow
- No bespoke onboarding state files — use standard run state

## Spec

### Requirement: First-run dispatcher trigger condition

Before entering any interactive TUI (listview, live-run, run-view, resume-TUI), the runner SHALL evaluate the dispatcher condition. The condition SHALL fire (offer onboarding) when ALL of the following hold:
- `settings.onboarding.completed_at` is unset;
- `settings.onboarding.dismissed` is unset;
- both stdin and stdout are TTYs.

When any condition is false, the runner SHALL proceed to its normal entry point without modifying settings.

#### Scenario: Fresh first run with TTY fires
- **WHEN** the user runs `agent-runner` with no command on a TTY, and `settings.onboarding.completed_at` and `settings.onboarding.dismissed` are both unset
- **THEN** the dispatcher fires and launches `onboarding:welcome`

#### Scenario: Already completed does not fire
- **WHEN** the user runs `agent-runner` with `settings.onboarding.completed_at` set
- **THEN** the dispatcher does not fire and the runner proceeds to its normal entry point

#### Scenario: Already dismissed does not fire
- **WHEN** the user runs `agent-runner` with `settings.onboarding.dismissed` set
- **THEN** the dispatcher does not fire

#### Scenario: Non-TTY does not fire
- **WHEN** the user runs `agent-runner` from a CI job where stdout is a pipe, and onboarding settings are unset
- **THEN** the dispatcher does not fire and SHALL NOT modify settings

#### Scenario: Non-TUI command does not fire
- **WHEN** the user runs `agent-runner -version` or `agent-runner run my-workflow`
- **THEN** the dispatcher does not fire even when conditions would otherwise be satisfied

### Requirement: Dispatcher launches via normal workflow path

When the dispatcher condition fires, the runner SHALL launch the embedded `onboarding:welcome` workflow using the standard workflow loader, runner, state, and audit machinery.

#### Scenario: Dispatcher uses standard machinery
- **WHEN** the dispatcher fires
- **THEN** `onboarding:welcome` runs through the same loader / runner / audit pipeline as any other invocation, and a normal run record is produced

### Requirement: Welcome screen actions

The first step of `onboarding:welcome` SHALL be a `mode: ui` informational screen with exactly three actions: `continue`, `not_now`, and `dismiss`.

#### Scenario: Welcome offers three actions
- **WHEN** the welcome screen renders
- **THEN** the user sees actions labelled to indicate continue, not-now, and dismiss; the underlying outcomes are exactly `continue`, `not_now`, `dismiss`

### Requirement: Dismiss action records dismissal and exits

When the user selects the `dismiss` action, the workflow SHALL set `settings.onboarding.dismissed` to the current RFC3339 timestamp. After the write, the workflow SHALL exit successfully without proceeding to setup.

#### Scenario: Dismiss writes timestamp
- **WHEN** the user selects `dismiss` on the welcome screen
- **THEN** `~/.agent-runner/settings.yaml` is written atomically with `onboarding.dismissed: <RFC3339 timestamp>` and the workflow exits successfully

#### Scenario: Dismiss does not run setup
- **WHEN** the user selects `dismiss`
- **THEN** the setup sub-workflow does not run

### Requirement: Not-now action exits without modifying settings

When the user selects the `not_now` action, the workflow SHALL exit successfully without writing to settings or running setup.

#### Scenario: Not-now leaves settings unchanged
- **WHEN** the user selects `not_now`
- **THEN** no settings keys are written and the workflow exits successfully

#### Scenario: Not-now does not suppress future prompts
- **WHEN** the user selects `not_now` and re-runs `agent-runner` later on a TTY
- **THEN** the dispatcher fires again

### Requirement: Continue action invokes setup

When the user selects the `continue` action, the workflow SHALL invoke `setup-agent-profile.yaml` as a sub-workflow.

#### Scenario: Continue runs setup
- **WHEN** the user selects `continue`
- **THEN** `setup-agent-profile.yaml` runs as a sub-workflow

### Requirement: Successful completion records completed_at

On successful return from the setup sub-workflow, `onboarding:welcome` SHALL set `settings.onboarding.completed_at` to the current RFC3339 timestamp.

#### Scenario: Successful setup writes completed_at
- **WHEN** the user runs through welcome → continue → setup, completes the editor write successfully
- **THEN** `settings.onboarding.completed_at` is written and the workflow exits successfully

### Requirement: Cancellation or failure leaves settings unchanged

When the setup sub-workflow fails or the user cancels, `onboarding:welcome` SHALL NOT modify settings. The dispatcher SHALL fire again on the next TUI entry.

#### Scenario: Cancel at confirmation does not record completion
- **WHEN** the user reaches the editor's confirmation screen and selects cancel
- **THEN** `settings.onboarding.completed_at` is not written and `settings.onboarding.dismissed` is not written

### Requirement: Re-entry by direct invocation

The user MAY re-run onboarding at any time via `agent-runner run onboarding:welcome`. The workflow SHALL execute regardless of settings state.

#### Scenario: Run after completion
- **WHEN** the user runs `agent-runner run onboarding:welcome` with `settings.onboarding.completed_at` already set
- **THEN** the workflow executes normally

### Requirement: Editor produces a fixed four-agent shape

The editor SHALL write exactly four agent entries under `profiles.default.agents`: `interactive_base`, `headless_base`, `planner`, `implementor`.

#### Scenario: Successful first-time write
- **WHEN** the user picks `cli: claude, model: opus` for interactive_base and `cli: codex, model: gpt-5` for headless_base, scope `global`, and confirms the write
- **THEN** `~/.agent-runner/config.yaml` contains exactly the four entries with the correct values

#### Scenario: Editor does not write summarizer
- **WHEN** the editor completes a successful write
- **THEN** the resulting `profiles.default.agents` map contains no `summarizer` entry from the editor

### Requirement: User chooses CLI and model for each base agent

The editor SHALL prompt the user to choose a CLI adapter and a model for `interactive_base`, and separately for `headless_base`. Model options SHALL be discovered at runtime by querying the chosen CLI adapter. When the CLI does not support model listing, the model selection step SHALL be skipped.

#### Scenario: CLI options reflect detected adapters
- **WHEN** the host has `claude` and `codex` on `$PATH` but not `cursor`
- **THEN** the CLI selection screens present `claude` and `codex` as the only options

#### Scenario: Model discovery returns empty
- **WHEN** the user picks a CLI adapter whose model discovery returns an empty list
- **THEN** the model selection step is skipped and the model field is written as empty

### Requirement: User chooses scope

The editor SHALL prompt the user to choose `global` or `project`. `global` writes to `~/.agent-runner/config.yaml`. `project` writes to `.agent-runner/config.yaml` relative to cwd.

#### Scenario: Global scope writes to home
- **WHEN** the user picks scope `global`
- **THEN** the write target is `~/.agent-runner/config.yaml`

#### Scenario: Project scope writes to cwd
- **WHEN** the user picks scope `project` and the runner was invoked from `/path/to/project`
- **THEN** the write target is `/path/to/project/.agent-runner/config.yaml`

### Requirement: Confirmation before write

The editor SHALL present a confirmation screen showing the chosen values before writing. The user MAY cancel.

#### Scenario: Cancel from confirmation
- **WHEN** the user reviews the confirmation screen and selects the cancel action
- **THEN** no file is created or modified and the workflow handles the cancellation per its existing failure-handling rules

### Requirement: Write goes through internal Go subcommand

The bundled writer script SHALL invoke `agent-runner internal write-profile`. The bundled script SHALL NOT emit YAML directly.

#### Scenario: Existing other agents preserved
- **WHEN** the chosen config file already contains an agent `team_implementor` and the editor writes the four-agent shape
- **THEN** the resulting file contains the four editor-written entries and still contains `team_implementor` unchanged

#### Scenario: Atomic write
- **WHEN** the write subcommand is interrupted or fails part-way through writing
- **THEN** the original file (if any) remains intact and no partial file is left at the target path

#### Scenario: File mode and parent directory creation
- **WHEN** the write target is `~/.agent-runner/config.yaml` and `~/.agent-runner/` does not exist
- **THEN** the directory is created with mode `0o755` and the file is written with mode `0o600`

### Requirement: Onboarding namespace embedded

The builtin set SHALL include an `onboarding` namespace. The `onboarding` namespace SHALL contain at minimum `welcome` and `setup-agent-profile` workflows.

#### Scenario: Onboarding workflows invoked by namespace
- **WHEN** the user runs `agent-runner run onboarding:welcome`
- **THEN** the workflow loads from the embedded `onboarding` namespace and executes

#### Scenario: Setup sub-workflow exists
- **WHEN** the embedded `onboarding:welcome` references `workflow: setup-agent-profile.yaml`
- **THEN** the sub-workflow loads from the embedded `onboarding/setup-agent-profile.yaml`

### Requirement: Non-YAML files embedded as bundled assets

Files in a namespace subdirectory whose names do not end in `.yaml` SHALL be embedded as bundled assets accessible at runtime.

#### Scenario: Embedded script asset accessible
- **WHEN** `onboarding:setup-agent-profile` declares `script: detect-adapters.sh` and the file `onboarding/detect-adapters.sh` exists in the embedded set
- **THEN** the runner resolves and executes that bundled asset at runtime

## Done When

- `agent-runner internal write-profile` works correctly (unit + integration tests pass)
- `agent-runner run onboarding:welcome` launches and runs through all phases in TUI mode
- First-run dispatcher fires correctly on bare invocation with empty settings
- Dispatcher does not fire after completion, after dismissal, on non-TTY, or on explicit commands
- All welcome-screen actions (continue, not_now, dismiss) produce correct settings writes
- The setup sub-workflow detects adapters, presents choices, and writes a valid config file
- `make test` passes; `make lint` passes
- The bundled scripts execute correctly on macOS (primary development platform)
