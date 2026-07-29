## ADDED Requirements

### Requirement: Latest logical workflow rows

The New tab SHALL select one candidate per canonical logical workflow name before applying its existing grouping, hidden-workflow, and search behavior. A valid logical group SHALL be represented by its numerically highest major/minor version. The row label and searchable identity SHALL use the version-free canonical logical name and MUST NOT display or index the physical filename, version suffix, or source path.

Opening a logical workflow's definition or starting its row SHALL use the selected latest version. Older versions MUST NOT appear as separate rows or become substitutes under search or the existing show-hidden toggle. Visibility, description, parameters, and other row behavior SHALL come from the selected latest version.

An invalid logical group SHALL appear as one non-launchable row labeled with its version-free logical name and containing its actionable validation error. A valid versioned sibling MUST NOT make that invalid group launchable. Invalid rows SHALL remain searchable by their version-free logical name.

#### Scenario: Multiple versions render one logical row
- **WHEN** discovery finds `deploy-v1.0.yaml` and `deploy-v2.0.yaml` in the winning source
- **THEN** the New tab renders exactly one row labeled `deploy`
- **AND** the row does not display either physical filename or a version suffix

#### Scenario: Definition view opens latest version
- **WHEN** `deploy-v2.0.yaml` is the selected latest version and the user opens the `deploy` row
- **THEN** the definition view loads `deploy-v2.0.yaml`

#### Scenario: Start action launches latest version
- **WHEN** `deploy-v2.0.yaml` is the selected latest version and the user starts the `deploy` row
- **THEN** Agent Runner starts a run from `deploy-v2.0.yaml`

#### Scenario: Version query does not match physical filename
- **WHEN** the New tab contains logical row `deploy` backed by `deploy-v2.0.yaml` and the user searches for `v2.0`
- **THEN** the `deploy` row is not included solely because its physical filename contains that version

#### Scenario: Logical-name query matches latest row
- **WHEN** the New tab contains logical row `team/deploy` backed by `team/deploy-v2.0.yaml` and the user searches for `deploy`
- **THEN** the latest logical row remains in the filtered results

#### Scenario: Older versions never render separately
- **WHEN** a logical workflow has multiple versions and the user searches or enables show-hidden
- **THEN** no older version appears as a separate row

#### Scenario: Latest hidden metadata controls visibility
- **WHEN** `deploy-v1.0.yaml` has `hidden: false` and selected latest `deploy-v2.0.yaml` has `hidden: true`
- **THEN** the `deploy` row is hidden by default and enabling show-hidden reveals the row backed by `deploy-v2.0.yaml`
- **AND** `deploy-v1.0.yaml` never substitutes for the hidden latest version

#### Scenario: Invalid group renders diagnostic row
- **WHEN** discovery finds invalid unversioned `deploy.yaml` and no other definition in the group
- **THEN** the New tab renders one non-launchable `deploy` row containing the actionable versioned-filename error

#### Scenario: Valid sibling does not make invalid group launchable
- **WHEN** discovery finds invalid `deploy.yaml` and valid `deploy-v2.0.yaml`
- **THEN** the New tab renders one non-launchable `deploy` error row rather than a launchable latest-version row

#### Scenario: Invalid row searchable by logical name
- **WHEN** the New tab contains an invalid `deploy` error row and the user searches for `deploy`
- **THEN** the error row remains in the filtered results so the user can read its migration guidance

#### Scenario: Invalid row actions are disabled
- **WHEN** the cursor is on an invalid logical workflow row and the user attempts to open or start it
- **THEN** no workflow definition view or run is launched
