## Coverage Strategy

The specifications remain the source of unit-test requirements. This plan records only the
additional integration and end-to-end obligations, the acceptance testing envelope, and any
human-only obligations.

The behavior change is one pure mapping in the Claude adapter, and the unit tests in
`internal/cli/adapter_test.go` listed in design.md cover every cell of it. The remaining risk sits
at the runner→adapter boundary in `internal/exec`. That boundary is where the invocation context is
resolved (including the case where a capture step is forced to headless), where the user's permission
setting is passed in, and where `AskUserQuestion` and the autonomy preamble are added. The integration
obligations below target that boundary through the existing exec-level seams: `interactiveRunnerFn`
for direct interactive launches and `mockRunner.RunAgent` for headless launches.

No new end-to-end test is warranted. The only public-surface difference is the argument vector passed
to a real `claude` process on the user's terminal. The PTY smoke harness
(`cmd/agent-runner/smoke_interactive_integration_test.go`) replaces the agent CLI with a fixture, so an
E2E would assert the same argument vector INT-001 already checks, at higher cost and with more
flakiness. The settings-file → `ExecutionContext.AutonomousPermissionMode` wiring has not changed and
is already covered by existing settings and runner tests.

## Integration Tests

### INT-001: An autonomous-interactive yolo Claude step launches in auto mode with the autonomy contract intact
- Covers: cli-adapter "Adapters honor autonomous permission mode": the "Claude yolo
  autonomous-interactive uses auto mode", "Claude yolo autonomous-interactive resume uses auto mode"
  and "Claude auto mode does not relax the autonomy contract" scenarios. Also the modified "No
  permission loosening in interactive mode" (an autonomous-interactive step still counts as
  autonomous).
- Boundary: `ExecuteAgentStep` → `resolveInvocationContext` → building `BuildArgsInput` (permission
  mode, disallowed tools, autonomy preamble) → `ClaudeAdapter.BuildArgsWithError` → the direct
  interactive launch.
- Setup: in `internal/exec/agent_test.go`, stub `interactiveRunnerFn` to record its args and return
  `interactive.DirectResult{Completed: true}`, and stub `isStdinTerminal` to return true. Use
  `makeCtx()` with `AutonomousBackend = "interactive-claude"` and `AutonomousPermissionMode = "yolo"`,
  and an autonomous step for the Claude CLI. Run two cases: a `SessionNew` step, and a
  `SessionResume` step with a prior session ID recorded (`ctx.SessionIDs["prev"]`, `LastSessionStepID`).
- Action: call `ExecuteAgentStep`.
- Assertions, both cases: no headless `RunAgent` call is made, and exactly one direct interactive launch
  happens. Its args contain `--permission-mode` followed by `auto`, contain no `bypassPermissions`, and
  contain `--disallowedTools` followed by `AskUserQuestion`.
- Assertions, fresh case only: the `--append-system-prompt` value begins with the autonomy preamble.
- Assertions, resume case only: the args contain `--resume <id>` and no `--append-system-prompt`. The
  positional prompt carries the current step instructions and the completion instruction (use the
  existing `assertControlCompletionInstruction` helper) and does not contain the autonomy preamble.
  This matches the existing resume behavior, which this change does not touch. The test asserts only
  launch args; it does not try to verify that the earlier session history contains the preamble.
- Execution: `go test ./internal/exec` (the CI `test` job's `go test ./...`).

### INT-002: Yolo Claude steps that resolve to headless keep bypassPermissions, including capture-forced steps
- Covers: cli-adapter "Adapters honor autonomous permission mode": the "Claude yolo
  autonomous-headless keeps bypassPermissions" and "Capture-forced headless Claude step keeps
  bypassPermissions" scenarios.
- Boundary: `ExecuteAgentStep` → capture forcing headless in `resolveInvocationContext` →
  `ClaudeAdapter` → `mockRunner.RunAgent`.
- Setup: `makeCtx()` with `AutonomousPermissionMode = "yolo"`, and a `mockRunner` returning
  `claudeResultOutput(...)`. Run two cases. (a) `AutonomousBackend = "interactive-claude"` and an
  autonomous step with `Capture` set. (b) `AutonomousBackend = "headless"` and an autonomous step
  without capture.
- Action: call `ExecuteAgentStep`.
- Assertions: no direct interactive launch happens. The recorded `RunAgent` args contain `-p` and
  `--permission-mode` followed by `bypassPermissions`, and do not contain `auto` as the
  `--permission-mode` value. In case (a) the captured variable is set.
- Execution: `go test ./internal/exec` (the CI `test` job).

## End-to-End Tests

None. See Coverage Strategy.

## Acceptance Testing Envelope

- Environments and sandboxes: the local checkout, run from source with `./dev.sh` (no `make build`
  is needed). Use scratch directories under `$TMPDIR` for any throwaway workflow. A synthetic PTY
  (Python `pty.fork` plus `pyte`) can drive the settings editor (`s` in the browser) and native setup
  to read the YOLO copy. Per the repository guidance, it cannot start runs from the browser.
- Credentials and secrets: the Claude CLI on this machine (Claude Code 2.1.296) is signed in to the
  user's account. No other credentials are needed or available.
- Authorized effects:
  - A small number of short real `claude` invocations to confirm that `--permission-mode auto` is
    accepted, for example `claude --permission-mode auto -p "reply ok"` in a scratch directory, a few
    cents at most.
  - Running autonomous-headless `./dev.sh` workflows that use Claude with a trivial prompt.
  - Temporarily setting `autonomous_permission_mode` in `~/.agent-runner/settings.yaml`, only after
    backing the file up, and restoring it byte-for-byte afterwards.
- Off limits:
  - Leaving the user's `~/.agent-runner/settings.yaml` modified.
  - Editing `~/.claude` settings, including `crossSessionInbound` and `defaultMode`.
  - Messaging, resuming or interfering with any other live Claude or Agent Factory session on the
    machine. The only exception is the two dedicated, test-owned sessions HT-001 creates in scratch
    directories.
  - Pushing, opening PRs or filing issues as part of acceptance.
  - Any workflow that edits real repositories other than scratch directories.
- Permitted substitutes:
  - Where a real autonomous-interactive Claude launch cannot be driven from a synthetic PTY, check
    the exact launch args through the exec-level tests (INT-001) or through run audit and launch
    evidence that records argv, and say which one was used.
  - Run the "auto unavailable" scenario at the argument level only. Do not toggle `disableAutoMode`
    in real Claude settings.
- Known risk areas:
  - Context mapping drift: external-user must stay unchanged, conservative must never emit `auto`,
    and plain interactive must emit no `--permission-mode`.
  - Capture steps silently becoming headless even when the user picked an interactive backend.
  - Accepted limitations (design.md): no detection or fallback when `auto` is unavailable (Claude
    starts in Manual); Claude CLIs that predate `auto` fail at launch; steps waiting on a Claude
    permission prompt are not shown in the run view; messages between `auto` steps and bypass-class
    peers (headless yolo, capture steps) are still held by default.
  - Spec overlap with the in-flight `make-pty-great` change in `cli-adapter`.

## Human-Only Testing

### HT-001: A real autonomous-interactive yolo Claude step runs in auto mode and receives cross-session messages
- Reason: this needs Agent Runner to hand a real terminal to a live Claude conversation and a second
  ordinary Claude session to message it. Repository guidance says starting runs, and any flow that
  needs a real conversation with an agent, still require a human at a real terminal. A synthetic PTY
  cannot launch the run.
- Prerequisites: INT-001 and INT-002 pass, and acceptance has confirmed that real `claude` accepts
  `--permission-mode auto` and that the YOLO copy renders. Both sessions must be dedicated and
  test-owned. Never use a live user or Agent Factory session as the sender or receiver.
- Instructions:
  1. Back up `~/.agent-runner/settings.yaml`, then set `autonomous_permission_mode: yolo` and
     `autonomous_backend: interactive-claude`. Do not change any Claude settings.
  2. Create two scratch directories under `$TMPDIR`. Neither may contain `.claude/settings*.json`.
  3. Receiver: in the first directory, in a real terminal, run `./dev.sh` on a trivial workflow
     whose only step is a non-capturing autonomous Claude step. The prompt should tell the agent to
     wait for, and acknowledge, one message from another session before completing.
  4. Before sending, record both sessions' effective state:
     - The receiver's mode indicator should read auto. It must not read bypass. If it reads Manual,
       auto mode is unavailable; record that.
     - Confirm that no `crossSessionInbound` value applies to the receiver. Check managed settings,
       the user's `~/.claude/settings.json`, project settings in the scratch directory, and any
       `--settings` in the launch args (the runner's `--settings` holds only the Stop hook).
  5. Sender: in the second directory, start an ordinary interactive `claude` session and confirm its
     mode indicator reads auto, not bypass. Ask it to send the receiver session a short, distinctive
     message, using `/list-agents` to find the receiver.
  6. Keep the receiver running for at least the default `dialogExpiry` of five minutes, or until the
     message is delivered, whichever is sooner. Then let the step complete, close both sessions,
     delete the scratch directories, and restore `settings.yaml` byte-for-byte.
- Required decision or observation:
  - **Pass:** the receiver shows auto mode, no inbound override applies, and the distinctive message is
    delivered to the receiver's Claude without an approval dialog or a held-message notice.
  - **Fail:** both sessions are in the prompting class with no override, and the message is held or
    expires.
  - **Blocked / inconclusive, not pass or fail:** an inherited or managed `crossSessionInbound`
    (`accept`, `hold` or `refuse`) applies to the receiver or cannot be ruled out, or auto mode is
    unavailable (Manual).
  - Record the observed modes, the policy check and the message outcome. Report this separately from
    INT-001, which remains the automated check of the launch arguments.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| Adapters honor autonomous permission mode: Claude yolo autonomous-interactive uses auto (fresh and resume) | INT-001 | — | HT-001 |
| Adapters honor autonomous permission mode: Claude auto mode does not relax the autonomy contract | INT-001 | — | — |
| Adapters honor autonomous permission mode: Claude yolo autonomous-headless keeps bypassPermissions | INT-002 | — | — |
| Adapters honor autonomous permission mode: Capture-forced headless Claude step keeps bypassPermissions | INT-002 | — | — |
| No permission loosening in interactive mode: autonomous-interactive step stays autonomous under a best-effort flag | INT-001 | — | — |
| Journey: cross-session messaging between an autonomous-interactive yolo step and an ordinary session | — | — | HT-001 |
