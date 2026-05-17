## MODIFIED Requirements

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

## ADDED Requirements

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
