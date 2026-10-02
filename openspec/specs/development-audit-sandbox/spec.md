# development-audit-sandbox Specification

## Purpose
TBD - created by archiving change agent-runner-eval-audit-support. Update Purpose after archive.
## Requirements
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

### Requirement: Smoke fixture registration requires a separate build selection

Ordinary untagged and `dev_audit` binaries SHALL NOT register the smoke fixture workflow. The product-owned smoke SHALL explicitly add `devaudit_smoke` through `--dev-audit --dev-audit-smoke`. That tag SHALL add only fixture registration and SHALL NOT replace production confinement, profile resolution, or audit lifecycle behavior.

#### Scenario: Ordinary development binary resolves the fixture
- **WHEN** an ordinary `dev_audit` binary is asked to resolve `openspec:audit-smoke`
- **THEN** that workflow is unavailable

#### Scenario: Smoke explicitly selects its fixture
- **WHEN** the smoke selects both development auditing and the separate fixture build option
- **THEN** its hidden canonical workflow is available and both model stages retain the production launcher

### Requirement: Audit Docker opt-in retains seccomp filtering

The opted-in Docker invocation SHALL use a repository-owned default-deny seccomp profile with only the user/mount namespace and mount-operation additions needed by the supported Bubblewrap launcher. It SHALL NOT disable seccomp or add container capabilities or privileged mode. Unsupported confinement prerequisites SHALL remain diagnostic failures without an unconfined fallback.

#### Scenario: Developer selects the audit container
- **WHEN** the developer runs the supported audit Docker invocation
- **THEN** seccomp remains active, required namespace setup succeeds on the supported environment, and model filesystem confinement is enforced

#### Scenario: Developer selects the default container
- **WHEN** the developer omits the audit opt-in
- **THEN** Docker's default seccomp profile remains selected

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

### Requirement: Development documentation states operating contracts

Development documentation SHALL describe the following:

- the difference between the opt-in and default builds;
- that the mounted source is authoritative and that Git metadata is unavailable in the container;
- the exact rules for which canonical workflows are eligible;
- how `lead` resolution is inherited;
- Linux prerequisites and fail-closed behavior;
- that the Darwin/Sequoia behavior is retained;
- how to run the smoke and how to read its artifacts;
- how the smoke owns and cleans up resources.

For resource ownership, the documentation SHALL cover:

- the run-unique `agent-runner-dev-audit-smoke:<run-id>` tag;
- that images and directories supplied through `IMAGE` and `ARTIFACT_DIR` belong to the caller and are
  kept;
- that owned artifacts are removed after success and kept after failure;
- that the build cache is left for the host to reclaim;
- how to recover by hand an image left behind by a run that was killed.

Documentation SHALL distinguish source completion from linked-audit completion, and confinement from
read and network restrictions.

#### Scenario: Developer follows sandbox audit instructions
- **WHEN** a developer reads the documented workflow
- **THEN** they can identify the build opt-in, eligible workflow/profile prerequisites, supported isolation environment, and the separate evidence needed to establish linked-audit completion

#### Scenario: Developer wants to keep smoke resources
- **WHEN** a developer reads the smoke documentation to keep a warm image or keep evidence from a successful run
- **THEN** they can identify that supplying `IMAGE` keeps a reusable tag and supplying `ARTIFACT_DIR` keeps evidence, and that without them the smoke removes its own image and its successful-run artifacts

#### Scenario: Operator finds a leftover smoke image
- **WHEN** an operator finds an `agent-runner-dev-audit-smoke:*` image after a run was killed
- **THEN** the documentation tells them that the image belongs to the smoke and can be removed by hand

### Requirement: Docker smoke releases only the Docker resources it owns

`scripts/docker-dev-audit-smoke.sh` SHALL own an image only when the caller did not supply `IMAGE`.
In that case the smoke SHALL build its sandbox image under a tag unique to the run, in the
`agent-runner-dev-audit-smoke` repository. When the smoke exits, it SHALL remove that tag without
forcing. This SHALL happen after success, after failure, and after an interrupt it can handle. When
the caller supplies `IMAGE`, the smoke SHALL build and use that tag and SHALL NOT remove it. The smoke
SHALL NOT build, retag, or remove `agent-runner-dev:local` unless the caller supplied that tag. It
SHALL NOT remove images by age or pattern, prune images or the build cache, or remove any image or
container that belongs to another run. Before it removes its image, the smoke SHALL stop and wait for
its own container, and only that container. A failure to clean up SHALL be reported on stderr and
SHALL NOT change the smoke's exit status.

#### Scenario: Smoke succeeds without a caller-supplied image
- **WHEN** the smoke runs without `IMAGE` and the detached-audit journey succeeds
- **THEN** the smoke exits successfully, the run's `agent-runner-dev-audit-smoke:<run-id>` tag no longer exists, and the run left no new dangling image

#### Scenario: Smoke fails without a caller-supplied image
- **WHEN** the smoke runs without `IMAGE` and the journey fails, times out, or produces invalid results
- **THEN** the smoke exits unsuccessfully and the run's image tag no longer exists

#### Scenario: Operator interrupts the smoke
- **WHEN** the operator interrupts a smoke run that did not receive `IMAGE`, while its container is still running
- **THEN** that run's container stops, the run's image tag is removed, and the smoke exits unsuccessfully

#### Scenario: Caller supplies an image tag
- **WHEN** the smoke runs with `IMAGE` set to a tag
- **THEN** the smoke builds and runs that tag, and the tag still exists after the smoke exits

#### Scenario: Shared development image exists
- **WHEN** `agent-runner-dev:local` exists before a smoke run that did not receive `IMAGE`
- **THEN** after the run, `agent-runner-dev:local` still refers to the same image as before

#### Scenario: Another run's image is present
- **WHEN** another smoke run's image or container exists on the host while this smoke runs and exits
- **THEN** this smoke leaves that image and container untouched

#### Scenario: Removing the image fails
- **WHEN** removing the run's image tag fails after the journey succeeded
- **THEN** the smoke reports the cleanup failure and the tag that remains, and still exits successfully

#### Scenario: Image build fails before an image exists
- **WHEN** the sandbox image build fails
- **THEN** the smoke exits unsuccessfully and does not report a cleanup failure for the image it never built

### Requirement: Docker smoke removes only its own artifact directory, and only after success

When the caller does not set `ARTIFACT_DIR`, the smoke SHALL create a temporary artifact directory
and own it. After a successful journey, the smoke SHALL remove that directory completely, including
files the container wrote. After a failure, a timeout, invalid results, or an interrupt, the smoke
SHALL keep that directory and print its path on stderr. A kept owned directory SHALL be handed to the
invoker: the smoke SHALL NOT delete it on any later run. When the caller sets `ARTIFACT_DIR`, the smoke
SHALL NOT delete that directory or its contents, whatever the outcome. Unattended callers SHOULD pass
`ARTIFACT_DIR` inside a directory whose lifecycle they manage, so failure evidence does not accumulate
under `${TMPDIR:-/tmp}`. A failure to remove an owned directory SHALL be reported with its path and
SHALL NOT change the smoke's exit status.

#### Scenario: Owned directory after success
- **WHEN** the smoke runs without `ARTIFACT_DIR` and the journey succeeds
- **THEN** the temporary artifact directory it created no longer exists

#### Scenario: Owned directory after failure
- **WHEN** the smoke runs without `ARTIFACT_DIR` and the journey fails or times out
- **THEN** the temporary artifact directory remains with its evidence, and its path appears in the smoke's stderr

#### Scenario: Owned directory after interrupt
- **WHEN** the operator interrupts a smoke run that did not receive `ARTIFACT_DIR`
- **THEN** the temporary artifact directory remains and its path appears in the smoke's stderr

#### Scenario: Caller-supplied directory after success
- **WHEN** the smoke runs with `ARTIFACT_DIR` set and the journey succeeds
- **THEN** that directory and the smoke's evidence inside it remain

#### Scenario: Kept evidence is not removed by a later run
- **WHEN** a smoke run without `ARTIFACT_DIR` fails and keeps its temporary artifact directory, and a later smoke run succeeds
- **THEN** the earlier run's directory still exists; only the later run's own directory is removed

#### Scenario: Unattended failure with a caller-managed artifact directory
- **WHEN** an unattended caller runs the smoke with `ARTIFACT_DIR` inside a directory it manages and the journey fails
- **THEN** the evidence remains in that caller-managed directory and nothing new is left under `${TMPDIR:-/tmp}`

#### Scenario: Artifact removal fails
- **WHEN** the owned artifact directory cannot be fully removed after a successful journey
- **THEN** the smoke reports the path it could not remove and still exits successfully

