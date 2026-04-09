# Task: System Prompt Routing

## Goal

Add system prompt support to the CLI adapter framework so that prompt content is delivered as a hidden system prompt in interactive mode (for adapters that support it) or wrapped in `<system>` XML tags (for adapters that don't), while keeping headless mode unchanged.

## Background

You MUST read these files before starting:
- `openspec/changes/system-prompt/design.md` for the full design and approach
- `openspec/changes/system-prompt/specs/system-prompt-delivery/spec.md` for routing behavior acceptance criteria
- `openspec/changes/system-prompt/specs/cli-adapter/spec.md` for adapter interface and arg construction acceptance criteria
- `openspec/changes/system-prompt/specs/engine-interface/spec.md` for enrichment separation acceptance criteria

**Current state:** `buildAgentPrompt` in `internal/exec/agent.go` concatenates the step prompt and engine enrichment into a single string (`prompt = prompt + "\n\n" + enrichment`), which is passed as the positional CLI argument via `BuildArgsInput.Prompt`. In interactive mode, this appears as the first user message, cluttering the session.

**What changes:**

1. **Adapter interface** (`internal/cli/adapter.go`): Add `SupportsSystemPrompt() bool` to the `Adapter` interface. Add `SystemPrompt string` field to `BuildArgsInput`.

2. **Claude adapter** (`internal/cli/claude.go`): `SupportsSystemPrompt()` returns `true`. `BuildArgs` emits `--append-system-prompt <content>` when `SystemPrompt` is non-empty.

3. **Codex adapter** (`internal/cli/codex.go`): `SupportsSystemPrompt()` returns `false`. No other changes — it ignores the `SystemPrompt` field.

4. **Routing logic** (`internal/exec/agent.go`): `buildAgentPrompt` stops concatenating — returns step prompt and enrichment as separate strings (signature stays `(prompt, enrichment string, err error)` but `prompt` no longer includes enrichment). At the `BuildArgsInput` construction site (~line 51), add inline routing:
   - If headless → concatenate step prompt + enrichment into `Prompt` (current behavior)
   - If interactive + adapter supports system prompt → set `SystemPrompt` to full content (step prompt + enrichment), leave `Prompt` empty
   - If interactive + adapter doesn't support → wrap full content in `<system>` XML tags, set as `Prompt`

**Constraints:**
- The `emitAgentStart` call on the line after `BuildArgs` uses both `prompt` and `enrichment` for audit logging — make sure audit logging still receives the appropriate values.
- Existing tests for `BuildArgs` in both adapters will need updated expectations for the new interface method and the system prompt flag behavior.

## Done When

All spec scenarios from the four spec files are covered by tests and passing. The `Adapter` interface compiles with the new method, both adapters implement it, and the routing logic correctly dispatches based on mode and adapter support.
