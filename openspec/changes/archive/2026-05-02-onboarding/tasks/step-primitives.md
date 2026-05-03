# Task: New Step Primitives (Typed Captures, Script Step, UI Step)

## Goal

Add two new step types (`mode: ui` and `script:`) and extend the capture system from string-only to typed values (`string | list<string> | map<string,string>`). This delivers the runtime primitives that the onboarding workflow (built in a subsequent task) depends on.

## Background

You MUST read these files before starting:
- `openspec/changes/onboarding/design.md` — full design with approach, data structures, and decisions
- `openspec/changes/onboarding/specs/ui-step/spec.md` — UI step requirements and scenarios
- `openspec/changes/onboarding/specs/workflow-bundled-scripts/spec.md` — script step requirements
- `openspec/changes/onboarding/specs/output-capture/spec.md` — typed capture requirements
- `openspec/changes/onboarding/specs/step-model/spec.md` — model/cli validation on new step types

### Architecture Summary

**Typed captures.** `CapturedVariables` is currently `map[string]string` in `internal/model/context.go` (ExecutionContext), `internal/model/state.go` (NestedStepState), and `internal/runner/runner.go` (Options). Change it to `map[string]CapturedValue` where:

```go
type CaptureKind string
const (
    CaptureString CaptureKind = "string"
    CaptureList   CaptureKind = "list"
    CaptureMap    CaptureKind = "map"
)
type CapturedValue struct {
    Kind CaptureKind
    Str  string
    List []string
    Map  map[string]string
}
```

State JSON uses a tagged envelope: `{"kind":"string","value":"hello"}`. A backward-compat UnmarshalJSON handles legacy state files with plain string values. Audit logs serialize strings as-is, lists as JSON arrays, maps as JSON objects.

**Interpolation.** The regex in `internal/textfmt/interpolation.go` is currently `\{\{(\w+)\}\}`. Extend to `\{\{(\w+(?:\.\w+)?)\}\}`. Add:
- `InterpolateTyped(template, params map[string]string, captures map[string]CapturedValue, builtins map[string]string)` — for string contexts; `{{name}}` requires string kind (errors on list/map), `{{name.field}}` requires map kind.
- `ResolveTypedValue(expr string, captures map[string]CapturedValue)` — for typed consumers (UI options, script_inputs); returns the full CapturedValue without requiring string coercion.
- A typed variant of `InterpolateShellSafe` that shell-quotes string captures and rejects non-string captures.

The existing `Interpolate` (flat string maps) remains for callers that don't use typed captures.

**Step model.** Add to `internal/model/step.go`:
- Fields: `Script`, `ScriptInputs map[string]string`, `CaptureFormat`, `Title`, `Body`, `Actions []UIAction`, `Inputs []UIInput`, `OutcomeCapture`
- Types: `UIAction{Label, Outcome}`, `UIInput{Kind, ID, Prompt, Options []string, Default}`
- Constant: `ModeUI StepMode = "ui"`
- `StepType()`: add `isUI := s.Mode == ModeUI` and `isScript := s.Script != ""` as booleans in `hasExactlyOneStepType`
- Validation: model/cli rejected on UI and script steps; capture allowed on shell, script, UI-with-inputs, and headless agent (preserving existing behavior); script_inputs requires script; actions requires mode:ui; at least one action required on mode:ui; title required on mode:ui; outcome_capture requires mode:ui with actions; capture_format requires script with capture

**Script step executor.** New file `internal/exec/script.go`:
- Extend `ProcessRunner` interface with `RunScript(path string, stdin []byte, captureStdout bool, workdir string) (ProcessResult, error)`. Implementation: `exec.Command(path)` directly (not sh -c); `c.Stdin = bytes.NewReader(stdin)`; same tee pattern as RunAgent.
- Path resolution: static literal (no interpolation in `script:` field); reject absolute paths and `..` at load time; reject symlinks escaping namespace dir at runtime. For builtin workflows resolve relative to `sessionDir/bundled/<namespace>/`; for user workflows resolve relative to the workflow YAML file's directory.
- Script inputs: interpolate `script_inputs` values (using typed resolution), JSON-encode as object to stdin. No script_inputs → empty stdin (not os.Stdin).
- Capture: `capture_format: text` (default) → CapturedValue string. `capture_format: json` → parse stdout (1 MiB cap, UTF-8 validated) at runtime; JSON array of strings → CaptureList; JSON object of string values → CaptureMap; invalid → step failure with descriptive error.
- Set `AGENT_RUNNER_BUNDLE_DIR` env var on spawned process pointing at the bundled assets directory.

**Bundled asset materialization.** On builtin workflow run start, materialize non-YAML files from the embedded FS namespace into `sessionDir/bundled/<namespace>/`. File modes: scripts 0o700, data 0o600, dirs 0o700. On resume, re-materialize if directory is missing. Add `ListAssets(namespace)` and `ReadAsset(path)` functions to `workflows/embed.go`.

**UI step executor.** New file `internal/exec/uistep.go` (or `internal/uistep/` package for the Bubble Tea model):
- Add `UIStepHandler func(UIStepRequest) (UIStepResult, error)` field to ExecutionContext.
- `UIStepRequest{StepID, Title, Body, Actions []UIAction, Inputs []UIInputResolved}` where options are already resolved to concrete `[]string`.
- `UIStepResult{Outcome string, Inputs map[string]string, Canceled bool}`.
- Executor interpolates body/options (stripping ANSI from runtime values), builds request, calls handler, maps result to captures.
- Handler nil → immediate error "UI steps require a TTY."
- Canceled → OutcomeAborted.
- `outcome_capture:` → CapturedValue string. `capture:` with inputs → CapturedValue map.

**TUI integration.** The liverun coordinator provides the UIStepHandler. It sends the request as a `tea.Msg` to the runview program and blocks on a response channel. The runview switches its main content area to a `uistep.Model` (new Bubble Tea model) while keeping the sidebar visible. The model renders markdown body, action buttons, single-select inputs. On user action, result is sent back through the response channel.

Cancellation: Ctrl-C → `UIStepResult{Canceled: true}` → OutcomeAborted. Program shutdown / context cancel → handler returns error → step failure.

**Settings refactor.** Refactor `internal/usersettings/settings.go` from hand-parsed YAML to struct-based marshal:
```go
type OnboardingSettings struct {
    CompletedAt string `yaml:"completed_at,omitempty"`
    Dismissed   string `yaml:"dismissed,omitempty"`
}
type Settings struct {
    Theme      Theme              `yaml:"theme,omitempty"`
    Onboarding OnboardingSettings `yaml:"onboarding,omitempty"`
}
```
Custom MarshalYAML/UnmarshalYAML preserving unknown keys via yaml.Node. Atomic write preserved.

### Key files to modify

- `internal/model/context.go` — CapturedVariables type change, add UIStepHandler field
- `internal/model/state.go` — CapturedVariables type change, custom JSON marshal/unmarshal
- `internal/model/step.go` — new fields, types, modes, validation
- `internal/textfmt/interpolation.go` — regex extension, InterpolateTyped, ResolveTypedValue
- `internal/exec/interfaces.go` — RunScript on ProcessRunner
- `internal/exec/shell.go` — adapt to CapturedValue (captureShellOutput, contextSnapshot, ShouldSkipStep)
- `internal/exec/agent.go` — adapt capture to CapturedValue
- `internal/exec/subworkflow.go` — adapt CapturedVariables propagation
- `internal/exec/loop.go` — adapt CapturedVariables copy
- `internal/runner/runner.go` — Options type, context creation, state writes
- `internal/runner/resume.go` — restore typed captures from state
- `internal/usersettings/settings.go` — struct-based refactor with onboarding keys
- `internal/liverun/coordinator.go` — provide UIStepHandler
- `internal/liverun/process_runner.go` — add RunScript implementation (same pattern as RunShell/RunAgent)
- `workflows/embed.go` — ListAssets, ReadAsset functions
- `cmd/agent-runner/main.go` — realProcessRunner.RunScript implementation

### New files to create

- `internal/exec/script.go` — script step executor
- `internal/exec/script_test.go`
- `internal/uistep/model.go` — Bubble Tea model for UI steps (or `internal/exec/uistep.go` if simpler)
- `internal/uistep/model_test.go`

### Test fakes to update

- `internal/runner/runner_test.go` — `mockRunner` needs RunScript method
- `internal/liverun/liverun_test.go` — `unusedRunner` needs RunScript method
- `internal/exec/shell_test.go` — adapt CapturedVariables assertions to use CapturedValue
- Any other test creating ExecutionContext with CapturedVariables

### Conventions

- Use `google/go-cmp` for structured comparisons in tests
- Keep executor implementations in `internal/exec/` to avoid circular imports
- Model types independent from engine/executor packages
- Test files next to source; local stubs, no mocking frameworks
- Format with `goimports` via `make fmt`

## Spec

### Requirement: Typed capture values

Captured variables SHALL carry one of three types:
- `string` — produced by shell steps (always), script steps with `capture_format: text` (default), and UI step `outcome_capture:` fields;
- `list<string>` — produced by script steps with `capture_format: json` whose stdout parses to a JSON array of strings;
- `map<string,string>` — produced by script steps with `capture_format: json` whose stdout parses to a JSON object of string values, and by UI steps with `inputs` and `capture:` (where keys are input ids).

The runner SHALL preserve and propagate these types through workflow state, audit logs, and resume. Existing string-only producers SHALL behave exactly as before — backward compatibility is required for shell-step capture.

#### Scenario: Shell capture remains string-typed
- **WHEN** a shell step with `capture: out` produces stdout `hello`
- **THEN** the captured value is the string `hello`; no type promotion or JSON parsing occurs

#### Scenario: Script step JSON list capture
- **WHEN** a script step with `capture: adapters` and `capture_format: json` writes `["claude","codex"]` to stdout
- **THEN** the captured value `adapters` is the typed list `["claude", "codex"]`

#### Scenario: UI step input capture
- **WHEN** a UI step with inputs `[adapter, model]` and `capture: profile` is completed by the user
- **THEN** the captured value `profile` is a typed map of `{adapter: <selected>, model: <selected>}`

### Requirement: Field-access interpolation for map captures

For map-typed captures, the runner SHALL support `{{var.field}}` interpolation that resolves to the string value at `field`. Whole-value interpolation `{{var}}` is permitted only where the consumer accepts the captured type natively (e.g., `mode: ui` `options:` accepts `list<string>`; `script_inputs:` value positions accept any of the three types). When `{{var}}` appears in a string-only context (a step `prompt`, a shell `command`, a static-string slot) and the captured value is not a string, the runner SHALL fail with a descriptive error.

#### Scenario: Field access resolves a string
- **WHEN** the captured map `profile` is `{adapter: "claude", model: "opus"}` and a prompt contains `{{profile.adapter}}`
- **THEN** the prompt is interpolated as `claude`

#### Scenario: Whole-value interpolation in list-accepting consumer
- **WHEN** the captured list `adapters` is `["claude", "codex"]` and a UI step's input declares `options: {{adapters}}`
- **THEN** the rendered single-select presents `claude` and `codex` as options

#### Scenario: Whole-value interpolation of map in string context
- **WHEN** the captured map `profile` is `{adapter: "claude"}` and a shell step's command contains `{{profile}}`
- **THEN** the runner fails with a descriptive error indicating that map captures cannot be interpolated in a string context; the error names the variable and suggests `{{profile.<field>}}` access

#### Scenario: Field access on string capture
- **WHEN** the captured value `out` is the string `hello` and a prompt contains `{{out.field}}`
- **THEN** the runner fails with a descriptive error indicating that field access requires a map-typed capture

### Requirement: Audit log serialization for typed captures

The audit log SHALL serialize captured values as follows:
- string captures SHALL appear as their existing string representation, unchanged from prior behavior;
- list-of-strings captures SHALL appear as a JSON array;
- map-of-strings captures SHALL appear as a JSON object.

Existing audit consumers reading shell-step capture entries SHALL see no change in shape.

#### Scenario: List capture serialized in audit log
- **WHEN** a script step captures the list `["claude", "codex"]` and the audit log entry for the step is written
- **THEN** the entry's captured-value field contains the JSON array `["claude","codex"]`

#### Scenario: Map capture serialized in audit log
- **WHEN** a UI step captures the map `{adapter: "claude", model: "opus"}` and the audit log entry for the step is written
- **THEN** the entry's captured-value field contains the JSON object `{"adapter":"claude","model":"opus"}`

#### Scenario: String capture serialization unchanged
- **WHEN** a shell step captures the string `tests passed` and the audit log entry for the step is written
- **THEN** the entry's captured-value field contains the existing string representation unchanged from prior behavior

### Requirement: Loop overwrite for typed captures

Inside a `loop:` step, captured variables SHALL be overwritten on each iteration the same way scalar (string) captures are today. Type SHALL persist across iterations.

#### Scenario: List capture overwritten per iteration
- **WHEN** a script step inside a `loop: { over: [a, b, c] }` body captures a list on each iteration
- **THEN** the captured variable holds only the most recent iteration's list value when read after the iteration completes

### Requirement: Stdout capture (broadened from shell-only)

A shell, script, or `mode: ui` step MAY have a `capture` field. When set, the runner SHALL capture the step's output into a named variable available to subsequent steps via `{{var_name}}` interpolation (or `{{var_name.field}}` for map-typed captures).

- **Shell steps**: capture stdout as a string (typed `string`). Output SHALL be both captured and displayed to the terminal in real time (tee behavior).
- **Script steps**: capture stdout per the rules in `workflow-bundled-scripts` — string by default; `capture_format: json` produces a typed list-of-strings or map-of-strings.
- **UI steps**: capture per the rules in `ui-step` — a map keyed by input id when `inputs` are declared; rejected when no inputs are declared.

The `capture` field SHALL fail at load time on interactive agent steps. Headless agent steps MAY use `capture` (preserving existing behavior).

#### Scenario: Capture on interactive agent step rejected
- **WHEN** an interactive agent step has a `capture` field
- **THEN** the runner fails at load time with a validation error indicating that `capture` is not valid on interactive agent steps

#### Scenario: Capture on headless agent step accepted
- **WHEN** a headless agent step has `capture: agent_output`
- **THEN** validation succeeds; the captured value is a string (stdout of the agent process)

#### Scenario: Capture on script step accepted
- **WHEN** a script step declares `capture: out`
- **THEN** validation succeeds; the captured value follows the rules in `workflow-bundled-scripts` (string by default; typed list/map with `capture_format: json`)

#### Scenario: Capture on UI step accepted when inputs are declared
- **WHEN** a UI step declares `inputs:` and `capture: profile`
- **THEN** validation succeeds; the captured value is a map keyed by input id, per `ui-step`

### Requirement: `model` field rejected on UI steps

The `model` field SHALL NOT be valid on `mode: ui` steps. Validation SHALL fail at workflow-load time when a UI step sets `model`.

#### Scenario: UI step with model field
- **WHEN** a step has `mode: ui` and sets `model: opus`
- **THEN** validation fails with an error indicating that `model` is not valid on UI steps

### Requirement: `cli` field rejected on UI steps

The `cli` field SHALL NOT be valid on `mode: ui` steps. Validation SHALL fail at workflow-load time when a UI step sets `cli`.

#### Scenario: UI step with cli field
- **WHEN** a step has `mode: ui` and sets `cli: claude`
- **THEN** validation fails with an error indicating that `cli` is not valid on UI steps

### Requirement: `model` field rejected on script steps

The `model` field SHALL NOT be valid on `script:` steps. Validation SHALL fail at workflow-load time when a script step sets `model`.

#### Scenario: Script step with model field
- **WHEN** a step declares `script: detect.sh` and sets `model: opus`
- **THEN** validation fails with an error indicating that `model` is not valid on script steps

### Requirement: `cli` field rejected on script steps

The `cli` field SHALL NOT be valid on `script:` steps. Validation SHALL fail at workflow-load time when a script step sets `cli`.

#### Scenario: Script step with cli field
- **WHEN** a step declares `script: detect.sh` and sets `cli: codex`
- **THEN** validation fails with an error indicating that `cli` is not valid on script steps

### Requirement: Script step shape

A step with a `script:` field is a script step. The `script:` value SHALL be a static file path relative to the workflow's directory (for user-authored workflows) or to the namespace's bundled-asset directory (for embedded builtin workflows).

#### Scenario: Script step recognized
- **WHEN** a step declares `script: detect-adapters.sh`
- **THEN** `StepType()` returns `"script"` and the step is handled by the script step executor

#### Scenario: Script with absolute path rejected
- **WHEN** a step declares `script: /usr/local/bin/detect.sh`
- **THEN** validation fails with an error indicating that absolute paths are not allowed

#### Scenario: Script with path traversal rejected
- **WHEN** a step declares `script: ../../etc/passwd`
- **THEN** validation fails with an error indicating that path traversal is not allowed

#### Scenario: Script path must be static
- **WHEN** a step declares `script: {{script_name}}.sh`
- **THEN** validation fails with an error indicating that interpolation is not allowed in the script field

### Requirement: Script execution mechanism

The runner SHALL execute script files directly via `exec.Command(path)` (not through `sh -c`). The script process SHALL receive JSON-encoded `script_inputs` on stdin when declared. The process SHALL NOT receive os.Stdin.

#### Scenario: Script inputs delivered as JSON on stdin
- **WHEN** a script step declares `script_inputs: {adapter: "{{chosen_cli}}"}` and `chosen_cli` captures the string `claude`
- **THEN** the script receives `{"adapter":"claude"}` on stdin

#### Scenario: No script inputs means empty stdin
- **WHEN** a script step has no `script_inputs` field
- **THEN** the script process receives an empty stdin (not the runner's os.Stdin)

### Requirement: Script capture format

A script step with `capture:` and `capture_format: json` SHALL parse stdout as JSON. A JSON array of strings produces a `list<string>` capture. A JSON object of string values produces a `map<string,string>` capture. Invalid JSON, non-string elements, stdout exceeding 1 MiB, or non-UTF-8 content SHALL fail the step with a descriptive error at runtime.

#### Scenario: JSON array capture
- **WHEN** a script with `capture_format: json` writes `["claude","codex"]` to stdout
- **THEN** the captured value is the typed list `["claude", "codex"]`

#### Scenario: JSON object capture
- **WHEN** a script with `capture_format: json` writes `{"adapter":"claude","model":"opus"}` to stdout
- **THEN** the captured value is the typed map `{adapter: "claude", model: "opus"}`

#### Scenario: Invalid JSON fails at runtime
- **WHEN** a script with `capture_format: json` writes `not valid json` to stdout
- **THEN** the step fails with a descriptive error indicating invalid JSON output

#### Scenario: Oversized stdout fails
- **WHEN** a script with `capture_format: json` writes more than 1 MiB to stdout
- **THEN** the step fails with a descriptive error indicating the output exceeded the size limit

### Requirement: UI step shape and fields

A step with `mode: ui` and no `command`, `prompt`, or `script` is a UI step. It SHALL declare a `title` (string), a `body` (string, may be empty), and one or more `actions`. It MAY also declare `inputs`.

#### Scenario: UI step with actions
- **WHEN** a step has `mode: ui`, `title: "Welcome"`, `body: "Choose an action"`, and `actions: [{label: Continue, outcome: continue}]`
- **THEN** validation succeeds and the step type is `"ui"`

#### Scenario: UI step with empty body permitted
- **WHEN** a step has `mode: ui`, `title: "Welcome"`, `body: ""`, and `actions: [{label: Continue, outcome: continue}]`
- **THEN** validation succeeds; the screen renders title and actions with no body content

#### Scenario: UI step without title rejected
- **WHEN** a step has `mode: ui` and `actions:` but no `title:`
- **THEN** validation fails with an error indicating that title is required on UI steps

### Requirement: UI step action outcomes

Each action SHALL declare a static `outcome` identifier matching `^[a-z][a-z0-9_]*$`. The selected action's outcome is stored via `outcome_capture:`.

#### Scenario: Outcome captured
- **WHEN** a UI step has `outcome_capture: user_action` and the user selects an action with `outcome: continue`
- **THEN** `user_action` is captured as the string `"continue"`

### Requirement: UI step single-select inputs

UI inputs are single-select from a declared `options` list. Options may be static strings or a `{{captured_list}}` reference resolved at runtime via `ResolveTypedValue`.

#### Scenario: Static options
- **WHEN** a UI input declares `options: [global, project]`
- **THEN** the user sees exactly those two options

#### Scenario: Options from captured list
- **WHEN** a UI input declares `options: {{adapters}}` and `adapters` is the captured list `["claude", "codex"]`
- **THEN** the user sees `claude` and `codex` as options

### Requirement: Runtime-value sanitization

All runtime-resolved interpolated values SHALL have ANSI escape sequences stripped before rendering in UI step body and option labels.

#### Scenario: ANSI stripped from interpolated body
- **WHEN** a UI step body contains `{{output}}` and the captured value includes ANSI codes `\x1b[31mred\x1b[0m`
- **THEN** the rendered body shows `red` without color codes

### Requirement: Non-interactive terminal failure

When the runner reaches a `mode: ui` step and no UIStepHandler is configured (headless mode, non-TTY), it SHALL fail with "UI steps require a TTY."

#### Scenario: Non-TTY failure
- **WHEN** a UI step is reached in headless mode (no UIStepHandler)
- **THEN** the step fails with an error indicating UI steps require a TTY

### Requirement: User cancellation

When the user presses Ctrl-C during a UI step, the step SHALL result in `OutcomeAborted` (handled per `continue_on_failure` rules).

#### Scenario: Ctrl-C aborts
- **WHEN** the user presses Ctrl-C during a UI step
- **THEN** the step outcome is aborted

### Requirement: Embedded asset accessibility

Files in a namespace subdirectory whose names do not end in `.yaml` SHALL be embedded as bundled assets and accessible at runtime via the relative paths declared by `script:` step fields.

#### Scenario: Embedded script asset accessible
- **WHEN** the embedded workflow `onboarding:setup-agent-profile` declares `script: detect-adapters.sh` and the file `onboarding/detect-adapters.sh` exists in the embedded set
- **THEN** the runner resolves and executes that bundled asset at runtime

#### Scenario: Embedded asset does not fall back to user directory
- **WHEN** the embedded workflow declares `script: detect-adapters.sh` and a file `.agent-runner/workflows/onboarding/detect-adapters.sh` also exists on the user's disk
- **THEN** the embedded asset is used; the user file is not consulted

## Done When

- All spec scenarios above are covered by tests and passing
- Existing tests still pass (typed captures are backward-compatible)
- `make test` passes; `make lint` passes
- A workflow YAML with `mode: ui` and `script:` steps can be loaded, validated, and executed in both TUI and headless modes (UI steps fail gracefully in headless)
- State serialization round-trips typed captures correctly (including legacy plain-string state files)
