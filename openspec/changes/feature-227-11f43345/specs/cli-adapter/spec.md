## ADDED Requirements

### Requirement: Additional workspace directories

When the runner supplies one or more additional workspace directories for an agent step, which happens when a built-in OpenSpec workflow runs with an external spec root, each adapter SHALL include those directories in the CLI's workspace using the CLI's add-directory mechanism (for example, `--add-dir <path>` for Claude, Codex, and Copilot). This lets the agent read and write there under the same permission rules that apply to the working directory. Adding a directory SHALL NOT emit any flag that auto-approves tools, paths, URLs, or commands beyond what the adapter already emits for that invocation context and permission mode. The "no permission loosening in interactive mode" requirement still applies. When no additional directories are supplied, the adapter's args SHALL be unchanged. If the adapter's CLI has no add-directory mechanism, the step SHALL fail before the CLI starts, with an error naming the CLI and the directory, unless the adapter's existing permission mode already permits writes outside the working directory.

The Claude, Codex, and Copilot adapters SHALL use `--add-dir`. The OpenCode adapter SHALL treat additional directories as already permitted, because the runner applies no filesystem confinement to OpenCode, and SHALL emit no flag for them. The Cursor adapter SHALL report that it cannot add directories. The runner SHALL supply as additional directories the workspace directories of the step's enclosing workflows, as defined in `sub-workflows`.

#### Scenario: Codex headless step with an external spec root
- **WHEN** an autonomous-headless Codex step runs in conservative mode with additional directory `/work/specs`
- **THEN** the args include `--sandbox workspace-write` and an add-directory entry for `/work/specs`, and no broader sandbox flag

#### Scenario: Interactive Claude step with an external spec root
- **WHEN** an interactive Claude step runs with additional directory `/work/specs`
- **THEN** the args include `--add-dir /work/specs` and no flag that auto-approves edits

#### Scenario: No additional directories
- **WHEN** an agent step runs with no additional directories
- **THEN** the adapter's args are identical to its args before this change

#### Scenario: OpenCode step with an additional directory
- **WHEN** an OpenCode step runs with additional directory `/work/specs`
- **THEN** the step runs, and the adapter's args are identical to its args without additional directories

#### Scenario: CLI cannot add a directory
- **WHEN** a step with an additional directory resolves to an adapter whose CLI has no add-directory mechanism and whose permission mode confines writes to the working directory
- **THEN** the step fails before spawning the CLI with an error naming the CLI and the directory
