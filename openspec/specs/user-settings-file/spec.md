# user-settings-file Specification

## Purpose
Define the per-user settings file at `~/.agent-runner/settings.yaml` — its location, format, atomic write semantics, parent directory creation, and error surfacing contract.
## Requirements
### Requirement: Settings file location
Agent Runner SHALL read and write user settings to a single file at `~/.agent-runner/settings.yaml`. The file is per-user and global (not per-project, not per-workflow). It SHALL be independent of `~/.agent-runner/config.yaml` — neither file references the other and they have separate schemas, separate loaders, and separate validation. The file MAY be absent on a fresh installation; absence is not an error.

#### Scenario: Resolved path on a typical user account
- **WHEN** the runner resolves the settings file path on a user whose `$HOME` is `/Users/alice`
- **THEN** the resolved path is `/Users/alice/.agent-runner/settings.yaml`

#### Scenario: File is absent
- **WHEN** the runner attempts to load settings and the file does not exist
- **THEN** the load completes without error, returning an empty settings value

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

### Requirement: Atomic writes
When the runner writes the settings file, it SHALL do so atomically by writing to a temporary file in the same directory and then renaming the temporary file to `settings.yaml`. Concurrent overlapping writes SHALL never produce a partial or interleaved file: the resulting file SHALL always be the complete contents of one writer's payload. The file mode SHALL be `0o600` (user read/write only).

#### Scenario: Successful write
- **WHEN** the runner persists settings and the OS rename succeeds
- **THEN** `~/.agent-runner/settings.yaml` exists with the new payload and mode `0o600`

#### Scenario: Concurrent overlapping writes
- **WHEN** two `agent-runner` processes simultaneously write different settings payloads
- **THEN** the resulting file is exactly one of the two payloads in its entirety; never a partial or interleaved file

#### Scenario: Temporary file failure leaves no garbage
- **WHEN** writing the temporary file fails partway through (e.g., disk full)
- **THEN** the runner does not leave the temporary file behind on disk after the call returns

### Requirement: Parent directory creation
The runner SHALL create the parent directory `~/.agent-runner/` (and any missing intermediate directories) when writing the settings file, if the directory does not already exist. The created directory SHALL have mode `0o755`. The runner SHALL NOT create the parent directory merely to read a missing file.

#### Scenario: Parent directory does not exist on first save
- **WHEN** the runner writes settings and `~/.agent-runner/` does not yet exist
- **THEN** the directory is created with mode `0o755` and the file is written inside it

#### Scenario: Parent directory already exists
- **WHEN** the runner writes settings and `~/.agent-runner/` already exists
- **THEN** the existing directory is left untouched (its mode is not changed) and the file is written inside it

#### Scenario: Read of missing file does not create directory
- **WHEN** the runner attempts to load settings and neither the file nor the parent directory exists
- **THEN** load returns an empty settings value and the parent directory is not created

### Requirement: Write errors surface
If a settings write fails (permission denied, disk full, EROFS, etc.), the runner SHALL return the underlying error to the caller without silently swallowing it. The caller is responsible for surfacing the error to the user; this capability does not prescribe exit codes or message formatting.

#### Scenario: Permission denied on rename
- **WHEN** the runner attempts to write settings but the OS returns EACCES on the final rename
- **THEN** the write call returns an error that identifies the file path and the underlying OS error

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

