## MODIFIED Requirements

### Requirement: Long-running MCP execution

Agent Runner MUST NOT impose a fixed duration limit on a valid agent call. When a host exposes a supported process-local MCP tool-execution wall-clock timeout control, the process-local MCP integration SHALL raise it so that a generic short host tool timeout does not govern called-agent execution, while preserving an explicit deadline configured by the user or requesting client. Claude is such a host. Raising the wall-clock control does not guarantee one uninterrupted waiting call: a host's idle timeout or automatic backgrounding of long tool calls can still end or detach the waiting request. In that case the child SHALL continue running within the parent attempt, and a parent that holds the call's `call_id` SHALL be able to obtain its result through `get_agent_call`. Recovering a `call_id` the parent never received is not guaranteed. A new `call_agent` discloses the active call's `call_id` through a `call_in_progress` rejection only while that call is still active. After the call completes, a new `call_agent` starts a new child. When an MCP client supplies a progress token, the bridge SHALL emit rate-limited progress notifications while the child remains active. Progress notifications MUST NOT be treated as a substitute for client-side timeout configuration or cancellation.

#### Scenario: Configurable host timeout does not bound the call
- **WHEN** a supported host exposes a process-local MCP tool-execution timeout control
- **THEN** Agent Runner provisions `call_agent` so the host's generic short default does not terminate an otherwise active child

#### Scenario: Claude parent's wall-clock tool timeout is raised
- **WHEN** a Claude parent with the agent-call integration invokes `call_agent` for a child that runs longer than Claude's default MCP tool timeout, and the user has not set `MCP_TOOL_TIMEOUT`
- **THEN** Claude's default wall-clock tool timeout does not abort the waiting call

#### Scenario: Host idle or backgrounding does not lose the child
- **WHEN** a host's idle timeout or automatic backgrounding ends or detaches a waiting `call_agent` request while the child is still active
- **THEN** the child continues to run, and a parent that holds the call's `call_id` can still reach its terminal result with `get_agent_call`

#### Scenario: Active call discloses its call_id to a retrying parent
- **WHEN** a parent whose waiting request was ended before it received a `call_id` invokes `call_agent` again while the original child is still active
- **THEN** Agent Runner rejects the new call with `call_in_progress`, reports the active call's `call_id`, and does not start another child

#### Scenario: Requested progress is reported
- **WHEN** an MCP client invokes `call_agent` with a progress token and the child remains active
- **THEN** the bridge emits rate-limited progress notifications until the call reaches a terminal result
