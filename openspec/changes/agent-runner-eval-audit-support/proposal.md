## Why

Private development audits cannot currently run their model stages in the Linux Docker sandbox: the sandbox builds an untagged binary and the audit's filesystem launcher supports only Darwin. Existing CLI E2E fixtures bypass that launcher, so they do not prove Linux isolation.

## What Changes

- Add explicit `scripts/sandbox-run.sh --dev-audit` build opt-in, preserving the untagged default and existing local development build behavior.
- Inject authoritative `/agent-runner-source` provenance and distinguish build diagnostics from verified launch-time source.
- Add fail-closed Linux model filesystem confinement through the real audit launcher; retain Darwin behavior and require macOS Sequoia acceptance.
- Specify and test existing canonical workflow eligibility and launch-time `crosscheck` resolution.
- Add a hermetic, product-owned Docker smoke that exercises the real confinement boundary and waits for the detached linked audit to become terminal.
- Document operation and limitations in development documentation and record acceptance obligations in `test-plan.md`.

## Capabilities

### New Capabilities
- `audit-model-isolation`: Linux and Darwin model write confinement, fail-closed behavior, and production-path proof.
- `development-audit-sandbox`: explicit Docker build opt-in and a hermetic detached-audit smoke.

### Modified Capabilities
- `development-audit-availability`: supported sandbox build path, truthful source provenance, and explicit role inheritance; move the Darwin-only isolation requirement into the new cross-platform capability.
- `automatic-run-audit`: clarify canonical namespace eligibility without broadening eligible workflows.

## Out of Scope

Running or modifying Agent Evals; real model, GitHub, Google, or other external service calls during verification; new audit runtime enablement or profile settings; broader workflow eligibility; release audit capability; new TUI flows; changes to reporting policy or source-run outcomes. Existing read and network behavior is retained; this is filesystem write confinement, not a new confidentiality or network isolation policy.

## Impact

Changes affect `scripts/sandbox-run.sh`, supporting Docker development setup as needed, `internal/devaudit/`, nearby tests, product-owned smoke fixtures/scripts, and development documentation. Ordinary untagged builds remain inert. No run-data or user-config migration is intended.
