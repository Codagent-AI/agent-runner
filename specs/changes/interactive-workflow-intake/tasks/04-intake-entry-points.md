# Task: Intake entry points — `-i`, run-scoped overrides, TTY gate, and the New tab entry

## Goal

Give users two ways into intake: a highlighted "Plan with an agent" entry at the top of the New tab, and
`agent-runner -i`, which starts intake immediately, skipping the New tab. Define `-i`'s flag
compatibility precisely, add run-scoped `--cli`/`--model` overrides that apply to the intake agent only,
and enforce that intake requires a real terminal however it is started.

To be unambiguous about what `-i` skips: it bypasses the workflow-browser list TUI, not the live
interactive run UI. The intake run itself executes through the normal live interactive path and requires
a real terminal — `-i` is never a headless or non-TUI mode.

Today the default entry point requires the user to choose a workflow before any agent can help them
choose one. This task is what puts intake in front of them.

## Background

### The manual browser is untouched

The existing workflow browser and parameter form remain unchanged and fully usable for users who already
know what they want. Invoking `agent-runner` with no arguments still opens the New tab; it does not start
intake automatically. Explicit CLI and headless invocations continue to bypass intake entirely.

### Flag rules

```
agent-runner -i
agent-runner -i --cli codex --model gpt-5.2
```

`-i` joins the existing mutual-exclusivity checks in `run()` (`cmd/agent-runner/main.go`, around the
`--list` / `--onboarding-from` / positional-argument handling at lines 440-473). It is rejected with
`--headless`, `--list`, `--resume`, `--inspect`, `--validate`, `--onboarding-from`, or a workflow
positional argument. `--cli` and `--model` require `-i`. `-C` remains compatible.

### The TTY check cannot go through `requireTTY`

`requireTTY` (`cmd/agent-runner/tty.go:14`) returns nil when `AGENT_RUNNER_NO_TUI=1` — precisely the
variable `--headless` sets. Intake checks `isatty` on **stdin and stdout directly** instead.

Crucially, **this requirement attaches to intake itself rather than to the `-i` flag.** The intake
workflow is an ordinary embedded workflow, so `agent-runner core:intake` starts it too. If the terminal
check lived only in the `-i` path, `--headless core:intake` would be accepted. Both `--headless -i` and
`--headless core:intake` must be refused, and so must any invocation of intake without a real stdin and
stdout. `-i` stays a pure entry-point convenience: it decides how intake *starts*, never what intake
*does*.

### Why `--cli`/`--model` are a run-scoped override rather than step interpolation

Making step-level `cli` and `model` interpolable is not viable as a contained change: `ParseWorkflow`
calls `w.Validate(cli.KnownCLIs())` at load time, **before any interpolation runs**, so a `{{...}}`
placeholder would be rejected outright rather than passed through. Supporting it would mean changing
loader validation, prevalidation, and probing, which widens the change well beyond intake.

Instead the override travels on the execution context and is consulted during agent resolution, in
`resolveStepProfile` (`internal/exec/`), immediately after the existing step-level overrides:

```go
// existing
if step.Model != "" { resolved.Model = step.Model }
if step.CLI   != "" { resolved.CLI   = step.CLI }

// new, run-scoped, highest precedence
if ov := ctx.AgentOverride; ov != nil {
    if ov.Model != "" { resolved.Model = ov.Model }
    if ov.CLI   != "" { resolved.CLI   = ov.CLI }
}
```

Precedence is therefore *command override → step declaration → agent profile*. Loader validation,
prevalidation, and probing are untouched by this choice.

**Validation happens at flag-parse time, before any run is created**, on two counts: the CLI must be in
`cli.KnownCLIs()`, and it must **support interactive steps**. The second check matters because OpenCode
is a *known* adapter that returns `InteractiveModeError`, so membership alone would accept
`-i --cli opencode` and then fail after a run had already been created.

The override is persisted in intake run state and restored by `PrepareResume`, so a resumed intake
continues under the same CLI and model rather than silently reverting to the profile. `PrepareResume`
copies an explicit field list, so this is a deliberate addition. It is **confined to the intake run**:
the launch path must neither read it nor populate it on the launched run's options, so the launched
workflow resolves every agent through the ordinary step and profile rules.

### The New tab entry

`internal/listview/newtab.go` models rows as a `rowKind` enum — `workflowRow`, `headerRow`,
`separatorRow` (lines 17-23) — built by `buildFilteredRows` (line ~88) and navigated via
`firstSelectableRow` and the cursor logic in `internal/listview/model.go:796-825` (moving down from the
search box goes to the first selectable item; moving up from the first selectable row focuses the search
box; navigation skips non-selectable rows).

The intake entry renders **above every group**, is the **initial cursor position**, and is exempt from
both the search filter and the `h` show-hidden toggle. Note `rebuildNewTabFiltered` resets the cursor to
the first selectable row on every filter or toggle change, so the entry must remain the first selectable
row under all filter and toggle states.

Selecting it emits a message that the top-level switcher turns into:

```go
execSelfWithEnv([]string{liveRunImmediateAltScreenEnv + "=1"}, "-i")
```

mirroring how `execStartRun` already works (`cmd/agent-runner/main.go:955-1063`).

### Interaction with the `h` toggle and `core:intake`

The intake workflow is embedded as `core:intake` with `hidden: true`, so it does not appear as an
ordinary workflow row by default but does appear in the Core group when `h` is on. That is existing
hidden-workflow behavior. The dedicated entry is independent of it and stays visible in both states —
so with `h` on, both the dedicated entry and a `core:intake` row are present, which is expected.

### What already exists that you depend on

`{{intake_handoff}}` is a built-in on every run, reserved and pre-validation-aware. The
`internal/intakeroute` sidecar exists. Route submission, eligibility, and freeze-on-completion work over
the control channel. `workflows/core/intake-v1.0.yaml` exists as a hidden `core:intake` workflow with a
route-eligible interactive agent step.

### Scope boundary

The cross-process launch of the selected workflow after intake finalizes is delivered elsewhere in this
change. This task ends when intake **starts** correctly from both entry points, under the right flags,
on a real terminal, with the right agent. Do not add the post-run launch gate.

Note that `styles.go` / `view.go` presentation choices — exact copy, color, and visual treatment — are
implementation details the spec deliberately does not pin. Per the repository's development workflow,
purely stylistic tweaks do not need TDD; the navigation and filtering **behavior** does.

## Spec

From `specs/workflow-intake/spec.md`:

### Requirement: Intake entry points

Agent Runner SHALL provide two entry points into the built-in intake workflow: a dedicated "Plan with an agent" entry on the New tab, and an `-i` command-line flag that starts intake directly without opening the TUI. Invoking `agent-runner` with no arguments SHALL continue to open the New tab rather than starting intake automatically.

#### Scenario: New tab entry starts intake
- **WHEN** the user selects the "Plan with an agent" entry on the New tab
- **THEN** Agent Runner starts a run of the built-in intake workflow

#### Scenario: `-i` starts intake directly
- **WHEN** the user runs `agent-runner -i` in an interactive terminal
- **THEN** Agent Runner starts a run of the built-in intake workflow without first rendering the New tab

#### Scenario: No-argument invocation is unchanged
- **WHEN** the user runs `agent-runner` with no arguments in an interactive terminal
- **THEN** the New tab opens as it does today, and no intake run starts until the user selects the intake entry

### Requirement: Intake invocation constraints

`-i` SHALL be rejected when combined with `--headless`, `--list`, `--resume`, `--inspect`, `--validate`, `--onboarding-from`, or a workflow positional argument. `--cli` and `--model` SHALL be rejected unless `-i` is also present. `-C` SHALL remain compatible with `-i`.

Intake SHALL require that both stdin and stdout are real interactive terminals, and SHALL NOT honor the `AGENT_RUNNER_NO_TUI` bypass that other TTY checks accept. This requirement attaches to intake itself rather than to the `-i` flag, so naming the intake workflow explicitly under `--headless` SHALL be rejected for the same reason.

#### Scenario: `-i` with `--headless` is rejected
- **WHEN** the user runs `agent-runner --headless -i`
- **THEN** Agent Runner exits nonzero with an error stating the flags are mutually exclusive
- **AND** no intake run is created

#### Scenario: `-i` with a workflow argument is rejected
- **WHEN** the user runs `agent-runner -i spec-driven:change`
- **THEN** Agent Runner exits nonzero with an error stating that `-i` cannot be combined with an explicit workflow
- **AND** neither intake nor the named workflow starts

#### Scenario: Override flags require `-i`
- **WHEN** the user runs `agent-runner --cli codex spec-driven:change`
- **THEN** Agent Runner exits nonzero with an error stating that `--cli` and `--model` require `-i`

#### Scenario: Intake requires a real terminal
- **WHEN** the user runs `agent-runner -i` with `AGENT_RUNNER_NO_TUI=1` set and stdout redirected to a file
- **THEN** Agent Runner exits nonzero with an error stating an interactive terminal is required
- **AND** the `AGENT_RUNNER_NO_TUI` value does not suppress the check

#### Scenario: Directory change remains compatible
- **WHEN** the user runs `agent-runner -C /path/to/project -i` in an interactive terminal
- **THEN** Agent Runner changes to the named directory and starts intake there

#### Scenario: Headless intake by name is rejected
- **WHEN** the user names the intake workflow explicitly under `--headless`
- **THEN** Agent Runner exits nonzero stating that intake requires an interactive terminal
- **AND** no intake run is created

### Requirement: Intake agent overrides

`--cli` and `--model` SHALL apply only to the intake agent, resolved with precedence *command override → step declaration → agent profile*. The overrides SHALL NOT be inherited by the workflow that intake subsequently launches. An override SHALL be rejected before the intake run starts when it names an unknown CLI, or when it names a known CLI that does not support interactive steps, since intake cannot execute under such an adapter. The overrides SHALL be persisted with the intake run and restored on resume, so a resumed intake continues under the same CLI and model it started with.

#### Scenario: Command override beats the profile
- **WHEN** the user runs `agent-runner -i --cli codex` and the intake agent's profile declares a different CLI
- **THEN** the intake agent session runs under `codex`

#### Scenario: Command override beats the step declaration
- **WHEN** the user runs `agent-runner -i --model <model>` and the intake workflow's agent step declares a different model
- **THEN** the intake agent session runs under the model given on the command line

#### Scenario: Overrides do not reach the launched run
- **WHEN** an intake run started with `--cli codex` launches a selected workflow
- **THEN** the launched run resolves every agent through the normal step and profile rules, with no intake override applied

#### Scenario: Unknown CLI is rejected before intake starts
- **WHEN** the user runs `agent-runner -i --cli not-a-real-cli`
- **THEN** Agent Runner exits nonzero naming the invalid value and the accepted values
- **AND** no intake run is created

#### Scenario: CLI that cannot run interactive steps is rejected
- **WHEN** the user overrides the CLI with a known adapter that rejects interactive steps
- **THEN** Agent Runner exits nonzero explaining that intake requires an interactive-capable CLI
- **AND** no intake run is created

#### Scenario: Overrides survive resume
- **WHEN** an intake run started with `--cli` or `--model` is interrupted and resumed
- **THEN** the resumed intake agent session runs under the same overridden CLI and model

> "Overrides do not reach the launched run" is the guarantee your design must make structurally true:
> the override lives on the intake run's own execution context and state, and nothing carries it
> outward. The launch path itself is delivered elsewhere in this change; your portion is that the
> override is confined to the intake run and is never written into anything a launch would read.

### Requirement: Invocations naming another workflow bypass intake

Invocations that name a workflow other than the intake workflow, and headless invocations that name no workflow, SHALL never start intake. Naming the intake workflow explicitly is not a bypass: it starts intake, subject to the interactive-terminal requirement above.

#### Scenario: Explicit non-intake workflow never starts intake
- **WHEN** the user runs `agent-runner spec-driven:change change_name=example`
- **THEN** the named workflow runs directly and no intake run is created

#### Scenario: Headless invocation with a workflow is unchanged
- **WHEN** the user runs `agent-runner --headless spec-driven:change change_name=example`
- **THEN** the named workflow runs headlessly exactly as it does today

#### Scenario: Headless invocation without a workflow does not start intake
- **WHEN** the user runs `agent-runner --headless` with no workflow
- **THEN** Agent Runner does not start intake

From `specs/new-tab-layout/spec.md`:

### Requirement: Plan with an agent entry

The new tab SHALL render a selectable "Plan with an agent" entry above every workflow group. Selecting it SHALL start the built-in intake workflow. The entry SHALL always be present: it SHALL NOT be removed by the search filter, and the show-hidden toggle SHALL NOT affect it. Exact copy and visual treatment are implementation details and are not pinned by this spec.

#### Scenario: Entry renders above every group
- **WHEN** the new tab renders with at least one visible workflow
- **THEN** the intake entry appears above the first group's header

#### Scenario: Entry renders when no workflow is visible
- **WHEN** the new tab renders and no workflow group is visible
- **THEN** the intake entry still appears and remains selectable

#### Scenario: Selecting the entry starts intake
- **WHEN** the cursor is on the intake entry and the user activates it
- **THEN** Agent Runner starts a run of the built-in intake workflow

#### Scenario: Search filter does not remove the entry
- **WHEN** the user types a search filter that excludes some or all workflow rows
- **THEN** the intake entry remains visible regardless of the filter text

#### Scenario: Hidden toggle does not affect the entry
- **WHEN** the user presses `h` to toggle hidden workflow visibility
- **THEN** the intake entry's presence is unchanged

#### Scenario: Downward navigation leaves the entry
- **WHEN** the cursor is on the intake entry and the user presses `down`
- **THEN** the cursor moves to the first workflow row of the first visible group, skipping that group's header

### Requirement: Workflow groups render with header and description

The new tab SHALL render each workflow group with a header containing the group's display name and its description. The header SHALL appear above the group's workflow rows. The header SHALL NOT be selectable — the cursor SHALL skip over it when the user navigates with the keyboard. The visual arrangement of the display name relative to the description (same line, separate lines, etc.) is an implementation detail and not pinned by this spec.

#### Scenario: Project group renders with header and description
- **WHEN** the new tab renders and the project scope contains at least one visible workflow
- **THEN** a header identifying the group as the project's workflows appears above the project workflows
- **AND** the header includes a non-empty description (exact copy and visual layout are implementation details and not pinned by this spec)

#### Scenario: User group renders with header and description
- **WHEN** the new tab renders and the user scope contains at least one visible workflow
- **THEN** a header identifying the group as the user's workflows appears above the user workflows
- **AND** the header includes a non-empty description (exact copy and visual layout are implementation details and not pinned by this spec)

#### Scenario: Builtin group renders using namespace metadata
- **WHEN** the new tab renders a builtin namespace whose metadata file declares a display name and description
- **THEN** the header shows the declared display name
- **AND** the header shows the declared description

#### Scenario: Header is not focusable when navigating downward
- **WHEN** the cursor is on the row immediately above a header and the user presses `down`
- **THEN** the cursor moves to the first workflow row of the group below the header, skipping the header

#### Scenario: Header is not focusable when navigating upward
- **WHEN** the cursor is on the first workflow row of a non-first group and the user presses `up`
- **THEN** the cursor lands on the last workflow row of the previous group, skipping the current group's header and any separator between the groups

#### Scenario: Initial cursor position is the intake entry
- **WHEN** the new tab opens fresh
- **THEN** the initial cursor position is on the "Plan with an agent" entry, above the first group's header

#### Scenario: Upward navigation from the first workflow reaches the intake entry
- **WHEN** the cursor is on the first workflow row of the first visible group and the user presses `up`
- **THEN** the cursor lands on the intake entry, skipping that group's header

#### Scenario: Upward navigation from the intake entry focuses the search box
- **WHEN** the cursor is on the intake entry and the user presses `up`
- **THEN** the search box receives focus and the cursor leaves the list

## Test Plan

You MUST read `test-plan.md` for the full text of the obligations below.

**E2E-005: Intake requires a real terminal however it is started.** Surface: the `agent-runner` binary.
Setup: isolated `HOME` and working directory; no PTY attached. Journey: invoke `-i` under `--headless`;
invoke the intake workflow **by name** under `--headless`; invoke `-i` with `AGENT_RUNNER_NO_TUI=1` and
stdout redirected; invoke a non-intake workflow normally. Assert: the first three exit nonzero stating an
interactive terminal is required and create **no run directory**; the fourth runs normally, proving the
check attaches to intake rather than to headless operation generally. Execution: `cmd/agent-runner`
integration test, `go test ./...`.

**INT-005: Handoff copy and provenance round-trip** — extend the existing resume round-trip coverage in
`internal/runner` with the override assertion: a resumed intake run restores its `--cli` and `--model`
override rather than reverting to the profile. Execution: `internal/runner` package tests,
`go test ./...`.

The test plan deliberately leaves the `-i` flag rejection matrix to unit tests, including `--cli` naming
an adapter that rejects interactive steps — `cmd/agent-runner/main_test.go` already covers argument
parsing, so extend it there. The design also asks for override precedence *command > step > profile* in
`internal/exec`, and a TTY check not bypassed by `AGENT_RUNNER_NO_TUI` for both `-i` and by-name
invocation in `cmd/agent-runner`. New tab entry position, filter exemption, toggle exemption, and the
up/down navigation scenarios belong in `internal/listview` tests.

## Done When

- `-i` starts a run of `core:intake` directly, without rendering the New tab; `agent-runner` with no
  arguments still opens the New tab and starts nothing.
- The flag rejection matrix is enforced in `run()`: `-i` with any of `--headless`, `--list`, `--resume`,
  `--inspect`, `--validate`, `--onboarding-from`, or a workflow positional argument exits nonzero with a
  specific message and creates no run; `--cli`/`--model` without `-i` exits nonzero; `-C` composes with
  `-i`.
- Intake's terminal check tests `isatty` on stdin **and** stdout directly, does not route through
  `requireTTY`, is not suppressed by `AGENT_RUNNER_NO_TUI=1`, and is attached to intake so that
  `--headless core:intake` is rejected for the same reason as `--headless -i`.
- `--cli`/`--model` are validated at flag-parse time against `cli.KnownCLIs()` **and** interactive-step
  support, so `-i --cli opencode` is rejected before a run exists, with a message explaining that intake
  requires an interactive-capable CLI. An unknown CLI names the invalid value and the accepted values.
- The override is carried on the execution context, consulted in `resolveStepProfile` immediately after
  the step-level overrides so precedence is command > step > profile, persisted in the intake run's
  state, and restored by `PrepareResume` from its explicit field list.
- The override is confined to the intake run: nothing writes it anywhere a subsequently launched run
  would read it.
- `internal/listview` renders a selectable intake entry above every group, holding the initial cursor,
  exempt from the search filter and the `h` toggle, and remaining the first selectable row after any
  filter or toggle rebuild. `down` from it reaches the first workflow row of the first visible group,
  skipping the header; `up` from it focuses the search box; `up` from the first workflow row reaches it.
- The entry renders and stays selectable when no workflow group is visible.
- Selecting it re-execs via `execSelfWithEnv([]string{liveRunImmediateAltScreenEnv + "=1"}, "-i")`.
- Existing group header rendering and header-skipping navigation still behave as specified.
- E2E-005 passes; the override-resume assertion of INT-005 passes; the flag matrix and listview
  navigation unit tests pass.
- Nothing launches after intake finishes — that gate is not part of this task.
- `make fmt`, `make lint`, and `make test` pass.
