# agent-runner

## 0.1.1

### Minor Changes

- [#38](https://github.com/Codagent-AI/agent-runner/pull/38) Improve the new-workflow tab so workflows are grouped more clearly, hidden/current-directory visibility is easier to understand, and descriptions are more useful while browsing.

### Patch Changes

- [#39](https://github.com/Codagent-AI/agent-runner/pull/39) Add a built-in debug workflow and failure entry points so failed runs can be inspected through state/audit-summary commands and routed toward user remediation or issue filing.

## 0.1.0

### Minor Changes

- Initial public release of Agent Runner, a Go CLI workflow orchestrator for deterministic multi-step AI agent workflows.
- Add built-in onboarding workflows, live run and list TUIs, workflow composition, step execution for agents/shell/scripts/UI, session resume support, and Agent Validator integration.
- Add release automation and Homebrew packaging support for publishing `agent-runner`.

### Patch Changes

- Fix live-run follow behavior while UI steps are active so follow mode does not snap back to the previous process-backed step.
- Treat omitted optional workflow parameters as empty strings during interpolation, allowing shell conditionals such as optional validator context files to render and branch correctly.
- Improve the Docker live-update skill with binary permission checks and robust install target discovery.
