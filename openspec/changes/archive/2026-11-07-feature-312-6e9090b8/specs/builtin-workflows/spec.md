## MODIFIED Requirements

### Requirement: Change v3 workflows implement through the audited workflow

The hidden `core:implement-change` workflow SHALL be backed by `core/implement-change-v2.0.yaml`, the audited implementation workflow: one `implementor` session implements against a merged verification checklist, the `crosscheck` audits the result, and `core:verify-change` opens the draft pull request and prepares acceptance. It SHALL commit the approved definition itself through `core:commit-definition`, skipped for spec-driven changes, and SHALL NOT need a task plan. `core/implement-change-v1.0.yaml` SHALL remain embedded as the deprecated task-based implementation, referenced by file path from the v1.0 and v2.0 change workflows.

Between its `implement` step and its `audit` step, `core/implement-change-v2.0.yaml` SHALL run the implementation completion loop (see `implementation-completion-loop`), and the loop SHALL be the only gate on implementation completeness before the audit. It SHALL NOT contain a `complete-evidence` step, and SHALL NOT run a pre-audit evidence check with a rerun repair. Its post-audit `complete-evidence-after-review` and `recheck-evidence` steps SHALL keep their current behavior. The completion judge SHALL run as a fresh `tester` session for each judgment, separate from the `acceptance-tester` session.

The steps outside the loop SHALL keep their IDs. A run recorded against the earlier layout, at a step or repair target that no longer exists (for example `complete-evidence`), SHALL fail to resume with an error naming the missing step. It SHALL NOT silently resume at a different step. A run recorded as having completed `implement` under the earlier layout lacks the implementor's captured final response. When it reaches the completion judge, the judge step SHALL fail with an undefined-variable error naming `implementor_response`, and no judge agent SHALL run without that response.

`openspec/change-v3.0.yaml` and `spec-driven/change-v3.0.yaml` SHALL be the selected `openspec:change` and `spec-driven:change`. Each SHALL run its validate, create, define, implement, accept, and finalize steps (plus archive for `openspec`) without a `plan` step, and its `implement` step SHALL invoke `../core/implement-change-v2.0.yaml` with `change_name`, `change_dir`, `change_label`, `change_kind`, `artifact_validation_instruction`, and `skip_validator`. No orchestrated implementation workflow SHALL be embedded.

#### Scenario: Latest change workflows are v3
- **WHEN** a user resolves `openspec:change` or `spec-driven:change`
- **THEN** the selected definition is the namespace's `change-v3.0.yaml`, and its earlier versions stay readable by exact reference

#### Scenario: Core implement-change resolves to the audited workflow
- **WHEN** a user resolves `core:implement-change`
- **THEN** the selected definition is `core/implement-change-v2.0.yaml`, while `core/implement-change-v1.0.yaml` remains readable by exact reference and path

#### Scenario: Completion loop sits between implement and audit
- **WHEN** `core/implement-change-v2.0.yaml` is loaded
- **THEN** its steps run `implement`, then the completion loop, then `audit`, with no `complete-evidence` step and no pre-audit `check-evidence` repair
- **AND** `complete-evidence-after-review` and `recheck-evidence` still follow `resolve-findings`

#### Scenario: Judge session is separate from the acceptance tester
- **WHEN** the completion judge runs and the run later reaches acceptance
- **THEN** the acceptance tester's session has none of the judge's history

#### Scenario: Run recorded after implement under the earlier layout
- **WHEN** a run recorded with `implement` completed and no `implementor_response` capture is resumed against the updated workflow, and its completion check fails
- **THEN** the judge step fails with an undefined-variable error naming `implementor_response`, and no judge agent process starts

#### Scenario: Run recorded in a removed step
- **WHEN** a run recorded with a failed `check-evidence` repair whose rerun target is `complete-evidence` is resumed against the updated workflow
- **THEN** resume fails with an error naming the missing step `complete-evidence`
