## Context

Baton is a small CLI workflow orchestrator (4 files, ~300 lines) that runs multi-step workflows by spawning Claude Code sessions. It currently has no extension points — step execution, state management, and prompt construction are all inline in the runner. The engine abstraction adds optional hooks at key points in the runner loop without changing the core execution model.

## Goals / Non-Goals

**Goals:**
- Engines can enrich prompts with external context (templates, output paths, dependencies)
- Engines can validate steps produced expected output
- Engines can validate workflow structure at load time
- Engines can control where the state file lives
- State file enables `baton resume` for any workflow
- Openspec engine is the first (and currently only) implementation

**Non-Goals:**
- Engine control over step execution order (engine is hooks, not an orchestrator)
- Plugin/package-based engine discovery (hardcoded registry is sufficient)
- Project-level context/rules injection (use CLAUDE.md instead)

## Decisions

### 1. Engine as optional hook bag, not orchestrator

The engine is a plain object with optional methods. The runner owns the loop and calls hooks at defined points. This keeps the runner simple and testable — an engine is just dependency injection.

```typescript
interface Engine {
  getStateDir?(params: Record<string, string>): string;
  validateWorkflow?(workflow: Workflow): void;  // throws on failure
  enrichPrompt?(stepId: string, params: Record<string, string>): Promise<string | undefined>;
  validateStep?(stepId: string, params: Record<string, string>): Promise<boolean>;
}
```

All methods optional. `enrichPrompt` returns `undefined` for step IDs it doesn't manage. `validateStep` returns `true` for step IDs it doesn't manage.

### 2. Hardcoded engine registry

A simple map in `src/engine.ts`:

```typescript
import { OpenSpecEngine } from './engines/openspec.ts';

const registry: Record<string, new (config: Record<string, unknown>) => Engine> = {
  openspec: OpenSpecEngine,
};

export function createEngine(engineBlock: { type: string; [k: string]: unknown }): Engine {
  const Ctor = registry[engineBlock.type];
  if (!Ctor) throw new Error(`Unknown engine type: "${engineBlock.type}"`);
  const { type, ...config } = engineBlock;
  return new Ctor(config);
}
```

### 3. State file replaces the old write-only state

The current `saveState` / `STATE_FILE` code is removed. New state file:

```typescript
interface RunState {
  workflowFile: string;       // path to workflow YAML
  workflowName: string;       // for display
  currentStep: string;        // step ID (not index)
  sessionIds: Record<string, string>;
  params: Record<string, string>;
  workflowHash: string;       // hash of workflow file contents for change detection
}
```

State file is named `baton-state.json` (visible, not hidden). Location: engine's `getStateDir(params)` or project root.

### 4. Step ID = artifact ID convention

No `artifact` field on steps. The openspec engine loads its schema's artifact IDs at init time and uses them as a set to determine which step IDs it manages. When `enrichPrompt("design", params)` is called, it checks if `"design"` is in its artifact set, and if so calls `openspec instructions design --change "<name>" --json`.

### 5. Openspec engine shells out to the CLI

The engine calls `openspec` via `Bun.spawn` (or equivalent). No programmatic dependency on the openspec package. This keeps baton decoupled — the openspec CLI is a runtime dependency, not a build dependency. The engine validates the CLI exists at init time by running `which openspec`.

### 6. Enrichment format

The engine prepends an `<artifact_context>` block to the prompt:

```xml
<artifact_context>
<output_path>/absolute/path/to/proposal.md</output_path>
<dependencies>
- /absolute/path/to/proposal.md (Initial proposal document)
</dependencies>
<template>
...full template content...
</template>
</artifact_context>
```

The `instruction` field from openspec is excluded — the prompt already invokes the skill.

### 7. Validation failure UX

When `validateStep` returns false, the runner prompts via stdin:

```
baton: artifact "design" was not created.
  [r] Resume previous session (interactive)
  [q] Exit workflow
```

On "r", the runner spawns `claude --resume <sessionId>` in interactive mode, then re-validates after that session ends. On "q", the runner exits (state file persists for later `baton resume`).

### 8. `baton resume` CLI command

New subcommand alongside `baton run` and `baton validate`:

```
baton resume <state-file-path>
```

Loads the state file, re-loads the workflow from `workflowFile`, compares `workflowHash` to detect changes (warn if different), resolves `currentStep` ID to an index, and runs from there with the persisted `sessionIds` and `params`.

### 9. Use commander for CLI parsing

Adopt the `commander` npm package, consistent with agent-gauntlet. One file per command in `src/commands/`.

File structure:

```
src/
  index.ts              # CLI entry — creates Commander program, registers commands
  schema.ts             # Zod types (workflow, step, param, engine block)
  loader.ts             # YAML loading, param interpolation
  engine.ts             # Engine interface, registry, createEngine()
  state.ts              # State file read/write/delete
  runner.ts             # Core step execution loop (receives engine as dependency)
  engines/
    openspec.ts         # OpenSpec engine implementation
  commands/
    index.ts            # Re-exports all register functions
    run.ts              # baton run <workflow.yaml> [params] [--from step]
    resume.ts           # baton resume <state-file-path>
    validate.ts         # baton validate <workflow.yaml>
```

## Risks / Trade-offs

- **Openspec CLI as runtime dependency** — If `openspec` isn't installed, the openspec engine fails at init. This is by design (fail fast with clear error), but it means baton can't be distributed as a standalone tool for openspec workflows without also installing openspec.
- **Step ID = artifact ID coupling** — Renaming an openspec artifact requires renaming the workflow step. This is intentional (eliminates a mapping layer) but means workflow authors need to know the schema's artifact IDs.
- **Shell-out to CLI vs programmatic API** — Shelling out to `openspec` is slower than a direct import but keeps baton decoupled. If performance becomes an issue, the engine could be swapped to use openspec's Node API. The engine interface doesn't change.
- **State file in change dir** — If the change dir is deleted or moved, the state file is lost. This is acceptable since the change dir is the canonical location for the change's lifecycle data.

## Migration Plan

1. Add `commander` dependency
2. Restructure `src/` to the new file layout (commands/, engines/)
3. Add engine interface, registry, and openspec engine
4. Refactor runner to accept engine and call hooks
5. Replace hand-rolled arg parsing with commander commands
6. Add state file persistence and `baton resume` command
7. Remove old `.baton-state.json` write-only code
8. Update `workflows/flokay.yaml` — add engine block, rename `propose` step to `proposal`

## Open Questions

None — all architectural decisions are settled.
