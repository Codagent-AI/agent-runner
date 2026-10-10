## ADDED Requirements

### Requirement: Cursor interactive session ID discovery

In interactive mode, when no preset session ID is supplied, the Cursor adapter SHALL discover the session ID by scanning Cursor's local chat store for chats created after spawn whose workspace matches the invocation working directory. It SHALL first match chat metadata (`meta.json` creation time and working directory), allowing one second of timestamp tolerance, and SHALL fall back to chat stores (`store.db` or its write-ahead log) modified after spawn that record the invocation workspace path. Supplied excluded session IDs, such as known nested agent-call children, SHALL never be returned. When exactly one matching chat remains, the adapter SHALL return that chat ID. When zero or more than one matching chat remains, the adapter SHALL return the empty string rather than guess.

#### Scenario: Unique matching chat after spawn
- **WHEN** interactive Cursor discovery finds exactly one chat written after spawn for the invocation workspace
- **THEN** the adapter returns that chat's session ID

#### Scenario: Ambiguous chats without excludes
- **WHEN** interactive Cursor discovery finds two matching chats for the invocation workspace and no session IDs are excluded
- **THEN** the adapter returns the empty string

#### Scenario: Nested agent-call chats are excluded
- **WHEN** interactive Cursor discovery finds the parent chat and a nested agent-call chat for the same workspace
- **AND** the nested chat ID is supplied as an excluded session ID
- **THEN** the adapter returns the parent chat ID

### Requirement: Cursor agent-call wait budget

Because Cursor's MCP client aborts a `tools/call` at about 60 seconds regardless of progress notifications, a parent agent step running on the Cursor CLI SHALL receive a positive agent-call wait budget shorter than that abort, and `call_agent` and `get_agent_call` SHALL return a non-terminal snapshot when that budget expires. Parents on CLIs that hold a long `tools/call` open SHALL keep an unbounded budget.

#### Scenario: Cursor parent gets a bounded wait
- **WHEN** the parent agent step runs on the Cursor CLI and its child is still running
- **THEN** `call_agent` returns a `call_id` with a non-terminal status before the host abort

#### Scenario: Other parents wait for the child
- **WHEN** the parent agent step runs on a CLI that tolerates a long `tools/call`
- **THEN** `call_agent` holds the request open until the call is terminal
