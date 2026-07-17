# Task: Implement completion and durability for all five adapters

## Goal

Give every supported CLI adapter (Claude, Codex, Copilot, Cursor, OpenCode) a working completion integration: the narrow pre-approval for the absolute-path completion client, the ephemeral post-turn hook or semantic session-store durability probe, and the optional `/next` command. When this task finishes, no matrix cell is unproven: every adapter completes without human approval and has committed-turn evidence, or implements its documented alternative.

## Background

You MUST read these files before starting:

- `openspec/changes/make-pty-great/design.md` — especially "Five-CLI completion matrix", "Turn durability", and "Prompt and adapter integration"
- `openspec/changes/make-pty-great/phase0-findings.md` — the proven flag syntax, config snippets, session-store record shapes, documented alternatives for any failed cell, and the chosen `/next` packaging; this document is authoritative where the design matrix said *(P0)*
- `openspec/changes/make-pty-great/specs/step-control-channel/spec.md` — the "Universal completion surface" requirement
- `openspec/changes/make-pty-great/specs/cli-adapter/spec.md` — the modified "No permission loosening in interactive mode" requirement

### What to build

All adapter code lives in `internal/cli/` (`claude.go`, `codex.go`, `copilot.go`, `cursor.go`, `opencode.go`, shared pieces in `adapter.go`):

- **Completion-command descriptor**: extend `BuildArgsInput` so the runner can pass the completion client's absolute executable path and fixed `step complete` arguments. Adapters use it to emit their CLI's narrow pre-approval and any ephemeral hook/notify configuration. The absolute path comes from the runner (`os.Executable()`); adapters never assume the binary is on PATH.
- **Narrow pre-approval per adapter**, exactly as proven in the findings (candidates from the design: Claude `--allowedTools "Bash(<abs> step complete)"`; Codex `-c` config approval override; Copilot granular `--allow-tool` shell scoping; Cursor permissions config allowlist; OpenCode `opencode.json` bash allow-pattern). The pre-approval covers only the exact absolute path with fixed arguments — never wildcards, chaining, substitutions, or other subcommands of the runner binary. Adapters whose CLI cannot express that narrowness implement their documented alternative integration instead. The existing interactive no-permission-loosening behavior stays intact for everything else (see the spec below and the current requirement in `openspec/specs/cli-adapter/spec.md`).
- **`TurnDurabilityProbe` implementations** for all five adapters, implementing the interface defined in `internal/cli`. Evidence hierarchy per adapter: (1) native post-turn hook configured ephemerally for the spawned process only (Claude: `Stop` hook injected via `--settings` running `agent-runner internal turn-committed`; Codex: `notify` on `agent-turn-complete`), (2) otherwise semantic inspection of the native session store for an explicitly completed assistant turn recorded after the checkpoint. Never file writes, mtimes, or quiet periods. Session stores: Claude `~/.claude/projects/<encoded-cwd>/<id>.jsonl`; Codex `~/.codex/sessions/YYYY/MM/DD/*.jsonl`; Copilot session dirs with `workspace.yaml`; Cursor local chat store; OpenCode `opencode db` queries. The existing `DiscoverSessionID`/`SessionExists` code in each adapter file shows how each store is located today.
- **Ephemeral config injection must be additive**: if a CLI merges injected settings destructively over user config (per findings), that adapter falls back to the session-store probe and the limitation is recorded in the findings document.
- **`/next` commands**: thin per-CLI custom commands (for CLIs that support them) that simply run the completion client, packaged per the findings' packaging choice.
- **Fixture tests**: probe tests run against fixture session stores checked into the package's testdata (real captured store shapes per findings), not against live CLIs. Autonomous-interactive behavior is unchanged structurally: those invocations may already carry each CLI's autonomous permission flags (see `internal/cli/adapter.go` and the `autonomous_permission_mode` handling).

### Conventions

TDD; tests next to the source package; `google/go-cmp` for structured comparisons; local fakes over mocking frameworks; `make fmt`; adapters remain registry-driven (see `internal/cli/adapter.go`).

## Spec

### Requirement: Universal completion surface

For every interactive or autonomous-interactive agent step, regardless of which CLI backs it, the agent SHALL have a working way to signal completion through the control channel. No step SHALL depend on CLI-specific byte-stream behavior to advance. The injected completion instructions SHALL reference the completion client by absolute path rather than assuming the runner binary is on the agent's PATH. For autonomous-interactive steps, the completion path SHALL NOT block on human permission approval. CLI-native completion surfaces (such as a user-invocable `/next`-style command or a tool exposed to the agent) MAY additionally exist, and any such surface SHALL route through the same control channel with the same credential validation.

#### Scenario: Any registered CLI can complete a step
- **WHEN** an interactive agent step runs with any registered CLI and the agent follows the injected completion instructions
- **THEN** the completion event is delivered through the control channel and the workflow advances with outcome `success`

#### Scenario: Instructions do not depend on PATH
- **WHEN** the runner injects completion instructions for an interactive agent step
- **THEN** the instructions reference the completion client by absolute path

#### Scenario: Autonomous-interactive completion does not block on approval
- **WHEN** an autonomous-interactive step's agent signals completion through its completion surface
- **THEN** the completion is delivered without waiting for human permission approval

#### Scenario: Native surface routes through the control channel
- **WHEN** a CLI-native completion surface exists and the user or agent invokes it (for example, typing `/next`)
- **THEN** the completion event is delivered through the control channel and validated against the current step credential, identically to the in-session command

> Note: this task delivers the adapter side (pre-approval emission, probes, `/next` commands). The first scenario becomes fully verifiable end-to-end once the direct-execution cutover task is also complete.

### Requirement: No permission loosening in interactive mode (modified)

In interactive context, no adapter SHALL emit a flag that bypasses or pre-approves the underlying CLI's permission/approval prompts. The human at the terminal supervises permissions; the runner MUST NOT preempt that supervision. Autonomous invocations (both headless and interactive backend) MAY emit such flags, since the step operates without human supervision.

Exception — the completion client: every adapter SHALL provide a completion path that does not require human approval. An adapter MAY pre-approve the completion client only when it can restrict approval to the exact absolute executable path and fixed `step complete` arguments — never a wildcard, shell chaining, substitution, or any other subcommand of the runner binary. If a CLI cannot express that narrow approval safely, its adapter SHALL use another completion integration rather than broadening permissions.

#### Scenario: Adapter omits permission-grant flags in interactive context
- **WHEN** any adapter constructs args for an interactive step
- **THEN** the args do not include any flag that auto-approves tools, paths, URLs, or commands (e.g., `--allow-all`, `--force`, `--yolo`, `--dangerously-skip-permissions`), with the sole exception of the narrow completion-client pre-approval

#### Scenario: Exact completion command runs without prompting
- **WHEN** the agent in an interactive step runs the completion client at its exact absolute path with the fixed `step complete` arguments
- **THEN** the command executes without a human approval prompt

#### Scenario: Broader completion-command forms are not pre-approved
- **WHEN** the agent attempts the completion command with additional arguments, shell chaining, substitutions, or a different agent-runner subcommand
- **THEN** the CLI's normal permission behavior applies; the pre-approval does not cover it

#### Scenario: Unrelated commands retain normal permission behavior
- **WHEN** the agent in an interactive step attempts any command other than the exact completion command
- **THEN** the CLI's normal permission prompts apply, unchanged by the completion-client pre-approval

#### Scenario: CLI that cannot express narrow approval uses another integration
- **WHEN** a CLI cannot restrict pre-approval to the exact absolute executable path and fixed arguments
- **THEN** its adapter provides a different completion integration (such as a native hook or trusted command surface) and does not emit a broader permission flag

#### Scenario: Autonomous-headless adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-headless step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

#### Scenario: Autonomous-interactive adapter MAY include permission-grant flags
- **WHEN** any adapter constructs args for an autonomous-interactive step
- **THEN** the adapter MAY include CLI-specific permission-grant flags as needed for unattended autonomous operation

## Done When

All five adapters emit their proven narrow pre-approval (or implement their documented alternative), all five `TurnDurabilityProbe` implementations pass fixture tests, the `/next` commands exist per the chosen packaging, and unit tests cover every scenario above at the adapter level. No matrix cell remains unproven: every *(P0)* cell is either proven or replaced by its documented alternative integration. `make test` and `make lint` pass, and the existing real-agent E2E suite (five interactive + five headless) still passes as a regression gate — production interactive execution is unchanged.
