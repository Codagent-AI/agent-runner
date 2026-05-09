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

### Requirement: Smooth scroll-up transitions between screens

When the user advances from one screen to the next, the native setup TUI SHALL animate the transition using harmonica spring physics. The outgoing screen SHALL scroll upward and out of view while the incoming screen scrolls upward into the centered position from below. The animation SHALL complete within approximately 200-300ms.

The animation SHALL apply to all native setup screens including the demo prompt screen.

#### Scenario: Advancing to next screen animates
- **WHEN** the user selects an option and the TUI advances to the next screen
- **THEN** the previous screen scrolls up and out while the new screen scrolls up into position with spring easing

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

#### Scenario: Scope selection explains options
- **WHEN** native setup renders the scope selection screen
- **THEN** the screen explains the difference between global and project scope

#### Scenario: Demo prompt explains the demo
- **WHEN** native setup renders the demo prompt screen
- **THEN** the screen explains what the onboarding demo contains and what the user will experience
