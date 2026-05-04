# Agent Runner Basics

Agent Runner is a Go CLI workflow orchestrator for AI agents. A workflow is a YAML file with ordered steps. The runner owns orchestration, state, resume behavior, captures, and audit records while individual agent CLIs handle agent conversations.

Common step types:

- UI steps (`mode: ui`) show a title, body, and actions. They can capture an action outcome for later steps.
- Interactive agent steps (`mode: interactive`) open an agent session that can converse with the user before the workflow continues.
- Headless agent steps (`mode: headless`) give an agent a bounded task and wait for the result without live interaction.
- Shell steps (`command:`) run deterministic local commands and can capture stdout into workflow variables.
- Script steps (`script:`) run packaged scripts that ship with a built-in workflow.
- Sub-workflow steps (`workflow:`) call another workflow, including embedded workflows from the same namespace.

Captured values can be interpolated later with `{{name}}`. Built-in onboarding workflows use this to pass choices and command output between steps without writing project files.

The optional demo is available as `agent-runner run onboarding:onboarding`. Native first-run setup creates agent profiles before the demo, and the demo shows how workflow primitives compose into an end-to-end run.
