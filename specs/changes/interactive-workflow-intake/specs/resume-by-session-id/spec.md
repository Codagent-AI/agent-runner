## ADDED Requirements

### Requirement: Intake provenance survives resume

Resuming a run SHALL restore the intake provenance the run was created with. A run launched from intake SHALL resume with its intake parent run ID and its sealed handoff path intact. A run invoked directly SHALL resume with no intake provenance. Resume SHALL NOT infer, re-derive, or discard provenance based on how the resume itself was invoked.

#### Scenario: Resumed intake-launched run restores its handoff
- **WHEN** a run launched from intake is interrupted and resumed with `--resume <id>`
- **THEN** the resumed run's steps see the same sealed handoff contents they saw before the interruption

#### Scenario: Resumed intake-launched run restores its parent
- **WHEN** a run launched from intake is resumed
- **THEN** its recorded intake parent run ID is unchanged

#### Scenario: Resumed direct run has no intake provenance
- **WHEN** a directly invoked run is resumed
- **THEN** it carries no intake parent and its handoff value remains empty

#### Scenario: Resumed intake run restores its staged route
- **WHEN** an intake run that staged a route without completing its step is resumed
- **THEN** the staged route is still present and can be replaced by the resumed attempt
