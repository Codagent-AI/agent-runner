## Why

Baton invokes flokay skills directly (e.g., `/flokay:propose`), bypassing the openspec instructions layer. The flokay skills expect template content, output paths, and dependency info to be "provided" to them — language like "to the outputPath provided, using the template structure provided." When `opsx:continue` orchestrates, it calls `openspec instructions` first and injects all of this. When baton orchestrates, none of it is provided. It works by accident (Claude searches the project and finds the template), but it's fragile and lossy.

Additionally, when resuming a partially-completed workflow, baton requires the user to pass `--from <step-id>` manually. A persistent state file enables `baton resume` to pick up where it left off.

## What Changes

- Add an **engine** abstraction to baton that pluggable modules can implement
- Implement an **openspec engine** that calls `openspec status` and `openspec instructions` to enrich baton's workflow execution
- Engine **enriches prompts** by prepending template content, output path, and dependency info before passing to Claude
- Engine **validates step completion** by checking that the expected artifact file exists after each step finishes
- Engine **controls state file location** so the openspec engine can place it in the change directory
- Workflow files gain an `engine` block; step IDs serve as the mapping key to engine-managed artifact IDs
- Baton persists workflow state to a file, enabling `baton resume` for any workflow

## Capabilities

### New Capabilities

- `engine-interface`: The pluggable engine abstraction — interface definition, engine loading from workflow config, integration into the runner lifecycle (enrich prompt, validate step, validate workflow, state dir), state file persistence, and `baton resume` command
- `openspec-engine`: The openspec-specific engine implementation — calls `openspec status` and `openspec instructions` CLI commands, parses JSON output, maps artifact IDs to workflow steps by matching step IDs, enriches prompts with template/outputPath/dependencies, validates artifact completion via openspec status
- `workflow-engine-config`: Schema and workflow file changes — `engine` block on workflows, step ID convention for engine matching, validation that engine-managed entities have corresponding workflow steps

### Modified Capabilities

_(none — baton has no existing specs)_

## Impact

- **`src/schema.ts`** — Add `engine` block to workflow schema
- **`src/runner.ts`** — Call engine methods during workflow execution (enrich prompt, validate step)
- **New `src/engine.ts`** — Engine interface definition and loader
- **New `src/state.ts`** — State file read/write/delete
- **New `src/engines/openspec.ts`** — OpenSpec engine implementation
- **New `src/commands/`** — Commander-based CLI with one file per command
- **`workflows/flokay.yaml`** — Add engine config, rename step IDs to match artifact IDs
- **Runtime dependency** — Requires `openspec` CLI to be installed when using the openspec engine
