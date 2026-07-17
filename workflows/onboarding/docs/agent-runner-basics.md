# Agent Runner Basics

Agent Runner is a Go CLI workflow orchestrator for AI agents. A workflow is a YAML file with ordered steps. The runner owns orchestration, state, resume behavior, captures, and audit records while individual agent CLIs handle agent conversations.

Common step types:

- UI steps (`mode: ui`) show a title, body, and actions. They can capture an action outcome for later steps.
- Interactive agent steps (`mode: interactive`) open an agent session that can converse with the user before the workflow continues.
- Autonomous agent steps (`mode: autonomous`) give an agent a bounded task and wait for the result without live interaction.
- Shell steps (`command:`) run deterministic local commands and can capture stdout into workflow variables.
- Script steps (`script:`) run static workflow-local or bundled scripts.
- Sub-workflow steps (`workflow:`) call another workflow, including embedded workflows from the same namespace.

Captured values can be interpolated later with `{{name}}`. Built-in onboarding workflows use this to pass choices and command output between steps without writing project files.

## Interactive Continuation

Interactive agent steps run in a live terminal session. The workflow waits there until the current step sends an authenticated completion event:

- The user asks the agent to continue, and the agent completes the step.
- The user types `/agent-runner:next` in Claude, Copilot, or Cursor.
- The user invokes `$agent-runner-next` in Codex.

When an interactive step starts, Agent Runner appends an absolute-path completion command to the agent prompt. The command sends a credentialed event through a private local control socket. Agent Runner acknowledges it, waits until the CLI has durably saved the completing turn, then closes the session and moves to the next workflow step. Workflow prompts often describe this as "complete the step" or "signal continuation."

Agent Runner does not draw a continuation overlay or intercept a global keyboard shortcut. If the agent exits before completion is accepted, Agent Runner treats the step as aborted so the run can be resumed later instead of silently skipping ahead.

The optional demo is available as `agent-runner run onboarding:onboarding`. Native first-run setup creates agent profiles before the demo, and the demo shows how workflow primitives compose into an end-to-end run.
