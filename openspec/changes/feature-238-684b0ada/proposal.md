## Why

Seven OpenSpec change folders remain unarchived under `openspec/changes/` on `main` even though their implementations shipped: `async-mcp`, `call-agent-skill`, `make-evals-great`, `make-pty-great`, `repair-development-audit-recovery`, `start-run`, and `testing-evidence-fixes`. While they sit there, `openspec/specs/` does not fully describe current behavior, `openspec list` reports finished work as active, and later changes and agents read stale or missing requirements. Some deltas were never synced (for example, there is no `call-agent-skill`, `workflow-discovery`, `workflow-param-form`, or `workflow-definition-view` main spec), some were synced by hand (the `cli-adapter` autonomous-permission-mode scenarios already appear in the main spec), and some are older than later changes to the same capability.

Archiving them as written is unsafe. Archiving applies each delta to `openspec/specs/`, so an old delta can overwrite a newer requirement. Known case: `make-pty-great/specs/cli-adapter/spec.md` says autonomous-interactive yolo "applies the same yolo-mode flag set it would apply in autonomous-headless". Open PR #232 (issue #231) replaces that rule with Claude `auto` mode in `openspec/specs/cli-adapter/spec.md`. Re-asserting the old text would make the main spec contradict the code, or conflict with #232.

Paul's decision (2026-10-09) settles direction: reconcile each change's deltas against the current code and newer specs, let the current code and newer requirements win any conflict, and then archive.

## What Changes

- Reconcile every spec delta in the six changes that carry deltas against the current main specs and the code on `main`. Each delta requirement falls into one of five cases:
  - **Apply:** it is missing or outdated in the main spec and matches the code. Its text is edited where needed to match the code.
  - **Reflected:** the main spec already says it. Omit it.
  - **Superseded:** newer requirements or the code changed it. Include a corrected version only where the main spec is wrong or missing something.
  - **Unimplemented:** the code does not do it. Omit it and record it for a follow-up issue.
  - **Inapplicable:** for example, `REMOVED` entries for the `pseudo-terminal` and `agent-continue-trigger` capabilities, which no longer exist. Omit it.
- Write the reconciled result as **this change's own spec deltas**, with one file per affected capability under `openspec/changes/feature-238-684b0ada/specs/`. Archiving this change applies them to `openspec/specs/`.
- Archive all seven old changes as authored with `openspec archive <change> --yes --skip-specs`. Their original deltas are kept in the archive as the historical record, but are not applied. `start-run`'s three tasks are confirmed implemented on `main` and are checked off before it is archived.
- Leave the two `cli-adapter` requirements that PR #232 edits untouched: "No permission loosening in interactive mode" and "Adapters honor autonomous permission mode". This change's deltas neither modify nor remove them.
- Do not keep a contradictory main-spec scenario to satisfy the archive guard. A scenario that cannot stay true is retired by removing its requirement and adding a correctly named replacement, for example in `step-control-channel` and `agent-calls`.
- Run `openspec validate --specs --strict --no-interactive` on the resulting main specs. It must exit successfully, and its results must cover every main spec.

No code, workflow YAML, or runtime behavior changes.

## Capabilities

### New Capabilities

- `call-agent-skill`: the contract of the `codagent:call-agent` skill, which this repo installs through `internal/agentplugin` and its built-in workflows depend on. The requirements are written from the current skill text.
- `workflow-discovery`: enumeration of workflows across project, user, and builtin scopes, shadowing, ordering, and invalid-file entries.
- `workflow-param-form`: the parameter form shown before launching a workflow from the New tab.

### Modified Capabilities

- `agent-calls`, `step-control-channel`, `cli-adapter`, `cursor-cli-support`, `step-model`, `builtin-workflows`: deltas from `call-agent-skill`, `async-mcp`, and `testing-evidence-fixes`.
- `interactive-terminal-handoff`, `audit-log-entries`: deltas from `make-pty-great`.
- `lightweight-audit-reporting`: deltas from `repair-development-audit-recovery`.
- `list-runs`, `new-tab-layout`, `view-run`: deltas from `start-run`. The planned `workflow-definition-view` capability is folded into `view-run`, which already owns the definition preview.

`interactive-shell-steps`, `live-run-view`, `workflow-execution`, and `automatic-run-audit` already reflect their deltas and do not change.

## Technical Approach

The archive tool imposes three constraints:
- A delta requirement can be applied only once: `ADDED` fails if the requirement already exists, and `MODIFIED` fails if it is absent.
- `MODIFIED` cannot drop a scenario name already in the main spec.
- Several of the main specs were already edited by hand in the implementing commits.

Applying each old change's deltas in sequence would therefore fail or reintroduce stale behavior. Instead, one consolidated set of deltas is produced in this change. It takes each main spec as it stands today and turns it into a description of the current code. The old changes are then archived as historical records with `--skip-specs`.

The reconciliation works in creation order, with later changes layered over earlier ones. The code on `main` is treated as ground truth: each classification was checked against the relevant packages and tests. `reconciliation.md` records each requirement's classification, the main-spec scenarios that were retired, and the follow-up items.

The sequence was checked as a dry run in a temporary copy: archive the seven old changes with `--skip-specs`, then archive this change normally. It applies +44 / ~17 / -3 requirements, and `openspec validate --specs --strict` passes all 83 main specs.

Risk: across about 80 requirements, it is easy to reconcile something subtly wrong. Mitigations: a recorded reconciliation baseline (`main` at `629bd3d4`) with a content-drift check before archiving, a per-requirement evidence record, a dry-run diff of each main spec, strict validation, and leaving the requirements PR #232 owns untouched.

## Out of Scope

- Any code, test, or workflow YAML change, including fixing mismatches between the code and a spec. If a spec requirement turns out to be unimplemented, record it and leave it for a follow-up issue. Do not implement it.
- The Claude `auto`-mode requirement for autonomous-interactive yolo, which belongs to PR #232 / issue #231.
- Restructuring or rewording main specs beyond what reconciliation needs.
- Already-archived changes and the change folder for this issue.

## Impact

- `openspec/specs/**`: new and updated capability specs.
- `openspec/changes/`: the seven folders move to `openspec/changes/archive/<archive-date>-<name>/`.
- New main specs created by archiving start with OpenSpec's placeholder `Purpose: TBD`, the same as 26 existing main specs.
- PR #232 does not conflict textually with this change: the two `cli-adapter` requirements it edits are left untouched.
- No impact on users, the binary, or APIs.
