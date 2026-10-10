## ADDED Requirements

### Requirement: OpenSpec engine on built-in openspec workflows

The built-in `openspec:change`, `openspec:plan-change`, and `openspec:implement-change` workflows (v2.0) SHALL run under the OpenSpec engine, bound to their `change_name` param. Through engine inheritance, the `proposal`, `specs`, and `design` steps of the shared core define sub-workflow and the `tasks` step of the shared core plan sub-workflow SHALL be enriched and validated by the engine in those runs. The built-in `spec-driven:*` workflows SHALL NOT run under any engine, so the same shared core steps run without OpenSpec enrichment or validation there.

The built-in `openspec:simple-change` workflows SHALL map their single `plan` step to the planning artifacts:
- v1.0 requires `proposal` and `tasks`, with `specs` and `design` optional;
- v2.0 requires `proposal`, `specs`, and `tasks`, with `design` optional.

`openspec:plan-change` v1.0 SHALL keep its per-artifact steps under the engine.

#### Scenario: openspec:change enriches definition steps
- **WHEN** a user runs `openspec:change` in a project whose `openspec/config.yaml` defines `context` and `rules.proposal`
- **THEN** the agent for the `proposal` step receives that context and those rules as engine enrichment

#### Scenario: openspec:change validates the tasks artifact
- **WHEN** the `tasks` step of an `openspec:change` run completes but `openspec status` does not report `tasks` as `done`
- **THEN** the `tasks` step fails with an error naming the `tasks` artifact

#### Scenario: openspec:change lifecycle steps are unaffected
- **WHEN** an `openspec:change` run executes its implement, accept, archive, and finalize sub-workflows
- **THEN** no step in them is enriched or validated by the engine, and none fails for an engine reason, including steps that run after the change is archived and child workflows without a `change_name` param

#### Scenario: spec-driven:change runs without the engine
- **WHEN** a user runs `spec-driven:change`
- **THEN** its `proposal`, `specs`, `design`, and `tasks` steps receive no OpenSpec enrichment and no OpenSpec validation

#### Scenario: simple-change plan without a design
- **WHEN** an `openspec:simple-change` v2.0 run's `plan` step writes `proposal.md`, specs, and `tasks.md` but no `design.md`
- **THEN** the `plan` step passes engine validation

#### Scenario: simple-change v1.0 plan without specs
- **WHEN** an `openspec:simple-change` v1.0 run's `plan` step writes `proposal.md` and `tasks.md` only
- **THEN** the `plan` step passes engine validation

#### Scenario: simple-change plan missing tasks
- **WHEN** an `openspec:simple-change` run's `plan` step finishes without `tasks.md`
- **THEN** the `plan` step fails with an error naming the `tasks` artifact

#### Scenario: simple-change plan receives planning context
- **WHEN** an `openspec:simple-change` run's `plan` step starts in a project with OpenSpec `context` and per-artifact rules
- **THEN** the agent receives the project context once, plus each planning artifact's output path and rules, with optional artifacts labeled optional
