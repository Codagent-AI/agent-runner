# Decisions

## propose

### Verdict: go

- **Decision:** Go. The change is small and low-risk. It aligns Claude with the Codex and Copilot adapters and with the current SHALL clauses in `cli-adapter` and `agent-calls`.
- **Alternatives considered:** No-go and rely only on `get_agent_call` polling. Rejected: that costs extra round trips and depends on the parent agent recovering from an abort.
- **Decision-bearing:** yes

### Mechanism: `MCP_TOOL_TIMEOUT` process environment variable

- **Decision:** Set `MCP_TOOL_TIMEOUT` through `ClaudeAdapter.SpawnEnv`, as the issue specifies.
- **Alternatives considered:** A per-server `timeout` field in the generated agent-call plugin `.mcp.json`. It has tighter scope, but it was rejected for now:
  - the issue names the env var;
  - per-server handling has reportedly regressed across Claude Code releases;
  - plugin-sourced support is unverified;
  - the plugin directory is shared with Cursor.

  Design may revisit it if verified.
- **Decision-bearing:** yes. The trade-off is that the raised ceiling also covers other MCP servers in the same spawned process.

### Value: `2147483647` ms (shared `agentCallTimeoutMilliseconds`)

- **Decision:** Reuse the Copilot constant. It is the largest Node.js timer value that does not overflow to an immediate timeout.
- **Alternatives considered:** A 30-day value like Codex's `tool_timeout_sec`. Rejected: in milliseconds it exceeds the Node timer limit. A finite shorter value was rejected because Runner must not impose a call duration limit.
- **Decision-bearing:** no

### Respect a user-set `MCP_TOOL_TIMEOUT`

- **Decision:** If the variable is already defined in the runner environment, leave it unchanged. This follows the spec's "preserve explicit deadline configured by the user" and the existing `BASH_DEFAULT_TIMEOUT_MS` pattern.
- **Alternatives considered:** Always override. Rejected because it contradicts the spec.
- **Decision-bearing:** no

### Scope: only when agent-call integration is present, both interactive and headless

- **Decision:** Gate on `validatedAgentCall(input)`, and apply in every invocation context that carries the integration.
- **Alternatives considered:** Headless-only, like the existing `SpawnEnv` entries. Rejected because interactive parents also get the plugin. Applying it always was rejected because it would broaden the side effect to runs without agent calls.
- **Decision-bearing:** no

### Spec interaction with PR #239

- **Decision:** Model this as a modification of `cli-adapter` "Long-running tool controls" and `agent-calls` "Long-running MCP execution". It adds Claude scenarios and restores SHALL if #239 softened it to MAY.
- **Alternatives considered:** Leave the spec wording untouched. Rejected because the issue's stated purpose is to bring Claude in line so the stronger contract holds.
- **Decision-bearing:** no. It is within the issue's stated direction, and the exact wording is settled at the spec step against whatever main contains then.

## proposal-review

### PR-001: `MCP_TOOL_TIMEOUT` does not raise Claude's stdio idle timeout (significant)

- **Decision:** Applied.
  - Corrected the Out of Scope claim that the idle timeout is remote-only. From v2.1.203 it also applies to stdio calls.
  - Narrowed the proposal and spec contract to raising the wall-clock tool-execution timeout. Dropped the uninterrupted-wait claim.
  - Recorded that idle aborts stay recoverable through `get_agent_call`.
  - Listed idle policy, progress-token verification, and a Claude-only per-server timeout as follow-up work.
- **Alternatives considered:**
  - Set `CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT` as well. Rejected: it is beyond the issue's ask, and it is unverified whether the variable is process-wide and whether it preserves a user override.
  - Switch to a per-server `.mcp.json` timeout. Rejected for now: it is unverified, and the plugin directory is shared with Cursor.
- **Decision-bearing:** no. This narrows the claims to what the issue requested and does not change direction.

### PR-002: interactive Claude auto-backgrounds long MCP calls (minor)

- **Decision:** Applied.
  - Kept Claude's default interactive backgrounding.
  - Qualified What Changes, Capabilities, and Impact: both modes get the raised ceiling, and interactive parents may get a backgrounded task whose result arrives later.
  - Added `CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS` and background-task changes to Out of Scope.
- **Alternatives considered:** Set `CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS`, or disable background tasks for interactive runs. Rejected: either would change interactive UX policy and goes beyond the issue.
- **Decision-bearing:** no

## spec

### Delta base: main's current requirement text

- **Decision:** Write MODIFIED deltas for `cli-adapter` "Long-running tool controls" and `agent-calls` "Long-running MCP execution". Base them on main's current SHALL text, since PR #239 is still open and unmerged.
- **Alternatives considered:**
  - Base the deltas on #239's MAY wording. Rejected because it is not on main.
  - Base them on the unarchived async-mcp delta. Rejected because it is not on main either.
- **Decision-bearing:** no. If #239 merges first, the archive step reconciles the wording. The intended outcome is unchanged: hosts with a wall-clock control SHALL raise it, Claude included, and idle or backgrounding limits remain recoverable through `get_agent_call`.

### Spec scope: wall-clock control only

- **Decision:** Add Claude scenarios for each of these cases:
  - headless and interactive invocations with the integration;
  - a user-set value preserved;
  - no integration means no variable;
  - no persistent config writes.

  Add an agent-calls scenario stating that an idle or backgrounding detach does not lose the child.
- **Alternatives considered:** Specify `CLAUDE_CODE_MCP_TOOL_IDLE_TIMEOUT` or auto-backgrounding behavior. Rejected because they are out of scope per proposal-review PR-001 and PR-002.
- **Decision-bearing:** no

### Value pinned in the spec

- **Decision:** The spec names `2147483647` ms as the exact value, which makes it testable and keeps it aligned with Copilot.
- **Alternatives considered:** Say only "a long value". Rejected because it is not testable.
- **Decision-bearing:** no

### Recovery wording

- **Decision:** The idle or backgrounding scenario says the parent "can still reach its terminal result with `get_agent_call`". A parent whose request was aborted learns the active `call_id` from the `call_in_progress` rejection of a new `call_agent` (`internal/exec/agent_call.go`).
- **Alternatives considered:** Require the parent to already hold the `call_id`. Rejected because an aborted request may never have returned one.
- **Decision-bearing:** no

## design

### Implementation point: `ClaudeAdapter.SpawnEnv`

- **Decision:** `SpawnEnv` produces the headless entries and the agent-call entry as two independent contributions.
  - It gates the agent-call entry on `validatedAgentCall` and returns that function's error, as Codex does.
  - It returns `nil` when there is nothing to contribute.
- **Alternatives considered:**
  - A per-server timeout in the plugin `.mcp.json`. Rejected because the plugin is shared with Cursor and the field is unverified.
  - A Claude-specific settings flag. Rejected because it is not process-local environment and the issue names the env var.
- **Decision-bearing:** no

### Inherited value semantics: "defined", not "non-empty"

- **Decision:** Use `os.LookupEnv`, matching the `BASH_DEFAULT_TIMEOUT_MS` precedent.
- **Alternatives considered:** Treat an empty value as unset. Rejected because it is inconsistent with the existing pattern.
- **Decision-bearing:** no

### Value source: reuse `agentCallTimeoutMilliseconds`

- **Decision:** Reuse the constant shared with Copilot, formatted with `strconv.FormatInt`.
- **Alternatives considered:** A separate Claude constant. Rejected because the values could drift.
- **Decision-bearing:** no

### Test isolation

- **Decision:** Existing Claude `SpawnEnv` tests and `agentCallTestInput` unset `MCP_TOOL_TIMEOUT`, so a developer's environment cannot change their results. Agent-call registration tests keep asserting a plugin timeout of 0, which protects Cursor's shared plugin.
- **Alternatives considered:** None.
- **Decision-bearing:** no

## test-plan

### One integration obligation, no E2E

- **Decision:** INT-001 covers the step wiring from a `call_agent` step through `buildStepInvocation` and the real Claude adapter to the spawn environment. No E2E test is added.
- **Alternatives considered:** An E2E test that runs a real Claude session past the default tool timeout. Rejected: it would take hours, needs credentials, and tests Claude's behavior rather than Runner's contract.
- **Decision-bearing:** no

### Acceptance envelope

- **Decision:**
  - Allow a stub `claude` executable on `PATH` that records its environment, for observing the spawned process.
  - Allow a few short real Claude runs to confirm launch.
  - Forbid persistent Claude or shell configuration changes.
  - Human-only testing: none.
- **Alternatives considered:** Require real long-running Claude verification. Rejected for cost and duration, and because it is outside Runner's contract.
- **Decision-bearing:** no

## approach-review

### AR-001: unconditional `get_agent_call` recovery promise is not supported by the code (medium)

- **Decision:** Applied, following the recommended path: the change stays limited to the wall-clock environment control, and the recovery claims are qualified.
  - **agent-calls delta:**
    - Recovery is guaranteed only for a parent that holds the `call_id`.
    - `call_in_progress` discloses a missing ID only while the call is still active.
    - A `call_agent` issued after completion starts a new child.
    - A new scenario, "Active call discloses its call_id to a retrying parent", records today's behavior. The existing test `internal/exec/agent_call_test.go` (concurrent start returns `call_in_progress` with `call_id`) already covers it.
  - **Proposal and design:**
    - The Why, Capabilities, Out of Scope, and Impact sections, and the design's docs instructions and risks, now carry the same qualification.
    - The docs must not present a retried `call_agent` as a safe general retry.
    - Lost-ID recovery after completion is recorded as an existing limitation and follow-up work.
- **Alternatives considered:** Add attempt-scoped recovery of accepted calls, with a bridge/handler test for an abort before the ID is delivered. Rejected for this change: it is new agent-call protocol behavior beyond the issue, which asks only for the Claude timeout.
- **Decision-bearing:** no. This narrows the claims and does not change direction.

## tasks

### Single implementation task

- **Decision:** One task covers the `SpawnEnv` change, the unit tests, INT-001, and the docs update. It follows the archived feature-211 `tasks.md` format and links to every source artifact.
- **Alternatives considered:** Split it into separate code, test, and docs tasks. Rejected because the step requires exactly one task and the change is small.
- **Decision-bearing:** no
