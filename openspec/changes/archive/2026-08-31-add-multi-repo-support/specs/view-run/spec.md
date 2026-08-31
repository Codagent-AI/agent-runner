## ADDED Requirements

### Requirement: Repository execution containers
The run view MUST represent each explicit repository execution as one container level in the workflow tree. The container label MUST be the configured repository name without a `repo` prefix or repository path. Workspace-scoped work MUST remain outside repository containers.

#### Scenario: Repository containers follow workspace work
- **WHEN** workspace planning is followed by repository execution for backend and frontend
- **THEN** the tree shows the planning steps at workspace level and separate `backend` and `frontend` containers at the repository-scoped position

#### Scenario: Repository label uses configured name
- **WHEN** a repository is configured with name `customer-facing-web-application`
- **THEN** its tree row uses that name and applies the existing sidebar truncation behavior rather than adding its path or a `repo` prefix

#### Scenario: Repository body remains nested
- **WHEN** backend contains implementation, validation, and pull-request steps
- **THEN** those steps appear beneath the backend container and do not appear as peers of frontend's steps

#### Scenario: Implicit single repository remains flattened
- **WHEN** a scope-aware run uses the implicit `default` repository in a traditional single-repository project
- **THEN** the run view omits the repository container and preserves the existing visible workflow shape

### Requirement: Repository container navigation and detail
A repository container MUST support the existing selection, inline expansion, drill-in, breadcrumb, log scoping, status, and aggregate-detail behaviors used by other execution containers. Its status MUST summarize only execution belonging to that repository.

#### Scenario: Selected repository expands
- **WHEN** the user selects backend in the top-level tree
- **THEN** its direct child execution rows expand inline using the existing container behavior

#### Scenario: Enter drills into repository
- **WHEN** the user presses Enter on backend
- **THEN** the sidebar and detail pane are scoped to backend and the breadcrumb appends `backend`

#### Scenario: Escape leaves repository
- **WHEN** the user presses Escape while drilled into backend
- **THEN** the view returns to the containing workflow level

#### Scenario: Repository detail is aggregate
- **WHEN** backend is selected
- **THEN** its detail summarizes backend's outcome, duration, metrics, and pull-request result without including frontend execution

#### Scenario: Failed repository is identifiable
- **WHEN** a nested backend step fails and frontend remains pending
- **THEN** backend shows a failed container status while frontend shows pending

#### Scenario: Saved run reconstructs repositories
- **WHEN** the user inspects a saved multi-repository run
- **THEN** the repository containers and their nested execution are reconstructed from persisted state and audit evidence

### Requirement: Repository pull requests in run detail
When a repository has a recorded pull-request URL, its container detail MUST display that repository's current pull-request link. The run breadcrumb MUST continue to display the complete comma-separated list defined by the pull-request-link capability.

#### Scenario: Repository detail shows its own pull request
- **WHEN** backend and frontend have different recorded pull-request URLs and backend is selected
- **THEN** backend detail links only backend's current pull request

#### Scenario: Repository without a pull request
- **WHEN** backend has no recorded pull-request URL
- **THEN** backend detail does not display a pull-request link for another repository
