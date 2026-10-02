## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

No new integration or end-to-end obligation is warranted:

- The simplification step's contract (`agent: implementor`, `session: new`, `mode: autonomous`, and the key prompt phrases) is a workflow YAML contract, covered by the rewritten unit test in `workflows/verify_change_test.go`.
- Dispatching a step with `agent:` and `session: new` is existing Runner behavior that is already tested; this change adds no Runner code.
- The gate scripts (`.validator/deadcode.sh`, `.validator/duplication.sh`) and the strict lint configuration are proven by running against this repository in Agent Validator and in the existing CI `lint` job, including on this change's own pull request. Fixture-based script tests would need deadcode and jscpd in the `test` job for little added value; the acceptance pass injects violations instead.
- The pass's agent behavior (diff-scoped cleanup, defect routing) depends on a real model and is non-deterministic, so it is exercised by the acceptance pass, not an automated test.

## Integration Tests

None.

## End-to-End Tests

None.

## Acceptance Testing Envelope

- Environments and sandboxes: a scratch clone of this repository under the tester's `scratch_dir`, on a throwaway branch, running the CLI from source with `./dev.sh`. Install the pinned tools there, not globally: `GOBIN=<scratch_dir>/bin go install golang.org/x/tools/cmd/deadcode@v0.50.0` and `npm install --prefix <scratch_dir>/npm jscpd@4.3.0`, with both on `PATH` for the session.
- Credentials and secrets: the user's existing agent CLI logins and `~/.agent-runner/` profiles (read only). No other credentials are needed.
- Authorized effects:
  - Inject violations in the scratch clone to confirm each gate fails, then passes once removed: a new function over gocognit/cyclop 25, a new Go clone of 100+ tokens (including in a `_test.go` file), a new `if` nest of depth 5 or more, a new dead exported function, and YAML or shell clones that push jscpd above 3.5%. Also confirm a `//nolint:dupl // <reason>` suppression passes both lint tiers.
  - Run `./dev.sh run core:verify-change --until simplify` with valid parameters on a throwaway branch carrying a small seeded change, using the user's real profiles. This runs `review-assumptions` (lead) and `simplify` (implementor), costs roughly $2 to $5, and stops before any push or pull-request step. Commits stay local to the scratch clone; delete the clone when done.
- Off limits: pushing, creating or editing pull requests, changing `origin` or any remote, editing `~/.agent-runner/` configuration, global tool installs, and modifying the working checkout outside the scratch clone.
- Permitted substitutes: if the real agent run cannot be performed (CLI login or profile unavailable), confirm from the audit log of a `--until` run that the `simplify` step dispatches a new `implementor` session rather than `lead-agent`, and record the agent-behavior scenarios as an unexercised limitation.
- Known risk areas:
  - `dupl` on test files may flag structurally similar table tests.
  - The jscpd ceiling has about 26 lines of headroom (3.15% measured against 3.5%).
  - deadcode does not check functions in files compiled into only one build (accepted in design).
  - The validator's strict tier diffs against the local `origin/main`, which may be stale.
  - The agent may grow code while deduplicating, or touch code outside the diff, despite the prompt. The `[simplify]` commits and `acceptance-assumptions.md` from the real run are the evidence for scope, code growth, separation of cleanup and defect-fix commits, and ledger routing.
  - The strict tier does not see deeper nesting, higher complexity, or a lower maintainability index added inside existing functions or nests (accepted in design); only new functions, new nests, and new clones are gated.
  - Whether the pass finds and records a defect in untouched code is non-deterministic; seed one near the change to give it a fair chance, and report what happened rather than forcing it.

## Human-Only Testing

None.

## Coverage Map

No requirement or journey has an additional `INT-*`, `E2E-*`, or `HT-*` obligation.
