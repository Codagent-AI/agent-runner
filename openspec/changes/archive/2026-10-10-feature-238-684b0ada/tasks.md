- [x] Archive the seven completed OpenSpec changes left on `main`, without applying their deltas, following `design.md` "Approach". This is a spec-only change, so there is no TDD: do not change code, tests, workflow YAML, or anything under `openspec/specs/`. This change's own deltas under `specs/` are applied later, by the factory archive step.

  **Mark start-run complete**
  - Check the three boxes in `openspec/changes/start-run/tasks.md`. `reconciliation.md` records the evidence that each task is implemented on `main`.

  **Archive the old changes**
  - Run `openspec archive <name> --yes --skip-specs` in this order: `start-run`, `make-evals-great`, `make-pty-great`, `call-agent-skill`, `async-mcp`, `repair-development-audit-recovery`, `testing-evidence-fixes`.
  - Each folder must land unchanged, apart from the `start-run` checklist, under `openspec/changes/archive/<YYYY-MM-DD>-<name>/`.
  - If an archive fails, fix the cause. Never delete or hand-move a folder to force the move.
  - Do not archive `feature-238-684b0ada`, and do not run `openspec archive` without `--skip-specs`.

  **Check for content drift** (design step 4)
  - For each of these capabilities, run `git diff 629bd3d4eede21f828fb79dfd3b3f506e628959f -- openspec/specs/<capability>`: `agent-calls`, `audit-log-entries`, `builtin-workflows`, `call-agent-skill`, `cli-adapter`, `cursor-cli-support`, `interactive-terminal-handoff`, `lightweight-audit-reporting`, `list-runs`, `new-tab-layout`, `step-control-channel`, `step-model`, `view-run`, `workflow-discovery`, `workflow-param-form`.
  - Confirm that `openspec/specs/call-agent-skill`, `openspec/specs/workflow-discovery`, and `openspec/specs/workflow-param-form` do not exist.
  - If any diff is non-empty:
    - Re-reconcile the affected entries in this change's `specs/` against the current code and the newer requirement text, following the per-requirement rules in `reconciliation.md`. Keep every current main-spec scenario name in a `MODIFIED` block, or use REMOVED plus ADDED under a new name.
    - Do not touch the two `cli-adapter` requirements that PR #232 owns: "No permission loosening in interactive mode" and "Adapters honor autonomous permission mode".
    - Record the new baseline and each refresh in `reconciliation.md` and `decisions.md`.
    - Repeat the check against the new baseline.

  **Verify**
  - `openspec list` shows only `feature-238-684b0ada` as active.
  - `git diff --stat 629bd3d4eede21f828fb79dfd3b3f506e628959f -- openspec/specs` is empty, unless drift was re-reconciled above.
  - `openspec validate feature-238-684b0ada --strict --no-interactive` passes.
  - Dry-run the factory archive. Copy `openspec/` to a temporary directory, run `openspec archive feature-238-684b0ada --yes` there, then run `openspec validate --specs --strict --no-interactive`, which must exit 0 with no failures. Expect +44 added, ~17 modified, −3 removed, unless drift changed the totals.
  - Read the dry run's diff of each touched main spec. Every removed or replaced line must be accounted for in `reconciliation.md`. Delete the temporary directory afterwards, and never run this archive in the real tree.
  - `make test` passes.

  **Commit**
  - Commit the archive moves and the `start-run` checklist edit as `chore: archive completed openspec changes left on main`. Add a `[<step_id>]` prefix when a workflow step creates the commit.

  Source files:
  - [proposal.md](proposal.md)
  - [design.md](design.md)
  - [test-plan.md](test-plan.md)
  - [reconciliation.md](reconciliation.md)
  - [decisions.md](decisions.md)
  - [specs/agent-calls/spec.md](specs/agent-calls/spec.md)
  - [specs/audit-log-entries/spec.md](specs/audit-log-entries/spec.md)
  - [specs/builtin-workflows/spec.md](specs/builtin-workflows/spec.md)
  - [specs/call-agent-skill/spec.md](specs/call-agent-skill/spec.md)
  - [specs/cli-adapter/spec.md](specs/cli-adapter/spec.md)
  - [specs/cursor-cli-support/spec.md](specs/cursor-cli-support/spec.md)
  - [specs/interactive-terminal-handoff/spec.md](specs/interactive-terminal-handoff/spec.md)
  - [specs/lightweight-audit-reporting/spec.md](specs/lightweight-audit-reporting/spec.md)
  - [specs/list-runs/spec.md](specs/list-runs/spec.md)
  - [specs/new-tab-layout/spec.md](specs/new-tab-layout/spec.md)
  - [specs/step-control-channel/spec.md](specs/step-control-channel/spec.md)
  - [specs/step-model/spec.md](specs/step-model/spec.md)
  - [specs/view-run/spec.md](specs/view-run/spec.md)
  - [specs/workflow-discovery/spec.md](specs/workflow-discovery/spec.md)
  - [specs/workflow-param-form/spec.md](specs/workflow-param-form/spec.md)
