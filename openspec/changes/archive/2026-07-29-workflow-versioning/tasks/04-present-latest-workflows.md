# Task: Present Latest Logical Workflows

## Goal

Make discovery and the New tab operate on logical workflow groups instead of physical files. Each source contributes one version-neutral row per group, backed by the latest valid version or by one actionable non-launchable group error.

## Background

`internal/discovery/discovery.go` currently deduplicates `.yaml`/`.yml` by extension and derives `CanonicalName` by stripping the extension. Replace that candidate map with the shared catalog:

- enumerate all YAML definitions in each project, user, or builtin source;
- skip underscore-prefixed metadata before catalog classification;
- build project, user, and builtin catalogs independently;
- validate every candidate in a group through the source-aware loader, not only the selected latest file;
- emit the selected latest candidate's exact `SourcePath`, description, hidden flag, and params for a valid group;
- emit one version-free `WorkflowEntry` with `ParseError` for an invalid group;
- apply project-over-user shadowing by logical canonical name before rendering; and
- keep builtin namespaces distinct from bare names.

The catalog selection happens before hidden filtering. A hidden latest version stays hidden until show-hidden is enabled; an older visible version cannot substitute. Preserve current scope/namespace ordering and `_group.yaml` metadata.

`internal/listview/newtab.go` must search version-neutral identity and existing version-neutral metadata without indexing `SourcePath`. Older physical candidates must not remain in the row model, so search and show-hidden cannot reveal them. Invalid rows remain searchable by logical name and must disable both definition and start actions.

Definition and start actions already carry `discovery.WorkflowEntry`, but they consume it differently. Verify that the exact selected latest `SourcePath` reaches `runview.NewForDefinition`. The start action MUST continue re-execing with only the version-free `CanonicalName`; the task-03 CLI resolver then selects the latest version again. Do not place `SourcePath` in exec-self launch arguments or create a path-based execution channel. `internal/runview/` definition preview must not add a version label.

Use injected/local filesystems in `internal/discovery/discovery_test.go`, row/filter tests in `internal/listview/`, and start/definition message tests in `cmd/agent-runner/`.

### Intermediate handoff from tasks 02 and 03

This task requires task 03's final logical CLI resolution and task 02's temporary discovery/listview assertions. Replace every intermediate version-bearing canonical-name expectation in discovery, listview, definition-message, and start-message tests with the final version-free logical name. Remove temporary comments added by task 02 once their assertions reach the final contract.

Keep the two action paths explicit in tests:

- opening a definition proves the selected latest entry's exact `SourcePath` is passed to the definition view;
- starting a run proves the exec-self message contains the version-free `CanonicalName`, and command-level resolution from task 03 independently proves that name reaches the latest exact path.

The actions must agree because they consume the same selected catalog entry and resolver rules, not because the New tab bypasses logical resolution. This task also completes the discovery-facing clauses and scenarios of “Builtin workflow set embedded at build time” that task 02 deferred.

## Spec

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

## Done When

- Discovery emits one stable version-free entry per logical group and validates every candidate, including older siblings.
- Project groups shadow user groups before version comparison; unrelated invalid groups do not suppress valid rows.
- Latest-version metadata drives hidden state, description, params, definition view, and start-run behavior.
- Definition-view tests assert exact latest `SourcePath`; start-run tests assert version-free `CanonicalName` and never pass a physical path to execution.
- New-tab search no longer indexes `SourcePath`; searches and show-hidden cannot reveal older physical versions.
- Invalid groups render one searchable diagnostic row with disabled actions.
- Task 02's temporary version-bearing canonical-name assertions are replaced with final version-free expectations.
- Focused discovery, list-view, definition-view, and start-run tests pass.
- `make fmt` and `make test` pass before task 05 begins.
