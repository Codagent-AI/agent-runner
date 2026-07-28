## Context

Workflow identity is currently the filename without its extension. Discovery, CLI resolution, embedded built-in resolution, workflow loading, saved-run inspection, and resume each use that identity in slightly different ways. As a result, a substantial rewrite is published under a second visible name such as `change2`, and the system has no common rule for treating `change.yaml` and `change2.yaml` as generations of one workflow.

The existing runtime already preserves the key historical pointer: `RunState.WorkflowFile` records the resolved workflow file, and `WorkflowHash` detects in-place edits. This change therefore does not need a new state field or a workflow archive. It needs a shared interpretation of versioned filenames for new-run selection, plus exact-file validation for paths already recorded in state or pinned by a parent workflow.

The change crosses several existing boundaries:

- `internal/discovery` finds project, user, and embedded workflows for the New tab.
- `cmd/agent-runner` separately resolves CLI workflow names.
- `workflows` resolves and reads embedded definitions.
- `internal/loader` validates workflow contents but currently has no filename contract.
- `internal/runner` reloads the exact path recorded in saved state.
- `internal/runview` derives display names from resolved paths and can reconstruct saved runs when definitions are unavailable.

The project is pre-release and has few active users, so the migration favors a clear required convention and actionable failures over a compatibility layer for unversioned files. Versioned definitions remain intentionally mutable: versions distinguish occasional coexisting generations, not every content edit.

## Goals / Non-Goals

### Goals

- Give filename parsing, numeric ordering, grouping, duplicate detection, and invalid-group behavior one shared implementation.
- Resolve a version-free logical name to the latest valid version within the existing project, user, or built-in source rules.
- Validate every exact workflow load against the versioned filename convention and the version-free YAML `name:`.
- Preserve exact-version loading for resume and sub-workflow references without silently upgrading either.
- Keep new-run UI and search version-neutral while showing the recorded version in saved, non-live run views.
- Migrate all embedded workflows and their explicit references as one consistent set.
- Preserve current non-blocking behavior when the contents of a recorded version are edited.

### Non-Goals

- Adding a version field to workflow YAML or saved state.
- Implementing full Semantic Versioning, patch versions, prereleases, or build metadata.
- Automatically bumping versions or deciding whether a change is major or minor.
- Making versioned files immutable or turning hash mismatches into resume errors.
- Launching a new run from an explicitly selected historical version.
- Archiving workflow contents or recovering a deleted historical definition.
- Generalizing saved-run path recovery beyond the behavior required to avoid substituting a newer version.

## Approach

Introduce a shared, source-neutral workflow catalog layer for version identity and selection. Source adapters enumerate candidate YAML files from a local directory or the embedded filesystem and feed their canonical relative paths into the catalog. The catalog produces one logical group per canonical name, with either a selected latest candidate or a deterministic group error.

```text
project files ─┐
user files ────┼─ source adapters ── workflow catalog ─┬─ CLI logical-name resolution
builtin FS ────┘                                      └─ New-tab discovery

recorded or referenced exact path ── loader validation ── exact workflow contents
```

Logical lookup and exact loading are deliberately separate:

- A **new run** supplies a version-free logical name. The catalog applies source precedence and selects the latest version in the winning source.
- **Resume**, **sub-workflow execution**, and exact internal built-in reads already have a physical versioned path. They bypass latest selection and pass that path through the loader's filename and YAML-name validation.
- A **saved-run view** derives its label from the filename stored in `RunState.WorkflowFile`. It does not need to load the definition merely to know the version.

The catalog should remain independent of workflow YAML parsing and runtime model types. It owns filename-derived facts and grouping; source adapters and the loader attach content-derived metadata and validation results. This avoids a dependency cycle among discovery, loading, and embedded workflow packages.

## Decisions

### 1. Use one catalog for filename identity and latest selection

The catalog exposes pure operations for:

- parsing a workflow basename into logical name, major version, and minor version;
- deriving a version-free canonical name while retaining directory or built-in namespace context;
- comparing versions numerically;
- grouping candidates within one source;
- detecting duplicate numeric versions across `.yaml` and `.yml`;
- invalidating a group when any candidate belonging to it is invalid; and
- returning the selected latest candidate or the group's actionable error.

CLI resolution and New-tab discovery use these same operations. Built-in logical resolution also delegates to the same catalog behavior; exact `builtin:` reads remain direct embedded-file reads.

**Alternative considered:** Make `internal/discovery` itself the universal resolver. This would reuse an existing package, but that package also owns UI-facing metadata and imports both the loader and embedded workflows, making it an awkward low-level dependency.

**Alternative considered:** Share only a filename parser while leaving selection in the CLI, discovery, and built-in packages. This is a smaller initial diff but duplicates the most important behavior and makes source precedence, invalid groups, and duplicate handling likely to drift.

### 2. Represent version components as validated decimal text

The specification does not impose an integer-size limit. After rejecting leading zeroes, two non-negative decimal components can be compared numerically by digit count and then lexically when their lengths match. Major is compared first, then minor.

This supports values larger than machine integers without a new dependency or an undocumented overflow limit. The original validated text is retained for display as `v<major>.<minor>`.

**Alternative considered:** Parse into `uint64`. That is simpler but would silently introduce a maximum version component not present in the contract.

**Alternative considered:** Use `math/big.Int`. It supports arbitrary size but is unnecessary for comparison and introduces mutable numeric values where normalized strings are sufficient.

### 3. Associate invalid filenames with a logical group deterministically

Invalid definitions must block valid siblings in the same logical group without blocking unrelated workflows. Candidate classification therefore returns both a validation result and a best-effort group key:

- a valid versioned filename uses its parsed logical name;
- an unversioned filename uses its entire stem;
- a malformed terminal version attempt beginning with `-v` followed by a digit uses the preceding stem, so `deploy-v1.yaml` and `deploy-v1.x.yaml` belong to `deploy`;
- case is normalized only for grouping, so `Deploy-v1.0.yaml` invalidates the `deploy` group rather than creating a second launchable identity.

Earlier `-v` text that is not a terminal version attempt remains part of the name, so `save-v-data-v1.2.yaml` groups as `save-v-data`.

Group errors are stable and name all relevant conflicting files. YAML files whose basenames start with `_` are filtered by every source adapter before workflow classification, preserving the existing project/user behavior as well as built-in metadata handling.

### 4. Apply source precedence before comparing versions

The catalog is built independently for each source. Resolution follows the existing source contract:

- a namespaced name queries only its embedded namespace;
- a bare or path-style name queries the project catalog first;
- the user catalog is queried only if the project catalog has no group with that logical name;
- bare names never fall back to built-ins.

The presence of a project group counts even when the group is invalid. Its error is returned instead of falling through to a valid user workflow. Version comparison never crosses source boundaries.

This also drives New-tab shadowing: project groups replace user groups by version-free canonical identity, rather than by physical filename.

### 5. Make exact-file loading filename-aware

`loader.LoadWorkflow` validates the filename before reading the file, then parses the YAML and checks that `workflow.Name` equals the parsed, version-free basename. Validating before the read ensures legacy on-disk state such as `deploy.yaml` receives migration guidance even when the old file has already been renamed or removed. Filename validation returns a structured error that retains whether the source was on disk or an embedded `builtin:` ref, allowing resume to provide source-appropriate guidance.

The raw `ParseWorkflow` operation remains content-only because bytes without a source path cannot be checked against a filename. A source-aware parse entry point is used when discovery has bytes from an injected or embedded `fs.FS`; it performs the same filename and YAML-name checks as `LoadWorkflow`.

This single enforcement point covers:

- CLI validation of an explicit versioned file;
- definitions selected by logical-name resolution;
- exact sub-workflow references;
- exact resume paths;
- definition views; and
- embedded discovery using test or production filesystems.

Discovery validates every candidate in a logical group, not only the latest. Therefore an invalid older version or a YAML-name mismatch cannot be hidden by a valid newer sibling.

### 6. Keep new-run arguments logical and version-free

Execution validates the argument as a lowercase logical name before checking for an existing filesystem path. A path, filename extension, dot, uppercase letter, or explicit version suffix is rejected for `run`. When a version-bearing name or file is recognizable, the error identifies the version-free logical name to launch.

Validation mode retains its exact-file escape hatch: an existing `.yaml` or `.yml` path is passed to the filename-aware loader. This validates historical or otherwise non-latest definitions without making them launchable as new runs.

A dotless terminal attempt such as `deploy-v1` remains a valid logical name and resolves normally when such a group exists. If lookup fails, the not-found error adds a conditional hint to run `deploy` if the user intended to select version 1.

Read-only debug inspection follows the same separation without granting launch permission: `<namespace>:<logical-name>` resolves the latest embedded version, while an exact `builtin:<namespace>/<versioned-file>` ref or on-disk path reads that exact version. A version-bearing namespaced shorthand is not added; exact historical built-ins use the existing unambiguous `builtin:` form.

### 7. Preserve exact paths for resume and sub-workflows

No version-selection lookup occurs after a run has recorded a path or a parent has named a child:

- resume passes `RunState.WorkflowFile` directly to the loader;
- sub-workflow path resolution continues resolving relative to the parent and then loads that exact file;
- a missing exact version fails without selecting a sibling;
- an unversioned on-disk exact path fails with rename guidance;
- an unversioned saved `builtin:` path fails with guidance to restart on the current binary or finish on the older binary, since users cannot rename embedded files; and
- a changed hash retains the current warning and resume continues.

This deliberately permits editing `deploy-v1.0.yaml` or a pinned child in place. The filename identifies a coexisting generation, not immutable content.

### 8. Do not add version to workflow or state models

The filename is the only version authority. New runs already persist the selected physical path in `RunState.WorkflowFile`; adding another field would duplicate that information and create mismatch cases.

Display code parses the recorded path directly:

- versioned saved state produces `v<major>.<minor>`;
- legacy unversioned saved state produces `unversioned`;
- the result remains available even if the file itself is missing.

The existing workflow hash remains the mechanism for noticing mutable contents.

### 9. Keep saved-run reconstruction from substituting latest

For saved non-live inspection, the resolver first attempts the exact recorded path using its existing absolute, recorded-cwd-relative, current-cwd-relative, and embedded-reference handling. If a non-empty recorded workflow path cannot be found, the view does not fall back from `WorkflowName` to a different version. It reconstructs whatever it can from state and audit evidence and reports the missing definition.

Name-based fallback remains available only for legacy state that has no recorded workflow file. This prevents a completed `v1.0` run from being rendered using `v2.0` merely because the historical file was removed.

An unversioned completed run is still allowed into inspection. Filename validation failure makes its definition unavailable for static reconstruction but does not block state/audit reconstruction or the `unversioned` label. Execution resume remains strict and rejects the same legacy path.

### 10. Make canonical display names version-neutral

Canonical-name formatting strips a valid terminal version suffix in addition to the extension. This affects top-level and nested workflow display names consistently, while physical source paths remain unchanged for loading and state.

The run-view model stores a recorded version label only when constructed for a saved, non-live run (`FromList` or `FromInspect`). Breadcrumb rendering appends that label to the top-level workflow name at every drill depth. `FromLiveRun` and `FromDefinition` do not set a label, so their breadcrumbs remain version-neutral.

### 11. Let the catalog shape the New tab

Discovery emits one `WorkflowEntry` per logical group:

- valid groups carry the latest candidate's exact `SourcePath`, description, parameters, and hidden value;
- invalid groups carry their version-free canonical name and group error but cannot be opened or launched.

Latest selection happens before hidden filtering. An older visible version never replaces a hidden latest version. Search matches version-free logical identity and existing version-neutral metadata only; it no longer indexes `SourcePath`. Older candidates are not retained as display rows, so search and show-hidden cannot reveal them.

### 12. Migrate built-ins atomically

All built-in workflow YAML definitions, excluding underscore-prefixed metadata, are renamed in one change:

- the three existing base/`2` pairs in the `openspec` namespace become coexisting `v1.0` and `v2.0` generations;
- every other built-in begins at `v1.0`;
- every YAML `name:` is changed to the lowercase version-free basename where necessary; and
- every relative sub-workflow reference is rewritten to an exact versioned filename.

The identically named `spec-driven` workflows have no `2` counterparts and therefore begin at `v1.0` only. Both `.yaml` and `.yml` files are workflow definitions; the bundled-asset API excludes both extensions.

The parent-child graph is validated as a complete embedded set. Pinning means a deliberately published child generation is not adopted until each parent reference is intentionally changed. That can cause parent version-bump cascades when authors want old and new compositions to coexist; this is accepted in exchange for stable historical composition. Ordinary in-place edits do not require such a cascade.

## Risks / Trade-offs

- **[A malformed file invalidates an otherwise runnable group]** → Emit one non-launchable diagnostic row and a CLI error naming the offending file, required pattern, and a concrete rename. Keep unrelated groups usable.
- **[Mutable versioned files are not reproducible snapshots]** → Preserve the workflow hash warning so users know the recorded version changed, while honoring the explicit decision that frequent edits remain resumable.
- **[Pinned sub-workflows increase maintenance when publishing coexisting generations]** → Validate the entire built-in reference graph and require exact references. In-place fixes remain available when a new coexisting generation is not needed.
- **[Catalog behavior could diverge across local and embedded sources]** → Keep parsing, grouping, comparison, duplicate handling, and group errors source-neutral; source adapters only enumerate and translate paths.
- **[Discovery now validates every version, increasing startup work]** → Workflow files are small and local, and correctness requires invalid older siblings to remain visible. Avoid loading unrelated groups during direct CLI resolution where the source adapter can narrow candidates safely.
- **[A missing historical definition limits saved-run detail]** → Never substitute a newer definition. Continue rendering all information recoverable from state and audit data and surface that static definition data is unavailable.
- **[Strict filename validation breaks existing user workflows and unfinished legacy runs]** → Provide concrete rename guidance for on-disk definitions and restart/older-binary guidance for embedded legacy runs. Completed legacy runs remain inspectable as `unversioned`.
- **[Downgrading loses renamed embedded files]** → Treat rollback as safe only before runs start on the new release. Once versioned built-in paths have been recorded, prefer fixing forward or retain the newer binary to finish those runs.

## Migration Plan

1. Add the pure filename/version/catalog primitives with table-driven tests for valid forms, malformed forms, case handling, numeric ordering, nested names, duplicates, and invalid-group association.
2. Make the loader source-aware and enforce filename plus YAML-name alignment for exact loads. Update loader fixtures and tests to use versioned filenames where they exercise `LoadWorkflow`; keep raw parsing tests pathless.
3. Route local discovery, embedded discovery, CLI logical resolution, and built-in logical resolution through the shared catalog. Add tests for project/user precedence, invalid higher-precedence groups, and namespaced isolation.
4. Update New-tab filtering and actions to consume logical groups, select metadata from the latest version, and render invalid groups as disabled diagnostic rows.
5. Update canonical display naming and saved-run construction so only non-live saved views show the recorded version. Disable name fallback when a non-empty recorded versioned path is missing.
6. Keep resume and sub-workflow execution on exact paths and add coverage for edited, missing, legacy unversioned, and newer-sibling cases.
7. Rename the complete built-in YAML set, update version-free `name:` values, and rewrite every sub-workflow reference to an exact version. Update Go-code embedded path literals, legacy-ref comparisons, and tests; ensure every `<namespace>:<logical-name>` call site uses catalog-backed resolution while exact internal reads retain versioned `builtin:` refs. Add an integration check that enumerates and loads every embedded definition and verifies every referenced child exists.
8. Update documentation, examples, and workflow fixtures that users may copy so they demonstrate the required filename convention and logical launch syntax.
9. Run focused package tests while iterating, then formatting, the full test suite, lint, and strict OpenSpec validation.

There is no automatic user-file or saved-state rewrite. Users rename definitions manually based on the diagnostic. Completed old runs remain inspectable; unfinished old runs must be restarted or have their recorded file migration handled manually outside Agent Runner.

Rollback before any run starts on the new build consists of restoring the previous binary and built-in set. After a run records a renamed embedded path, rollback cannot preserve resume with the old binary because that file was not embedded there. At that point the supported operational response is to fix forward or use the newer binary until the run completes.

## Open Questions

None. The filename grammar, source precedence, exact-version behavior, mutability policy, UI exposure, legacy handling, built-in mapping, and rollback posture are settled by the proposal and specifications.
