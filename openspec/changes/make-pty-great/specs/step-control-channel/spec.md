# Capability: step-control-channel

## ADDED Requirements

### Requirement: Per-run control endpoint

Before spawning an interactive or autonomous-interactive agent step, the runner SHALL ensure a private, per-run local control endpoint exists, accessible only to the local user. The endpoint's address and the step's completion credential SHALL be exposed to the child process through environment variables. The endpoint SHALL be closed and removed when the run ends. If the endpoint cannot be created, the step SHALL fail before the CLI process is spawned, with a descriptive error.

<!-- deferred-to-design: exact transport (e.g., Unix domain socket path) and environment variable names -->

#### Scenario: Control environment variables present in the child
- **WHEN** the runner spawns the CLI process for an interactive agent step
- **THEN** the child's environment contains the control endpoint address and the current step attempt's completion credential

#### Scenario: Endpoint creation failure fails the step
- **WHEN** the runner cannot create the control endpoint for a run
- **THEN** the interactive step fails with a descriptive error before the CLI process is spawned

#### Scenario: Endpoint is private to the local user
- **WHEN** the control endpoint exists for a run
- **THEN** it is not accessible to other users of the machine

### Requirement: Per-step completion credential

Each interactive step attempt SHALL receive a fresh single-use completion credential. The runner SHALL accept a completion event only when it carries the credential issued for the currently running step attempt. Completion events carrying a stale credential (from an earlier attempt), an unknown credential, or a malformed payload SHALL be rejected without advancing the workflow, and each rejection SHALL be recorded in the audit log.

#### Scenario: Current credential accepted
- **WHEN** a completion event arrives carrying the credential issued for the currently running step attempt
- **THEN** the runner accepts the event and completes the step

#### Scenario: Stale credential rejected
- **WHEN** a completion event arrives carrying a credential issued for a previous step attempt
- **THEN** the runner rejects the event, does not advance the workflow, and records the rejection in the audit log

#### Scenario: Malformed event rejected
- **WHEN** a connection to the control endpoint delivers a payload that is not a well-formed completion event
- **THEN** the runner rejects it, does not advance the workflow, and records the rejection in the audit log

### Requirement: Completion event semantics

A valid completion event SHALL mark the currently running interactive step as `success`, trigger graceful termination of the CLI process (per `interactive-terminal-handoff`), and advance the workflow. Completion events are success-only: the event carries no outcome parameter. Duplicate completion events after the first accepted one SHALL be ignored. A completion event arriving when no interactive step is running SHALL be rejected.

#### Scenario: Agent-initiated completion advances the workflow
- **WHEN** the agent sends a valid completion event during an interactive step
- **THEN** the step outcome is `success`, the CLI is terminated gracefully, and the workflow advances to the next step

#### Scenario: Duplicate completion event ignored
- **WHEN** a second completion event arrives after one has already been accepted for the current step
- **THEN** the runner ignores it and the workflow state is unaffected

#### Scenario: Completion event with no active interactive step
- **WHEN** a completion event arrives while no interactive step is running
- **THEN** the runner rejects it and the workflow state is unaffected

### Requirement: In-session completion command

The runner binary SHALL provide a `step complete` subcommand that reads the control endpoint address and completion credential from the environment and sends a completion event. The command is in-session only: when the control environment variables are absent, it SHALL exit non-zero with a message explaining that it must be run from within an interactive agent step session. The command SHALL NOT provide cross-terminal run targeting.

#### Scenario: Command run inside an interactive step session
- **WHEN** `agent-runner step complete` runs in an environment containing the control variables for the currently running step attempt
- **THEN** a completion event is sent and the workflow advances with outcome `success`

#### Scenario: Command run outside a step session
- **WHEN** `agent-runner step complete` runs in an environment without the control variables
- **THEN** the command exits non-zero with a message explaining it must be run from within an interactive agent step session

### Requirement: Completion instruction injection

The runner SHALL append completion instructions to the prompt for every interactive and autonomous-interactive agent step, telling the agent how to signal step completion through the control channel. Headless agent steps SHALL NOT receive completion instructions. These instructions replace the retired sentinel instructions.

#### Scenario: Interactive step receives completion instructions
- **WHEN** the runner builds the prompt for an interactive agent step
- **THEN** the prompt includes instructions telling the agent how to signal completion through the control channel

#### Scenario: Autonomous-interactive step receives completion instructions
- **WHEN** the runner builds the prompt for an autonomous-interactive agent step
- **THEN** the prompt includes the same completion instructions

#### Scenario: Headless step receives no completion instructions
- **WHEN** the runner builds the prompt for a headless agent step
- **THEN** the prompt does not include completion instructions

### Requirement: Universal completion surface

For every interactive or autonomous-interactive agent step, regardless of which CLI backs it, the injected completion instructions combined with the in-session `step complete` command SHALL be sufficient for the agent to signal completion. No step SHALL depend on CLI-specific byte-stream behavior to advance. CLI-native completion surfaces (such as a user-invocable `/next`-style command or a tool exposed to the agent) MAY additionally exist, and any such surface SHALL route through the same control channel with the same credential validation.

#### Scenario: Any registered CLI can complete a step
- **WHEN** an interactive agent step runs with any registered CLI and the agent follows the injected completion instructions
- **THEN** the completion event is delivered through the control channel and the workflow advances with outcome `success`

#### Scenario: Native surface routes through the control channel
- **WHEN** a CLI-native completion surface exists and the user or agent invokes it (for example, typing `/next`)
- **THEN** the completion event is delivered through the control channel and validated against the current step credential, identically to the in-session command

<!-- deferred-to-design: which CLIs get native completion surfaces and how they are packaged and distributed -->
