- [x] Implement the change described by these files:
  - [proposal.md](proposal.md)
  - [specs/agent-calls/spec.md](specs/agent-calls/spec.md)
  - [specs/cli-adapter/spec.md](specs/cli-adapter/spec.md)
  - [specs/step-control-channel/spec.md](specs/step-control-channel/spec.md)
  - [specs/call-agent-skill/spec.md](specs/call-agent-skill/spec.md)
  - [design.md](design.md)

- [x] Add failing tests for accepted `call_agent` returning `call_id` and a non-terminal `accepted` or `running` status without waiting, immediate `get_agent_call` snapshots, cached terminal results including post-accept launch failure via `get_agent_call`, explicit cancel, MCP/bridge disconnect after accept leaving the child running, `call_in_progress` including `call_id`, and adapter provisioning of all three tools
- [x] Split the control-channel agent-call RPC so start accepts and returns `call_id` without waiting for the child, get returns status or the cached terminal result, and cancel terminates a running child
- [x] Lease an accepted child to the parent attempt; do not cancel it when a start/poll MCP request, stdio bridge, or control connection ends
- [x] Publish `call_agent`, `get_agent_call`, and `cancel_agent_call` from the process-local MCP bridge with schemas and descriptions that match the specs
- [x] Provision and autonomously pre-authorize all three tools from the existing `tools: [call_agent]` declaration across supported CLI adapters
- [x] Update `docs/agent-calls.md` for start/poll/cancel, the parent-attempt lease, and troubleshooting of host `tools/call` timeouts
- [x] Update `/Users/paul/codagent/agent-skills/skills/call-agent/SKILL.md` to start, poll until terminal, and cancel explicitly; do not treat in-progress status as success
- [x] Make the new tests pass and keep existing agent-call coverage aligned with the non-blocking MCP contract
- [x] Exclude nested agent-call session IDs from parent Cursor interactive discovery so a child chat cannot blank the parent session ID

