## Why

Agent-runner ships a useful set of workflows (planning, implementation, validation, PR finalization) in this repo's top-level `workflows/` directory, but users in other projects can only run them by copying YAML files locally. The current resolver also makes no distinction between "shipped with the binary" and "defined by this project", so there is no way to ship workflows as a stable, discoverable feature of the tool.

## What Changes

- **BREAKING** Workflows are no longer resolved from a top-level `workflows/` directory in the user's current working directory. User-defined workflows must live under `.agent-runner/workflows/`.
- **BREAKING** A namespaced name (`<ns>:<name>`) always resolves to an embedded builtin. It never falls back to local files.
- **BREAKING** Bare names (no colon) never resolve to builtins. A non-namespaced argument must exist under `.agent-runner/workflows/`.
- Workflows from this repo's `workflows/` directory are embedded into the `agent-runner` binary at build time and become the set of builtins.
- Top-level YAML files in `workflows/` (currently `finalize-pr`, `implement-task`, `run-validator`) move into a new `workflows/core/` subdirectory. Users invoke them as `core:finalize-pr`, `core:implement-task`, `core:run-validator`.
- Workflow name validation is extended to accept `/` in bare names so users can organize `.agent-runner/workflows/` into subdirectories and invoke nested workflows as `subdir/name`.
- This repo's own `smoke-test*.yaml` workflows move out of `workflows/` (which now only contains builtins) into this repo's `.agent-runner/workflows/` directory.

## Capabilities

### New Capabilities
- `builtin-workflows`: workflows embedded into the `agent-runner` binary at build time, accessible by namespace prefix.

### Modified Capabilities
- `workflow-name-resolution`: resolution split into two disjoint namespaces — prefixed names resolve to builtins only; bare names resolve from `.agent-runner/workflows/` only and may contain `/` for subdirectory paths.

## Out of Scope

- A `list` or discovery command for builtin or user workflows.
- Letting users override a builtin with a same-named local workflow (namespaces are disjoint by design).
- Packaging or distributing user workflows as builtins (e.g., plugin-style workflow registries).
- Changes to how sub-workflow references inside a workflow (`workflow: other.yaml`) resolve relative to the containing workflow.

## Impact

- `cmd/agent-runner/main.go`: `resolveWorkflowArg` rewritten; new builtin FS resolution path; validation pattern updated.
- New embedded file system for builtins using Go's `embed` package, rooted at the repo's `workflows/` directory.
- `internal/loader`: must be able to load a workflow from the embedded FS, not only from disk (including resolving sub-workflow references from within an embedded workflow).
- `workflows/` directory contents reorganized: `finalize-pr.yaml`, `implement-task.yaml`, `run-validator.yaml` moved to `workflows/core/`; `smoke-test*.yaml` moved to `.agent-runner/workflows/`.
- Existing test scripts referencing `workflows/` as a working location in CWD need to migrate to `.agent-runner/workflows/`.
- Any user or documentation referencing the top-level `workflows/` convention needs updating (README, docs).
