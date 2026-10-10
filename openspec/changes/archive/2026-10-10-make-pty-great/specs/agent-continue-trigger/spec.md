# Capability: agent-continue-trigger (delta)

## REMOVED Requirements

### Requirement: Agent-initiated continue via PTY sentinel
**Reason**: In-band sentinel detection is retired along with byte-stream parsing; the runner no longer sees the agent's terminal output at all. The sentinel's per-attempt freshness guarantee (stale markers cannot advance a later step) is carried forward by the control channel's per-step single-use credential.
**Migration**: Agent-initiated continuation is the `step-control-channel` completion event: the agent signals completion through the control endpoint (via the injected instructions, the in-session `agent-runner step complete` command, or a CLI-native surface), validated against the current step attempt's credential.

### Requirement: Sentinel instruction injection
**Reason**: Sentinel instructions are meaningless without a sentinel detector.
**Migration**: See `step-control-channel` "Completion instruction injection" — interactive and autonomous-interactive prompts receive control-channel completion instructions; headless prompts receive none, preserving the existing interactive-yes/headless-no rule.
