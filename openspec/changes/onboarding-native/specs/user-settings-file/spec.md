## MODIFIED Requirements

### Requirement: Settings file format

The file's contents SHALL be valid YAML whose top-level value is a mapping. Top-level keys not recognized by the current binary SHALL be ignored at load time, so a newer file remains forward-compatible with an older binary that does not know about a key. Recognized keys include setup tracking keys defined by native setup, onboarding demo tracking keys, and other keys defined by separate capabilities.

#### Scenario: File parses as a mapping
- **WHEN** the file contains valid YAML whose root is a mapping with one or more recognized keys
- **THEN** load succeeds and recognized keys, including native setup tracking keys, are exposed to callers

#### Scenario: File contains unknown keys
- **WHEN** the file contains a top-level key like `experimental_foo: 7` that the current binary does not recognize
- **THEN** load succeeds; the unknown key is ignored without warning or error

#### Scenario: File is empty
- **WHEN** the file exists but is zero bytes or contains only whitespace/comments
- **THEN** load succeeds and returns an empty settings value, equivalent to a missing file from the caller's perspective

#### Scenario: File is unparseable YAML
- **WHEN** the file contains content that is not valid YAML
- **THEN** load returns an empty settings value, equivalent to a missing file; the caller may treat this as settings need to be rewritten

#### Scenario: File root is not a mapping
- **WHEN** the file parses successfully but its root value is a scalar, sequence, or null
- **THEN** load returns an empty settings value, equivalent to a missing file

## ADDED Requirements

### Requirement: Native setup settings

The user settings schema SHALL support native setup tracking under a `setup` mapping. `setup.completed_at` records successful setup completion. When set, it SHALL be an RFC3339 timestamp and SHALL be preserved by settings load and save operations. Native setup SHALL NOT define a setup dismissal setting.

#### Scenario: Completed setup timestamp loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `setup.completed_at: 2026-05-03T00:00:00Z`
- **THEN** settings load exposes that timestamp to native setup dispatch logic

#### Scenario: Setup timestamp is preserved on write
- **WHEN** the runner writes unrelated settings
- **THEN** existing `setup.completed_at` is preserved unless the caller explicitly changes it

#### Scenario: Setup dismissed key is ignored
- **WHEN** `~/.agent-runner/settings.yaml` contains `setup.dismissed: 2026-05-03T00:00:00Z`
- **THEN** native setup dispatch does not treat that value as suppressing mandatory setup

### Requirement: Onboarding demo settings

The user settings schema SHALL continue to support onboarding demo completion under `onboarding.completed_at` and onboarding demo dismissal under `onboarding.dismissed`. `onboarding.completed_at` records successful completion of the onboarding demo workflow. `onboarding.dismissed` records explicit dismissal of the optional onboarding demo. Both settings SHALL be distinct from native setup completion.

#### Scenario: Completed onboarding timestamp loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `onboarding.completed_at: 2026-05-03T00:00:00Z`
- **THEN** settings load exposes that timestamp to onboarding demo dispatch logic

#### Scenario: Dismissed onboarding timestamp loads
- **WHEN** `~/.agent-runner/settings.yaml` contains `onboarding.dismissed: 2026-05-03T00:00:00Z`
- **THEN** settings load exposes that timestamp to onboarding demo dispatch logic

#### Scenario: Setup and onboarding completion are independent
- **WHEN** settings contain both `setup.completed_at` and `onboarding.completed_at`
- **THEN** settings load exposes both timestamps independently

#### Scenario: Onboarding completion preserved on setup write
- **WHEN** the runner writes setup tracking settings
- **THEN** existing `onboarding.completed_at` is preserved unless the caller explicitly changes it
