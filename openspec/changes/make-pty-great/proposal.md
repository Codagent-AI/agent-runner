# Direct Terminal Handoff for Interactive Agent Steps

## Why

Agent Runner currently proxies every terminal byte between the user and interactive agent CLIs through its own PTY, interpreting and rewriting the stream to detect `/next`, Ctrl-], and continuation sentinels. This makes Agent Runner a de facto terminal emulator: every terminal protocol an agent CLI adopts (SS3 application-cursor mode, SGR mouse reporting, and whatever comes next) breaks the proxy and demands a bespoke patch. Two shipped fixes prove the pattern: the SS3 split that corrupted arrow keys under Herdr, and commit `7adcbfb` which had to watch child output to conditionally un-drop mouse input for Claude Code. The deterministic fake-agent harness and five real-agent interactive E2Es now exist as a behavioral baseline, so this is the right moment to remove the proxy rather than keep patching it.

## What Changes

- Interactive agent steps stop using an Agent Runner PTY. The child process inherits the user's real terminal (`os.Stdin`/`os.Stdout`/`os.Stderr`) directly; Agent Runner remains the supervising parent but never sees or modifies terminal bytes.
- Workflow step completion moves to a structured out-of-band control channel: a private, per-run local endpoint with scoped per-step authentication, exposed to the child via environment variables.
- A new `agent-runner step complete` command sends an authenticated completion event to the control endpoint. It is in-session only: the agent runs it from inside the step session using injected environment variables; there is no cross-terminal invocation.
- Every supported CLI gets a working completion surface: injected completion instructions plus the in-session command are the universal baseline, and CLI-native surfaces (slash command, tool) may be added where a CLI supports them — adapter-specific details are a design concern, not a spec requirement.
- The user-typed `/next` experience is preserved: adapters whose CLIs support custom commands get a real `/next` slash command or skill (shipped through the existing agent-skills plugin or a new one — exact packaging decided in specs) that routes through the control channel. Where a CLI supports no custom commands, the user asks the agent in natural language to continue, or runs the external command.
- On completion, Agent Runner gracefully terminates the child's process group (bounded grace period, then kill), advances the workflow, and restores the live-run TUI. Natural exit and crash still stop the workflow with resume instructions, as today.
- The live-run TUI releases the terminal before the child starts and restores/repaints after it exits, reusing the existing coordinator release/restore behavior.
- **BREAKING** (narrow): the Ctrl-] shortcut is removed — a universal hotkey requires reading the byte stream, which is exactly what is being given up. There is no direct replacement; when the agent is stuck, the user quits the CLI and resumes the run. The output sentinel (`AGENT_RUNNER_CONTINUE_*` marker and legacy OSC form) also stops working, but agents receive the new completion instructions instead, so no user action is needed.
- Interactive agent steps produce no terminal transcript (this formalizes current behavior: the PTY transcript is captured today but discarded at `internal/exec/agent.go`). The record of a session is the agent's native session log plus workflow audit events.
- The agent-path terminal machinery is deleted once direct handoff lands: input parsing and enhanced-key/SS3 handling, `/next` line tracking, mouse-mode tracking, sentinel scanning and marker stripping, idle hints, and compensating terminal-mode resets.
- The PTY layer is retained only for interactive shell steps, narrowed to an opaque byte relay (create, resize, copy, capture) with hardened lifecycle: process-group signaling, explicit close ordering, bounded draining, and propagated errors.

## Capabilities

### New Capabilities

- `interactive-terminal-handoff`: direct terminal inheritance for interactive agent steps — child stream inheritance, live-run TUI release/restore around the child, process supervision, graceful process-group termination on completion, and natural-exit/crash handling.
- `step-control-channel`: the out-of-band workflow control plane — per-run endpoint lifecycle, scoped per-step authentication, the `complete_step` event contract, the in-session `agent-runner step complete` command, completion-instruction delivery to the agent, and a CLI-agnostic universal completion surface (adapter-specific integrations are a design concern).

### Modified Capabilities

- `pseudo-terminal`: narrowed from "PTY hosting for interactive steps" to the retained relay for interactive shell steps only. Requirements for hosting interactive *agent* steps in a PTY, in-band continue triggers, and sentinel stripping are removed; termination and exit-handling requirements move to `interactive-terminal-handoff`. Retained PTY requirements gain the lifecycle hardening (process-group termination, bounded draining).
- `agent-continue-trigger`: retired as an in-band mechanism. Sentinel detection, stripping, and sentinel-instruction injection are removed; agent-initiated continuation is redefined by `step-control-channel`.

## Technical Approach

```text
today:   terminal ↔ agent-runner PTY proxy (parse, rewrite, strip) ↔ agent CLI
after:   terminal ←——————— kernel ———————→ agent CLI
                    agent-runner (parent process: spawn, supervise,
                    terminate; sees zero terminal bytes)
                            ↑
                control.sock ← `agent-runner step complete` / MCP tool
```

In plain language: today agent-runner sits between the user and the agent CLI, reading every keystroke and every byte the agent draws, hoping to spot `/next`, Ctrl-], or the hidden completion codeword. After this change, the agent CLI is connected straight to the user's terminal — exactly like a shell running vim — and agent-runner is just the waiting parent process that started it. Since agent-runner can no longer eavesdrop for the codeword, it instead opens a private mailbox for each step: a socket file in the run's directory, whose address and a one-time per-step password are handed to the agent session through environment variables. `complete_step` (whether invoked as a tool, a `/next` slash command, or `agent-runner step complete` run by the agent inside its session) is a tiny client that connects to that mailbox and delivers one message: "this step is done", password attached. Agent-runner, waiting on either the mailbox or the child exiting, validates the password and step, records completion, shuts the CLI down gracefully, and advances the workflow. The per-step password is what stops stale messages, replayed transcripts, or unrelated processes from advancing a run. The old model was the agent shouting a codeword on screen while agent-runner listened to everything; the new model is a doorbell wired directly to agent-runner.

Key decisions:

- **Direct replacement, no feature flag.** Interactive agent execution switches to direct handoff outright, protected by the existing deterministic harness and real-agent E2Es. The parser path is deleted in the same change, consistent with the pre-release posture.
- **Agent Runner stays supervisor.** It still owns command construction, sessions, workflow state, shutdown, and TUI coordination — it only exits the byte path.
- **Native terminal fidelity over a universal hotkey.** A parent cannot both grant full terminal ownership and intercept keys from the same stream. `/next` becomes a control-plane operation; quit-and-resume is the fallback when the agent is stuck.
- **Control transport is a private local endpoint** (working shape: `runs/<run-id>/control.sock` plus `AGENT_RUNNER_CONTROL_*` env vars and a per-step nonce). Exact transport and the MCP tool lifecycle are design-phase decisions.
- **No library swap for its own sake.** `creack/pty` remains for the shell relay; `charmbracelet/x/term`/`x/ansi` may replace handwritten primitives where parsing is still genuinely needed.
- **Existing tests are the migration boundary.** The deterministic fake-agent harness and the five real-agent interactive plus five headless E2Es define observable behavior; tests asserting in-band marker semantics are rewritten against the control contract.

### Alternatives Considered

- **Status quo plus tactical patches.** Cheapest short-term, but both shipped incidents (SS3/Herdr, `7adcbfb` mouse forwarding) were user-visible input corruption that was hard to diagnose, and the class recurs with every terminal protocol agent CLIs adopt (Kitty keyboard, focus events, future extensions). This is an open-ended series of fire drills, not a stable end state.
- **Opaque PTY relay + control channel.** The strongest alternative: keep the PTY but strip all parsing, moving completion to the same control channel. Both historical bugs came from parsing, not the PTY itself, so this would also have prevented them, and it preserves the option of terminal recording. Rejected because recording is out of scope (its only remaining benefit), and a relay's transparency must be actively maintained (raw-mode fidelity, resize forwarding, close ordering) while inheritance's transparency holds by construction. If recording ever becomes a requirement, an opt-in relay of this shape is the natural mechanism.
- **Multiplexer-based hosting (tmux-style).** Adds a heavy dependency and reintroduces the same emulation-fidelity problem one layer down.

Honest cost accounting: the control plane (endpoint lifecycle, per-step auth, five adapter integrations, plugin packaging) is real new machinery, plausibly comparable in size to the parser being deleted. The difference is that it is deterministic and testable, while the parser's costs were unpredictable and user-facing.

The intended code shape splits an `interactive` package (direct handoff, control endpoint, process lifecycle, TUI coordination) from a narrowed `pty` package (session, opaque copy, transcript). The tactical SS3 fix is retained during implementation as an intra-change ordering detail: it stays only until this change's own parser-deletion step lands, and is gone by the time the change is complete.

## Out of Scope

- Terminal recording of interactive agent steps, including any opt-in PTY relay mode for them. Interactive agent transcripts are discarded today; this change formalizes that rather than adding recording.
- Replacing or reworking the interactive shell step experience beyond the lifecycle hardening of its retained PTY path.
- Windows support for the control transport (noted as a future concern, not designed here).
- Herdr-side changes; optional real-Herdr compatibility coverage may be added but Herdr itself is untouched.
- Changing headless agent execution, which never used a PTY.

## Impact

- **Code**: `internal/pty` (agent-path input/output/mouse/hint machinery removed; session/copy/transcript retained and hardened), `internal/exec/agent.go` interactive branch, `internal/liverun/coordinator.go` (release/restore reuse), `internal/cli` adapters (completion integration per CLI), `cmd/agent-runner` (new `step complete` subcommand), new packages for the control endpoint and direct runner.
- **Specs**: `pseudo-terminal` and `agent-continue-trigger` requirements substantially rewritten/retired; two new capability specs. `interactive-shell-steps` and `output-capture` are compatible as written (capture already forbidden on interactive agent steps).
- **Workflows/prompts**: sentinel completion instructions injected into interactive prompts are replaced by control-plane completion instructions; built-in workflow behavior is otherwise unchanged.
- **Users**: interactive sessions look and advance the same — the agent finishes and the workflow moves on, and typed `/next` keeps working where the hosting CLI supports custom commands. What users gain is full native terminal behavior (mouse, paste, SS3, future protocols) with no Agent Runner patches; what they lose is Ctrl-].
- **Upgrade path**: no workflow YAML, config, or state migration. Upgrading the binary (plus the plugin carrying the `/next` command) is sufficient; in-flight runs resume cleanly because each step's prompt injection is generated fresh. Release notes must call out the Ctrl-] removal.
- **Dependencies**: no removals; `creack/pty` stays for the shell relay; possible additions of `charmbracelet/x/term`/`x/ansi` decided in design.
- **Tests**: existing fake-agent characterization suite and real-agent E2Es act as the regression contract; marker-stripping assertions are rewritten to assert the structured control contract.
