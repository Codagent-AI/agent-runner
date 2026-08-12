# Task: Extract Invocation and Control Core

## Goal

Create the behavior-preserving execution and control-plane foundation required for a parent agent process and a called child agent process to overlap safely. The resulting contracts must be independently testable and reusable by ordinary workflow steps and runner-owned agent calls without enabling `call_agent` yet.

## Background

You MUST read these approved artifacts before starting:

- `proposal.md`, especially **Technical Approach** and **Impact**.
- `design.md`, especially **Decision 2: Generalize the existing control channel**, **Decision 3: Extract a shared agent-invocation core**, **Decision 4: Make process execution invocation-scoped**, and migration steps 1–2.

Agent Runner must remain the owner of CLI process execution, session resolution, output handling, and durable evidence. This refactor is the stable foundation for later agent-call behavior; it must not introduce a second runner or move execution into an MCP process.

Current implementation seams:

- `internal/exec/agent.go` combines workflow-step concerns with reusable agent invocation concerns in `ExecuteAgentStep`, including profile and adapter resolution, session discovery, process execution, output filtering, usage extraction, and step-specific audit/capture bookkeeping.
- `internal/exec/interfaces.go` exposes `ProcessRunner.RunAgent` without an invocation context, while `internal/exec/agent.go` and `internal/liverun/process_runner.go` mutate the active prefix and output wrappers through setter interfaces. That mutable current-step state cannot safely represent overlapping parent and child lifetimes.
- `internal/interactive/control.go` owns the run-scoped Unix socket, attempt credential, request validation, completion deduplication, and turn-commit delivery, even though autonomous parents will also need the endpoint.
- `internal/model/context.go` stores the server opaquely as `InteractiveControl`; model types must remain independent of executor and control packages.
- `internal/interactive/runner.go` and `internal/interactive/process.go` own direct-terminal execution and process-group supervision. Interactive completion and durability behavior must remain a consumer of the generalized control infrastructure.
- Existing behavior is covered primarily by `internal/exec/agent_test.go`, `internal/interactive/control_test.go`, `internal/interactive/runner_test.go`, `internal/liverun/liverun_test.go`, `internal/session/session_test.go`, and CLI adapter tests.

Implement these approved design constraints:

- Extract a shared invocation core that accepts resolved invocation inputs and returns a rich result. Keep workflow-step lifecycle behavior—step events, capture variables, workflow-engine enrichment, ordinary session bookkeeping, and step outcomes—in a thin workflow-step wrapper.
- Make agent process execution invocation-scoped. Immutable options must carry context/cancellation, environment additions and removals, workdir, structural prefix, output routing/wrappers, and supervision settings. Do not serialize overlapping invocations by locking mutable runner state.
- Move the generic authenticated endpoint, active-attempt registry, request framing/validation, and idempotency primitives into a mode-neutral `internal/control/` package. Keep terminal ownership, durability probing, and interactive completion orchestration in `internal/interactive/`.
- Preserve the private socket lifecycle: lazy creation after the run lock exists, a pointer in the run directory, user-private permissions, safe stale cleanup proven by the run lock, and close/unlink on normal exit.
- Preserve existing completion acknowledgement, committed-turn evidence, credential rotation, environment names, audit events, direct-terminal supervision, and resume behavior.
- Keep model package dependencies acyclic. An opaque control reference in `model.ExecutionContext` is acceptable; concrete control types belong above `internal/model/`.
- Use local fakes and `google/go-cmp`; do not add a mocking framework.

This task is intentionally behavior-preserving and therefore has no copied delta requirement section. It establishes independently verifiable structure required by the approved design.

## Done When

- Ordinary interactive, autonomous-headless, and autonomous-interactive workflow agent steps retain their existing CLI args, prompts, system-prompt enrichment, permissions, session creation/resume/discovery, capture behavior, output filtering, audit events, metrics inputs, failure outcomes, and terminal behavior.
- A shared agent-invocation API returns enough typed data for distinct wrappers to consume the filtered response, raw stdout/stderr, exit status, resolved CLI/model/session information, usage/cost extraction, invocation timing, and whether the CLI launched.
- Agent process execution no longer depends on mutable global/current-step prefix, workdir, environment, output-wrapper, or cancellation fields. Two fake invocations can overlap without cross-contaminating output, environment, prefix, or cancellation.
- The generalized `internal/control/` server accepts the existing completion and turn-commit clients with fresh attempt credentials and unchanged idempotency and rejection behavior.
- `internal/interactive/` uses the generalized endpoint without regressing acknowledgement-before-termination, semantic durability, watchdog cleanup, or direct terminal handoff.
- Focused regression tests cover the extracted invocation core, concurrent invocation isolation, generalized control request validation/deduplication, socket lifecycle, and existing interactive completion flows.
- `make fmt` succeeds, and targeted tests for `internal/exec`, `internal/control`, `internal/interactive`, `internal/liverun`, `internal/session`, and `internal/cli` pass.
