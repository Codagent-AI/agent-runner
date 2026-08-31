## ADDED Requirements

### Requirement: Live repository visibility
The live run view MUST show explicit repository executions using the same named repository containers as the saved run view. Repository containers MUST update their status and nested output as audit events arrive.

#### Scenario: Repository starts live
- **WHEN** backend repository execution begins while the run view is open
- **THEN** the backend container becomes active and its nested execution becomes visible

#### Scenario: Repository completes live
- **WHEN** backend completes successfully
- **THEN** its container immediately shows success without waiting for the run to finish

#### Scenario: Next repository starts
- **WHEN** frontend begins after backend completes
- **THEN** backend remains visibly complete and frontend becomes active

#### Scenario: Implicit single repository stays flattened
- **WHEN** a live run uses the implicit single repository
- **THEN** the new repository level is not rendered

### Requirement: Auto-follow tracks active repository execution
While auto-follow is engaged, the cursor and visible tree MUST follow the deepest active execution through its repository container. Moving between repositories MUST use the same automatic ancestry and focus behavior as moving between existing nested containers.

#### Scenario: Active repository is followed
- **WHEN** a nested implementation step begins in backend
- **THEN** auto-follow exposes backend's active ancestry and selects the deepest active step

#### Scenario: Auto-follow advances between repositories
- **WHEN** backend finishes and frontend starts
- **THEN** auto-follow leaves backend and follows frontend's active nested step

#### Scenario: Manual navigation pauses repository following
- **WHEN** the user manually selects another row or drills to another scope while repository execution continues
- **THEN** new repository activity does not steal focus

#### Scenario: Jump to live restores repository following
- **WHEN** the user invokes the existing jump-to-live action after pausing on another repository
- **THEN** the view returns to the active repository's deepest active execution and re-engages auto-follow

#### Scenario: Failure selects nested repository step
- **WHEN** a nested backend step fails
- **THEN** the view focuses that failed step within backend rather than selecting only the repository container

### Requirement: Live repository output isolation
Live detail output selected within a repository MUST include only the selected execution subtree. Switching repository selection MUST replace the detail with that repository's accumulated output without losing persisted output from completed repositories.

#### Scenario: Active backend output streams
- **WHEN** a selected backend step emits output
- **THEN** its output streams in the detail pane without frontend output being mixed into the block

#### Scenario: Completed repository output remains available
- **WHEN** backend completes and frontend becomes active
- **THEN** the user can manually return to backend and inspect its accumulated output
