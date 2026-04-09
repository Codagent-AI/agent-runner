## ADDED Requirements

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
