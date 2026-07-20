# Design: Runner-Native Agent Calls

## Context

Agent Runner currently owns agent selection, CLI invocation, named-session reuse, process supervision, audit logging, metrics, and workflow advancement. Agent steps are executed as workflow nodes, and the existing authenticated control channel primarily supports completion of interactive steps.

`call_agent` introduces an invocation inside an already-running agent attempt. The parent CLI remains alive while the called CLI executes, so the two invocations overlap even though the tool call is synchronous. They must have independent process, output, audit, and metric scopes while sharing the run's profile definitions, named-session map, and cancellation lifecycle.

The integration must work across Claude, Codex, Copilot, Cursor, and OpenCode without modifying global or project CLI configuration. It must also preserve Agent Runner's architectural ownership: MCP exposes a tool to the agent, but Agent Runner—not the MCP process or another orchestration service—continues to own execution policy and durable evidence.

Affected stakeholders include workflow authors delegating work between profiles, users combining models or cost tiers, adapter maintainers, and consumers of audit and run-metrics artifacts.

## Goals / Non-Goals

### Goals

- Expose `call_agent` as a native, process-local MCP tool on every supported adapter when the workflow-authored parent prompt contains the literal `call_agent` substring.
- Reuse Agent Runner's profile, adapter, session, permission, workdir, output, and usage machinery.
- Support fresh profile calls and persistent workflow-declared named sessions.
- Keep parent and called-child execution evidence distinct and attributable.
- Propagate cancellation and prevent orphaned child processes.
- Display calls coherently in live, completed, and inspected run views.
- Preserve serialized agent-call semantics: one call may run at a time per parent attempt.
- Keep credentials attempt-scoped and prevent called children from delegating recursively.

### Non-Goals

- Parallel calls, fan-out, background calls, or asynchronous result collection.
- Recursive delegation or multi-level agent trees.
- A general multi-agent scheduler, task ledger, mailbox, or messaging protocol.
- Remote agents, A2A transport, hosted orchestration, or a long-running MCP daemon.
- New session strategies beyond fresh profile calls and existing declared named sessions.
- Automatic retries, call budgets, merge coordination, or workspace isolation beyond the parent worktree.
- Updating `implement-change2` or `review-assumptions` to use the tool in this change.
- Replacing the existing CLI command used to complete workflow steps.

## Decisions

### 1. Expose `call_agent` through a Runner-owned stdio MCP bridge

Each eligible parent CLI invocation receives a process-local MCP server registration pointing to the current Agent Runner executable and a fixed internal subcommand. Eligibility is determined by a literal, case-sensitive `call_agent` substring in the workflow-authored step prompt template before interpolation or engine enrichment. The server is named `agent-runner` and exposes only `call_agent`. Called children are ineligible regardless of their prompt.

The MCP process does not launch agents itself. It inherits the active attempt's control credentials, forwards a typed request over the private Unix socket, waits for the Runner's response, emits rate-limited MCP progress notifications when the client supplied a progress token, and translates the response into an MCP result or tool error:

```text
parent CLI
  -> process-local call_agent MCP tool
     -> agent-runner internal MCP bridge
        -> authenticated per-run control socket
           -> supervising Runner
              -> called agent CLI
```

The implementation uses the official MCP Go SDK for protocol framing, lifecycle negotiation, tool schemas, cancellation, and compatibility.

The initial version uses an ordinary synchronous `tools/call` request. MCP defines progress and cancellation but no universal per-request timeout value; the requesting host controls its deadline. Experimental task-augmented execution is not sufficiently portable across the five supported CLIs and would contradict this version's synchronous contract.

Alternatives considered:

- A skill invoking a shell command is sufficient for fixed workflow completion, but arbitrary multiline prompts, structured responses, permission matching, and mid-turn failures make it a weaker boundary for agent calls.
- A persistent HTTP service adds endpoint lifecycle and security complexity.
- An external orchestration service would split ownership of sessions, processes, and evidence.
- MCP task-augmented execution supports polling and deferred results but is experimental, client-dependent, and outside the synchronous v1 scope.

### 2. Generalize the existing control channel

Socket lifecycle, authentication, active-attempt registration, and request deduplication move from `internal/interactive` into a mode-neutral `internal/control` package. Interactive completion remains a consumer of this package.

Before spawning any parent that receives Runner tools, the executor activates an attempt containing:

- run, step, and attempt identity;
- a fresh credential;
- completion checkpoint information where applicable;
- the registered agent-call handler; and
- a context canceled when the attempt ends.

The control server validates every request against the active attempt. For agent calls, the first accepted request ID creates a shared in-progress result. A duplicate request ID waits for or receives that same result. A different request while a call is active receives `call_in_progress`, including the active target and elapsed time plus an instruction that calls are serial and the caller must wait for the active call to finish or be canceled. A later request never preempts an active child because that child may already be mutating the worktree.

The accepted MCP request and its authenticated control connection lease the call. MCP cancellation closes the bridge-side control request; bridge exit or connection loss has the same effect. The control server cancels the child process group, records its terminal evidence, caches the canceled result for same-request-ID retries, and releases the in-flight slot while keeping the parent attempt active. Deactivating the parent attempt also cancels the active call and releases waiting clients.

All inherited `AGENT_RUNNER_CONTROL_*` variables are removed before every agent spawn. Current-attempt values are then injected only into eligible parents. Called children receive neither the MCP registration nor usable credentials.

Alternatives considered:

- Keeping the server under `internal/interactive` creates a misleading headless dependency.
- A second call-specific socket duplicates authentication and lifecycle rules.

### 3. Extract a shared agent-invocation core

The current agent-step executor is split into thin workflow-step and agent-call wrappers around a shared invocation core:

```text
workflow-step wrapper --+
                        +--> shared agent-invocation core
agent-call wrapper -----+
```

The shared core owns profile resolution, invocation overrides, adapter construction, session resume and discovery, process execution, output filtering, usage collection, and cost collection. It returns a rich invocation result rather than emitting workflow-step semantics itself.

The wrappers retain their distinct behavior:

- The workflow-step wrapper emits step events, captures workflow outputs, advances ordinary session bookkeeping, and applies workflow-step lifecycle behavior.
- The agent-call wrapper emits call events, produces the MCP result, and records nested evidence.
- A named-session call updates and flushes the shared named-session map.
- A fresh profile call does not enter that map or change the workflow's last-session-step state.
- Calls do not run workflow-engine step enrichment because they are not workflow steps; they use the resolved profile system prompt and supplied call prompt.

Alternatives considered:

- A synthetic workflow step would emit incorrect step events and mutate workflow sequencing state.
- Duplicating the executor would allow session and adapter behavior to drift.
- Executing from the MCP process would divide Runner ownership.

### 4. Make process execution invocation-scoped

Every agent invocation receives immutable execution options containing its context, environment, workdir, structural prefix, output routing, and supervision settings. Mutable current-step fields are removed from shared process runners.

The called child's context derives from the parent attempt. Each invocation receives its own process group and output pipeline. Existing process-identity and watchdog primitives are reused so cancellation, parent exit, or Runner termination cleans up the called child and its subprocesses.

This is transient process scope, not a second persisted workflow-state tree. Durable relationships remain in call identity, audit events, metrics, output artifacts, and named-session state.

Alternatives considered:

- A separate child-only runner duplicates launch and cleanup behavior.
- Locking the existing mutable runner cannot safely represent overlapping parent and child lifetimes.

### 5. Extend the existing process-local adapter integration

The adapter input carries a Runner integration descriptor rather than only the current completion command. It identifies the fixed completion client and fixed MCP server command without carrying attempt credentials.

Adapters provision the MCP server through their existing isolated mechanisms:

- Claude and Cursor use generated plugin configuration.
- Copilot uses process-local MCP configuration.
- Codex uses generated private `CODEX_HOME` configuration.
- OpenCode uses process-local configuration content.

Exact host-visible namespacing may vary by CLI, but the server name, tool schema, and behavior remain identical. Preparation failure fails the enabled parent before its CLI starts. Global and project agent configuration remain untouched.

Adapters provision the integration only when the workflow-authored step prompt template contains the literal `call_agent` substring. The scan deliberately does not inspect interpolated values, profile system prompts, workflow-engine enrichment, or later interactive messages. This makes the authored prompt the capability opt-in without adding a workflow-schema field. Mere provisioning does not invoke a child or incur child-agent cost.

Agent Runner imposes no call-level duration limit. Where a host exposes process-local MCP tool-execution timeout control, the adapter disables a generic short default or raises it to a high implementation ceiling so it does not normally govern called-agent execution. An explicit deadline configured by the user or requesting client remains authoritative. Where no such control exists, Agent Runner preserves native host behavior rather than inventing a Runner deadline. Progress is emitted when requested for visibility, not relied upon to reset client deadlines.

Eligible autonomous parents receive pre-approval for only the Runner-owned `call_agent` tool so headless execution cannot stall. Eligible interactive parents use their CLI's normal MCP approval flow, preserving consent before a potentially costly call. Called children receive no server registration or permission.

Alternatives considered:

- Global registration risks leaking Runner integration outside the run.
- A skill-mediated shell command has weaker typing, quoting, and permission isolation.
- A new `runner_tools` workflow field makes capability intent explicit but duplicates a directive already present in the authored prompt and expands the workflow schema.
- Pre-approving the tool interactively removes useful cost consent; requiring approval headlessly can stall the workflow.
- A fixed Runner timeout would add execution policy unrelated to ordinary agent-step behavior; model-selected timeouts could interrupt a child after it has mutated the worktree.

### 6. Keep call resolution and results narrow

The MCP schema requires `prompt` and exactly one of `agent` or `session`. A fresh `agent` target accepts optional `cli`, `model`, and `workdir`; a named `session` target accepts optional `model` and `workdir` and always uses the CLI resolved from its declared profile. MCP performs structural validation; the Runner remains authoritative for profiles, named-session declarations, overrides, workdirs, and active-session safety.

A fresh profile target uses the shared invocation core without modifying the named-session map. A named target uses the existing declaration and run-scoped map, persisting a newly discovered session ID before returning success. Before launch, the handler compares a named target's resolved CLI and session identity with the active parent and rejects self-resume. Called children always use autonomous-headless mode.

A successful MCP result has this conceptual shape:

```json
{
  "target": { "kind": "agent", "name": "implementor" },
  "response": "child's filtered final response"
}
```

Failures use MCP tool-error semantics with a stable error code, message, and requested target where available. The tool description states that calls are synchronous and serial, called children receive the profile system prompt and call prompt but no workflow-engine step enrichment, and a second call must wait for the active call. `call_in_progress` repeats that instruction in-band for models that attempt parallel tool use. Results omit raw session IDs, usage, and cost. The parent remains active and decides whether to retry.

Alternatives considered:

- Returning all execution metadata leaks internal identifiers and duplicates evidence already owned by the Runner.
- Automatic retry obscures cost and can repeat mutations.
- A new call-specific session store would conflict with existing named-session behavior.
- Allowing `cli` on a named-session call could attempt to resume a CLI-specific session ID through a different adapter; selecting another CLI requires another profile or named-session declaration.

### 7. Represent calls as nested execution evidence

Each accepted call receives a unique `call_id`; its structural prefix appends `call:<call-id>` to the parent path. Audit events carry both the call ID and active parent-attempt identity:

```text
step_start:       review-assumptions
agent_call_start: review-assumptions/call:<id>
agent_call_end:   review-assumptions/call:<id>
step_end:         review-assumptions
```

The call's filtered output uses ordinary headless-agent persistence, privacy, and size behavior under that prefix. Full response text is returned to the parent but not duplicated into audit entries.

`run-metrics.json` remains schema v1. Its existing `steps[]` execution-record union gains records with `kind: "agent-call"` and additive `call_id`, `parent_attempt_id`, `target_kind`, and `target_name` fields. The remaining outcome, duration, session, usage, and cost fields reuse ordinary agent-record semantics.

Parent records retain only parent usage and cost. Agent-call records contribute to aggregate usage, cost, and coverage exactly once, but their overlapping duration does not increase run active duration. Only completed calls are durable metric records. Rejected requests produce control-rejection audit evidence but no call event pair or metric record.

Named-session state is flushed through `state.json`; call results are reconstructed from audit, metrics, and output artifacts rather than added to workflow sequencing state.

Alternatives considered:

- Folding child metrics into the parent destroys attribution and risks double counting.
- A second `agent_calls[]` collection duplicates the record model.
- A schema-v2 `executions[]` migration is unnecessary because `steps[]` already contains non-step iteration records.

### 8. Add dynamic call nodes to run views

The audit projection adds an agent-call node type beneath its parent agent attempt. Calls are dynamic execution children, not workflow-definition steps.

Rows use explicit target labels and `↗` as the agent-call type glyph:

```text
● review-assumptions                    (2 calls)
  ✓ ↗ call session: implementor-session
  ● ↗ call agent: implementor
```

For autonomous parents, call rows appear and stream output live. Auto-follow enters the active call and returns to the next active execution afterward. For interactive parents, the CLI retains terminal ownership; accumulated call evidence appears when the run TUI resumes.

Completed summaries treat a parent with calls as a container:

```text
review-assumptions
├─ parent turn
├─ call session: implementor-session
└─ call agent: implementor
```

Usage and cost roll up exactly once. Duration remains the parent attempt's wall-clock duration rather than parent plus child durations. In inactive runs, a completed call with a known CLI session ID can use the existing direct-resume action.

Alternatives considered:

- Flattening calls beside workflow steps misrepresents workflow structure.
- Showing calls only in logs makes live delegation and failed child attempts difficult to understand.
- Adding child duration to the parent total double-counts synchronous waiting time.

## Risks / Trade-offs

- **[Cross-adapter MCP behavior differs or changes]** → Keep one canonical tool schema and transport contract, isolate registration in adapters, use table-driven adapter tests, exercise named-session delegation in the smoke-test workflow, and add a real-agent end-to-end tool-path test.
- **[A host's generic MCP timeout terminates real implementation work]** → Apply supported process-local timeout controls, emit progress when requested, test the generated adapter configuration, and cover a long-running bridge request without treating progress as a timeout guarantee.
- **[A host abandons a request while its child keeps running]** → Lease the call to its MCP/control connection so cancellation, bridge exit, or disconnect terminates the child, caches the canceled result, and releases the call slot.
- **[Shared invocation refactoring regresses ordinary agent steps]** → Extract behavior incrementally under existing executor tests before enabling calls; keep workflow-step and call-specific side effects in thin wrappers.
- **[Parent and child output becomes mixed or misattributed]** → Use immutable invocation scopes, independent capture pipelines, and call-specific structural prefixes.
- **[A canceled or crashed run leaves a child agent running]** → Derive child context from the parent attempt, supervise a separate process group, and reuse the process-identity watchdog.
- **[Stale or inherited control credentials invoke a later attempt]** → Strip all control variables before every spawn, issue a fresh attempt token, validate run, step, attempt, and token together, and give called children no control integration.
- **[A model resumes its own active CLI session and deadlocks or corrupts it]** → Resolve named-session identity before launch and reject a target matching the parent's active CLI and session ID.
- **[A named-session CLI override makes its stored session ID unusable]** → Disallow `cli` for named-session calls and always resolve the adapter from the declared profile.
- **[Duplicate delivery repeats an expensive or mutating call]** → Keep an attempt-scoped request registry whose first request owns a shared result; duplicates reuse it and distinct concurrent requests receive an instructive rejection.
- **[Delegation amplifies token usage or cost]** → Provision only when the authored step prompt contains `call_agent`, preserve interactive approval, limit each attempt to one in-flight call, expose every call and its metrics in run views, and retain call budgets as a future feature.
- **[Other parent tools or external processes mutate the shared worktree concurrently]** → Guarantee only serialization among agent calls from one attempt; do not claim exclusive one-writer isolation for the worktree.
- **[Large child responses are truncated by a host tool UI]** → Persist ordinary headless output separately under the call identity so inspection retains the same privacy and size behavior as agent-step output, even when the parent sees a host-limited result.
- **[Older metrics consumers encounter the new record kind]** → Keep schema v1 fields additive, document `steps[]` as an execution-record union, retain authoritative top-level totals, and test resume and rehydration with mixed step, iteration, and agent-call records.
- **[Interactive calls are invisible while the child runs]** → Preserve terminal ownership rather than interrupting the parent CLI; surface all accumulated call evidence immediately when the TUI resumes.

## Migration Plan

1. Extract the shared invocation core and invocation-scoped process API without changing ordinary step behavior.
2. Move the generic authenticated control infrastructure into `internal/control`, preserving completion and turn-commit behavior.
3. Add the official MCP SDK, internal stdio server, canonical `call_agent` schema, prompt-gated process-local adapter registration, long-running host controls, and progress reporting.
4. Add the attempt-scoped handler, safe named-session resolution, request leasing, deduplication, cancellation, instructive concurrency errors, and result mapping.
5. Add call audit events, output identity, schema-v1 metric records, and run-view projection and rendering.
6. Update the project-local smoke-test workflow to exercise a declared named session.
7. Add focused unit and integration coverage—including timeout configuration, progress, disconnect cancellation, and prompt gating—plus a real-agent end-to-end call path, then update user and run-artifact documentation. Documentation and the MCP tool description state that calls omit workflow-engine step enrichment and are synchronous and serial.

No workflow, profile, global CLI configuration, or persisted-state migration is required. Existing runs remain readable and have no call nodes. New named-session entries use the existing state schema.

Rollback removes tool provisioning and prevents new calls. Existing audit, output, and metric evidence remains on disk; no destructive rollback is needed. Runs containing agent-call evidence should be inspected with a version that understands the new event and record kinds.

## Open Questions

None for the initial synchronous, single-level design.
