- [x] Implement the change described by these files, using TDD per `CLAUDE.md`. Satisfy every spec scenario and every `INT-*`/`E2E-*` obligation in the test plan:
  - add `const automaticAuditEnabled = false` and gate the hook registration in `internal/devaudit/provider_enabled.go`;
  - add the mirrored skip guards and the paused-mode CLI E2E test in `internal/devaudit/cli_e2e_test.go`;
  - add the replay fallback to `scripts/docker-dev-audit-smoke-container.sh`, with its cases in `scripts/docker_dev_audit_smoke_test.py`;
  - update `docs/dev/sandbox.md`.

  Delete no audit code, workflows, or tests. Finish with `make fmt`, `make test`, `make lint`, and `python3 scripts/docker_dev_audit_smoke_test.py` passing. Source files:
  - [proposal.md](proposal.md)
  - [specs/automatic-run-audit/spec.md](specs/automatic-run-audit/spec.md)
  - [specs/development-audit-availability/spec.md](specs/development-audit-availability/spec.md)
  - [specs/development-audit-sandbox/spec.md](specs/development-audit-sandbox/spec.md)
  - [design.md](design.md)
  - [test-plan.md](test-plan.md)
  - [decisions.md](decisions.md)
