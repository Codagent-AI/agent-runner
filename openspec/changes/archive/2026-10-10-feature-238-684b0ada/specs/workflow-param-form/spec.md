## ADDED Requirements

### Requirement: Parameter form display
When the user starts a run from the New tab or the workflow definition view for a workflow that declares one or more parameters, the system SHALL present a parameter form before launching. The form SHALL show the workflow's canonical name at the top in the accent color and bold, followed by its description in dim text when one is present. It SHALL display one labeled single-line text input per declared parameter, in the order the parameters are declared in the workflow YAML, and each label SHALL be the parameter name. Required parameters, including parameters whose `required` field is omitted, SHALL be marked with a `*`. A parameter with a `default` value SHALL have its input pre-populated with that default. The form SHALL end with a focusable Start button and a help line describing `tab/shift+tab navigate`, `enter start`, and `esc cancel`. The first input SHALL have focus when the form opens.

A workflow that declares no parameters SHALL NOT show the form; its run SHALL launch immediately.

#### Scenario: Required param shown with marker
- **WHEN** the param form opens for a workflow with a required parameter `task_file`
- **THEN** the form displays a text input labeled `task_file` marked with `*`

#### Scenario: Param without required field is treated as required
- **WHEN** the param form opens for a workflow whose parameter `target` omits the `required` field
- **THEN** the `target` input is marked with `*` and is validated as required

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
The user SHALL move focus between the inputs and the Start button with Tab (forward) and Shift+Tab (backward). Navigation SHALL wrap: Tab from the last input moves to the Start button and Tab from the Start button returns to the first input; Shift+Tab from the first input moves to the Start button and Shift+Tab from the Start button moves to the last input. Left and right arrow keys within an input SHALL move its text cursor. The focused input SHALL be indicated by a `▶` marker, an accent-colored bold label, and an accent-colored underline; unfocused inputs SHALL use a dim underline. The Start button SHALL render highlighted when focused.

#### Scenario: Tab moves to next field
- **WHEN** the user presses Tab while focused on an input that is not the last
- **THEN** focus moves to the next input in order

#### Scenario: Tab from last field moves to Start button
- **WHEN** the user presses Tab while focused on the last input
- **THEN** focus moves to the Start button

#### Scenario: Tab from Start button wraps to first field
- **WHEN** the user presses Tab while the Start button is focused
- **THEN** focus wraps to the first input

#### Scenario: Shift+Tab moves to previous field
- **WHEN** the user presses Shift+Tab while focused on an input that is not the first
- **THEN** focus moves to the previous input

#### Scenario: Shift+Tab from first field moves to Start button
- **WHEN** the user presses Shift+Tab while focused on the first input
- **THEN** focus moves to the Start button

### Requirement: Form submission and validation
The form SHALL submit when the user presses Enter on the last input or presses Enter while the Start button is focused. Enter on any other input SHALL NOT submit. On submit, the form SHALL validate that every required parameter has a value that is not empty or whitespace-only. If validation fails, the form SHALL show `<name> is required` beneath each failing input and SHALL NOT launch the run; editing an input SHALL clear its error. If validation passes, the run SHALL launch by replacing the TUI process with `agent-runner <canonical-name>` followed by one `<name>=<value>` argument per declared parameter in declaration order, which opens the live run view for the new run.

#### Scenario: Submit with all required params filled
- **WHEN** all required parameter inputs have non-blank values and the user submits
- **THEN** the run launches with the entered parameter values

#### Scenario: Submit with missing required param
- **WHEN** a required parameter input is empty or contains only whitespace and the user submits
- **THEN** the form shows `<name> is required` beneath that input and does not launch

#### Scenario: Submit with optional param left empty
- **WHEN** an optional parameter input without a default is left empty and the user submits
- **THEN** validation passes and the parameter is passed as an empty string

#### Scenario: Default value accepted without editing
- **WHEN** a parameter with a default is not edited by the user and the user submits
- **THEN** the default value is used for that parameter

#### Scenario: Enter on last field submits
- **WHEN** the user presses Enter while focused on the last input
- **THEN** the form submits and validation and launch proceed

#### Scenario: Enter on Start button submits
- **WHEN** the user presses Enter while the Start button is focused
- **THEN** the form submits and validation and launch proceed

#### Scenario: Enter on an earlier field does not submit
- **WHEN** the user presses Enter while focused on an input that is not the last
- **THEN** the form neither submits nor moves focus

### Requirement: Form cancellation
Pressing Escape SHALL close the param form without launching a run and return to the view it was opened from: the New tab list or the workflow definition view, with that view's prior state intact. Entered values SHALL NOT be persisted.

#### Scenario: Escape cancels and returns to previous view
- **WHEN** the user presses Escape on a param form opened from the New tab
- **THEN** the form closes without launching and the New tab is shown with its prior cursor and filter

#### Scenario: Escape returns to definition view
- **WHEN** the user presses Escape on a param form opened from a workflow definition view
- **THEN** the form closes without launching and the definition view is shown again

#### Scenario: Partial input discarded on cancel
- **WHEN** the user has entered values into some inputs and presses Escape
- **THEN** all entered values are discarded and no run is launched; reopening the form shows only declared defaults
