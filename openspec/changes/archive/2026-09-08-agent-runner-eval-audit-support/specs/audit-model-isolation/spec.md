## ADDED Requirements

### Requirement: Model stages require operating-system filesystem confinement

A development-audit build SHALL run both value and correctness model stages on supported Linux and Darwin systems only after establishing operating-system-enforced filesystem write confinement. Model processes and their descendants MUST be unable to modify the source project, live source-run evidence, snapshotted evidence, Runner source, or other ordinary filesystem paths outside the audit-owned model-output subtree. Reads and existing network behavior MAY remain available. This requirement SHALL NOT expose audit capability in untagged binaries.

#### Scenario: Linux model stages run with confinement
- **WHEN** both model stages execute on a supported Linux Docker development environment
- **THEN** each uses the production confinement launcher, can read required evidence and write model output, and cannot write outside the allowed subtree

#### Scenario: Audit model stage runs on Darwin
- **WHEN** a development audit reaches a model-driven stage on Darwin and the required operating-system sandbox is available
- **THEN** Agent Runner launches the resolved crosscheck using the existing parameterized filesystem sandbox with writes restricted to model output

#### Scenario: Model spawns a child
- **WHEN** a model process starts a child that attempts a protected filesystem write
- **THEN** the child remains confined and the protected file is unchanged

#### Scenario: Model attempts boundary escape
- **WHEN** a model attempts an outside write through traversal, an output symlink to protected data, or rename/link operations across the boundary
- **THEN** the operation cannot modify protected data

### Requirement: Unavailable confinement fails closed

If the operating system, kernel facility, sandbox executable, permissions, or sandbox setup cannot enforce the boundary, Agent Runner MUST NOT launch the affected model command unconfined. It SHALL retain an explicit linked-audit diagnostic and preserve the source outcome, completion state, resumability, and process exit status. Neither a test escape hatch nor a runtime fallback SHALL enable unconfined model execution in an ordinary development-audit binary.

#### Scenario: Linux confinement setup is rejected
- **WHEN** the Linux environment rejects the required confinement setup
- **THEN** no model payload executes, the linked audit records the cause, and the source result is unchanged

#### Scenario: Audit model stage runs on an unsupported operating system
- **WHEN** a model stage reaches an operating system without a supported confinement backend
- **THEN** the linked audit records an unsupported-platform diagnostic without launching the model or changing the source result

#### Scenario: Allowed output path is unsafe
- **WHEN** the configured output boundary overlaps trusted inputs or resolves to an unsafe location
- **THEN** launch fails diagnostically before model execution

### Requirement: Model runtime state remains inside the write allowance

Agent Runner SHALL keep its disposable model runtime home, cache, and temporary files under the audit-owned model-output subtree. Existing Codex authentication may be read from its selected source but MUST remain unmodified. Cleanup SHALL remove disposable runtime state without modifying source authentication or evidence.

#### Scenario: Disposable Codex runtime is usable
- **WHEN** a fake Codex executable runs through the real Linux or Darwin launcher
- **THEN** it can read synthetic source authentication and create runtime state in its disposable home and temporary directory while writes to source authentication are denied

#### Scenario: Disposable runtime is cleaned
- **WHEN** the model invocation finishes
- **THEN** disposable runtime directories are removed and source authentication and trusted inputs retain their original contents

### Requirement: Isolation evidence exercises production launch behavior

Linux isolation verification SHALL execute real subprocesses through the launcher used by ordinary `dev_audit` binaries. A replaced launcher, successful argument construction, filesystem modes alone, or a post-run fingerprint SHALL NOT count as enforcement proof. Darwin acceptance SHALL explicitly include macOS Sequoia and retain its existing write confinement and disposable runtime behavior.

#### Scenario: Linux confinement proof runs
- **WHEN** the Linux isolation suite is executed
- **THEN** an unsandboxed control can write the test targets, sandboxed output writes succeed, protected write attempts fail, and protected files remain unchanged

#### Scenario: Sequoia acceptance runs
- **WHEN** an agent performs required macOS Sequoia acceptance
- **THEN** the recorded OS version and real sandboxed execution demonstrate allowed writes, denied writes, readable synthetic authentication, and preserved source state

#### Scenario: Required platform is unavailable
- **WHEN** Linux success-path or Sequoia acceptance cannot execute on the required platform
- **THEN** the missing evidence is reported as incomplete acceptance rather than a passing substitute
