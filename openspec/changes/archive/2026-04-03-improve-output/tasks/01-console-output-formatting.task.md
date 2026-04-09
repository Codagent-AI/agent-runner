# Task: Console output formatting

## Goal

Add visual separator lines and breadcrumb-style step headings to baton's console output during workflow execution, replacing the current flat `--- step N/M: stepId [type] ---` format.

## Background

Baton's console output during workflow execution is hard to follow. Step boundaries use a flat format with no visual separation, and nesting context (loops, sub-workflows) is invisible to the operator. The nesting data is already tracked internally via `NestingSegment[]` in `ExecutionContext` and formatted for audit logs by `buildPrefix()` in `src/audit.ts`, but none of it surfaces in console output.

**New module: `src/format.ts`** — Create a new module for display-formatting functions, separate from `src/audit.ts` (which is for machine-oriented audit logging). This module contains three functions:

- **`buildBreadcrumb(nestingPath: NestingSegment[], stepId: string): string`** — Iterates `nestingPath`, emitting `stepId` for plain segments, `stepId > iteration N` for loop iterations (converting 0-indexed to 1-indexed), and appending `subWorkflowName` for sub-workflow segments. Joins with ` > `, appends the current `stepId`.
  - `[] + 'validate'` → `validate`
  - `[{stepId: "task-loop", iteration: 0}] + 'implement'` → `task-loop > iteration 1 > implement`
  - `[{stepId: "task-loop", iteration: 0}, {stepId: "verify", subWorkflowName: "verify-task"}] + 'check'` → `task-loop > iteration 1 > verify > verify-task > check`

- **`printSeparator(): void`** — Writes a fixed-width line of `━` characters to stdout.

- **`printStepHeading(index: number, total: number, breadcrumb: string, stepType: string, skipped: boolean): void`** — Prints the formatted heading. When `skipped` is false: `━━ step N/M: breadcrumb [type] ━━`. When `skipped` is true: `━━ step N/M: breadcrumb [skipped] ━━`.

**Modified: `src/runner.ts`** — In the `dispatchStep` function, replace the two `console.log` calls that print step headers (lines ~439-449) with calls to `printSeparator()` and `printStepHeading()`. Both skipped and non-skipped steps print the separator and heading. The `NestingSegment` type is defined in `src/context.ts`. The `buildPrefix()` function in `src/audit.ts` shows how the existing nesting path is iterated — `buildBreadcrumb` follows a similar pattern but produces human-readable output instead of machine-oriented audit prefixes.

**Key files:**
- `src/format.ts` — create this new module
- `src/runner.ts` — modify `dispatchStep()` to use the new formatting functions (the two `console.log` calls at lines ~439 and ~447-449)
- `src/context.ts` — contains `NestingSegment` type (read-only, no changes needed)
- `src/audit.ts` — contains `buildPrefix()` for reference on nesting path iteration (read-only, no changes needed)

**Constraints:**
- Do not modify the audit log format — `buildPrefix()` in `audit.ts` stays untouched
- No color or rich terminal formatting — output is plain text with box-drawing characters
- Do not detect terminal width — separator is a fixed width
- Do not truncate long breadcrumbs
- The `index` parameter in `printStepHeading` is 0-based; display as 1-based (i.e., `index + 1`)

## Spec

### Requirement: Step separator lines

Baton SHALL print a horizontal rule of `━` characters at a fixed width before each step heading to visually separate workflow steps.

#### Scenario: Separator printed before step header
- **WHEN** baton dispatches any step (shell, agent, loop, sub-workflow, or group)
- **THEN** a fixed-width horizontal rule of `━` characters is printed to stdout before the step heading

#### Scenario: First step includes separator
- **WHEN** the first step in a workflow is dispatched
- **THEN** the separator is still printed (no special-case omission)

#### Scenario: Skipped steps include separator
- **WHEN** a step is skipped due to `skip_if` evaluation
- **THEN** the separator and heading are still printed (with the `[skipped]` label)

### Requirement: Breadcrumb step headings

Baton SHALL replace the current `--- step N/M: stepId [type] ---` heading with a breadcrumb format that includes the step counter, the full nesting path with 1-indexed iteration numbers, and the step type. Format: `━━ step N/M: segment > segment > stepId [type] ━━`.

#### Scenario: Top-level step heading
- **WHEN** a top-level step `validate` of type `shell` is dispatched as step 1 of 5
- **THEN** the heading is printed as `━━ step 1/5: validate [shell] ━━`

#### Scenario: Step inside a loop iteration
- **WHEN** step `implement` (type `headless`) runs inside loop `task-loop` at iteration index 0, as step 1 of 3 within the loop
- **THEN** the heading is printed as `━━ step 1/3: task-loop > iteration 1 > implement [headless] ━━`

#### Scenario: Step inside a sub-workflow inside a loop
- **WHEN** step `check` (type `shell`) runs inside sub-workflow `verify-task`, invoked from step `verify` inside loop `task-loop` at iteration index 0
- **THEN** the heading is printed as `━━ step 1/2: task-loop > iteration 1 > verify > verify-task > check [shell] ━━`

#### Scenario: Skipped step heading
- **WHEN** a step `deploy` is skipped as step 3 of 5
- **THEN** the heading is printed as `━━ step 3/5: deploy [skipped] ━━`

## Done When

All seven scenarios above are covered by tests and passing. The new `src/format.ts` module exists with `buildBreadcrumb`, `printSeparator`, and `printStepHeading`. The `dispatchStep` function in `runner.ts` uses the new formatting functions instead of the old `console.log` calls.
