# Agent Runner User Guide

## Getting started

### Prerequisites

- [Go](https://golang.org) 1.23+
- [Claude Code](https://claude.com/claude-code) CLI installed and authenticated
- (Optional) [OpenSpec](https://github.com/pacaplan/openspec) CLI, if using the openspec engine

### Installation

```bash
git clone <repo-url>
cd agent-runner
go build ./cmd/agent-runner    # compiles to agent-runner binary
```

Add the binary to your PATH, or run it directly from the current directory: `./agent-runner`.

## Writing workflows

A workflow is a YAML file that defines a sequence of steps. Each step either runs an agent session, a shell command, a loop, or a sub-workflow.

### Minimal example

```yaml
name: hello
description: "A simple two-step workflow"

steps:
  - id: greet
    mode: headless
    prompt: "Say hello and list the files in the current directory."

  - id: summarize
    mode: headless
    session: resume
    prompt: "Summarize what you found."
```

Run it:

```bash
agent-runner run hello.yaml
```

### With parameters

```yaml
name: review-pr
description: "Review a pull request"

params:
  - name: pr_number
    required: true

steps:
  - id: fetch
    mode: shell
    command: gh pr checkout {{pr_number}}

  - id: review
    mode: interactive
    session: new
    prompt: "Review the changes in this PR and suggest improvements."
```

Run it:

```bash
agent-runner run review-pr.yaml 42
```

Parameters are positional -- they map to the `params` array in order.

## Step modes

### Interactive

The agent runs in full interactive mode. You collaborate with it in your terminal. When you're done with the step, exit the agent session to advance to the next step.

If you exit the session, agent-runner treats the step as complete and moves on. The state file persists so you can resume later if the workflow is interrupted.

### Headless

The agent runs non-interactively (`claude -p`). Its output streams to your terminal so you can watch progress. When the agent finishes, agent-runner automatically advances.

Use headless for steps that don't need human interaction -- task generation, code review, implementation, etc.

Pressing ctrl-c during a headless step kills the agent subprocess and exits agent-runner. The state file preserves the interrupted step so you can resume later.

### Shell

No agent involved. Agent Runner runs a shell command directly. Useful for:

- Scaffolding: `openspec new change "{{name}}"`
- Validation: `openspec validate`
- Git operations: `git commit -m "..."`
- Any CLI tool

Shell steps fail the workflow on non-zero exit codes.

## Session management

### New sessions

```yaml
- id: design
  mode: headless
  session: new
  prompt: "Design the architecture..."
```

Starts a fresh agent session with no prior context. The agent reads what it needs from disk. Use this when:

- The step has a different concern than the previous one
- Context from prior steps would be bloating or confusing
- The step is self-contained

### Resumed sessions

```yaml
- id: specs
  mode: interactive
  session: resume
  prompt: "Now write the specs based on what we discussed."
```

Continues the most recent session within the current workflow. The agent has full conversational context from earlier. Use this when:

- Steps are tightly coupled (proposal -> specs uses the same conversation)
- The agent needs to remember decisions from the prior step
- You want continuity in an ongoing dialogue

### Inherited sessions

```yaml
# Inside a sub-workflow
- id: fix
  mode: headless
  session: inherit
  prompt: "Fix the issues found by the gauntlet."
```

Crosses sub-workflow boundaries to resume the parent workflow's most recent session. Use this when:

- A sub-workflow needs to continue the conversation from the parent
- The gauntlet pattern: a sub-workflow needs to fix issues using the context of the step that created the code

`session: inherit` walks the parent context chain to find the nearest session from a different workflow file.

## Per-step CLI override

Agent steps can specify which CLI backend to use via the `cli` field:

```yaml
- id: implement
  mode: headless
  cli: codex
  model: o3
  prompt: "Implement the feature."

- id: review
  mode: headless
  prompt: "Review the implementation."
```

When `cli` is set, agent-runner uses the corresponding CLI adapter for arg construction and session discovery. When absent, `claude` is used by default. Currently supported values: `claude`, `codex`. The `cli` field is only valid on agent steps (headless or interactive), not shell steps.

## Per-step model override

Agent steps can specify which model the agent should use:

```yaml
- id: quick-check
  mode: headless
  model: sonnet
  prompt: "Do a quick syntax check on the files."

- id: deep-review
  mode: headless
  model: opus
  prompt: "Do a thorough code review."
```

When `model` is set, agent-runner passes it through the CLI adapter (e.g., `--model <value>` for Claude, `-m <value>` for Codex). When absent, the CLI uses its default model. The `model` field is only valid on agent steps (headless or interactive), not shell steps.

## Loops

### Counted loops

Repeat a group of steps up to N times:

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
      session: new
      prompt: |
        The gauntlet found issues:
        {{gauntlet_output}}
        Fix them.
      skip_if: previous_success
```

The loop runs up to 3 times. If `break_if: success` triggers on the gauntlet step (exit code 0), the loop exits early. If the loop exhausts all iterations without breaking, it fails the workflow.

### For-each loops

Iterate over a list of files matching a glob pattern:

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

The `over` field accepts a glob pattern, expanded at runtime. Each match is bound to the variable named in `as`, available via `{{task_file}}` interpolation in nested steps. Each iteration gets a fresh context for session IDs and captured variables.

### Loop early exit with break_if

`break_if` is evaluated after a step executes:

- `break_if: success` -- exit the enclosing loop if the step succeeded
- `break_if: failure` -- exit the enclosing loop if the step failed

Execution continues with the next step after the loop.

## Sub-workflows

Complex patterns can be extracted into reusable workflow files:

```yaml
# workflows/implement-task.yaml
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

Invoke from a parent workflow:

```yaml
- id: implement-single-task
  workflow: implement-task.yaml
  params:
    task_file: "{{task_file}}"
```

Sub-workflows:

- Execute in the same process
- Get their own execution context (session IDs, captured variables)
- Receive only explicitly passed parameters
- Can nest arbitrarily (sub-workflow calling sub-workflow)
- Support `session: inherit` to resume a parent session

## Flow control

### continue_on_failure

By default, a failed step stops the workflow. `continue_on_failure: true` allows the workflow to proceed:

```yaml
- id: gauntlet
  mode: shell
  command: agent-gauntlet run
  continue_on_failure: true
```

Essential for the verify-fix pattern where gauntlet failure is expected and handled by the next step.

### skip_if

Skip a step based on the previous step's outcome:

```yaml
- id: fix
  mode: headless
  session: resume
  prompt: "Fix the issues..."
  skip_if: previous_success
```

When `skip_if: previous_success` is set, the step is skipped if the previous step succeeded. This pairs with `continue_on_failure` to create conditional execution: run the fix step only when the gauntlet fails.

## Output capture

Shell steps can capture their stdout into a named variable:

```yaml
- id: gauntlet
  mode: shell
  command: agent-gauntlet run
  capture: gauntlet_output
  continue_on_failure: true

- id: fix
  mode: headless
  prompt: |
    Fix these issues:
    {{gauntlet_output}}
  skip_if: previous_success
```

The captured output is both displayed to the terminal (tee behavior) and stored in the variable. Captured variables are available to all subsequent steps via `{{var_name}}` interpolation and are persisted in the state file for resume.

## Running workflows

### Basic run

```bash
agent-runner run workflows/flokay.yaml my-change
```

### Starting from a specific step

```bash
agent-runner run workflows/flokay.yaml my-change --from design
```

Skips all steps before `design` and starts there. Useful when you've already completed earlier steps manually or want to re-run a specific phase.

### Starting with an existing Claude session

```bash
agent-runner run workflows/plan-change.yaml my-change --session <session-id>
```

Seeds the workflow with a Claude session ID from a conversation you were already having. The first step that uses `session: resume` will continue that conversation, giving the agent full context from your prior discussion.

This is the natural flow when you've been exploring an idea with Claude and want to transition into a structured workflow:

1. Chat with Claude about a feature idea
2. Decide to formalize it: `agent-runner run workflows/plan-change.yaml my-feature --session <id>`
3. The first `session: resume` step picks up where your conversation left off

Steps using `session: new` are unaffected -- the seeded session is only used by `session: resume`. If no step uses `session: resume`, the flag is ignored. The seed propagates through sub-workflows and loop iterations, so it works even when the first agent step is inside a nested workflow.

You can find your current session ID in `~/.claude/projects/<encoded-cwd>/` -- it's the filename (without `.jsonl`) of the most recently modified conversation file.

### Resuming interrupted workflows

If a workflow is interrupted (you abort, a step fails, your machine restarts), agent-runner saves its state to `agent-runner-state.json`. Resume with:

```bash
agent-runner resume path/to/agent-runner-state.json
```

This reloads the workflow, restores session IDs and parameters, and picks up from the last step. If the workflow file has changed since the state was written, agent-runner warns you but proceeds.

The state file location depends on the engine. The openspec engine stores it in the change directory (`openspec/changes/<name>/agent-runner-state.json`). Without an engine, it's in the project root.

### Validating workflows

Check that a workflow is syntactically valid without running it:

```bash
agent-runner validate workflows/flokay.yaml
```

With an engine configured, this also runs engine-specific validation (e.g., checking that every openspec artifact has a matching workflow step).

## Engines

Engines are optional plugins that hook into agent-runner's execution lifecycle. They enrich prompts with external context, validate that steps produced expected output, and control where state files live.

### The openspec engine

The built-in openspec engine integrates with the OpenSpec CLI. It:

1. **Enriches prompts** -- Before each artifact step, calls `openspec instructions` to get the template, output path, and dependencies, and appends them to the prompt in an `<artifact_context>` block.

2. **Validates steps** -- After each artifact step, calls `openspec status` to check that the artifact was created. If validation fails, agent-runner offers you the choice to resume the session interactively or exit.

3. **Validates the workflow** -- At load time, checks that every openspec schema artifact has a matching step ID in the workflow.

4. **Controls state directory** -- Places `agent-runner-state.json` in the openspec change directory.

#### Configuration

```yaml
engine:
  type: openspec
  change_param: change_name    # which workflow param holds the change name
```

The engine uses the step ID to determine which steps are artifact steps. Step IDs must match the openspec schema's artifact IDs exactly (e.g., `proposal`, `specs`, `design`, `tasks`, `review`).

#### Requirements

The `openspec` CLI must be installed and on your PATH. The engine checks for this at initialization and fails fast with a clear error if it's missing.

### Writing custom engines

Engines implement the `Engine` interface (all methods optional):

```typescript
interface Engine {
  getStateDir?(params: Record<string, string>): string;
  validateWorkflow?(workflow: Workflow, params: Record<string, string>): void;
  enrichPrompt?(stepId: string, params: Record<string, string>): string | undefined;
  validateStep?(stepId: string, params: Record<string, string>): boolean;
}
```

Register your engine in `src/engine.ts`:

```typescript
import { myEngine } from './engines/my-engine.ts';

const engineRegistry: Record<string, EngineConstructor> = {
  openspec: createOpenSpecEngine,
  'my-engine': myEngine,
};
```

## The flokay workflow

The flokay workflow (`workflows/flokay.yaml`) orchestrates the full change lifecycle using composition. It calls into sub-workflows for the implementation phase:

| Step | Type | What it does |
|------|------|-------------|
| `create` | shell | Scaffolds a new openspec change |
| `proposal` | interactive | Collaboratively write the proposal |
| `specs` | interactive (resume) | Write specs based on the proposal |
| `design` | interactive | Design the architecture |
| `tasks` | headless | Generate implementation tasks |
| `review` | headless (resume) | Run gauntlet review |
| `implement` | sub-workflow | Loops over task files, implements each with gauntlet retry |
| `verify` | headless | Verify implementation with gauntlet |
| `archive` | headless (resume) | Archive the change, sync specs |
| `archive-verify` | shell | Skip gauntlet for archive-only changes |
| `finalize` | headless (resume) | Push PR, wait for CI, fix failures |

The `implement` step invokes `implement-change.yaml`, which loops over task files and for each one invokes `implement-task.yaml`, which itself invokes `run-gauntlet.yaml` for the verify-fix retry loop.

Run it:

```bash
agent-runner run workflows/flokay.yaml my-feature-name
```

## Audit logging

Every workflow run produces a structured audit log for post-failure troubleshooting and visibility. Log files are stored at:

```text
~/.agent-runner/projects/{encoded-cwd}/logs/{workflow-name}-{timestamp}.log
```

The `{encoded-cwd}` replaces `/`, `.`, and `_` in your project path with `-` (e.g., `/Users/foo/my_project` becomes `-Users-foo-my-project`).

### Log format

Each line is a hybrid of human-scannable prefix and structured JSON:

```text
2026-03-15T18:30:00Z [validate] step_start {"command":"npm test","context":{...}}
2026-03-15T18:30:02Z [validate] step_end {"outcome":"success","duration_ms":2000,"exit_code":0,"stderr":""}
2026-03-15T18:30:02Z [task-loop:0, implement] step_start {"prompt":"Implement tasks/1.md",...}
```

The nesting prefix in brackets shows exactly where in the execution you are:

- `[validate]` -- top-level step
- `[task-loop:0, implement]` -- step `implement` inside loop `task-loop` at iteration 0
- `[task-loop:0, verify, sub:verify-task, check]` -- step `check` inside sub-workflow `verify-task`, invoked from loop iteration 0

### Event types

The audit log emits paired start/end events at every lifecycle boundary:

| Events | When |
|--------|------|
| `run_start` / `run_end` | Workflow execution begins and ends |
| `step_start` / `step_end` | Before and after each step |
| `iteration_start` / `iteration_end` | Before and after each loop iteration |
| `sub_workflow_start` / `sub_workflow_end` | Before and after sub-workflow execution |
| `error` | Uncaught exception during execution |

### What gets captured

**Start events** include a full context snapshot (all params and captured variables at that point), plus step-type-specific data:

- **Shell steps**: interpolated command
- **Agent steps**: interpolated prompt, mode, session strategy, resolved session ID, model, engine enrichment
- **Loop steps**: loop type, max or glob pattern with resolved matches
- **Sub-workflow steps**: resolved workflow path, interpolated params

**End events** include outcome, duration in milliseconds, and step-type-specific results:

- **Shell steps**: exit code, stderr (always captured), stdout (if `capture` set)
- **Agent steps**: exit code, discovered session ID
- **Loop steps**: iterations completed, whether break was triggered
- **Skipped steps**: the `skip_if` condition that triggered the skip

### Crash safety

The audit log flushes each entry to disk immediately. If the process crashes, an `error` event and `run_end` are emitted before exit. All entries written before the crash are preserved.

### Retention

Log files are never automatically deleted. One file is created per workflow execution (including resumed runs). You manage cleanup yourself.

### Using the log for troubleshooting

When a workflow fails:

1. Find the log file in `~/.agent-runner/projects/{encoded-cwd}/logs/`
2. Search for `step_end` events with `"outcome":"failed"` to find the failing step
3. Check the `stderr` field for error output
4. Look at the `step_start` event's context snapshot to see what params and variables the step received
5. Use the nesting prefix to trace through loop iterations or sub-workflow calls

You can also pipe through `jq` by splitting on the first `{`:

```bash
grep 'step_end' logfile.log | sed 's/.*{/{/' | jq 'select(.outcome=="failed")'
```

## Troubleshooting

### "Missing required parameter"

You forgot to pass a required parameter. Check the workflow's `params` section and pass values as positional arguments.

### "Step not found in workflow"

The `--from` step ID doesn't match any step in the workflow. Check `agent-runner validate` output for the list of step IDs.

### "Unknown engine type"

The engine type in the workflow doesn't match any registered engine. Currently only `openspec` is built-in.

### "openspec CLI not found"

The openspec engine requires the `openspec` CLI on your PATH. Install it and try again.

### Interactive step won't advance

Exit the agent session to advance to the next step. If the agent session is stuck, press ctrl-c to terminate it.

### Workflow interrupted, how to resume

Find the `agent-runner-state.json` file (in the project root or the engine's state directory) and run:

```bash
agent-runner resume agent-runner-state.json
```
