## MODIFIED Requirements

### Requirement: Product-owned Docker smoke proves the detached audit journey

Agent Runner SHALL provide a hermetic Docker smoke using its own fixtures and the supported sandbox build path. It SHALL run an eligible source workflow, exercise both model stages through the production Linux confinement launcher, and verify durable reciprocal linkage, validated local output, and terminal linked-audit state. It MUST NOT run or modify Agent Evals, use real model/reporting services, import host credentials, or replace confinement with the existing E2E bypass.

If the source run returns with an automatic linked audit, the smoke SHALL verify that exactly one linked audit was active when the source returned. If the source run returns with no automatic audit, as it does while automatic auditing is paused, the smoke SHALL launch the linked audit itself through explicit `audit replay` of the source run's execution session. It SHALL report that it used the replay path, and it SHALL then apply the same active-audit, linkage, output, confinement, and terminal-state checks to the replayed audit. The smoke SHALL choose its path from the source run's observed audit state, so resuming automatic auditing requires no smoke change.

#### Scenario: Smoke completes locally
- **WHEN** the smoke runs in the supported Docker environment with deterministic fake agents and no Google connection
- **THEN** the source finishes successfully, both model stages produce validated local output, the linked audit becomes terminal, and missing reporting configuration remains a local warning

#### Scenario: Smoke replays while automatic auditing is paused
- **WHEN** the smoke's source run returns from a development-audit build with automatic auditing paused, leaving no audit lifecycle
- **THEN** the smoke reports that it is using explicit replay, launches exactly one replay audit for the source execution session, and completes the same journey checks against that audit

#### Scenario: Smoke keeps automatic assertion when auditing is resumed
- **WHEN** the smoke's source run returns with an automatic linked audit
- **THEN** the smoke does not replay and verifies the automatic audit as before

#### Scenario: Source process exits before audit completion
- **WHEN** the source CLI returns while its linked audit remains active
- **THEN** the smoke supervisor keeps the container alive and waits for that linked audit without changing the source CLI's non-blocking behavior

#### Scenario: Replay cannot be launched
- **WHEN** the source run returns with no audit lifecycle and the explicit replay fails or the execution session cannot be identified
- **THEN** the smoke exits unsuccessfully with the source identity and available diagnostics preserved

#### Scenario: Audit never becomes terminal
- **WHEN** the linked audit remains nonterminal past the smoke deadline
- **THEN** the smoke exits unsuccessfully with source identity, audit identity when available, and available state and diagnostic artifacts preserved

#### Scenario: Audit terminates without required results
- **WHEN** an audit reaches terminal state with missing model-stage evidence or invalid local output
- **THEN** the smoke fails even if the source CLI returned success

#### Scenario: Smoke runs without external access
- **WHEN** the pre-provisioned smoke executes with external networking disabled and isolated configuration
- **THEN** local fixtures suffice for the full expected journey and no external model or reporting operation is required
