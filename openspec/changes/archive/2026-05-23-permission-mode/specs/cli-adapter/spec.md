## ADDED Requirements

### Requirement: Adapters honor autonomous permission mode

In autonomous invocation contexts (both autonomous-headless and autonomous-interactive), each CLI adapter SHALL receive the resolved `autonomous_permission_mode` setting and SHALL emit permission-grant flags accordingly:

- When the mode is `conservative`, the adapter SHALL emit only the per-CLI baseline permission flags that it emits today for autonomous contexts. The adapter SHALL NOT emit additional broad-authority flags (e.g., Cursor `--force`, Claude `--permission-mode bypassPermissions`, Codex `--sandbox danger-full-access`, Copilot `--allow-all-tools`).
- When the mode is `yolo`, the adapter MAY additionally emit each CLI's broadest-authority permission flag where the backing CLI provides one. Adapters whose CLI has no equivalent broader flag MAY ignore the mode and behave identically in both values.

The setting SHALL NOT affect interactive (non-autonomous) invocations. The existing "no permission loosening in interactive mode" requirement remains in force regardless of `autonomous_permission_mode`.

`BuildArgsInput` (or its equivalent) SHALL expose the resolved mode to adapters so they can branch on it; the runner SHALL populate the field from the user setting on every autonomous step invocation.

#### Scenario: Conservative mode preserves today's baseline flags

- **WHEN** an autonomous agent step runs with `autonomous_permission_mode: conservative` (or the setting absent)
- **THEN** each adapter's emitted args match the per-CLI autonomous baseline it emits today: Claude includes `--permission-mode acceptEdits`, Codex includes `--sandbox workspace-write`, Copilot includes `--allow-tool=write --autopilot`, Cursor includes `--trust` only, OpenCode emits no permission flag

#### Scenario: YOLO mode permits broader authority flag

- **WHEN** an autonomous agent step runs with `autonomous_permission_mode: yolo`
- **THEN** each adapter MAY emit an additional broader-authority flag appropriate to its CLI in addition to the baseline flags

#### Scenario: Setting does not affect interactive context

- **WHEN** an interactive (non-autonomous) agent step runs with `autonomous_permission_mode: yolo`
- **THEN** the adapter does not emit any flag that auto-approves tools, paths, URLs, or commands (the "no permission loosening in interactive mode" rule still holds)

#### Scenario: Setting applies to both autonomous-headless and autonomous-interactive

- **WHEN** an autonomous step runs with `autonomous_permission_mode: yolo` and the resolved backend is autonomous-interactive (e.g., Claude in a PTY)
- **THEN** the adapter applies the same yolo-mode flag set it would apply in autonomous-headless

#### Scenario: Adapter without a broader flag is mode-insensitive

- **WHEN** an autonomous OpenCode step runs and OpenCode has no broader-authority flag exposed by its CLI
- **THEN** the OpenCode adapter emits the same args under `conservative` and `yolo`
