# Task: Preserve Recorded and Referenced Versions

## Goal

Carry exact versioned paths through nested execution, audit evidence, saved state, resume, script resolution, and non-live inspection. Historical runs and pinned children must never be silently replaced by a newer logical version, while completed legacy runs remain inspectable.

## Background

Exact-path consumers must remain separate from logical latest lookup:

- `internal/exec/subworkflow.go` resolves interpolation and relative paths, then loads that exact version;
- `internal/runner/runner.go` persists `WorkflowFile`, computes the hash from that file, and emits run metadata;
- `internal/runner/resume.go` reloads `RunState.WorkflowFile` exactly and preserves the changed-hash warning;
- `cmd/agent-runner/main.go` routes unfinished `--resume <id>` to execution and completed sessions to `runview.FromInspect`;
- `internal/runview/resolve.go` reconstructs saved views from the recorded path and currently falls back by name;
- `internal/runview/model.go`, `internal/runview/names.go`, and `internal/runview/breadcrumb.go` derive canonical names and saved-view labels;
- `internal/exec/step_audit.go` and `internal/exec/subworkflow.go` emit nested audit metadata; and
- `internal/exec/script.go` resolves scripts relative to the containing workflow.

Keep `RunState` unchanged: filename is the version authority and `WorkflowHash` remains the mutable-content warning mechanism. Resume must pass the recorded path directly to the filename-aware loader. Missing versions fail without sibling lookup. For unversioned saved state, on-disk paths receive rename guidance, while `builtin:` paths explain binary incompatibility and never tell users to rename embedded files.

Completed sessions are inspection, not execution. They must open even when their definition is missing or unversioned, using state/audit reconstruction. In `ResolveWorkflow`, allow name fallback only when legacy state has no recorded workflow file. A non-empty missing recorded path must never resolve `WorkflowName` to a newer definition.

Canonical formatting strips a valid terminal version suffix but preserves directory/namespace context. Add a recorded version label to `runview.Model` only for `FromList` and `FromInspect`; `FromLiveRun` and `FromDefinition` stay version-neutral. Derive `v<major>.<minor>` or `unversioned` from `RunState.WorkflowFile` without requiring the file to exist, and retain the label at every breadcrumb depth.

For sub-workflow `step_start`, include the exact resolved child path and interpolated params once they are known. Keep `sub_workflow_start`/`sub_workflow_end` nesting and exact path metadata. Script resolution stays within the containing embedded namespace or on-disk workflow directory and never falls back across sources.

Update user-facing documentation and copyable examples in `docs/writing-workflows.md`, `docs/cli-reference.md`, `docs/built-in-workflows.md`, `docs/run-state-and-audit.md`, `docs/troubleshooting.md`, `docs/quickstart.md`, and any other affected page. Follow `docs/AGENTS.md`: current logical launch syntax is version-free, authored filenames and pinned child refs are versioned, exact historical refs are read-only, and migration/rollback limits are explicit.

Finish with targeted tests, `make fmt`, `make test`, `make lint`, `make build`, and strict OpenSpec validation.

## Spec

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

### Requirement: Resume by session ID

The CLI SHALL accept a `--resume` flag that optionally takes a session ID. When `--resume` is passed without a session ID, it SHALL launch the run list TUI. When `--resume <id>` is passed with a session ID for an unfinished run, it SHALL resume workflow execution from that session's saved state using the exact recorded workflow version. A newer workflow version MUST NOT replace the recorded version.

For an unfinished run, a changed workflow hash SHALL retain the existing warning behavior and SHALL NOT block resume. A missing recorded versioned file SHALL fail resume without selecting another version. An unversioned recorded on-disk workflow file SHALL fail with actionable filename migration guidance and MUST NOT fall back to a versioned file. An unversioned recorded `builtin:` workflow reference SHALL instead explain that the run predates workflow versioning and cannot resume with the current binary, and SHALL guide the user to restart it or finish it using the older binary.

When `--resume <id>` is passed with a session ID for a completed run, it SHALL open the run view for that session in inspect mode (the same read-only view as `--inspect <id>`) instead of validating the workflow filename for execution or resuming. Completed unversioned runs and completed runs whose workflow files are missing SHALL remain inspectable from recoverable saved run evidence.

#### Scenario: Resume with explicit session ID on an unfinished run
- **WHEN** `--resume <id>` is passed and the matching session's saved state indicates the workflow is not yet complete
- **THEN** the runner resumes workflow execution from that session's saved state

#### Scenario: Unfinished run resumes exact recorded version
- **WHEN** an unfinished run records `deploy-v1.0.yaml`, `deploy-v2.0.yaml` also exists, and the user resumes the run
- **THEN** the runner loads `deploy-v1.0.yaml` and does not select `deploy-v2.0.yaml`

#### Scenario: Edited recorded version warns and resumes
- **WHEN** an unfinished run's recorded versioned file differs from its saved workflow hash
- **THEN** Agent Runner emits the existing changed-file warning and continues resume from that recorded file

#### Scenario: Missing recorded version fails resume
- **WHEN** an unfinished run records `deploy-v1.0.yaml` and that file is missing
- **THEN** resume fails with an error naming the missing recorded version and does not select another version

#### Scenario: Unfinished legacy run fails migration validation
- **WHEN** an unfinished run records unversioned `deploy.yaml` and the user resumes the run
- **THEN** resume fails with actionable guidance to migrate to a versioned filename and does not fall back to `deploy-v1.0.yaml`

#### Scenario: Unfinished legacy builtin run requires restart or older binary
- **WHEN** an unfinished run records `builtin:onboarding/onboarding.yaml` and the user resumes the run
- **THEN** resume fails with an error that the run predates workflow versioning
- **AND** the error guides the user to restart with the current binary or finish with the older binary instead of instructing them to rename an embedded file

#### Scenario: Resume with explicit session ID on a completed run opens run view
- **WHEN** `--resume <id>` is passed and the matching session's saved state indicates the workflow has already completed (either because `completed` is true, or because no incomplete steps remain)
- **THEN** the runner opens the run view for that session in inspect mode so the user can read its recorded output, and exits when the view is dismissed

#### Scenario: Completed unversioned run remains inspectable
- **WHEN** a completed run records unversioned `deploy.yaml` and the user invokes `--resume <id>`
- **THEN** the runner opens the run in inspect mode and displays its workflow version as `unversioned`

#### Scenario: Completed run with missing definition remains inspectable
- **WHEN** a completed run's recorded workflow file is missing but its saved run evidence can reconstruct the view
- **THEN** `--resume <id>` opens the run in inspect mode without substituting another workflow version

#### Scenario: Resume without session ID launches TUI
- **WHEN** `--resume` is passed without a session ID
- **THEN** the run list TUI is launched

#### Scenario: Resume with nonexistent session ID
- **WHEN** `--resume <id>` is passed and no session matches that ID
- **THEN** the runner exits with an error indicating the session was not found

#### Scenario: Resume rejects extra positional arguments
- **WHEN** `--resume` is passed with more than one positional argument
- **THEN** the runner exits with an error indicating resume mode accepts at most one argument (the session ID)

### Requirement: Sub-workflow invocation

A step with a `workflow` field SHALL load and execute the exact referenced workflow file. The resolved reference MUST use a valid versioned workflow filename and MUST NOT be replaced by a newer available version. The step MUST NOT have `prompt`, `command`, or `mode` — it delegates entirely to the sub-workflow. The sub-workflow executes in the same process as the parent.

#### Scenario: Sub-workflow executes successfully
- **WHEN** a step has `workflow: workflows/run-validator-v1.0.yaml` and the referenced file exists
- **THEN** Agent Runner loads that exact sub-workflow version, executes its steps, and continues with the next step in the parent

#### Scenario: Sub-workflow file not found
- **WHEN** a step has `workflow: workflows/missing-v1.0.yaml` and the file does not exist
- **THEN** Agent Runner fails with a descriptive error naming the missing versioned file

#### Scenario: Unversioned sub-workflow rejected
- **WHEN** a step has `workflow: workflows/run-validator.yaml`
- **THEN** Agent Runner fails with the actionable versioned-filename error

#### Scenario: Sub-workflow step is mutually exclusive with prompt/command/mode
- **WHEN** a step has both `workflow` and `prompt` (or `command` or `mode`)
- **THEN** Agent Runner fails at load time with a validation error

### Requirement: Parameter passing to sub-workflows

A step with `workflow` MAY include a `params` map that passes values to the sub-workflow. Values support `{{var}}` interpolation. The sub-workflow SHALL receive only the parameters explicitly passed — it MUST NOT implicitly inherit the parent's parameter scope.

#### Scenario: Parameters passed to sub-workflow
- **WHEN** a step has `workflow: workflows/implement-task-v1.0.yaml` and `params: { task_file: "{{task_file}}" }`
- **THEN** the sub-workflow receives `task_file` as a parameter and can reference it via `{{task_file}}`

#### Scenario: Missing required parameter
- **WHEN** a sub-workflow declares a required parameter and the parent step's `params` map does not include it
- **THEN** Agent Runner fails with a descriptive error naming the missing parameter

#### Scenario: Sub-workflow does not inherit parent params implicitly
- **WHEN** the parent workflow has a parameter `change_name` but the step's `params` map does not pass it
- **THEN** the sub-workflow cannot reference `{{change_name}}`

### Requirement: Run start context

The `run_start` event SHALL include the exact resolved versioned workflow file path, version-free workflow name, workflow hash, and all params.

#### Scenario: Run start captures workflow metadata
- **WHEN** a run begins for workflow `workflows/deploy-v1.0.yaml` with params `{env: "staging"}`
- **THEN** the `run_start` entry includes the exact versioned file path, version-free name, hash, and params

### Requirement: Sub-workflow-level events

Agent Runner SHALL emit `sub_workflow_start` before executing an exact versioned sub-workflow's steps and `sub_workflow_end` after all sub-workflow steps complete, nested between the sub-workflow step's `step_start` and `step_end`.

#### Scenario: Sub-workflow executes
- **WHEN** a sub-workflow step invokes `verify-task-v1.0.yaml`
- **THEN** Agent Runner emits `sub_workflow_start`, then child step events, then `sub_workflow_end`, all nested within the step's `step_start` / `step_end`

### Requirement: Sub-workflow step-specific data

Sub-workflow step `step_start` entries SHALL include the exact resolved versioned workflow path and interpolated params passed.

#### Scenario: Sub-workflow start
- **WHEN** a sub-workflow step starts with resolved path `workflows/verify-v1.0.yaml` and params `{task: "tasks/1.md"}`
- **THEN** the `step_start` entry includes the exact versioned path and params

### Requirement: Embedded vs on-disk script resolution

When the containing versioned workflow is part of the embedded builtin set, the runner SHALL resolve `script:` references only against the embedded namespace and SHALL NOT fall back to user-authored workflows under `.agent-runner/workflows/`. When the containing versioned workflow is loaded from disk, the runner SHALL read the script from disk relative to the workflow file's directory.

#### Scenario: Embedded script resolves within embedded namespace
- **WHEN** an embedded versioned workflow in the `onboarding` namespace declares `script: helper.sh`
- **THEN** the runner reads the script from the embedded `onboarding/helper.sh` and executes it

#### Scenario: Embedded script does not fall back to user directory
- **WHEN** an embedded versioned workflow in the `onboarding` namespace declares `script: helper.sh` and a file `.agent-runner/workflows/onboarding/helper.sh` exists on the user's disk
- **THEN** the runner uses the embedded script, not the user file

#### Scenario: On-disk workflow reads script from disk
- **WHEN** a workflow loaded from `.agent-runner/workflows/foo/main-v1.0.yaml` declares `script: helper.sh`
- **THEN** the runner executes `.agent-runner/workflows/foo/helper.sh`

### Requirement: Drill-in navigation with breadcrumbs

The run view SHALL support drilling into sub-workflows and loops via a drill-in model. Enter on a drillable row SHALL scope both the step list AND the log to that container's subtree: the step list shows that container's children, and the log shows those children's blocks (with descendants inline). A breadcrumb line at the top SHALL show the current depth path (run name, then each entered container in order).

When a saved run is opened for non-live inspection from the run list or through `--inspect`, the top-level breadcrumb SHALL show the recorded workflow version next to the version-free canonical runnable name using a `v<major>.<minor>` label. The version label SHALL remain present at every drill depth and for every saved-run status. A saved run whose recorded workflow file is unversioned SHALL remain inspectable and SHALL display `unversioned` instead of a numeric version.

The live run view and the pre-run definition preview MUST NOT show a workflow version. The exact separator and styling of the non-live version label are design details.

Drillable rows SHALL be: sub-workflow steps and loop steps. Drill-in SHALL be available on `pending` containers (children read from the workflow file or resolved statically) as well as executed ones.

#### Scenario: Top-level breadcrumb rendering
- **WHEN** the run view is at the top level (no drill-in)
- **THEN** the breadcrumb shows the workflow's version-free canonical runnable name, the start time, and the run status (active/failed/completed/inactive)

#### Scenario: Non-live saved run shows recorded version
- **WHEN** a saved run recorded `deploy-v2.0.yaml` and is opened from the run list or through `--inspect`
- **THEN** the top-level breadcrumb shows canonical name `deploy` with version label `v2.0`

#### Scenario: Saved-run status does not suppress version
- **WHEN** a saved run is opened for non-live inspection with status inactive, failed, or completed
- **THEN** the breadcrumb shows its recorded version alongside the status

#### Scenario: Version remains visible after drill-in
- **WHEN** the user drills into a sub-workflow, loop, or iteration while inspecting a saved versioned run
- **THEN** the breadcrumb retains the top-level workflow's recorded version while appending the entered container

#### Scenario: Live run omits version
- **WHEN** a workflow is executing in the live run view
- **THEN** the breadcrumb shows the version-free canonical workflow name without a version label

#### Scenario: Definition preview omits version
- **WHEN** the user opens a workflow's pre-run definition preview
- **THEN** the breadcrumb shows the version-free canonical workflow name without a version label

#### Scenario: Legacy saved run displays unversioned
- **WHEN** a saved run recorded an unversioned workflow file and is opened for non-live inspection
- **THEN** the breadcrumb displays `unversioned` and inspection continues without a filename-version error

#### Scenario: Enter on sub-workflow drills in and scopes log
- **WHEN** the user presses Enter on a sub-workflow step row
- **THEN** the step list is replaced by the sub-workflow's children, the log is scoped to show only that sub-workflow's children's blocks (and their descendants inline), and the breadcrumb appends the sub-workflow entry

#### Scenario: Enter on loop drills into iteration list and scopes log
- **WHEN** the user presses Enter on a loop step row
- **THEN** the step list is replaced by a list of iterations, the log is scoped to show only that loop's iteration blocks (and their descendants inline), and the breadcrumb appends the loop entry

#### Scenario: Enter on iteration drills into iteration children
- **WHEN** the user presses Enter on an iteration row in the iteration list
- **THEN** the step list is replaced by that iteration's child steps, the log is scoped to show only those children's blocks (and their descendants inline), and the breadcrumb appends the iteration identifier

#### Scenario: Drill in to pending sub-workflow
- **WHEN** the user presses Enter on a sub-workflow step that has not yet executed
- **THEN** the sub-workflow file is read and its children are displayed with status `pending`; the log contains no blocks at that level (pending steps are hidden from the log)

#### Scenario: Enter on shell step is a no-op
- **WHEN** the user presses Enter on a shell step row
- **THEN** nothing happens (shell steps are neither drillable nor resumable)

#### Scenario: Enter on agent step without session ID is a no-op
- **WHEN** the user presses Enter on an agent step that has no resolved session ID
- **THEN** nothing happens (the resume action requires a session ID)

## Done When

- New runs persist and audit the exact selected versioned path with the version-free YAML name and unchanged hash semantics.
- Sub-workflows, including interpolated and builtin references, load exact versioned targets; step and lifecycle audit events contain the exact child path and params.
- Unfinished resume handles exact, edited, missing, on-disk legacy, and builtin legacy state with the specified behavior and no latest-version fallback.
- Completed legacy or missing-definition sessions enter read-only inspection and reconstruct from state/audit evidence without filename validation blocking the view.
- Saved non-live breadcrumbs show `v<major>.<minor>` or `unversioned` at every drill depth; live and definition views remain version-neutral.
- Embedded and on-disk scripts resolve relative to the containing versioned workflow without cross-source fallback.
- Documentation and examples consistently explain authored filenames, logical launch names, pinned children, read-only exact refs, migration, mutability, and rollback constraints.
- `make fmt`, `make test`, `make lint`, `make build`, and `openspec validate --type change workflow-versioning --strict` pass.
