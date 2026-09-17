## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records additional integration, end-to-end, agent-acceptance, and exceptional human-only obligations. Use implementation-time TDD for substantive behavior changes and extend existing nearby tests rather than duplicating their coverage.

The user approved production Linux confinement proof, explicit Sequoia acceptance, build/provenance and workflow/profile contracts, and a hermetic detached Docker smoke. Deterministic fake agents and synthetic authentication are the approved substitutes for real model calls and personal credentials. They MUST run inside the real production filesystem sandbox. No real external service, Agent Evals execution or modification, host credential import, or external publication is authorized.

Unit tests cover isolated eligibility decisions, option parsing, schema/validation logic, and error classification; do not duplicate their full case matrix here. Preserve existing lifecycle, replay, reporting, and source-outcome regression coverage. The new E2E journey is deliberately small. Existing `devaudit_e2e` tests remain useful for their original purposes but do not establish isolation.

Provision Docker images/toolchains/dependencies before the hermetic execution phase. Run that phase with external network access disabled and no default secret import. Offline Go module availability is a prerequisite, not a reason to enable networking during model/audit execution. Use the Go version in `go.mod`, targeted tests during implementation, then repository-wide untagged and tagged checks. Temporary test-owned binary builds are expected; do not use `make build` as a generic verification step.

## Integration Tests

### INT-001: Sandbox build boundary and mounted-source provenance
- Covers: development-audit-availability build/source requirements and development-audit-sandbox opt-in.
- Boundary: shell option handling, actual Go build, tagged CLI registration, and audit request/snapshot creation.
- Setup: temporary builds/configuration and a read-only Runner source mount; include a worktree with inaccessible Git indirection and a distinct evaluated project/build-copy path. Reuse `build_boundary_integration_test.go` and `scripts/sandbox_scripts_test.go` where appropriate.
- Action: inspect help/dry-run argument boundaries; build default and opted-in binaries; exercise private audit command recognition and create a request from the mounted source.
- Assertions: the default binary has no audit command, injected hidden asset, or active audit hook; explicit opt-in has all three. Runtime configuration cannot activate the untagged binary. Opt-in injects `/agent-runner-source`; request/snapshot contents correspond to that mount. Unknown launch revision/dirty state is not synthesized from build values. Missing/wrong/incomplete source still blocks source-verified publication; a complete source tree with unavailable Git metadata is distinguished from missing source. Dry-run exposes no secrets and preserves argv/mount behavior.
- Execution: script/package integration tests plus actual build/request checks in the Docker environment. Shell dry runs alone are insufficient for build/provenance assertions.

### INT-002: Linux production filesystem boundary
- Covers: all Linux confinement, runtime, and enforcement-proof requirements in audit-model-isolation.
- Boundary: the actual Linux OS sandbox launcher, subprocess execution, descendant processes, filesystem, and adapter runtime setup.
- Setup: supported Linux Docker environment; writable control files in a source project, live run data, evidence snapshot, Runner source snapshot, unrelated home/temp locations, and the allowed output directory. Use synthetic authentication, paths with spaces, and an output symlink targeting a protected file. Establish that the same unconfined fixture can modify the targets.
- Action: invoke real subprocess probes through the production command factory and exercise both value and correctness stage wiring. Attempt create, overwrite/truncate, deletion, cross-boundary rename/link, traversal/symlink escape, and descendant writes; exercise writable runtime home/cache/temp and read-only auth.
- Assertions: output/runtime writes and required reads succeed; protected changes fail at the attempted operation and all protected files remain unchanged. No overly broad home/temp allowance exists. Cleanup removes disposable runtime only. Both stages use the launcher. Writable inherited descriptors and unsafe boundary paths cannot bypass confinement.
- Execution: Linux `internal/devaudit` tagged integration tests and the supported Docker environment. An unsupported-kernel skip may describe a generic host limitation but MUST NOT satisfy required Linux success-path evidence.

### INT-003: Confinement failures preserve source outcomes
- Covers: fail-closed launch, unsupported platforms, and existing source-outcome authority.
- Boundary: sandbox setup failure through model-stage diagnostics and linked-audit lifecycle handling.
- Setup: controlled unavailable/denied backend setup and invalid output boundaries; an observable fake model start marker; successful and failed source fixtures with saved terminal result, completion state, exit status, and resumability.
- Action: cause setup failure before model exec and allow linked-audit handling to finish.
- Assertions: model start marker is absent; diagnostic identifies unsupported/denied/invalid setup; no direct-exec fallback runs; source result and resumability remain unchanged. Only permitted audit-link/lifecycle metadata changes. Normal runtime configuration cannot enable a test bypass.
- Execution: tagged package integration tests; use an actual denied/unavailable Linux setup where possible, alongside narrow fault injection for deterministic setup errors. Mock errors do not replace INT-002 real confinement proof.

### INT-004: Workflow identity and launch-time role resolution
- Covers: automatic-run-audit eligibility and development-audit-availability role requirements.
- Boundary: normal workflow resolution/finalization, coordinator, layered profile configuration, frozen request, and both model invocations.
- Setup: one canonical eligible workflow and one similarly named unrelated workflow; source-recorded profile differing from current active selection; inherited/built-in crosscheck definitions; isolated user/project config. Add invalid recorded selection/role and unavailable CLI variants at this boundary.
- Action: finalize workflows; resolve and freeze the audit request; change profile configuration after freezing; exercise both fake model stages.
- Assertions: resolved canonical identity controls launch; display/path naming does not broaden eligibility. Existing top-level terminal-session, duplicate handling, and no-recursion contracts are preserved. Source-recorded selection wins, inheritance/defaults work, both stages invoke the frozen CLI/model/effort, and resolution/invocation failures remain linked-audit diagnostics.
- Execution: tagged coordinator/config-to-model integration tests. Isolated boolean eligibility cases remain unit tests.

### INT-005: Darwin sandbox and disposable runtime regression
- Covers: retained Darwin behavior and model-runtime requirements.
- Boundary: real `sandbox-exec`, parameterized paths, child processes, and disposable runtime/authentication handling.
- Setup: actual macOS; synthetic authentication; writable control targets and output paths containing spaces/quotes. Extend existing `TestSandboxedCrosscheckCanExecuteAndOnlyWriteAuditOutput` and `TestSandboxedCodexGetsDisposableWritableRuntime`.
- Action: execute the real confined probes and both stage wiring where needed.
- Assertions: allowed output/runtime writes and auth reads succeed; outside and child writes fail; authentication/trusted evidence stay unchanged; cleanup succeeds. Existing read/network policy is not reported as newly restricted.
- Execution: Darwin tagged integration tests. Record actual OS version. Running on a different macOS release does not discharge AT-003 Sequoia acceptance.

## End-to-End Tests

### E2E-001: Hermetic Docker source-to-terminal-linked-audit journey
- Covers: product-owned smoke, production model confinement, source/audit independence, reciprocal identity, and mounted provenance.
- Surface: delivered `scripts/docker-dev-audit-smoke.sh` through `scripts/sandbox-run.sh --dev-audit` and the real source CLI.
- Setup: pre-provisioned Docker image/dependencies; isolated temporary project/home; product-owned eligible fixture; deterministic fake crosscheck executables producing valid value and empty correctness-candidate responses; no Google connection or host auth; external network disabled. Keep fixture hooks limited to data/workflow registration if required; do not use `devaudit_e2e` or replace production launch/lifecycle logic.
- Journey: build/select the opted-in binary, run the source, deterministically hold the fake audit long enough to observe source return, release it from the supervisor, follow durable linkage, and wait for lifecycle and audit state to become terminal with required outputs.
- Assertions: source exits successfully before audit completion; exactly one matching execution-session link exists in both directions; both confined model stages actually execute; valid local observations/report and correctness output exist; provenance names the mounted Runner source; missing reporting configuration is a local warning; source success is unchanged. A sandbox failure or empty terminal audit cannot count as smoke success.
- Failure variants: hold the fake audit past a configurable short deadline to prove bounded failure and artifact preservation; produce a terminal audit missing required output to prove nonzero smoke exit. Exercise these as harness integration checks where possible rather than repeating the full image build.
- Execution: product-owned Docker smoke after relevant implementation changes, with a documented command and artifact directory. Preserve source/audit state, request, lifecycle, stage diagnostics, local report, OS/kernel/container configuration, and exit statuses. Configure or document a repeatable automation entry point that treats missing prerequisites as failure/incomplete, not a green smoke.

## Agent Acceptance Tests

### AT-001: Developer selects and inspects the sandbox audit build
- Classification: Required.
- Covers: opt-in/default behavior, documentation, profile/eligibility expectations, mounted provenance.
- Actor and surface: developer using documented shell commands, `sandbox-run.sh` help/dry-run, and actual sandbox CLI.
- Setup: trusted Runner checkout, isolated fake project/profile and artifact directory; pre-provisioned Docker; no secrets.
- Steps: follow development docs to inspect the default and opted-in builds; verify the default rejects private audit capability; inspect an opted-in audit request generated from an eligible fixture; explain from visible configuration which inherited crosscheck definition was used; confirm the docs state Linux prerequisites and fail-closed behavior, retained Darwin/Sequoia behavior, and the distinction between filesystem write confinement and read/network restrictions.
- Expected: docs suffice to select the build and understand eligibility/profile and Linux isolation prerequisites, explain fail-closed behavior and retained Darwin/Sequoia behavior, and accurately distinguish write confinement from read/network restrictions; source root is `/agent-runner-source`; unavailable Git metadata is explicit; default remains untagged.
- Evidence: exact commands, concise command output, relevant request/provenance/profile fields, artifact paths, and documentation section references covering the operating contracts checked above.
- Effects and cleanup: local temporary builds/containers/files only; retain evidence and remove temporary runtime directories.
- Permitted substitutes: deterministic fake agents and synthetic config/auth. Dry-run may verify command presentation only, not actual binary capability or source provenance.

### AT-002: Developer observes detached Docker audit completion
- Classification: Required.
- Covers: source-return/audit-terminal distinction and delivered smoke usability.
- Actor and surface: developer running the documented product-owned smoke and inspecting its persisted artifacts.
- Setup: supported Linux Docker environment, pre-provisioned dependencies, network-disabled local fixture execution, no credentials.
- Steps: run the documented smoke command; observe separate source completion and linked-audit completion; inspect both model-stage outputs, report, reciprocal link, and confinement evidence; use the harness's controlled timeout fixture to inspect failure reporting.
- Expected: successful smoke waits for real terminal audit evidence and retains a local reporting warning; timeout exits nonzero with actionable local diagnostics. The source CLI itself remains non-blocking.
- Evidence: smoke output and exit status; source/audit identities; source result; terminal lifecycle/state; validated outputs; confinement probes and environment identity.
- Effects and cleanup: temporary local Docker resources and artifacts only; retain evidence, terminate test-owned descendants, remove transient runtime data.
- Permitted substitutes: fake model responses and absent external reporting connection are required. No replaced sandbox, dry-run-only execution, Agent Evals run, or real external service call.

### AT-003: Explicit macOS Sequoia acceptance
- Classification: Required.
- Covers: retained Darwin filesystem behavior and disposable runtime, explicitly on macOS Sequoia.
- Actor and surface: agent acting as a developer at a Sequoia terminal, using a temporary tagged CLI and product-owned fake audit fixture plus real OS sandbox probes.
- Setup: macOS Sequoia, synthetic credentials, isolated home/project/artifacts, no real service calls; record `sw_vers`.
- Steps: run a representative eligible audit using fake models through the real Darwin launcher; observe source return and linked-audit terminal results; demonstrate allowed output/runtime writes and denied outside writes with actual subprocesses; inspect preserved synthetic auth and source state.
- Expected: both stages work within the existing write boundary, source outcome remains authoritative, runtime cleanup preserves original authentication, and the report/diagnostics remain local. Existing read/network behavior is explicitly accepted; no new restriction is claimed.
- Evidence: OS version, executed commands, source and linked-audit identity/state, model outputs, actual permitted/denied write results, auth/source comparison, and cleanup result.
- Effects and cleanup: local temporary files/processes only; no personal credentials, model cost, publication, or reporting service interaction.
- Permitted substitutes: fake agents/synthetic auth replace external services. Another macOS version, Linux, an argument-generation assertion, or a mocked `sandbox-exec` cannot replace Sequoia execution. If Sequoia is unavailable, record acceptance as incomplete.

## Human-Only Testing

None. Platform availability alone does not make the Sequoia flow inherently human-only; an agent with that platform and tools can perform it.

## Coverage Map

| Requirement or journey | INT | E2E | AT | HT |
| --- | --- | --- | --- | --- |
| Explicit sandbox opt-in and untagged exclusion | INT-001 | E2E-001 (opt-in only) | AT-001 | — |
| Authoritative mounted source and honest Git provenance | INT-001 | E2E-001 | AT-001 | — |
| Linux production write confinement and runtime | INT-002 | E2E-001 | AT-002 | — |
| Unavailable confinement and source-outcome preservation | INT-003 | E2E-001 (source-outcome preservation only) | AT-002 (source-outcome preservation only) | — |
| Canonical workflow identity and frozen inherited crosscheck | INT-004 | E2E-001 | AT-001 | — |
| Retained Darwin behavior and explicit Sequoia acceptance | INT-005 | — | AT-003 | — |
| Detached smoke completion, timeout, diagnostics, and hermetic execution | — | E2E-001 | AT-002 | — |
| Development documentation and usable commands | — | — | AT-001, AT-002 | — |
