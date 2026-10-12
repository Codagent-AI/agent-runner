# Direct Terminal Handoff for Interactive Terminal Steps

## Why

Before this change, Agent Runner proxied every terminal byte between the user and interactive agent CLIs through its own hosted terminal, interpreting and rewriting the stream to detect continuation signals. That made Agent Runner a de facto terminal emulator: protocol changes such as application-cursor mode and mouse reporting broke the proxy and demanded bespoke patches. Two shipped fixes proved the pattern, while the deterministic fake-agent harness and real-agent E2Es provided the behavioral baseline needed to remove it.

## What Changes

- Interactive agent and shell steps inherit the user's real terminal (`os.Stdin`/`os.Stdout`/`os.Stderr`) directly. Agent Runner remains the supervising parent but never sees or modifies terminal bytes.
- Direct inheritance includes real Unix job control: the child runs in its own process group and is made the terminal's foreground process group for the step's duration (so terminal-generated signals like Ctrl-C reach only the child), with the runner reclaiming the foreground afterward.
- Workflow step completion moves to a structured out-of-band control channel: a private, per-run local endpoint with scoped per-step authentication, exposed to the child via environment variables.
- A new `agent-runner step complete` command sends an authenticated completion event to the control endpoint. It is in-session only: the agent runs it from inside the step session using injected environment variables; there is no cross-terminal invocation.
- Every supported CLI gets a working completion surface: injected completion instructions plus the in-session command are the universal baseline, and CLI-native surfaces (slash command, tool) may be added where a CLI supports them — adapter-specific details are a design concern, not a spec requirement.
- Each CLI gets a process-local native completion surface that routes through the control channel: `/agent-runner:next` in Claude, Copilot, Cursor, and OpenCode, and `$agent-runner-next` in Codex because current Codex exposes injected skills rather than plugin slash commands. The user can also ask the agent in natural language to continue.
- Completion is a two-phase handshake, because the event is sent from inside the agent's own turn: the runner acknowledges the accepted event first (so the completion invocation returns to the agent), waits (bounded) for the turn to conclude and the CLI's session state to become durable, then gracefully terminates the child's process group (SIGTERM, bounded grace period, then SIGKILL), advances the workflow, and restores the live-run TUI. Natural exit and crash still stop the workflow with resume instructions, as today.
- The live-run TUI releases the terminal before the child starts and restores/repaints after it exits. Terminal release becomes fatal on failure (the child is not spawned), and restore failures are surfaced instead of silently logged — a hardening of the current coordinator behavior.
- Direct inheritance must not orphan a CLI that owns the terminal: a small supervision process outlives the runner and terminates the child if the runner crashes (after verifying process identity, never a reused PID), and resuming a crashed run cleans up any verified surviving child and stale control endpoint.
- **BREAKING** (narrow): the Ctrl-] shortcut is removed — a universal hotkey requires reading the byte stream, which is exactly what is being given up. There is no direct replacement; when the agent is stuck, the user quits the CLI and resumes the run. The output sentinel (the retired per-attempt continuation marker and legacy OSC form) also stops working, but agents receive the new completion instructions instead, so no user action is needed.
- Interactive terminal steps produce no transcript. The record of an agent session is the CLI's native session log plus workflow audit events; an interactive shell step records command metadata and exit outcome only.
- The agent-path terminal machinery is deleted once direct handoff lands: input parsing and enhanced-key/SS3 handling, `/next` line tracking, mouse-mode tracking, sentinel scanning and marker stripping, idle hints, and compensating terminal-mode resets.
- Interactive shell steps move to the same direct terminal-process and job-control supervisor. They finish from the command's exit code and do not use the agent completion channel.

## Capabilities

### New Capabilities

- `interactive-terminal-handoff`: direct terminal inheritance for all interactive terminal steps — child stream inheritance, live-run TUI release/restore, process supervision, job control, process-group termination, and crash handling.
- `step-control-channel`: the out-of-band workflow control plane — per-run endpoint lifecycle, scoped per-step authentication, the `complete_step` event contract, the in-session `agent-runner step complete` command, completion-instruction delivery to the agent, and a CLI-agnostic universal completion surface (adapter-specific integrations are a design concern).

### Modified Capabilities

- `pseudo-terminal`: retired from runtime behavior. Interactive agent and shell steps both use `interactive-terminal-handoff`; an outer synthetic terminal remains test infrastructure only.
- `agent-continue-trigger`: retired as an in-band mechanism. Sentinel detection, stripping, and sentinel-instruction injection are removed; agent-initiated continuation is redefined by `step-control-channel`.
- `cli-adapter`: the "no permission loosening in interactive mode" requirement gains a narrow, spec-bounded exception for the completion client — an adapter may pre-approve exactly the absolute-path `step complete` command and nothing broader. The capture rationale is also updated because interactive output goes straight to the user's terminal.
- `workflow-execution`: the agent step dispatch requirement is reworded — interactive steps execute via direct terminal handoff instead of "via the PTY layer".
- `audit-log-entries`: the closed event-type list gains the control-plane and job-control event types (`completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, `child_continued`).
- `live-run-view`: the interactive-step TUI scenarios are reworded from continue-trigger terminology to control-channel completion; behavior is unchanged.

## Technical Approach

```text
before:  terminal ↔ agent-runner byte proxy (parse, rewrite, strip) ↔ agent CLI
after:   terminal ←——————— kernel ———————→ agent CLI or shell command
                    agent-runner (parent process: spawn, supervise,
                    terminate; sees zero terminal bytes)
                            ↑
                control socket ← `agent-runner step complete`
```

In plain language: the old runner sat between the user and the agent CLI, reading every keystroke and every byte the agent drew. After this change, an interactive child is connected straight to the user's terminal — exactly like a shell running vim — and Agent Runner is the waiting parent process that started it. Agent steps use a private per-run mailbox whose address and one-time per-step credential are inherited through environment variables. The completion client delivers one authenticated message; Agent Runner acknowledges it, proves the turn durable, shuts the CLI down gracefully, and advances. Interactive shell steps need no mailbox: their exit status is the workflow result.

Key decisions:

- **Direct replacement, no public feature flag — but staged internally.** Several existing assertions exercise the sentinel machinery being deleted, so rewriting them simultaneously with the cutover would weaken them as characterization coverage. Implementation order within this change: build the control channel and direct runner with their own tests while the old production path still runs, prove terminal fidelity, session durability, and completion on the new path, switch production execution, then delete the parser path and its obsolete assertions. All within this change, consistent with the pre-release posture.
- **Agent Runner stays supervisor.** It still owns command construction, sessions, workflow state, shutdown, and TUI coordination — it only exits the byte path.
- **Native terminal fidelity over a universal hotkey.** A parent cannot both grant full terminal ownership and intercept keys from the same stream. Native Agent Runner commands become control-plane operations; quit-and-resume is the fallback when the agent is stuck.
- **Control transport is a private local endpoint** — a per-run Unix socket in a user-private temp directory (the run directory holds a pointer file; socket paths have a low OS length limit), plus `AGENT_RUNNER_CONTROL_*` env vars and a fresh per-attempt credential. The design chose an absolute-path shell client as the sole completion transport; there is no MCP server.
- **No runtime relay library.** `creack/pty` remains only in E2E test code, where a synthetic outer terminal is required to verify real terminal behavior in automation.
- **Existing tests are the migration boundary.** The deterministic fake-agent harness and the five real-agent interactive plus five headless E2Es define observable behavior; tests asserting in-band marker semantics are rewritten against the control contract.

### Alternatives Considered

- **Status quo plus tactical patches.** Cheapest short-term, but both shipped incidents (SS3/Herdr, `7adcbfb` mouse forwarding) were user-visible input corruption that was hard to diagnose, and the class recurs with every terminal protocol agent CLIs adopt (Kitty keyboard, focus events, future extensions). This is an open-ended series of fire drills, not a stable end state.
- **Opaque PTY relay + control channel.** The strongest alternative: keep the PTY but strip all parsing, moving completion to the same control channel. Both historical bugs came from parsing, not the PTY itself, so this would also have prevented them, and it preserves the option of terminal recording. Rejected because recording is out of scope (its only remaining benefit), and a relay's transparency must be actively maintained (raw-mode fidelity, resize forwarding, close ordering) while inheritance's transparency holds by construction. If recording ever becomes a requirement, an opt-in relay of this shape is the natural mechanism.
- **Multiplexer-based hosting (tmux-style).** Adds a heavy dependency and reintroduces the same emulation-fidelity problem one layer down.

Honest cost accounting: the control plane (endpoint lifecycle, per-step auth, five adapter integrations, plugin packaging) is real new machinery, plausibly comparable in size to the parser being deleted. The difference is that it is deterministic and testable, while the parser's costs were unpredictable and user-facing.

The resulting code shape keeps direct handoff, the control endpoint, process lifecycle, shell execution, and TUI coordination in `internal/interactive`. The former runtime terminal-relay package is deleted.

## Out of Scope

- Terminal recording of interactive agent or shell steps, including any opt-in relay mode.
- Windows support for the control transport (noted as a future concern, not designed here).
- Herdr-side changes; optional real-Herdr compatibility coverage may be added but Herdr itself is untouched.
- Changing headless agent or autonomous shell execution.

## Impact

- **Code**: the runtime `internal/pty` package is removed; `internal/interactive` owns the shared direct terminal process, control endpoint, and supervision; `internal/exec` dispatches both agent and shell interactive steps; adapters and CLI commands provide completion integration.
- **Specs**: `pseudo-terminal` and `agent-continue-trigger` are retired; `interactive-terminal-handoff` and `step-control-channel` define current behavior; `interactive-shell-steps`, `cli-adapter`, `workflow-execution`, audit, and live-run specs are updated consistently.
- **Workflows/prompts**: sentinel completion instructions injected into interactive prompts are replaced by control-plane completion instructions; built-in workflow behavior is otherwise unchanged.
- **Users**: interactive sessions still advance when the agent finishes or the user invokes the CLI-native Agent Runner command. The native spelling is `/agent-runner:next` for Claude, Copilot, Cursor, and OpenCode, and `$agent-runner-next` for Codex. Users gain full native terminal behavior (mouse, paste, SS3, future protocols) with no Agent Runner patches; they lose Ctrl-] and the runner-drawn continuation overlay.
- **Upgrade path**: no workflow YAML, config, or state migration. Upgrading the binary is sufficient because adapters inject process-local completion commands and each step's prompt injection is generated fresh. Release notes must call out the Ctrl-] and overlay removal.
- **Dependencies**: `creack/pty` remains a test dependency for automated terminal E2Es but is no longer linked through production code.
- **Tests**: existing fake-agent characterization suite and real-agent E2Es act as the regression contract; marker-stripping assertions are rewritten to assert the structured control contract.
