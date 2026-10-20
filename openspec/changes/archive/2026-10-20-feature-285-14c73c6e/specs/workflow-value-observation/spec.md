## ADDED Requirements

### Requirement: Observation cost carries its basis

A value observation's cost field group SHALL record, next to its scalar USD cost, the basis of that cost: `reported` when every contributing leaf cost was CLI-reported or producer-reported, `estimated` when every one was a Runner estimate under `cost-estimation`, and `mixed` when both contributed. When the scalar cost is unknown, the basis SHALL be absent. The basis SHALL be derived from the source `run-metrics.json` records' cost bases, and the leaf's detailed local evidence SHALL keep each contributing record's basis.

#### Scenario: Leaf with an estimated Codex cost
- **WHEN** a leaf step's only contributing record is a Codex attempt with an estimated cost
- **THEN** the observation's cost is that estimate, with basis `estimated`

#### Scenario: Leaf mixing reported and estimated costs
- **WHEN** a leaf's contributing records include a CLI-reported cost and a Runner estimate
- **THEN** the observation's cost is their sum, with basis `mixed`

#### Scenario: Leaf with unknown cost
- **WHEN** one contributing record of a leaf has a null cost
- **THEN** the observation's scalar cost stays unknown and it carries no basis
