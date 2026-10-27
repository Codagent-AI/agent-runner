## MODIFIED Requirements

### Requirement: Implement-change Validator skipping

The preserved `core:implement-change` versions, the audited `core/implement-change-v2.0.yaml` and the task-based `core/implement-change-v1.0.yaml`, SHALL accept `skip_validator` only as `true` or `false`. The value SHALL control every Agent Validator invocation owned by that workflow and the `core/verify-change-v1.0.yaml` workflow it composes, including task-level validation (v1.0 only), final validation, and validation after acceptance remediation. Each of those invocations SHALL also run only when `.validator/config.yml` exists in the project root. Skipping validation SHALL NOT skip draft pull-request creation, acceptance preparation, evidence generation, or the final acceptance handoff. The current `core:implement-change` version SHALL NOT accept `skip_validator`; it takes `validation_workflow` as `pluggable-validation` describes.

#### Scenario: Validation enabled
- **WHEN** a preserved `core:implement-change` version runs with `skip_validator=false` in a project with `.validator/config.yml`
- **THEN** final Agent Validator steps run, and task-level steps run in v1.0
- **AND** each acceptance round that remediates findings runs the `core:run-validator` workflow before pushing

#### Scenario: Validator not configured
- **WHEN** a preserved `core:implement-change` version runs with `skip_validator=false` in a project without `.validator/config.yml`
- **THEN** task-level (v1.0), final, and acceptance-round Agent Validator steps record their normal skipped outcomes, and no `agent-validator` command runs

#### Scenario: Validation skipped
- **WHEN** a preserved `core:implement-change` version runs or resumes with `skip_validator=true`
- **THEN** task-level (v1.0), final, and acceptance-round Agent Validator steps record their normal skipped outcomes
- **AND** acceptance remediation does not invoke Agent Validator
- **AND** the workflow continues through draft pull-request creation, acceptance preparation, evidence generation, and the final acceptance handoff

#### Scenario: Invalid skip value rejected
- **WHEN** a preserved `core:implement-change` version receives a `skip_validator` value other than `true` or `false`
- **THEN** deterministic parameter validation fails before implementation begins

### Requirement: Change v3 workflows implement through the audited workflow

The hidden `core:implement-change` workflow SHALL be backed by `core/implement-change-v3.0.yaml`, the current audited implementation workflow: one `implementor` session implements against a merged verification checklist, the `crosscheck` audits the result, and the current `core:verify-change` opens the draft pull request and prepares acceptance. It SHALL commit the approved definition itself through the current `core:commit-definition`, skipped for spec-driven changes, and SHALL NOT need a task plan. `core/implement-change-v2.0.yaml` SHALL remain embedded and readable by exact reference, and `core/implement-change-v1.0.yaml` SHALL remain embedded as the deprecated task-based implementation, referenced by file path from the v1.0 and v2.0 change workflows.

`openspec/change-v3.0.yaml` and `spec-driven/change-v3.0.yaml` SHALL be the selected `openspec:change` and `spec-driven:change`. Each SHALL run its validate, create, define, implement, accept, and finalize steps (plus archive for `openspec`) without a `plan` step, and its `implement` step SHALL invoke `../core/implement-change-v3.0.yaml` with `change_name`, `change_dir`, `change_label`, `change_kind`, `artifact_validation_instruction`, and `validation_workflow`. Their `accept` and `finalize` steps SHALL invoke `../core/accept-change-v2.0.yaml` and `../core/finalize-pr-v2.0.yaml`, and `core/implement-change-v3.0.yaml` SHALL compose `core/verify-change-v2.0.yaml`; `core/accept-change-v1.0.yaml`, `core/finalize-pr-v1.0.yaml`, and `core/verify-change-v1.0.yaml` SHALL remain embedded for the preserved workflows. No orchestrated implementation workflow SHALL be embedded.

#### Scenario: Latest change workflows are v3
- **WHEN** a user resolves `openspec:change` or `spec-driven:change`
- **THEN** the selected definition is the namespace's `change-v3.0.yaml`, and its earlier versions stay readable by exact reference

#### Scenario: Core implement-change resolves to the current audited workflow
- **WHEN** a user resolves `core:implement-change`
- **THEN** the selected definition is `core/implement-change-v3.0.yaml`, while `core/implement-change-v2.0.yaml` and `core/implement-change-v1.0.yaml` remain readable by exact reference and path

### Requirement: Verify-change workflow

The preserved `core/verify-change-v1.0.yaml` workflow, which the preserved `core/implement-change` v1.0 and v2.0 compose, SHALL hold the verification tail of a structured change: assumption review, simplification, final validation, a draft pull request, bounded acceptance-test rounds, and the acceptance handoff. It SHALL require `change_name`, `change_dir`, `change_label`, and `artifact_validation_instruction`, and SHALL accept `skip_validator` (default `false`), `acceptance_rounds` (default `3`), `skip_implementation_review` (default `false`), `stop_unchanged_acceptance` (default `false`), and `acceptance_fix_scope` (default: the scope instruction below). It SHALL validate `change_name`, `skip_validator`, `stop_unchanged_acceptance`, `skip_implementation_review`, and `acceptance_fix_scope` itself so it is valid standalone, and SHALL declare the `lead-agent` (`lead`) and `acceptance-tester` (`tester`) sessions identically to those `core/implement-change` versions, which SHALL compose it after their task-index verification so they share those named sessions. Every Agent Validator invocation in it SHALL also run only when `.validator/config.yml` exists in the project root; without that file, those steps record their normal skipped outcomes even when `skip_validator` is `false`. The current `core:verify-change` version is described by its observable validation behavior in `pluggable-validation`. The simplification step SHALL run in a new session of the `implementor` agent profile rather than the `lead-agent` session, SHALL review only the change's own diff, and SHALL NOT run Agent Validator or push. It SHALL commit behavior-preserving cleanups and any defect fixes separately with the `[simplify]` prefix, and SHALL record unfixed defects and findings needing a product, scope, or design decision in `acceptance-assumptions.md`, replacing the `No unresolved assumptions or context gaps.` statement when it adds the first entry. When `skip_implementation_review` is `true`, the assumption-review and simplification steps SHALL NOT run, and the check that `acceptance-assumptions.md` exists and is non-empty SHALL still run; the value SHALL be accepted only as `true` or `false`.

`acceptance_fix_scope` SHALL set which findings the lead resolves in each acceptance round's fix step. The fix prompt SHALL state the value where it states that scope, in place of any built-in scope wording. The default SHALL be exactly `Resolve every high-confidence finding you can, including implementation, CI, PR-state, tooling, setup, and environmental problems.`, so a caller that omits the param gets the same fix prompt as before the param existed. A caller-supplied value SHALL change only the scope sentence. The rest of the fix prompt SHALL stay the same: the triage into a fix, an escalation, or an assumptions-ledger entry; the rule that a reproduced violation of the approved specs is not dismissed as "not a defect"; preserving approved behavior, scope, and design; regression coverage, test limits, evidence, and commits; the retest reason; and recording human-only decisions in the ledger. No other step's prompt SHALL change with the value. A value that is empty or consists only of ASCII whitespace (space, tab, line feed, vertical tab, form feed, or carriage return) SHALL fail the workflow before any agent step runs, with an error naming `acceptance_fix_scope`, whether or not `jq` is installed. Any other character, including non-ASCII whitespace or literal backslash text such as `\f`, SHALL make the value non-blank.

No step of `core/verify-change-v1.0.yaml` SHALL use the Runner-owned `call_agent` tool; the independent tester is an ordinary workflow step in the `acceptance-tester` session. No step prompt SHALL run Agent Validator, directly or through a skill; validation SHALL run only as the `core:run-validator` workflow in its own step. The draft pull request SHALL be pushed and created or updated directly with `git` and `gh`.

Each acceptance round SHALL create `{{session_dir}}/scratch/acceptance-test` and pass it to the tester as `scratch_dir` for temporary files, clones, build outputs, and servers' working directories; the Runner leaves the folder in place, and it is removed with the run's session folder.

Each acceptance round SHALL clear the previous round's `acceptance-round-status.txt` and `acceptance-handoff.md`, keeping `exploration-log.md`, `acceptance-tested-revision.txt`, and `acceptance-findings.md` as the tester's history and diff base, run the tester, then run a deterministic convergence gate. The gate SHALL pass only when the tester's `acceptance-round-status.txt` ends with `READY <local HEAD SHA>`, `acceptance-tested-revision.txt` names local `HEAD`, `exploration-log.md` and `acceptance-handoff.md` in the evidence directory exist and are non-empty, and no tracked file has uncommitted changes. A passing gate SHALL end the rounds. Otherwise, except in the final round, the lead SHALL fix the findings without running Agent Validator or pushing, the `core:run-validator` workflow SHALL run unless `skip_validator` is `true`, a push step SHALL push the branch without force-pushing, and the shared draft-pull-request check that `verify-draft-pr` also uses SHALL verify, with retries, that the single open draft pull request's head equals local `HEAD`. The acceptance-round `core:run-validator` workflow SHALL record its result in `acceptance-validator-result.txt` in the session output directory, overwriting it each round, and a failing validator there SHALL NOT block the rounds or convergence. The final round SHALL end after its gate so no fix is left untested. When `stop_unchanged_acceptance` is `true`, a check directly after the lead's fix SHALL end the rounds, before validation and the push, when local `HEAD` equals the SHA in `acceptance-tested-revision.txt`, no tracked file has uncommitted changes, and the lead wrote no non-empty `acceptance-retest-reason.txt` in the session output directory. That file SHALL be kept for the next round's tester, whose prompt names it as the reason to recheck despite an unchanged revision, and SHALL be cleared before the lead's next fix. The value SHALL be accepted only as `true` or `false`, and when it is `false` the check SHALL NOT run. After the rounds, `acceptance-preparation-status.txt` SHALL record `ACCEPTANCE_COMPLETE` when the gate passes and `ACCEPTANCE_FAILED` otherwise, and on failure the workflow SHALL write a short `acceptance-handoff.md` giving local `HEAD`, the reasons, and pointers to the findings, the assumptions ledger, and any tester-written handoff, which it keeps as `acceptance-handoff-tester.md`. Writing the status SHALL be safe to rerun: a rerun SHALL NOT replace the kept tester handoff with a handoff the workflow generated. When the final handoff check fails, including on a resume that lands on it, its repair SHALL rerun the status step once rather than invoke an agent.

#### Scenario: Acceptance converges after a fix
- **WHEN** the tester reports defects in the first round and readiness for the fixed head in the second
- **THEN** the lead fixes, validation runs, the fix is pushed, the second round's gate passes, and the status is `ACCEPTANCE_COMPLETE`

#### Scenario: Rounds exhausted
- **WHEN** the tester never reports readiness within `acceptance_rounds` rounds
- **THEN** the tester runs `acceptance_rounds` times, the fix, validation, and push steps run only between rounds, the status is `ACCEPTANCE_FAILED`, and `acceptance-handoff.md` points to the open findings

#### Scenario: Red validator after an acceptance fix is recorded without blocking
- **WHEN** Agent Validator still fails after the lead fixes an acceptance round's findings, `skip_validator` is `false`, and `.validator/config.yml` exists
- **THEN** `acceptance-validator-result.txt` in the session output directory starts with `FAIL` and lists the failing checks, and the fix is still pushed and the next round still runs

#### Scenario: Stale evidence does not converge
- **WHEN** the tester's `READY` status names an earlier revision than local `HEAD`
- **THEN** the gate does not pass

#### Scenario: Simplification runs in a fresh implementor session
- **WHEN** `core/verify-change-v1.0.yaml` reaches its simplification step
- **THEN** the step starts a new session with the user's `implementor` profile, and the `lead-agent` session's history does not include it

#### Scenario: Simplification records an out-of-scope defect
- **WHEN** the simplification pass finds a defect in code the change did not touch
- **THEN** it leaves that code unchanged and `acceptance-assumptions.md` lists the defect in place of the no-unresolved-assumptions statement

#### Scenario: An acceptance fix that changes nothing ends the rounds
- **WHEN** `stop_unchanged_acceptance` is `true` and the lead's acceptance fix commits nothing and writes no retest reason
- **THEN** no further tester round, validation, or push runs, the status is `ACCEPTANCE_FAILED`, and `acceptance-handoff.md` points to the open findings

#### Scenario: A retest reason keeps the rounds going
- **WHEN** `stop_unchanged_acceptance` is `true` and the lead changes no tracked file but writes `acceptance-retest-reason.txt`
- **THEN** the next tester round runs with that file present, and the file is removed before the lead's next fix

#### Scenario: Caller skips the implementation review
- **WHEN** `core/verify-change-v1.0.yaml` runs with `skip_implementation_review=true`
- **THEN** neither the assumption review nor the simplification pass runs, and the run fails at the assumptions handoff check unless the caller already wrote a non-empty `acceptance-assumptions.md`

#### Scenario: Default acceptance-fix scope is unchanged
- **WHEN** a caller runs `core/verify-change-v1.0.yaml` without `acceptance_fix_scope` and an acceptance round's fix step runs
- **THEN** the lead's fix prompt tells it to resolve every high-confidence finding it can, including implementation, CI, PR-state, tooling, setup, and environmental problems, and is otherwise identical to the prompt before the param existed

#### Scenario: Caller narrows the acceptance-fix scope
- **WHEN** a caller passes `acceptance_fix_scope` telling the lead to fix only failures the change caused and to record unrelated CI, tooling, environmental, or `.github/workflows` problems in the assumptions ledger
- **THEN** the lead's fix prompt contains that instruction in place of the default scope sentence and does not contain the default wording, and the rest of the fix guidance, including the rule against dismissing a reproduced spec violation, is still present

#### Scenario: Scope affects only the acceptance-fix step
- **WHEN** a caller passes a non-default `acceptance_fix_scope`
- **THEN** the prompts of the assumption review, the simplification pass, the acceptance tester, and the acceptance-push repair are the same as with the default

#### Scenario: Empty acceptance-fix scope is rejected
- **WHEN** a caller passes `acceptance_fix_scope` as an empty value or one made only of ASCII whitespace, such as spaces, a form feed, or a vertical tab
- **THEN** the workflow fails before the assumption review or any other agent step runs, and the error names `acceptance_fix_scope`

#### Scenario: Preserved verify-change without validator configuration
- **WHEN** `core/verify-change-v1.0.yaml` runs with `skip_validator=false` in a project without `.validator/config.yml`
- **THEN** its final and acceptance-round validator steps record their normal skipped outcomes, no `agent-validator` command runs, and the workflow continues through the draft pull request and acceptance rounds

### Requirement: Plan-change Validator skipping

The `core:plan-change` workflow SHALL accept an optional `skip_validator` parameter, defaulting to `false` and accepted only as `true` or `false`. It SHALL validate the value before any planning agent runs. When it is `true`, committing the plan SHALL NOT invoke Agent Validator; in particular, it SHALL NOT run `agent-validator skip` to advance the Validator baseline. When it is `false` or omitted, committing the plan SHALL run `agent-validator skip` only when `.validator/config.yml` exists in the project root. `commit-change-plan.sh` SHALL treat an absent `skip_validator` input as `false`, and SHALL never run an Agent Validator command when `.validator/config.yml` does not exist. Its other caller, `core:commit-change-plan`, used by the current `openspec:simple-change`, SHALL advance the baseline only when the run's selected validation workflow runs Agent Validator, as `pluggable-validation` describes. Skipping validation SHALL NOT skip definition checking, task writing, task review, plan validation, or the plan commit. When the repository-root `openspec` path is a symbolic link and the change directory is under `openspec/`, `commit-change-plan.sh` SHALL NOT stage or commit anything. It SHALL report that the plan lives outside the repository and must be committed there by hand, apply the same `skip_validator` rule to the Validator baseline as on a normal commit, and exit successfully. Every workflow that commits a plan through `commit-change-plan.sh` gets this behavior with no new parameter. When `openspec` is not a symbolic link, committing the plan SHALL behave as before.

#### Scenario: Validator baseline advances by default
- **WHEN** `core:plan-change` runs without `skip_validator` in a project with `.validator/config.yml`
- **THEN** committing the plan runs `agent-validator skip` exactly as before

#### Scenario: Unconfigured project does not advance the baseline
- **WHEN** `core:plan-change` runs without `skip_validator` in a project without `.validator/config.yml`
- **THEN** the plan is committed and no Agent Validator command runs

#### Scenario: Validator skipped during planning
- **WHEN** `core:plan-change` runs with `skip_validator=true`
- **THEN** the plan is committed and no Agent Validator command runs during the plan step

#### Scenario: Plan already committed while skipping
- **WHEN** `core:plan-change` runs with `skip_validator=true` and the change directory is already committed
- **THEN** the commit step succeeds without running `agent-validator skip`

#### Scenario: Current simple-change follows its validation workflow
- **WHEN** the current `openspec:simple-change` commits its plan through `core:commit-change-plan` with `validation_workflow=none` in a project with `.validator/config.yml`
- **THEN** the plan is committed and no Agent Validator command runs

#### Scenario: Invalid planning skip value rejected
- **WHEN** `core:plan-change` receives a `skip_validator` value other than `true` or `false`
- **THEN** deterministic parameter validation fails before any planning agent runs

#### Scenario: Symlinked OpenSpec directory skips the plan commit
- **WHEN** `commit-change-plan.sh` runs without a `skip_validator` input in a project with `.validator/config.yml`, the repository-root `openspec` is a symbolic link to a directory in another repository, and the change directory is under `openspec/`
- **THEN** the script exits successfully, the repository gains no new commit, and nothing is staged
- **AND** it reports that the plan must be committed by hand in the repository that holds the OpenSpec project
- **AND** it runs `agent-validator skip`

#### Scenario: Symlinked OpenSpec directory while skipping the Validator
- **WHEN** `commit-change-plan.sh` runs with `skip_validator=true`, the repository-root `openspec` is a symbolic link, and the change directory is under `openspec/`
- **THEN** the script exits successfully without committing and without running any Agent Validator command
