## ADDED Requirements

### Requirement: Adapter mode coverage

Every registered CLI adapter SHALL support both interactive and headless modes. The runner SHALL NOT reject either mode for any registered `cli`.

#### Scenario: Interactive step succeeds for any registered CLI
- **WHEN** an agent step runs in interactive mode with any registered `cli`
- **THEN** the runner spawns the CLI without emitting an unsupported-mode error

#### Scenario: Headless step succeeds for any registered CLI
- **WHEN** an agent step runs in headless mode with any registered `cli`
- **THEN** the runner spawns the CLI without emitting an unsupported-mode error

### Requirement: No permission loosening in interactive mode

In interactive mode, no adapter SHALL emit a flag that bypasses or pre-approves the underlying CLI's permission/approval prompts. The human at the terminal supervises permissions; the runner MUST NOT preempt that supervision. Headless invocations MAY emit such flags, since no human is present to approve.

#### Scenario: Adapter omits permission-grant flags in interactive mode
- **WHEN** any adapter constructs args for an interactive step
- **THEN** the args do not include any flag that auto-approves tools, paths, URLs, or commands (e.g., `--allow-all`, `--force`, `--yolo`, `--dangerously-skip-permissions`)

#### Scenario: Headless adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for a headless step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

### Requirement: AskUserQuestion blocked in headless, allowed in interactive

The runner SHALL include `AskUserQuestion` in `DisallowedTools` for every headless agent step — an agent in headless mode has no human to answer. The runner SHALL leave `DisallowedTools` empty for every interactive agent step — the human at the terminal can answer or decline tool calls directly. Adapter-side translation of `DisallowedTools` to CLI flags is already covered by the existing "Adapter arg construction" requirement.

#### Scenario: Headless step blocks AskUserQuestion
- **WHEN** the runner builds adapter input for a headless agent step
- **THEN** `DisallowedTools` includes `"AskUserQuestion"`

#### Scenario: Interactive step does not block AskUserQuestion
- **WHEN** the runner builds adapter input for an interactive agent step
- **THEN** `DisallowedTools` is empty

## MODIFIED Requirements

### Requirement: CLI adapter registry

The runner SHALL maintain a hard-coded registry of known CLI adapters. Each adapter SHALL be identified by a string key (e.g., `claude`, `codex`, `copilot`, `cursor`, `opencode`). The registry is compile-time — adding a new CLI requires a code change.

#### Scenario: Known CLI resolved
- **WHEN** a step specifies `cli: claude`
- **THEN** the runner resolves the Claude adapter from the registry

#### Scenario: Copilot CLI resolved
- **WHEN** a step specifies `cli: copilot`
- **THEN** the runner resolves the Copilot adapter from the registry

#### Scenario: Cursor CLI resolved
- **WHEN** a step specifies `cli: cursor`
- **THEN** the runner resolves the Cursor adapter from the registry

#### Scenario: OpenCode CLI resolved
- **WHEN** a step specifies `cli: opencode`
- **THEN** the runner resolves the OpenCode adapter from the registry

#### Scenario: Codex CLI resolved
- **WHEN** a step specifies `cli: codex`
- **THEN** the runner resolves the Codex adapter from the registry

#### Scenario: Unknown CLI requested
- **WHEN** a step specifies a `cli` value not in the registry
- **THEN** the runner fails at load time with a validation error indicating the CLI is not supported

### Requirement: Adapter system prompt capability

Each adapter SHALL declare whether it supports native system prompt delivery. The Claude adapter SHALL declare support. The Codex, Copilot, Cursor, and OpenCode adapters SHALL declare no support.

Adapters declare support via a `SupportsSystemPrompt() bool` method on the `Adapter` interface. The caller queries this before constructing `BuildArgsInput`, setting the `SystemPrompt` field only when the adapter supports it.

#### Scenario: Claude adapter declares support
- **WHEN** the runner queries the Claude adapter for system prompt capability
- **THEN** the adapter indicates it supports native system prompts

#### Scenario: Codex adapter declares no support
- **WHEN** the runner queries the Codex adapter for system prompt capability
- **THEN** the adapter indicates it does not support native system prompts

#### Scenario: Copilot adapter declares no support
- **WHEN** the runner queries the Copilot adapter for system prompt capability
- **THEN** the adapter indicates it does not support native system prompts

#### Scenario: Cursor adapter declares no support
- **WHEN** the runner queries the Cursor adapter for system prompt capability
- **THEN** the adapter indicates it does not support native system prompts

#### Scenario: OpenCode adapter declares no support
- **WHEN** the runner queries the OpenCode adapter for system prompt capability
- **THEN** the adapter indicates it does not support native system prompts
