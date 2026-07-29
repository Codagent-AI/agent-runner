## Why

Agent Runner can sequence agents deterministically, but an agent cannot delegate a task discovered during its own turn without doing the work itself or relying on a CLI-specific subagent system. A runner-native agent-call tool enables portable orchestration across models and cost tiers while preserving Agent Runner's profiles, sessions, audit trail, and execution controls.

## What Changes

- Expose a synchronous agent-call tool to interactive and autonomous runner-spawned agent steps whose workflow-authored prompt contains the literal, case-sensitive `call_agent` substring, on all supported adapters: Claude, Codex, Copilot, Cursor, and OpenCode. The scan occurs before interpolation and engine enrichment. Called agents do not receive the tool regardless of their prompt.
- Accept the agent-step invocation fields that apply to an in-turn call: `prompt`, `agent`, `session`, `cli`, `model`, and `workdir`. A bare `agent: <profile>` selects a fresh profile-backed session and can override `cli`, `model`, or `workdir`; `session: <name>` selects a workflow-declared named session, forbids `agent` and `cli`, and can override `model` or `workdir`. The `session` field is reserved for declared names, so agent calls reject `session: new`, `session: resume`, `session: inherit`, and `mode` because child execution is always autonomous-headless.
- Reuse a named session's existing run-scoped CLI session when it has already been created by a workflow step or earlier agent call; otherwise create it through the named session's existing first-use behavior. Create a fresh session for every direct `agent` call.
- Reject a call whose named-session target resolves to the calling parent's active CLI session, preventing concurrent turns against the same native session.
- Permit at most one in-flight agent call per parent attempt; reject a concurrent second call with a structured `call_in_progress` error that identifies the active target and elapsed time and instructs the model to wait rather than queueing, canceling, or running calls in parallel.
- Run called agents in autonomous-headless mode in the same worktree, using the target profile's CLI, model, effort, system prompt, and the runner's existing execution controls. A call receives the supplied prompt and profile system prompt, not workflow-engine step enrichment.
- Let valid calls run for the duration of their supervised child execution without a Runner-imposed call timeout. Adapter provisioning prevents generic short host defaults from terminating long-running calls where the host exposes such a control, and the bridge reports MCP progress when requested.
- Treat the live MCP request as the call's lease: client cancellation, bridge exit, or control-connection loss cancels the child and releases the in-flight slot without ending the parent attempt.
- Return the called agent's final response or a structured failure to the parent tool call while retaining child execution evidence in runner state, output, metrics, and audit data.
- Make the tool available only to the parent invocation; called agents cannot recursively call other agents in this version.
- Exercise named-session delegation in the project-local smoke-test workflow and add real-agent end-to-end coverage for the tool path.

## Capabilities

### New Capabilities

- `agent-calls`: Defines prompt-gated tool availability, the supported invocation fields and validation rules, named-session and fresh-profile targeting, synchronous autonomous-headless execution, long-running request and cancellation behavior, self-session and concurrent-call rejection, result and failure behavior, shared-worktree semantics, and the single-level delegation boundary.

### Modified Capabilities

- `named-sessions`: Agent calls can use the existing named-session map, reusing sessions already created by scheduled workflow steps or earlier calls and retaining the same first-use, run-scoped persistence, and composition rules.
- `cli-adapter`: Every supported adapter provisions the runner-owned agent-call tool process-locally for prompt-enabled parents, without mutating global or project CLI configuration, and applies supported long-running tool controls.
- `step-control-channel`: The authenticated runner control plane is created for interactive or headless parent steps that receive runner integration, issues a fresh credential to each such active attempt, accepts synchronous agent-call requests in addition to interactive step-completion events, and cancels calls whose client connection is lost.
- `audit-log-entries`: Agent-call lifecycle and nested child execution data are recorded beneath the active parent step and contribute to run evidence and metrics.
- `run-metrics-artifact`: Called-agent executions receive distinct nested records and contribute usage and cost to run totals without being counted as workflow steps or double-counted through their parent.
- `view-run`: Agent calls appear as inspectable child execution rows beneath their parent agent step, with explicit target labels, call details, output, and session resume behavior.
- `live-run-view`: Calls from autonomous parents appear and stream output live with normal auto-follow behavior, while calls from interactive parents preserve terminal ownership and appear when the run view resumes.
- `run-complete-screen`: Parent agent rows roll up their own turn and called-agent metrics, and drill-down separates the parent turn from each call without double-counting overlapping duration.

## Technical Approach

Extend Agent Runner's private, authenticated per-run control-plane pattern with a process-local MCP tool bridge injected by each CLI adapter. An agent step is eligible when its workflow-authored prompt template contains the literal, case-sensitive `call_agent` substring before interpolation or engine enrichment. Before any interactive or headless eligible parent starts, the runner ensures the control endpoint exists and gives that attempt a fresh credential. The bridge identifies the active run and parent attempt, forwards a validated autonomous invocation to the supervising runner, waits while the runner executes the child through the existing agent executor, reports MCP progress when requested, and returns the child's final response or structured failure. The bridge's live connection leases the call so request cancellation or connection loss terminates the child.

```text
parent agent step
  -> agent-call tool bridge
     -> authenticated runner control plane
        -> resolve named session or fresh agent profile
        -> execute autonomous-headless child via existing adapter path
        -> persist session, output, usage, cost, and audit evidence
     <- child response or structured failure
  <- tool result; parent decides how to proceed
```

Named-session calls use the existing run-scoped name-to-session mapping, including sessions created by ordinary workflow steps, so subsequent references resume the same CLI session and survive runner resume. Calls with a bare `agent` deliberately bypass that mapping and start fresh. Direct agent calls can override `cli`, `model`, and `workdir`; named-session calls retain the declared profile's CLI and can override only `model` and `workdir`. The runner rejects a named target that resolves to the parent's active CLI session and enforces one in-flight call per parent attempt, returning an instructive structured error for a concurrent second request. Child invocations do not receive the agent-call integration, preventing recursive delegation.

## Out of Scope

- Interactive called agents or terminal handoff from a parent to a child.
- Recursive child-to-child delegation.
- Parallel calls, fan-out, scheduling, or result aggregation across multiple children; a second concurrent call from the same parent attempt is rejected rather than queued.
- Agent-call duration budgets or a model-selected timeout; cancellation remains owned by the parent, client, and supervising runner.
- Wrapping or standardizing the proprietary subagent APIs provided by individual agent CLIs.
- Automatically translating agent calls into new workflow steps or modifying normal workflow sequencing.
- Updating `implement-change2` or `review-assumptions` to use the tool.

## Impact

- **Execution and control:** Agent execution, runner supervision, authenticated control endpoint lifecycle and credentials for interactive and headless parents, session resolution, state persistence, output capture, and nested execution context will support an agent invocation that occurs while its parent step remains active.
- **CLI adapters:** Claude, Codex, Copilot, Cursor, and OpenCode integrations will provision the tool for prompt-enabled parents through isolated per-process configuration, following the existing completion-integration pattern where applicable, and will apply supported long-running tool controls.
- **Audit and metrics:** Called agents will remain attributable to their parent step while retaining their own execution, session, usage, cost, output, and failure evidence.
- **Workflow/config APIs:** Existing `sessions:` declarations and agent profiles gain a new runtime consumer; existing workflow YAML remains valid and unchanged unless authors choose to use agent calls.
- **Verification:** `.agent-runner/workflows/smoke-test.yaml` and real-agent end-to-end tests will verify delegation and named-session reuse without changing `implement-change2`.
- **Dependencies and systems:** The official MCP Go SDK is added for the stdio bridge. No external orchestration service or global agent-CLI configuration is introduced; the feature remains local to the Agent Runner process and its child CLIs.
