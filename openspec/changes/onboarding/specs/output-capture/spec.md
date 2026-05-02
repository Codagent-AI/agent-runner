## MODIFIED Requirements

### Requirement: Stdout capture (broadened from shell-only)

A shell, script, or `mode: ui` step MAY have a `capture` field. When set, the runner SHALL capture the step's output into a named variable available to subsequent steps via `{{var_name}}` interpolation (or `{{var_name.field}}` for map-typed captures).

- **Shell steps**: capture stdout as a string (typed `string`). Output SHALL be both captured and displayed to the terminal in real time (tee behavior).
- **Script steps**: capture stdout per the rules in `workflow-bundled-scripts` — string by default; `capture_format: json` produces a typed list-of-strings or map-of-strings.
- **UI steps**: capture per the rules in `ui-step` — a map keyed by input id when `inputs` are declared; rejected when no inputs are declared.

The `capture` field SHALL fail at load time on agent steps (interactive or headless).

#### Scenario: Shell capture stores stdout as string
- **WHEN** a shell step has `capture: validator_output` and produces stdout `tests passed`
- **THEN** the captured value `validator_output` is the string `tests passed` and is available via `{{validator_output}}` in subsequent steps

#### Scenario: Tee behavior on shell capture
- **WHEN** a shell step has `capture: validator_output`
- **THEN** stdout is displayed to the terminal in real time AND captured into the variable

#### Scenario: Captured variable used in subsequent step prompt
- **WHEN** a step's prompt contains `{{validator_output}}` and a prior shell step captured into `validator_output`
- **THEN** the runner interpolates the captured value into the prompt

#### Scenario: Captured variable not set
- **WHEN** a step references `{{validator_output}}` but no prior step captured into that variable
- **THEN** the runner fails with a descriptive error naming the undefined variable

#### Scenario: Capture on agent step rejected
- **WHEN** an agent step (interactive or headless) has a `capture` field
- **THEN** the runner fails at load time with a validation error indicating that `capture` is not valid on agent steps

#### Scenario: Capture on script step accepted
- **WHEN** a script step declares `capture: out`
- **THEN** validation succeeds; the captured value follows the rules in `workflow-bundled-scripts` (string by default; typed list/map with `capture_format: json`)

#### Scenario: Capture on UI step accepted when inputs are declared
- **WHEN** a UI step declares `inputs:` and `capture: profile`
- **THEN** validation succeeds; the captured value is a map keyed by input id, per `ui-step`

## ADDED Requirements

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

Inside a `loop:` step, captured variables SHALL be overwritten on each iteration the same way scalar (string) captures are today. Type SHALL persist across iterations (i.e., a loop body that captures a list on every iteration produces a list capture in each iteration; the runner does not implicitly accumulate across iterations).

#### Scenario: List capture overwritten per iteration
- **WHEN** a script step inside a `loop: { over: [a, b, c] }` body captures a list on each iteration
- **THEN** the captured variable holds only the most recent iteration's list value when read after the iteration completes

#### Scenario: Map capture overwritten per iteration
- **WHEN** a UI step inside a loop body captures a map on each iteration
- **THEN** the captured variable holds only the most recent iteration's map value
