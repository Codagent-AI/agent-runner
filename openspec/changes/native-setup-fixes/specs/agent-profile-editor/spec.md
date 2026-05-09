## MODIFIED Requirements

### Requirement: Editor produces a fixed two-agent shape

The editor SHALL write exactly two agent entries under `profiles.default.agents` in the chosen config file: `planner` and `implementor`. `planner` SHALL include `default_mode: interactive` and the user-chosen interactive `cli` and `model`. `implementor` SHALL include `default_mode: headless` and the user-chosen headless `cli` and `model`. The editor SHALL NOT write `interactive_base`, `headless_base`, `summarizer`, or any other agent. When the model field is empty (adapter default), it SHALL be omitted from the written entry.

#### Scenario: Successful first-time write
- **WHEN** the user picks `cli: claude, model: opus` for planner and `cli: codex, model: gpt-5` for implementor, scope `global`, and the write proceeds
- **THEN** `~/.agent-runner/config.yaml` contains exactly two entries: `planner` with `default_mode: interactive, cli: claude, model: opus`; `implementor` with `default_mode: headless, cli: codex, model: gpt-5`

#### Scenario: Editor does not write base agents
- **WHEN** the editor completes a successful write
- **THEN** the resulting `profiles.default.agents` map contains no `interactive_base` or `headless_base` entry from the editor; if those entries existed in the file before, they are preserved unchanged

#### Scenario: Editor does not write summarizer
- **WHEN** the editor completes a successful write
- **THEN** the resulting `profiles.default.agents` map contains no `summarizer` entry from the editor; if a `summarizer` entry existed in the file before, it is preserved unchanged

### Requirement: User chooses CLI and model for planner and implementor

The native setup profile editor SHALL prompt the user to choose a CLI adapter and a model for the planner agent (interactive), and separately for the implementor agent (headless). CLI options SHALL be drawn at runtime from the existing adapter detection behavior. Model options SHALL be discovered at runtime by querying the chosen CLI adapter for available models where the adapter exposes model listing. When the CLI does not support model listing or model discovery returns an empty list, the editor SHALL show an explicit default-model screen with a Continue action; after Continue, the model field SHALL be omitted from the written entry.

The model discovery for `codex` SHALL parse the `{"models": [...]}` JSON envelope returned by `codex debug models`, extracting models from the `models` array field. Each model entry with `"visibility": "list"` SHALL be included; entries with other visibility values SHALL be excluded.

The `claude` CLI SHALL use the known Claude Code model aliases `opus` and `sonnet`, in that order, because the installed Claude Code CLI accepts model aliases but does not expose a model-listing command.

Model discovery SHALL run asynchronously after the user selects a CLI. The TUI SHALL animate directly to the corresponding model screen, show a loading indicator on that model screen while discovery is running, and update the same screen with options or the default-model Continue action when discovery completes. The TUI SHALL NOT animate to a separate "discovering models" screen.

#### Scenario: CLI options reflect detected adapters
- **WHEN** the host has `claude` and `codex` on `$PATH` but not `cursor`
- **THEN** the native setup CLI selection screens for both planner and implementor present `claude` and `codex` as the only options

#### Scenario: Planner CLI recommends Claude
- **WHEN** the host has `claude` and `codex` on `$PATH`
- **THEN** the planner CLI selection screen marks `claude` as recommended and defaults focus to `claude`

#### Scenario: Implementor CLI recommends Codex
- **WHEN** the host has `claude` and `codex` on `$PATH`
- **THEN** the implementor CLI selection screen marks `codex` as recommended and defaults focus to `codex`

#### Scenario: Claude aliases are ordered by recommendation
- **WHEN** the user picks `cli: claude`
- **THEN** the model selection screen presents `opus` before `sonnet`

#### Scenario: Model options reflect chosen CLI
- **WHEN** the user picks `cli: codex` and model discovery returns `["gpt-5.5", "gpt-5.4"]`
- **THEN** the native setup model selection screen presents only those discovered models

#### Scenario: Model discovery loads on model screen
- **WHEN** the user selects a CLI and model discovery is still running
- **THEN** native setup shows the corresponding model screen with a loading indicator rather than a separate discovery screen

#### Scenario: Model discovery returns empty
- **WHEN** the user picks a CLI adapter whose model discovery returns an empty list
- **THEN** native setup shows a default-model screen explaining that Agent Runner will use the CLI default and omit the model field after the user continues

#### Scenario: No detected adapters
- **WHEN** adapter detection returns an empty list
- **THEN** native setup fails the CLI selection step with an error indicating no supported CLI adapters were found on `$PATH`

#### Scenario: Codex model discovery parses envelope format
- **WHEN** the user picks `cli: codex` and `codex debug models` returns `{"models":[{"slug":"gpt-5.5","visibility":"list"},{"slug":"internal","visibility":"internal"}]}`
- **THEN** the model selection screen presents only `gpt-5.5`

## REMOVED Requirements

### Requirement: Confirmation before write

**Reason:** The confirmation screen is removed to streamline the setup flow. After scope selection, the editor proceeds directly to collision check (if applicable) and write.

**Migration:** Remove the `stageConfirm` stage and `confirmOptions` from the native setup TUI. Remove the confirmation-related test cases.

## MODIFIED Requirements

### Requirement: Overwrite confirmation when entries already exist

Before writing, the native setup profile editor SHALL inspect the chosen scope's config file, if it exists, for either `planner` or `implementor` under `profiles.default.agents`. If any are present, the editor SHALL display an additional confirmation screen naming the colliding entries and offering an `overwrite` action and a `cancel` action. The `overwrite` action SHALL proceed with the write, replacing those entries. The `cancel` action SHALL leave the file unchanged and leave native setup incomplete.

#### Scenario: No collisions, no overwrite screen
- **WHEN** the chosen config file does not exist or contains no entry named `planner` or `implementor` under `profiles.default.agents`
- **THEN** the editor proceeds directly to write with no overwrite screen

#### Scenario: Existing planner triggers overwrite screen
- **WHEN** the chosen config file already contains a `planner` entry under `profiles.default.agents`
- **THEN** native setup shows an overwrite confirmation naming `planner` and any other colliding entries

#### Scenario: User cancels overwrite
- **WHEN** the overwrite confirmation screen is shown and the user selects cancel
- **THEN** no profile file is modified and native setup is not marked complete

#### Scenario: User overwrites
- **WHEN** the overwrite confirmation screen is shown and the user selects overwrite
- **THEN** the write proceeds and the two editor-managed entries replace any pre-existing entries of the same name
