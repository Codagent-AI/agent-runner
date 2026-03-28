# Loops and Sub-Workflows

Agent Runner supports loops (counted and for-each), sub-workflow invocation, output capture, and flow control primitives. These features enable complex orchestration patterns like iterating over task files with verify-fix retry loops.

## Loops

A loop step repeats its child steps according to a loop configuration. Two flavors are supported.

### Counted loop

Repeat up to N times:

```yaml
- id: verify-fix
  loop:
    max: 3
  steps:
    - id: gauntlet
      mode: shell
      command: agent-gauntlet run
      capture: gauntlet_output
      continue_on_failure: true
      break_if: success

    - id: fix
      mode: headless
      session: resume
      prompt: |
        Fix these issues:
        {{gauntlet_output}}
      skip_if: previous_success
```

When the loop reaches `max` iterations without `break_if` triggering, the loop outcome is `exhausted`, which fails the workflow. Use `break_if` to define the success condition.

### For-each loop

Iterate over files matching a glob pattern:

```yaml
- id: per-task
  loop:
    over: "openspec/changes/{{change_name}}/tasks/*.task.md"
    as: task_file
  steps:
    - id: implement
      mode: headless
      session: new
      prompt: "Implement {{task_file}}"
```

- `over` -- glob pattern, expanded at runtime. Supports `{{param}}` interpolation.
- `as` -- variable name. Each match is bound to this name, accessible via `{{task_file}}`.

Matches are sorted alphabetically. If no files match, the loop succeeds immediately with no iterations.

### Loop schema

```yaml
loop:
  max: N              # counted loop: repeat up to N times
  # -- or --
  over: "glob/pattern" # for-each loop: iterate over matches
  as: variable_name    # variable name for each match
```

A loop requires either `max` or both `over` and `as`.

### Iteration context

Each loop iteration gets its own execution context:

- Fresh `sessionIds` and `capturedVariables` (not inherited from previous iterations)
- Inherits `params` from parent, plus any loop variable (`as`)
- `lastStepOutcome` resets each iteration

This means `session: resume` inside a loop body resumes from a step within the *same iteration*, not from a previous iteration.

## Sub-Workflows

A step can invoke another workflow YAML file:

```yaml
- id: implement-single-task
  workflow: implement-task.yaml
  params:
    task_file: "{{task_file}}"
```

### Path resolution

The `workflow` path is resolved relative to the parent workflow's directory. If the parent workflow is at `workflows/flokay.yaml` and a step references `implement-task.yaml`, agent-runner resolves it relative to the directory containing `flokay.yaml`.

### Parameter passing

Parameters are passed explicitly via the `params` map. The sub-workflow only sees the parameters listed in `params` -- it does not inherit the parent's full parameter set. Parameter values support `{{variable}}` interpolation.

### Execution model

Sub-workflows:

- Execute in the same process (no subprocess)
- Get their own `ExecutionContext` (session IDs, captured variables, last step outcome)
- Share the parent's engine reference
- Can nest arbitrarily deep
- Are loaded lazily at execution time

### session: inherit

Inside a sub-workflow, `session: inherit` crosses the sub-workflow boundary to find the parent's most recent session:

```yaml
# In run-gauntlet.yaml (a sub-workflow)
- id: fix-violations
  mode: headless
  session: inherit
  prompt: "Fix the gauntlet violations..."
```

This walks up the parent context chain until it finds a context with a different `workflowFile`, then returns that context's most recent session ID. It enables the pattern where a sub-workflow needs to resume the agent session that produced the code being verified.

## Flow Control

### continue_on_failure

```yaml
- id: gauntlet
  mode: shell
  command: agent-gauntlet run
  continue_on_failure: true
```

Normally a failed step stops the workflow. With `continue_on_failure: true`, the workflow continues to the next step. The step's outcome is tracked and available for conditions.

### skip_if

```yaml
- id: fix
  mode: headless
  skip_if: previous_success
  prompt: "Fix the issues..."
```

Skip this step if the previous step succeeded. Pairs with `continue_on_failure` to create conditional execution patterns.

### break_if

```yaml
- id: gauntlet
  mode: shell
  command: agent-gauntlet run
  break_if: success
```

Evaluated after the step executes. Exits the enclosing loop:

- `break_if: success` -- exit loop when step succeeds (exit code 0)
- `break_if: failure` -- exit loop when step fails (non-zero exit code)

When triggered, execution continues with the next step after the loop. A loop that exits via `break_if` is considered successful.

## Output Capture

Shell steps can capture their stdout into a named variable:

```yaml
- id: gauntlet
  mode: shell
  command: agent-gauntlet run
  capture: gauntlet_output
  continue_on_failure: true
```

- Output is both displayed to the terminal and stored in the variable (tee behavior)
- Captured variables are available to subsequent steps via `{{gauntlet_output}}` interpolation
- Variables persist in the state file for resume
- Captured variables take precedence over params when names collide
- Only shell steps support `capture` (schema validation rejects it on agent steps)

## Composed Example

The flokay implement workflow demonstrates all these features working together:

**workflows/implement-change.yaml** -- for-each loop over task files:

```yaml
name: implement-change
params:
  - name: change_name
    required: true

steps:
  - id: implement-tasks
    loop:
      over: "openspec/changes/{{change_name}}/tasks/*.task.md"
      as: task_file
    steps:
      - id: implement-single-task
        workflow: implement-task.yaml
        params:
          task_file: "{{task_file}}"
```

**workflows/implement-task.yaml** -- agent step then gauntlet retry:

```yaml
name: implement-task
params:
  - name: task_file
    required: true

steps:
  - id: implement
    mode: headless
    session: new
    prompt: "Implement the task described in {{task_file}}."

  - id: run-gauntlet
    workflow: run-gauntlet.yaml
```

**workflows/run-gauntlet.yaml** -- counted retry loop with capture and flow control:

```yaml
name: run-gauntlet
steps:
  - id: gauntlet-retry
    loop:
      max: 3
    steps:
      - id: run-gauntlet
        mode: shell
        command: agent-gauntlet run --enable-review task-compliance
        capture: gauntlet_output
        continue_on_failure: true
        break_if: success

      - id: fix-violations
        mode: headless
        session: inherit
        prompt: |
          The gauntlet found violations. Fix them:
          {{gauntlet_output}}
        skip_if: previous_success
        continue_on_failure: true
```

The session behavior chains naturally:

- `implement` starts a new session (S1)
- `fix-violations` uses `session: inherit` to resume S1 from across the sub-workflow boundary
- Each fix iteration builds on the previous: S1 -> fix1 -> fix2 -> fix3
- When the outer loop moves to the next task, `implement` starts fresh (S2)

## Session Behavior Inside Loops

**Inner loop (verify-fix), single task:**

```text
Iteration 1:
  gauntlet     -> shell (no session)
  fix          -> inherits implement's session -> S1'

Iteration 2 (if gauntlet fails again):
  gauntlet     -> shell (no session)
  fix          -> resumes S1' -> S1''

Iteration 3 (if still failing):
  gauntlet     -> shell (no session)
  fix          -> resumes S1'' -> S1'''
```

**Outer loop (per task), across tasks:**

```text
Task 1:
  implement    -> session S1 (new)
  [verify-fix loop uses S1, S1', S1'']

Task 2:
  implement    -> session S2 (new)     <- fresh start
  [verify-fix loop uses S2, S2', S2'']
```
