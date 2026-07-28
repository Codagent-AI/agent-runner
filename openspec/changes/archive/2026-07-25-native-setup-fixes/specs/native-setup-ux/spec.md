## ADDED Requirements

### Requirement: Centered layout with graceful fallback

The native setup TUI SHALL center its content both horizontally and vertically within the terminal. When the terminal dimensions are below a minimum threshold (height < 24 or width < 80), the TUI SHALL fall back to top-left aligned rendering to avoid clipping or pushing content off-screen.

#### Scenario: Large terminal centers content
- **WHEN** the terminal is 120x40 and native setup renders a selection screen
- **THEN** the content is centered horizontally and vertically within the terminal

#### Scenario: Small terminal uses top-left alignment
- **WHEN** the terminal is 60x18 and native setup renders a selection screen
- **THEN** the content is rendered starting from the top-left without centering

#### Scenario: Terminal resize updates layout
- **WHEN** the user resizes the terminal during native setup
- **THEN** the layout recalculates centering or fallback based on the new dimensions

#### Scenario: Copy wraps within a readable panel
- **WHEN** native setup renders explanatory copy in a wide terminal
- **THEN** the copy is constrained to a readable panel width and wraps without orphaning short words on their own line when avoidable

### Requirement: Smooth scroll-up transitions between screens

When the user advances from one screen to the next, the native setup TUI SHALL animate the transition using Bubble Tea tick messages. The outgoing screen SHALL scroll upward and out of view while the incoming screen scrolls upward into the centered position from below. The animation SHALL complete within approximately 200-300ms.

The animation SHALL apply to all native setup screens including the demo prompt screen.

#### Scenario: Advancing to next screen animates
- **WHEN** the user selects an option and the TUI advances to the next screen
- **THEN** the previous screen scrolls up and out while the new screen scrolls up into position

#### Scenario: Animation completes promptly
- **WHEN** a screen transition animation begins
- **THEN** the animation completes within approximately 300ms

#### Scenario: Cancel/failure does not animate
- **WHEN** the user presses Escape or an error occurs
- **THEN** the TUI exits without a transition animation

### Requirement: Explanatory copy on each screen

Every selection screen in native setup SHALL include a title and a short explanatory paragraph (2-4 sentences) that describes what is being asked and why it matters. The tone SHALL be friendly and informative — not terse, not verbose.

The first screen (planner CLI selection) SHALL include a brief welcoming sentence acknowledging this is initial setup. Subsequent screens SHALL explain what the selection controls and how it affects Agent Runner behavior.

The demo prompt screen SHALL explain what the onboarding demo is and what the user will see if they continue.

#### Scenario: First screen includes welcome language
- **WHEN** native setup renders the planner CLI selection screen
- **THEN** the screen includes a welcoming sentence and an explanation of what the planner agent is used for

#### Scenario: Model selection explains purpose
- **WHEN** native setup renders a model selection screen
- **THEN** the screen explains that this model will be used for the corresponding agent type and what that agent type does

#### Scenario: Default-model screen explains fallback
- **WHEN** native setup renders a default-model screen after empty model discovery
- **THEN** the screen explains that Agent Runner will use the CLI default and leave the model field unset

#### Scenario: Scope selection explains options
- **WHEN** native setup renders the scope selection screen
- **THEN** the screen explains the difference between global and project scope

#### Scenario: Demo prompt explains the demo
- **WHEN** native setup renders the demo prompt screen
- **THEN** the screen explains what the onboarding demo contains and what the user will experience

### Requirement: Demo prompt uses button actions

The demo prompt screen SHALL render Continue, Not now, and Dismiss as horizontal buttons instead of vertical list options. The row SHALL align the first button toward the left edge of the panel and the last button toward the right edge when space allows. Left and Right keys SHALL move the focused button.

#### Scenario: Demo actions render as buttons
- **WHEN** native setup renders the demo prompt screen
- **THEN** Continue, Not now, and Dismiss are shown as horizontal buttons
- **AND** they are not shown as the standard vertical option list

#### Scenario: Demo buttons use left-right navigation
- **WHEN** the demo prompt screen is focused
- **AND** the user presses Left or Right
- **THEN** the focused button changes horizontally

### Requirement: Native setup shows wizard progress

Native setup SHALL show a compact wizard-style step indicator inside the setup panel. The indicator SHALL be centered above the screen heading, and SHALL include text in the form `Step N of X` plus a visual progress bar. Model loading and default-model fallback screens SHALL count as the same model-selection step. The overwrite confirmation SHALL add an extra step only when it is shown. Demo-prompt-only re-show mode SHALL NOT show native setup progress.

#### Scenario: First setup screen shows progress
- **WHEN** native setup renders the planner CLI screen
- **THEN** the panel shows `Step 1 of 6`
- **AND** the progress indicator is centered above the screen heading

#### Scenario: Default model fallback preserves wizard step
- **WHEN** native setup renders a default-model fallback screen
- **THEN** the panel shows the same step number as the corresponding model-selection screen

#### Scenario: Overwrite confirmation adds a step
- **WHEN** native setup shows the overwrite confirmation screen
- **THEN** the panel shows `Step 6 of 7`
- **AND** the later demo prompt shows `Step 7 of 7`
