## Why

Baton's console output during workflow execution is hard to follow. Headless agent steps show almost nothing (just `mode: headless` and then silence until completion), and step boundaries blend together, making it difficult to tell where you are in a multi-step, nested workflow. These are low-effort improvements that significantly improve the operator experience.

## What Changes

- **Headless prompt display**: When a headless agent step starts, print the resolved prompt so the operator can see what the agent was asked to do.
- **Headless spinner**: Show an ASCII spinner animation while a headless agent step is running, so it's clear the step is in progress (not hung).
- **Step separator lines**: Print a horizontal rule (`━━━...`) before each step header to visually separate workflow steps.
- **Breadcrumb step headings**: Replace the current `--- step 1/5: proposal [interactive] ---` format with a human-readable breadcrumb that shows the full nesting path and iteration, e.g. `━━ gauntlet-retry > iteration 2 > implement [headless] ━━`. Derived from the same nesting context used by the audit log prefix, but formatted for humans.

## Capabilities

### New Capabilities
- `console-output-formatting`: Covers step separator lines, breadcrumb-style step headings, and general console output formatting rules for baton workflow execution.
- `headless-progress-indication`: Covers displaying the resolved prompt at headless agent step start and showing an ASCII spinner during headless execution.

### Modified Capabilities
<!-- None — these changes are additive console output improvements with no spec-level behavior changes to existing capabilities. -->

## Impact

- `src/runner.ts` — step header formatting, separator line insertion
- `src/executors/agent.ts` — headless prompt display, spinner lifecycle
- `src/context.ts` — possibly expose a human-readable breadcrumb from `ExecutionContext`
- `src/audit.ts` — reuse or adapt `buildPrefix()` logic for breadcrumb generation
- Tests covering step output format will need updating
