---
title: Direct Terminal Handoff
group: Guides
order: 6
description: How interactive agent and shell steps share the real terminal safely.
---

# Direct Terminal Handoff

Interactive agent and shell steps give the child process direct ownership of the user's terminal. Agent Runner remains the supervising parent. Agent steps advance through a separate authenticated control channel; shell steps finish normally from their command's exit status.

This document describes the runtime as it works now. It covers interactive agent steps, the completion handshake, session durability, Unix job control, adapter integrations, failure handling, and tests.

## Scope

Direct terminal handoff applies to agent steps using `interactive` or `autonomous-interactive` execution and shell steps using `mode: interactive`.

- Headless agent steps use the existing non-interactive execution path.
- Autonomous shell steps use the existing piped `sh -c` path.
- Interactive terminal output is visible live but is not captured or written to the audit log. Interactive steps cannot use `capture`.
- OpenCode interactive steps fail before spawn while [anomalyco/opencode#37536](https://github.com/anomalyco/opencode/issues/37536) remains present in supported OpenCode releases. Fresh and resumed OpenCode headless steps remain supported.
- Windows control transport is outside the current scope.

Agent Runner does not draw a continuation overlay or reserve a global continuation key. The CLI owns the terminal input stream.

## Terminal terms

A terminal emulator is the application window. It renders output bytes and converts keyboard, paste, and mouse actions into input bytes.

A TTY is the kernel terminal device exposed to a process as standard input, output, and error. It also stores terminal modes and the identity of the foreground process group.

A process group is a set of related processes that Unix can signal together. The foreground process group owns terminal input and receives terminal-generated signals such as Ctrl-C.

## Architecture

Interactive terminal traffic and workflow control travel over separate channels.

```text
Terminal data plane

  user's terminal device
          ^
          | inherited stdin, stdout, stderr
          v
  agent CLI or shell-command process group
          |
          | foreground terminal owner during the step
          v
  mouse, paste, resize, arrow keys, Ctrl-C, native terminal UI


Workflow control plane

  agent or native completion command
          |
          | agent-runner step complete
          v
  private per-run Unix socket
          |
          | authenticated JSON event
          v
  Agent Runner parent
          |
          +--> verify completing turn is durable
          +--> terminate and reap CLI process group
          +--> record outcome and advance workflow
```

Agent Runner keeps the terminal file descriptors open because it must reclaim the terminal after the child finishes. It does not read or rewrite terminal traffic while the step is active.

## Process model

The shell, Agent Runner, the CLI, and the watchdog remain in one terminal session.

```text
user's shell
└── agent-runner
    ├── agent CLI or interactive shell command
    │   └── child helper processes
    └── agent-runner internal watchdog

foreground ownership while the step runs:

    terminal foreground process group --> child process group
    Agent Runner                       --> supervising parent
    watchdog                           --> separate process group
```

Agent Runner starts the child in a new process group and asks the kernel to make that group foreground before it reads the terminal. The parent exclusively waits for child state with `wait4(WUNTRACED | WCONTINUED)`. One waiter owns exit, stop, and continue events, which avoids races between `cmd.Wait` and job-control handling.

The watchdog runs in its own process group. A pipe connects it to the parent. If the parent exits or crashes, the pipe reaches EOF. The watchdog verifies the child's PID and process start time before sending any signal, which prevents a reused PID from being killed.

## Interactive agent lifecycle

One interactive step follows this sequence:

1. The live-run TUI owns the terminal while it displays workflow state.
2. The TUI releases the terminal. A release error fails the step before the CLI starts.
3. Agent Runner creates a completion attempt with a fresh credential.
4. The adapter builds the CLI arguments, process-local completion integration, environment, and durability probe.
5. Agent Runner starts the CLI with the real terminal descriptors and a new process group.
6. The CLI process group becomes the terminal foreground owner.
7. Agent Runner waits for a completion event or a child process event.
8. After completion or exit, Agent Runner reclaims foreground ownership and restores its saved terminal modes.
9. The live-run TUI restores its screen and repaints workflow state.

The TUI can skip a redundant release and restore between consecutive interactive steps, provided terminal ownership and modes remain correct.

## Control endpoint

Each run gets one lazily created Unix socket server. It starts after Agent Runner acquires the run lock and before the first interactive step releases the terminal. It remains available across later steps in that run and closes when the run ends.

Unix socket paths have a small platform limit, so the server uses a short path inside a user-private temporary directory. The run directory stores a pointer to that socket. Stale socket cleanup requires proof that the current process holds the run lock.

Every step attempt rotates the credential and exposes four values to the child:

```text
AGENT_RUNNER_CONTROL_SOCKET
AGENT_RUNNER_RUN_ID
AGENT_RUNNER_STEP_ID
AGENT_RUNNER_CONTROL_TOKEN
```

The completion client sends one newline-delimited JSON object:

```json
{
  "type": "complete_step",
  "run_id": "...",
  "step_id": "...",
  "token": "...",
  "request_id": "..."
}
```

The server accepts an event only when its run, step, token, and active attempt match. Unknown credentials, inactive attempts, malformed messages, and stale retries are rejected and audited. Retrying the same accepted `request_id` is idempotent and returns the original receipt.

## How a step is completed

The universal completion path is the absolute command injected into the step prompt:

```text
/absolute/path/to/agent-runner step complete
```

The command takes no workflow identifiers on its command line. It reads the socket address, identities, and credential from the environment inherited by the CLI and its shell tools. This keeps completion scoped to the active session. There is no cross-terminal force-advance command.

Users have two convenient ways to request the same event:

- Ask the agent to continue to the next workflow step. The agent follows its injected instruction and runs the completion client.
- Invoke the adapter's process-local native completion command.

| CLI | Explicit completion command | Delivery |
| --- | --- | --- |
| Claude | `/agent-runner:next` | A generated `agent-runner` plugin is passed with `--plugin-dir`. |
| Codex | `$agent-runner-next` | A generated skill is added through an isolated `CODEX_HOME`. Current Codex does not expose plugin commands as slash commands. |
| Copilot | `/agent-runner:next` | A generated `agent-runner` plugin is passed with `--plugin-dir`. Copilot asks for supervised shell approval because its allow rule cannot safely restrict the fixed arguments. |
| Cursor | `/agent-runner:next` | A generated `agent-runner` plugin is passed with `--plugin-dir`. |
| OpenCode | `/agent-runner:next` | `OPENCODE_CONFIG_CONTENT` adds a process-local command. Agent Runner interactive OpenCode remains blocked by the resumed-prompt product bug. |

The generated plugins and skills live in the user's cache. Adapters pass them only to the spawned process. Users do not install a global plugin, and Agent Runner does not modify project files.

Claude, Copilot, and Cursor expose the same command name because their plugin systems namespace `commands/next.md` by the plugin name `agent-runner`. Codex uses a skill because its current TUI supports explicit skill mentions and does not load plugin command directories as slash commands.

Codex receives a run-scoped, content-addressed private `CODEX_HOME`. Different Agent Runner runs cannot share its mutable config, while later steps in the same run reuse it. The private home links the user's authentication, session stores, shell snapshots, plugins, and unrelated state; copies the current config; and adds the completion skill. Agent Runner creates and links the shared session-state directories before launch, so state created by the first interactive turn remains visible to normal and headless Codex invocations. Codex may write hook trust decisions into the private config, and those writes remain available to later steps in the same run.

Codex identifies hook trust by the absolute `hooks.json` path. Because the private home presents the same hook file at a run-specific path, Agent Runner copies any trusted hook hash from the user's source path to the corresponding private path. A hook the user has already trusted therefore remains trusted across new workflow runs without weakening trust for unrelated hooks.

## Completion and turn durability

The completion command runs inside the agent's current turn. Agent Runner must let that tool call return and let the CLI save the turn before it terminates the process.

```text
agent runs completion client
          |
          v
server validates current attempt
          |
          v
capture accept-time session checkpoint
          |
          v
acknowledge client with unique receipt
          |
          v
wait up to 30 seconds of active runtime
          |
          +--> committed-turn evidence found
          |        |
          |        v
          |    terminate CLI group, record success
          |
          +--> bound expires or probe fails
                   |
                   v
               terminate CLI group, record failure
```

Acceptance is an intermediate state. It is recorded as `completion_requested` and `completion_acknowledged`, not as step success.

The server captures a checkpoint of the CLI's native session store before it acknowledges the completion client. A fresh session may not have created its store yet. In that case the checkpoint is an empty baseline and the durability probe waits for the store to appear within the normal bound.

The acknowledgement lets the shell tool return. It includes a unique request receipt, and the client prints that receipt in its tool output. The CLI can then finish and save the turn normally.

Agent Runner waits for semantic evidence recorded after the checkpoint. It does not use file modification times, quiet windows, or a fixed post-command sleep.

| CLI | Primary committed-turn evidence | Store fallback or detail |
| --- | --- | --- |
| Claude | Process-local `Stop` hook sends `turn_committed` after completion has been accepted. | A completed assistant record with a terminal stop reason in the session JSONL. Stop hooks that fire before completion acceptance are acknowledged and ignored. |
| Codex | `notify` sends `turn_committed` on agent-turn completion. | A terminal `task_complete` rollout record after the checkpoint. |
| Copilot | Semantic `assistant.turn_end` event in session state. | Intermediate messages do not satisfy the probe. |
| Cursor | The completion receipt appears in a post-checkpoint shell tool-result row in `store.db`. | Cursor has no separate terminal turn marker. The completion command must be the final action in the response. |
| OpenCode | A newly completed final assistant message returned by `opencode db`. | `finish: "tool-calls"` is intermediate and does not satisfy the probe. |

The durability deadline is 30 seconds of active runtime. Suspending the job pauses this clock. If the child exits after completion acceptance, Agent Runner continues inspecting the native store for the remaining bound because the CLI may have flushed the turn during shutdown.

Confirmed durability produces step outcome `success`. A durability timeout or probe error produces `failed` with the CLI name, session ID, timeout, inspected artifact, and underlying error in the audit record. A later `session: resume` step can rely on every successful completing turn being present.

## Termination

After durability is confirmed, Agent Runner sends SIGTERM to the CLI's process group. The group gets three seconds of active runtime to exit. Agent Runner then sends SIGKILL if any process remains and reaps the child.

Group signaling covers helper processes started by the CLI. Signals always use the verified child process group. Cleanup code does not signal an unverified PGID.

If no completion event was accepted and the CLI exits, the step is recorded as `aborted`. Resuming the workflow retries that step with a fresh attempt credential.

## Unix job control

Ctrl-C is generated by the terminal and delivered to the foreground process group, so it reaches the CLI rather than Agent Runner.

Ctrl-Z and external stop signals use full job-control forwarding:

1. The supervisor observes that the child process group stopped.
2. Agent Runner saves the child's terminal modes.
3. Agent Runner reclaims foreground ownership and restores its shell-facing modes.
4. Agent Runner stops its own process group, allowing the user's shell to report the complete job as suspended.
5. When the user runs `fg`, Agent Runner verifies that it is foreground, restores the child's modes, transfers the terminal to the child group, and sends SIGCONT.

Running the job in the background does not give the stopped child terminal access. Agent Runner waits until the shell places the whole job in the foreground.

The active-runtime durability and termination timers pause while the child is stopped.

## Crash and resume cleanup

For each interactive attempt, Agent Runner persists the child PID, process group ID, process start time, and socket path.

If the parent disappears, the watchdog reads EOF from its liveness pipe. It checks the live process start time against the persisted identity, then terminates the matching process group. A mismatched start time means the PID has been reused and no signal is sent.

When a run resumes after a crash, cleanup happens under the run lock. Agent Runner checks the saved PID and start time, terminates a verified survivor, removes the stale socket, and starts a new attempt with a new credential.

## Failure behavior

| Failure | Result |
| --- | --- |
| Control endpoint cannot be created | The interactive step fails before spawn. |
| Live-run TUI cannot release the terminal | The step fails before spawn. |
| Adapter cannot create its process-local command or skill | The step fails before spawn with an adapter-specific error. |
| Child spawn, foreground transfer, or supervision fails | The step fails and partial child state is cleaned up. |
| Completion request is stale or malformed | The request is rejected and audited. The active step keeps running. |
| Durability cannot be proven | The CLI is terminated and the step outcome is `failed`. |
| CLI exits before completion acceptance | The step outcome is `aborted` and the run can be resumed. |
| Terminal restore fails | The error is surfaced. The already recorded workflow outcome is preserved. |
| Agent Runner crashes | The watchdog terminates a verified surviving CLI process group. |

## Interactive shell steps

Interactive shell steps use the same direct terminal-process primitive and job-control supervisor as interactive agent steps. Agent Runner releases the live-run TUI, starts `sh -c <command>` with inherited stdin, stdout, and stderr, makes the command's process group foreground, waits for it to exit, then reclaims the terminal and restores the TUI.

Shell steps do not use the agent control endpoint or turn-durability handshake. Exit code 0 produces `success`; a nonzero exit code produces `failed`. Terminal output is live-only: Agent Runner does not proxy it, retain a transcript, or include it in `step_end`. Use an autonomous shell step with `capture` when later workflow steps need command output.

## Verification

The implementation has several test layers:

- Control-server unit tests cover endpoint permissions, credential rotation, stale attempts, malformed messages, idempotent retries, request size limits, acknowledgement races, shutdown ordering, and early post-turn hooks.
- Adapter unit tests cover process-local command generation, safe permission behavior, private configuration, durability probes, and failure before spawn when an integration cannot be created.
- Session-store fixtures contain representative Claude, Codex, Copilot, Cursor, and OpenCode records. Tests prove that intermediate records are rejected and terminal records are accepted.
- The deterministic fake-agent integration harness exercises terminal inheritance, input and output fidelity, completion, process-group termination, TUI release and restore errors, and job-control state transitions.
- A direct-shell E2E records the outer terminal device and proves that an interactive shell child inherits that exact device on stdin, stdout, and stderr.
- Real-agent interactive E2Es test both completion paths. The fresh step uses the explicit native command. The resumed step asks the agent in natural language to continue. A generated phrase must survive into the resumed session, which proves the completing turn was durably saved before termination.
- Claude, Codex, Copilot, and Cursor run the complete fresh plus resume workflow through Agent Runner.
- OpenCode runs two real fresh-TUI completion tests, one for `/agent-runner:next` and one for natural-language continuation. A separate workflow E2E proves that interactive OpenCode fails early while resumed prompt submission remains broken.
- Five real-agent headless E2Es cover fresh and resumed sessions for every adapter.

The real-agent suites are separate from `make test` because they require installed and authenticated CLI products. The repository's release workflow invokes them as an external compatibility gate.

## Code map

```text
internal/interactive/
  control.go       per-run socket, attempts, authentication, acknowledgements
  runner.go        direct child lifecycle and completion state machine
  terminal_process.go  shared direct terminal execution for shell steps
  process.go       wait4 ownership, foreground transfer, stop and continue
  durability.go    committed-turn wait and active-runtime deadline
  watchdog.go      parent-crash cleanup

internal/cli/
  completion_plugin.go   generated commands, Codex private home and skill
  durability.go          five native session-store probes
  claude.go              Stop hook, command plugin, exact pre-approval
  codex.go               notify hook and private CODEX_HOME
  copilot.go             command plugin and supervised approval behavior
  cursor.go              command plugin, private permission config, receipt probe
  opencode.go            inline command, permissions, interactive fail-fast

internal/exec/agent.go
  builds the absolute completion instruction and launches DirectRunner

internal/exec/shell.go
  launches interactive shell commands through the shared direct terminal runner

internal/liverun/
  releases and restores the live-run TUI around terminal ownership

cmd/agent-runner/
  step complete              authenticated completion client
  internal turn-committed    adapter hook client
  internal watchdog          parent-crash supervisor
```
