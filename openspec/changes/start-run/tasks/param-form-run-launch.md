# Task: Param Form and Run Launch

## Goal

Add the workflow parameter form TUI component and wire up the full run-launch flow: when the user presses `r` on a workflow (from the new tab or the definition view), the TUI shows a parameter form if the workflow has params, then launches the run via exec-self. After a run completes or fails, Escape at the top level navigates to the list TUI on the current-dir tab instead of exiting the program.

## Background

### Prerequisites — what this task builds on

This task depends on two packages created before it runs:

1. **`internal/discovery/`** — provides the `WorkflowEntry` type (`Name`, `Description`, `Scope`, `Path`, `Params []model.Param`, `ParseError`). This package must exist before this task starts.

2. **`internal/listview/`** and **`internal/runview/`** with the following message types defined:
   - `StartRunMsg{Entry discovery.WorkflowEntry}` — emitted when the user presses `r` on a workflow row (from the list's new tab) or in the definition view
   - `ViewDefinitionMsg{Entry discovery.WorkflowEntry}` — emitted when the user presses Enter on a workflow row
   - `FromDefinition` entry mode on the run view's `Entered` enum

   These types and the `FromDefinition` enum value are defined in the prior task. This task wires the handler for `StartRunMsg`.

### Exec-self pattern

When launching a new run from the TUI, use `syscall.Exec` to replace the current process with `agent-runner run <workflow-canonical-name> param1=value1 param2=value2 ...`. This reuses the existing `handleRun()` path in `cmd/agent-runner/main.go` exactly — no in-process wiring needed.

Look at the existing `execRunnerResume()` function (around line 546 in `cmd/agent-runner/main.go`) for the exact pattern:
```go
self, _ := os.Executable()
syscall.Exec(self, []string{filepath.Base(self), "--resume", runID}, os.Environ())
```

For launching a new run, the args would be: `[basename, "run", canonicalName, "key=value", ...]`.

Similarly, when a `FromLiveRun` or resumed run completes/fails and the user presses Escape, exec-self with `[basename, "--resume"]` (no run ID) — which opens the list TUI on the current-dir tab.

### StartRunMsg handling

The prior task emits `StartRunMsg{WorkflowEntry}` when `r` is pressed on the new tab (both from the workflow list and from the definition view). This task handles that message in the top-level switcher (wherever `ViewRunMsg` and `ViewDefinitionMsg` are handled in `cmd/agent-runner/main.go`).

**Handling StartRunMsg:**
1. If the workflow has no params (`len(entry.Params) == 0`): exec-self immediately with `run <name>` (no param form needed).
2. If the workflow has params: show the param form as a modal. On form submission, exec-self with `run <name> key=value...`. On Escape, return to the previous view.

The definition view's `r` keybinding also emits `StartRunMsg` — wire it the same way.

### Param form component: `internal/paramform/`

Create a new bubbletea model in `internal/paramform/` using `charmbracelet/bubbles/textinput`.

Add `github.com/charmbracelet/bubbles` to `go.mod`. This is the standard companion library to bubbletea, maintained by the same team. Use only the `textinput` sub-package.

**Model structure:**
```go
type Model struct {
    entry    discovery.WorkflowEntry
    inputs   []textinput.Model  // one per param, in declaration order
    focused  int                // index of focused input (-1 = Start button focused)
    errors   []string           // one per input, empty string if no error
    width    int
}
```

**Initialization:**
- Create one `textinput.Model` per `entry.Params` entry.
- Set `Default` as the initial value if present (use `SetValue`).
- Focus the first input.

**Layout (rendered by `View()`):**
```
 <workflow canonical name>      ← AccentCyan bold (HeaderStyle)
 <description>                  ← DimText (if present)

 <label> *  │<input text>                   │   ← focused: AccentCyan border
 <label>    │<input text>                   │   ← unfocused: DimText border

                  ‹ Start ›

```
- Labels right-padded to align all input boxes.
- `*` in `FailedRed` after the label for required params (`param.Required == nil || *param.Required == true`).
- Input borders `│` in `DimText`; focused field borders in `AccentCyan`.
- Input text in `BodyText`.
- `‹ Start ›` button below fields in `AccentCyan`; bold when focused (focused == -1).
- Validation errors appear below each offending field in `FailedRed` after a failed submit attempt.
- No tab bar — the param form is a modal that replaces the list view entirely.
- Help bar: `tab/shift+tab navigate  enter start  esc cancel`

**Navigation:**
- `Tab` moves focus forward through inputs then to the Start button, wrapping.
- `Shift+Tab` moves focus backward.
- Arrow keys within a focused input move the text cursor (handled by `textinput.Update`).

**Submission:**
- `Enter` on the last input or on the Start button triggers submit.
- Validate: all required params (nil or true) must have non-empty values.
- On failure: populate `errors` slice, re-render (errors shown inline per field). Do not exec.
- On success: return the param map as `map[string]string` so the caller can build the exec args.

**Cancellation:**
- `Escape`: return a cancel signal to the caller. The caller (top-level switcher) restores the previous view.

**`Update()` return value:** The model should return a message or command that signals completion. A clean approach: define `type SubmittedMsg map[string]string` and `type CancelledMsg struct{}` in the paramform package. The top-level switcher handles these.

### Post-run Escape to list

In `internal/runview/model.go`, the `handleEsc()` function (around line 679) handles Escape at the top level. Currently, for `FromLiveRun`, it exits the program. Change this:

The rule: **if the run is in a terminal state (completed or failed) AND the entry mode is not `FromInspect`, exec-self to the list. Otherwise use the existing behavior.**

Concretely:
- **`FromLiveRun`, terminal state**: exec-self with `[basename, "--resume"]` (no arg) — opens list on current-dir tab.
- **`FromList`, terminal state**: exec-self with `[basename, "--resume"]` (no arg). This covers runs resumed via `r` from the run list that have now finished.
- **`FromList`, non-terminal state**: Unchanged — returns `BackMsg{}` to the list.
- **`FromInspect`**: Unchanged — Escape exits the program regardless of run state.
- **`FromDefinition`**: Unchanged from prior task — returns `BackMsg{}` to the list.

To detect "terminal state": check `m.liveResult` (set when `FromLiveRun` completes), OR check `m.active == false` and read run status (`completed` or `failed`) from the run state file. For `FromList`, `m.active` is set from the run-lock check at init — a resumed run that reaches terminal state will have `m.active == false` and a `completed`/`failed` status in the state file.

Look at `internal/runview/model.go` carefully — the `Entered` field and `running`/`active` fields govern this logic. The existing `execRunnerResume()` pattern (already used for agent-CLI session resume) is the right pattern.

### Definition view `r` keybinding

In `FromDefinition` mode, `r` should emit `StartRunMsg{workflowEntry}`. The workflow entry must be passed into `runview.New()` when entering `FromDefinition` mode (add it as a field on the model, set only in `FromDefinition` mode). The help bar in `FromDefinition` mode should show `r start run`.

**Key files to read before starting:**
- `internal/runview/model.go` — full file, especially `handleEsc()`, `Entered` modes, `liveResult`, `active`, `running` fields
- `cmd/agent-runner/main.go` — `execRunnerResume()` (~line 546), `handleRun()` (~line 808), `ViewRunMsg` handling, `BackMsg` handling
- `internal/listview/model.go` — `StartRunMsg` type definition (if defined there in prior task) and `ViewDefinitionMsg`
- `internal/tuistyle/styles.go` — color tokens for param form styling
- `internal/model/step.go` — `Param` struct (`Required *bool`, `Default string`)
- `internal/discovery/` — `WorkflowEntry` type
- Follow TDD: write a failing test first for each behavioral unit

## Spec

### Requirement: Start run from definition view
The workflow definition view SHALL provide an `r` keybinding that initiates starting a run. Pressing `r` SHALL transition to the `workflow-param-form` (if the workflow has parameters) or launch the run directly (if no parameters).

#### Scenario: r on workflow with parameters opens param form
- **WHEN** the user presses `r` on a workflow that declares one or more parameters
- **THEN** the param form is presented for that workflow

#### Scenario: r on workflow with no parameters launches immediately
- **WHEN** the user presses `r` on a workflow with no declared parameters
- **THEN** a new run is launched and the view transitions to the live run view

#### Scenario: Help bar shows r binding
- **WHEN** the workflow definition view is open
- **THEN** the help bar includes `r start run`

### Requirement: New tab r keybinding starts a run
On the new tab, `r` on a workflow row SHALL initiate starting a run.

#### Scenario: r starts a run with parameters
- **WHEN** the user presses `r` on a workflow that declares parameters
- **THEN** the param form is presented for that workflow

#### Scenario: r starts a run without parameters
- **WHEN** the user presses `r` on a workflow with no declared parameters
- **THEN** a new run launches and the view transitions to the live run view

#### Scenario: r on malformed workflow is ignored
- **WHEN** the user presses `r` on a malformed workflow row
- **THEN** no action is taken

### Requirement: Parameter form display
The param form SHALL display one labeled text input field per declared `Param` on the workflow. Required parameters SHALL be visually marked. Parameters with a `Default` value SHALL pre-populate the input field with that default. Parameters SHALL be displayed in the order they are declared in the workflow YAML.

#### Scenario: Required param shown with marker
- **WHEN** the param form opens for a workflow with a required parameter `task_file`
- **THEN** the form displays a text input labeled `task_file` with a visual indicator that it is required

#### Scenario: Optional param with default pre-populated
- **WHEN** the param form opens for a workflow with an optional parameter `branch` that has default `main`
- **THEN** the form displays a text input labeled `branch` pre-populated with `main`

#### Scenario: Optional param without default shown empty
- **WHEN** the param form opens for a workflow with an optional parameter `tag` that has no default
- **THEN** the form displays an empty text input labeled `tag`

#### Scenario: Params displayed in declaration order
- **WHEN** the workflow declares params `[a, b, c]` in that order
- **THEN** the form displays fields in the order `a`, `b`, `c`

### Requirement: Form navigation
The user SHALL navigate between fields using Tab (forward) and Shift+Tab (backward). Focused field borders SHALL render in the accent color; unfocused borders in the dim color.

#### Scenario: Tab moves to next field
- **WHEN** the user presses Tab while focused on a field
- **THEN** focus moves to the next field in order

#### Scenario: Shift+Tab moves to previous field
- **WHEN** the user presses Shift+Tab while focused on a field that is not the first
- **THEN** focus moves to the previous field

### Requirement: Form submission and validation
On submit, the form SHALL validate that all required parameters have non-empty values. Failure shows inline errors without launching the run. Success returns the parameter map and the run launches.

#### Scenario: Submit with all required params filled
- **WHEN** all required parameter fields have non-empty values and the user submits
- **THEN** the run launches with the entered parameter values

#### Scenario: Submit with missing required param
- **WHEN** a required parameter field is empty and the user submits
- **THEN** the form displays an error identifying the missing required field and does not launch

#### Scenario: Submit with optional param left empty
- **WHEN** an optional parameter field is left empty (no default) and the user submits
- **THEN** validation passes; the parameter is passed as an empty string

#### Scenario: Default value accepted without editing
- **WHEN** a parameter with a default is not edited by the user and the user submits
- **THEN** the default value is used for that parameter

#### Scenario: Enter on last field submits
- **WHEN** the user presses Enter while focused on the last field
- **THEN** the form submits (validation and launch proceed)

### Requirement: Form cancellation
Pressing Escape SHALL cancel the param form and return to the previous view without launching a run.

#### Scenario: Escape cancels and returns to previous view
- **WHEN** the user presses Escape on the param form
- **THEN** the form closes without launching, and the previous view is restored

#### Scenario: Partial input discarded on cancel
- **WHEN** the user has entered values into some fields and presses Escape
- **THEN** all entered values are discarded; no run is launched

### Requirement: Post-run Escape navigates to list
When a run has reached a terminal state (completed or failed), Escape at the top level SHALL exec `agent-runner --resume` (no arg), opening the list TUI on the current-dir tab.

#### Scenario: Escape after live run completion opens list
- **WHEN** a run started via `FromLiveRun` has completed and the user presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` (no arg), opening the list TUI on the current-dir tab

#### Scenario: Escape after live run failure opens list
- **WHEN** a run started via `FromLiveRun` has failed and the user presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` (no arg), opening the list TUI on the current-dir tab

#### Scenario: Escape after resumed run completion opens list
- **WHEN** a run resumed via `r` from the run list has completed and the user presses Escape at the top level
- **THEN** the process execs `agent-runner --resume` (no arg), opening the list TUI on the current-dir tab

#### Scenario: Escape after --inspect still exits
- **WHEN** a run opened via `--inspect` is at the top level and the user presses Escape
- **THEN** the program exits (unchanged behavior)

## Done When

Tests covering the above scenarios pass. The param form renders correctly, validates on submit, and cancels cleanly. Pressing `r` on a workflow with params shows the form; without params, execs directly into a run. After run completion/failure, Escape opens the list TUI on the current-dir tab. `make build` succeeds with the new `charmbracelet/bubbles` dependency.
