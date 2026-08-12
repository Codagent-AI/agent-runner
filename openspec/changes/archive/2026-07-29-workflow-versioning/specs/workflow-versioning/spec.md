## ADDED Requirements

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

### Requirement: YAML name matches logical basename

Every workflow definition's required YAML `name:` value MUST exactly equal its lowercase, version-free filename basename. Directory segments, the version suffix, and the file extension MUST NOT appear in `name:`. Agent Runner SHALL reject a mismatch with an error containing both the expected and actual values.

#### Scenario: Nested workflow name matches basename
- **WHEN** `team/deploy-v2.0.yaml` contains `name: deploy`
- **THEN** the workflow passes name-alignment validation

#### Scenario: Directory-qualified YAML name rejected
- **WHEN** `team/deploy-v2.0.yaml` contains `name: team/deploy`
- **THEN** Agent Runner rejects the workflow and reports that the expected name is `deploy`

#### Scenario: Version-bearing YAML name rejected
- **WHEN** `deploy-v2.0.yaml` contains `name: deploy-v2.0`
- **THEN** Agent Runner rejects the workflow and reports that the expected name is `deploy`

#### Scenario: Uppercase YAML name rejected
- **WHEN** `deploy-v2.0.yaml` contains `name: Deploy`
- **THEN** Agent Runner rejects the workflow and reports that the expected name is `deploy`

### Requirement: Explicit sub-workflow version references

Every resolved sub-workflow reference MUST identify a workflow file with a valid versioned filename. Agent Runner SHALL load the exact referenced version and MUST NOT substitute a newer version. Editing the contents of the referenced versioned file in place SHALL remain supported and subsequent execution SHALL load its current contents.

#### Scenario: Exact child version remains pinned
- **WHEN** a parent references `validator-v1.0.yaml` and `validator-v1.1.yaml` also exists
- **THEN** Agent Runner loads `validator-v1.0.yaml`

#### Scenario: Edited child version remains usable
- **WHEN** a parent references `validator-v1.0.yaml` and that file has been edited in place
- **THEN** subsequent execution loads the current contents of `validator-v1.0.yaml` without requiring another version

#### Scenario: Unversioned child reference rejected
- **WHEN** a parent workflow resolves a sub-workflow reference to `validator.yaml`
- **THEN** Agent Runner rejects the reference with the actionable versioned-filename error

### Requirement: Recorded version with mutable contents

Run state SHALL record the resolved versioned workflow file used to start the run. Resume SHALL load that recorded version and MUST NOT substitute a newer available version. If the recorded file's contents differ from the saved workflow hash, Agent Runner SHALL retain its existing changed-file warning and continue resume. If the recorded versioned file is missing, resume SHALL fail clearly.

State that records an unversioned on-disk workflow file SHALL fail with the actionable filename migration error and MUST NOT fall back to a versioned file. State that records an unversioned `builtin:` workflow reference SHALL instead report that the run predates workflow versioning and cannot be resumed by the current binary, with guidance to restart the workflow or finish the run using the older binary; it MUST NOT tell the user to rename an embedded file.

#### Scenario: Resume retains recorded version
- **WHEN** a run was started with `deploy-v1.0.yaml` and `deploy-v2.0.yaml` is published before resume
- **THEN** resume loads `deploy-v1.0.yaml`

#### Scenario: Resume continues after in-place edit
- **WHEN** the recorded `deploy-v1.0.yaml` content differs from the run's saved workflow hash
- **THEN** Agent Runner warns that the file changed and continues resuming from `deploy-v1.0.yaml`

#### Scenario: Missing recorded version fails
- **WHEN** run state records `deploy-v1.0.yaml` and that file no longer exists
- **THEN** resume fails with an error naming the missing recorded version and does not load another version

#### Scenario: Legacy unversioned state fails
- **WHEN** run state records `deploy.yaml`
- **THEN** resume fails with migration guidance for a versioned filename and does not fall back to `deploy-v1.0.yaml`

#### Scenario: Legacy unversioned builtin state explains binary incompatibility
- **WHEN** run state records `builtin:onboarding/onboarding.yaml`
- **THEN** resume fails with guidance to restart using the current binary or finish using the older binary
- **AND** the error does not instruct the user to rename the embedded workflow file
