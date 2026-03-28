# Capability: workflow-loops

## Purpose

Defines loop constructs for workflow steps: counted loops with max iterations, for-each loops over glob patterns, nested step groups, loop break conditions, and session scoping within loops.

## Requirements

### Requirement: Counted loop execution

A step with `loop: { max: N }` and a `steps` array SHALL execute its child steps sequentially, then repeat from the first child step, up to N total iterations. The loop body is the `steps` array.

#### Scenario: Loop runs to completion
- **WHEN** a step has `loop: { max: 3 }` and no `break_if` triggers during any iteration
- **THEN** baton executes the loop body 3 times, then fails the workflow (exhaustion)

#### Scenario: Loop with break_if exits early
- **WHEN** a step has `loop: { max: 3 }` and a child step's `break_if` triggers on iteration 2
- **THEN** baton executes 2 iterations, exits the loop, and continues with the next step after the loop

#### Scenario: Max is required for counted loops
- **WHEN** a step has `loop: {}` with no `max` and no `over`
- **THEN** baton fails at load time with a validation error

### Requirement: For-each loop execution

A step with `loop: { over: <glob>, as: <var> }` and a `steps` array SHALL expand the glob pattern at runtime, then execute the child steps once per match. Each match is bound to the variable named in `as`, available via `{{var}}` interpolation in child steps.

#### Scenario: Glob matches multiple files
- **WHEN** a step has `loop: { over: "tasks/*.task.md", as: task_file }` and the glob matches 3 files
- **THEN** baton executes the loop body 3 times, binding `{{task_file}}` to each matched path in order

#### Scenario: Glob matches zero files
- **WHEN** a step has `loop: { over: "tasks/*.task.md", as: task_file }` and the glob matches no files
- **THEN** baton skips the loop body and continues with the next step after the loop

#### Scenario: Glob pattern supports parameter interpolation
- **WHEN** a step has `loop: { over: "openspec/changes/{{change_name}}/tasks/*.task.md", as: task_file }`
- **THEN** baton interpolates `{{change_name}}` before expanding the glob

### Requirement: Nested steps

A step with a `steps` array SHALL execute its child steps sequentially. Child steps inherit the parent's parameter scope, including loop variables and captured variables from prior sibling steps. Nesting is supported to arbitrary depth.

#### Scenario: Steps nested without a loop
- **WHEN** a step has a `steps` array but no `loop` field
- **THEN** baton executes the child steps sequentially as a group

#### Scenario: Child steps inherit parent scope
- **WHEN** a parent step binds `{{task_file}}` via a for-each loop and a child step references `{{task_file}}`
- **THEN** the child step receives the interpolated value

### Requirement: break_if

A step within a loop body with `break_if: success` or `break_if: failure` SHALL be evaluated after the step executes. If the condition matches the step's outcome, baton SHALL exit the enclosing loop immediately, skipping any remaining steps in the current iteration. Execution continues with the next step after the loop.

#### Scenario: break_if success triggers on passing step
- **WHEN** a shell step has `break_if: success` and exits with code 0
- **THEN** baton exits the enclosing loop and skips remaining steps in this iteration

#### Scenario: break_if success does not trigger on failing step
- **WHEN** a shell step has `break_if: success` and exits with non-zero code
- **THEN** baton continues to the next step in the loop body

#### Scenario: break_if outside a loop
- **WHEN** a step has `break_if` but is not inside a loop
- **THEN** baton fails at validation time with a validation error (load time for the parent workflow, execution time for lazily loaded sub-workflows)

### Requirement: Session scoping within loops

Within a loop iteration, all agent steps SHALL share one session chain. `session: new` (the default) creates a fresh session on the first agent step of each iteration. `session: resume` SHALL resume the most recent session within the current iteration.

```text
OUTER LOOP: implement-tasks (iterating over task files)
═══════════════════════════════════════════════════════════════

TASK 1 (001-add-schema.task.md)
┌─────────────────────────────────────────────────────────────┐
│ implement [session: new] ──────────────────→ session A      │
│                                                             │
│ INNER LOOP: run-gauntlet (max 3)                           │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ Iteration 1:                                            │ │
│ │   gauntlet [shell]        → fail                        │ │
│ │   fix [session: inherit]  → resumes A → session A'      │ │
│ │                                                         │ │
│ │ Iteration 2:                                            │ │
│ │   gauntlet [shell]        → fail                        │ │
│ │   fix [session: inherit]  → resumes A' → session A''    │ │
│ │                                                         │ │
│ │ Iteration 3:                                            │ │
│ │   gauntlet [shell]        → pass → BREAK                │ │
│ │   fix                     → SKIPPED                     │ │
│ └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘

TASK 2 (002-add-runner.task.md)
┌─────────────────────────────────────────────────────────────┐
│ implement [session: new] ──────────────────→ session B      │
│                                              ↑              │
│                              fresh start, A/A'/A'' forgotten│
│                                                             │
│ INNER LOOP: run-gauntlet (max 3)                           │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ Iteration 1:                                            │ │
│ │   gauntlet [shell]        → pass → BREAK                │ │
│ │   fix                     → SKIPPED                     │ │
│ └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

#### Scenario: New session per outer loop iteration
- **WHEN** a for-each loop iterates over tasks and the first agent step has `session: new`
- **THEN** each iteration starts a fresh session, independent of previous iterations

#### Scenario: Session chains within an iteration
- **WHEN** multiple agent steps within a single loop iteration use `session: resume`
- **THEN** each resume extends the same session chain started by the first agent step in that iteration

#### Scenario: Inner loop resumes across iterations
- **WHEN** a `session: inherit` step runs in iteration 2 of an inner loop
- **THEN** it resumes the session from iteration 1's last agent step, forming a chain
