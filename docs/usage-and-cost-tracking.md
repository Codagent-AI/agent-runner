---
title: Usage And Cost Tracking
group: Usage
order: 1
description: How Agent Runner collects, displays, and stores token usage, duration, and CLI-reported cost.
---

# Usage And Cost Tracking

Agent Runner records duration for every completed step. For agent steps, it also collects token usage and USD cost when the agent CLI includes those values in its structured output.

Agent Runner uses the CLI you already have installed and authenticated. It does not require a separate API key for metrics collection. A subscription-backed CLI works the same way: if the CLI reports usage or cost, Agent Runner records it.

## What Gets Collected

The run summary has these columns:

| Column | Meaning |
| --- | --- |
| Duration | Active execution time for the step or container. |
| Input | Input tokens reported by the CLI. |
| Cache read | Input tokens read from a provider cache. |
| Cache write | Input tokens written to a provider cache. |
| Output | Output tokens reported by the CLI. |
| Reasoning | Reasoning tokens reported separately by the CLI. |
| Cost | The USD cost reported by the CLI. |

CLIs do not all report the same token categories. Agent Runner preserves the categories it receives instead of filling missing categories with zeroes.

The summary also shows canonical processed-token totals when an adapter can calculate them without double-counting cache or reasoning fields. These are the best totals to use when you need one input count, one output count, and one overall token count for an evaluation suite.

## CLI Support

| CLI | Token usage | Canonical totals | USD cost |
| --- | --- | --- | --- |
| Claude Code | Input, cache read, cache write, output | Yes | Yes, from `total_cost_usd` when reported |
| Codex | Input, cached input, output, reasoning | Yes | No |
| OpenCode | Input, cache read, cache write, output, reasoning | Yes | Yes, from reported step cost |
| Copilot | Available token metrics from assistant events | No | No; AI Credits are not converted to USD |
| Cursor | Best-effort input, cache read, cache write, output | No | No |

The cost value comes directly from the CLI and may differ from your provider's final bill. Agent Runner has no pricing catalog, does no token-times-rate math, and does not convert credits into dollars.

For Claude Code, Agent Runner captures `total_cost_usd` whenever the final result event includes it. This does not depend on Agent Runner having an Anthropic API key. Your normal Claude Code authentication is enough to run the step. If Claude omits the field, Agent Runner leaves cost unavailable.

## When Collection Works

Token and cost extraction requires captured structured output. It works for autonomous agent steps that use the headless backend.

Interactive agent steps and autonomous steps using an interactive backend own a PTY, so Agent Runner cannot inspect their structured stdout. Their usage and cost are unavailable. An autonomous agent step with `capture:` uses the headless path because its output must be captured.

Failed agent steps still count. If a CLI process consumes tokens, reports metrics, and then exits with an error, Agent Runner keeps those metrics in the step and run totals.

Shell, script, UI, loop, group, and sub-workflow steps do not call an agent CLI themselves. Their token and cost cells show a dash. Container rows roll up metrics from agent steps below them.

## Reading The Summary

| Display | Meaning |
| --- | --- |
| A number | The CLI reported this value. |
| `0` | The CLI explicitly reported zero. |
| A dash | The category is absent or does not apply to this step. |
| `?` | Usage or cost was expected but could not be collected. |
| `(partial)` | The displayed total includes reporting steps, but at least one eligible agent step did not report the metric. |

These symbols apply to the TUI. In `run-metrics.json`, unavailable usage remains a structured `unavailable` record with a reason, while missing cost is `null`.

Press `s` in the detailed run view to open the summary. Press `v` in the summary to return to the run view. Up and down select rows. Enter drills into loops, groups, and sub-workflows, and Escape moves back up one level. The Total row describes the current level.

Coverage appears separately for usage, canonical processed-token totals, and cost:

| Coverage | Meaning |
| --- | --- |
| `complete` | Every agent attempt that launched its CLI reported the metric. |
| `partial` | Some eligible attempts reported the metric and others did not. |
| `none` | No eligible attempt reported the metric. |

Skipped agent steps and steps that fail before launching a CLI do not reduce coverage.

## Retries And Resumed Runs

Each execution attempt gets its own metrics record. If a step fails and runs again, both attempts contribute to the run totals.

Resuming an Agent Runner run appends to the same metrics artifact. Active duration excludes time spent paused between invocations.

Codex reports cumulative session usage. Agent Runner subtracts the last trusted total for that Codex session so each step receives only its own usage. If a resumed session has no trusted baseline, or its counters reset, that step shows `?` rather than claiming the session's lifetime usage. The new cumulative value becomes the baseline for the next invocation.

## The Metrics Artifact

Each run stores a versioned file beside `state.json` and `audit.log`:

```text
~/.agent-runner/projects/<encoded-cwd>/runs/<run-id>/run-metrics.json
```

`run-metrics.json` is the supported input for evaluation tools and other automated consumers. It contains:

- one append-only record per step attempt;
- duration, outcome, nesting identity, usage provenance, token categories, canonical totals, and reported cost;
- execution-session durations for interrupted and resumed runs;
- run totals and coverage values; and
- `history_complete`, which tells consumers whether earlier metrics were lost during recovery.

The file is replaced atomically after each completed step or loop iteration. An interrupted run therefore keeps a valid artifact for work that already finished.

See [Run State And Audit](run-state-and-audit.md) for the full run-directory layout and inspection commands.

## Legacy Runs

Runs created before metrics collection have no structured usage or cost records. They open in the original detail view instead of an empty summary, and their agent blocks omit usage and cost lines.

If the original workflow file was deleted or moved, Agent Runner reconstructs the executed top-level steps from `audit.log`. Audit recovery can show steps that actually ran, but it cannot recreate pending steps that never emitted an event.

## Testing Cost Collection

The shortest cost test is one autonomous headless agent step using Claude Code or OpenCode:

```yaml
name: metrics-check

steps:
  - id: ask
    agent: implementor
    cli: claude
    mode: autonomous
    capture: response
    prompt: "Reply with one short sentence."
```

The `capture:` field forces this autonomous step onto the headless path. Run the workflow with your normally authenticated CLI, then open the completed run or inspect `run-metrics.json`. Claude or OpenCode must include a USD cost in its structured result for the Cost column to contain a number.

If usage shows `?`, check whether the step used a PTY-backed mode, whether the CLI emitted its final usage event, and whether a resumed cumulative session had a trusted baseline. If cost alone shows `?`, the CLI probably did not report a USD cost for that invocation.
