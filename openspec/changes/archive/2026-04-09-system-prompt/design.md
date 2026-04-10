## Context

Today, `buildAgentPrompt` in `internal/exec/agent.go` concatenates the step prompt and engine enrichment into a single string, passed as the positional CLI argument. In interactive mode, this appears as the first user message — cluttering the session with long instructions the user didn't write. Claude Code supports `--append-system-prompt` to deliver content invisibly as a system prompt; Codex does not.

The specs define three routing modes:
- **Interactive + native support**: full content via system prompt mechanism, no positional arg
- **Interactive + no support**: full content wrapped in `<system>` XML tags as positional arg
- **Headless**: concatenated into positional arg unchanged (current behavior)

## Goals / Non-Goals

**Goals:**
- Route prompt content as a system prompt in interactive mode for adapters that support it
- Provide a structured XML fallback for adapters that don't
- Keep headless mode behavior unchanged

**Non-Goals:**
- Changing the engine's `enrichPrompt` interface (it still returns a string)
- Supporting per-step opt-in/opt-out of system prompt routing
- Changing headless mode behavior

## Approach

Three changes, all localized:

### 1. Adapter interface additions (`internal/cli/adapter.go`)

Add `SupportsSystemPrompt() bool` to the `Adapter` interface. Add `SystemPrompt string` to `BuildArgsInput`.

```go
type Adapter interface {
    BuildArgs(input BuildArgsInput) []string
    DiscoverSessionID(opts DiscoverOptions) string
    SupportsSystemPrompt() bool
}

type BuildArgsInput struct {
    Prompt       string
    SystemPrompt string
    SessionID    string
    Resume       bool
    Model        string
    Headless     bool
}
```

### 2. Adapter implementations

**Claude (`internal/cli/claude.go`):**
- `SupportsSystemPrompt()` returns `true`
- `BuildArgs` emits `--append-system-prompt <content>` when `SystemPrompt` is non-empty

**Codex (`internal/cli/codex.go`):**
- `SupportsSystemPrompt()` returns `false`
- `BuildArgs` ignores the `SystemPrompt` field

### 3. Routing logic in `internal/exec/agent.go`

**`buildAgentPrompt` stops concatenating.** Currently it does `prompt = prompt + "\n\n" + enrichment`. Change: return step prompt and enrichment as separate strings. The function signature stays `(prompt, enrichment string, err error)` but `prompt` no longer includes enrichment.

**Inline routing at the `BuildArgsInput` construction site** (~line 51):

```go
stepPrompt, enrichment, err := buildAgentPrompt(step, ctx)
// ...
fullPrompt := stepPrompt
if enrichment != "" {
    fullPrompt = stepPrompt + "\n\n" + enrichment
}

input := cli.BuildArgsInput{
    SessionID: sessionID,
    Resume:    isResume,
    Model:     step.Model,
    Headless:  headless,
}

if headless {
    input.Prompt = fullPrompt
} else if adapter.SupportsSystemPrompt() {
    input.SystemPrompt = fullPrompt
} else {
    input.Prompt = "<system>\n" + fullPrompt + "\n</system>"
}
```

## Decisions

| Decision | Rationale |
|----------|-----------|
| `SupportsSystemPrompt() bool` method over returning the flag name | Keeps CLI-specific details (flag names) inside the adapter. The caller only needs a yes/no answer. |
| Inline routing over a helper function | Single call site, ~10 lines of straightforward if/else. A helper would be premature abstraction. |
| `buildAgentPrompt` keeps its signature, stops concatenating | Minimal change. The caller takes over composition, which it needs to do for routing anyway. |
| `<system>` XML tag for fallback wrapping | Common LLM prompting convention for structural delimiters. No closing rationale needed for the tag name — it's descriptive and unambiguous. |

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| Codex sessions now get `<system>` XML wrapping they didn't have before | LLMs treat XML tags as structural delimiters. Verify manually that Codex behavior is unaffected. |
| Claude interactive sessions start with no positional arg (just system prompt) | Claude Code supports this — sessions begin with system prompt context and wait for user interaction. |

## Open Questions

None — all deferred-to-design items resolved.
