## Context

The one-shot sandbox mounts the checkout read-only at `/agent-runner-source`, copies it without Git data to `/tmp/agent-runner-local`, and builds an untagged binary there. Host revision/dirty environment values are currently available only as external-tool diagnostics. A worktree's `.git` file can point outside the container mount.

Both audit model stages already call `crosscheckCommand`; its production implementation rejects non-Darwin platforms. Darwin uses `sandbox-exec` with a parameterized output directory. Codex receives a disposable runtime beneath model output. Existing `dev_audit,devaudit_e2e` builds replace the sandbox command with direct execution and therefore cannot establish confinement.

## Decisions

### Explicit build opt-in and source authority

Add `--dev-audit` to the one-shot sandbox script, including help and dry-run output. Only that explicit option adds the development tag and audit linker assignments. Keep default sandbox builds and releases untagged; retain `dev.sh` and `make build` behavior. The opt-in is build selection, not a runtime audit setting.

The compiled checkout copy is expendable. Inject `/agent-runner-source` as the source root, with available host build revision and dirty diagnostics through safely quoted linker arguments. Audit launch validates and snapshots that mounted source. Never substitute the working project, `/eval-input`, or copied build directory for authoritative Runner source.

A complete source tree does not require accessible Git internals. Record unavailable launch-time revision/dirty information explicitly when a worktree's Git metadata is inaccessible, without fabricating it from build environment values or claiming a clean checkout. Source coverage and Git metadata availability are distinct. Keep missing/wrong/incomplete source publication guards and build-to-launch mismatch diagnostics.

### One production model-launch boundary

Keep the platform decision behind the shared command factory used by both value and correctness stages. Linux must establish OS-enforced write confinement before executing the adapter command, inherited by model descendants. The output subtree is the only writable ordinary filesystem area; runtime home, cache, and temporary files belong beneath it. Reads and existing network behavior are retained.

Choose the concrete Linux OS mechanism during implementation against these fixed constraints: it must work in the delivered Docker development invocation, actually deny filesystem operations, establish confinement before model execution, and report unsupported or denied setup without running the model. The backend implementation is not a new user-selectable mode. Document its minimum platform prerequisites and any narrowly necessary Docker settings. Do not require privileged containers or introduce a fallback that runs unconfined.

Review resolution: use Bubblewrap with a read-only root and a writable model-output bind, and retain Docker seccomp filtering through a pinned Moby default-deny profile with user/mount namespace and mount-operation additions. No extra container capabilities are granted. A final Linux model-exec helper marks nonstandard inherited descriptors close-on-exec before executing the adapter, requiring Linux 5.11 or newer; failure remains diagnostic. The profile provenance and exact additions are documented in `docker/dev/README.md`.

Validate/canonicalize boundary paths, reject an output allowance that overlaps trusted input or resolves through an unsafe escape, and prevent inherited writable filesystem descriptors from defeating confinement. Test traversal, symlink, rename, and child-process writes. Preserve ordinary standard stream operation. Fingerprints remain defense in depth; detection after a write is not confinement.

Retain Darwin's parameterized `sandbox-exec` path and disposable Codex runtime. Accept the existing read/network behavior explicitly; do not claim a new restriction on either. Sequoia requires real OS evidence, not an assumed equivalence with other macOS versions.

### Existing contracts, made explicit

Eligibility uses the resolved canonical workflow reference passed to finalization: after the optional `builtin:` prefix, its namespace segment must be exactly `openspec/` or `spec-driven/`. Display names, arbitrary filenames, and enclosing project names do not grant eligibility. Preserve top-level terminal-session requirements, all three terminal outcomes, idempotence, non-recursion, and distinct resumed execution identities.

Use the source's recorded profile-set selection with configuration available at audit launch, including existing layering, inheritance, and built-in defaults. Absence of an explicit project `crosscheck` stanza is not itself an error. Freeze the CLI/model/effort actually resolved and use it for both stages. Invalid configuration, unresolved selection, or unavailable invocation remains an audit diagnostic.

### Detached smoke belongs to Agent Runner

Deliver `scripts/docker-dev-audit-smoke.sh` with fixtures owned by this repository. Use isolated temporary project/home/artifact directories, deterministic fake adapter executables, no host credentials, no default secret import, and no real reporting services. The smoke passes through the delivered sandbox opt-in and real production model launcher.

Prefer an ordinary fixture workflow loaded through the existing resolver. If deterministic canonical registration requires a test-only asset, a separate fixture-only build tag may register that asset but MUST NOT replace the sandbox, coordinator, profile resolver, source snapshotter, or completion logic. The ordinary `--dev-audit` binary must also be verified independently. The existing `devaudit_e2e` bypass is not permitted as isolation evidence.

Review resolution: the smoke explicitly selects `--dev-audit --dev-audit-smoke`, compiling `dev_audit,devaudit_smoke`. The additional tag only registers the hidden canonical fixture. Ordinary development-audit builds do not register it.

Use structured fake model responses for both stages, zero correctness candidates, and missing Google connection state. A retained local report with a reporting warning is expected; successful remote delivery is not required. Record model invocation evidence under permitted output, not an unconstrained marker outside it.

The container's foreground smoke supervisor launches the source CLI and stays alive after that CLI returns. It discovers the audit through durable reciprocal linkage and polls until both lifecycle and audit state are terminal and expected outputs are present. Use a bounded deadline, tolerate in-progress atomic writes, and fail with preserved diagnostics on timeout, launch failure, missing model stages, or invalid report. Do not change normal Runner execution to wait for auditing. Test source-return-before-audit-finish using deterministic fixture coordination, not timing guesses.

Hermetic means the audit execution has only local fixtures and no external service effects. Image/toolchain provisioning is separate; pre-provision image and Go dependencies before network-disabled execution. No acceptance claim is based solely on building an image or printing Docker arguments.

## Risks and Acceptance

- Docker kernel/security policy may deny the selected Linux mechanism. The negative path must remain safe, but a negative-only run does not satisfy Linux support acceptance.
- Worktree metadata can be unavailable inside Docker. Preserve truthful partial Git provenance while verifying the actual source contents.
- Arbitrary agent CLIs may expect writable state elsewhere. Keep the boundary and report unsupported behavior; do not grant general home or temporary-directory writes.
- Smoke process lifetime can race detached completion. The supervisor and terminal-state checks are part of the product-owned test harness.
- Sequoia availability is an execution prerequisite, not a reason to replace its required acceptance with another OS.

## Migration

No settings or run-state migration. Users opt into the sandbox build flag; existing untagged behavior remains the rollback path. Historical audits remain readable. See `test-plan.md` for required evidence before implementation acceptance.
