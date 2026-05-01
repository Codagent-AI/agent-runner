# cursor-cli-support Specification

## Purpose
TBD - created by archiving change cursor-support. Update Purpose after archive.
## Requirements
### Requirement: Cursor headless invocation

The Cursor adapter SHALL construct headless invocations using:

```
agent -p --output-format stream-json --force --trust [--model <m>] <prompt>
```

- `-p` selects non-interactive (print) mode.
- `--output-format stream-json` emits one JSON event per line on stdout, including a `session_id` field on every event. This format is required for session ID discovery; it is only valid together with `-p`.
- `--force` allows all tool calls unless explicitly denied — the autonomous-operation analogue of copilot's `--allow-all`.
- `--trust` trusts the current workspace without prompting; cursor only accepts this flag in `--print`/headless mode.
- `--model <m>` selects the model for fresh sessions; see the model and resume requirements below.

Cursor has no CLI equivalents for reasoning effort or tool-level restrictions, so those inputs do not produce flags.

#### Scenario: Fresh headless Cursor step
- **WHEN** the runner executes a headless step with `cli: cursor` and session strategy `new`
- **THEN** the adapter returns args beginning with `agent` and including `-p`, `--output-format stream-json`, `--force`, `--trust`, and the prompt, and does not include `--resume`

#### Scenario: Headless autonomy flags required
- **WHEN** the runner constructs args for any Cursor headless step (fresh or resumed)
- **THEN** the args include both `--force` and `--trust` regardless of other options

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

