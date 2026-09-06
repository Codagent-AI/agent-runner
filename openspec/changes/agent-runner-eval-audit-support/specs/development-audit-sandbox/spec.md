## ADDED Requirements

### Requirement: Docker development auditing requires explicit build opt-in

`scripts/sandbox-run.sh` SHALL accept `--dev-audit` to build with `dev_audit` and audit source provenance. Without this option it SHALL build an untagged binary. Help and dry-run output SHALL make the selected build behavior inspectable without exposing secret values. Existing argument boundaries, read-only source/input mounts, and explicit credential pass-through behavior SHALL be preserved.

#### Scenario: Operator selects development audits
- **WHEN** the sandbox script runs with `--dev-audit`
- **THEN** the produced binary contains private audit capability and identifies `/agent-runner-source` as Runner source

#### Scenario: Operator uses default sandbox
- **WHEN** the sandbox script runs without `--dev-audit`
- **THEN** the produced binary does not contain an enabled audit command, workflow, or lifecycle hook

#### Scenario: Operator inspects opt-in without execution
- **WHEN** the operator requests help or a dry run with the opt-in
- **THEN** the build selection is visible and dry-run output neither starts a container nor prints secret values

### Requirement: Product-owned Docker smoke proves the detached audit journey

Agent Runner SHALL provide a hermetic Docker smoke using its own fixtures and the supported sandbox build path. It SHALL run an eligible source workflow, exercise both model stages through the production Linux confinement launcher, and verify durable reciprocal linkage, validated local output, and terminal linked-audit state. It MUST NOT run or modify Agent Evals, use real model/reporting services, import host credentials, or replace confinement with the existing E2E bypass.

#### Scenario: Smoke completes locally
- **WHEN** the smoke runs in the supported Docker environment with deterministic fake agents and no Google connection
- **THEN** the source finishes successfully, both model stages produce validated local output, the linked audit becomes terminal, and missing reporting configuration remains a local warning

#### Scenario: Source process exits before audit completion
- **WHEN** the source CLI returns while its linked audit remains active
- **THEN** the smoke supervisor keeps the container alive and waits for that linked audit without changing the source CLI's non-blocking behavior

#### Scenario: Audit never becomes terminal
- **WHEN** the linked audit remains nonterminal past the smoke deadline
- **THEN** the smoke exits unsuccessfully with source identity, audit identity when available, and available state and diagnostic artifacts preserved

#### Scenario: Audit terminates without required results
- **WHEN** an audit reaches terminal state with missing model-stage evidence or invalid local output
- **THEN** the smoke fails even if the source CLI returned success

#### Scenario: Smoke runs without external access
- **WHEN** the pre-provisioned smoke executes with external networking disabled and isolated configuration
- **THEN** local fixtures suffice for the full expected journey and no external model or reporting operation is required

### Requirement: Development documentation states operating contracts

Development documentation SHALL describe the opt-in/default distinction, authoritative mounted source and unavailable Git metadata, exact canonical workflow eligibility, inherited `crosscheck` resolution, Linux prerequisites and fail-closed behavior, retained Darwin/Sequoia behavior, and smoke usage and artifact interpretation. Documentation SHALL distinguish source completion from linked-audit completion and confinement from read/network restrictions.

#### Scenario: Developer follows sandbox audit instructions
- **WHEN** a developer reads the documented workflow
- **THEN** they can identify the build opt-in, eligible workflow/profile prerequisites, supported isolation environment, and the separate evidence needed to establish linked-audit completion
