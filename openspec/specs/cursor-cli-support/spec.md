# cursor-cli-support Specification

## Purpose
Define Cursor CLI adapter behavior for autonomous and interactive invocation, session resume, effort handling, and session discovery.
## Requirements
### Requirement: Cursor headless invocation

The Cursor adapter SHALL construct headless invocations using:

```
agent -p --output-format stream-json --trust [--force] [--model <m>] <prompt>
```

- `-p` selects non-interactive (print) mode.
- `--output-format stream-json` emits one JSON event per line on stdout, including a `session_id` field on every event. This format is required for session ID discovery; it is only valid together with `-p`.
- `--trust` trusts the current workspace without prompting; cursor only accepts this flag in `--print`/headless mode. `--trust` trusts the workspace but does NOT grant permission to run shell commands or other tools.
- `--force` (also known as `--yolo`) bypasses cursor's per-tool approval prompts and is required for the agent to execute shell, file, and network actions in headless mode. Its presence is governed by the `autonomous_permission_mode` setting defined by `user-settings-file`.
- `--model <m>` selects the model for fresh sessions; see the model and resume requirements below.

Cursor has no CLI equivalents for reasoning effort or tool-level restrictions, so those inputs do not produce flags.

#### Scenario: Fresh headless Cursor step

- **WHEN** the runner executes a headless step with `cli: cursor` and session strategy `new`
- **THEN** the adapter returns args beginning with `agent` and including `-p`, `--output-format stream-json`, `--trust`, and the prompt, and does not include `--resume`

#### Scenario: Headless conservative mode omits force

- **WHEN** the runner constructs args for any Cursor headless step and `autonomous_permission_mode` resolves to `conservative`
- **THEN** the args include `--trust` and do NOT include `--force`

#### Scenario: Headless yolo mode includes force

- **WHEN** the runner constructs args for any Cursor headless step and `autonomous_permission_mode` resolves to `yolo`
- **THEN** the args include both `--trust` and `--force`

#### Scenario: Output format is always stream-json

- **WHEN** the runner constructs args for any Cursor headless step
- **THEN** the args include `--output-format stream-json`

### Requirement: Cursor session resume

The Cursor adapter SHALL support session resume in headless mode by emitting `--resume=<session-id>`. On resume, the adapter SHALL NOT emit `--model` (a resumed cursor chat keeps the model it was started with).

#### Scenario: Headless Cursor step resumes prior session
- **WHEN** a Cursor headless step has session strategy `resume` and a session ID exists in state
- **THEN** the adapter invocation includes `--resume=<session-id>` and does not include a `--model` flag even if one is set on the profile

#### Scenario: Model specified on fresh Cursor step
- **WHEN** a fresh headless Cursor step has `model: gpt-5.3-codex`
- **THEN** the adapter includes `--model gpt-5.3-codex` in the invocation args

#### Scenario: Model specified on resumed Cursor step is omitted
- **WHEN** a resumed headless Cursor step has `model: gpt-5.3-codex`
- **THEN** the adapter does NOT include `--model` in the invocation args

### Requirement: Cursor effort values ignored

The Cursor CLI has no reasoning-effort flag. The adapter SHALL silently ignore any `effort` value provided by the runner.

#### Scenario: Effort level specified is not emitted
- **WHEN** a Cursor headless step has `effort: high`
- **THEN** the adapter does not include `--reasoning-effort`, `--effort`, or any similar flag in the invocation args

### Requirement: Cursor disallowed tools ignored

The Cursor CLI has no tool-level restriction flags. The adapter SHALL silently ignore any entries in `DisallowedTools`, including `"AskUserQuestion"`.

#### Scenario: DisallowedTools does not affect args
- **WHEN** the runner provides `DisallowedTools: ["AskUserQuestion"]` to the Cursor adapter
- **THEN** the adapter returns the same args it would have returned with an empty `DisallowedTools` list

### Requirement: Cursor does not support a native system prompt

The Cursor adapter SHALL report `SupportsSystemPrompt() == false` and SHALL NOT emit any system-prompt flag. Any `SystemPrompt` value on the input is ignored at the adapter layer; the runner's generic fallback (prepending system-prompt content to the user prompt) applies.

#### Scenario: SystemPrompt input is ignored by the adapter
- **WHEN** the runner calls `BuildArgs` with a non-empty `SystemPrompt`
- **THEN** the returned args contain no flag that carries the system-prompt content as a separate argument

### Requirement: Cursor session ID discovery

After a headless Cursor process exits, the adapter SHALL discover the session ID by parsing the captured process stdout for the first JSON object that contains a `session_id` string field, and SHALL return that value. If no such object is found (e.g. the process exited before emitting any event, or output is corrupt), the adapter SHALL return the empty string.

Parsing SHALL tolerate:
- lines that are not valid JSON (skip and continue to the next line)
- JSON objects that do not contain `session_id` (skip and continue)
- trailing whitespace and blank lines

The adapter SHALL NOT depend on any specific `type` or `subtype` value — any event that carries a `session_id` is a valid source.

#### Scenario: Session ID discovered from stream-json init event
- **WHEN** the captured stdout's first JSON line is `{"type":"system","subtype":"init","session_id":"chat-abc-123","model":"composer-1.5","cwd":"/tmp","permissionMode":"default"}`
- **THEN** the adapter returns `"chat-abc-123"`

#### Scenario: Session ID discovered from a later event when earlier lines lack it
- **WHEN** the captured stdout starts with a non-JSON log line followed by `{"type":"assistant","session_id":"chat-xyz","message":{}}`
- **THEN** the adapter returns `"chat-xyz"`

#### Scenario: No session ID in output
- **WHEN** the captured stdout contains no JSON object with a `session_id` field
- **THEN** the adapter returns the empty string

#### Scenario: Empty output
- **WHEN** the captured stdout is empty
- **THEN** the adapter returns the empty string

### Requirement: Cursor registered as a known CLI

The adapter registry SHALL expose `cursor` as a resolvable adapter name, and the configuration validator SHALL accept `cli: cursor` in agent-profile and workflow configs.

#### Scenario: Adapter registry resolves cursor
- **WHEN** code calls `cli.Get("cursor")`
- **THEN** a non-nil adapter is returned with no error

#### Scenario: Adapter registry lists cursor
- **WHEN** code calls `cli.KnownCLIs()`
- **THEN** the returned slice contains `"cursor"` alongside `"claude"`, `"codex"`, and `"copilot"`

#### Scenario: Config accepts cli: cursor
- **WHEN** a configuration file sets `cli: cursor` on an agent profile
- **THEN** configuration validation succeeds and the error message listing valid CLIs (when some other invalid value is used) mentions `cursor`

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

