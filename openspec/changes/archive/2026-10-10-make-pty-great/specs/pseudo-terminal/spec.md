# Capability: pseudo-terminal (delta)

## REMOVED Requirements

### Requirement: Runtime pseudo-terminal hosting
**Reason**: Interactive agent and shell children now inherit the user's real terminal directly. Agent Runner no longer hosts, relays, captures, resizes, or parses terminal byte streams in production.
**Migration**: See `interactive-terminal-handoff` for direct terminal inheritance and `interactive-shell-steps` for shell exit and output semantics.

### Requirement: In-band continue triggers
**Reason**: Agent completion uses the authenticated `step-control-channel`; shell steps complete from exit status. No runtime component observes terminal bytes for commands, keys, or sentinels.
**Migration**: Use the process-local native completion command, ask the agent to continue, or let an interactive shell command exit.

### Requirement: Relay lifecycle and resize propagation
**Reason**: Directly inherited terminal descriptors receive resize events and terminal protocols natively, so relay drain, close ordering, and explicit resize propagation no longer exist.
**Migration**: Process-group termination, job control, terminal restore, and crash cleanup are defined by `interactive-terminal-handoff`.
