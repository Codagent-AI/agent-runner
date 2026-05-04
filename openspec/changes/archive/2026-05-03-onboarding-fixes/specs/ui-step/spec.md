## ADDED Requirements

### Requirement: Inputs and actions render together as a single form

When a `mode: ui` step has both an `inputs` list and an `actions` list, the runner SHALL render them on a single screen as a unified form: the inputs SHALL be presented first, followed by the actions, with the user able to traverse focus between every input and every action using the standard navigation keys (Tab / Shift-Tab and arrow keys). The runner SHALL NOT split the inputs and actions across two separate screens (e.g., showing the inputs first and then re-rendering with only the actions).

While focus is on an input, arrow-up / arrow-down SHALL move the highlighted option within that input and SHALL NOT move focus to a different input or action. Tab and Shift-Tab SHALL move focus across input and action elements. When focus is on an action, pressing Enter SHALL fire that action; when focus is on an input, pressing Enter SHALL move focus to the next form element (input or action) without firing any action, unless the step declares a default action (in which case existing default-action behavior applies — see "Optional defaults").

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

#### Scenario: Enter on action fires the action
- **WHEN** focus is on an action and the user presses Enter
- **THEN** the step SHALL resolve with that action's outcome, regardless of which input options are highlighted

#### Scenario: Step with actions and no inputs renders unchanged
- **WHEN** a UI step declares actions but no inputs (e.g., the welcome screen)
- **THEN** the rendered screen SHALL present the actions as the only interactive elements and existing welcome-screen behavior SHALL be preserved
