## ADDED Requirements

### Requirement: Repository identity in metrics records

Every `run-metrics.json` step, iteration, and agent-call record produced while an explicit repository is active MUST include `repository_name` and `repository_dir` fields in addition to its nesting prefix. Workspace records and transparent implicit-repository records MUST omit those fields. Run-level aggregation MUST continue to include workspace and repository records exactly once without requiring external consumers to parse repository identity from the prefix.

#### Scenario: Explicit repository metric record
- **WHEN** an implementation step records metrics while backend is active
- **THEN** its artifact record identifies backend and backend's canonical repository root

#### Scenario: Workspace metric record
- **WHEN** a planning step records metrics in workspace scope
- **THEN** its artifact record omits repository identity fields

#### Scenario: Implicit repository metric compatibility
- **WHEN** a traditional project executes through the transparent implicit repository
- **THEN** its metric record retains the legacy shape without explicit repository identity

#### Scenario: External consumer groups repository metrics
- **WHEN** an external consumer reads records from a multi-repository run
- **THEN** it can group explicit repository records by `repository_name` without decoding nesting prefixes
