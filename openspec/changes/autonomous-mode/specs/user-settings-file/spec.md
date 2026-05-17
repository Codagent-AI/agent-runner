## ADDED Requirements

### Requirement: Autonomous backend setting
The user settings schema SHALL support an `autonomous_backend` top-level key that controls how autonomous agent steps are invoked. Valid values are `headless`, `interactive`, and `interactive-claude`. When the key is absent from the file, the loader SHALL expose a default value of `headless`. When the key is present with a value not in the valid set, settings load SHALL fail with a validation error identifying the invalid value and the valid options.

#### Scenario: Valid autonomous_backend loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `autonomous_backend: interactive-claude`
- **THEN** settings load exposes `interactive-claude` as the autonomous backend value

#### Scenario: Absent autonomous_backend defaults to headless
- **WHEN** `~/.agent-runner/settings.yaml` exists but does not contain an `autonomous_backend` key
- **THEN** settings load exposes `headless` as the autonomous backend value

#### Scenario: Invalid autonomous_backend rejected
- **WHEN** `~/.agent-runner/settings.yaml` contains `autonomous_backend: magic`
- **THEN** settings load fails with a validation error identifying `magic` as invalid and listing the valid values

#### Scenario: Autonomous backend preserved on unrelated write
- **WHEN** the runner writes unrelated settings (e.g., theme change) and `autonomous_backend: interactive` is already in the file
- **THEN** the existing `autonomous_backend` value is preserved
