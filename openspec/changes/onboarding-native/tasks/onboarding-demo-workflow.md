# Task: Onboarding Demo Workflow

## Goal

Replace the old workflow-owned setup entry with the optional onboarding demo workflow. This task delivers `onboarding:onboarding`, preserves `onboarding:step-types-demo`, removes the old `welcome` and `setup-agent-profile` builtin contract, and updates tests/docs/references.

## Background

You MUST read these files before starting:
- `openspec/changes/onboarding-native/proposal.md` sections `What Changes`, `Capabilities`, and `Impact`
- `openspec/changes/onboarding-native/design.md` sections `Approach`, `Decisions`, and `Migration Plan`
- `openspec/changes/onboarding-native/specs/onboarding-workflow/spec.md` all requirements
- `openspec/changes/onboarding-native/specs/builtin-workflows/spec.md` all requirements
- `openspec/changes/onboarding-native/specs/workflow-bundled-scripts/spec.md` requirement `Embedded vs on-disk script resolution`
- `openspec/changes/onboarding-native/specs/user-settings-file/spec.md` requirement `Onboarding demo settings`
- `workflows/onboarding/welcome.yaml` for old demo/setup orchestration to replace
- `workflows/onboarding/setup-agent-profile.yaml` for old setup workflow references to remove
- `workflows/onboarding/step-types-demo.yaml` for the remaining demo sub-workflow
- `workflows/embed.go` builtin resolution behavior
- `workflows/embed_test.go` onboarding namespace tests
- `internal/loader/onboarding_test.go` embedded onboarding load tests
- `workflows/onboarding_step_types_demo_test.go` current onboarding workflow assertions
- `workflows/onboarding/docs/agent-runner-basics.md` user-facing old workflow name references

The design keeps the demo as a real workflow example while moving mandatory setup to native TUI. Add `workflows/onboarding/onboarding.yaml` as the top-level demo workflow. Its first step is a UI intro for the optional workflow demo, not setup welcome copy. The intro outcomes are `continue`, `not_now`, and `dismiss`: continue runs `step-types-demo.yaml`, not-now writes nothing, dismiss writes `settings.onboarding.dismissed`, and successful continue writes `settings.onboarding.completed_at`.

The old builtin entry points `onboarding:welcome` and `onboarding:setup-agent-profile` must no longer be required or exposed by tests/docs. Keep `onboarding:step-types-demo` directly runnable. Remove shell scripts/assets that are no longer referenced by remaining workflows, unless another builtin workflow still needs them.

## Spec

### Requirement: Onboarding demo intro actions

The first step of `onboarding:onboarding` SHALL be a `mode: ui` informational screen that introduces the onboarding demo and offers exactly three outcomes: continue to the demo, defer the demo until later, and dismiss the demo. The step SHALL NOT be named or presented as the setup welcome screen.

#### Scenario: Intro offers three actions
- **WHEN** `onboarding:onboarding` starts
- **THEN** the first step offers actions whose outcomes are exactly `continue`, `not_now`, and `dismiss`

#### Scenario: Intro is not setup welcome
- **WHEN** the onboarding demo intro renders
- **THEN** the copy describes the optional workflow demo rather than mandatory setup

#### Scenario: Not-now leaves demo eligible
- **WHEN** the user selects the not-now action
- **THEN** no onboarding completion or dismissal setting is written

#### Scenario: Dismiss records demo dismissal
- **WHEN** the user selects the dismiss action
- **THEN** `settings.onboarding.dismissed` is written with the current RFC3339 timestamp

### Requirement: Embedded onboarding namespace contents

The `onboarding` builtin workflow namespace SHALL contain at minimum:
- `onboarding` - the top-level demo workflow started after successful native setup and by direct invocation;
- `step-types-demo` - the workflow used to demonstrate UI, interactive agent, headless agent, shell, and capture behavior;
- the packaged documentation needed by the Q&A agent.

The onboarding namespace SHALL NOT own first-run setup or setup completion tracking. It SHALL own onboarding demo defer and dismissal behavior.

#### Scenario: Onboarding demo workflow resolves
- **WHEN** the user runs `agent-runner run onboarding:onboarding`
- **THEN** the workflow loads from the embedded namespace and starts executing

#### Scenario: Step types demo workflow resolves
- **WHEN** the user runs `agent-runner run onboarding:step-types-demo`
- **THEN** the workflow loads from the embedded namespace and starts executing

#### Scenario: Welcome workflow is not the demo entry
- **WHEN** the user runs `agent-runner run onboarding:welcome`
- **THEN** the runner fails with a workflow-not-found error

#### Scenario: Setup workflow is not an onboarding workflow
- **WHEN** the user runs `agent-runner run onboarding:setup-agent-profile`
- **THEN** the runner fails with a workflow-not-found error

### Requirement: First-run dispatcher trigger condition

Before entering any interactive TUI, the runner SHALL evaluate native setup before onboarding demo dispatch. The onboarding demo dispatcher SHALL fire when all of the following hold:
- `settings.setup.completed_at` is set;
- `settings.onboarding.completed_at` is unset;
- `settings.onboarding.dismissed` is unset;
- both stdin and stdout are TTYs.

When any condition is false, the runner SHALL proceed to its normal entry point without modifying onboarding settings.

#### Scenario: Setup runs before onboarding demo
- **WHEN** setup settings and onboarding settings are unset
- **THEN** the runner opens native setup before any onboarding demo workflow

#### Scenario: Completed setup starts demo
- **WHEN** `settings.setup.completed_at` is set and `settings.onboarding.completed_at` and `settings.onboarding.dismissed` are unset
- **THEN** the dispatcher launches `onboarding:onboarding`

#### Scenario: Completed onboarding demo does not fire
- **WHEN** `settings.onboarding.completed_at` is set
- **THEN** the onboarding demo dispatcher does not fire and the runner proceeds to its normal entry point

#### Scenario: Dismissed onboarding demo does not fire
- **WHEN** `settings.onboarding.dismissed` is set
- **THEN** the onboarding demo dispatcher does not fire

#### Scenario: Non-TTY does not fire
- **WHEN** the runner starts with stdin or stdout connected to a pipe
- **THEN** the onboarding demo dispatcher does not fire and SHALL NOT modify settings

#### Scenario: Non-TUI command does not fire
- **WHEN** the user runs `agent-runner -version` or `agent-runner run my-workflow`
- **THEN** the onboarding demo dispatcher does not fire even when conditions would otherwise be satisfied

### Requirement: Continue action invokes setup

The onboarding demo workflow SHALL NOT invoke setup. Setup is native TUI functionality that runs before the onboarding demo. When the user selects the continue action in `onboarding:onboarding`, the workflow SHALL invoke the `step-types-demo` workflow or otherwise run the step-types demo sequence.

#### Scenario: Demo skips setup
- **WHEN** the user selects continue in `onboarding:onboarding`
- **THEN** it does not invoke `setup-agent-profile.yaml`

#### Scenario: Demo runs step types
- **WHEN** the user selects continue in `onboarding:onboarding`
- **THEN** it runs the step-types demo workflow sequence

### Requirement: Successful completion records `completed_at`

When the continue path of `onboarding:onboarding` completes successfully, the runner SHALL set `settings.onboarding.completed_at` to the current RFC3339 timestamp via the existing user settings atomic-write path. Successful onboarding demo completion SHALL NOT write setup completion settings.

#### Scenario: Demo completion records onboarding completion
- **WHEN** `onboarding:onboarding` completes its continue path successfully
- **THEN** `settings.onboarding.completed_at` is written

#### Scenario: Demo completion does not write setup completion
- **WHEN** `onboarding:onboarding` completes successfully
- **THEN** `settings.setup.completed_at` is not modified

### Requirement: Re-entry by direct invocation

The user MAY re-run the onboarding demo at any time via `agent-runner run onboarding:onboarding`. The workflow SHALL execute regardless of the current state of `settings.onboarding.completed_at`, `settings.onboarding.dismissed`, or `settings.setup.completed_at`. Direct invocation of `onboarding:onboarding` SHALL use the standard direct-run post-run behavior.

#### Scenario: Run after demo completion
- **WHEN** the user runs `agent-runner run onboarding:onboarding` with `settings.onboarding.completed_at` already set
- **THEN** the workflow executes normally

#### Scenario: Run after demo dismissal
- **WHEN** the user runs `agent-runner run onboarding:onboarding` with `settings.onboarding.dismissed` already set
- **THEN** the workflow executes normally

#### Scenario: Run without setup completion
- **WHEN** the user runs `agent-runner run onboarding:onboarding` with `settings.setup.completed_at` unset
- **THEN** the workflow executes normally

### Requirement: Step types demo failure leaves onboarding incomplete

When `onboarding:onboarding` or `onboarding:step-types-demo` fails or is cancelled before completing the continue path, the runner SHALL NOT write `settings.onboarding.completed_at`. Native setup completion settings SHALL remain unchanged.

#### Scenario: Demo failure does not write onboarding settings
- **WHEN** `onboarding:onboarding` starts and the step-types demo fails before completion
- **THEN** `settings.onboarding.completed_at` is not written

#### Scenario: Demo failure does not change setup state
- **WHEN** `onboarding:onboarding` fails after native setup completed
- **THEN** `settings.setup.completed_at` remains unchanged

#### Scenario: Direct invocation with demo failure keeps standard post-run behavior
- **WHEN** the user runs `agent-runner run onboarding:onboarding` directly and the demo reaches a terminal failure or cancellation
- **THEN** the runner SHALL behave per the existing direct-run view rules and SHALL NOT auto-transition to the list-runs TUI

### Requirement: Onboarding namespace embedded

The builtin set SHALL include an `onboarding` namespace alongside the existing `core`, `openspec`, and `spec-driven` namespaces. The `onboarding` namespace SHALL contain at minimum `onboarding` as the top-level demo workflow and `step-types-demo` as the workflow step demonstration. The namespace SHALL NOT be required to contain `welcome` or `setup-agent-profile` workflows because first-run setup is native TUI functionality.

#### Scenario: Onboarding demo workflow invoked by namespace
- **WHEN** the user runs `agent-runner run onboarding:onboarding`
- **THEN** the workflow loads from the embedded `onboarding` namespace and executes

#### Scenario: Step types demo workflow exists
- **WHEN** the user runs `agent-runner run onboarding:step-types-demo`
- **THEN** the workflow loads from the embedded `onboarding` namespace and executes

#### Scenario: Welcome workflow not required
- **WHEN** the builtin onboarding namespace is embedded
- **THEN** it is valid for `welcome` to be absent as a workflow because native setup owns the welcome surface

#### Scenario: Setup workflow not required
- **WHEN** the builtin onboarding namespace is embedded
- **THEN** it is valid for `setup-agent-profile` to be absent as a workflow because native setup owns profile setup

### Requirement: Non-YAML files embedded as bundled assets

Files in a namespace subdirectory whose names do not end in `.yaml` SHALL be embedded as bundled assets and accessible at runtime via the relative paths declared by supported builtin workflow references. The embed mechanism SHALL preserve file mode bits relevant to execution where the host filesystem records them. Asset path resolution SHALL stay within the namespace; the runner SHALL NOT fall back to user-authored workflows under `.agent-runner/workflows/` when an embedded workflow references a bundled asset.

#### Scenario: Embedded onboarding docs accessible
- **WHEN** the embedded onboarding demo references packaged documentation for Q&A
- **THEN** the documentation files are embedded and accessible at runtime

#### Scenario: Embedded asset does not fall back to user directory
- **WHEN** an embedded onboarding workflow references a bundled asset and a user-authored file with the same relative path exists
- **THEN** the embedded asset is used and the user file is not consulted

#### Scenario: Bundled JSON data file embedded
- **WHEN** a namespace subdirectory contains a non-YAML data file referenced by a bundled workflow or asset
- **THEN** the file is embedded and accessible at runtime via its relative path within the namespace

#### Scenario: Top-level non-YAML files not exposed
- **WHEN** the repository's `workflows/` directory contains a non-YAML file at the top level
- **THEN** that file is not exposed as a bundled asset under any namespace

### Requirement: Embedded vs on-disk script resolution

When the containing workflow is part of the embedded builtin set, the runner SHALL resolve `script:` references only against the embedded namespace and SHALL NOT fall back to user-authored workflows under `.agent-runner/workflows/`. When the containing workflow is loaded from disk, the runner SHALL read the script from disk relative to the workflow file's directory.

#### Scenario: Embedded script resolves within embedded namespace
- **WHEN** an embedded workflow in the `onboarding` namespace declares `script: helper.sh`
- **THEN** the runner reads the script from the embedded `onboarding/helper.sh` and executes it

#### Scenario: Embedded script does not fall back to user directory
- **WHEN** an embedded workflow in the `onboarding` namespace declares `script: helper.sh` and a file `.agent-runner/workflows/onboarding/helper.sh` exists on the user's disk
- **THEN** the runner uses the embedded script, not the user file

#### Scenario: On-disk workflow reads script from disk
- **WHEN** a workflow loaded from `.agent-runner/workflows/foo/main.yaml` declares `script: helper.sh`
- **THEN** the runner executes `.agent-runner/workflows/foo/helper.sh`

### Requirement: Onboarding demo settings

The user settings schema SHALL continue to support onboarding demo completion under `onboarding.completed_at` and onboarding demo dismissal under `onboarding.dismissed`. `onboarding.completed_at` records successful completion of the onboarding demo workflow. `onboarding.dismissed` records explicit dismissal of the optional onboarding demo. Both settings SHALL be distinct from native setup completion.

#### Scenario: Completed onboarding timestamp loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `onboarding.completed_at: 2026-05-03T00:00:00Z`
- **THEN** settings load exposes that timestamp to onboarding demo dispatch logic

#### Scenario: Dismissed onboarding timestamp loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `onboarding.dismissed: 2026-05-03T00:00:00Z`
- **THEN** settings load exposes that timestamp to onboarding demo dispatch logic

#### Scenario: Setup and onboarding completion are independent
- **WHEN** settings contain both `setup.completed_at` and `onboarding.completed_at`
- **THEN** settings load exposes both timestamps independently

#### Scenario: Onboarding completion preserved on setup write
- **WHEN** the runner writes setup tracking settings
- **THEN** existing `onboarding.completed_at` is preserved unless the caller explicitly changes it

## Done When

Targeted tests cover the onboarding demo workflow scenarios and builtin namespace scenarios above. `agent-runner run onboarding:onboarding` resolves and runs the demo intro path, `onboarding:welcome` and `onboarding:setup-agent-profile` no longer resolve, and docs/tests no longer describe setup as workflow-owned.
