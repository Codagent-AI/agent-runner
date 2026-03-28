## Why

Running a workflow today requires typing the full relative path including the directory and extension (`baton run workflows/flokay.yaml`). Since all workflows live in `workflows/` and use the `.yaml` extension, this is unnecessary friction. Users should be able to run `baton run flokay` and have the tool resolve the rest.

## What Changes

- The `run` command gains workflow name resolution: if the argument is not a direct file path, resolve it as `workflows/<name>.yaml`
- Direct file paths (relative or absolute) continue to work as before — resolution is a fallback, not a replacement

## Capabilities

### New Capabilities
- `workflow-name-resolution`: Resolve a bare workflow name to a file path by searching the `workflows/` directory with the `.yaml` extension

### Modified Capabilities

## Impact

- `src/commands/run.ts` — argument handling before `loadWorkflow()` call
- `src/loader.ts` — potentially, if resolution logic lives closer to loading
