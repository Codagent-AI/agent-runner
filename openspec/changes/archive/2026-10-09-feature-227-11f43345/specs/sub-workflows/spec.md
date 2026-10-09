## ADDED Requirements

### Requirement: Workflow workspace directories

A workflow MAY declare `workspace_dirs`, a list of directory strings at the workflow level. The values are interpolated against the workflow's own parameters and the captured variables visible at that point. Interpolation happens when an agent step, `call_agent` child, or external-user turn in that workflow is about to start, and when one of its sub-workflow steps starts, so values may reference captures from earlier steps. Resume uses the restored captures. An undefined variable at that point SHALL fail that step before any CLI spawns. The resulting directories SHALL apply to every agent step in that workflow and in all of its nested sub-workflows. A nested workflow's directories are the union of its parent's directories and its own. Before any directory is used, the runner SHALL normalize the list:

- empty values are dropped;
- symlinks are resolved;
- duplicates are removed;
- directories equal to the run's project root, or inside it, are dropped.

Every remaining directory SHALL be absolute and SHALL exist. Otherwise the step that triggered evaluation SHALL fail before any CLI spawns or child step runs, with an error naming the value. Agent steps, `call_agent` child agents, and external-user turns SHALL receive the normalized list as additional workspace directories (see `cli-adapter`). A workflow without `workspace_dirs` SHALL inherit its parent's list unchanged.

#### Scenario: Directory applies to nested agent steps
- **WHEN** a workflow with `workspace_dirs: ["{{spec_root}}"]` and `spec_root=/work/specs` invokes a core sub-workflow that runs an agent step
- **THEN** that agent step receives `/work/specs` as an additional workspace directory

#### Scenario: Directory taken from an earlier capture
- **WHEN** a workflow declares `workspace_dirs: ["{{openspec.spec_root}}"]`, its first step captures `openspec` with `spec_root=/work/specs`, and a later step runs an agent
- **THEN** that agent step receives `/work/specs` as an additional workspace directory

#### Scenario: Directory inside the project root is dropped
- **WHEN** a workflow declares `workspace_dirs: ["{{spec_root}}"]` and `spec_root` equals the run's project root
- **THEN** agent steps receive no additional workspace directories

#### Scenario: Missing directory
- **WHEN** a workflow declares `workspace_dirs: ["{{spec_root}}"]` and `spec_root=/work/missing` does not exist
- **THEN** the first agent or sub-workflow step that evaluates the list fails with an error naming `/work/missing`, and no agent spawns
