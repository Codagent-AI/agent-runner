# Task: Agent Executor + Documentation

## Goal

Refactor the existing agent step execution logic from `runner.ts` into a standalone AgentExecutor, add per-step model override, add ctrl-c handling for headless mode, and update all documentation to cover the new loop, sub-workflow, and flow control features.

## Background

### Current Agent Execution

Agent step execution is currently inline in `src/runner.ts` (the `runAgentStep` function). It:

1. Interpolates params in the prompt
2. Appends engine enrichment (`engine.enrichPrompt()`) if available
3. Builds args: `['claude']` + optional `--resume <sessionId>` + optional `-p` flag + prompt
4. Spawns subprocess with stdin/stdout/stderr inheritance
5. For interactive mode: waits for `.baton-signal` file via polling (`waitForSignalOrExit`)
6. For headless mode: waits for process exit
7. Finds conversation ID from `~/.claude/projects/<encoded-cwd>/` JSONL files

This logic moves to `src/executors/agent.ts`. The dispatcher (already implemented) routes `prompt`/`mode` steps to the AgentExecutor.

### Per-Step Model Override

The `model` field on a step specifies which model the agent should use. When present, add `--model <value>` to the claude invocation args. When absent, no model flag is added. The `model` field is only valid on agent steps — schema validation (already implemented) rejects it on shell steps.

### Headless Mode Ctrl-C

When a headless agent step is running and the user sends SIGINT (ctrl-c), baton must:

1. Kill the spawned agent subprocess (`proc.kill()`)
2. Allow the normal exit flow to persist state (the state file should reflect the interrupted step)
3. Exit baton with a non-zero exit code

Register a SIGINT handler before spawning the headless subprocess. Remove the handler after the process exits normally. The handler should kill the child process and then allow baton to exit.

### Session Resolution

Session handling uses the resolution functions in `src/shared/session.ts` (already implemented):

- `session: new` — no `--resume` flag, start fresh. Store new conversation ID in context's `sessionIds`.
- `session: resume` — find most recent session in current context's `sessionIds`. Pass as `--resume <id>`.
- `session: inherit` — walk `parentContext` chain across sub-workflow boundary, return parent's most recent session.

### Signal File Handling (Interactive Mode)

Interactive mode polls `.baton-signal` for JSON `{ action: 'continue' | 'abort' }` every 500ms. If the process exits before a signal, the step is treated as aborted. The signal file constant is `SIGNAL_FILE = '.baton-signal'`.

### Conversation ID Discovery

After an agent step completes, the conversation ID is discovered by scanning `~/.claude/projects/<encoded-cwd>/` for the most recently modified JSONL file. The encoded path replaces `/` and `.` with `-`.

### Engine Hooks

- `engine.enrichPrompt(stepId, params)` — returns additional context to append after the step's prompt
- `engine.validateStep(stepId, params)` — returns boolean; if false, prompts user to resume or exit

Validation failure handling (`handleValidationFailure`) prompts the user: "Resume previous session [r] / Exit [q]". On 'r', spawns `claude --resume <sessionId>` and re-validates. On 'q', stops workflow.

### Key Files

- `src/executors/agent.ts` — create AgentExecutor here
- `src/runner.ts` — extract `runAgentStep`, `waitForSignalOrExit`, `findConversationId`, `handleValidationFailure` into the executor. The dispatcher already routes agent steps here.
- `src/shared/session.ts` — session resolution (already implemented)
- `README.md` — update with new features
- `docs/USER-GUIDE.md` — update with loops, sub-workflows, flow control, capture, model override
- `docs/LOOPS-AND-SUBWORKFLOWS.md` — replace proposal content with actual documentation of implemented features

### Documentation Updates

**README.md**: Add loops, sub-workflows, flow control, and output capture to the feature overview. Update the workflow format reference with the new fields (`steps`, `loop`, `workflow`, `capture`, `continue_on_failure`, `skip_if`, `break_if`, `model`, `params` on workflow steps). Update the architecture section if it references the flat runner.

**docs/USER-GUIDE.md**: Add new sections covering:
- Loops: counted loops (`loop: { max: N }`), for-each loops (`loop: { over, as }`), `break_if`
- Sub-workflows: invocation, parameter passing, `session: inherit`
- Flow control: `continue_on_failure`, `skip_if: previous_success`
- Output capture: `capture` field, tee behavior, `{{var}}` interpolation
- Per-step model override: `model` field
- Updated session management: `session: inherit` in addition to `new`/`resume`
- Reference the decomposed flokay workflow (`workflows/implement-change.yaml` etc.) as an example

**docs/LOOPS-AND-SUBWORKFLOWS.md**: This file currently contains the proposal/motivation for the feature. Replace its content with actual documentation of the implemented feature set — syntax reference, examples, and behavior details.

### Codebase Conventions

- **Linting:** biome with max 500 lines per file, max 75 lines per function, max cognitive complexity 15
- **Testing:** `bun test` with `bun:test` framework
- **Formatting:** single quotes, always semicolons

### No E2E Test

Agent steps spawn `claude` as a subprocess, which is not available in test environments. This task uses unit tests only, with the existing mock patterns: mock `Bun.spawn`, verify args, simulate exit codes.

## Spec

### Requirement: Per-step model override

A step MAY include a `model` field specifying which model the agent should use. When present, baton SHALL pass the model to the agent invocation. When absent, the agent uses its default model. The `model` field is only valid on agent steps (headless or interactive), not shell steps.

#### Scenario: Model specified on agent step
- **WHEN** a headless step has `model: sonnet`
- **THEN** baton passes the model flag to the claude invocation

#### Scenario: No model specified
- **WHEN** a step does not have a `model` field
- **THEN** baton invokes the agent without a model flag, using the agent's default

#### Scenario: Model on shell step
- **WHEN** a shell step has a `model` field
- **THEN** baton fails at load time with a validation error

### Requirement: Headless mode ctrl-c termination

When an agent step is running in headless mode, pressing ctrl-c (SIGINT) SHALL terminate the spawned agent subprocess and exit baton cleanly. The user must be able to interrupt a long-running headless agent step without leaving orphaned processes.

#### Scenario: Ctrl-c terminates headless agent subprocess
- **WHEN** a headless agent step is running and the user sends SIGINT (ctrl-c)
- **THEN** baton kills the spawned agent subprocess and exits with a non-zero exit code

#### Scenario: State file preserved on ctrl-c
- **WHEN** a headless agent step is interrupted via ctrl-c
- **THEN** the state file reflects the interrupted step so the workflow can be resumed

## Done When

- AgentExecutor handles headless and interactive modes, session resolution, engine enrichment, and validation failure flow.
- `--model <value>` is passed to claude when `model` is set on the step.
- Ctrl-c during headless mode kills the subprocess and preserves state for resume.
- All documentation (README, USER-GUIDE, LOOPS-AND-SUBWORKFLOWS) is updated to reflect the full feature set including loops, sub-workflows, flow control, capture, and model override.
- Unit tests cover model override, session strategies, and ctrl-c behavior.
