## MODIFIED Requirements

### Requirement: No permission loosening in interactive mode

In interactive context, no adapter SHALL emit a flag that broadly bypasses or pre-approves the underlying CLI's permission/approval prompts. The human at the terminal supervises permissions; the runner MUST NOT preempt that supervision. An adapter MAY pre-approve the completion client only when it can restrict approval to the exact absolute executable path and fixed `step complete` arguments. If a CLI cannot express that narrow rule safely, the adapter SHALL retain the CLI's normal supervised approval prompt rather than broaden the rule. Autonomous invocations (both headless and interactive backend) MAY emit broader permission flags because the step operates without human supervision. An autonomous permission flag MAY provide best-effort rather than guaranteed approval-free operation: under such a flag the backing CLI can still raise its own permission prompt (for example, Claude's `auto` mode on the autonomous-interactive backend, per "Adapters honor autonomous permission mode"). Such a CLI permission prompt does not make the step interactive: the step remains autonomous, and the runner continues to block agent clarification questions and to deliver autonomy instructions as it does for any autonomous step.

#### Scenario: Adapter omits permission-grant flags in interactive context
- **WHEN** any adapter constructs args for an interactive step
- **THEN** the args do not include any broad permission grant; only an exact completion-client rule is permitted

#### Scenario: Broader completion forms are not pre-approved
- **WHEN** the agent adds arguments, shell chaining, substitutions, or uses another Agent Runner subcommand
- **THEN** the CLI's normal permission behavior applies

#### Scenario: CLI without exact rules stays supervised
- **WHEN** a CLI cannot constrain pre-approval to the absolute executable and fixed `step complete` arguments
- **THEN** its adapter emits no broader rule and the completion command uses the normal interactive approval prompt

#### Scenario: Autonomous-headless adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-headless step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

#### Scenario: Autonomous-interactive adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-interactive step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation, including a flag under which the backing CLI may still occasionally raise its own permission prompt

### Requirement: Adapters honor autonomous permission mode

In autonomous invocation contexts (both autonomous-headless and autonomous-interactive), each CLI adapter SHALL receive the resolved `autonomous_permission_mode` setting and SHALL emit permission-grant flags accordingly:

- When the mode is `conservative`, the adapter SHALL emit only the per-CLI baseline permission flags that it emits today for autonomous contexts. The adapter SHALL NOT emit additional broad-authority flags (e.g., Cursor `--force`, Claude `--permission-mode bypassPermissions` or `--permission-mode auto`, Codex `--sandbox danger-full-access`, Copilot `--allow-all-tools`).
- When the mode is `yolo`, the adapter MAY additionally emit each CLI's broadest-authority permission flag where the backing CLI provides one. Adapters whose CLI has no equivalent broader flag MAY ignore the mode and behave identically in both values. An adapter MAY choose a different yolo flag per autonomous backend.

The Claude adapter SHALL select its yolo permission mode by the resolved invocation context:

- autonomous-headless: `--permission-mode bypassPermissions`.
- autonomous-interactive: `--permission-mode auto`. The Claude adapter SHALL NOT emit `--permission-mode bypassPermissions` in autonomous-interactive context.

The Claude adapter SHALL emit the context-selected permission mode for both fresh and resumed sessions. The adapter SHALL emit `--permission-mode auto` without probing whether Claude's auto mode is available to the user's account, model, or settings, and SHALL NOT substitute `bypassPermissions` or any other mode when it is not; when Claude starts the session in another mode instead (for example Manual), the step runs under whatever permission prompts that mode raises. Choosing `auto` for the autonomous-interactive backend SHALL NOT change the autonomy contract of that step: the runner SHALL continue to disallow `AskUserQuestion` and SHALL deliver autonomy instructions exactly as it does for any autonomous-interactive step: prepended to the system prompt on a fresh session, and not re-injected on a resumed session, whose existing resume prompt and completion instruction are unchanged.

The setting SHALL NOT affect interactive (non-autonomous) invocations. The existing "no permission loosening in interactive mode" requirement remains in force regardless of `autonomous_permission_mode`.

`BuildArgsInput` (or its equivalent) SHALL expose the resolved mode to adapters so they can branch on it; the runner SHALL populate the field from the user setting on every autonomous step invocation.

#### Scenario: Conservative mode preserves today's baseline flags

- **WHEN** an autonomous agent step runs with `autonomous_permission_mode: conservative` (or the setting absent)
- **THEN** each adapter's emitted args match the per-CLI autonomous baseline it emits today: Claude includes `--permission-mode acceptEdits`, Codex includes `--sandbox workspace-write` and, for headless `exec` invocations, `exec --skip-git-repo-check`, Copilot includes `--allow-tool=write --autopilot`, Cursor includes `--trust` only, OpenCode emits no permission flag

#### Scenario: Conservative Claude autonomous-interactive keeps acceptEdits

- **WHEN** a Claude agent step runs in autonomous-interactive context with `autonomous_permission_mode: conservative` (or the setting absent)
- **THEN** the args include `--permission-mode acceptEdits` and include neither `--permission-mode auto` nor `--permission-mode bypassPermissions`

#### Scenario: YOLO mode permits broader authority flag

- **WHEN** an autonomous agent step runs with `autonomous_permission_mode: yolo`
- **THEN** each adapter MAY emit an additional broader-authority flag appropriate to its CLI and backend in addition to the baseline flags

#### Scenario: Setting does not affect interactive context

- **WHEN** an interactive (non-autonomous) agent step runs with `autonomous_permission_mode: yolo`
- **THEN** the adapter does not emit any flag that auto-approves tools, paths, URLs, or commands (the "no permission loosening in interactive mode" rule still holds), and Claude's args include no `--permission-mode` value

#### Scenario: Setting applies to both autonomous-headless and autonomous-interactive

- **WHEN** an autonomous step runs with `autonomous_permission_mode: yolo` and the resolved backend is either autonomous-headless or autonomous-interactive
- **THEN** the adapter applies its yolo-mode flag set for that backend; for every adapter other than Claude, that flag set is identical on both backends

#### Scenario: Claude yolo autonomous-interactive uses auto mode

- **WHEN** a Claude agent step runs in autonomous-interactive context with `autonomous_permission_mode: yolo`
- **THEN** the args include `--permission-mode auto` and do not include `--permission-mode bypassPermissions`

#### Scenario: Claude yolo autonomous-interactive resume uses auto mode

- **WHEN** a Claude agent step resumes an existing session (`--resume <id>`) in autonomous-interactive context with `autonomous_permission_mode: yolo`
- **THEN** the args include `--permission-mode auto` and do not include `--permission-mode bypassPermissions`

#### Scenario: Claude yolo autonomous-headless keeps bypassPermissions

- **WHEN** a Claude agent step runs in autonomous-headless context with `autonomous_permission_mode: yolo`
- **THEN** the args include `--permission-mode bypassPermissions` and do not include `--permission-mode auto`

#### Scenario: Capture-forced headless Claude step keeps bypassPermissions

- **WHEN** an autonomous Claude agent step with output capture runs while the user's autonomous backend selects interactive Claude, with `autonomous_permission_mode: yolo`
- **THEN** the step resolves to autonomous-headless context and its args include `--permission-mode bypassPermissions`

#### Scenario: Claude auto mode does not relax the autonomy contract on a fresh session

- **WHEN** a Claude agent step starts a fresh session in autonomous-interactive context with `autonomous_permission_mode: yolo`
- **THEN** the step's disallowed tools include `AskUserQuestion` and its system prompt begins with the autonomy instructions, as for any fresh autonomous-interactive step

#### Scenario: Claude auto mode does not relax the autonomy contract on a resumed session

- **WHEN** a Claude agent step resumes an existing session in autonomous-interactive context with `autonomous_permission_mode: yolo`
- **THEN** the step's disallowed tools include `AskUserQuestion`, the autonomy instructions are not re-injected, and the step's prompt and completion instruction are the same as for any resumed autonomous-interactive step

#### Scenario: Unavailable Claude auto mode is not replaced

- **WHEN** a Claude agent step runs in autonomous-interactive context with `autonomous_permission_mode: yolo` and Claude's auto mode is unavailable to the session (for example an unsupported model, `disableAutoMode`, or a server-side disable)
- **THEN** the runner still launches Claude with `--permission-mode auto`, does not relaunch or retry with `bypassPermissions` or another mode, and the step proceeds under whatever permission mode Claude starts in

#### Scenario: Claude external-user context is unchanged

- **WHEN** a Claude agent step runs in external-user context under either `autonomous_permission_mode` value
- **THEN** the args do not include `--permission-mode auto`, and the emitted permission mode is the same one the adapter emitted for that context and setting before this change

#### Scenario: Adapter without a broader flag is mode-insensitive

- **WHEN** an autonomous OpenCode step runs and OpenCode has no broader-authority flag exposed by its CLI
- **THEN** the OpenCode adapter emits the same args under `conservative` and `yolo`
