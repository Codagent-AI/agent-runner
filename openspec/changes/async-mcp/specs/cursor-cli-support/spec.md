## ADDED Requirements

### Requirement: Cursor interactive session ID discovery

In interactive mode, the Cursor adapter SHALL discover the session ID by scanning Cursor's local chat store for chats created after spawn whose workspace matches the invocation working directory. When exactly one matching chat remains after excluding known nested agent-call session IDs, the adapter SHALL return that chat ID. When zero or more than one matching chat remains, the adapter SHALL return the empty string rather than guess.

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
