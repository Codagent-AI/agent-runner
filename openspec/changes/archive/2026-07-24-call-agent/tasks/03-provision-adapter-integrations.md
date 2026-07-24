# Task: Provision Adapter Integrations

## Goal

Provision the Runner-owned `call_agent` MCP tool process-locally across Claude, Codex, Copilot, Cursor, and OpenCode. Preserve each CLI's existing isolation and permission model, remove generic short host timeouts where supported, and prove the real tool path without modifying global or project configuration.

## Background

You MUST read these approved artifacts before starting:

- `proposal.md`, especially the adapter, verification, dependency, and configuration impacts.
- `design.md`, especially **Decision 1: Expose `call_agent` through a Runner-owned stdio MCP bridge**, **Decision 5: Extend the existing process-local adapter integration**, and the cross-adapter/timeout risks.
- `specs/agent-calls/spec.md` and `specs/cli-adapter/spec.md` for the acceptance criteria copied below.

Relevant implementation and verification paths:

- `internal/cli/adapter.go` defines `BuildArgsInput`, invocation contexts, spawn-environment extension points, and the adapter registry.
- `internal/cli/claude.go`, `internal/cli/cursor.go`, and `internal/cli/completion_plugin.go` generate isolated plugin/configuration surfaces.
- `internal/cli/codex.go` and `internal/cli/completion_plugin.go` create a private per-run `CODEX_HOME` and preserve selected user state.
- `internal/cli/copilot.go` and `internal/cli/opencode.go` build their process-local CLI and configuration inputs.
- Adapter behavior is tested in `internal/cli/adapter_test.go`, `internal/cli/completion_test.go`, `internal/cli/opencode_test.go`, and focused provider tests.
- `.agent-runner/workflows/smoke-test.yaml` already declares the `claude-headless` named session and must exercise a call followed by reuse of that same named session.
- `cmd/agent-runner/real_agent_e2e_test.go` contains the `e2e_agents` harness and environment isolation for real CLI tests.

Consume a trusted Runner integration descriptor rather than reconstructing prompt eligibility inside adapters. The descriptor identifies the fixed internal MCP server command and the narrow permission needed by this invocation, but never carries active attempt credentials. Credentials arrive only through the sanitized child environment.

Provision the server name `agent-runner` and only the `call_agent` tool. Keep one canonical schema/description across adapters. Called children receive no MCP registration and no approval. Eligible autonomous parents pre-authorize only this Runner-owned tool; eligible interactive parents retain the CLI's normal MCP approval flow.

Use each adapter's existing private/process-local mechanism. Do not write persistent user or project settings. Preparation must be atomic from the parent's perspective: an invalid executable, unsafe config, permission conflict, or failed private-home/plugin generation fails before the CLI spawns and reports the cause.

Agent Runner adds no call deadline. Where a CLI has a supported process-local MCP tool timeout control, disable the generic short default or raise it to a high implementation ceiling while preserving an explicit user/client deadline. Where no control exists, keep native host behavior. Do not assume progress notifications extend deadlines.

`OpenCodeAdapter.InteractiveModeError` currently rejects workflow-level interactive OpenCode invocations. Account for that existing constraint without silently broadening this change into general OpenCode interactive support: the agent-call integration builder itself must remain mode-neutral and add no `call_agent`-specific mode restriction, even if the enclosing invocation is rejected by the existing adapter policy.

This task owns adapter registration, permissions, host timeout settings, the project smoke workflow, and real-CLI coverage. Runtime eligibility is already carried in the descriptor, and child execution remains supervised by the Runner.

## Spec

### Shared requirements from `specs/agent-calls/spec.md`

### Requirement: Agent-call tool availability

Agent Runner SHALL expose a `call_agent` tool to an interactive or autonomous agent step when that step's workflow-authored prompt template contains the literal, case-sensitive substring `call_agent`. Agent Runner SHALL evaluate this condition before prompt interpolation and workflow-engine enrichment. A step whose authored prompt does not contain that substring SHALL NOT receive the tool. An agent started by `call_agent` MUST NOT receive the tool regardless of its prompt.

#### Scenario: Interactive enabled parent receives the tool
- **WHEN** Agent Runner starts an interactive agent step whose authored prompt contains `call_agent`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Autonomous enabled parent receives the tool
- **WHEN** Agent Runner starts an autonomous agent step whose authored prompt contains `call_agent`
- **THEN** the agent can invoke `call_agent`

#### Scenario: Prompt without token receives no tool
- **WHEN** Agent Runner starts an ordinary agent step whose authored prompt does not contain `call_agent`
- **THEN** the agent does not receive the `call_agent` tool

#### Scenario: Autonomous enabled parent receives pre-authorized access
- **WHEN** Agent Runner provisions `call_agent` for an autonomous agent step whose authored prompt contains `call_agent`
- **THEN** only the Runner-owned `call_agent` tool is pre-authorized and its invocation does not wait for interactive approval

#### Scenario: Interactive enabled parent uses normal tool approval
- **WHEN** Agent Runner provisions `call_agent` for an interactive agent step whose authored prompt contains `call_agent`
- **THEN** invocation follows that CLI's normal MCP tool-approval flow

#### Scenario: Called child cannot delegate recursively
- **WHEN** `call_agent` starts a child agent
- **THEN** the child does not receive the `call_agent` tool

### Requirement: Long-running MCP execution

Agent Runner MUST NOT impose a fixed duration limit on a valid agent call. The process-local MCP integration SHALL avoid allowing a generic short host tool timeout to govern called-agent execution when the host exposes a supported timeout control, while preserving an explicit deadline configured by the user or requesting client. When an MCP client supplies a progress token, the bridge SHALL emit rate-limited progress notifications while the child remains active. Progress notifications MUST NOT be treated as a substitute for client-side timeout configuration or cancellation.

#### Scenario: Configurable host timeout does not bound the call
- **WHEN** a supported host exposes a process-local MCP tool-execution timeout control
- **THEN** Agent Runner provisions `call_agent` so the host's generic short default does not terminate an otherwise active child

#### Scenario: Requested progress is reported
- **WHEN** an MCP client invokes `call_agent` with a progress token and the child remains active
- **THEN** the bridge emits rate-limited progress notifications until the call reaches a terminal result

### From `specs/cli-adapter/spec.md`

### Requirement: Agent-call tool provisioning

Every registered CLI adapter SHALL provision the `call_agent` tool for an interactive or autonomous parent agent invocation whose workflow-authored prompt template contains the literal `call_agent` substring, using process-local configuration. The integration MUST NOT modify global or project agent configuration and MUST NOT be provisioned to an ordinary step without that substring or to an agent started by `call_agent`. If an adapter cannot prepare the required integration, Agent Runner SHALL fail the enabled parent step with the preparation cause before launching the parent CLI.

#### Scenario: Registered adapter provisions the tool
- **WHEN** Agent Runner prepares a parent agent invocation whose authored prompt contains `call_agent` through any registered CLI adapter
- **THEN** the invocation exposes the `call_agent` tool

#### Scenario: Enabled parent mode does not change availability
- **WHEN** Agent Runner prepares an interactive or autonomous parent agent invocation whose authored prompt contains `call_agent`
- **THEN** the adapter provisions the same `call_agent` capability in either mode

#### Scenario: Unenabled parent omits the integration
- **WHEN** Agent Runner prepares an ordinary parent whose authored prompt does not contain `call_agent`
- **THEN** the adapter does not provision the `call_agent` integration

#### Scenario: Called child omits the integration
- **WHEN** Agent Runner prepares an agent invocation started by `call_agent`
- **THEN** the adapter does not provision `call_agent` to that child

#### Scenario: User configuration remains unchanged
- **WHEN** an adapter provisions `call_agent` for a parent invocation
- **THEN** no global or project agent configuration is created or modified

#### Scenario: Provisioning failure prevents launch
- **WHEN** an adapter cannot prepare a safe process-local `call_agent` integration
- **THEN** Agent Runner fails the parent step before launching its CLI and reports the preparation cause

### Requirement: Long-running tool controls

When a supported CLI exposes process-local control over MCP tool-execution timeouts, its adapter SHALL configure the Runner-owned server so a generic short host default does not govern `call_agent`, while preserving an explicit deadline configured by the user or requesting client. An adapter MUST NOT introduce a Runner-level call duration limit when the host exposes no such control. Timeout handling MUST remain isolated to the spawned parent and MUST NOT modify global or project configuration.

#### Scenario: Supported timeout control is applied process-locally
- **WHEN** an enabled parent uses a CLI with a supported MCP tool-execution timeout setting
- **THEN** the adapter configures the Runner-owned server for long-running calls without changing persistent user or project settings

#### Scenario: Adapter without timeout control adds no Runner deadline
- **WHEN** an enabled parent uses a CLI without a supported MCP tool-execution timeout setting
- **THEN** Agent Runner preserves the host's native behavior and introduces no fixed call duration limit of its own

## Done When

- Table-driven adapter tests cover Claude, Codex, Copilot, Cursor, and OpenCode in eligible autonomous and interactive invocation contexts, ineligible prompts, called children, malformed integration descriptors, and preparation failures.
- Generated args, environment, plugin files, private homes, and config content register the same `agent-runner` server/tool contract and point only to the fixed absolute Agent Runner internal MCP command.
- Autonomous configuration narrowly pre-authorizes `call_agent` without loosening other tools; interactive configuration preserves native approval behavior.
- Tests snapshot or inspect supported long-running timeout controls and prove adapters without such controls add no Runner deadline. An explicit user/client deadline remains authoritative.
- Tests prove global and project configuration bytes, permissions, and mtimes are unchanged after both successful provisioning and preparation failure.
- Called-agent invocation inputs and ordinary prompts without the literal authored token receive no registration, permission, or private integration files.
- `.agent-runner/workflows/smoke-test.yaml` invokes `call_agent` through the declared `claude-headless` session and then proves an ordinary workflow step reuses the call-created session.
- The `e2e_agents` suite includes a real agent path that discovers and invokes the Runner-owned tool, returns the child response to the parent, and verifies named-session reuse without relying on a proprietary subagent API.
- Tests for every scenario copied into this task pass. Run `make fmt`, targeted `internal/cli` and command tests, `go test ./...`, and the documented opt-in real-agent E2E command when credentials are available.

