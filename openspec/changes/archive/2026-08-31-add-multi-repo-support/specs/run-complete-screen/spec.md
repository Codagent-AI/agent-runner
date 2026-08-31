## ADDED Requirements

### Requirement: Repository rows in completion summaries
At a summary level containing explicit repository executions, the completion summary MUST show one row per repository in persisted affected-repository order. Each repository row MUST roll up only that repository's nested outcome, duration, token usage, and cost.

#### Scenario: Two repositories appear in order
- **WHEN** a completed run selected backend then frontend
- **THEN** the summary shows a backend row followed by a frontend row at the repository-scoped position

#### Scenario: Repository row rolls up nested work
- **WHEN** backend contains implementation, validation, and pull-request steps
- **THEN** backend's row aggregates the metrics and outcome of those nested executions

#### Scenario: Repository metrics remain isolated
- **WHEN** backend and frontend both report usage and cost
- **THEN** neither repository row includes metrics belonging to the other repository

#### Scenario: Failed repository row
- **WHEN** backend fails before frontend starts
- **THEN** backend shows a failed outcome and frontend shows a pending outcome

#### Scenario: Implicit single repository stays flattened
- **WHEN** a scope-aware run uses the implicit single repository
- **THEN** the completion summary preserves the existing visible hierarchy without a repository row

### Requirement: Repository summary drill-down
A repository summary row MUST be drillable using the existing summary navigation. Drilling into a repository MUST show its immediate child execution rows and MUST scope displayed totals to that repository.

#### Scenario: Drill into repository
- **WHEN** the user enters the backend summary row
- **THEN** the summary shows backend's immediate child rows and a backend-scoped totals line

#### Scenario: Leave repository summary
- **WHEN** the user leaves the backend summary level
- **THEN** the summary returns to the containing level with all repository rows in affected order

#### Scenario: Repository breadcrumb
- **WHEN** the user drills into backend's completion summary
- **THEN** the breadcrumb appends `backend` while retaining the run's comma-separated pull-request segment

### Requirement: Run totals include workspace and repositories once
The run totals line MUST aggregate workspace-scoped execution and every repository execution without double-counting child metrics already included in a repository row. Existing unavailable-cost and partial-pricing behavior MUST apply independently to repository rows and the run total.

#### Scenario: Workspace and repository metrics combine
- **WHEN** planning reports workspace metrics and backend and frontend report repository metrics
- **THEN** the run total includes all three execution scopes exactly once

#### Scenario: Repository with unavailable cost
- **WHEN** one backend execution has usage but unavailable cost
- **THEN** backend's row and the run total use the existing unavailable or partial-cost presentation

#### Scenario: Called-agent metrics remain nested once
- **WHEN** a repository-scoped parent includes called-agent metrics in its existing rollup
- **THEN** the repository and run totals do not add the called-agent metrics a second time
