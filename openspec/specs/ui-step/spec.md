# ui-step Specification

## Purpose
Define the `mode: ui` step primitive that renders interactive TUI screens within workflows, allowing users to make selections, confirm actions, and provide input through a structured terminal interface.
## Requirements
### Requirement: Mode "ui" step shape

A workflow step MAY declare `mode: ui` to render an interactive TUI screen. A UI step SHALL have a `title` (string), a `body` (string), and one or more `actions`. A UI step MAY have an `inputs` list. A UI step MAY have an `outcome_capture` field naming the variable that receives the selected action's outcome. UI steps SHALL NOT set `agent`, `cli`, `model`, `session`, `prompt`, or `command`.

#### Scenario: Informational step validates
- **WHEN** a step declares `mode: ui` with a non-empty title, a body, and two actions, and no inputs
- **THEN** validation succeeds and the step is accepted by the loader

#### Scenario: UI step with agent field rejected
- **WHEN** a step declares `mode: ui` and also sets `agent: planner`
- **THEN** validation fails with an error indicating that `agent` is not valid on UI steps

#### Scenario: UI step with command field rejected
- **WHEN** a step declares `mode: ui` and also sets `command: echo hi`
- **THEN** validation fails with an error indicating that `command` is not valid on UI steps

#### Scenario: UI step with zero actions rejected
- **WHEN** a step declares `mode: ui` with no actions list (or an empty list)
- **THEN** validation fails with an error indicating that at least one action is required

### Requirement: Markdown body rendering

The `body` of a UI step SHALL be rendered as markdown using the runner's existing TUI styling stack at execution time, after `{{...}}` interpolation has resolved any references. An empty body SHALL be allowed; the screen then renders only the title and actions.

#### Scenario: Markdown formatting renders via TUI styling
- **WHEN** a UI step's body contains markdown headings, bulleted lists, and a fenced code block
- **THEN** the rendered screen displays the body with TUI styling applied to those constructs

#### Scenario: Interpolation resolves before rendering
- **WHEN** a UI step's body contains `{{profile.adapter}}` and a prior step has captured `profile` as a map containing `adapter: claude`
- **THEN** the rendered body displays `claude` in place of the interpolation expression

#### Scenario: Empty body is permitted
- **WHEN** a UI step declares an empty body
- **THEN** validation succeeds and the rendered screen contains the title and actions with no body content

### Requirement: Action outcomes

Each action SHALL declare a `label` (display text) and an `outcome` (string identifier). Outcomes SHALL match the regex `^[a-z][a-z0-9_]*$` (no whitespace, no shell metacharacters, no interpolation). Outcomes SHALL be unique within a single step. When the user selects an action, the runner SHALL record that action's outcome.

When the UI step declares `outcome_capture: <name>`, the recorded outcome SHALL be exposed as the captured variable `<name>` (a string), available to subsequent steps via `{{<name>}}` interpolation. Subsequent steps' `skip_if` / `break_if` use this through the existing shell-expression form, e.g. `skip_if: 'sh: [ "{{user_action}}" = "skip" ]'`.

#### Scenario: Selected action's outcome is recorded
- **WHEN** the user selects an action whose outcome is `dismiss` and the step declares `outcome_capture: user_action`
- **THEN** the captured variable `user_action` is the string `dismiss`

#### Scenario: Subsequent skip_if reads the outcome
- **WHEN** a UI step captures outcome into `user_action`, and the next step declares `skip_if: 'sh: [ "{{user_action}}" = "skip" ]'`
- **THEN** the subsequent step is skipped exactly when the user selected the action whose outcome is `skip`

#### Scenario: Duplicate outcomes within a step rejected
- **WHEN** a UI step declares two actions whose `outcome` values are identical (e.g., both `continue`)
- **THEN** validation fails with an error identifying the duplicated outcome

#### Scenario: Outcome value is interpolated
- **WHEN** an action declares `outcome: '{{some_var}}'`
- **THEN** validation fails with an error indicating that outcomes must be static identifiers

#### Scenario: Outcome value contains invalid characters
- **WHEN** an action declares `outcome: "skip onboarding"` (whitespace) or `outcome: "skip;rm"` (shell metacharacter)
- **THEN** validation fails with an error indicating that outcomes must match `^[a-z][a-z0-9_]*$`

### Requirement: Single-select input fields

A UI step's `inputs` list MAY contain entries with `kind: single_select`. Each input SHALL have an `id` (unique within the step), a `prompt`, and an `options` value. Input ids SHALL match the regex `^[a-z][a-z0-9_]*$`. The `options` value SHALL be either a YAML list of strings (static) or a `{{...}}` interpolation expression that resolves at execution time to a list of strings (dynamic). Input kinds other than `single_select` SHALL be rejected by validation in this version.

#### Scenario: Static option list renders selectable items
- **WHEN** an input declares `kind: single_select` and `options: [claude, codex]`
- **THEN** the rendered screen presents `claude` and `codex` as selectable items for that input

#### Scenario: Interpolated options resolve to a list at runtime
- **WHEN** an input's `options` value is `{{detected_adapters}}` and a prior step captured `detected_adapters` as the list `[claude, codex]`
- **THEN** the rendered screen presents `claude` and `codex` as selectable items

#### Scenario: Input without id rejected
- **WHEN** a UI step declares an input with `kind: single_select` but no `id`
- **THEN** validation fails with an error indicating that input `id` is required

#### Scenario: Duplicate input ids rejected
- **WHEN** a UI step declares two inputs both with `id: adapter`
- **THEN** validation fails with an error identifying the duplicated id

#### Scenario: Input id with invalid characters
- **WHEN** an input declares `id: "Adapter Choice"` (whitespace, capital)
- **THEN** validation fails with an error indicating that ids must match `^[a-z][a-z0-9_]*$`

#### Scenario: Unsupported input kind rejected
- **WHEN** a UI step declares an input with `kind: text`
- **THEN** validation fails with an error indicating that only `single_select` inputs are supported

#### Scenario: Interpolated options resolve to non-list value
- **WHEN** an input's `options` interpolation resolves at runtime to a string, an object, or a list whose elements are not all strings
- **THEN** the step fails with an error indicating that single-select options must resolve to a list of strings

#### Scenario: Resolved options list is empty
- **WHEN** an input's `options` (whether static or interpolated) resolves to an empty list at execution time
- **THEN** the step fails with an error indicating that the input has no available options

### Requirement: Captured input values

A UI step with `inputs` MAY declare `capture: <name>`. When set, the runner SHALL produce a map (typed capture, see `output-capture`) whose keys are each input's `id` and whose values are the user-selected option strings. A `capture:` field SHALL be rejected on UI steps that have no `inputs`. UI steps with `inputs` and no `capture:` SHALL be valid; the user-selected values are not exposed to subsequent steps. `capture:` and `outcome_capture:` are independent fields and MAY both be set on the same step; they SHALL NOT name the same variable.

#### Scenario: Capture map includes one key per input id
- **WHEN** a UI step has inputs with ids `adapter` and `model`, declares `capture: profile`, and the user selects `claude` for adapter and `opus` for model
- **THEN** the captured value `profile` is the map `{adapter: "claude", model: "opus"}`

#### Scenario: Capture without inputs rejected
- **WHEN** a UI step has no `inputs` and sets `capture: foo`
- **THEN** validation fails with an error indicating that `capture` requires `inputs`

#### Scenario: Inputs without capture is valid
- **WHEN** a UI step has inputs but does not declare `capture:`
- **THEN** validation succeeds and the user's selections are not exposed to subsequent steps

#### Scenario: capture and outcome_capture name collision
- **WHEN** a UI step declares both `capture: foo` and `outcome_capture: foo`
- **THEN** validation fails with an error indicating that the two fields must name distinct variables

### Requirement: Optional defaults

A UI step MAY declare a default action that fires when the user presses Enter. Each input MAY declare a default selected option. Both are optional. When no default action is declared, pressing Enter SHALL NOT advance the step; the user must explicitly select an action through keyboard navigation. Validation SHALL reject a default that does not match a real action or static option.

#### Scenario: Default action fires on Enter
- **WHEN** a UI step declares its `continue` action as default and the user presses Enter without further navigation
- **THEN** the step resolves with outcome `continue`

#### Scenario: No default action, Enter does nothing
- **WHEN** a UI step declares no default action and the user presses Enter without focusing an action
- **THEN** the step does not advance and continues to await action selection

#### Scenario: Default input option pre-selected
- **WHEN** a single-select input declares `default: claude` with static `options: [claude, codex]`
- **THEN** `claude` is the initially highlighted option for that input

#### Scenario: Default action does not match any declared outcome
- **WHEN** a UI step's default action references an outcome string that no action in the step declares
- **THEN** validation fails with an error identifying the dangling default-action reference

#### Scenario: Default input value not in static options
- **WHEN** a single-select input declares `default: opus` with static `options: [claude, codex]`
- **THEN** validation fails with an error indicating the default is not among the declared options

#### Scenario: Default input value not validated for dynamic options
- **WHEN** a single-select input declares `default: claude` and `options: {{detected_adapters}}`
- **THEN** validation succeeds; if the runtime-resolved option list does not contain `claude`, the step fails at execution time with an error indicating the default is not among the resolved options

### Requirement: Runtime-value sanitization before rendering

Before rendering, the runner SHALL strip ANSI escape sequences and C0/C1 control characters from any value that originated from runtime context — interpolated `{{...}}` expansions in title, body, action labels, input prompts, dynamic option labels, and any text echoed from script-step stdout. Static YAML-authored content (literal title, literal body, literal action labels) SHALL be rendered as-is. Hostile workflow authorship is out of scope for this change.

#### Scenario: Captured value with ANSI codes is neutralized in body
- **WHEN** a prior step captures the string `"claude\x1b[2J"` into `chosen` and a UI step's body contains `Chosen: {{chosen}}`
- **THEN** the rendered screen displays `Chosen: claude` with the screen-clear sequence stripped

#### Scenario: Dynamic option label with control characters is neutralized
- **WHEN** an input's interpolated options list contains a string with embedded `\r` or `\x1b[31m`
- **THEN** the rendered selectable label displays the printable characters only, with control sequences removed

#### Scenario: Static body markdown renders as-is
- **WHEN** a UI step's body is a literal markdown string with no interpolation
- **THEN** the body renders exactly as authored, including any intentional formatting

### Requirement: Non-interactive terminal failure

When `mode: ui` executes in an environment where stdin or stdout is not a TTY, the step SHALL fail with an error indicating UI steps require an interactive terminal. The runner SHALL NOT render the screen or auto-skip the step.

#### Scenario: Non-TTY stdin
- **WHEN** stdin is redirected from a file or pipe at the time a UI step would execute
- **THEN** the step fails with an error indicating UI steps require an interactive terminal

#### Scenario: Non-TTY stdout
- **WHEN** stdout is redirected to a file or pipe at the time a UI step would execute
- **THEN** the step fails with an error indicating UI steps require an interactive terminal

### Requirement: User cancellation

If the user cancels the screen (e.g., by pressing Ctrl-C or otherwise terminating the input loop) before selecting an action, the step SHALL fail with a cancellation error. The runner SHALL NOT silently skip the step or treat cancellation as any specific named outcome.

#### Scenario: Ctrl-C during UI step
- **WHEN** the user presses Ctrl-C while a UI step is awaiting action selection
- **THEN** the step fails with a cancellation error and the workflow handles the failure per its existing error-handling rules (e.g., `continue_on_failure`)

### Requirement: Loop nesting

`mode: ui` steps MAY appear inside a `loop:` step's body. Each iteration SHALL render the screen with iteration-variable interpolation applied to title, body, action labels, and input options. Per-iteration captures follow the existing loop-capture semantics for non-UI steps.

#### Scenario: UI step renders once per loop iteration
- **WHEN** a `loop:` step iterates over `[a, b, c]` and its body contains a UI step whose title is `Item {{item}}`
- **THEN** the screen renders three times, with title `Item a`, then `Item b`, then `Item c`

#### Scenario: Iteration variable available to action labels
- **WHEN** an action's label is `Skip {{item}}` inside a loop body
- **THEN** the action's rendered label uses the current iteration's `item` value

### Requirement: UI inputs are not for secrets

UI step input values are persisted in workflow state and recorded in audit logs. This version SHALL NOT be used to collect credentials or secret values; redaction is out of scope. Workflow authors SHALL design `mode: ui` flows so that inputs collect non-sensitive information (adapter names, model names, scope choices, etc.).

#### Scenario: Captured input visible in audit log
- **WHEN** a UI step captures `{adapter: claude}` and the workflow run is later inspected
- **THEN** the audit log entry for that step contains the captured value as ordinary structured data with no redaction

### Requirement: Inputs and actions render together as a single form

When a `mode: ui` step has both an `inputs` list and an `actions` list, the runner SHALL render them on a single screen as a unified form: the inputs SHALL be presented first, followed by the actions, with the user able to traverse focus between every input and every action using the standard navigation keys (Tab / Shift-Tab and arrow keys). The runner SHALL NOT split the inputs and actions across two separate screens (e.g., showing the inputs first and then re-rendering with only the actions).

Actions SHALL render together on a single horizontal row. While focus is on an input, arrow-up / arrow-down SHALL move the highlighted option within that input and SHALL NOT move focus to a different input or action. Tab and Shift-Tab SHALL move focus across input and action elements. While focus is on an action, arrow-left / arrow-right SHALL move focus between actions on the row. When focus is on an action, pressing Enter SHALL fire that action; when focus is on an input, pressing Enter SHALL move focus to the next form element (input or action) without firing any action, unless the step declares a default action (in which case existing default-action behavior applies — see "Optional defaults").

A UI step with `actions` and no `inputs` SHALL render its actions as the only interactive elements on the screen. A UI step with `inputs` and no `actions` SHALL fail validation per the existing "Mode `ui` step shape" requirement (one or more actions are required).

#### Scenario: Adapter-selection screen renders input and Continue together
- **WHEN** a UI step declares one `single_select` input with `id: cli` and one action `Continue` (`outcome: continue`)
- **THEN** the rendered screen SHALL show the input prompt with selectable options *and* the Continue button on the same screen, simultaneously

#### Scenario: Arrow keys within an input do not break focus
- **WHEN** focus is on a single-select input and the user presses arrow-up or arrow-down
- **THEN** the highlighted option within that input SHALL change and the screen SHALL remain interactive (i.e., focus is not lost; pressing Enter or Tab afterwards SHALL behave per their normal semantics)

#### Scenario: Tab moves focus between input and action
- **WHEN** focus is on a single-select input and the user presses Tab
- **THEN** focus SHALL move to the next form element (the action button if no further inputs exist), and the action button SHALL render in its focused state

#### Scenario: Actions render horizontally and arrow keys move focus
- **WHEN** a UI step declares actions `Continue`, `Not now`, and `Dismiss`
- **THEN** those actions SHALL render on one row, and pressing arrow-right from `Continue` SHALL focus `Not now`

#### Scenario: Enter on action fires the action
- **WHEN** focus is on an action and the user presses Enter
- **THEN** the step SHALL resolve with that action's outcome, regardless of which input options are highlighted

#### Scenario: Step with actions and no inputs renders unchanged
- **WHEN** a UI step declares actions but no inputs (e.g., the welcome screen)
- **THEN** the rendered screen SHALL present the actions as the only interactive elements and existing welcome-screen behavior SHALL be preserved

### Requirement: Live run-view navigation remains available during UI steps

When a `mode: ui` step is rendered inside the live run view, the UI step SHALL NOT behave as a modal that blocks the run view's existing step navigation. The user SHALL be able to move the selected step away from the active UI step while the UI step remains pending. When the selected step is not the active UI step, the run view SHALL render normal details for the selected step and route navigation keys through the run-view key handler.

When the selected step is the active UI step, keys that operate the UI form itself, including Tab/Shift-Tab, arrow-left/arrow-right, Enter, and Esc, SHALL continue to be handled by the UI step. The live run view SHALL provide `d` ("drill down") as a separate shortcut to drill into a selected loop, sub-workflow, or iteration while the UI step is active. The live run view SHALL use the existing run-view scroll shortcuts, `j` and `k`, for UI content that exceeds the detail pane height. The run-view quit shortcut `q` SHALL remain available while a UI step is active. Live UI single-select inputs SHALL use arrow-left/arrow-right for option navigation because arrow-up/arrow-down are owned by step navigation. Standalone UI-step rendering outside the live run view MAY keep using arrow-up and arrow-down for single-select inputs.

#### Scenario: Step navigation leaves UI step pending
- **WHEN** the live run view is displaying a pending UI step and another step is selectable in the current step list
- **AND** the user navigates to another step
- **THEN** the selected run-view step changes and the UI step remains pending

#### Scenario: Run-view navigation works away from active UI step
- **WHEN** the live run view has a pending UI step
- **AND** the user has navigated selection away from the active UI step
- **THEN** run-view navigation keys operate on the selected step rather than being captured by the UI step

#### Scenario: Drill shortcut opens selected container during active UI step
- **WHEN** the live run view selection is on a loop, sub-workflow, or iteration containing the active UI step
- **AND** the user presses `d`
- **THEN** the run view drills into the selected container and the UI step remains pending

#### Scenario: Existing run-view scroll keys scroll overflowing UI content
- **WHEN** the live run view selection is on a pending UI step whose content exceeds the detail pane height
- **AND** the user presses `j` or `k`
- **THEN** the visible UI content scrolls without changing the selected step or resolving the UI step

#### Scenario: Quit shortcut remains available during active UI step
- **WHEN** the live run view selection is on a pending UI step
- **AND** the user presses `q`
- **THEN** the run view starts its normal quit flow rather than routing `q` to the UI step

#### Scenario: UI action keys still resolve the active UI step
- **WHEN** the live run view selection is on a pending UI step with actions
- **AND** the user presses arrow-left/arrow-right and Enter
- **THEN** the UI step moves action focus and returns the selected outcome
