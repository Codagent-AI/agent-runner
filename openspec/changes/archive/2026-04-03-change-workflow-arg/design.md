## Context

The `baton run` and `baton validate` commands currently accept a file path argument (e.g., `workflows/flokay.yaml`) which is passed directly to `loadWorkflow()` and resolved via `readFileSync`. There is no validation or name-based resolution — users must know and type the full relative path including directory and extension.

The goal is to let users type `baton run flokay` instead of `baton run workflows/flokay.yaml`.

## Goals / Non-Goals

**Goals:**
- Accept bare workflow names (e.g., `flokay`, `plan-change`) and resolve them to files in `workflows/`
- Validate that the argument is a bare name — reject paths and file extensions
- Support both `.yaml` and `.yml` extensions with `.yaml` preferred
- Apply consistently to both `run` and `validate` commands

**Non-Goals:**
- Backward compatibility with file path arguments — this is a deliberate breaking change
- Project root detection (walking up directories to find `workflows/`) — resolve relative to cwd
- Changes to sub-workflow resolution in `sub-workflow.ts`
- Changes to `loadWorkflow()` internals

## Decisions

**Shared helper function in `loader.ts`**: A `resolveWorkflowName(name: string): string` function handles validation and resolution. Both `run.ts` and `validate.ts` call it before `loadWorkflow()`. This keeps the logic DRY and independently testable without muddying `loadWorkflow`'s single responsibility of parsing and validating YAML.

**Strict bare-name enforcement**: Arguments containing `/` or `.` are rejected with an error. No fallback to file path resolution. Users must use bare names only. This is a breaking change but the migration is trivial (drop the path prefix and extension).

**Resolution relative to cwd**: The resolver uses `resolve('workflows', ...)` which resolves relative to the current working directory. This matches current behavior — users already run `baton` from the project root.

**Resolution order**: Try `.yaml` first, then `.yml`. Error if neither exists. Use `existsSync` to check file existence before returning the path.

**Flow**:
1. Validate argument against `^[a-zA-Z0-9_-]+$` — throw if invalid
2. Try `resolve('workflows', `${name}.yaml`)` — return if exists
3. Try `resolve('workflows', `${name}.yml`)` — return if exists
4. Throw: `Workflow '${name}' not found`

## Risks / Trade-offs

**Breaking change**: Existing usage like `baton run workflows/flokay.yaml` will error. This is acceptable because baton is an internal tool, the migration path is obvious, and the error message will make the fix clear.

**No project root detection**: Users must run `baton` from the directory containing `workflows/`. This matches current behavior and avoids unnecessary complexity. Can be added later if needed.

## Migration Plan

No phased rollout needed. After implementation:
1. Update any scripts or documentation that use file path arguments to use bare names
2. The error message on invalid names will guide users to the correct syntax

## Open Questions

None — all architectural decisions are settled.
