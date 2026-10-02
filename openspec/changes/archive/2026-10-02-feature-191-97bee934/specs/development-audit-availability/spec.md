## MODIFIED Requirements

### Requirement: Audit capability exists only in development-audit builds

Agent Runner SHALL compile the automatic audit hook, hidden workflow asset, replay and setup commands, and concrete audit integrations only into a development-audit build produced by the repository's supported local build paths. `make build` and `dev.sh` SHALL produce development-audit builds. The Docker development sandbox SHALL additionally offer an explicit `--dev-audit` build option; its default build SHALL remain untagged. Release and ordinary untagged builds SHALL NOT register an audit command, inject an audit workflow, or provide a runtime setting that can enable the capability.

A development-audit build SHALL keep its explicit audit commands and hidden audit workflow while automatic post-run auditing is paused. Only the automatic post-finalization trigger is paused.

#### Scenario: Local Make build is used
- **WHEN** the operator builds Agent Runner through `make build`
- **THEN** the resulting binary contains the development audit capability, including the explicit audit commands, and automatically audits eligible executions only while automatic auditing is resumed

#### Scenario: Local development script is used
- **WHEN** the operator runs Agent Runner through `dev.sh`
- **THEN** the resulting process contains the same development audit capability

#### Scenario: Paused development build keeps explicit audit commands
- **WHEN** an operator runs `agent-runner audit`, `audit status`, or `audit replay` in a development-audit build with automatic auditing paused
- **THEN** each command behaves as specified for the development audit capability

#### Scenario: Production release is built
- **WHEN** Agent Runner is built through the production release process without the development audit tag
- **THEN** its commands, workflow catalog, and runtime lifecycle contain no audit option

#### Scenario: Runtime configuration attempts to enable production audit
- **WHEN** an untagged binary reads user or project configuration
- **THEN** no configuration value can enable the absent audit capability

#### Scenario: Sandbox opts into development audit build
- **WHEN** the operator runs the supported sandbox script with `--dev-audit`
- **THEN** the container builds a development-audit binary with the private audit capability and injected mounted-source provenance

#### Scenario: Sandbox retains default build
- **WHEN** the operator runs the sandbox script without `--dev-audit`
- **THEN** the binary remains untagged and runtime configuration cannot enable auditing

### Requirement: Development auditing needs no enablement setting

While automatic auditing is resumed, a development-audit build SHALL automatically attempt to audit every eligible finalized execution. While it is paused, the build SHALL attempt no automatic audit. Whether auditing is paused SHALL be fixed at build time by a single source-level switch and SHALL NOT be a runtime setting. Audit enablement, model selection, and an Agent Runner repository path SHALL NOT be added to layered user or project configuration.

Both model stages SHALL resolve the existing `lead` role, the same agent that leads the source workflow, using the profile-set name recorded by the source run and the profile configuration available at audit launch. The audit SHALL freeze the resolved CLI, model, and reasoning-effort provenance that it actually invokes. It SHALL NOT claim that a profile-set name alone reproduces an earlier resolved agent definition. Existing configuration layering, inheritance, and built-in role defaults SHALL apply; an absent explicit project `lead` entry SHALL NOT by itself constitute a resolution failure. Failure to resolve or invoke that agent SHALL fail or degrade only the linked audit and SHALL NOT alter the source execution. An explicitly recorded profile-set name that cannot be resolved SHALL NOT silently fall back to a different profile set.

#### Scenario: Eligible local execution completes
- **WHEN** an eligible workflow execution is finalized by a development-audit build with automatic auditing resumed
- **THEN** Agent Runner attempts to launch the linked audit without consulting an enablement setting

#### Scenario: Eligible local execution completes while paused
- **WHEN** an eligible workflow execution is finalized by a development-audit build with automatic auditing paused
- **THEN** Agent Runner does not launch an audit and does not consult any enablement setting

#### Scenario: Source profile resolves lead
- **WHEN** the source run's recorded profile-set name resolves a valid `lead` agent at audit launch
- **THEN** both model audit stages use that resolved definition and the audit freezes its actual CLI, model, and effort provenance

#### Scenario: Source profile cannot resolve lead
- **WHEN** the source run's recorded profile-set name cannot resolve `lead` at audit launch
- **THEN** the audit records a diagnostic failure and preserves the source result

#### Scenario: Source configuration inherits lead
- **WHEN** the selected source profile inherits a valid `lead` definition without declaring one explicitly in project configuration
- **THEN** the audit uses and records the inherited resolved CLI, model, and effort

#### Scenario: Built-in lead default applies
- **WHEN** no user or project override replaces the applicable built-in `lead` definition
- **THEN** the audit resolves that default through the existing profile mechanism

#### Scenario: Recorded profile takes precedence at audit launch
- **WHEN** the source records a named profile and current configuration selects another active profile
- **THEN** the audit resolves the recorded profile name using launch-time configuration and freezes the resulting invocation definition

#### Scenario: Profile configuration changes after audit launch
- **WHEN** configuration changes after the audit has frozen its resolved lead definition
- **THEN** both model stages retain that frozen definition rather than independently re-resolving configuration

#### Scenario: Recorded profile or agent invocation is unavailable
- **WHEN** the recorded profile is missing, its role definition is invalid, or the resolved CLI cannot execute
- **THEN** the linked audit retains a diagnostic and the source outcome and exit status remain unchanged
