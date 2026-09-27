## ADDED Requirements

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
SHALL keep that directory and print its path on stderr. When the caller sets `ARTIFACT_DIR`, the smoke
SHALL NOT delete that directory or its contents, whatever the outcome. A failure to remove an owned
directory SHALL be reported with its path and SHALL NOT change the smoke's exit status.

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

#### Scenario: Artifact removal fails
- **WHEN** the owned artifact directory cannot be fully removed after a successful journey
- **THEN** the smoke reports the path it could not remove and still exits successfully

## MODIFIED Requirements

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
