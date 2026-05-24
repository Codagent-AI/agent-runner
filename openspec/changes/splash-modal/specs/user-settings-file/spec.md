## ADDED Requirements

### Requirement: Splash modal settings

The user settings schema SHALL support a `splash` mapping with a `dismissed` field that records persistent suppression of the home-screen splash modal. When set, `splash.dismissed` SHALL be an RFC3339 timestamp and SHALL be preserved by settings load and save operations. `splash.dismissed` SHALL be independent of `setup.completed_at`, `onboarding.completed_at`, and `onboarding.dismissed`: changes to one SHALL NOT affect the loaded value of the others, and an unrelated write SHALL preserve all such keys' existing values.

The `splash` mapping SHALL NOT define a separate `completed_at` field — the splash has no notion of completion distinct from dismissal.

#### Scenario: Dismissed splash timestamp loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `splash.dismissed: 2026-05-24T00:00:00Z`
- **THEN** settings load exposes that timestamp to splash dispatch logic

#### Scenario: Splash dismissal is preserved on unrelated write
- **WHEN** the runner writes unrelated settings (e.g., theme change) and `splash.dismissed` is already in the file
- **THEN** the existing `splash.dismissed` value is preserved

#### Scenario: Splash and onboarding dismissal are independent
- **WHEN** settings contain `splash.dismissed` and `onboarding.dismissed`
- **THEN** settings load exposes both timestamps independently and neither affects the other

#### Scenario: Splash completed_at key is ignored
- **WHEN** `~/.agent-runner/settings.yaml` contains `splash.completed_at: 2026-05-24T00:00:00Z`
- **THEN** splash dispatch does not treat that value as suppressing the splash modal

#### Scenario: Round-trip preserves splash mapping
- **WHEN** the runner loads settings containing `splash.dismissed: 2026-05-24T00:00:00Z`, mutates an unrelated field, and saves
- **THEN** the written file still contains `splash.dismissed: 2026-05-24T00:00:00Z`
