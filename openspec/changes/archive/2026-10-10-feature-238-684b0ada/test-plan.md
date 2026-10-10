## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only obligations.

This change is spec-only. It moves seven completed OpenSpec change folders into `openspec/changes/archive/` and carries reconciled spec deltas that the factory's archive step applies to `openspec/specs/`. It changes no code, tests, or workflow YAML, so there is no runtime boundary or user journey for a new automated test to exercise.

Every added or modified requirement describes behavior that already exists on `main` and is already covered by that package's tests. `reconciliation.md` cites those tests, for example `internal/discovery/discovery_test.go`, `internal/paramform`, `internal/control/control_test.go`, `internal/exec/agent_call_test.go`, and `internal/interactive/process_test.go`. Repository rules also forbid adding test suites.

The change's correctness is checked by OpenSpec tooling, not by Go tests:
- `openspec validate feature-238-684b0ada --strict --no-interactive`
- a temporary-copy dry run of `openspec archive feature-238-684b0ada --yes`
- after the factory archive, `openspec validate --specs --strict --no-interactive`

The design's verification steps and the factory `verify` step already require all three. They check structure only. Content preservation is checked separately by the design's baseline drift check: `git diff 629bd3d4eede21f828fb79dfd3b3f506e628959f -- openspec/specs/<capability>` must be empty for each of the 15 touched capabilities, and the full dry-run diff must be reviewed against `reconciliation.md`. That check is part of implementation verification and, like validation, is not a Go test. The existing `go test ./...` suite must stay green, which also proves nothing outside `openspec/` changed.

## Integration Tests

None. No component boundary changes. The behavior the reconciled specs describe already has tests next to its source packages, and duplicating them here would add no detection value.

## End-to-End Tests

None. No public entry point changes. A spec-only change cannot break a user journey, and the OpenSpec checks above cover the change's own deliverable.

## Acceptance Testing Envelope

The exploratory pass checks that each reconciled requirement in `specs/` matches what the product actually does. Use the cheapest faithful means:
- read the cited code and tests;
- run the package's existing tests;
- for TUI-visible requirements (New tab, search, param form, definition view, post-run Escape), drive `./dev.sh` under a PTY reconstructed with `pyte`, as described in `CLAUDE.md`.

Then confirm that the archived result in `openspec/specs/` is what the dry run predicted.

- **Environments and sandboxes:**
  - The local checkout of this branch.
  - Temporary copies of `openspec/` for archive dry runs.
  - `./dev.sh` for the CLI and TUI from source.
  - Throwaway directories with `.agent-runner/workflows/` fixtures for discovery and shadowing checks.
  - Disposable `HOME` directories for user-scope workflows.
- **Credentials and secrets:** none are needed. Agent CLI accounts on the machine must not be used to launch real agent runs.
- **Authorized effects:**
  - Local file writes inside temporary directories and the PTY harness.
  - Running `go test` on existing packages.
  - Shell-only fixture workflows launched through `./dev.sh`, with no agent steps.
  - Remove all temporary directories afterwards.
- **Off limits:**
  - Running `openspec archive` in the real tree for `feature-238-684b0ada`; the factory archive step owns it.
  - Editing `openspec/specs/` or code.
  - Launching real agent CLIs (Claude, Codex, Cursor, Copilot), which costs money and needs personal accounts.
  - Pushing, opening PRs, or filing issues.
  - Touching PR #232 or the two `cli-adapter` requirements it owns.
  - The user's real `~/.agent-runner`.
- **Permitted substitutes:**
  - Agent-call, step-control-channel, CLI-adapter argument, and Cursor discovery requirements are verified through their existing package tests and code reading instead of live agent sessions. A real CLI conversation is off limits.
  - Starting a run with `r` does not take effect under a synthetic PTY (`CLAUDE.md`). Confirm launch wiring through `cmd/agent-runner` tests such as `start_run_test.go` and `start_run_escape_test.go`.
- **Known risk areas:**
  - Reconciliation accuracy in `agent-calls` and `step-control-channel`. There, the code differs from the async-mcp design: `call_agent` waits by default, and Cursor alone has a 45-second wait budget. A lost client does not cancel the child.
  - Scenario names kept with corrected bodies.
  - The removed-and-renamed requirements "Fresh authenticated attempt credential" and "Agent-call cancellation propagation".
  - `audit-log-entries`, whose event list was rebuilt from `internal/audit/types.go`.
  - Requirements narrowed to what the code does after approach review:
    - Cursor-only exclusion of called children in parent session discovery.
    - OpenCode rejecting interactive steps, so it has no interactive completion path.
    - Deterministic-only note filtering in `lightweight-audit-reporting`.
    - The New tab "Plan with an agent" entry as the initial row and the target of upward navigation.
  - Content drift: any `main` edit to a touched spec after `629bd3d4` must have been re-reconciled, and the archived text must not drop newer wording.
  - **Accepted limitations, not defects of this change:**
    - The follow-ups listed in `reconciliation.md`, including post-run Escape losing the run from Current Dir, `q` quitting from text inputs, and Copilot/Codex completion approval.
    - `Purpose: TBD` on the three new main specs.
    - The `cli-adapter` scenarios deferred until PR #232.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| (none: no additional obligations beyond specs, existing package tests, and OpenSpec validation) | — | — | — |
