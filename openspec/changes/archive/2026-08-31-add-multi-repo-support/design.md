# Design: Multi-Repository Workspace Execution

## Context

Agent Runner currently has one launch directory, one discovered project root, and one recursive execution state. A step may declare `workdir`, but the runner has no durable concept of an orchestration workspace containing several independent Git repositories. Named sessions are already shared across loop and sub-workflow contexts, while unnamed sessions and captures follow the existing structural context tree.

The target project shape is:

```text
foo/                         # coordination Git repository
├── .agent-runner/
│   └── config.yaml
├── openspec/                # shared definition and planning root
├── a/                       # ignored independent Git repository
├── b/                       # ignored independent Git repository
└── c/                       # ignored independent Git repository
```

Repository paths may also point outside `foo/`, including sibling directories. One run launched from the workspace Git root owns the state, audit log, artifacts, resume identity, OpenSpec lifecycle, and workspace pull request. Work assigned to a repository must start from that repository, use its Agent Validator configuration and Git state, and produce its own pull request.

OpenSpec 1.6 already supports finding a planning home independently of Git and models stores/worksets separately. Agent Runner must remain compatible with that behavior: the shared OpenSpec root stays at the coordination workspace, while Agent Runner's repository configuration identifies executable Git roots. Agent Runner does not manage OpenSpec stores, worksets, or registries.

## Goals and Non-Goals

### Goals

- Preserve one workspace-owned workflow run across all selected repositories.
- Support both traditional single-repository projects and configured multi-repository workspaces with the existing `change` and `simple-change` workflow families.
- Make workspace-once and repository-for-each execution explicit in workflow YAML.
- Keep every task in the built-in repository implementation phase contiguous, with deterministic sequential fail-fast execution.
- Resolve relative step workdirs from the effective workspace or repository root.
- Persist enough repository identity and progress to resume without replaying completed repositories or silently changing targets.
- Reuse named sessions inherited from workspace scope while isolating named sessions first created in repository scope, captures, and unnamed sessions by repository.
- Represent repositories consistently in audit data, saved and live run views, summaries, metrics, and pull-request links.

### Non-Goals

- Parallel repository execution.
- Cross-repository transactions, rollback, or atomic commits.
- A coordinated commit or single pull request spanning repositories.
- Repository cloning, fetching, branch creation, worktree provisioning, or dependency management.
- Filesystem sandboxing between repositories.
- Managing OpenSpec stores, worksets, or workspace registries.

## Architecture Overview

```text
workspace run
├── workspace context
│   ├── canonical workspace directory
│   ├── configured repository identities
│   ├── ordered selected repositories
│   └── shared state, audit, artifacts, and inherited named sessions
└── ordinary recursive execution
    ├── workspace-scoped body (once)
    └── repository fan-out boundary
        ├── repository a context → complete body
        ├── repository b context → complete body
        └── repository c context → complete body
```

Repository fan-out is a first-class execution boundary around the existing executor. It is not lowered into a synthetic workflow loop and it does not create separate runs. The same boundary supplies the active repository context, recursive state frame, audit prefix, run-view node, and metric roll-up.

## Workspace and Repository Configuration

Repository declarations live only in the project-level `.agent-runner/config.yaml` at the coordination workspace. Global configuration continues to provide agent profiles but cannot declare repositories for a project.

```yaml
repositories:
  backend:
    path: ../backend
  frontend:
    path: ../frontend
```

The mapping key is the stable repository name used by task groups, workflow parameters, state, audit records, prompts, and the run view. Keeping identity separate from the path allows a checkout to move without changing its logical name. Declaration order does not select repositories or determine execution order; the approved task groups do.

At run preparation, a workspace resolver:

1. Requires the launch directory itself to be the canonical coordination Git worktree root and stores it as `workspace_dir`; a scope-aware run outside Git or below the root fails before execution.
2. Resolves every configured path relative to `workspace_dir`, including paths outside it.
3. Resolves symlinks and verifies with Git that the result is exactly a worktree root, not merely a directory inside one.
4. Rejects missing roots, roots equal to the workspace, duplicate canonical roots, the reserved name `default`, names longer than 63 characters, and names outside `[a-z0-9][a-z0-9._-]*`.
5. Stores the canonical name-to-root mapping in the run's workspace context.

When no repositories are declared, the resolver creates one internal repository named `default` at the workspace Git root. That implicit identity is transparent for ordinary audit prefixes, output filenames, and run-view presentation.

## Scope Model and Validation

Add a string-backed scope enum with three internal states:

- omitted: legacy behavior;
- `workspace`: execute once from the coordination workspace;
- `repositories`: execute once for each selected repository unless a repository is already active.

`Workflow` and `Step` gain a `scope` field. Omission must remain distinguishable from `workspace`; otherwise old project workflows would begin adopting new dispatch behavior merely because repository configuration exists.

Validation enforces the approved composition rules:

- A scope-aware workflow declares `scope: workspace` or `scope: repositories`.
- A step may declare scope only when its containing workflow is scope-aware.
- Agent, shell, script, UI, group, and loop steps support a scope override.
- A sub-workflow step cannot declare scope; the referenced child workflow is authoritative.
- A workspace workflow may contain workspace or repository work.
- A repository workflow cannot override a step to workspace scope or invoke a workspace-scoped child.
- A repository-scoped workflow declares a required `repositories` parameter.
- Every embedded orchestration workflow receives an explicit scope. Reusable context-neutral blocks intentionally omit scope so they execute once in the caller's effective workspace or active-repository context. External workflows that omit it retain their existing behavior.

Static composition validation checks resolvable child workflows before execution. Dynamically interpolated child paths receive the same validation when resolved at runtime.

## Repository Selection and Task Groups

The public `repositories` workflow parameter remains a string because Agent Runner's CLI, parameter form, interpolation, sub-workflow arguments, and persisted params are string-based. Its syntax is an ordered comma-separated list:

```text
repositories=backend,frontend
```

Parsing trims separator whitespace and rejects an empty item, duplicate name, unknown name, or implicit substitution of every configured repository. A top-level repository-scoped launch in a configured workspace therefore requires an explicit value. A repository-scoped child also receives the value explicitly through its `params` map, even if the caller currently has an active repository. With no declarations, Runner injects the transparent implicit target internally so existing standalone workflows do not require users to type `default`.

`repositories` is a control parameter, not a built-in. Capture and outcome-capture sinks cannot use that name. Built-in top-level workflows capture the parser result under `affected_repositories` and explicitly bind `repositories: "{{affected_repositories}}"` on every repository-scoped child.

Multi-repository plans keep `tasks.md` as their authoritative human-readable and machine-readable index. They use strict repository headings and group task files beneath the matching repository name:

```markdown
## Repository: backend

- [ ] [Add API](tasks/backend/01-add-api.md)
- [ ] [Add persistence](tasks/backend/02-add-persistence.md)

## Repository: frontend

- [ ] [Build UI](tasks/frontend/01-build-ui.md)
```

One authoritative Go task-group parser owned by Agent Runner and used by OpenSpec and spec-driven workflows:

- reads headings in document order to derive `backend,frontend`;
- requires each repository to appear in one contiguous task group;
- verifies every linked task is under `tasks/<repository-name>/`;
- verifies every task file is non-empty, linked once, and owned by one group;
- records a normalized group snapshot and fingerprint with the resolved selection;
- returns canonical absolute, literal-safe active-repository task paths or a task glob for the existing loop executor.

Traditional implicit full-change plans retain the existing flat `tasks/*.md` shape. Traditional implicit `simple-change` plans retain the existing monolithic `tasks.md` implementation input. A configured workspace, even when only one configured repository is affected, uses strict repository headings and linked files beneath `tasks/<repository>/`; a multi-repository simple change may use only one small file per repository. All three shapes resolve through the same parser, and the internal implicit target is never exposed in user-authored artifacts.

The normalized snapshot is persisted before repository execution. Resume re-parses the current index and task ownership and compares it with the snapshot. A changed repository order, owner, or task set fails resume rather than silently routing work differently.

The repository-scoped middle of `implement-change` retains the existing programmatic task loop:

```yaml
scope: workspace

steps:
  - id: implement-task-groups
    scope: repositories
    steps:
      - id: resolve-repository-tasks
        script: resolve-task-group.sh
        script_inputs:
          change_dir: "{{change_dir}}"
          repository_name: "{{repository_name}}"
        capture: repository_task_glob

      - id: implement-tasks
        loop:
          over: "{{repository_task_glob}}"
          as: task_file
          require_matches: true
        steps:
          - id: implement-single-task
            workflow: implement-task-v1.0.yaml
            params:
              task_file: "{{task_file}}" # canonical workspace-owned absolute path

      - id: review-assumptions
        # repository review

      - id: run-validator
        # repository validation

      - id: prepare-draft-pr
        # repository Git and draft-PR work
```

Repository scope is the outer loop. The current glob loop remains the inner loop and preserves numeric filename order. The complete group body finishes for one repository before the next repository begins.

`validate-planning-artifacts.sh` delegates task-index ownership, link, and shape validation to the same Go parser rather than retaining its current direct-only `tasks/*.md` check. Built-in OpenSpec paths are expressed as `{{workspace_dir}}/openspec/changes/<name>`. Spec-driven change directories pass through an explicit workspace resolver before use. The validator accepts canonical absolute paths and validates their intended workspace-relative identity instead of requiring the old relative string literal.

## Execution Context and Workdir Resolution

Add a shared workspace object to `ExecutionContext` and an optional active repository:

```go
type WorkspaceContext struct {
    Dir          string
    Repositories map[string]Repository
    Selected     []string
}

type Repository struct {
    Name string
    Dir  string
}
```

The exact representation may remain in `internal/model`, but it must not depend on executor or engine packages. Child loop and sub-workflow contexts share the immutable workspace identity and active repository. Control state, audit logger, run directory, engine, profile store, and already-instantiated ancestor named-session bindings remain shared as they are today.

Entering a repository fan-out creates a repository child context that:

- sets `ActiveRepository` to the selected name and canonical root;
- uses the repository root as `ProjectRoot` and default `WorkingDir` for process launch;
- starts with workspace captures plus captures already retained for that repository;
- uses a repository-local namespace for unnamed sessions, captures, evidence, and named sessions first instantiated inside that repository;
- retains the same run directory, audit logger, control channel, profiles, and inherited ancestor named-session bindings.

An active repository suppresses nested fan-out. A repository-scoped workflow or step entered from backend therefore executes once for backend while retaining the complete `repositories` parameter for explicit child calls.

Workdir resolution moves to one shared pre-dispatch helper instead of being reimplemented by each executor:

- relative workspace workdirs resolve from `workspace_dir`;
- relative repository workdirs resolve from `repository_dir`;
- an omitted workdir uses the effective scope root;
- an explicit absolute step workdir remains supported according to existing step semantics;
- generic relative loop globs resolve from the effective scope root;
- shared planning paths are canonicalized to absolute paths before fan-out, while configured artifact globs remain workspace-owned.

Called agents retain the stricter existing starting-directory boundary. Their omitted or relative `workdir` resolves from the parent's effective directory and must remain within the active workspace or repository boundary after symlink resolution. This boundary controls only where the child starts; neither calls nor ordinary scoped execution become filesystem sandboxes.

The built-in variables are supplied from this context:

- `workspace_dir`: always available and immutable for the run;
- `repository_name`: available only with an active repository;
- `repository_dir`: available only with an active repository.
- `repository_output_dir`: available only with an active repository and isolated by explicit repository, while implicit `default` uses the legacy run output directory.

The four names are reserved against workflow params and capture sinks so their canonical values cannot be shadowed. Prevalidation checks availability against each step's effective scope rather than treating repository-only built-ins as globally valid.

## Fan-Out Execution and Failure Semantics

Introduce one dispatcher around workflow and non-sub-workflow step execution:

1. Resolve the effective scope.
2. For legacy or workspace scope, execute the existing body once.
3. For repository scope with an active repository, execute the existing body once in that context.
4. Otherwise parse and validate the complete selected list before starting anything.
5. Persist the selection and fan-out frame.
6. Before mutation, acquire process-lifetime advisory locks for every selected canonical root in sorted canonical-path order; reject the run if another live process owns any lock.
7. For each selected repository in requested order, create its context, emit its boundary event, execute the complete body, persist completion, and then advance.

The dispatcher wraps a complete leaf, group, loop, or workflow body. It does not duplicate executor-specific behavior. Separate repository-scoped leaf steps are separate fan-out boundaries and may revisit repositories; built-ins use one group where an entire repository phase must remain contiguous. `skip_if`, captures, and `outcome_capture` operate inside each repository context. `break_if` and top-level `continue_on_failure` observe the aggregate returned after fan-out. A repository failure ends the current boundary, leaves later repositories pending, and returns one failed aggregate outcome; `continue_on_failure` never means "continue with later repositories."

Repository locks live in an atomic registry beneath Agent Runner's existing user-level state directory and are keyed by a collision-resistant digest of the canonical repository root. Lock metadata records the canonical root, workspace run identity, and owning process ID for diagnostics. Acquisition uses an atomic filesystem primitive; a lock whose recorded process is no longer live is recoverable. The registry is independent of `workspace_dir`, so sibling workspaces selecting the same checkout contend on the same identity.

## Persistence and Resume

State remains in the workspace run directory. Additive state records:

- canonical `workspace_dir`;
- the selected repository names, canonical roots, and order;
- the normalized task-group snapshot/fingerprint when selection came from a built-in plan;
- a repository fan-out frame at each scoped workflow or step boundary;
- per-repository status (`pending`, `active`, `completed`, or failed at the retained active position);
- the current repository's ordinary nested `NestedStepState` chain;
- repository-keyed captures, unnamed sessions, repository-local named sessions, outcomes, evidence references, and pull-request URL.

The fan-out frame is analogous to the current loop iteration frame but carries a stable repository name rather than only an integer cursor. Each frame retains completed siblings and the active child's full existing recursive state, so a failure inside a loop inside a sub-workflow can resume at the same deepest point.

Resume performs identity checks before executor dispatch:

- the launch workspace matches the persisted canonical workspace;
- every selected name still exists;
- every selected name resolves to the same canonical root;
- an explicitly supplied `repositories` value matches the persisted names and order;
- built-in task groups still match the persisted normalized snapshot.

It then skips completed repositories, restores the active repository context and nested child state, and leaves later repositories pending. State without the additive repository fields follows the current legacy resume path, even if the current workspace now declares repositories.

## Captures and Sessions

Workspace captures are copied into a repository context as shared inputs. Repository captures overlay those values only while that repository is active and are merged back only into that repository's runtime namespace. They do not leak to another repository or become workspace variables after fan-out. Existing sub-workflow capture non-propagation remains unchanged; merely putting a sub-workflow inside repository scope does not make its internal captures visible to its parent.

Named sessions use inherited bindings. When `lead-agent` is instantiated in workspace scope before fan-out, every repository context sees and reuses that binding. When the name has no instantiated ancestor binding, its first use creates a repository-local binding; later descendants of that repository reuse it, but sibling repositories create their own. Repository-local bindings are not promoted back to workspace scope. Each invocation starts the CLI from its current effective directory and built-in prompts explicitly state the active repository and workspace.

Unnamed `session: new` and `session: resume` state remains structural and becomes repository-keyed. `session: inherit` may resolve a compatible parent session inside the active repository but never falls back to a workspace session merely because repository-local state is absent. Implementors created for backend cannot be resumed accidentally by frontend. Workflow authors instantiate a named session before fan-out when they want cross-repository conversational continuity.

Repository-scoped automatic process output and workflow-written durable evidence are stored beneath `{{repository_output_dir}}`. The live and historical output readers resolve files through the active repository identity. Workspace acceptance reads those directories explicitly in persisted repository order and writes only aggregate handoffs beneath `{{session_dir}}/output`, preventing fixed filenames and broad report globs from colliding across repositories.

Runner maintains `{{session_dir}}/output/repository-evidence-index.json`, an ordered workspace-readable index whose records contain the repository name and canonical evidence directory. The implicit project record points to the legacy output directory and is marked implicit so presentation can keep it transparent. Aggregate acceptance consumes this index rather than attempting to interpolate the repository-only `repository_output_dir` built-in.

When aggregate acceptance approves implementation remediation, the workspace step writes `{{session_dir}}/output/acceptance-remediation.json`. The document contains an ordered repository-keyed list of approved instructions plus a separate workspace-owned section for shared specification changes. Runner validates repository keys against the persisted selected set before fan-out. Each repository remediation invocation receives only its active repository's entry and performs no implementation change when that entry is absent; it never infers ownership from prose or scans sibling entries. Targeted workspace reacceptance consumes both the remediation ledger and the newly produced repository evidence.

## Audit, Pull Requests, Metrics, and Run Views

Explicit repository execution emits `repository_start` and `repository_end`. Every event produced with an explicit active repository carries `repository_name` and `repository_dir`, and its nesting prefix includes `repo:<name>` at the fan-out boundary. Workspace events omit repository fields. The implicit `default` compatibility path retains legacy prefixes and output basenames and does not expose a repository container.

Extend the run tree with `NodeRepository`. The repository node is a normal container for selection, expansion, drill-in, breadcrumbs, log filtering, status, duration, metrics, and summary roll-up. Tree reconstruction combines persisted selection/state with audit events so repositories that never started after an earlier failure can still appear as pending. The implicit `default` node remains in the data model but is flattened by the presentation layer.

Live auto-follow treats the repository node as another ancestry level. It follows the deepest active child, moves to the next repository when it starts, and preserves existing manual-navigation pause and jump-to-live behavior.

Pull-request capture changes from one run-global last URL to one run-level URL plus a repository-keyed map. Repository recordings are deduplicated only within their repository. Breadcrumbs render all repository URLs in persisted affected order, comma separated, with separate safe hyperlinks. Repository detail shows only that repository's URL. Metrics use the same repository prefix and tree boundary so repository and run totals roll up without double-counting.

## Built-In Workflow Topology

Shipped orchestration workflows receive explicit defaults. Planning, OpenSpec lifecycle, aggregate acceptance, and repository fan-out wrappers use explicit scope. Context-neutral building blocks—including feature-branch validation, single-task implementation, Validator, remediation, and PR finalization—intentionally omit scope and execute once in the caller's effective context. This lets the same file operate on the workspace directly and on each repository when nested beneath a repository-scoped group.

The full change lifecycle becomes:

1. Create and define the change once in the workspace.
2. Plan once, validate strict task groups with the authoritative parser, capture `affected_repositories`, and explicitly pass it as `repositories`.
3. Run shared plan validation once.
4. Enter one repository-scoped implementation group:
   - repository feature-branch, clean-tree, and Validator preflight;
   - existing numbered task-file loop for the active repository only;
   - assumption review and simplification;
   - repository Validator and Git checks;
   - push and prepare that repository's draft pull request.
5. Return to workspace scope, complete the shared task index once, and prepare aggregate acceptance once.
6. Perform interactive evidence review and record approved remediation once at the workspace without directly modifying implementation repositories.
7. Enter a repository-scoped remediation group for all selected repositories; each repository applies only relevant approved fixes, runs its Validator, commits and pushes, and re-verifies its draft PR head. Return to workspace scope for targeted reacceptance.
8. Archive OpenSpec once at the workspace.
9. Finalize the workspace repository's OpenSpec/archive pull request once, then invoke context-neutral final PR/CI handling beneath a repository-scoped group for every selected implementation repository.

The finalization fan-out is necessarily after shared acceptance and archival; it is separate from task implementation but uses the same persisted repository order and fail-fast semantics. It does not rerun task groups.

`simple-change` uses the same boundaries with fewer planning and acceptance steps: compact planning in the applicable parser mode, one contiguous implementation/validation section per repository, one workspace flow-test/review phase that records requested remediation, an optional repository remediation pass, then per-repository PR finalization. This change intentionally adds PR finalization to the single-repository simple lifecycle as well. It does not create an empty workspace PR when the workspace has no tracked change. No multi-repository workflow variants are introduced.

Repository-scoped built-in agent prompts state:

```text
For repository {{repository_name}} at {{repository_dir}}, with shared workspace
{{workspace_dir}}, ...
```

Workspace commit/archive scripts retain their existing Git and Validator behavior against the required workspace repository. Repository Git and Validator operations run independently against the active repository. OpenSpec runs therefore produce one workspace PR plus one PR per affected implementation repository; spec-driven runs omit the workspace PR when they leave it unchanged.

## Decisions and Alternatives

### First-class fan-out rather than synthetic loops

A synthetic loop would reuse iteration machinery but would make repository identity an incidental loop variable. Resume validation, captures, pull requests, audit events, and run-view projection would then need separate side channels. A first-class boundary gives all of them one identity and preserves complete-body-per-repository ordering.

### One workspace run rather than one run per repository

Separate runs would simplify repository-local execution but split the approved plan, named lead session, acceptance evidence, audit history, and resume identity. It would also require a new parent-run coordinator. The workspace run remains the unit the user launched and reviewed.

### Stable configured names rather than deriving names from paths

Path basenames can collide, worktree directory names may be branch-specific, and moving a checkout would otherwise change persisted identity. A keyed configuration keeps the stable name concise while allowing paths to move between new runs.

### Strict `tasks.md` groups rather than a second manifest

The task index is already required and reviewed. Strict headings plus repository directories make ownership both readable and deterministic, while preserving the current file-glob task loop. A separate JSON/YAML manifest would duplicate the index and introduce drift between two planning artifacts.

### Comma-separated params rather than list-typed workflow params

List-typed params would require changes to CLI parsing, forms, interpolation, child binding, validation, and persistence unrelated to repository execution. Repository names already need stable validation, so reserving comma gives a small and readable boundary syntax.

### Inherited named sessions with repository-local fallback

A workspace-instantiated named session represents one declared conversational identity and remains visible to descendants, preserving the full-change lead conversation. A name first encountered inside fan-out cannot safely become sibling-global without making the first repository special, so it is stored in that repository's namespace. This gives standalone repository workflows one named session per repository without leaking it to siblings.

### Context-neutral building blocks rather than duplicate scoped copies

Branch validation, Validator, task implementation, remediation, and PR finalization operate on exactly one effective checkout. Leaving those small reusable workflows intentionally unscoped preserves their current standalone behavior and lets an explicit repository-scoped parent supply fan-out. Giving each a fixed workspace or repository default would make existing call sites mutually incompatible; cloning them would create the maintenance matrix this change is intended to avoid.

## Risks and Mitigations

- **Recursive state becomes more complex.** Keep repository fan-out as one additive frame around the existing nested state rather than replacing loop/sub-workflow state with a new general scheduler. Cover failures at every nesting depth.
- **A resumed run could target a changed checkout.** Persist canonical roots and task-group snapshots and reject identity or ownership drift before spawning work.
- **Repository captures, evidence, or sessions could leak.** Construct repository contexts from workspace state plus one repository namespace, isolate directly named files beneath `repository_output_dir`, and test identical capture, evidence, and session names across repositories.
- **Run-view reconstruction could disagree with execution.** Use persisted affected order as authority and audit events as lifecycle evidence; do not infer order from event replay or configuration maps.
- **Long repository names consume sidebar width.** Render only the configured name and reuse existing truncation behavior; never prefix it with `repo` or append the path.
- **An inherited named agent may remember the prior repository.** Launch each invocation from the active repository, require explicit repository/workspace prompt context, and exercise cross-directory resume through an actual supported agent in acceptance testing.
- **Task artifacts may be edited after execution begins.** Persist a normalized task-group snapshot and reject drift on resume.
- **Workspace and implementation Git lifecycles could be conflated.** Preserve workspace checks, commits, archive, and PR finalization while running repository preflight, Validator, commits, and PR handling only in the active implementation repository.
- **Two runs could mutate the same checkout.** Acquire all selected repository locks in canonical-path order before mutation and hold them for the process lifetime; stale PID ownership is recoverable after crashes.

## Migration and Compatibility

- State files lacking repository fields use the legacy resume path.
- External workflows lacking scope execute once with their existing roots and do not fan out.
- Existing single-Git-repository projects receive the implicit `default` repository; its visual container is flattened.
- Existing flat single-repository task plans remain valid.
- Embedded orchestration workflows are migrated together to explicit scope declarations; context-neutral building blocks remain intentionally unscoped. Repository-scoped children receive required params, prompts identify active context, evidence paths are isolated, and workspace/repository PR handling remains independent.
- Configuration is additive. A project can adopt multi-repository behavior by adding the `repositories` mapping without creating new workflow files.

## Verification Strategy

- Model/loader tests for scope omission, valid and invalid scope combinations, required `repositories`, reserved built-ins, and composition rules.
- Configuration tests for required workspace Git roots, ignored nested/sibling roots, Git-root equality, symlinks, duplicate names/roots, reserved or invalid names, and implicit repository creation.
- Task-group parser tests for ordering, grouped directories, flat implicit plans, missing ownership, duplicates, unknown names, unsafe links, and fingerprint drift.
- Executor tests for leaf, group, loop, workflow, and nested repository fan-out; active-repository suppression; fail-fast behavior; relative/absolute workdirs; and called-agent starting boundaries.
- State/resume tests for completed, active, and pending repositories; deep loop/sub-workflow positions; configuration drift; plan drift; legacy state; and repeated fan-out later in one workflow.
- Capture/session tests proving workspace-to-repository visibility, repository isolation, no reverse aggregation, inherited workspace named sessions, repository-local named and unnamed sessions, and repository evidence-path isolation.
- Audit and TUI tests for repository prefixes, explicit fields, pending nodes, live transitions, auto-follow, drill-in, summaries, metrics, implicit flattening, and ordered PR links.
- Built-in workflow validation proving every orchestration workflow declares scope, approved context-neutral building blocks remain single-execution, and every configured-workspace repository child receives `repositories`.
- End-to-end fixture launched from a Git-backed workspace with shared OpenSpec artifacts and ignored nested or sibling Git repositories. It must plan and commit at the workspace, modify and validate selected repositories using their own Validator configs, retain run artifacts at the workspace, resume correctly, and finalize the workspace and implementation pull requests independently.

## Open Questions

None. Parallel execution and coordinated cross-repository delivery remain explicit future work.
