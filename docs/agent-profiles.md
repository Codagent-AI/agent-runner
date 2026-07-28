---
title: Agent Profiles
group: Guides
order: 4
description: Configure the four canonical agent roles, their CLIs, models, and execution modes.
---

# Agent Profiles

Agent Runner workflows use four canonical roles: Lead, Crosscheck, Implementor, and Tester. A named agent profile selects the role's default execution mode, CLI adapter, model, effort, and optional system prompt, so workflows can name a responsibility without repeating runtime configuration.

## Canonical Roles

| Role | Default mode | Responsibility |
| --- | --- | --- |
| `lead` | Interactive | Works with you to shape goals, decisions, proposals, specifications, designs, and plans. |
| `crosscheck` | Autonomous | Independently challenges Lead's planning artifacts, checks completeness, and finds omissions. |
| `implementor` | Autonomous | Performs code changes and focused implementation validation. |
| `tester` | Autonomous | Exercises completed work against acceptance expectations and reports evidence. |

Crosscheck is not a code-review system. It supplies an independent perspective while work is being defined and planned. Agent Validator owns implementation-code checks and reviews; Tester owns acceptance-flow verification after implementation.

Agent Runner also provides `interactive_base`, `autonomous_base`, and `summarizer` as built-in support profiles. Native setup does not create or replace those entries.

## Recommendation Policy

During initial setup, Agent Runner detects installed `claude`, `codex`, `copilot`, `cursor`, and `opencode` adapters and discovers their available models. It then recommends all four roles together.

| Role | CLI precedence | Preferred family | Power tier | Pairing policy |
| --- | --- | --- | --- | --- |
| Lead | Claude, Codex, OpenCode, Copilot, Cursor | Claude, then GPT | Flagship | Establishes the family Crosscheck should avoid. |
| Crosscheck | Claude, Codex, OpenCode, Copilot, Cursor | GPT, then Claude | Flagship | Prefers a known family different from Lead. |
| Implementor | Codex, Cursor, OpenCode, Claude, Copilot | GPT, then Claude | Balanced | Establishes the family Tester should avoid. |
| Tester | Claude, Codex, OpenCode, Copilot, Cursor | Claude, then GPT | Balanced | Prefers a known family different from Implementor. |

The defaults reflect practical tradeoffs, not a universal ranking. Lead and Crosscheck favor strong reasoning for definition work. Implementor starts with CLIs that commonly provide strong implementation quality at a lower practical cost, while Tester uses a balanced tier and an independent family where possible. Every choice remains customizable.

The maintained tier policy is deliberately small:

- Claude Opus is the flagship tier; Claude Sonnet is the balanced tier.
- GPT identifiers with boundary-safe `sol` markers are flagship; identifiers with `terra` markers are balanced.
- When several matching GPT versions are available, the newest numeric version is preferred.
- Unclassified models remain available for manual selection but are not guessed as automatic recommendations.

When a paired role cannot use a known different family, setup applies normal precedence and identifies the affected Lead/Crosscheck or Implementor/Tester pair. A model-less default from Claude is treated as Claude, one from Codex as GPT, and one from a multi-provider CLI as unknown.

## Worked Recommendations

With Claude exposing Opus and Sonnet and Codex exposing `gpt-5.6-sol` and `gpt-5.6-terra`, setup recommends:

| Role | CLI and model |
| --- | --- |
| Lead | Claude / Opus |
| Crosscheck | Codex / `gpt-5.6-sol` |
| Implementor | Codex / `gpt-5.6-terra` |
| Tester | Claude / Sonnet |

If Lead is customized to Codex / `gpt-5.6-sol`, Crosscheck recomputes to Claude / Opus. Implementor and Tester remain unchanged because they form a separate diversity pair.

If only OpenCode, Copilot, and Cursor are installed and none of their discovered models match a recognized tier, the recommendation uses OpenCode's CLI default for Lead, Crosscheck, and Tester and Cursor's CLI default for Implementor. Setup explains that known family diversity could not be established.

## Accept or Customize

The recommendation summary has two actions:

- Accept all recommendations freezes the four displayed selections and skips individual role screens.
- Customize visits Lead, Crosscheck, Implementor, and Tester in that order.

Changing Lead recomputes Crosscheck from the discovery snapshot. Changing Implementor recomputes Tester. These are starting recommendations only: you can deliberately choose the same model family for either paired role.

If a CLI returns no models or model discovery fails, setup keeps the CLI available and offers its default. When discovered models are all unclassified, the model screen focuses `Use CLI default` and lists every discovered model afterward. Choosing a CLI default leaves `model` unset.

## Setup-Written Profiles

Native setup writes direct entries under `profiles.default.agents`; it does not use `extends` for the four managed roles:

```yaml
profiles:
  default:
    agents:
      lead:
        default_mode: interactive
        cli: claude
        model: opus
      crosscheck:
        default_mode: autonomous
        cli: codex
        model: gpt-5.6-sol
      implementor:
        default_mode: autonomous
        cli: codex
        model: gpt-5.6-terra
      tester:
        default_mode: autonomous
        cli: claude
        model: sonnet
```

An adapter-default selection omits the `model` field. Setup preserves other profile sets, top-level settings, and unmanaged agents such as bases, Summarizer, or team-specific profiles.

## Configuration Layers

Configuration resolves in this order:

| Order | Source |
| --- | --- |
| 1 | Built-in defaults shipped with Agent Runner |
| 2 | Global config at `~/.agent-runner/config.yaml` |
| 3 | Project config at `.agent-runner/config.yaml` |

Project config wins over global config. Only project config may set `active_profile`, which prevents a machine-wide setting from silently selecting a project's profile set.

Built-in agents act as fallbacks. An explicit `extends` parent in a global or project profile set replaces built-in agents as the child set's inherited fallback; direct child entries still override that parent.

## Legacy Aliases

`planner` remains an undated compatibility alias for `lead`, and `reviewer` remains an undated compatibility alias for `crosscheck`. Existing configurations and workflows continue to resolve those names with deprecation warnings. Warnings are limited to aliases in the active profile inheritance chain or aliases referenced explicitly; dormant profiles do not warn merely because their configuration was loaded. There is no scheduled removal date.

Loading a legacy file never rewrites it. If you explicitly confirm native setup's overwrite prompt, setup removes `planner` and `reviewer` from `profiles.default.agents` in the selected file and writes the four canonical roles. Unrelated layers and agents are preserved.

Older Agent Runner binaries do not understand canonical `lead` and `crosscheck` entries. Before downgrading after a setup migration, manually rename `lead` to `planner` and `crosscheck` to `reviewer` in the affected config file.

## Using a Profile

Reference a canonical profile with the `agent` key:

```yaml
- id: plan
  agent: lead
  prompt: "Plan the change."

- id: implement
  agent: implementor
  session: new
  mode: autonomous
  prompt: "Implement the plan."

- id: accept
  agent: tester
  session: new
  mode: autonomous
  prompt: "Exercise the acceptance scenarios."
```

A step-level `mode`, `cli`, or `model` overrides the resolved profile for that step. See [Sessions And Modes](sessions-and-modes.md) for session strategies.

## User Settings

User settings live in `~/.agent-runner/settings.yaml`:

| Setting | Values |
| --- | --- |
| `theme` | `light` or `dark` |
| `autonomous_backend` | `headless`, `interactive`, or `interactive-claude` |
| `autonomous_permission_mode` | `conservative` or `yolo` |

Setup, onboarding, and splash lifecycle fields are managed by Agent Runner.
