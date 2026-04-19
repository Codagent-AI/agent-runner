# Capability: cli-adapter

## Purpose

Defines the CLI adapter abstraction for constructing CLI invocation args across different backends (Claude, Codex) in headless and interactive modes.
## Requirements
### Requirement: CLI adapter registry

The runner SHALL maintain a hard-coded registry of known CLI adapters. Each adapter SHALL be identified by a string key (e.g., `claude`, `codex`, `copilot`). The registry is compile-time — adding a new CLI requires a code change.

#### Scenario: Known CLI resolved
- **WHEN** a step specifies `cli: claude`
- **THEN** the runner resolves the Claude adapter from the registry

#### Scenario: Copilot CLI resolved
- **WHEN** a step specifies `cli: copilot`
- **THEN** the runner resolves the Copilot adapter from the registry

#### Scenario: Unknown CLI requested
- **WHEN** a step specifies a `cli` value not in the registry
- **THEN** the runner fails at load time with a validation error indicating the CLI is not supported

### Requirement: Adapter system prompt capability

Each adapter SHALL declare whether it supports native system prompt delivery. The Claude adapter SHALL declare support. The Codex adapter SHALL declare no support. The Copilot adapter SHALL declare no support.

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

### Requirement: Adapter arg construction
Each adapter SHALL construct the CLI invocation args for both headless and interactive modes. The adapter receives a `BuildArgsInput` containing the prompt, system prompt content (if applicable), session ID (present for both fresh and resumed sessions; paired with a `Resume` flag distinguishing the two), model override (if specified), effort level (if specified), a headless flag, and an optional `DisallowedTools` list, and returns the full command and args. When effort is provided, the adapter SHALL include the appropriate CLI flag for the effort level. If effort is empty, no effort flag is emitted. If system prompt content is provided and the adapter supports it, the adapter SHALL include the appropriate CLI flags to deliver it as a system prompt (e.g., `--append-system-prompt` for Claude). When `DisallowedTools` is non-empty, the adapter SHALL emit the CLI flags needed to disable those tools where the backing CLI supports such a flag (e.g., `--disallowedTools` for Claude); adapters whose CLI has no equivalent flag MAY ignore the list.

#### Scenario: Headless invocation with model override
- **WHEN** the runner executes a headless step with `model: sonnet` and a session ID from state
- **THEN** the adapter returns args that include the prompt, model flag, session resume flag, and headless flag appropriate to that CLI

#### Scenario: Interactive invocation with no session
- **WHEN** the runner executes an interactive step with session strategy `new`
- **THEN** the adapter returns args for a fresh interactive session (no resume flag)

#### Scenario: Interactive invocation with system prompt (Claude)
- **WHEN** the runner provides system prompt content to the Claude adapter for an interactive step
- **THEN** the adapter includes `--append-system-prompt` with the content in the args

#### Scenario: System prompt content provided to unsupporting adapter
- **WHEN** the runner provides system prompt content to an adapter that does not support it
- **THEN** the adapter ignores the system prompt field (the runner handles fallback wrapping)

#### Scenario: Effort level specified
- **WHEN** the runner provides effort level "high" to an adapter
- **THEN** the adapter includes the CLI-appropriate effort flag in the args

#### Scenario: Effort level not specified
- **WHEN** the runner provides no effort level (empty string)
- **THEN** the adapter does not include any effort flag in the args

### Requirement: Adapter session ID return

After a CLI process exits, the adapter SHALL attempt to return a session ID. The runner stores this ID in state.json for future resume. How the adapter obtains the session ID is adapter-specific. If the adapter cannot determine the session ID, it SHALL return empty.

#### Scenario: Session ID returned after first run
- **WHEN** a CLI step completes (fresh session, no prior session ID)
- **THEN** the adapter returns a session ID and the runner stores it in state

#### Scenario: Session ID returned after resumed run
- **WHEN** a CLI step completes after resuming a prior session
- **THEN** the adapter returns the session ID (which may be the same or updated) and the runner stores it in state

#### Scenario: Session ID unavailable
- **WHEN** a CLI step completes but the adapter cannot determine the session ID
- **THEN** the adapter returns empty and the runner logs a warning; future resume for this step is not possible

### Requirement: Session ID persisted before CLI spawn when known

When the session ID is known at spawn time — either because the runner pre-generated it (fresh Claude sessions) or because it was carried in from prior state (any resumed session) — the runner SHALL persist it into the execution context and flush the run state BEFORE spawning the CLI process. This ensures that if the runner is killed mid-step (ctrl-c, terminal hangup, crash) the session is recoverable on workflow resume rather than orphaned. When the session ID is not knowable until after the process has run (fresh Codex sessions), the runner MAY defer persistence to post-exit via the adapter's `DiscoverSessionID`.

#### Scenario: Fresh Claude session persisted before spawn
- **WHEN** a fresh Claude step is about to spawn the CLI process
- **THEN** the pre-generated session ID is written to `ctx.SessionIDs[step.id]` and flushed to state before the process is invoked

#### Scenario: Resumed session re-persisted before spawn
- **WHEN** any resumed agent step is about to spawn the CLI process
- **THEN** the carried-in session ID is written to `ctx.SessionIDs[step.id]` and flushed to state before the process is invoked

#### Scenario: Runner killed mid-step recovers session on resume
- **WHEN** a fresh Claude step has been spawned and the runner is killed before the CLI process exits
- **THEN** the next `--resume` of that run sees the session ID already in state.json and reconnects to the existing CLI session rather than starting a new one

#### Scenario: Fresh Codex session deferred to post-exit
- **WHEN** a fresh Codex step spawns the CLI process and no pre-generated ID exists
- **THEN** the runner does not pre-flush a session ID; post-exit discovery via `DiscoverSessionID` remains the sole persistence point

