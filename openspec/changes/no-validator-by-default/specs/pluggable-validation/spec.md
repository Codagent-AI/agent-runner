## ADDED Requirements

### Requirement: Validation workflow selection

The current OpenSpec and spec-driven `change` (v3.0), `simple-change` (v2.0), and `scaffold` (v1.0) workflows SHALL accept an optional `validation_workflow` parameter. Its default SHALL be `auto`.

The parameter SHALL accept any of:
- the short names `auto`, `agent-validator`, or `none`;
- a `builtin:` workflow reference;
- an absolute path to a workflow file.

Every validation point in the run SHALL run the selected workflow. A short name that is not recognized, a reference or path that does not resolve, or a target that is not a valid workflow SHALL fail pre-run validation before any step runs. These workflows SHALL NOT declare a `skip_validator` parameter.

#### Scenario: Default selection
- **WHEN** a current `change` workflow runs without `validation_workflow`
- **THEN** every validation point runs the built-in `auto` validation workflow

#### Scenario: Custom workflow by absolute path
- **WHEN** a current `change` workflow runs with `validation_workflow` set to the absolute path of a valid workflow file in the project
- **THEN** pre-run validation loads that file from disk
- **AND** every validation point runs it

#### Scenario: Unresolvable selection fails before the run starts
- **WHEN** `validation_workflow` names an unknown short name or a file that does not exist
- **THEN** pre-run validation fails with an error naming the target
- **AND** no step runs

### Requirement: Built-in validation workflows

Agent Runner SHALL provide three built-in validation workflows.

**`auto`** SHALL check, each time it runs, whether `.validator/config.yml` exists in the project root.
- When the file exists, it SHALL behave exactly as `agent-validator`.
- When it does not exist, it SHALL pass without invoking `agent-validator`, and SHALL NOT require the `agent-validator` executable to be installed.

**`agent-validator`** SHALL run Agent Validator's bounded retry loop. When the `agent-validator` executable is not installed, or `.validator/config.yml` does not exist, the validation step SHALL fail with an error naming what is missing, rather than pass.

**`none`** SHALL pass without running anything.

#### Scenario: auto without configuration
- **WHEN** `auto` runs in a project without `.validator/config.yml`, and `agent-validator` is not installed
- **THEN** the validation point passes without invoking `agent-validator`

#### Scenario: auto picks up configuration created during the run
- **WHEN** a scaffold run creates `.validator/config.yml` before its validation point and runs with `auto`
- **THEN** that validation point runs Agent Validator

#### Scenario: Explicit agent-validator without configuration
- **WHEN** `agent-validator` is selected and the project has no `.validator/config.yml`
- **THEN** the validation step fails with an error that names the missing configuration file

#### Scenario: none
- **WHEN** `none` is selected
- **THEN** every validation point passes without invoking `agent-validator`

### Requirement: Validation repairs run in an implementation-capable session

Each validation point SHALL pass the validation workflow the name of the session that repairs findings. That session SHALL be an implementation-capable session chosen by the calling workflow.

The built-in `agent-validator` workflow SHALL make its repairs in that session. It SHALL never use whichever session ran most recently, such as an acceptance or flow tester that is forbidden to fix code. A validation point MAY also pass a task file and a task-compliance setting for validation workflows that use them.

#### Scenario: Repair after acceptance testing
- **WHEN** validation runs directly after the agent acceptance rounds and Agent Validator reports violations
- **THEN** the repairs are made in the session the calling workflow named
- **AND** they are not made in the acceptance-tester session

### Requirement: Unresolved validation warns and continues

When validation still fails after its bounded repairs, at any validation point:
- the validation step SHALL end with warning status;
- it SHALL preserve the final failure output as evidence;
- the workflow SHALL continue.

No validation point SHALL block draft pull-request work, human review, archival, marking the pull request ready, or pushing a CI fix. A run whose only problem is unresolved validation SHALL complete with warnings. The human-review handoff and the run's final status SHALL show any unresolved validation failure.

#### Scenario: Red validation after acceptance testing
- **WHEN** validation after the agent acceptance rounds remains failing after its repairs
- **THEN** the workflow proceeds to human review
- **AND** the review handoff identifies the unresolved validation failure

#### Scenario: Red validation on a CI fix
- **WHEN** validation of a committed CI fix remains failing after its repairs
- **THEN** the fix is still pushed
- **AND** CI runs on it, and the step carries a warning

### Requirement: Validation points in change workflows

The current OpenSpec and spec-driven `change` workflows SHALL run the selected validation workflow only at these points:
1. after the agent acceptance rounds end, before human review;
2. after human review, requested refinements, and any re-acceptance and recovery have settled, and before archival (OpenSpec) and finalization;
3. after each CI fix is committed, before that fix is pushed.

No other step of these workflows SHALL invoke Agent Validator. No step prompt, and no skill these workflows invoke, SHALL instruct an agent to run Agent Validator.

Between validation points, agents SHALL verify their fixes with the project's own relevant tests and checks, as the project's `AGENTS.md` describes, and never with `agent-validator` commands. When validation at the second point commits fixes that change behavior, the tester SHALL retest that behavior.

#### Scenario: Typical change
- **WHEN** a change passes acceptance after one fix round, receives human refinements, and CI passes on the first push
- **THEN** the validation workflow runs exactly twice: after acceptance testing and after human review settles

#### Scenario: Acceptance fix is not validated immediately
- **WHEN** the lead fixes an acceptance round's findings
- **THEN** no validation runs before the next acceptance round
- **AND** that fix is validated at the next validation point

#### Scenario: CI fix validated before push
- **WHEN** CI fails and the fixer commits a fix
- **THEN** the validation workflow runs on that fix before it is pushed

#### Scenario: Behavior-changing fixes at the second point are retested
- **WHEN** validation after human review commits fixes that change behavior
- **THEN** the tester retests the affected behavior before archival or finalization

### Requirement: Validation points in simple-change workflows

The current OpenSpec and spec-driven `simple-change` workflows SHALL run the selected validation workflow at two points:
1. after flow testing;
2. after the user's review settles.

They SHALL NOT validate after each task. The rule against agent instructions to run Agent Validator SHALL apply to them as well. Because these workflows create no pull request, they SHALL NOT push after validation.

#### Scenario: Simple change validation
- **WHEN** a `simple-change` run implements its tasks, completes flow testing, and the user finishes review with one requested fix
- **THEN** validation runs after flow testing and once after review
- **AND** it never runs between tasks

### Requirement: Validation fixes reach the pull request

At each `change` validation point that has a pull request, the workflow SHALL then push the branch without force-pushing, and verify that the open pull request's head equals local `HEAD`. The check SHALL accept the pull request in either draft or ready-for-review state.

#### Scenario: Fixes from human-phase validation are pushed
- **WHEN** validation after human review commits fixes
- **THEN** those commits are pushed, and the pull request head equals local `HEAD` before archival or finalization begins

#### Scenario: Ready pull request
- **WHEN** validation of a CI fix runs after the pull request has been marked ready
- **THEN** the head check passes for the ready pull request

### Requirement: Tested and validated revisions are tracked separately

The acceptance result SHALL record the revision the tester exercised. Commits made by validation after that revision SHALL NOT invalidate the acceptance result.

The human-review handoff SHALL list the commits validation made since the tested revision. The re-acceptance recommendation SHALL cover every change since the last tested revision, including validation commits. When the user declines re-acceptance, the workflow SHALL record that those commits received no exploratory testing.

#### Scenario: Validation commits after a passing acceptance
- **WHEN** acceptance passes on revision A and validation then commits hardening fixes, producing revision B
- **THEN** the acceptance result remains complete for A
- **AND** the review handoff lists the commits between A and B

#### Scenario: Declined re-acceptance
- **WHEN** the user declines re-acceptance while validation commits remain untested
- **THEN** the handoff records those commits as not exploratory-tested

### Requirement: Validator bookkeeping follows the selected validation workflow

Agent Validator bookkeeping SHALL run only when the selected validation workflow runs Agent Validator, which is when either:
- `auto` is selected and `.validator/config.yml` exists; or
- `agent-validator` is selected.

The bookkeeping covers:
- the check for unvalidated changes when a change is created;
- the baseline advances when a definition is committed and when a change is archived.

Otherwise the bookkeeping SHALL NOT run, and creating a change SHALL NOT require the `agent-validator` executable.

#### Scenario: none selected in a configured project
- **WHEN** a change is created with `validation_workflow=none` in a project that has `.validator/config.yml` and unvalidated changes
- **THEN** creation proceeds without invoking `agent-validator`
- **AND** archival does not advance the validator baseline

#### Scenario: Validator not installed
- **WHEN** a change is created with `auto` in a project without `.validator/config.yml`, and `agent-validator` is not installed
- **THEN** creation succeeds

#### Scenario: Configured project keeps the clean-start check
- **WHEN** a change is created with `auto` in a configured project that has unvalidated changes
- **THEN** creation fails, asking for those changes to be validated first

### Requirement: Optional validator setup

The onboarding validator phase and the `scaffold` workflows SHALL ask the user whether to set up Agent Validator.
- **When the user accepts,** setup SHALL proceed as before.
- **When the user declines,** setup SHALL be skipped, and Agent Runner SHALL tell the user to make sure the project's `AGENTS.md` explains how to verify the project: linters, test suite, and any project-specific checks.
- **When onboarding is declined,** the remaining validator demonstration steps SHALL also be skipped.
- **When scaffold is declined,** it SHALL complete without validator configuration, and its validation point SHALL pass under `auto`.

#### Scenario: Declining in scaffold
- **WHEN** the user declines validator setup during scaffold
- **THEN** no `.validator/config.yml` is created
- **AND** the user sees the `AGENTS.md` verification guidance
- **AND** scaffold completes, with its validation point passing under `auto`

#### Scenario: Declining in onboarding
- **WHEN** the user declines validator setup in the onboarding validator phase
- **THEN** the phase shows the `AGENTS.md` verification guidance and skips the validator demonstration
- **AND** onboarding continues to its next phase

#### Scenario: Accepting in onboarding
- **WHEN** the user accepts validator setup
- **THEN** the phase runs validator initialization, setup, and the demonstration as before
