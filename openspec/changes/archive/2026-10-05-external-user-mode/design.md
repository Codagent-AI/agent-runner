## Context

Interactive agent steps today go through `runDirectInteractive` (`internal/exec/agent.go`), which hands the terminal to the CLI. `internal/interactive/runner.go` (around 285-302) fails with "stdin is not a terminal" without one. Completion arrives over the per-run control socket (`internal/control`) from `agent-runner step complete`. The step succeeds after the acknowledgement and semantic turn-durability rules of `step-control-channel`.

Headless (`autonomous-headless`) invocations already:
- capture stdout;
- parse usage;
- resume sessions (`claude --resume <id> -p …`, `codex … exec … resume --json <id> <prompt>`);
- receive control-socket environment through `ControlServer.ActivateAttempt` when they declare `call_agent` (`prepareAgentCallRuntime`).

They get no completion command: `completionExecutableForContext` returns `""`, and `validateCompletionIntegration` rejects one.

The proof of concept lives at `/Users/paul/codagent/agent-evals.eval-the-whole-workflow/openspec/changes/eval-the-whole-workflow/poc/`; see `README.md` and `headless-turns-poc.mjs`. It ran the real `proposal` step prompt from `workflows/core/define-change-v1.0.yaml` as headless turns, outside Agent Runner:
- 2 Claude runs (`claude-opus-5-5`) and 2 Codex runs (`gpt-6.1-sol`, high effort), all on the host;
- every run asked plain-text questions, waited, wrote the proposal after approval, and signalled completion in 3–4 turns;
- no lead took the `codagent:ask-questions` headless branch in any of 15 turns.

It substituted a marker-file `touch` for `agent-runner step complete` and skipped the crosscheck `call_agent`. It did not exercise step transitions, resume, or the sandbox.

## Goals / Non-Goals

**Goals**
- Implement the `external-user-mode` spec, the `run-until` resume cap, `call_agent` pre-authorization, and turn usage.
- Make the built-in change workflow runnable in a repo with no remotes.

**Non-goals**
- Cursor, Copilot, or OpenCode leads.
- `AskUserQuestion` translation.
- Any isolation between the responder and the agent beyond the file contract.
- Any change to TTY behavior when the mode is off.

## Decisions

### 1. A new invocation context, not a flag on the existing ones

Add `cli.ContextExternalUser` (`"external-user"`) in `internal/cli/adapter.go`:
- `IsHeadless()` is true: no TTY, stdout captured, headless argv;
- `IsAutonomous()` is false: no autonomy preamble;
- `IsInteractive()` is false: no direct handoff.

`resolveInvocationContext` returns it for `ModeInteractive` when `ctx.ExternalUser != nil`. For `ModeAutonomous` in external-user mode, it returns `ContextAutonomousHeadless` regardless of `AutonomousBackend`.

Audit every `IsHeadless()` and `IsAutonomous()` call site; this is the main implementation risk. The known sites that must special-case the new context:

| Site | Required behavior for `external-user` |
|---|---|
| `completionExecutableForContext` | return the executable, as for interactive |
| `validateCompletionIntegration` | require the completion command, as for interactive |
| `buildAdapterInput`: `DisallowedTools` | include `AskUserQuestion` |
| `buildAdapterInput`: autonomy preamble | omit; already keyed on `IsAutonomous()` |
| `buildAdapterInput`: prompt delivery switch | use the interactive branches (system prompt plus "Let's start the X step" for fresh Claude, the full prompt in the user message on resume, Codex's bare prompt or `<system>` wrapper), not the headless `input.Prompt = fullPrompt` branch |
| `continueMarkerPromptNeedsRefresh` | behave as interactive |
| Claude `--allowedTools mcp__agent-runner__*` and Codex agent-call pre-approval | pre-authorize, as for autonomous |
| Codex `--sandbox` (only added for `IsAutonomous()` today) | add it, using the autonomous permission mapping (`workspace-write`, or `danger-full-access` for YOLO) |
| Claude `--permission-mode` | the autonomous permission setting, as for headless today |
| `prepareAgentCallRuntime` / control activation | activate the attempt with `CompletionEligible: true` and the adapter's durability `Checkpoint`, as the direct runner does |
| usage (`defaultAgentUsage`, `interactive-context`) | parse turn usage as headless |
| `interactiveModeError` | not applicable; the rejection is per CLI instead (decision 6) |

Leave the existing three contexts unchanged.

### 2. Where the loop lives

Keep it in `internal/exec`, in a new `external_user.go`, so executors stay together per the architecture notes. `ExecuteAgentStep` branches when the context is `external-user`. The branch:
1. activates one control attempt for the whole step attempt; its credential env applies to every turn;
2. builds and runs turn 1 through the existing headless `InvokeAgent` path;
3. after each turn's process exits:
   - if completion was accepted, finishes with the existing acknowledgement and durability wait (decision 4);
   - if the process exited non-zero, fails;
   - otherwise writes the request and waits for the reply;
4. runs the next turn with `isResume=true`, the reply verbatim as `input.Prompt`, and no step prefix, completion instruction, or system prompt. The step attempt's first turn already delivered the completion instruction. The PoC proved this survives resume: completion came on turn 4 after a single delivery.

Every turn's process gets stdin from `/dev/null`. An inherited pipe that never closes, such as under `docker exec -i`, makes Codex block on "Reading additional input from stdin...".

The session ID comes from the first turn: Claude's `--session-id` is known before spawn, and Codex's comes from `thread.started` (the existing discovery). Persist it before turn 2 so resume works mid-step.

Put the file protocol in a small dependency-free package, `internal/externaluser`. It covers request and reply types, step-key sanitizing, atomic write (temp file plus rename in the same directory), the reply wait (poll about every 250 ms; honor context cancellation and the optional timeout), and reply validation.

### 3. Turn text

The request's `agent_message` is all assistant text of the turn:
- Codex: `extractCodexAgentMessages` already joins a turn's `agent_message` items. Use it, not the last message. The PoC's last-message relay made the responder approve a scope it never saw.
- Claude: `FilterOutput` returns only `result.result`, which is the last text block. Add a helper that joins every assistant text block in the turn's stream-json output, in order. Skip messages with a non-null `parent_tool_use_id`, which are subagent (Task or Explore) output the user never sees. Keep `FilterOutput` unchanged for capture.

`empty_turn` is true when the joined text is empty after trimming.

### 4. Completion in headless turns

The control server already accepts `complete_step` from any process that carries the attempt env. In this mode:
- the attempt's first turn carries the completion instruction in its prompt, as an interactive launch does. Reply turns don't (decision 2). Every turn's argv carries the adapter's completion integration and control env;
- a replayed first turn (decision 5) is exempt and sends the recorded reply verbatim. The replay record stores the completion command delivered in each session. If the current command differs (for example after a rebuild or a different `AGENT_RUNNER_EXECUTABLE`), append the current completion instruction after the reply and emit an audit event saying it was appended;
- the adapters' completion integration (the Claude plugin command and Stop hook, Codex `notify`) is injected for the context.

After a turn's process exits, the loop asks the attempt whether completion was accepted. If it was, it reuses the existing durability wait (`WaitForCommittedTurn` from the adapter's `TurnDurabilityProbe`, checkpointed at acceptance). A headless process exiting on its own is not durability evidence.

If completion is accepted while the turn is still running, the existing post-acknowledgement path may terminate the process group. This is the same behavior as interactive.

**Verify early in implementation** that Claude `-p` fires the plugin Stop hook and that Codex `exec` honors `notify`. If either does not, the store probe alone is sufficient evidence under `step-control-channel`. Record the result in the PR.

### 5. Replay store and resume

Record exchanges in `<run session dir>/external-user/exchanges.jsonl`, one line per event, each appended and fsynced before the action it records:
- `request_written`, with its identity and the full request body. It is appended BEFORE the request file is renamed into the exchange directory (write-ahead), so a crash can never leave a published request that the record doesn't know about;
- `reply_acted`, with its identity, the reply text or abort reason, and the time;
- `turn_finished`, with its identity.

This is the authoritative replay record. The exchange directory is only transport.

On re-entering an interactive step in this mode, read the last events for that step key:

- **Last event is `reply_acted` (text) with no later `turn_finished`:** resend that text as the first user turn of the new attempt.
- **Last event is `request_written` with no `reply_acted`:** if the request file is missing from the exchange directory, re-materialize it from the recorded body (atomic write). Then wait on its reply file under its original identity. Covers interruption, timeout, a malformed reply (the responder must replace the file), and a crash between recording and publishing.
- **Otherwise:** use the existing interactive resume message (`Resume the <step> step.` plus the completion instruction).

Turn numbering restarts at 1 for the new attempt. The attempt number comes from the audit pipeline (`attemptForIdentity`). An abort reply is recorded as `reply_acted` with action `abort` and is terminal: a later resume uses the standard resume message.

Exchange-directory hygiene:
- A fresh `run` refuses a directory that already holds `*.request.json` or `*.reply.json` (file names carry no run ID).
- Writing a new request fails the step if its `.reply.json` already exists. That would be a stale file, or one pre-written by the agent itself, which can write anywhere under YOLO permissions. Replay waits are exempt.
- Request files are written with mode 0644, because host and container UIDs differ across the bind mount.

### 5a. Codex argv: `--` before positionals

`CodexAdapter.BuildArgsWithError` appends the session ID and prompt as bare positionals. clap then parses a prompt that starts with `-` as an option. Verified on codex-cli 0.160.0:
- `codex … exec --skip-git-repo-check resume --json <id> "- option a"` fails with `error: unexpected argument '- ' found`;
- `codex … exec --skip-git-repo-check resume --json -- <id> "- option a"` parses: it got as far as "no rollout found" for a dummy ID.

Insert `--` before the positional session ID and prompt in every Codex argv form. This also fixes the same latent bug for fresh, headless, and interactive Codex prompts. Verify that the interactive `codex resume --no-alt-screen -- <id> <prompt>` and the fresh `codex --no-alt-screen -- <prompt>` forms parse too. Add adapter tests with a prompt of "- option a".

### 6. CLI support check

Before spawn, an interactive step in this mode whose resolved CLI is not `claude` or `codex` fails with "external-user mode supports only Claude and Codex leads; step <id> resolved to <cli>". The general loop has no Claude- or Codex-specific logic beyond the turn-text helper, so widening later is a list change plus proof.

### 7. Flags, state, and resume

- `RunState` gets `ExternalUser *ExternalUserSettings {Dir string; Timeout string}`, with `omitempty`.
- `ExecutionContext` gets the resolved settings, so child contexts inherit them.
- `runner.Options` gets `ExternalUser` and resume reads it from state.
- In `cmd/agent-runner/main.go`:
  - the run path parses `--external-user` and `--external-user-timeout`, validates the directory (exists, is a directory, and is writable, checked with a probe temp file that is then removed), and sets `AGENT_RUNNER_NO_TUI=1`;
  - the resume path rejects both flags, and if the state has `ExternalUser`, takes the no-TUI branch;
  - `-i` combined with `--external-user` is rejected.
- `--resume` accepts `--until`. Wire it into the `ResumeWorkflow` and `PrepareResume` options (currently no `Until` is passed; around lines 936-941) and validate with `validateUntilStep` plus a check that the target is not before the resume point.

### 8. Interactive shell and UI steps

In external-user mode, the interactive-shell and UI step executors fail immediately with "<kind> steps are not supported in external-user mode". The built-in define path has neither.

### 9. `validate-feature-branch` without remotes (workflow YAML)

This is behavior of a built-in workflow, so it is recorded here rather than as a spec delta. In `workflows/core/validate-feature-branch-v1.0.yaml`:
- If `git remote` lists nothing, determine the default branch locally: `git config init.defaultBranch` if that branch exists, else `main` if it exists, else `master` if it exists. If none exists, fail with "could not determine the default branch: the repository has no remotes and no main or master branch".
- Otherwise keep the current `gh repo view` path and its error unchanged.
- The detached-HEAD and on-default-branch failures are unchanged.

No test is needed for the YAML edit (project rule). Exercise it in the flow test.

### 10. Usage

Each turn's `cli.UsageExtraction` is collected as headless. The step attempt's usage is the aggregate of the turns' attributed usage, following the existing "Attribution follows source counter semantics" and "Per-step attribution for cumulative usage sources" requirements:
- **Claude `-p`:** reports per invocation. Record each turn directly and never subtract.
- **Codex:** `ExtractUsage` reports session-cumulative totals (`RawCumulative`, `internal/cli/codex.go` around line 348). Attribute each turn by the difference from the previous recorded snapshot for that session, with the existing reset and no-baseline safeguards. The loop sees every turn of the step, so a baseline exists after the session's first turn in the run. A named session resumed from an earlier step in the same run already has one.

A turn whose usage is unavailable makes the aggregate partial.

### 11. Docs

Add `docs/external-user-mode.md`. It covers:
- the flags;
- the supported capability probe: `agent-runner --help` lists `--external-user`;
- the exchange-file contract: names, JSON fields, atomic writes on both sides, abort, timeout, and replay;
- the responder's obligations:
  - write replies atomically, with temp file plus rename;
  - replace a malformed reply, because a resume re-reads it and fails again;
  - start each run with an empty directory;
  - use file modes readable across the bind mount, for example 0644;
- supported CLIs;
- the permission note: turns use the autonomous permission setting, and the caller's sandbox is the boundary;
- how a caller tells a `--until` stop apart from an interruption without resuming. Exit code 0 with `stopped after step "<id>" (--until).` is one signal. Verify and document what `state.json` (`currentStep`, `completed`) and the audit log's top-level `step_end` record in each case;
- where each headless turn's raw CLI output is retained in the run directory, for the eval's transcript audit. Check whether the existing headless output capture keeps per-invocation raw stdout. If it does, document the path. If it doesn't, say so: callers then collect the CLI session files. This change adds no new retention.

Link it from the docs index and mention `--resume … --until`.

## Risks / Trade-offs

- **Context call-site audit.** A missed `IsHeadless()` or `IsAutonomous()` check would, for example, add the autonomy preamble or drop the completion command. Mitigation: table-driven tests over `buildAdapterInput` and the Claude and Codex argv for the new context.
- **Prompt and permission drift from the PoC.** The PoC's Claude lead used `--permission-mode acceptEdits` with an allowlist; the Runner uses the user's autonomous permission setting. In the eval sandbox this is `yolo`, which the eval accepts. Denials, not prompts, are the failure mode on hosts.
- **Polling the exchange directory.** This adds up to about 250 ms of latency per turn. It is negligible next to model turns and avoids an fsnotify dependency.
- **Durability evidence in headless turns** is unproven (decision 4); the store probe is the fallback.
- **Environment leakage when the Runner itself runs under Claude Code.** Claude's `DropSpawnEnvVars` already drops `CLAUDECODE` and session variables. Confirm it also covers `CLAUDE_CODE_SESSION_ATTENDED` and `CLAUDE_EFFORT`, which the PoC saw leak.

## Migration

None. The mode is off unless requested, and the new state field is optional.

## Verification

- Unit tests:
  - the `internal/externaluser` protocol (names, sanitizing, atomic write, validation, timeout, cancellation);
  - the exec loop with fake process runners (multi-turn, completion, abort, timeout, malformed reply, non-zero exit, empty turn, replay cases);
  - adapter argv and input for the new context;
  - CLI flag validation;
  - resume `--until`.
- Flow test with a scripted responder (a shell loop that answers each `.request.json`) driving a small interactive test workflow. Use real Claude and Codex where available.
- The eval's end-to-end acceptance runs in agent-evals, not here. It requires `proposal` with crosscheck and `approach-review`, Claude and Codex, inside `sandbox-run.sh --no-default-secrets`, and a capped resume.
