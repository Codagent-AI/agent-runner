## ADDED Requirements

### Requirement: UI steps render inside the live-run-view chrome

When the active step in the active workflow run is `mode: ui`, the live-run-view SHALL render that step's title, body, inputs, and actions inside its own chrome — the same chrome used for any other step. The chrome SHALL include, at minimum:

- the workflow name and breadcrumb header at the top;
- the step list (sidebar) with the active step highlighted;
- a content area in which the UI step renders.

The runner SHALL NOT render the UI step as a standalone full-screen overlay that hides the chrome. UI step content SHALL be bounded by the chrome's content-area width and height; the existing TUI styling stack's word-wrap and truncation rules apply within that area.

While the UI step is awaiting input, the chrome's status indicators (active-step glyph, breadcrumb, sidebar) SHALL reflect the current state — the active step appears as the in-progress step in the sidebar, and prior steps render with their final status.

The existing top-level keybindings of the live-run-view (`q`, `Ctrl+C`, Escape) continue to apply outside the UI step's input area; keystrokes routed to the UI step (selection arrows, Tab, Enter on a focused element, etc.) are consumed by the UI step and do not trigger top-level chrome actions.

#### Scenario: Workflow name and sidebar visible during a UI step
- **WHEN** the active step is `mode: ui` and the live-run-view is on screen
- **THEN** the rendered view SHALL include the workflow name in a breadcrumb header and the step list in a sidebar, in addition to the UI step's content

#### Scenario: UI step body wraps within the content area
- **WHEN** a UI step's body text is longer than the chrome's content-area width
- **THEN** the body SHALL wrap within that width rather than being truncated or extending past the chrome

#### Scenario: Sidebar reflects the active UI step
- **WHEN** the active step is a `mode: ui` step partway through a workflow
- **THEN** the sidebar SHALL highlight that step as the active step and SHALL show prior steps with their final status

#### Scenario: UI step input does not trigger chrome quit
- **WHEN** focus is on a UI step's input or action and the user presses Enter, arrow, or Tab keys
- **THEN** those keystrokes SHALL be consumed by the UI step and SHALL NOT trigger the live-run-view's top-level quit confirmation
