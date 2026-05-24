## MODIFIED Requirements

### Requirement: Copilot headless invocation

The Copilot adapter SHALL construct headless invocations using:

```
copilot -p <prompt> --allow-tool=write --autopilot -s [--allow-all-tools] [--model <m>] [--reasoning-effort <e>]
```

- `--allow-tool=write` grants the write permission required for autonomous workspace edits.
- `--autopilot` keeps the agent running until the task is complete.
- `--allow-all-tools` (or Copilot's equivalent broadest-authority flag, subject to implementation verification against the Copilot CLI) is the optional yolo-mode flag. Its presence is governed by the `autonomous_permission_mode` setting defined by `user-settings-file`.
- `-s` suppresses interactive output, emitting only the agent response text.

#### Scenario: Fresh headless Copilot step

- **WHEN** the runner executes a headless step with `cli: copilot` and session strategy `new`
- **THEN** the adapter returns args beginning with `copilot` and including `-p`, the prompt, `--allow-tool=write`, `--autopilot`, and `-s`, and does not include `--resume`

#### Scenario: Conservative mode keeps baseline flags only

- **WHEN** the runner constructs args for any Copilot headless step and `autonomous_permission_mode` resolves to `conservative`
- **THEN** the args include `--allow-tool=write` and `--autopilot` and do NOT include `--allow-all-tools`

#### Scenario: YOLO mode adds broader authority flag

- **WHEN** the runner constructs args for any Copilot headless step and `autonomous_permission_mode` resolves to `yolo`
- **THEN** the args include `--allow-tool=write`, `--autopilot`, and Copilot's broadest-authority flag (`--allow-all-tools` or its verified equivalent)
