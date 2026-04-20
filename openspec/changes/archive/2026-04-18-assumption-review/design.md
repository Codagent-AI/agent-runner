## Context

Today, agent-runner offers three session-resolution modes:

- `session: new` — start a fresh session.
- `session: resume` — resume the most recent session in the same workflow file.
- `session: inherit` — resume the parent workflow's most recent session.

All three are scoped by file path or by the parent chain. The motivating case for this change is the OpenSpec workflow: `plan-change.yaml` creates a planner session, and the sibling sub-workflow `implement-change.yaml` (later) needs the same planner session to review implementor assumptions. Sibling sub-workflows have no parent–child relationship to each other, so neither `inherit` nor `resume` reaches.

## Goals / Non-Goals

**Goals:**
- A role-keyed reference: `session: planner` means "the planner session" wherever it appears in the composition tree.
- Sub-workflow files remain independently runnable and testable.
- Validation catches unresolved references, agent conflicts, and incompatible cross-file declarations before any agent step runs.
- Resume across runner restarts preserves named-session identities.

**Non-Goals:**
- Per-iteration isolation in loops — iterations share the named session by design.
- Garbage collection of orphaned named sessions when a declaration is removed.
- A CLI surface to list or inspect named sessions.
- Migrating the existing OpenSpec workflows to use named sessions (follow-up).

## Approach

### Declaration

Workflow files gain a top-level `sessions:` array. Each entry is a `{name, agent}` pair. Each file declares the sessions *it* uses, so files are self-contained for standalone runs.

### Reference

A step uses `session: <name>`, where `<name>` matches a declaration in the composition tree. The agent profile comes from the declaration; the step MUST NOT also set `agent:`.

### Composition: compatible-merge

When a root invokes sub-workflows, declarations are collected across the tree. Same-name entries are checked for `agent` equality. Compatible duplicates merge silently; incompatible duplicates fail validation.

### Resolution

A new `NamedSessions map[string]string` (name → session ID) lives on `ExecutionContext`. On first reference, the runner starts a session via the agent's CLI, captures the ID, and stores it. On subsequent references — anywhere in the composition tree — the runner passes the stored ID to the CLI as a resume.

The map is propagated to children alongside `_seed` (`internal/model/context.go:133`, `:189`). Writes from a child are visible to the parent so later siblings can reuse a session created earlier.

### Persistence

`RunState` gains a `NamedSessions` field. The map is flushed via the existing `FlushState` hook after any step that mutates it. On `--resume`, the map is restored before any step runs.

### Validation walk

`internal/validate/workflow.go` performs:

- **Per-file (always):** reserved names, duplicate names within one file, agent conflict on each step.
- **Composition (when validating from a root):** walk every reachable sub-workflow, collect all declarations, check cross-file compatibility, verify every `session: <name>` reference resolves.
- **Standalone:** enforce only local checks plus reference-resolves-against-local-declarations. References that depend on a root's declarations therefore fail standalone — by design.

## Decisions

### Compatible-merge instead of forbid-shadowing

An earlier draft proposed "redeclaration in a nested workflow that inherited it = validate error." That rule couples each sub-workflow to one specific root: `plan-change.yaml` could not declare `planner` because the root already does, leaving the sub-workflow non-runnable on its own.

We invert it: any workflow may declare any name; same-name declarations across files are allowed as long as their `agent` matches, and only incompatible declarations (same name, different agent) fail. Sub-workflows stay self-contained for standalone runs while the integrity guarantee — no two incompatible declarations of the same name — is preserved.

### Loops share the named session

For-each and other loop steps run all iterations against the same named session. The natural reading of "the planner" is singular; per-iteration isolation already has a clear escape hatch (`session: new`). This avoids inventing iteration-suffixed naming and keeps the named-session map small and stable.

### Trust persisted ID over current declaration on drift

If the declaration's `agent` changes between runs, the persisted session ID still belongs to the original agent's CLI. Auto-recreation would silently strand the user's prior work. Warn loudly and continue with the persisted ID.

### Reserved names

`new`, `resume`, and `inherit` are existing session-strategy keywords. A declaration using one of those names would create parser-level ambiguity. Reject at validate time.

## Risks / Trade-offs

- **Standalone validation cannot catch every reference error.** A reference declared only at the root passes when validated under the root and fails standalone. CI should validate from the root to catch every reference.
- **Map mutations require careful flushing.** A step that creates a named session must persist the new entry before the step is considered complete, or a mid-step crash leaves the user resumable but with the named session untracked.
- **Loop sharing is opinionated.** Workflows that genuinely want per-iteration named sessions cannot express that; they must drop down to `session: new`.
- **Cross-root references can succeed at runtime even when standalone validation would fail.** If a developer skips validation and runs a sub-workflow standalone whose reference is satisfied only by a root, the runner fails at execution with a less helpful error than the validator's. Document this in the user guide.
