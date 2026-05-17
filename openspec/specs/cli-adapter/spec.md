# Capability: cli-adapter

## Purpose

Defines the CLI adapter abstraction for constructing CLI invocation args across different backends (Claude, Codex) in headless and interactive modes.
## Requirements
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

### Requirement: Adapter arg construction
Each adapter SHALL construct the CLI invocation args for three invocation contexts: interactive, autonomous-headless, and autonomous-interactive. The adapter receives the step's prompt, system prompt content (if applicable), session ID (paired with a resume indicator), model override (if specified), effort level (if specified), invocation context, and an optional disallowed-tools list, and returns the full command and args. When effort is provided, the adapter SHALL include the appropriate CLI flag for the effort level. If effort is empty, no effort flag is emitted. If system prompt content is provided and the adapter supports it, the adapter SHALL include the appropriate CLI flags to deliver it as a system prompt (e.g., `--append-system-prompt` for Claude). When the disallowed-tools list is non-empty, the adapter SHALL emit the CLI flags needed to disable those tools where the backing CLI supports such a flag; adapters whose CLI has no equivalent flag MAY ignore the list. In autonomous-interactive context, adapters SHALL include adapter-specific autonomy flags where the backing CLI supports them (e.g., `--autopilot` for Copilot).

#### Scenario: Autonomous-headless invocation with model override
- **WHEN** the runner executes an autonomous-headless step with `model: sonnet` and a session ID from state
- **THEN** the adapter returns args that include the prompt, model flag, session resume flag, and headless/print-mode flag appropriate to that CLI

#### Scenario: Interactive invocation with no session
- **WHEN** the runner executes an interactive step with session strategy `new`
- **THEN** the adapter returns args for a fresh interactive session (no resume flag, no headless flag)

#### Scenario: Autonomous-interactive invocation
- **WHEN** the runner executes an autonomous-interactive step
- **THEN** the adapter returns args for an interactive session (no headless/print-mode flag) and includes any adapter-specific autonomy flags

#### Scenario: Autonomous-interactive with autonomy flag (Copilot)
- **WHEN** the runner executes an autonomous-interactive step using the Copilot adapter
- **THEN** the adapter includes `--autopilot` in the args alongside the interactive session flags

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

### Requirement: Adapter mode coverage

Every registered CLI adapter SHALL support all three invocation contexts: interactive, autonomous-headless, and autonomous-interactive. The runner SHALL NOT reject any invocation context for any registered `cli`.

#### Scenario: Interactive step succeeds for any registered CLI
- **WHEN** an agent step runs in interactive context with any registered `cli`
- **THEN** the runner spawns the CLI without emitting an unsupported-context error

#### Scenario: Autonomous-headless step succeeds for any registered CLI
- **WHEN** an agent step runs in autonomous-headless context with any registered `cli`
- **THEN** the runner spawns the CLI without emitting an unsupported-context error

#### Scenario: Autonomous-interactive step succeeds for any registered CLI
- **WHEN** an agent step runs in autonomous-interactive context with any registered `cli`
- **THEN** the runner spawns the CLI without emitting an unsupported-context error

### Requirement: No permission loosening in interactive mode

In interactive context, no adapter SHALL emit a flag that bypasses or pre-approves the underlying CLI's permission/approval prompts. The human at the terminal supervises permissions; the runner MUST NOT preempt that supervision. Autonomous invocations (both headless and interactive backend) MAY emit such flags, since the step operates without human supervision.

#### Scenario: Adapter omits permission-grant flags in interactive context
- **WHEN** any adapter constructs args for an interactive step
- **THEN** the args do not include any flag that auto-approves tools, paths, URLs, or commands (e.g., `--allow-all`, `--force`, `--yolo`, `--dangerously-skip-permissions`)

#### Scenario: Autonomous-headless adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-headless step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

#### Scenario: Autonomous-interactive adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-interactive step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

### Requirement: AskUserQuestion blocked in headless, allowed in interactive

The runner SHALL include `AskUserQuestion` in the disallowed-tools list for every autonomous agent step — regardless of whether the backend is headless or interactive, an autonomous step has no human available to answer. The runner SHALL leave the disallowed-tools list empty for every interactive agent step — the human at the terminal can answer or decline tool calls directly.

#### Scenario: Autonomous-headless step blocks AskUserQuestion
- **WHEN** the runner builds adapter input for an autonomous-headless agent step
- **THEN** the disallowed-tools list includes `"AskUserQuestion"`

#### Scenario: Autonomous-interactive step blocks AskUserQuestion
- **WHEN** the runner builds adapter input for an autonomous-interactive agent step
- **THEN** the disallowed-tools list includes `"AskUserQuestion"`

#### Scenario: Interactive step does not block AskUserQuestion
- **WHEN** the runner builds adapter input for an interactive agent step
- **THEN** the disallowed-tools list is empty

### Requirement: Autonomy system prompt enrichment for interactive backend

When the invocation context is autonomous-interactive, the runner SHALL prepend autonomy instructions to the step's system prompt before passing it to the adapter. The instructions SHALL direct the agent to work autonomously without asking for human input and to signal continuation when done using the same continuation mechanism that interactive steps use. The autonomy instructions SHALL be prepended before any engine enrichment or step-level system prompt content.

#### Scenario: Autonomy instructions prepended in autonomous-interactive context
- **WHEN** the runner prepares system prompt content for an autonomous-interactive step
- **THEN** the system prompt begins with autonomy instructions followed by any agent-level, step-level, and engine-provided content

#### Scenario: No continuation-signal instructions in autonomous-headless context
- **WHEN** the runner prepares system prompt content for an autonomous-headless step
- **THEN** the existing headless preamble is prepended as before, but no continuation-signal autonomy instructions are added (the headless backend exits on completion rather than signalling)

#### Scenario: No autonomy instructions in interactive context
- **WHEN** the runner prepares system prompt content for an interactive step
- **THEN** no autonomy instructions are prepended (the human supervises directly)

### Requirement: TTY fallback for autonomous-interactive

When the runner determines that the invocation context should be autonomous-interactive (based on the step mode and the `autonomous_backend` setting) but no TTY is available, the runner SHALL fall back to autonomous-headless for that step and SHALL log a warning indicating the fallback occurred and the reason. The fallback SHALL be per-step, not global — other steps in the same run that do have a TTY available (or that are already autonomous-headless) are unaffected.

#### Scenario: No TTY triggers fallback to headless
- **WHEN** the `autonomous_backend` setting is `interactive` and the runner is executing an autonomous step without a TTY (e.g., in CI or Docker)
- **THEN** the runner invokes the step as autonomous-headless and logs a warning

#### Scenario: TTY available uses interactive backend as configured
- **WHEN** the `autonomous_backend` setting is `interactive` and the runner is executing an autonomous step with a TTY available
- **THEN** the runner invokes the step as autonomous-interactive

#### Scenario: Fallback is per-step
- **WHEN** a run contains two autonomous steps, one with TTY available and one without
- **THEN** the step without TTY falls back to autonomous-headless while the step with TTY uses autonomous-interactive

#### Scenario: Interactive-claude backend with non-Claude adapter
- **WHEN** the `autonomous_backend` setting is `interactive-claude` and the adapter is Codex (not Claude)
- **THEN** the runner invokes the step as autonomous-headless regardless of TTY availability

