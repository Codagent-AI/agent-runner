## Context

Seven change folders under `openspec/changes/` describe work that has already shipped: `start-run`, `make-evals-great`, `make-pty-great`, `call-agent-skill`, `async-mcp`, `repair-development-audit-recovery`, and `testing-evidence-fixes`. Most of their requirements were copied into `openspec/specs/` by hand in the implementing commits, some under different names. Later changes then moved the code and the specs further.

This change's `specs/` already holds the reconciled outcome: one set of deltas for 15 capabilities, checked against the code on `main`. `reconciliation.md` records how each old requirement was handled.

Constraints that shape the design:

- **openspec 1.6.0 apply rules.** `ADDED` fails if the requirement already exists in the main spec. `MODIFIED` and `REMOVED` fail if it is absent. `MODIFIED` also fails if it drops a scenario name the main spec still has (`dist/core/specs-apply.js:207-223`). `openspec archive --skip-specs` moves a change without applying its deltas.
- **Factory pipeline.**
  - The factory feature workflow (`.agent-runner/workflows/factory-feature-v1.0.yaml`) archives *this* change itself after implementation. Its `archive` step runs `builtin:openspec/archive-change-v1.0.yaml`, which calls `workflows/openspec/archive-transition.sh`. That script runs `openspec validate --type change` and then `openspec archive feature-238-684b0ada --yes`.
  - It commits only the archive directory and the spec files the archive changed.
  - The `verify` step then runs `openspec validate --specs --strict`.
- **PR #232 is open and not merged.** It edits two `cli-adapter` requirements, "No permission loosening in interactive mode" and "Adapters honor autonomous permission mode". This change must not touch them.

## Goals / Non-Goals

**Goals:**

- Leave `openspec/changes/` holding only active work: the seven folders move under `openspec/changes/archive/`.
- After the factory archives this change, `openspec/specs/` describes the current code for every capability those seven changes touched, and strict validation passes.
- Keep the original old deltas readable in the archive as history.

**Non-Goals:**

- Code, test, or workflow YAML changes. The follow-ups in `reconciliation.md` are left for separate issues.
- Editing `openspec/specs/` directly during implementation. Spec application happens only when the factory archives this change.
- Rewording main-spec requirements that none of the seven changes touched.

## Approach

Implementation is a pure file move plus one checklist edit. The spec changes ride on this change's own archive.

```
implementation (tasks)                        factory archive step (after implementation)
──────────────────────                        ────────────────────────────────────────────
1. check off start-run/tasks.md               openspec archive feature-238-684b0ada --yes
2. openspec archive <old> --yes --skip-specs     └─ applies specs/* deltas → openspec/specs/
   × 7 (oldest first)                         verify: openspec validate --specs --strict
3. verify: no openspec/specs diff,
   dry-run of the factory archive passes
```

1. **Mark `start-run` tasks complete.** Check the three boxes in `openspec/changes/start-run/tasks.md`. The reconciliation confirmed each one is implemented on `main`:
   - `internal/discovery`
   - `internal/listview/newtab.go` and `internal/runview` (definition view)
   - `internal/paramform`, with launch wiring in `cmd/agent-runner/main.go`

   The per-task files under `tasks/` contain no checkboxes.
2. **Archive the old changes without applying specs.** In creation order, run `openspec archive <name> --yes --skip-specs` for `start-run`, `make-evals-great`, `make-pty-great`, `call-agent-skill`, `async-mcp`, `repair-development-audit-recovery`, and `testing-evidence-fixes`.
   - openspec names each archive directory `<YYYY-MM-DD>-<name>` using the run date.
   - The folders keep their content unchanged, apart from the `start-run` checklist edit.
   - `make-evals-great` holds only `.openspec.yaml`; `--skip-specs` makes it archivable even though it has no deltas.
3. **Do not archive `feature-238-684b0ada` and do not edit `openspec/specs/`.** The factory's archive step does both. Archiving this change early would make that step fail: it expects to perform the archive itself.
4. **Check for content drift since the reconciliation baseline.**
   - The reconciliation was done against `main` at `629bd3d4eede21f828fb79dfd3b3f506e628959f`. Every delta is a full copy of main-spec text at that revision, recorded in `reconciliation.md`.
   - OpenSpec's archive guard checks only that requirement names exist and that scenario names are kept. A newer edit to a requirement body, or to a scenario body under an unchanged name, would be silently overwritten by this change's MODIFIED block, or silently deleted by its REMOVED entry. Strict validation would still pass.
   - So before committing, run `git diff 629bd3d4eede21f828fb79dfd3b3f506e628959f -- openspec/specs/<capability>` for each of the 15 capabilities under this change's `specs/`: agent-calls audit-log-entries builtin-workflows call-agent-skill cli-adapter cursor-cli-support interactive-terminal-handoff lightweight-audit-reporting list-runs new-tab-layout step-control-channel step-model view-run workflow-discovery workflow-param-form. Also confirm that `openspec/specs/call-agent-skill`, `workflow-discovery`, and `workflow-param-form` still do not exist.
   - Any non-empty diff means `main` moved under the reconciliation. For each requirement in that diff, re-reconcile the affected delta entries against the then-current code and newer requirement text, even if OpenSpec would accept the stale delta. Record the new baseline revision and each refresh decision in `reconciliation.md` and `decisions.md`, then repeat this check against the new baseline.
5. **Verify before committing.**
   - `openspec list` shows only `feature-238-684b0ada`.
   - `git diff --stat main -- openspec/specs` is empty.
   - `openspec validate feature-238-684b0ada --strict --no-interactive` passes.
   - Dry-run the factory's archive in a temporary copy: copy `openspec/` to a temp dir, run `openspec archive feature-238-684b0ada --yes` there, then run `openspec validate --specs --strict --no-interactive`. It must exit 0 with no failures. Never run this archive in the real tree.
   - Compare the dry run with the expected totals of +44 added, ~17 modified, and −3 removed requirements.
   - Read the full dry-run diff of each touched main spec against the baseline revision. Every removed or replaced line must be one that `reconciliation.md` accounts for, and no text newer than the baseline may be lost.

**Failure behavior.**
- If an old archive fails, for example because an archive directory with that name already exists, stop and fix the cause. Never delete a folder to force a move.
- If `main` has moved, the drift check in step 4 decides what to refresh, whether or not the dry run fails. Refresh each affected block from the current main spec, keeping the reconciled edits only where the current code still supports them. A dry-run failure on a `MODIFIED` block is just one symptom of the same drift.

## Decisions

- **One consolidated delta set, with the old changes archived using `--skip-specs`.** This beats editing each old delta in place and archiving it normally:
  - The hand-synced main specs would make many old `ADDED` and `MODIFIED` entries fail.
  - The scenario guard would block retiring false scenarios.
  - Any requirement applied once by an old archive and again by this change's archive would collide.

  It also keeps the whole reconciliation reviewable and validated as a single delta.
- **The spec changes are applied by the factory archive step, not by implementation.** This matches how every factory feature reaches `openspec/specs/`. Applying them during implementation would duplicate the requirements, and the archive step would fail.
- **Retire false scenarios by replacing the whole requirement.** Three requirements are removed and re-added under new names:
  - `step-control-channel` "Fresh authenticated attempt" becomes "Fresh authenticated attempt credential".
  - `agent-calls` "Cancellation propagation" becomes "Agent-call cancellation propagation".
  - `new-tab-layout` "Workflow groups render with header and description" becomes "Workflow group headers" plus "Plan with an agent entry". This is the only way the apply rules allow a scenario name to leave the main spec.
- **Accept openspec's `Purpose: TBD` header on the three new main specs** (`call-agent-skill`, `workflow-discovery`, `workflow-param-form`). Pre-creating stub main specs with a real purpose was tested and rejected: an empty `## Requirements` section fails `openspec validate --specs --strict`, which the validator runs during implementation. 26 existing main specs already carry `TBD`, so this follows repository convention.
- **Guard against content drift with a recorded baseline, not archive failures.** The archive guard compares names only, so a git-diff check of the touched main specs against `629bd3d4` is the cheapest reliable detector. No tooling or workflow change is needed.
- **Archive in creation order.** The order only affects the order of the dated directories on the same day. Following creation order keeps the history readable.

## Risks / Trade-offs

- **`main` moves before this change is archived.**
  - If PR #232 merges first, it only touches the two `cli-adapter` requirements this change leaves alone, so the archive still applies. A trailing-line textual conflict in `openspec/specs/cli-adapter/spec.md` would be a trivial git merge.
  - If another change edits a requirement that this change MODIFIES or REMOVES, the archive guard does **not** reliably catch it. It checks names, not bodies, so a newer body or scenario text would be silently overwritten. The baseline drift check (Approach, step 4) is the safeguard, not archive failure.
  - Residual window: the factory merges `origin/main` into the branch when a run starts or resumes (`prepare-branch`). If a resumed run reaches the archive step without re-running implementation, the drift check does not run again. Once this change is archived, a later `main` edit to the same lines conflicts in git and needs a human merge. The PR's archive diff against the baseline is the last review point.
- **Reconciliation mistakes.** About 80 requirements were classified by hand against the code. Mitigations:
  - `reconciliation.md` cites code evidence for each one.
  - The dry-run diff shows exactly what lands in `openspec/specs/`.
  - Strict validation checks structure.
  - Review of this PR checks the content.
- **Loss of the old deltas' wording.** None: the old folders are archived unchanged, so the original intent stays readable next to the reconciled result.

## Migration Plan

No runtime migration. Rollback means reverting the PR's commits, which moves the folders back and undoes the spec application.

## Open Questions

None. The follow-up issues to file are listed in `reconciliation.md`.
