## ADDED Requirements

### Requirement: Pre-validation honors the launch profile override

When an invocation supplies a launch-time profile-set override, the layered-config load performed by
pre-validation SHALL apply that override, and session-aware effective agent resolution SHALL draw every
`(cli, model, effort)` triple from the overridden profile set's agents. This SHALL apply to both
pre-validation contexts that run today: fresh runs and explicit `--validate` invocations.

#### Scenario: Fresh run pre-validates against the overridden profile set

- **WHEN** `agent-runner --profile copilot my-workflow` is invoked, the `copilot` profile set resolves its
  implementor agent to a different CLI and model than the `default` set, and pre-validation runs
- **THEN** the probed `(cli, model, effort)` triples are those of the `copilot` set's agents, and none of
  the `default` set's agents are probed

#### Scenario: Explicit validate against the overridden profile set

- **WHEN** `agent-runner --validate --profile copilot my-workflow` is invoked
- **THEN** validation resolves and probes agents from the `copilot` profile set

#### Scenario: Agent missing from the overridden profile set fails pre-validation

- **WHEN** a workflow references `agent: auditor`, the overridden profile set does not define `auditor`,
  and the profile set that `active_profile` selects does
- **THEN** pre-validation fails with an error indicating that `auditor` is not defined in the active
  profile

#### Scenario: Validate reports the profile set it validated against

- **WHEN** `agent-runner --validate --profile copilot my-workflow` succeeds
- **THEN** the validation output names `copilot` as the profile set the workflow was validated against

### Requirement: Pre-validation errors report the overridden profile set

When pre-validation reports a failure whose structured profile-set context identifies the profile set used
for agent resolution or model probing, and an override is in effect, that context SHALL be the overridden
set's name rather than the name that `active_profile` or the `default` fallback would have produced.

This SHALL NOT change which profile set is named by a config-validation failure. A validation error caused by
an invalid agent definition SHALL continue to identify the profile set that contains the invalid definition,
even when that set is not the selected one (see the `config-profiles` capability).

#### Scenario: Resolution failure names the overridden set

- **WHEN** an override selects `copilot`, the project config sets `active_profile: work`, and a workflow step
  references an agent that the `copilot` set does not define, so resolution fails during pre-validation
- **THEN** the error's profile set context is `copilot`

#### Scenario: Validation failure names the offending set, not the overridden set

- **WHEN** an override selects `copilot` and the non-selected `work` profile set contains an agent with an
  invalid field value
- **THEN** the reported profile set is `work`, the set containing the invalid definition, not `copilot`

### Requirement: Override does not extend pre-validation to resume

Supplying a launch-time profile-set override SHALL NOT cause pre-validation to run on `--resume`
invocations. Resume continues to load only the top-level workflow file, whether or not `--profile` is
passed.

#### Scenario: Resume with an override skips pre-validation

- **WHEN** `agent-runner --resume --profile copilot` is invoked
- **THEN** pre-validation does not run, and resume loads only the top-level workflow file as it does today
