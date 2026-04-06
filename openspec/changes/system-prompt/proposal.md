# Proposal: System Prompt Support for CLI Adapters

## Why

Engine enrichment (workflow context, artifact instructions, dependency content) is currently passed as part of the user-visible prompt. This clutters the interactive session with long preambles the user didn't write and doesn't need to see. Claude Code supports `--append-system-prompt` to deliver instructions invisibly as a system prompt; we should use it where available.

## What Changes

- Add a `SystemPrompt` field to `BuildArgsInput` so adapters can receive enrichment separately from the user prompt.
- **Claude adapter**: when `SystemPrompt` is non-empty, pass it via `--append-system-prompt` instead of concatenating it into the positional prompt argument.
- **Codex adapter**: no change — Codex does not support a system prompt flag, so enrichment continues to be concatenated into the user-visible prompt.
- `buildAgentPrompt` splits its return into user prompt and enrichment, routing enrichment to `SystemPrompt` instead of concatenating unconditionally.

## Capabilities

### New Capabilities

- `system-prompt-delivery` — Adapter-level support for passing engine enrichment as a hidden system prompt when the underlying CLI supports it.

### Modified Capabilities

- `cli-adapter` — `BuildArgsInput` gains a `SystemPrompt` field; adapters that support it use it, others ignore it.
- `engine-interface` — Enrichment is no longer unconditionally concatenated into the prompt; it is returned separately and routed to the adapter's system prompt channel.

## Impact

- **`internal/cli/adapter.go`**: `BuildArgsInput` struct gains `SystemPrompt string` field.
- **`internal/cli/claude.go`**: `BuildArgs` emits `--append-system-prompt <text>` when system prompt is provided.
- **`internal/cli/codex.go`**: No change (falls back to prompt concatenation).
- **`internal/exec/agent.go`**: `buildAgentPrompt` returns prompt and enrichment separately; caller routes enrichment to `SystemPrompt` on the adapter input.
- **Existing tests**: Adapter tests need updated expectations for the new flag; `buildAgentPrompt` tests need to verify separation.
