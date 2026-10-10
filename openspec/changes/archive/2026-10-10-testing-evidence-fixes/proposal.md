## Why

Headless Claude agent steps can end their turn while a background Bash task is still running. `claude -p` does not keep the process alive for background Bash tasks: it emits the `result` event, exits, and kills the task. The step is recorded as `success`, and the lost work surfaces later as a misleading downstream check failure (#151). In the observed run, `prepare-acceptance` started Agent Validator with `run_in_background`, ended its turn with "I'll pick up the result when it finishes", and `verify-acceptance-handoff` then failed with `acceptance handoff is missing or empty`. A local acceptance run on 2026-08-28 shows the same pattern about 15 times in `call_agent` tester children.

Separately, `verify-acceptance-handoff` in `implement-change` is the only `verify-*` check in that workflow without a `repair` block. When `prepare-acceptance` ends without its required outputs for any reason, the run fails outright, and `--resume` restarts at the check, which fails again immediately (#153).

## What Changes

- The Claude adapter disables background tasks for every headless Claude invocation by setting `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` in the spawned process's environment. This removes `run_in_background` from the Bash and Agent tools and turns off auto-backgrounding of long foreground commands, so no task can be killed after the turn ends.
- The same invocations get `BASH_DEFAULT_TIMEOUT_MS=600000` (unless the runner's own environment already sets it), so a foreground validator run that takes longer than Claude's two-minute default is not killed and retried.
- `verify-acceptance-handoff` in `workflows/core/implement-change-v1.0.yaml` gets an inline `repair` that resumes the `lead-agent` session to finish the interrupted work and write honest acceptance outputs, without further tester calls.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `cli-adapter`: adds a requirement that headless Claude invocations run with background tasks disabled and a 10-minute default Bash timeout, applied only to the spawned process.

## Out of Scope

- Detecting killed background tasks in the Claude stream, auto-resuming a session, or adding a new failure outcome.
- Prompt changes to the autonomy preamble or step prompts about background tasks.
- Codex, Cursor, Copilot, and OpenCode adapters.
- Interactive Claude invocations, which keep the session alive and are not affected by this failure.
- The `Monitor` tool, which stays available: `-p` keeps the process alive and wakes the model for monitor events, so it does not lose work.
- Adding `tools` to inline repair blocks, or allowing the #153 repair to call the acceptance tester.
- Extending the run-level classified failure reason to agent-step failures.

## Impact

- `internal/cli/claude.go`: the Claude adapter implements `SpawnEnvContributor` for headless invocations. This covers workflow agent steps, inline repair agents, and `call_agent` children, which all resolve spawn env through `cli.SpawnEnvForInvocation`.
- `internal/cli/adapter.go`: the `SpawnEnvContributor` doc comment currently says contributed entries apply to interactive-backend spawns; update it to reflect headless use.
- `workflows/core/implement-change-v1.0.yaml`: new `repair` block on `verify-acceptance-handoff`.
- Behavior for users: headless Claude agents can no longer background work; long commands run in the foreground up to 10 minutes by default. A hung command now takes up to 10 minutes to time out instead of 2.
