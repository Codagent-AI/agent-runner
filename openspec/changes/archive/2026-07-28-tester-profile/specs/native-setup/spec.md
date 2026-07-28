## MODIFIED Requirements

### Requirement: Native setup is mandatory

Native setup SHALL detect supported CLI adapters and discover their available models before presenting an actionable profile-selection surface. While recommendation inputs are being collected, setup SHALL display a non-actionable loading state. When at least one supported CLI is detected, the first actionable profile surface SHALL be the four-role recommendation summary defined by the agent-profile-editor capability rather than an individual lead CLI screen.

Native setup SHALL NOT offer skip, not-now, or dismiss actions during required profile setup. A user who cancels or interrupts recommendation, customization, backend, permission, scope, overwrite, or skill-installation stages SHALL leave setup incomplete, and native setup SHALL be offered again on the next eligible launch.

Failure to discover models from one detected CLI SHALL NOT fail setup; setup SHALL retain that CLI with its adapter default, carry the discovery limitation into the recommendation summary, and continue. Failure to detect any supported CLI SHALL present a blocking failure as the first actionable result.

#### Scenario: Recommendation summary is the first actionable profile surface
- **WHEN** native setup detects at least one supported CLI and finishes recommendation discovery
- **THEN** setup presents the four-role recommendation summary without first presenting an individual lead CLI screen

#### Scenario: Recommendation discovery shows loading state
- **WHEN** native setup is still detecting adapters or discovering models needed for the recommendation
- **THEN** setup displays a non-actionable loading state and does not permit accepting an incomplete recommendation

#### Scenario: Individual discovery failure remains usable
- **WHEN** model discovery fails for one detected CLI and at least one supported CLI was detected
- **THEN** setup presents the recommendation summary with that CLI eligible at its adapter default and visibly identifies the discovery limitation

#### Scenario: Adapter detection failure is blocking
- **WHEN** native setup detects no supported CLI adapters
- **THEN** the failure message is the first actionable result and no profile recommendation can be accepted

#### Scenario: Setup cannot be skipped
- **WHEN** native setup renders recommendation, customization, backend, permission, scope, overwrite, or skill-installation stages
- **THEN** no skip, not-now, or dismiss action is available

#### Scenario: Cancel leaves setup incomplete
- **WHEN** the user cancels or interrupts native setup before all required setup actions complete
- **THEN** setup completion is not recorded and native setup is offered again on the next eligible launch

### Requirement: Autonomous backend selection during setup

After the user accepts the complete four-role recommendation or finishes lead, crosscheck, implementor, and tester customization, native setup SHALL present an "Autonomous Backend" selection screen. The screen SHALL display the three `autonomous_backend` options Headless, Interactive, and Interactive for Claude, each with a one-sentence explanation of invocation behavior. `Headless` SHALL be preselected and labeled as the recommended default. Backend descriptions SHALL NOT claim that one invocation mode avoids API billing.

After the user selects an autonomous backend, setup SHALL present the existing Autonomous Permission Mode selection screen. The selected backend SHALL be written to `~/.agent-runner/settings.yaml` only when setup completes successfully. Changing the setup recommendation SHALL NOT migrate or rewrite an already persisted backend outside a completed setup flow.

#### Scenario: Accept-all proceeds to backend selection
- **WHEN** the user accepts the complete four-role recommendation
- **THEN** setup presents the Autonomous Backend screen without showing individual role-selection screens

#### Scenario: Customization proceeds to backend selection
- **WHEN** the user completes tester customization after lead, crosscheck, and implementor
- **THEN** setup presents the Autonomous Backend screen

#### Scenario: Headless is preselected
- **WHEN** the Autonomous Backend screen is presented
- **THEN** `Headless` is focused and labeled as the recommended default

#### Scenario: Every backend option has behavioral copy
- **WHEN** the Autonomous Backend screen is presented
- **THEN** Headless, Interactive, and Interactive for Claude are all available with explanations of invocation behavior and without provider-billing claims

#### Scenario: Permission mode follows backend
- **WHEN** the user selects an Autonomous Backend option
- **THEN** setup presents the Autonomous Permission Mode screen before scope and skill installation complete

#### Scenario: Selected backend is persisted on setup completion
- **WHEN** the user selects an autonomous backend and setup completes successfully
- **THEN** `~/.agent-runner/settings.yaml` contains the selected `autonomous_backend` value

#### Scenario: Cancelled setup does not persist backend
- **WHEN** the user selects an autonomous backend but cancels setup before completion
- **THEN** `~/.agent-runner/settings.yaml` does not contain an `autonomous_backend` value from the canceled attempt

#### Scenario: Existing backend is not migrated automatically
- **WHEN** a user already has `autonomous_backend: interactive-claude` and native setup does not complete a new setup flow
- **THEN** the runner leaves that persisted value unchanged

## REMOVED Requirements

### Requirement: Native setup implementor CLI billing disclosure

The native setup implementor CLI selection SHALL disclose Claude programmatic usage billing next to the Claude option. The disclosure SHALL appear before the user confirms the implementor CLI choice so users can choose the default implementor backend with that cost context visible.

**Reason**: Current Claude Code billing depends on authentication and provider policy rather than whether Agent Runner invokes Claude interactively or through `claude -p`; the provider's previously announced programmatic-usage separation was paused. A mandatory warning tied specifically to autonomous Claude selection is therefore misleading.

**Migration**: Remove Claude-specific programmatic/API-billing warnings from native setup recommendation and customization surfaces. Keep backend descriptions focused on invocation behavior. Provider billing guidance, if presented elsewhere, must describe authentication-dependent behavior without using backend mode as a billing proxy.

#### Scenario: Claude implementor option shows programmatic billing disclosure
- **WHEN** native setup renders the implementor CLI selection and `claude` is an available option
- **THEN** the `claude` option includes a visible programmatic credits/API-rate billing disclosure
