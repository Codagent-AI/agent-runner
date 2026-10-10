## ADDED Requirements

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
The user SHALL navigate between fields and the Start button using Tab (forward) and Shift+Tab (backward). Navigation wraps: Tab from the Start button returns to the first field; Shift+Tab from the first field moves to the Start button. Arrow keys within a field SHALL move the text cursor. The form SHALL visually indicate which field is focused.

The form SHALL include a focusable Start button below the fields. The Start button SHALL be reachable via Tab navigation and SHALL submit the form when Enter is pressed while it is focused.

Focused field input borders SHALL render in the accent color; unfocused field borders SHALL render in the dim color. The workflow name SHALL render at the top in accent color bold, with the description below in dim text.

#### Scenario: Tab moves to next field
- **WHEN** the user presses Tab while focused on a field that is not the last
- **THEN** focus moves to the next field in order

#### Scenario: Tab from last field moves to Start button
- **WHEN** the user presses Tab while focused on the last field
- **THEN** focus moves to the Start button

#### Scenario: Tab from Start button wraps to first field
- **WHEN** the user presses Tab while the Start button is focused
- **THEN** focus wraps to the first field

#### Scenario: Shift+Tab moves to previous field
- **WHEN** the user presses Shift+Tab while focused on a field that is not the first
- **THEN** focus moves to the previous field

#### Scenario: Shift+Tab from first field moves to Start button
- **WHEN** the user presses Shift+Tab while focused on the first field
- **THEN** focus moves to the Start button

### Requirement: Form submission and validation
The form SHALL submit when the user presses Enter on the last field or presses Enter when the Start button is focused. On submit, the form SHALL validate that all required parameters have non-empty values. If validation fails, the form SHALL display error messages identifying which required fields are empty and SHALL NOT launch the run. If validation passes, the form SHALL return the parameter map and the run SHALL launch.

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

#### Scenario: Enter on Start button submits
- **WHEN** the user presses Enter while the Start button is focused
- **THEN** the form submits (validation and launch proceed)

### Requirement: Form cancellation
Pressing Escape SHALL cancel the param form and return to the previous view (the workflow definition view or the list tab) without launching a run. No parameter values are persisted.

#### Scenario: Escape cancels and returns to previous view
- **WHEN** the user presses Escape on the param form
- **THEN** the form closes without launching, and the previous view is restored

#### Scenario: Partial input discarded on cancel
- **WHEN** the user has entered values into some fields and presses Escape
- **THEN** all entered values are discarded; no run is launched
