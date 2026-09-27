## Context

`claude -p` keeps its process alive for `Monitor` watches but not for background Bash tasks. When the model ends its turn with a background Bash task pending, the CLI emits `result` (`subtype: success`, `terminal_reason: completed`) and then kills the task (`system/task_updated` with `patch.status: killed`, followed by `system/task_notification` with `status: stopped`). The model does this because in interactive use a finished background task wakes it up again; in print mode nothing does.

Covers GitHub issues #151 (root cause) and #153 (safety net).

## Evidence

Verified on Claude Code 2.1.282, `claude -p --output-format stream-json --verbose`, model Haiku. Raw streams were kept in the planning session's scratchpad.

| Case | Without flag | With `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1` |
|---|---|---|
| Bash `run_in_background: true` | Task started, killed right after `result` | `InputValidationError: An unexpected parameter run_in_background was provided` |
| Foreground command, auto-background forced at 5s (`CLAUDE_AUTO_BACKGROUND_TASKS=1`, `CLAUDE_CODE_AUTO_BACKGROUND_TIMEOUT_MS=5000`) | Moved to background at 5s, killed after `result` | Ran in the foreground to completion |
| Foreground command starting with `sleep` | Refused, pointing at `run_in_background` or `Monitor` | Ran |
| `Agent` tool asked to run in the background | Not tested | Ran in the foreground and returned its report |
| `Monitor` | Not tested | Still available; `-p` stayed alive and produced one `result` per monitor event until the source ended |
| Foreground command past `BASH_MAX_TIMEOUT_MS` | Not tested | Killed with exit 143; the model receives `Command timed out after …` |

The flag is read in the Claude binary as `settings.backgroundTasksDisabled || env.CLAUDE_CODE_DISABLE_BACKGROUND_TASKS`.

Validator durations: across 64 local `validation-metrics.json` invocations, the longest run took 4.3 minutes and typical runs took 2 to 4 minutes. Claude's default Bash timeout is 2 minutes and its max is 10 minutes.

## Decisions

### Prevent background tasks instead of detecting kills

Disabling background tasks removes the failure at its source for every headless Claude invocation. Stream detection, auto-resume, and prompt guidance were considered and rejected: detection needs undocumented event parsing and still loses the work, auto-resume adds a new recovery mechanism, and the autonomy preamble is not sent on resumed sessions (where #151 occurred).

### Use the process environment, not `--settings`

The Claude adapter already contributes process-local configuration, and `SpawnEnvContributor` (`internal/cli/adapter.go`) is the existing hook that both the workflow agent path (`internal/exec/agent.go`) and the `call_agent` child path (`internal/exec/agent_call.go`) consume. The headless process runner applies these entries through `BuildAgentEnvironment` (`internal/liverun/process_runner.go`). The adapter's existing `--settings` JSON is only emitted for the completion Stop hook, so reusing it would couple two unrelated features.

Implement `SpawnEnv` on `ClaudeAdapter`, returning entries only when `input` describes a headless invocation. Update the `SpawnEnvContributor` doc comment, which still says entries apply only to interactive-backend spawns.

### Raise only the default Bash timeout

`BASH_DEFAULT_TIMEOUT_MS=600000` lets a foreground validator run finish without the model passing an explicit timeout. `BASH_MAX_TIMEOUT_MS` stays at Claude's default (10 minutes), which is above every observed validator run. A user-provided `BASH_DEFAULT_TIMEOUT_MS` in the runner's environment wins. The disable flag always wins, because allowing background tasks in headless runs is the defect.

### Leave `Monitor` available

`-p` keeps running while a monitor is active and wakes the model on each event, so monitors do not lose work.

### #153: YAML-only inline repair without tester calls

Inline repair steps are synthesized without `tools` (`internal/exec/repair.go`), so the repair cannot call `acceptance-tester`. Rather than extending the repair schema, the repair finishes in-progress work and reports honestly. If a targeted retest is still owed, it writes `ACCEPTANCE_FAILED` and names the owed retest in the handoff. With background tasks disabled, this repair mainly covers other early endings (crash, forgotten outputs, turn ended mid-fix).

Per project convention, this workflow YAML change has no spec delta.

Add this block to `verify-acceptance-handoff` in `workflows/core/implement-change-v1.0.yaml`, leaving its `command` unchanged:

```yaml
    repair:
      session: lead-agent
      max: 1
      prompt: |
        The acceptance-preparation step ended before its required outputs existed: `{{session_dir}}/output/acceptance-handoff.md` is missing or empty, or `{{session_dir}}/output/acceptance-preparation-status.txt` does not hold `ACCEPTANCE_COMPLETE` or `ACCEPTANCE_FAILED`.

        Resume the acceptance preparation where it stopped, following the original step's instructions: finish any in-progress fix, run the required checks and validation in the foreground and wait for them, then commit, push, and verify local `HEAD` equals the draft PR head. You cannot call the acceptance tester in this repair.

        Then write both files so they honestly reflect the result. Write `ACCEPTANCE_COMPLETE` only if every required or activated `AT-*` already has passing evidence for the current HEAD. If any tracked change since the last tester pass still needs targeted re-testing, write `ACCEPTANCE_FAILED` and state in `acceptance-handoff.md` which `AT-*` obligations still need re-testing and why.

        Never declare success yourself; the check that runs after you decides. If only a human can resolve the failure, make no unsafe change, explain why, and end your response with the line `REPAIR_BLOCKED`.
```

## Risks and Trade-offs

- `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS` is not a documented contract. If a future Claude CLI ignores it, behavior reverts to today's, not worse.
- Headless Claude agents can no longer run work in parallel via background tasks; long waits must be foreground commands or `Monitor` watches.
- A hung foreground command now takes up to 10 minutes to time out instead of 2.
- If a validator run exceeds 10 minutes, the command times out visibly (exit 143) and the model must handle it; `BASH_MAX_TIMEOUT_MS` would then need raising.
