## MODIFIED Requirements

### Requirement: User chooses CLI and model for each base agent

The native setup profile editor SHALL prompt the user to choose a CLI adapter and a model for `interactive_base`, and separately for `headless_base`. CLI options SHALL be drawn at runtime from the existing adapter detection behavior. Model options SHALL be discovered at runtime by querying the chosen CLI adapter for available models. When the CLI does not support model listing or model discovery returns an empty list, the model selection step SHALL be skipped and the model field SHALL be written as empty, meaning adapter default.

#### Scenario: CLI options reflect detected adapters
- **WHEN** the host has `claude` and `codex` on `$PATH` but not `cursor`
- **THEN** the native setup CLI selection screens for both `interactive_base` and `headless_base` present `claude` and `codex` as the only options

#### Scenario: Model options reflect chosen CLI
- **WHEN** the user picks `cli: claude` for `interactive_base` and model discovery returns `["opus", "sonnet", "haiku"]`
- **THEN** the native setup model selection screen presents only those discovered models

#### Scenario: Model discovery returns empty
- **WHEN** the user picks a CLI adapter whose model discovery returns an empty list
- **THEN** the model selection step is skipped and the model field is written as empty

#### Scenario: No detected adapters
- **WHEN** adapter detection returns an empty list
- **THEN** native setup fails the CLI selection step with an error indicating no supported CLI adapters were found on `$PATH`

### Requirement: User chooses scope

The native setup profile editor SHALL prompt the user to choose `global` or `project`. `global` SHALL write to `~/.agent-runner/config.yaml`. `project` SHALL write to `.agent-runner/config.yaml` resolved against the runner's working directory. The runner SHALL NOT inspect the cwd for project markers; whichever cwd the user invoked from is the project location.

#### Scenario: Global scope writes to home
- **WHEN** the user picks scope `global`
- **THEN** the write target is `~/.agent-runner/config.yaml`

#### Scenario: Project scope writes to cwd
- **WHEN** the user picks scope `project` and the runner was invoked from `/path/to/project`
- **THEN** the write target is `/path/to/project/.agent-runner/config.yaml`

#### Scenario: Project scope without project markers
- **WHEN** the user picks scope `project` and the cwd has no `.git` directory or other project marker
- **THEN** the write proceeds without warning and the file is created at `<cwd>/.agent-runner/config.yaml`

### Requirement: Confirmation before write

The native setup profile editor SHALL present a confirmation screen showing the chosen CLIs, models, and scope before invoking the write path. The user MAY cancel from this screen; cancellation SHALL leave profile configuration unchanged and leave native setup incomplete.

#### Scenario: Confirm proceeds to write
- **WHEN** the user reviews the confirmation screen and selects confirm
- **THEN** native setup invokes the profile write path

#### Scenario: Cancel from confirmation
- **WHEN** the user reviews the confirmation screen and selects cancel
- **THEN** no profile file is created or modified and native setup is not marked complete

### Requirement: Overwrite confirmation when entries already exist

Before writing, the native setup profile editor SHALL inspect the chosen scope's config file, if it exists, for any of the four entries `interactive_base`, `headless_base`, `planner`, or `implementor` under `profiles.default.agents`. If any are present, the editor SHALL display an additional confirmation screen naming the colliding entries and offering an `overwrite` action and a `cancel` action. The `overwrite` action SHALL proceed with the write, replacing those entries. The `cancel` action SHALL leave the file unchanged and leave native setup incomplete.

#### Scenario: No collisions, no overwrite screen
- **WHEN** the chosen config file does not exist or contains no entry named `interactive_base`, `headless_base`, `planner`, or `implementor` under `profiles.default.agents`
- **THEN** the editor proceeds directly from confirmation to write with no overwrite screen

#### Scenario: Existing planner triggers overwrite screen
- **WHEN** the chosen config file already contains a `planner` entry under `profiles.default.agents`
- **THEN** native setup shows an overwrite confirmation naming `planner` and any other colliding entries

#### Scenario: User cancels overwrite
- **WHEN** the overwrite confirmation screen is shown and the user selects cancel
- **THEN** no profile file is modified and native setup is not marked complete

#### Scenario: User overwrites
- **WHEN** the overwrite confirmation screen is shown and the user selects overwrite
- **THEN** the write proceeds and the four editor-managed entries replace any pre-existing entries of the same name

### Requirement: User-initiated, never auto-generated

The editor SHALL run only from a user-facing setup action. Native first-run setup MAY offer the editor when setup has not completed, but the runner SHALL NOT silently generate profile configuration without the user continuing through setup and confirming the write. The workflow entry point `onboarding:setup-agent-profile` is no longer required for this capability.

#### Scenario: Editor only runs from setup interaction
- **WHEN** the runner starts on a host with no config file and native setup is not eligible
- **THEN** no profile file is created

#### Scenario: Native setup invokes editor
- **WHEN** the user chooses continue in native setup
- **THEN** the native setup TUI collects profile choices and can invoke the profile write path after confirmation

#### Scenario: Old setup workflow is not required
- **WHEN** the user runs `agent-runner run onboarding:setup-agent-profile`
- **THEN** the agent profile editor capability does not require that workflow to exist

## ADDED Requirements

### Requirement: Profile write uses shared Go writer

The native setup profile editor SHALL use the tested internal Go profile-writing path directly. The existing `agent-runner internal write-profile` subcommand SHALL remain a wrapper around that same shared writer. User-selected values SHALL NOT enter a YAML emitter inside a shell script. The shared writer SHALL:
- Accept its inputs as structured data for interactive cli/model, headless cli/model, and target path.
- Read the existing file, if any, parse it, and merge the four entries into `profiles.default.agents` while preserving any other agents, other profile sets, and any other top-level keys.
- Write the result atomically using a temp file and rename in the same directory.
- Set the resulting file mode to `0o600` and create any missing parent directories with mode `0o755`.

#### Scenario: Existing other agents preserved
- **WHEN** the chosen config file already contains an agent `team_implementor` under `profiles.default.agents` and unrelated profile sets `team` and `prod` and the editor writes the four-agent shape with overwrite accepted for any collisions
- **THEN** the resulting file contains the four editor-written entries and still contains `team_implementor`, `team`, and `prod` unchanged

#### Scenario: Atomic write
- **WHEN** the write operation is interrupted or fails part-way through writing
- **THEN** the original file, if any, remains intact and no partial file is left at the target path

#### Scenario: File mode and parent directory creation
- **WHEN** the write target is `~/.agent-runner/config.yaml` and `~/.agent-runner/` does not exist
- **THEN** the directory is created with mode `0o755` and the file is written with mode `0o600`

#### Scenario: Internal command remains wrapper
- **WHEN** `agent-runner internal write-profile` is invoked with a valid payload
- **THEN** it writes through the same shared Go writer used by native setup

#### Scenario: Shell-side YAML rejected by tests
- **WHEN** the native setup profile editor implementation is examined by automated tests
- **THEN** the tests confirm user-selected values are not rendered into YAML by shell script string construction

## REMOVED Requirements

### Requirement: Write goes through an internal Go subcommand

**Reason**: Native setup should call the shared Go profile-writing package directly rather than shelling through the internal CLI command.
**Migration**: Use `Profile write uses shared Go writer`; keep `agent-runner internal write-profile` as a wrapper for compatibility with existing internal-command tests and any remaining callers.
