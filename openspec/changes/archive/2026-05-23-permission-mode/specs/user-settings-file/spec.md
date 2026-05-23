## ADDED Requirements

### Requirement: Autonomous permission mode setting

The user settings schema SHALL support an `autonomous_permission_mode` top-level key that controls how much authority CLI adapters pre-grant to agents in autonomous invocation contexts. Valid values are `conservative` and `yolo`. When the key is absent from the file, the loader SHALL expose a default value of `conservative`. When the key is present with a value not in the valid set, settings load SHALL fail with a validation error identifying the invalid value and the valid options.

The setting SHALL be independent of `autonomous_backend`: changes to one SHALL NOT affect the loaded value of the other, and an unrelated write SHALL preserve both keys' existing values.

#### Scenario: Valid autonomous_permission_mode loads

- **WHEN** `~/.agent-runner/settings.yaml` contains `autonomous_permission_mode: yolo`
- **THEN** settings load exposes `yolo` as the autonomous permission mode value

#### Scenario: Absent autonomous_permission_mode defaults to conservative

- **WHEN** `~/.agent-runner/settings.yaml` exists but does not contain an `autonomous_permission_mode` key
- **THEN** settings load exposes `conservative` as the autonomous permission mode value

#### Scenario: Invalid autonomous_permission_mode rejected

- **WHEN** `~/.agent-runner/settings.yaml` contains `autonomous_permission_mode: ludicrous`
- **THEN** settings load fails with a validation error identifying `ludicrous` as invalid and listing the valid values

#### Scenario: Permission mode preserved on unrelated write

- **WHEN** the runner writes unrelated settings (e.g., theme change) and `autonomous_permission_mode: yolo` is already in the file
- **THEN** the existing `autonomous_permission_mode` value is preserved

#### Scenario: Permission mode and autonomous backend coexist

- **WHEN** `~/.agent-runner/settings.yaml` contains both `autonomous_backend: interactive-claude` and `autonomous_permission_mode: yolo`
- **THEN** settings load exposes both values independently and a write that changes only one preserves the other
