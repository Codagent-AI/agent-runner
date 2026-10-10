## ADDED Requirements

### Requirement: Headless Claude runs without background tasks

When the Claude adapter builds a headless invocation, it SHALL contribute `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` to the spawned process's environment. It SHALL also contribute `BASH_DEFAULT_TIMEOUT_MS=600000` unless the runner's own environment already defines `BASH_DEFAULT_TIMEOUT_MS`, in which case the inherited value SHALL be left unchanged. These entries SHALL apply to every headless Claude invocation the runner spawns, including workflow agent steps, inline repair agents, and `call_agent` children. They SHALL apply only to the spawned process and MUST NOT modify the runner's own environment or any user, project, or global Claude configuration. Interactive Claude invocations SHALL NOT receive these entries.

#### Scenario: Headless agent step disables background tasks
- **WHEN** an autonomous headless Claude agent step is spawned and the runner's environment does not define `BASH_DEFAULT_TIMEOUT_MS`
- **THEN** the spawned process's environment contains `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` and `BASH_DEFAULT_TIMEOUT_MS=600000`

#### Scenario: Inherited default Bash timeout is preserved
- **WHEN** a headless Claude invocation is spawned and the runner's environment defines `BASH_DEFAULT_TIMEOUT_MS=900000`
- **THEN** the spawned process's environment contains `BASH_DEFAULT_TIMEOUT_MS=900000` and `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`

#### Scenario: Inherited background-task setting is overridden
- **WHEN** a headless Claude invocation is spawned and the runner's environment defines `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=0`
- **THEN** the spawned process's environment contains `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`

#### Scenario: Agent-call child on Claude
- **WHEN** a parent agent's `call_agent` request spawns a headless Claude child
- **THEN** the child process's environment contains `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1`

#### Scenario: Interactive Claude step is unchanged
- **WHEN** an interactive Claude agent step is spawned
- **THEN** the adapter contributes neither `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS` nor `BASH_DEFAULT_TIMEOUT_MS`

#### Scenario: No persistent configuration changes
- **WHEN** a headless Claude invocation is spawned with these entries
- **THEN** the runner's own environment and all user, project, and global Claude settings files are unchanged

#### Scenario: Other adapters are unchanged
- **WHEN** a headless Codex, Cursor, Copilot, or OpenCode invocation is spawned
- **THEN** the adapter contributes neither `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS` nor `BASH_DEFAULT_TIMEOUT_MS`
