## MODIFIED Requirements

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
