# Capability: cli-adapter (delta)

## MODIFIED Requirements

### Requirement: No permission loosening in interactive mode

In interactive context, no adapter SHALL emit a flag that bypasses or pre-approves the underlying CLI's permission/approval prompts. The human at the terminal supervises permissions; the runner MUST NOT preempt that supervision. Autonomous invocations (both headless and interactive backend) MAY emit such flags, since the step operates without human supervision.

Exception — the completion client: an adapter MAY pre-approve the completion client only when it can restrict approval to the exact absolute executable path and fixed `step complete` arguments — never a wildcard, shell chaining, substitution, or any other subcommand of the runner binary. If a CLI cannot express that narrow approval safely, its adapter SHALL NOT broaden permissions to compensate: in interactive context the completion command MAY instead be gated by the CLI's normal supervised approval prompt (the human at the terminal approves it like any other command), and unattended contexts follow the autonomous completion rules in `step-control-channel` (approval-free via the CLI's autonomous permission flags, or failing early).

#### Scenario: Adapter omits permission-grant flags in interactive context
- **WHEN** any adapter constructs args for an interactive step
- **THEN** the args do not include any flag that auto-approves tools, paths, URLs, or commands (e.g., `--allow-all`, `--force`, `--yolo`, `--dangerously-skip-permissions`), with the sole exception of the narrow completion-client pre-approval

#### Scenario: Exact completion command runs without prompting
- **WHEN** the agent in an interactive step whose adapter emits the narrow pre-approval runs the completion client at its exact absolute path with the fixed `step complete` arguments
- **THEN** the command executes without a human approval prompt

#### Scenario: Broader completion-command forms are not pre-approved
- **WHEN** the agent attempts the completion command with additional arguments, shell chaining, substitutions, or a different agent-runner subcommand
- **THEN** the CLI's normal permission behavior applies; the pre-approval does not cover it

#### Scenario: Unrelated commands retain normal permission behavior
- **WHEN** the agent in an interactive step attempts any command other than the exact completion command
- **THEN** the CLI's normal permission prompts apply, unchanged by the completion-client pre-approval

#### Scenario: CLI that cannot express narrow approval keeps supervised prompting
- **WHEN** a CLI cannot restrict pre-approval to the exact absolute executable path and fixed arguments
- **THEN** its adapter emits no broader permission flag, and in interactive context the completion command is subject to the CLI's normal supervised approval prompt

#### Scenario: Autonomous-headless adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-headless step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

#### Scenario: Autonomous-interactive adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-interactive step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

### Requirement: Capture forces autonomous-headless

When an autonomous step has a `capture` field, the runner SHALL force the invocation context to autonomous-headless regardless of the `autonomous_backend` setting or TTY availability. Capture requires a clean stdout pipe, which only the headless execution path provides; the interactive backend attaches the CLI directly to the user's terminal, so its output goes to the terminal and is never available for programmatic capture. This override is per-step — other autonomous steps in the same run that do not use `capture` are unaffected by this rule.

#### Scenario: Capture step with interactive backend forced to headless
- **WHEN** the `autonomous_backend` setting is `interactive` and an autonomous step has `capture: result`
- **THEN** the runner invokes the step as autonomous-headless and captures stdout into the variable

#### Scenario: Non-capture step unaffected
- **WHEN** the `autonomous_backend` setting is `interactive` and an autonomous step does not have `capture`
- **THEN** the runner routes the step per the normal backend and TTY rules

### Requirement: Adapters honor autonomous permission mode

In autonomous invocation contexts (both autonomous-headless and autonomous-interactive), each CLI adapter SHALL receive the resolved `autonomous_permission_mode` setting and SHALL emit permission-grant flags accordingly:

- When the mode is `conservative`, the adapter SHALL emit only the per-CLI baseline permission flags that it emits today for autonomous contexts. The adapter SHALL NOT emit additional broad-authority flags (e.g., Cursor `--force`, Claude `--permission-mode bypassPermissions`, Codex `--sandbox danger-full-access`, Copilot `--allow-all-tools`).
- When the mode is `yolo`, the adapter MAY additionally emit each CLI's broadest-authority permission flag where the backing CLI provides one. Adapters whose CLI has no equivalent broader flag MAY ignore the mode and behave identically in both values.

The setting SHALL NOT affect interactive (non-autonomous) invocations. The existing "no permission loosening in interactive mode" requirement remains in force regardless of `autonomous_permission_mode`.

`BuildArgsInput` (or its equivalent) SHALL expose the resolved mode to adapters so they can branch on it; the runner SHALL populate the field from the user setting on every autonomous step invocation.

#### Scenario: Conservative mode preserves today's baseline flags

- **WHEN** an autonomous agent step runs with `autonomous_permission_mode: conservative` (or the setting absent)
- **THEN** each adapter's emitted args match the per-CLI autonomous baseline it emits today: Claude includes `--permission-mode acceptEdits`, Codex includes `--sandbox workspace-write` and, for headless `exec` invocations, `exec --skip-git-repo-check`, Copilot includes `--allow-tool=write --autopilot`, Cursor includes `--trust` only, OpenCode emits no permission flag

#### Scenario: YOLO mode permits broader authority flag

- **WHEN** an autonomous agent step runs with `autonomous_permission_mode: yolo`
- **THEN** each adapter MAY emit an additional broader-authority flag appropriate to its CLI in addition to the baseline flags

#### Scenario: Setting does not affect interactive context

- **WHEN** an interactive (non-autonomous) agent step runs with `autonomous_permission_mode: yolo`
- **THEN** the adapter does not emit any flag that auto-approves tools, paths, URLs, or commands (the "no permission loosening in interactive mode" rule still holds)

#### Scenario: Setting applies to both autonomous-headless and autonomous-interactive

- **WHEN** an autonomous step runs with `autonomous_permission_mode: yolo` and the resolved backend is autonomous-interactive (e.g., Claude on the user's own terminal)
- **THEN** the adapter applies the same yolo-mode flag set it would apply in autonomous-headless

#### Scenario: Adapter without a broader flag is mode-insensitive

- **WHEN** an autonomous OpenCode step runs and OpenCode has no broader-authority flag exposed by its CLI
- **THEN** the OpenCode adapter emits the same args under `conservative` and `yolo`
