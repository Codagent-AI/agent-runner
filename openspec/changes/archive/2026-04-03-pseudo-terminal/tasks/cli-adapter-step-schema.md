# Task: CLI adapter and step schema

## Goal

Introduce a CLI adapter abstraction so Agent Runner can invoke Claude and Codex through a uniform interface, add per-step CLI selection via a `cli` field on the step schema, replace the current hardcoded Claude invocation and heuristic session discovery with adapter-based dispatch, remove the signal file mechanism, and update docs.

## Background

Agent Runner currently hardcodes Claude CLI invocation in `internal/exec/agent.go`. The function `buildAgentArgs()` constructs `claude --resume <id> --model <m> -p <prompt>` directly, and `findConversationID()` discovers session IDs by scanning `~/.claude/projects/` for the most recently modified `.jsonl` file — a heuristic that's subject to race conditions. Adding Codex as a second backend requires abstracting invocation, arg construction, and session discovery.

**CLI adapter package (`internal/cli`):** Create a new package with an `Adapter` interface that has two methods: `BuildArgs` (receives prompt, mode, session ID, and model; returns the full command and args) and `DiscoverSessionID` (returns a session ID after the CLI exits). A hard-coded registry map populated at init with `"claude"` and `"codex"` entries provides a `Get(name string) (Adapter, error)` lookup function.

**Claude adapter** arg patterns: fresh interactive uses `claude --session-id <uuid> <prompt>`, fresh headless uses `claude --session-id <uuid> -p <prompt>`, resume interactive uses `claude --resume <uuid> <prompt>`, resume headless uses `claude --resume <uuid> -p <prompt>`. Model override appends `--model <m>`. Session discovery is deterministic: the runner generates a UUID upfront and passes it via `--session-id`; the adapter returns this same UUID.

**Codex adapter** arg patterns: fresh interactive uses `codex --no-alt-screen <prompt>`, fresh headless uses `codex exec --json <prompt>`, resume interactive uses `codex resume --no-alt-screen <uuid> <prompt>`, resume headless uses `codex exec resume <uuid> <prompt>`. Model override appends `-m <m>`. The `--no-alt-screen` flag is always required for interactive mode. Session discovery for headless parses the `thread_id` from the first JSONL event (`{"type":"thread.started","thread_id":"<uuid>"}`). Session discovery for interactive scans `~/.codex/sessions/YYYY/MM/DD/` for the most recent `.jsonl` file created after spawn time, matching CWD from the `session_meta` payload.

**Step schema changes:** Add a `CLI string` field to `Step` in `internal/model/step.go` (`yaml:"cli,omitempty"`). Remove the `Agent string` field from `Workflow`. Remove the `AgentCmd` field from `ExecutionContext` in `internal/model/context.go` and its propagation in `NewLoopIterationContext()` and `NewSubWorkflowContext()`. Remove `ctx.AgentCmd = workflow.Agent` in `internal/runner/runner.go` (line 181).

**Validation:** `Step.Validate()` must reject `cli` on shell steps (same pattern as `model` validation) and validate that the `cli` value is a known adapter name. To avoid importing `internal/cli` from the model package, pass the list of known adapter names as a `[]string` to the validator.

**Executor changes:** Rewrite `ExecuteAgentStep` in `internal/exec/agent.go` to resolve the CLI adapter from `step.CLI` (default `"claude"`), call `adapter.BuildArgs(...)` instead of `buildAgentArgs()`, and call `adapter.DiscoverSessionID(...)` instead of `findConversationID()`. For headless steps, use `runner.RunAgent(args)` then discover session ID. For interactive steps, the executor must also use the adapter for arg construction and session discovery. The interactive execution mechanism itself (how the process is spawned and how continue is detected) is out of scope for this task — preserve existing interactive behavior while routing through the adapter.

**Removals:** Remove `buildAgentArgs()`, `findConversationID()`, `encodeCwd()`, `discoverAndStoreSession()`. Remove signal file code: `cleanSignalFile()`, `readSignalAction()`, `waitForSignalOrExit()`, and the `signalFile` constant. Remove `StartAgent` from the `ProcessRunner` interface in `internal/exec/interfaces.go`.

**Workflow YAML updates:** Remove the `agent: claude` line from `workflows/implement-change.yaml`, `workflows/plan-change.yaml`, `workflows/implement-task.yaml`, and `workflows/run-gauntlet.yaml`.

**Documentation updates:** Update `README.md` Features section to mention multi-CLI support and Codex. Update `docs/USER-GUIDE.md` "Step modes > Interactive" section to remove references to `/continue` writing a signal file and the `.agent-runner-signal` mechanism. Add a section on per-step CLI override (`cli` field). Update the model override section to mention it goes through the CLI adapter.

**Test updates:** Rewrite `internal/exec/agent_test.go` arg-building tests for Claude adapter's `BuildArgs`. Add tests for Codex adapter's `BuildArgs` covering all command variations. Test the registry (known CLI resolves, unknown CLI returns error) and validation (`cli` on shell step rejected, unknown `cli` value rejected at load time).

## Spec

### Requirement: CLI adapter registry

The runner SHALL maintain a hard-coded registry of known CLI adapters. Each adapter SHALL be identified by a string key (e.g., `claude`, `codex`). The registry is compile-time — adding a new CLI requires a code change.

#### Scenario: Known CLI resolved
- **WHEN** a step specifies `cli: claude`
- **THEN** the runner resolves the Claude adapter from the registry

#### Scenario: Unknown CLI requested
- **WHEN** a step specifies a `cli` value not in the registry
- **THEN** the runner fails at load time with a validation error indicating the CLI is not supported

### Requirement: Adapter arg construction

Each adapter SHALL construct the CLI invocation args for both headless and interactive modes. The adapter receives the prompt, session ID (if resuming), and model override (if specified), and returns the full command and args.

#### Scenario: Headless invocation with model override
- **WHEN** the runner executes a headless step with `model: sonnet` and a session ID from state
- **THEN** the adapter returns args that include the prompt, model flag, session resume flag, and headless flag appropriate to that CLI

#### Scenario: Interactive invocation with no session
- **WHEN** the runner executes an interactive step with session strategy `new`
- **THEN** the adapter returns args for a fresh interactive session (no resume flag)

### Requirement: Adapter session ID return

After a CLI process exits, the adapter SHALL return a session ID. The runner stores this ID in state.json for future resume. How the adapter obtains the session ID is adapter-specific.

#### Scenario: Session ID returned after first run
- **WHEN** a CLI step completes (fresh session, no prior session ID)
- **THEN** the adapter returns a session ID and the runner stores it in state

#### Scenario: Session ID returned after resumed run
- **WHEN** a CLI step completes after resuming a prior session
- **THEN** the adapter returns the session ID (which may be the same or updated) and the runner stores it in state

#### Scenario: Session ID unavailable
- **WHEN** a CLI step completes but the adapter cannot determine the session ID
- **THEN** the adapter returns empty and the runner logs a warning; future resume for this step is not possible

### Requirement: Codex interactive invocation

The Codex adapter SHALL always include the `--no-alt-screen` flag when constructing args for interactive mode steps. This prevents Codex from using the alternate screen buffer.

#### Scenario: Interactive Codex step
- **WHEN** the runner executes an interactive step with `cli: codex`
- **THEN** the adapter includes `--no-alt-screen` in the invocation args

#### Scenario: Headless Codex step
- **WHEN** the runner executes a headless step with `cli: codex`
- **THEN** the adapter does not include `--no-alt-screen`

### Requirement: Codex model override

The Codex adapter SHALL support per-step model overrides. When a step specifies a `model` field, the adapter SHALL pass it to the Codex CLI.

#### Scenario: Model specified on Codex step
- **WHEN** a Codex step has `model: o3`
- **THEN** the adapter passes the model flag to the Codex invocation

#### Scenario: No model on Codex step
- **WHEN** a Codex step has no `model` field
- **THEN** the adapter invokes Codex without a model flag, using its default

### Requirement: Codex session resume

The Codex adapter SHALL support session resume. For interactive mode, the adapter SHALL use `codex resume --no-alt-screen <session-id> <prompt>`. For headless mode, the adapter SHALL use `codex exec resume <session-id> <prompt>`.

#### Scenario: Codex interactive step resumes prior session
- **WHEN** a Codex interactive step has session strategy `resume` and a session ID exists in state
- **THEN** the adapter invokes `codex resume --no-alt-screen <session-id> <prompt>`

#### Scenario: Codex headless step resumes prior session
- **WHEN** a Codex headless step has session strategy `resume` and a session ID exists in state
- **THEN** the adapter invokes `codex exec resume <session-id> <prompt>`

### Requirement: Codex session discovery

After a Codex step completes, the adapter SHALL return a session ID. For headless mode, the adapter SHALL parse the `thread_id` from the `thread.started` JSONL event emitted by `codex exec --json`. For interactive mode, the adapter SHALL scan `~/.codex/sessions/` for the most recent session file created after the step's spawn time, matching on CWD from the `session_meta` payload.

#### Scenario: Codex headless session ID from JSONL
- **WHEN** a headless Codex step completes
- **THEN** the adapter parses the `thread_id` from the `thread.started` event in the JSONL output

#### Scenario: Codex interactive session ID from filesystem
- **WHEN** an interactive Codex step completes
- **THEN** the adapter scans `~/.codex/sessions/` for the most recent file created after spawn time and extracts the session ID from the `session_meta` payload, matching on CWD

### Requirement: Per-step model override

A step MAY include a `model` field specifying which model the agent should use. When present, the runner SHALL pass the model to the CLI adapter for inclusion in the invocation args. When absent, the CLI uses its default model. The `model` field is only valid on agent steps (headless or interactive), not shell steps.

#### Scenario: Model specified on agent step
- **WHEN** a headless step has `model: sonnet`
- **THEN** the runner passes the model to the CLI adapter for invocation

#### Scenario: No model specified
- **WHEN** a step does not have a `model` field
- **THEN** the runner invokes the CLI adapter without a model override, using the CLI's default

#### Scenario: Model on shell step
- **WHEN** a shell step has a `model` field
- **THEN** the runner fails at load time with a validation error

### Requirement: Per-step CLI override

A step MAY include a `cli` field specifying which CLI backend to use (e.g., `claude`, `codex`). When absent, the runner SHALL default to `claude`. The `cli` field is only valid on agent steps, not shell steps.

#### Scenario: CLI specified on agent step
- **WHEN** an agent step has `cli: codex`
- **THEN** the runner uses the Codex adapter for that step

#### Scenario: CLI not specified
- **WHEN** an agent step has no `cli` field
- **THEN** the runner uses the Claude adapter (hard-coded default)

#### Scenario: CLI on shell step
- **WHEN** a shell step has a `cli` field
- **THEN** the runner fails with a validation error

### Requirement: Agent step execution dispatch

The runner's agent step executor SHALL delegate CLI invocation to the resolved CLI adapter. Interactive steps SHALL execute via the PTY layer. Headless steps SHALL execute via direct process execution. Both paths use the adapter for arg construction.

#### Scenario: Headless step executes via direct exec
- **WHEN** the runner executes a headless agent step
- **THEN** the executor delegates arg construction to the CLI adapter and launches the process via direct exec

Note: The interactive PTY execution path scenario ("Interactive step executes via PTY") becomes fully verifiable once the PTY task is also complete. This task covers adapter dispatch for both paths but only fully implements the headless path.

## Done When

- `internal/cli` package exists with `Adapter` interface, Claude adapter, and Codex adapter
- Adapter registry resolves known CLIs and returns errors for unknown CLIs
- `Step.CLI` field is parsed from YAML and validated at load time
- `Workflow.Agent` and `ExecutionContext.AgentCmd` are removed
- `ExecuteAgentStep` uses the adapter for arg construction and session discovery
- Signal file code is fully removed from `internal/exec/agent.go`
- All workflow YAML files have `agent:` removed
- `README.md` and `docs/USER-GUIDE.md` are updated
- Tests cover adapter arg construction (both CLIs), registry resolution, and validation
