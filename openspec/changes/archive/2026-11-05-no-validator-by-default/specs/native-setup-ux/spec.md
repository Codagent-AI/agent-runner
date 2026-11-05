## MODIFIED Requirements

### Requirement: Explanatory copy on each screen

Every actionable selection screen in native setup SHALL include a title and a short explanatory paragraph (2-4 sentences) that describes what is being asked and why it matters. The tone SHALL be friendly and informative — not terse, not verbose. The non-actionable recommendation loading state SHALL include concise status copy explaining that Agent Runner is inspecting available CLIs and models.

The recommendation experience SHALL include a brief welcoming sentence acknowledging this is initial setup. Its summary SHALL explain that the four selections configure interactive leadership, independent planning crosscheck, implementation, and acceptance testing. Crosscheck copy SHALL explain that it challenges Lead's planning artifacts and looks for omissions. It SHALL NOT state that Agent Validator always reviews implementation code; where it mentions implementation-code review, it SHALL attribute it to the project's validation, which uses Agent Validator when the project has set it up. Each role-customization screen SHALL explain what that role does and what its CLI or model selection controls.

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
- **AND** it does not present Agent Validator as a required or always-present implementation-code reviewer

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
