# Task: Workflow Name Resolution

## Goal

Add a `resolveWorkflowName()` helper to `src/loader.ts` that validates bare workflow names and resolves them to file paths, then wire it into both the `run` and `validate` commands so users can type `baton run flokay` instead of `baton run workflows/flokay.yaml`.

## Background

You MUST read these files before starting:
- `design.md` for full design details and decisions
- `specs/workflow-name-resolution/spec.md` for all acceptance criteria (bare name validation and file resolution scenarios)

Currently, `src/commands/run.ts:25` and `src/commands/validate.ts:57` pass the CLI argument directly to `loadWorkflow(file)` with no resolution. The design calls for a shared `resolveWorkflowName(name: string): string` function in `src/loader.ts` that:

1. Validates the argument against `^[a-zA-Z0-9_-]+$` — throws if it contains `/`, `.`, or other invalid characters
2. Tries `resolve('workflows', `${name}.yaml`)` — returns if file exists
3. Tries `resolve('workflows', `${name}.yml`)` — returns if file exists
4. Throws `Workflow '${name}' not found` if neither exists

Both `run.ts` and `validate.ts` call `resolveWorkflowName()` before `loadWorkflow()`. The `loadWorkflow()` function itself is not modified — it continues to accept a file path.

Key files to modify:
- `src/loader.ts` — add `resolveWorkflowName()` (use `existsSync` from `node:fs` and `resolve` from `node:path`)
- `src/commands/run.ts` — call `resolveWorkflowName(file)` before `loadWorkflow()` (line 25) and before `resolve(file)` in the `workflowFile` option (line 44)
- `src/commands/validate.ts` — call `resolveWorkflowName(file)` before `loadWorkflow()` (line 57)

This is a deliberate breaking change — existing usage like `baton run workflows/flokay.yaml` will error. The argument description in both commands should change from "Path to workflow YAML file" to "Workflow name" or similar.

## Done When

All spec scenarios pass review. `baton run flokay` resolves and loads `workflows/flokay.yaml`. Invalid names (containing `/` or `.`) are rejected with a clear error. Missing workflows produce a "not found" error.
