## ADDED Requirements

### Requirement: Editor produces a fixed four-agent shape

The editor SHALL write exactly four agent entries under `profiles.default.agents` in the chosen config file: `interactive_base`, `headless_base`, `planner`, `implementor`. `interactive_base` SHALL include `default_mode: interactive` and the user-chosen `cli` and `model`. `headless_base` SHALL include `default_mode: headless` and the user-chosen `cli` and `model`. `planner` SHALL declare `extends: interactive_base` and SHALL NOT include any other field. `implementor` SHALL declare `extends: headless_base` and SHALL NOT include any other field. The editor SHALL NOT write `summarizer` or any other agent.

#### Scenario: Successful first-time write
- **WHEN** the user picks `cli: claude, model: opus` for interactive_base and `cli: codex, model: gpt-5` for headless_base, scope `global`, and confirms the write
- **THEN** `~/.agent-runner/config.yaml` contains exactly the four entries: `interactive_base` with `default_mode: interactive, cli: claude, model: opus`; `headless_base` with `default_mode: headless, cli: codex, model: gpt-5`; `planner` with only `extends: interactive_base`; `implementor` with only `extends: headless_base`

#### Scenario: Editor does not write summarizer
- **WHEN** the editor completes a successful write
- **THEN** the resulting `profiles.default.agents` map contains no `summarizer` entry from the editor; if a `summarizer` entry existed in the file before, it is preserved unchanged

### Requirement: User chooses CLI and model for each base agent

The editor SHALL prompt the user to choose a CLI adapter and a model for `interactive_base`, and separately for `headless_base`. CLI options SHALL be drawn at runtime from the bundled adapter-detection script's output (the list of CLIs available on the host's `$PATH`). Model options SHALL be discovered at runtime by querying the chosen CLI adapter for available models. When the CLI does not support model listing (the discovery script returns an empty list), the model selection step SHALL be skipped and the model field SHALL be written as empty (meaning "adapter default").

#### Scenario: CLI options reflect detected adapters
- **WHEN** the host has `claude` and `codex` on `$PATH` but not `cursor`
- **THEN** the CLI selection screens for both `interactive_base` and `headless_base` present `claude` and `codex` as the only options

#### Scenario: Model options reflect chosen CLI
- **WHEN** the user picks `cli: claude` for interactive_base and the model discovery script returns `["opus", "sonnet", "haiku"]`
- **THEN** the next screen presents only those discovered models

#### Scenario: Model discovery returns empty
- **WHEN** the user picks a CLI adapter whose model discovery returns an empty list
- **THEN** the model selection step is skipped and the model field is written as empty

#### Scenario: No detected adapters
- **WHEN** adapter detection returns an empty list
- **THEN** the editor fails the CLI selection step with an error indicating no supported CLI adapters were found on `$PATH`

### Requirement: User chooses scope

The editor SHALL prompt the user to choose `global` or `project`. `global` SHALL write to `~/.agent-runner/config.yaml`. `project` SHALL write to `.agent-runner/config.yaml` resolved against the runner's working directory. The runner SHALL NOT inspect the cwd for project markers (no `.git`, no project-root heuristic); whichever cwd the user invoked from is the project location.

#### Scenario: Global scope writes to home
- **WHEN** the user picks scope `global`
- **THEN** the write target is `~/.agent-runner/config.yaml`

#### Scenario: Project scope writes to cwd
- **WHEN** the user picks scope `project` and the runner was invoked from `/path/to/project`
- **THEN** the write target is `/path/to/project/.agent-runner/config.yaml`

#### Scenario: Project scope without project markers
- **WHEN** the user picks scope `project` and the cwd has no `.git` directory or other project marker
- **THEN** the write proceeds without warning; the file is created at `<cwd>/.agent-runner/config.yaml`

### Requirement: Confirmation before write

The editor SHALL present a confirmation screen showing the chosen CLIs, models, and scope before invoking the write step. The user MAY cancel from this screen via a `cancel` action; cancellation SHALL fail the write step without modifying any file.

#### Scenario: Confirm proceeds to write
- **WHEN** the user reviews the confirmation screen and selects the confirm action
- **THEN** the editor invokes the write step

#### Scenario: Cancel from confirmation
- **WHEN** the user reviews the confirmation screen and selects the cancel action
- **THEN** no file is created or modified and the workflow handles the cancellation per its existing failure-handling rules

### Requirement: Overwrite confirmation when entries already exist

Before writing, the editor SHALL inspect the chosen scope's config file (if it exists) for any of the four entries `interactive_base`, `headless_base`, `planner`, or `implementor` under `profiles.default.agents`. If any are present, the editor SHALL display an additional confirmation screen naming the colliding entries and offering an `overwrite` action and a `cancel` action. The `overwrite` action SHALL proceed with the write, replacing those entries. The `cancel` action SHALL fail the write step without modifying the file.

#### Scenario: No collisions, no overwrite screen
- **WHEN** the chosen config file does not exist or contains no entry named `interactive_base`, `headless_base`, `planner`, or `implementor` under `profiles.default.agents`
- **THEN** the editor proceeds directly from confirmation to write with no overwrite screen

#### Scenario: Existing planner triggers overwrite screen
- **WHEN** the chosen config file already contains a `planner` entry under `profiles.default.agents`
- **THEN** the editor shows an overwrite confirmation screen naming `planner` (and any other colliding entries), offering overwrite and cancel actions

#### Scenario: User cancels overwrite
- **WHEN** the overwrite confirmation screen is shown and the user selects `cancel`
- **THEN** no file is modified and the workflow handles the failure per its existing rules

#### Scenario: User overwrites
- **WHEN** the overwrite confirmation screen is shown and the user selects `overwrite`
- **THEN** the write proceeds and the four entries replace any pre-existing entries of the same name

### Requirement: Write goes through an internal Go subcommand

The bundled writer script SHALL invoke a named, tested internal subcommand `agent-runner internal write-profile`. The bundled script SHALL NOT emit YAML directly; user-selected values SHALL NOT enter a YAML emitter inside a shell script. The internal subcommand SHALL be implemented in Go and SHALL have automated test coverage. The subcommand SHALL:
- Accept its inputs as a JSON payload on stdin (interactive cli/model, headless cli/model, target path).
- Read the existing file (if any), parse it, and merge the four entries into `profiles.default.agents` while preserving any other agents, other profile sets, and any other top-level keys.
- Write the result atomically (temp file + rename in the same directory).
- Set the resulting file mode to `0o600` and create any missing parent directories with mode `0o755`.

#### Scenario: Existing other agents preserved
- **WHEN** the chosen config file already contains an agent `team_implementor` under `profiles.default.agents` and unrelated profile sets `team` and `prod` and the editor writes the four-agent shape with `overwrite` accepted for any collisions
- **THEN** the resulting file contains the four editor-written entries and still contains `team_implementor`, `team`, and `prod` unchanged

#### Scenario: Atomic write
- **WHEN** the write subcommand is interrupted or fails part-way through writing
- **THEN** the original file (if any) remains intact and no partial file is left at the target path

#### Scenario: File mode and parent directory creation
- **WHEN** the write target is `~/.agent-runner/config.yaml` and `~/.agent-runner/` does not exist
- **THEN** the directory is created with mode `0o755` and the file is written with mode `0o600`

#### Scenario: Shell-side YAML rejected by tests
- **WHEN** the bundled writer script's content is examined by automated tests
- **THEN** the tests confirm the script invokes `agent-runner internal write-profile` and does not contain shell-side YAML construction (no `echo` / `cat` heredocs producing structured config)

### Requirement: User-initiated, never auto-generated

The editor SHALL run only when explicitly invoked. Valid entry points are: the onboarding workflow's setup phase, and the user running `agent-runner run onboarding:setup-agent-profile` directly. The runner SHALL NOT invoke the editor automatically at startup outside the onboarding first-run dispatch path. This preserves the existing rule from `agent-profiles` that config files are not auto-generated.

#### Scenario: Editor only runs from explicit entry points
- **WHEN** the runner starts on a host with no config file and the first-run dispatcher condition is not satisfied (e.g., onboarding is dismissed)
- **THEN** the editor does not run and no config file is created

#### Scenario: Direct invocation
- **WHEN** the user runs `agent-runner run onboarding:setup-agent-profile`
- **THEN** the editor runs as a normal workflow

### Requirement: One profile pass per editor session

A single editor session SHALL produce one set of writes. The editor SHALL NOT loop back from the confirmation screen to re-collect choices within the same session in this version. To revise choices, the user re-runs the editor.

#### Scenario: User wants to revise after confirming
- **WHEN** the user has reached the confirmation screen and wants to change a previous choice
- **THEN** the only path is to cancel the current session and re-run the editor; the session does not offer a "back" navigation
