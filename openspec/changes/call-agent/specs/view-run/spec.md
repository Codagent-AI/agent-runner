## ADDED Requirements

### Requirement: Agent-call hierarchy rendering

A parent agent row with accepted agent calls SHALL display its call count and become expandable through the existing nested-row interaction. When expanded, each call SHALL appear as a dynamic child execution row rather than a workflow step, ordered by invocation time. A named-session target SHALL be labeled `call session: <name>` and a profile target SHALL be labeled `call agent: <profile>`. Each call row SHALL display its own status and an agent-call type glyph; the exact glyph is a design choice.

#### Scenario: Parent displays call count
- **WHEN** a parent agent attempt has two accepted calls
- **THEN** its row displays `(2 calls)`

#### Scenario: Expanded parent shows chronological calls
- **WHEN** the user expands a parent with multiple calls
- **THEN** the run view shows one child execution row per call in invocation order

#### Scenario: Target form is explicit
- **WHEN** one call targets a named session and another targets an agent profile
- **THEN** their rows use `call session: <name>` and `call agent: <profile>` labels respectively

#### Scenario: Call status is independent
- **WHEN** a parent recovers from a failed call and later succeeds
- **THEN** the failed call remains visible with failed status beneath the successful parent

#### Scenario: Repeated target calls remain distinct
- **WHEN** a parent calls the same target multiple times
- **THEN** each invocation appears as a separate child row

#### Scenario: Inspect reconstructs call hierarchy
- **WHEN** a completed run containing agent calls is opened for inspection
- **THEN** the run view reconstructs the parent call count and child rows from persisted run evidence

### Requirement: Agent-call detail and resume

Selecting an agent-call row SHALL show the target kind and name; resolved profile, CLI, model, session metadata, and working directory; prompt, outcome, duration, usage, cost, and error; and stdout and stderr retained through ordinary headless-agent output behavior. The detail view MUST NOT reconstruct full output from `audit.log`.

When the run is inactive, a completed called-agent execution with a known CLI session ID SHALL offer the existing direct session-resume action. The action MUST NOT be available while the run is active or when no session ID is known.

#### Scenario: Selected call shows execution detail
- **WHEN** the user selects a running or completed agent-call row
- **THEN** the detail pane shows the call's target, resolved agent metadata, prompt, status, timing, metrics, and error information available for that execution

#### Scenario: Persisted call output is displayed
- **WHEN** ordinary headless-agent output persistence created stdout or stderr files for the selected call
- **THEN** the detail pane displays that persisted output

#### Scenario: Audit metadata is not treated as full output
- **WHEN** no persisted output exists for a selected call
- **THEN** the run view does not reconstruct or display a full child response from `audit.log`

#### Scenario: Inactive call session can be resumed
- **WHEN** the run is inactive and the selected completed call has a known CLI session ID
- **THEN** the existing direct resume action is available for that called-agent session

#### Scenario: Resume unavailable during active run
- **WHEN** the run is active or the selected call has no known CLI session ID
- **THEN** the direct resume action is unavailable for that call
