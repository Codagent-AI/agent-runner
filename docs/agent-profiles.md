---
title: Agent Profiles
group: Guides
order: 4
description: Named profiles that set an agent step's mode, CLI, model, and effort.
---

# Agent Profiles

Agent steps run against named profiles. A profile defines the default mode, CLI adapter, model, effort, and an optional system prompt, so a workflow step just names the role it wants instead of repeating engine configuration.

## Built-In Defaults

Agent Runner ships a `default` profile. You can use it as-is, or override any part of it from your own config.

```yaml
profiles:
  default:
    agents:
      interactive_base:
        default_mode: interactive
        cli: claude
        model: opus
        effort: high
      autonomous_base:
        default_mode: autonomous
        cli: claude
        model: opus
        effort: high
      planner:
        extends: interactive_base
      implementor:
        extends: autonomous_base
      summarizer:
        default_mode: autonomous
        cli: claude
        model: haiku
        effort: low
```

Supported CLI adapters are `claude`, `codex`, `copilot`, `cursor`, and `opencode`. A step-level `mode`, `cli`, or `model` overrides the resolved profile for that one step.

## How Config Layers

Configuration resolves in three passes, each overriding the one before it:

| Order | Source |
| --- | --- |
| 1 | Built-in defaults shipped with Agent Runner |
| 2 | Global config at `~/.agent-runner/config.yaml` |
| 3 | Project config at `.agent-runner/config.yaml` |

Project config wins over global config.

> [!NOTE]
> Only **project** config may set `active_profile`. Global config cannot. This keeps a machine-wide default from silently changing which profile a project runs under.

## User Settings

User settings live in `~/.agent-runner/settings.yaml`. The settings editor in the TUI writes the user-facing preferences:

| Setting | Values |
| --- | --- |
| `theme` | `light` or `dark` |
| `autonomous_backend` | `headless`, `interactive`, or `interactive-claude` |
| `autonomous_permission_mode` | `conservative` or `yolo` |

Setup, onboarding, and splash-screen lifecycle fields under `setup`, `onboarding`, and `splash` are managed by Agent Runner.

## Using A Profile

Reference the profile by name with the `agent` key. Session strategy and mode can be set per step:

```yaml
- id: plan
  agent: planner
  prompt: "Plan the change."

- id: implement
  agent: implementor
  session: new
  mode: autonomous
  prompt: "Implement the plan."
```

## Engines

Engines are Go plugins registered in the binary. The engine interface supports workflow validation, deferred validation, prompt enrichment, and post-step validation.

```go
type Engine interface {
    ValidateWorkflow(workflow *model.Workflow, params map[string]string, workflowFile string) error
    NeedsDeferredValidation() bool
    EnrichPrompt(stepID string, params map[string]string, opts engine.EnrichOptions) string
    ValidateStep(stepID string, params map[string]string) (bool, error)
}
```

The built-in `openspec` engine is configured like this:

```yaml
engine:
  type: openspec
  change_param: change_name
```

It uses `openspec status --change <name> --json` and `openspec instructions <step> --change <name> --json` to validate artifact steps and enrich prompts.

## Session Strategies

Agent steps support four session strategies. See [Sessions And Modes](sessions-and-modes.md) for the full model.

| Session | Meaning |
| --- | --- |
| `new` | Start a fresh session using the step's `agent` profile. |
| `resume` | Resume the most recent session in the current workflow context. |
| `inherit` | In a sub-workflow, resume the parent workflow's most recent session. |
| named session | Resume or create a declared session such as `lead-agent`. |
