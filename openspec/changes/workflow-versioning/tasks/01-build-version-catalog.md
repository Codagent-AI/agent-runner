# Task: Build the Workflow Version Catalog

## Goal

Create the source-neutral catalog that defines workflow filename identity, validation, grouping, duplicate detection, and arbitrary-size major/minor ordering. This package is the shared contract used by local discovery, builtin discovery, CLI resolution, exact-file validation, and saved-run display.

## Background

The current code derives names independently in `internal/discovery/discovery.go`, `cmd/agent-runner/main.go`, `workflows/embed.go`, and `internal/runview/`. Introduce a low-level package such as `internal/workflowcatalog/` that does not import `internal/loader`, `internal/discovery`, `internal/model`, or `workflows`; those higher layers must be able to consume it without creating dependency cycles.

The catalog owns:

- parsing `<logical-name>-v<major>.<minor>.yaml` and `.yml`;
- preserving validated version components as decimal strings and comparing them by digit count and then lexically, major before minor, without a machine-integer limit;
- deriving the version-free basename, canonical name (including nested directory segments), and display label;
- classifying malformed filenames into deterministic best-effort groups;
- case-normalized invalid-group association without accepting uppercase names;
- grouping candidates within one source;
- rejecting duplicate numeric versions across `.yaml` and `.yml`;
- invalidating a logical group when any associated candidate is invalid; and
- selecting the numerically highest valid major/minor version when the group is otherwise valid.

For malformed-group association, an unversioned filename uses its full stem. A terminal malformed version attempt beginning with `-v` and a digit uses the preceding stem, while earlier non-terminal `-v` text remains part of the logical name. Underscore-prefixed YAML basenames are filtered by source adapters and must also be easy for callers to recognize as exempt.

Errors returned by the catalog must retain enough structured information for higher layers to produce stable, actionable messages naming all conflicting files, the required pattern, and a concrete rename example. Do not parse workflow YAML or apply project/user/builtin precedence in this package; catalogs are built per source and precedence is a caller responsibility.

Use table-driven tests next to the package. Prefer `google/go-cmp` for structured results.

## Spec

### Requirement: Versioned workflow filename validation

Every YAML file treated as a workflow definition MUST have a basename matching `<logical-name>-v<major>.<minor>.yaml` or `<logical-name>-v<major>.<minor>.yml`. The logical-name component MUST contain one or more lowercase ASCII letters, digits, hyphens, or underscores and MUST NOT contain uppercase letters. The major and minor components MUST each be a non-negative decimal integer without leading zeroes unless the component is exactly `0`. The filename MUST contain exactly two version components; patch, prerelease, and build-metadata components are invalid.

YAML files whose basenames start with an underscore (`_`) SHALL be ignored as workflow definitions in project, user, and built-in workflow sources and SHALL remain exempt from the workflow filename convention. A built-in namespace's `_group.yaml` SHALL retain its existing metadata meaning.

When an operation discovers or targets an invalid workflow definition, Agent Runner SHALL mark that logical workflow group invalid and report an error containing the offending filename, the required filename pattern, and a concrete rename example. A valid versioned sibling MUST NOT suppress the invalid-file error for the same logical workflow group. Invalid files in one logical workflow group MUST NOT prevent unrelated valid groups from being discovered, validated, or run.

For invalid-group association, an unversioned filename SHALL use its entire stem as its logical group. A malformed terminal version attempt beginning with `-v` followed by a decimal digit SHALL use the preceding stem, so malformed `deploy-v1.yaml` and `deploy-v1.x.yaml` belong to logical group `deploy`. Case SHALL be normalized for group association only, so invalid `Deploy-v1.0.yaml` belongs to and invalidates lowercase group `deploy`; uppercase filenames remain invalid.

#### Scenario: Versioned YAML extensions accepted
- **WHEN** workflow definitions are named `deploy-v1.0.yaml` and `verify-v2.3.yml`
- **THEN** both filenames pass versioned workflow filename validation

#### Scenario: Zero version accepted
- **WHEN** a workflow definition is named `prototype-v0.0.yaml`
- **THEN** the filename passes versioned workflow filename validation

#### Scenario: Uppercase logical name rejected
- **WHEN** a workflow definition is named `Deploy-v1.0.yaml`
- **THEN** Agent Runner rejects it with an error that names the file and requires a lowercase logical name

#### Scenario: Invalid version forms rejected
- **WHEN** workflow definitions are named `deploy-v1.yaml`, `deploy-v1.2.3.yaml`, `deploy-v01.2.yaml`, or `deploy-v1.x.yaml`
- **THEN** Agent Runner rejects each definition with an error that states the required `<logical-name>-v<major>.<minor>.yaml` or `.yml` pattern

#### Scenario: Malformed version attempt invalidates intended group
- **WHEN** one workflow source contains malformed `deploy-v1.yaml` and valid `deploy-v2.0.yaml`
- **THEN** Agent Runner treats both files as members of logical group `deploy`, reports the malformed filename error, and does not expose `deploy` as launchable

#### Scenario: Uppercase filename invalidates lowercase group
- **WHEN** one workflow source contains invalid `Deploy-v1.0.yaml` and valid `deploy-v2.0.yaml`
- **THEN** Agent Runner associates the invalid file case-insensitively with logical group `deploy`, reports the lowercase-name error, and does not expose `deploy` as launchable

#### Scenario: Unversioned workflow rejected with migration guidance
- **WHEN** a workflow definition is named `deploy.yaml`
- **THEN** Agent Runner rejects it with an error that names `deploy.yaml` and suggests a versioned filename such as `deploy-v1.0.yaml`

#### Scenario: Valid sibling does not hide unversioned error
- **WHEN** one workflow source contains both `deploy.yaml` and `deploy-v2.0.yaml`
- **THEN** the logical workflow `deploy` is invalid and Agent Runner reports the actionable error for `deploy.yaml`

#### Scenario: Invalid group does not disable unrelated workflow
- **WHEN** one workflow source contains invalid `deploy.yaml` and valid `verify-v1.0.yaml`
- **THEN** Agent Runner reports `deploy` as invalid while `verify` remains discoverable, validatable, and runnable

#### Scenario: Builtin metadata filename exempt
- **WHEN** a built-in namespace contains `_group.yaml`
- **THEN** Agent Runner treats it as namespace metadata without applying workflow filename validation

#### Scenario: Underscore-prefixed project file ignored
- **WHEN** a project workflow directory contains `_helpers.yaml`
- **THEN** Agent Runner ignores the file as a workflow definition and does not create an invalid `_helpers` group

#### Scenario: Underscore-prefixed user file ignored
- **WHEN** a user workflow directory contains `_notes.yml`
- **THEN** Agent Runner ignores the file as a workflow definition and does not create an invalid `_notes` group

### Requirement: Logical identity and latest version selection

Agent Runner SHALL derive a workflow's logical basename and version from the final `-v<major>.<minor>` suffix of its filename. Earlier `-v` text in the logical-name component SHALL remain part of the logical name. Directory segments SHALL remain part of the canonical runnable name but SHALL NOT become part of the YAML `name:` value.

Within one workflow source, Agent Runner SHALL group definitions by canonical logical name and select the version with the numerically greatest major component, breaking equal-major ties with the numerically greatest minor component. Two definitions with the same canonical logical name and numeric version MUST be rejected as duplicates regardless of whether they use `.yaml` or `.yml`.

#### Scenario: Nested workflow identity
- **WHEN** a workflow definition is stored as `team/deploy-v2.0.yaml`
- **THEN** its canonical runnable name is `team/deploy`, its logical basename is `deploy`, and its version is `2.0`

#### Scenario: Final version suffix determines identity
- **WHEN** a workflow definition is named `save-v-data-v1.2.yaml`
- **THEN** its logical basename is `save-v-data` and its version is `1.2`

#### Scenario: Minor versions compared numerically
- **WHEN** one logical workflow has versions `v2.9` and `v2.10`
- **THEN** Agent Runner selects `v2.10` for a new run

#### Scenario: Major version takes precedence
- **WHEN** one logical workflow has versions `v1.99` and `v2.0`
- **THEN** Agent Runner selects `v2.0` for a new run

#### Scenario: Duplicate version across extensions rejected
- **WHEN** one workflow source contains `deploy-v1.0.yaml` and `deploy-v1.0.yml`
- **THEN** Agent Runner rejects the logical workflow as a duplicate version and names both files in the error

## Done When

- The shared catalog package has table-driven tests for every scenario above, including version components larger than `uint64`.
- Tests prove grouping is source-neutral, deterministic, independent of candidate enumeration order, and does not let one invalid group disable unrelated groups.
- The package exposes stable facts and errors that CLI, discovery, loader, builtin, and run-view code can consume without importing each other.
- `go test ./internal/workflowcatalog` (or the chosen package path) passes and `make fmt` leaves the package clean.
