## Why

Multi-stage agent workflows need to share one logical agent session across sub-workflow boundaries — the planner that drafts a change should be the same Claude session that later reviews implementor assumptions in a sibling sub-workflow. Today, `session: inherit` and `session: resume` are scoped by file path or parent chain; there is no role-keyed mechanism that survives file boundaries or step renames.

## What Changes

- New top-level `sessions:` block on workflow files, declaring named agent sessions by role (`name` + `agent`).
- New `session: <name>` step value: first reference creates the session under that name; later references resume it.
- Each workflow file MAY declare the named sessions it uses; when files are composed under a root, declarations merge by name with same-agent compatibility (incompatible declarations fail validation).
- `NamedSessions map[string]string` propagated through `ExecutionContext` alongside `_seed`, and persisted in `RunState` for `--resume`.
- New validate rules: reserved names (`new`, `resume`, `inherit`), agent-conflict on the same step, unresolved references, incompatible cross-file declarations.
- Existing `session: new`, `session: resume`, `session: inherit`, and bare `agent:` semantics are unchanged.

## Capabilities

### New Capabilities
- `named-sessions`: role-keyed agent sessions creatable and resumable across sub-workflow boundaries.

### Modified Capabilities
<!-- none — existing inherit/resume semantics unchanged -->

## Out of Scope

- The `review-assumptions` step in `workflows/openspec/implement-change.yaml` (follow-up change).
- Any update to the `session-report` skill or its marker syntax (lives in `agent-skills` repo).
- Migrating `workflows/openspec/change.yaml`, `plan-change.yaml`, `implement-change.yaml` to consume named sessions (follow-up).
- CLI surface for listing or inspecting named sessions in `view-run`.
- Garbage collection of orphaned named-session entries when declarations are removed.

## Impact

- `internal/model/workflow.go` — add `Sessions []SessionDecl` to the workflow model.
- `internal/model/context.go` — add `NamedSessions map[string]string`; propagate in `NewLoopIterationContext` and `NewSubWorkflowContext` alongside `_seed`.
- `internal/model/state.go` — persist `NamedSessions` on `RunState`; restore on `--resume`.
- `internal/model/step.go` — relax the session-strategy validator to accept names; reject `session: <name>` paired with `agent:`.
- `internal/session/session.go` — add `ResolveNamedSession`; write back on creation.
- `internal/loader/` — parse top-level `sessions:`.
- `internal/validate/workflow.go` — enforce reserved-names, duplicate-name, agent-conflict, unresolved-reference, and cross-file compatibility rules.
- `internal/exec/agent.go` — dispatch on `session: <name>`.
- `internal/exec/subworkflow.go` — ensure child writes are visible to parent's named-session map.
- No CLI flag changes.
