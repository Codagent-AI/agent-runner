# native-setup-ux Specification

## Purpose
Define the native first-run setup experience, including responsive TUI behavior, role configuration, plugin installation, deferred persistence, and completion flows.
## Requirements
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

Every actionable selection screen in native setup SHALL include a title and a short explanatory paragraph (2-4 sentences) that describes what is being asked and why it matters. The tone SHALL be friendly and informative — not terse, not verbose. The non-actionable recommendation loading state SHALL include concise status copy explaining that Agent Runner is inspecting available CLIs and models.

The recommendation experience SHALL include a brief welcoming sentence acknowledging this is initial setup. Its summary SHALL explain that the four selections configure interactive leadership, independent planning crosscheck, implementation, and acceptance testing. Crosscheck copy SHALL explain that it challenges Lead's planning artifacts and looks for omissions, while Agent Validator owns implementation-code review. Each role-customization screen SHALL explain what that role does and what its CLI or model selection controls.

A default-model screen SHALL explain that Agent Runner will use the CLI default and leave the model field unset. The autonomous-backend screen SHALL explain the runtime behavior of the headless, interactive, and interactive-Claude choices, SHALL identify headless as the recommendation, and SHALL NOT make claims about provider billing policy. The scope screen SHALL explain the difference between global and project scope. The demo prompt screen SHALL explain what the onboarding demo is and what the user will see if they continue.

#### Scenario: Recommendation experience includes welcome language
- **WHEN** native setup renders recommendation loading or the recommendation summary
- **THEN** the experience includes a welcoming sentence for initial setup
- **AND** it explains that Agent Runner is preparing or presenting a four-role profile

#### Scenario: Role customization explains purpose
- **WHEN** native setup renders a CLI or model selection screen for a role
- **THEN** the screen explains what the corresponding role does
- **AND** it explains what the current selection controls

#### Scenario: Crosscheck is distinguished from code review
- **WHEN** native setup presents the recommendation summary or Crosscheck customization
- **THEN** the copy describes Crosscheck as an independent challenge to Lead's planning artifacts and completeness
- **AND** identifies Agent Validator as the implementation-code review system

#### Scenario: Default-model screen explains fallback
- **WHEN** native setup renders a default-model screen after empty or failed model discovery
- **THEN** the screen explains that Agent Runner will use the CLI default and leave the model field unset

#### Scenario: Backend screen explains behavior without billing claims
- **WHEN** native setup renders the autonomous-backend selection screen
- **THEN** the screen explains the runtime behavior of headless, interactive, and interactive-Claude execution
- **AND** it identifies headless as the recommended choice
- **AND** it does not claim that a choice avoids or incurs provider billing

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

Native setup SHALL show a compact wizard-style step indicator inside the setup panel on actionable setup screens. The indicator SHALL be centered above the screen heading and SHALL include text in the form `Step N of X` plus a visual progress bar. Recommendation loading SHALL NOT show a wizard step indicator, and the recommendation summary SHALL be the first actionable step.

When the user accepts all recommendations, the wizard total SHALL omit the four role-customization steps. When the user chooses customization, the wizard total SHALL include one semantic step for each role in lead, crosscheck, implementor, then tester order. A role's CLI selection, model selection, and default-model fallback SHALL all show the same step number. The total SHALL reflect the selected branch and SHALL include an overwrite-confirmation step only when that confirmation is shown. Demo-prompt-only re-show mode SHALL NOT show native setup progress.

#### Scenario: Loading state omits progress
- **WHEN** native setup is loading recommendations
- **THEN** the panel does not show a `Step N of X` indicator

#### Scenario: Recommendation summary starts progress
- **WHEN** native setup renders the actionable recommendation summary
- **THEN** the panel shows `Step 1 of X`
- **AND** the progress indicator is centered above the screen heading

#### Scenario: Accept-all omits customization steps
- **WHEN** the user accepts all four recommendations
- **THEN** the progress total omits lead, crosscheck, implementor, and tester customization steps
- **AND** the next actionable screen advances from the recommendation-summary step

#### Scenario: Customize adds one step per role
- **WHEN** the user chooses to customize the recommendations
- **THEN** the progress total includes one step each for lead, crosscheck, implementor, and tester
- **AND** those role steps occur in that order

#### Scenario: Role sub-states preserve wizard step
- **WHEN** native setup moves among CLI selection, model selection, or default-model fallback for one role
- **THEN** the panel shows the same step number for all of those states

#### Scenario: Overwrite confirmation conditionally adds a step
- **WHEN** native setup shows the overwrite confirmation screen
- **THEN** the progress total includes that confirmation as one additional step
- **AND** later screens retain the adjusted total

#### Scenario: Demo-only mode omits progress
- **WHEN** native setup re-shows only the demo prompt
- **THEN** the panel does not show a native setup progress indicator

### Requirement: Recommendation-first setup presentation

Native setup SHALL present a dedicated non-actionable loading state while it detects available CLIs and discovers models for the four standard roles. After discovery completes, native setup SHALL present a single actionable summary containing the recommended lead, crosscheck, implementor, and tester selections before asking the user to accept or customize them.

The summary SHALL identify the recommended CLI and model for each role, SHALL disclose any model-discovery failure or fallback that affected a recommendation, and SHALL offer distinct actions to accept all recommendations or customize them. The customization path SHALL visit roles in lead, crosscheck, implementor, then tester order.

#### Scenario: Discovery has a dedicated loading state
- **WHEN** native setup is detecting CLIs or discovering models
- **THEN** the panel shows a non-actionable loading state
- **AND** the panel does not show stale recommendation or selection controls

#### Scenario: Summary presents all recommendations together
- **WHEN** CLI and model discovery completes
- **THEN** native setup presents lead, crosscheck, implementor, and tester recommendations on one summary screen
- **AND** each recommendation identifies its CLI and selected model or CLI-default fallback
- **AND** the screen offers actions to accept all recommendations or customize them

#### Scenario: Discovery limitation is visible
- **WHEN** a CLI remains eligible after its model discovery fails
- **THEN** the recommendation summary identifies that CLI's discovery limitation
- **AND** the affected recommendation identifies that the CLI default will be used

#### Scenario: Customization follows role order
- **WHEN** the user chooses to customize the recommendations
- **THEN** native setup presents role customization in lead, crosscheck, implementor, then tester order
