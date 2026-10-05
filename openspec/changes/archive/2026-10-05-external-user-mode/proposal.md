## Why

Interactive workflow steps need a real terminal: Agent Runner hands the TTY to the agent CLI and a human types every user turn. Nothing lets an external program play the user, and `--headless` does not make interactive steps work without a TTY.

agent-evals is building a suite (`evals/agent-runner/and-scene-define/`, change `eval-the-whole-workflow`) that evaluates the interactive define workflow (`openspec:change --until define`). An eval-owned simulated user, which holds a hidden reference change, answers the lead agent's questions from outside the Docker sandbox where the workflow runs. That sandbox has no terminal, no GitHub remote, and no GitHub credential. A proof of concept (`agent-evals.eval-the-whole-workflow/openspec/changes/eval-the-whole-workflow/poc/`) drove the real `proposal` step prompt as a loop of headless turns on Claude and Codex. The leads asked plain-text questions, waited for answers, and completed after approval. This change builds that loop into Agent Runner.

## What Changes

- Add an opt-in, per-run **external-user mode**: `agent-runner run <workflow> --external-user <exchange-dir> [--external-user-timeout <duration>]`. The settings are saved in run state and reused on resume. The mode implies no TUI.
- In this mode, each interactive agent step runs as a loop of headless turns on one CLI session:
  - Each turn the agent ends without completing the step goes to an external responder as a request file in the exchange directory.
  - The responder's reply file becomes the next user turn.
  - The loop repeats until the agent completes the step through the existing control channel.
- Each exchange carries run, step, attempt, and turn identity. Replies are recorded and replayed on resume without re-asking. The responder can abort the run cleanly.
- `AskUserQuestion` is disabled and the autonomy preamble is omitted, so skills ask in plain text.
- `call_agent` is pre-authorized for interactive parents in this mode.
- Only Claude and Codex leads are supported; other CLIs are rejected clearly.
- Interactive steps in this mode report real usage, summed across their turns.
- `agent-runner --resume <run-id> --until <step>` caps a resumed invocation. The cap is still not saved in state.
- Codex invocations put `--` before the session ID and prompt. Today a prompt or reply that starts with `-` is parsed as a flag and the turn fails.
- The built-in `validate-feature-branch` workflow determines the default branch locally when the repository has no remotes, instead of requiring `gh`.

## Capabilities

### New Capabilities
- `external-user-mode`: running interactive agent steps without a terminal, with user turns supplied by an external responder through a file exchange. Covers activation, the turn loop, the exchange protocol, completion, replay, failures, supported CLIs, and how other step types behave in the mode.

### Modified Capabilities
- `run-until`: `--until` is accepted on `--resume` as an invocation-only cap.
- `agent-calls`: interactive parents running in external-user mode get pre-authorized `call_agent` access instead of the CLI's approval prompt.
- `agent-usage-collection`: interactive steps in external-user mode report collected usage instead of `interactive-context` unavailable.

## Out of Scope

- The eval suite, the simulated user's policies, judging, and the contamination audit (agent-evals).
- Process or user isolation between the responder and the agent on Fly/Agent Factory machines.
- Cursor, Copilot, and OpenCode leads in this mode.
- Translating `AskUserQuestion` calls into exchanges.
- Any change to interactive TTY behavior when the mode is off.
- Changes to `create-change.sh`. In a repo without a committed `.validator/config.yml`, `agent-validator detect` exits 1 rather than 2. The eval's starting repo will commit a minimal validator config instead.

## Impact

- `cmd/agent-runner/main.go`: new flags on `run`, `--until` on `--resume`, and flag-combination validation.
- `internal/model` (`RunState`, `ExecutionContext`), `internal/runner` (options, resume), `internal/exec/agent.go` (invocation context, adapter input, turn loop), `internal/cli` (Claude and Codex argv for the new context), `internal/control` (completion for headless turns), and usage and audit emission.
- A new protocol package for the exchange files.
- `workflows/core/validate-feature-branch-v1.0.yaml`.
- Docs: `docs/` gets a page describing the mode and the exchange-file contract that the eval's adapter targets.
