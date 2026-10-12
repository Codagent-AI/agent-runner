# Capability: step-control-channel

## ADDED Requirements

### Requirement: Per-run control endpoint

Before spawning an interactive or autonomous-interactive agent step, the runner SHALL ensure a private, per-run local control endpoint exists, accessible only to the local user. The endpoint's address and the step's completion credential SHALL be exposed to the child process through environment variables. The endpoint SHALL be closed and removed when the run ends. If the endpoint cannot be created, the step SHALL fail before the CLI process is spawned, with a descriptive error.

<!-- resolved-in-design: Unix domain socket in a user-private short-path directory; env vars AGENT_RUNNER_CONTROL_SOCKET / AGENT_RUNNER_RUN_ID / AGENT_RUNNER_STEP_ID / AGENT_RUNNER_CONTROL_TOKEN — see design.md "Control plane" -->

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

Each interactive step attempt SHALL receive a fresh single-use completion credential. The runner SHALL accept a completion event only when it carries the credential issued for the currently running step attempt. Completion events carrying a stale credential (from an earlier attempt), an unknown credential, or a malformed payload SHALL be rejected without advancing the workflow, and each rejection SHALL be recorded in the audit log. Single-use applies to completion acceptance: after a completion event is accepted, further completion events are handled per "Completion event semantics", while the same attempt credential SHALL remain valid for authenticating turn-durability evidence (such as a committed-turn signal) until the step concludes.

#### Scenario: Current credential accepted
- **WHEN** a completion event arrives carrying the credential issued for the currently running step attempt
- **THEN** the runner accepts the event and completes the step

#### Scenario: Stale credential rejected
- **WHEN** a completion event arrives carrying a credential issued for a previous step attempt
- **THEN** the runner rejects the event, does not advance the workflow, and records the rejection in the audit log

#### Scenario: Malformed event rejected
- **WHEN** a connection to the control endpoint delivers a payload that is not a well-formed completion event
- **THEN** the runner rejects it, does not advance the workflow, and records the rejection in the audit log

#### Scenario: Committed-turn evidence accepted after completion
- **WHEN** a committed-turn signal carrying the current attempt's credential arrives after the completion event was accepted but before the step concludes
- **THEN** the runner accepts it as turn-durability evidence rather than rejecting the credential as consumed

### Requirement: Completion event semantics

A valid completion event SHALL mark the currently running interactive step as `success`, conclude the completion handshake (see "Completion acknowledgement and turn durability"), trigger graceful termination of the CLI process (per `interactive-terminal-handoff`), and advance the workflow. Completion events are success-only: the event carries no outcome parameter. Duplicate completion events after the first accepted one SHALL be ignored. A completion event arriving when no interactive step is running SHALL be rejected.

#### Scenario: Agent-initiated completion advances the workflow
- **WHEN** the agent sends a valid completion event during an interactive step
- **THEN** the step outcome is `success`, the CLI is terminated gracefully, and the workflow advances to the next step

#### Scenario: Duplicate completion event ignored
- **WHEN** a second completion event arrives after one has already been accepted for the current step
- **THEN** the runner ignores it and the workflow state is unaffected

#### Scenario: Completion event with no active interactive step
- **WHEN** a completion event arrives while no interactive step is running
- **THEN** the runner rejects it and the workflow state is unaffected

### Requirement: Completion acknowledgement and turn durability

Because the completion event is sent from inside the agent's own turn, accepting it SHALL NOT immediately terminate the CLI, and acceptance SHALL be recorded as an intermediate audit event only — never as step success. The runner SHALL first acknowledge the accepted event to the submitting client, so the completion invocation can return to the agent. The runner SHALL then wait for semantic evidence that the CLI's turn is durably committed (an adapter-supplied committed-turn confirmation — a native post-turn signal or an explicitly completed assistant turn recorded in the CLI's native session store after acceptance; never file-write timing alone), bounded by a timeout that counts active runtime only (the clock pauses while the child is suspended). On confirmation, the runner terminates the CLI gracefully and records the step outcome as `success`. If the bound elapses without confirmation, the runner SHALL record a durability failure in the audit log — naming the CLI, session ID, timeout, and inspected artifact — terminate the CLI gracefully, record the step outcome as `failed`, and stop the workflow. If the CLI process exits after acceptance but before confirmation, the runner SHALL continue seeking committed-turn evidence from the CLI's native session store for the remainder of the bound (a post-turn signal can no longer arrive from an exited process); confirmation yields `success`, and an elapsed bound follows the durability-failure path. A resumed workflow issues a fresh attempt credential and retries the step normally.

<!-- resolved-in-design: TurnDurabilityProbe interface (Checkpoint / WaitForCommittedTurn), per-CLI evidence hierarchy, 30s active-runtime bound — see design.md "Turn durability" and the five-CLI completion matrix -->

#### Scenario: Client receives acknowledgement before termination begins
- **WHEN** a valid completion event is accepted
- **THEN** the submitting client receives a success acknowledgement before any termination signal is sent to the CLI's process group

#### Scenario: Session resumable after completion
- **WHEN** an interactive step completes via the control channel and a later step resumes the same agent session
- **THEN** the resumed session includes the turn during which the completion event was sent

#### Scenario: Durability confirmation times out
- **WHEN** a completion event has been accepted and no committed-turn confirmation arrives within the active-runtime bound
- **THEN** the runner records a durability failure naming the CLI, session ID, timeout, and inspected artifact, terminates the CLI gracefully, records the step outcome as `failed`, and stops the workflow

#### Scenario: CLI exits before durability is confirmed
- **WHEN** the CLI process exits after a completion event was accepted but before committed-turn confirmation
- **THEN** the runner continues the durability check against the CLI's native session store for the remainder of the bound, and the step outcome is `success` on confirmation or follows the durability-failure path otherwise

#### Scenario: Resumed workflow retries after durability failure
- **WHEN** a run whose step failed on durability timeout is resumed
- **THEN** the step runs a fresh attempt with a newly issued completion credential

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

For every interactive or autonomous-interactive agent step, regardless of which CLI backs it, the agent SHALL have a working way to signal completion through the control channel. No step SHALL depend on CLI-specific byte-stream behavior to advance. The injected completion instructions SHALL reference the completion client by absolute path rather than assuming the runner binary is on the agent's PATH. In interactive context, the completion command MAY be gated by the CLI's normal supervised approval prompt when the adapter has no safe narrow pre-approval (per `cli-adapter`). For autonomous-interactive steps, the completion path SHALL NOT silently block on human permission approval: the adapter SHALL provide an approval-free path (narrow pre-approval, its autonomous permission flags, or a native integration), or the step SHALL fail early with a clear explanation that unattended completion requires one. CLI-native completion surfaces (such as `/agent-runner:next` or `$agent-runner-next`) MAY additionally exist, and any such surface SHALL route through the same control channel with the same credential validation.

<!-- resolved-in-design: absolute-path shell client with per-CLI narrow pre-approval (see the cli-adapter delta spec in this change), proven per CLI in the blocking Phase 0 feasibility batch — see design.md "Five-CLI completion matrix" -->

#### Scenario: Any registered CLI can complete a step
- **WHEN** an interactive agent step runs with any registered CLI and the agent follows the injected completion instructions
- **THEN** the completion event is delivered through the control channel and the workflow advances with outcome `success`

#### Scenario: Instructions do not depend on PATH
- **WHEN** the runner injects completion instructions for an interactive agent step
- **THEN** the instructions reference the completion client by absolute path

#### Scenario: Autonomous-interactive completion does not block on approval
- **WHEN** an autonomous-interactive step runs with an approval-free completion path and its agent signals completion through that surface
- **THEN** the completion is delivered without waiting for human permission approval

#### Scenario: Autonomous-interactive without an approval-free path fails early
- **WHEN** an autonomous-interactive step starts with a CLI configuration that provides no approval-free completion path
- **THEN** the step fails early with a clear explanation instead of hanging while awaiting human approval

#### Scenario: Native surface routes through the control channel
- **WHEN** a CLI-native completion surface exists and the user or agent invokes it (for example, `/agent-runner:next` or `$agent-runner-next`)
- **THEN** the completion event is delivered through the control channel and validated against the current step credential, identically to the in-session command
