# development-audit-availability Specification

## Purpose
TBD - created by archiving change audit-step. Update Purpose after archive.
## Requirements
### Requirement: Audit capability exists only in development-audit builds

Agent Runner SHALL compile the automatic audit hook, hidden workflow asset, replay and setup commands, and concrete audit integrations only into a development-audit build produced by the repository's supported local build paths. `make build` and `dev.sh` SHALL produce development-audit builds. Release and ordinary untagged builds SHALL NOT register an audit command, inject an audit workflow, or provide a runtime setting that can enable the capability.

#### Scenario: Local Make build is used
- **WHEN** the operator builds Agent Runner through `make build`
- **THEN** the resulting binary contains the development audit capability and automatically audits eligible executions

#### Scenario: Local development script is used
- **WHEN** the operator runs Agent Runner through `dev.sh`
- **THEN** the resulting process contains the same development audit capability

#### Scenario: Production release is built
- **WHEN** Agent Runner is built through the production release process without the development audit tag
- **THEN** its commands, workflow catalog, and runtime lifecycle contain no audit option

#### Scenario: Runtime configuration attempts to enable production audit
- **WHEN** an untagged binary reads user or project configuration
- **THEN** no configuration value can enable the absent audit capability

### Requirement: Development auditing needs no enablement setting

A development-audit build SHALL automatically attempt to audit every eligible finalized execution. Audit enablement, model selection, and an Agent Runner repository path SHALL NOT be added to layered user or project configuration.

Both model stages SHALL resolve the existing `crosscheck` role using the profile-set name recorded by the source run and the profile configuration available at audit launch. The audit SHALL freeze the resolved CLI, model, and reasoning-effort provenance that it actually invokes. It SHALL NOT claim that a profile-set name alone reproduces an earlier resolved agent definition. Failure to resolve that agent SHALL fail or degrade only the linked audit and SHALL NOT alter the source execution.

#### Scenario: Eligible local execution completes
- **WHEN** an eligible workflow execution is finalized by a development-audit build
- **THEN** Agent Runner attempts to launch the linked audit without consulting an enablement setting

#### Scenario: Source profile resolves crosscheck
- **WHEN** the source run's recorded profile-set name resolves a valid `crosscheck` agent at audit launch
- **THEN** both model audit stages use that resolved definition and the audit freezes its actual CLI, model, and effort provenance

#### Scenario: Source profile cannot resolve crosscheck
- **WHEN** the source run's recorded profile-set name cannot resolve `crosscheck` at audit launch
- **THEN** the audit records a diagnostic failure and preserves the source result

### Requirement: The build identifies its Agent Runner source

The supported development build paths SHALL inject the absolute Agent Runner checkout root and available build Git provenance into the tagged binary. At audit launch, Agent Runner SHALL verify that root, record its then-current revision and dirty state, and create a read-only snapshot for correctness verification. That launch-time snapshot SHALL be authoritative for whether the suspected behavior is a current Agent Runner defect. Build-time provenance SHALL remain diagnostic context and SHALL NOT require a separate repository setting.

Before correctness publication, Agent Runner SHALL verify that the launch-time snapshot identifies the Agent Runner module. Missing, wrong-module, or incomplete launch-time source SHALL reduce coverage and prevent issue creation. A difference between build-time and launch-time provenance SHALL be recorded and MAY reduce confidence, but SHALL NOT by itself block publication of a defect verified against the authoritative launch-time snapshot.

#### Scenario: Local binary runs from another project
- **WHEN** a development-audit binary built in the Agent Runner checkout audits a workflow in another project
- **THEN** the correctness stage inspects the injected Agent Runner checkout rather than the audited project's repository as Runner source

#### Scenario: Two machines use different checkouts
- **WHEN** development binaries are built from different absolute checkout paths on two machines
- **THEN** each binary uses the path and provenance injected by its own build

#### Scenario: Build checkout moved or removed
- **WHEN** the injected checkout path cannot be verified at audit time
- **THEN** the audit records unavailable Runner source coverage and does not file a source-verified issue from that evidence

#### Scenario: Checkout changed after build
- **WHEN** the current checkout revision or dirty state materially differs from the recorded build provenance
- **THEN** the discrepancy is retained locally while the verified audit-launch snapshot remains authoritative for current-defect assessment

#### Scenario: Launch snapshot is not Agent Runner source
- **WHEN** the injected path resolves at launch but its snapshot does not identify the Agent Runner module
- **THEN** correctness publication is blocked

### Requirement: Model isolation is Darwin-only in the initial development audit

The initial development-audit implementation SHALL invoke its model-driven stages only on Darwin, using the operating-system filesystem sandbox to keep the source and Runner snapshots read-only and to restrict writes to audit-owned output. On every other operating system, the model-driven stages SHALL fail closed with an explicit unsupported-platform diagnostic. This limitation SHALL affect only the linked audit and SHALL NOT alter the source execution's result.

#### Scenario: Audit model stage runs on Darwin
- **WHEN** a development audit reaches a model-driven stage on Darwin and the required operating-system sandbox is available
- **THEN** Agent Runner launches the resolved crosscheck within the restricted audit workspace

#### Scenario: Audit model stage runs on another operating system
- **WHEN** a development audit reaches a model-driven stage on a non-Darwin operating system
- **THEN** the linked audit records an unsupported-platform failure without launching the model or changing the source result

### Requirement: Google connection state is private and does not control auditing

The development-audit build SHALL provide a one-time setup operation that imports compatible Google OAuth client and authorized-user token material together with an existing spreadsheet ID and worksheet tab into protected Agent Runner user storage. This record SHALL be integration state rather than layered user or project configuration, and its presence SHALL NOT enable or disable automatic auditing.

Missing, malformed, incomplete, or unusable Google connection state SHALL prevent or degrade only external reporting. The complete local audit report SHALL remain available for retry and the source outcome SHALL remain unchanged.

#### Scenario: Connection is imported
- **WHEN** the operator supplies compatible OAuth files and an existing spreadsheet destination to the setup operation
- **THEN** Agent Runner copies the required values into protected user-scoped storage without retaining a runtime dependency on the source application

#### Scenario: No Google connection exists
- **WHEN** an eligible local audit runs before Google setup is complete
- **THEN** model auditing completes locally where possible and reporting records a retryable warning

#### Scenario: Connection changes during audit
- **WHEN** the operator changes the stored destination after an audit has frozen its completed report
- **THEN** retry uses the destination identity frozen for that audit unless the operator explicitly requests migration

#### Scenario: Connection contains secrets
- **WHEN** OAuth client or token material is imported
- **THEN** it is stored atomically in a user-only record and is not copied into project configuration, run artifacts, logs, model prompts, or the spreadsheet

