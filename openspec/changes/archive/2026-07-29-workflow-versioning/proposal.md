## Why

Workflow rewrites currently require publishing a second workflow under a different user-facing name, such as `change2`, which exposes implementation history and leaves no durable version identity for saved runs. Filename-based major/minor versioning will let authors maintain coexisting generations of a logical workflow while keeping new-run selection simple and historical runs associated with the version they used.

## What Changes

- Introduce a required `<logical-name>-v<major>.<minor>.yaml` filename convention for workflow definitions, with numerically ordered major/minor components and no patch component.
- Group coexisting versioned files under one logical workflow name and select the highest version numerically for new runs.
- Keep version suffixes out of new-run rows, search behavior, and logical CLI names while retaining the selected version in the resolved workflow file.
- Reject direct execution of version-bearing names or versioned file paths so new runs always select the latest version through a version-free logical name.
- Require each workflow's YAML `name:` to match its version-free filename basename and report a clear validation error for mismatches.
- Show the recorded workflow version in the non-live run view and resume incomplete runs against that version while continuing to permit in-place workflow edits.
- Reject attempts to launch an unversioned workflow with an actionable error that identifies the file, states the required convention, and gives a concrete rename example.
- Preserve underscore-prefixed YAML files as ignored, non-workflow files in project, user, and built-in workflow sources.
- **BREAKING**: Rename built-in workflow files to the versioned convention and update explicit sub-workflow references; unversioned workflow definitions are no longer valid for new runs or resume.

## Capabilities

### New Capabilities

- `workflow-versioning`: Define workflow filename identity, major/minor validation and ordering, YAML name alignment, latest-version selection, historical-version retention, and version-aware resume behavior.

### Modified Capabilities

- `workflow-name-resolution`: Resolve a version-free logical workflow name to the highest available major/minor version within the existing source-precedence rules, reject direct historical launches, and report actionable errors for unversioned files.
- `builtin-workflows`: Discover and embed multiple versions of a logical built-in workflow while exposing only the latest version for new runs, and distinguish YAML workflow definitions from bundled assets across both supported YAML extensions.
- `new-tab-layout`: Render and search one version-neutral row per logical workflow, backed by its latest available version.
- `view-run`: Display the recorded workflow version when inspecting a non-live run.
- `resume-by-session-id`: Resume the recorded workflow version, report source-appropriate errors for state that names an unversioned workflow, and require logical names rather than file paths for fresh execution.
- `sub-workflows`: Require exact versioned filenames in sub-workflow references and pre-validation scenarios.
- `workflow-pre-validation`: Validate versioned workflow paths and compositions while retaining exact versioned file paths for explicit validation.
- `debug-inspection-cli`: Resolve version-free built-in logical names to latest while preserving exact versioned paths and embedded refs for read-only historical inspection.
- `audit-log-lifecycle`: Record exact versioned workflow paths in run and sub-workflow lifecycle events.
- `audit-log-entries`: Record the exact resolved versioned path in sub-workflow step metadata.
- `workflow-bundled-scripts`: Resolve bundled and on-disk scripts relative to their containing versioned workflow definitions.

## Technical Approach

Derive logical identity and version from each workflow's filename basename, group candidates by logical name within each project, user, or built-in source, and compare major/minor components numerically. Directories remain part of canonical names, so `team/deploy-v2.0.yaml` is launched as `team/deploy`, while its YAML contains `name: deploy`. Existing project-over-user precedence remains unchanged; version selection occurs within the winning source.

```text
change-v1.0.yaml ─┐
change-v2.0.yaml ─┴─ logical workflow "change" ── latest for new run
                                                   │
                                                   └─ state records change-v2.0.yaml
                                                        ├─ inspect shows change v2.0
                                                        └─ resume reloads change-v2.0.yaml
```

Historical definitions remain regular workflow files rather than being copied into a separate archive. Run state already records the resolved workflow file, so resume continues targeting that version as long as authors retain it. Versioned files remain editable during active development, and resume retains the current changed-file warning behavior rather than enforcing immutability or requiring a version bump.

Sub-workflow references use explicit versioned filenames and never auto-upgrade to another version. Authors may still edit either parent or child files in place; publishing new dependent versions is required only when authors deliberately want additional generations to coexist. Unversioned workflow files are invalid even when a valid versioned sibling exists; operations report source-appropriate errors rather than applying a compatibility fallback.

For legacy unfinished runs, migration guidance depends on the recorded source. On-disk paths receive concrete rename guidance. Unversioned `builtin:` paths cannot be renamed by the user, so resume explains that the run predates workflow versioning and must be restarted with the current binary or finished with the older binary.

## Out of Scope

- Patch, prerelease, or build-metadata version components.
- Commands or UI controls for starting a new run from a non-latest historical version.
- Automatic version bumping or inferring whether a workflow change is major or minor.
- Enforcing workflow immutability or blocking resume because a versioned file's contents changed.
- Fixing general resume path-resolution behavior unrelated to the filename migration.
- Recovering a historical workflow after its versioned file has been removed.

## Impact

- Workflow discovery, built-in embedding, CLI name resolution, and validation must understand versioned filenames, version-free YAML names, and logical workflow groups.
- New-run list and definition views must use the latest version while presenting and searching only version-neutral workflow metadata.
- Saved-run resolution and the non-live run-view breadcrumb must preserve and surface the recorded version; pre-migration state naming an unversioned file fails with source-appropriate migration guidance.
- Built-in workflow YAML files and their relative sub-workflow references must be migrated to versioned filenames.
- User-authored workflows must be renamed to the required convention before new runs can be launched or resumed; validation errors will explain the migration.
