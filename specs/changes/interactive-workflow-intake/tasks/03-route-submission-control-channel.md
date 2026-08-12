# Task: Route submission over the control channel, adapter grants, and the embedded intake workflow

## Goal

Give the intake agent an authenticated, runner-validated way to name the workflow it and the user agreed
on, with errors returned inline while the conversation is still live. That means: a fourth control
message type with its own acceptance state, runner-established eligibility, freeze-on-completion
ordering, the fixed `agent-runner step submit-route` client, pre-approval of that command across the
adapter registry, and the embedded `core:intake` workflow whose identity eligibility is defined against.

## Background

### Why the command is argument-free and file-backed

The agent writes its choice to a JSON file at a runner-provided path and then runs a command whose text
**never varies**:

```
env:  AGENT_RUNNER_ROUTE_REQUEST=<run-dir>/route-request.json

      { "workflow": "spec-driven:change",
        "params":   { "change_name": "interactive-workflow-intake" },
        "handoff":  "<run-dir>/intake-handoff.md" }

run:  agent-runner step submit-route
```

The alternative — passing the workflow and parameters as command arguments — conflicts with a deliberate
constraint in the CLI adapters. `cli.CompletionCommand.Valid()` (`internal/cli/adapter.go:160`) requires
the pre-approved argv to be exactly `["step", "complete"]`, and the type comment states that separating
executable from argv *"prevents adapters from accepting shell fragments supplied as a single opaque
string."* Claude turns that descriptor into an exact-string `Bash(...)` grant. A command carrying
model-authored values would either lose pre-approval and prompt on every attempt, or require a wildcard
grant materially broader than what the codebase currently allows itself. Moving the variable data into a
validated file keeps the permission grant exact and makes shell quoting irrelevant.

### The runner reads the request; the client never transmits it

`agent-runner step submit-route` sends a **payload-free** `submit_route` control message. The runner
then reads the request from the path it created and advertised. This is deliberate: the path cannot be
redirected by the agent, size bounds are enforced at read time, and the control message stays
payload-free so no new framing concerns arise.

### The handoff path is runner-owned

The agent is told where to write rather than choosing. Besides removing a class of validation, this is
what makes exploration-only reportable: when no route request is ever submitted, the runner still knows
where the handoff would be. The route request still carries a `handoff` field, which validation requires
to resolve to that same runner-owned path.

When a step is route-eligible, the child's environment gains:

```
AGENT_RUNNER_ROUTE_REQUEST=<run-dir>/route-request.json
AGENT_RUNNER_INTAKE_HANDOFF=<run-dir>/intake-handoff.md
```

### Eligibility requires runner-established intake identity

The intake step opts in through the existing tools mechanism:

```yaml
- id: plan
  session: intake-planner
  mode: interactive
  tools: [submit_route]
```

**The tool declaration is necessary but not sufficient.** `tools:` is a public workflow field validated
generically (see `model.RunnerTool` and `Step.validateTools` at `internal/model/step.go:37` and `:438`,
which today accept only `call_agent`), so any project or user workflow could write
`tools: [submit_route]`. If that alone conferred eligibility, and the post-run check launches any frozen
route, this change would have quietly shipped general dynamic workflow chaining from arbitrary agents —
an explicit non-goal. Eligibility therefore requires intake identity as well:

```go
routeEligible := step.HasTool(model.RunnerToolSubmitRoute) &&
    ctx.IsTopLevelWorkflow() &&                 // not reached as a sub-workflow
    isBuiltinIntakeWorkflow(ctx.WorkflowFile)   // the embedded core:intake, by reference
```

Static validation rejects the declaration outright on any step that cannot satisfy those conditions, so
a user workflow fails at **load** rather than silently never working. `internal/prevalidate` reports the
same violation statically.

`ExecuteAgentStep` in `internal/exec/agent.go` passes the eligibility result into
`control.AttemptOptions` alongside a route handler — the same shape `model.RunnerToolCallAgent` uses
today (see `internal/exec/agent_call.go` and `internal/control/control.go:148`).

### Freeze and completion ordering

`internal/control/control.go` today defines three message types at lines 35-37 (`complete_step`,
`turn_committed`, `agent_call`), handled in per-connection goroutines under a server mutex.
`handleCompletion` is at `internal/control/control.go:461`, `acknowledgeCompletion` at `:497`.

`handleCompletion` gains one step, under the mutex it already holds, before it marks the completion
accepted:

```
handleCompletion:
  if a route is staged:
      Freeze()
      if Freeze fails -> reject the completion request; do not accept
  mark completion accepted
  capture durability checkpoint      (outside the mutex, as today)
  acknowledge
```

Two consequences are load-bearing:

- **Completion with no staged route succeeds normally.** That is the exploration-only path.
- **A failed freeze rejects completion.** The runner must never acknowledge completion while the route
  is in an indeterminate state. The agent receives the error and can retry `step complete`.

**The linearization point must be explicit, because "freeze is under the mutex" is not sufficient on its
own.** Control requests are handled in per-connection goroutines, and the existing completion path
deliberately releases the mutex to capture the durability checkpoint. If route submission validated
*and staged* outside the mutex, a validated replacement could be written after a freeze had already run.
So:

- Validation — decoding, catalog resolution, parameter checks, handoff reading and copying — happens
  **outside** the mutex, since it does filesystem work and must not block the server.
- The **final eligibility recheck and the staging write** happen **inside** the same mutex that guards
  completion acceptance.

That yields exactly two possible outcomes for any interleaving: the submission stages and is later
frozen, or it observes an accepted completion and is rejected. A submission can never be staged after a
freeze.

**This is why `internal/intakeroute` exposes its two staging phases separately, and you must use them
that way.** The validate/prepare phase does all decoding, catalog resolution, parameter checking,
handoff opening, and handoff copying — the copy landing at a temporary path — and returns a prepared
route. The publication phase takes that prepared route and does nothing but the atomic staged-sidecar
write. Run the first outside the mutex and the second inside it. Do not hold the mutex across the
filesystem work, and do not let the prepare phase publish anything.

**Discard the prepared route whenever you do not publish it.** If the final eligibility recheck inside
the mutex fails — most importantly because a completion was accepted while validation was running — the
prepared route must be discarded through the store's discard path, so the temporary snapshot is removed
and any previously staged route is left byte-identical. A submission that loses the race leaves nothing
behind.

This is the highest-regression-risk edit in the whole change. `handleCompletion` is load-bearing for
every interactive step in every workflow. The freeze is additive and no-ops when no route is staged, but
the new rejection branch is a genuinely new failure mode for a path that previously always accepted.

### Adapter command grants

`CompletionCommand` becomes a general descriptor whose argv is derived from its kind, so kind and argv
can never disagree:

```go
type RunnerCommandKind int

const (
    RunnerCommandCompleteStep RunnerCommandKind = iota
    RunnerCommandSubmitRoute
)

type RunnerCommand struct {
    Kind       RunnerCommandKind
    Executable string
}

func (c RunnerCommand) Args() []string {
    switch c.Kind {
    case RunnerCommandCompleteStep: return []string{"step", "complete"}
    case RunnerCommandSubmitRoute:  return []string{"step", "submit-route"}
    default:                        return nil
    }
}

func (c RunnerCommand) Valid() bool {
    return filepath.IsAbs(c.Executable) && c.Args() != nil
}
```

`cli.BuildArgsInput` (`internal/cli/adapter.go:111`) carries `RunnerCommands []RunnerCommand` in place of
the single `CompletionCommand *CompletionCommand` field. Adapters iterate and grant each exactly, rather
than special-casing one field. `RunnerCommandCompleteStep` is supplied wherever `CompletionCommand` is
supplied today; `RunnerCommandSubmitRoute` is supplied **only** for a route-eligible step. Adding a
third runner command later touches no adapter.

The `turn_committed` hook command (`CompletionCommand.hookCommand`, `internal/cli/adapter.go:177`) stays
attached to the completion grant, since it is completion-specific. `ShellCommand` rendering and
`shellQuote` behavior carry over unchanged.

`cli.KnownCLIs()` holds five adapters — claude, codex, copilot, cursor, and opencode
(`internal/cli/claude.go`, `codex.go`, `copilot.go`, `cursor.go`, `opencode.go`). The rename is
mechanical but broad. A parallel `RouteCommand` field was rejected because each adapter would grow a
second near-identical branch and a third runner command would repeat the duplication. **OpenCode needs
no route grant**, since it declares `InteractiveModeError` and cannot run interactive steps at all.

### The embedded intake workflow

Eligibility is defined against the built-in intake workflow, so it must exist. Add
`workflows/core/intake-v1.0.yaml`, embedded like every other file under `workflows/` (see
`workflows/core/` for the existing set and `_group.yaml` for namespace metadata; the package is imported
as `builtinworkflows "github.com/codagent/agent-runner/workflows"`).

It is a single interactive agent step with `tools: [submit_route]`, reusing the existing `lead` profile
so custom profile sets keep working untouched. It declares `hidden: true` — the New tab surfaces intake
through a dedicated entry, and listing it a second time as an ordinary workflow row would duplicate the
same action. Existing hidden-workflow behavior already keeps it runnable from the CLI and shows it under
the browser's `h` toggle; you should not need to change `internal/listview` for that.

The step prompt frames the agent's job: explore the problem with the user by inspecting the repository,
researching, brainstorming, clarifying scope and non-goals, and recommending whether implementation is
warranted and under which workflow. The agent recommends; the human decides. It writes its handoff to
the runner-supplied path and submits a route only after the user agrees. Workflow YAML is configuration
— focused unit tests are not expected for the YAML itself, but the behavior it enables is tested.

### Audit events

`internal/audit/types.go` defines `EventType` constants (`run_start` … `child_continued` at lines 8-26).
Add the in-run route lifecycle types this task emits: `route_submitted`, `route_accepted`,
`route_rejected`, `route_frozen`. Emission goes through the existing `ControlServer.emit`
(`internal/control/control.go:642`) and the real `audit.EventLogger` interface — do not replace it with
an empty interface. `route_accepted` must carry the selected workflow, the exact resolved workflow source
reference, the supplied parameters, and the sealed handoff location. `route_rejected` must carry a
reason. No route event may record a launched-run ID.

(Two further event types, `route_launch_attempted` and `route_launch_failed`, belong to the
post-finalization launch path and are not emitted by this task. Adding all six constants at once is
fine and mechanical; emitting the launch pair is not your responsibility.)

### What already exists that you depend on

`{{intake_handoff}}` is already a built-in defined on every run, reserved against parameters and
captures, and recognized by pre-validation. The `internal/intakeroute` package already exists with
`Request`, `Sealed`, `State`, `Store` (`Load`/`Freeze` plus the two-phase staging operations), and a
transport-independent validate/prepare phase that performs strict decoding, catalog resolution,
self-routing rejection, parameter checks, and handoff containment/regular-file/non-empty/size checks,
and that snapshots the handoff bytes from the same validated handle to a temporary path. Publication of
a prepared route and discarding an unpublished one are separate operations. Consume those; do not
reimplement validation or re-resolve the workflow name.

### Scope boundary

Entry points (`-i`, `--cli`, `--model`, the TTY gate, the New tab entry) and the cross-process launch
are delivered elsewhere in this change. This task ends when a route can be submitted, validated, staged,
replaced, and frozen — not when anything launches. Do not add a launch path or emit launch events.

## Spec

From `specs/intake-route-submission/spec.md`:

### Requirement: Route request location and submission client

Agent Runner SHALL provide the intake agent with the path of a run-owned route request file through the attempt environment. The binary SHALL expose `agent-runner step submit-route`, a fixed command that accepts no arguments and reads endpoint and credential data only from the inherited control environment. The command SHALL transmit no route request content: Agent Runner SHALL read the request from the path it supplied, so the submitting client cannot select or influence which file is read. The command SHALL NOT accept workflow, parameter, handoff, run, step, socket, or token values on the command line. It SHALL exit nonzero with guidance when the control environment is absent.

#### Scenario: Agent submits a route
- **WHEN** the intake agent writes a valid route request to the runner-provided path and runs the exact absolute-path client inside its session
- **THEN** the active attempt receives an authenticated route submission and the client reports success

#### Scenario: Client cannot select another request file
- **WHEN** a route submission is received
- **THEN** Agent Runner reads the request only from the path it supplied for that attempt
- **AND** no value originating from the submitting client determines which file is read

#### Scenario: Arguments are rejected
- **WHEN** the client is invoked with any argument
- **THEN** it exits nonzero without contacting any run

#### Scenario: Command runs outside a session
- **WHEN** the control environment is absent
- **THEN** the client exits nonzero and does not target any run

### Requirement: Route eligibility

A route submission SHALL be accepted only for a step attempt that Agent Runner has marked route-eligible. Eligibility SHALL require runner-established intake identity and SHALL NOT be conferred by a workflow declaration alone: the attempt must belong to the built-in intake workflow running as the top-level workflow, at its designated step, un-nested. A workflow that merely declares the route-submission tool SHALL NOT thereby gain the ability to launch another workflow, since that would amount to general dynamic workflow chaining from arbitrary agents, which this change excludes.

Submissions from any other step SHALL be rejected with an actionable error and audited, without changing workflow state. Declaring the route-submission tool on a step that cannot be route-eligible SHALL be reported statically rather than only at runtime.

#### Scenario: Eligible intake step accepts submission
- **WHEN** a well-formed route submission carries the active credential of the built-in intake workflow's designated step, running as the top-level workflow
- **THEN** the runner validates it

#### Scenario: Ineligible step is rejected
- **WHEN** an interactive agent step that is not route-eligible submits a route
- **THEN** the runner rejects it with an error stating route submission is unavailable for the step, records the rejection, and leaves workflow state unchanged

#### Scenario: A user workflow cannot grant itself route submission
- **WHEN** a project or user workflow declares the route-submission tool on one of its agent steps
- **THEN** that declaration is reported as invalid
- **AND** no route submission from that step is ever accepted

#### Scenario: Nested intake is not eligible
- **WHEN** the intake workflow is reached as a sub-workflow of another workflow rather than as the top-level workflow
- **THEN** its step is not route-eligible and any submission is rejected

### Requirement: Replacement and retry idempotency

A repeated submission carrying an already-accepted request ID SHALL return its original acknowledgement and SHALL NOT create a second staged route. While the step attempt remains active, a later valid submission SHALL replace the previously staged route. A later invalid submission SHALL leave the previously staged route intact.

#### Scenario: Retry of an accepted submission is idempotent
- **WHEN** the same accepted request ID is submitted again after a lost response
- **THEN** the runner returns the original successful acknowledgement
- **AND** exactly one route remains staged

#### Scenario: Later valid submission replaces the earlier one
- **WHEN** the agent submits a second, different, valid route while the step is still active
- **THEN** the staged route becomes the second one
- **AND** the launch uses the second route

#### Scenario: Later invalid submission preserves the staged route
- **WHEN** the agent submits an invalid route after a valid one was accepted
- **THEN** the client receives the validation error
- **AND** the previously staged route remains staged unchanged

### Requirement: Freeze on completion

Acceptance of the step's completion request SHALL freeze the staged route before the completion is acknowledged. Any route submission arriving after a completion has been accepted SHALL be rejected and audited. Once frozen, a route SHALL be immutable for the remainder of the run, including across resume.

Route staging and completion acceptance SHALL share a single ordering point, so that every interleaving produces one of exactly two outcomes: the submission is staged and then frozen, or the submission observes an accepted completion and is rejected. No interleaving may result in a submission being staged after the route was frozen, or in a completion being acknowledged while a validated submission is still in flight. Validation work MAY occur outside that ordering point, but the final eligibility check and the staging write SHALL occur within it.

If freezing fails, the completion request SHALL be rejected rather than acknowledged, so the run never acknowledges completion with a route in an indeterminate state.

#### Scenario: Submission after accepted completion is rejected
- **WHEN** a route submission arrives after the step's completion has been accepted
- **THEN** the runner rejects it with an error stating the route is already frozen, and records the rejection

#### Scenario: Concurrent submission and completion resolve to one of two outcomes
- **WHEN** a route submission and a completion request are processed concurrently
- **THEN** either the submission is staged and subsequently frozen, or it is rejected because completion was already accepted
- **AND** no submission is ever staged after the route was frozen

#### Scenario: Failed freeze rejects the completion
- **WHEN** freezing a staged route fails during completion acceptance
- **THEN** the completion request is rejected rather than acknowledged
- **AND** the client may retry completion

#### Scenario: The frozen route is what launches
- **WHEN** an intake run finishes successfully after its route was frozen
- **THEN** the launched workflow is the one recorded in the frozen route

#### Scenario: A frozen route cannot be replaced on resume
- **WHEN** a resumed intake attempt submits a route after an earlier attempt froze one
- **THEN** the submission is rejected and the frozen route is unchanged

> "The frozen route is what launches" is stated here as the guarantee your ordering rules must make
> true. Performing the launch itself is not part of this task; your portion is that exactly one
> immutable frozen record exists for the run and that nothing can replace it afterwards.

From `specs/step-control-channel/spec.md`:

### Requirement: Route submission message type

The control channel SHALL accept a route submission message type alongside completion, committed-turn, and agent-call messages. It SHALL apply the same attempt-scoped authentication as completion: only the active run, step, attempt, and credential are accepted, and malformed, stale, unknown, or ineligible messages SHALL be rejected and audited without advancing the workflow. Route submission SHALL maintain its own acceptance state, distinct from completion acceptance.

#### Scenario: Active credential is accepted
- **WHEN** a well-formed route submission carries the active attempt's credential and the step is route-eligible
- **THEN** the server admits it for validation

#### Scenario: Stale credential is rejected
- **WHEN** a route submission carries an earlier attempt's credential
- **THEN** the server rejects and audits it without changing workflow state

#### Scenario: Route acceptance is independent of completion acceptance
- **WHEN** a route submission is accepted for an attempt whose completion has not been requested
- **THEN** the step does not advance, and the attempt remains active

### Requirement: Acknowledgement precedes termination

Completion acceptance SHALL be an intermediate state, not success. The server SHALL capture the adapter's accept-time durability checkpoint, freeze any staged route so no later route submission can change what will launch, record `completion_requested` and `completion_acknowledged`, and return the acknowledgement before sending any termination signal. Early committed-turn hook events that arrive before acceptance SHALL be acknowledged and ignored rather than failing the agent turn.

#### Scenario: Tool call returns before shutdown
- **WHEN** a valid completion request is accepted
- **THEN** its client receives a success acknowledgement before CLI termination begins

#### Scenario: Hook fires before completion acceptance
- **WHEN** a native turn hook sends `turn_committed` with no accepted completion
- **THEN** the server acknowledges and ignores it without failing or advancing the step

#### Scenario: Route freezes before the completion is acknowledged
- **WHEN** a completion request is accepted for an attempt with a staged route
- **THEN** the route is frozen before the completion acknowledgement is returned
- **AND** a route submission arriving afterwards is rejected

### Requirement: Completion integration preserves supervision

Adapters MAY pre-approve only the exact absolute-path runner commands with fixed arguments: the completion command with fixed `step complete` arguments, and the route submission command with fixed `step submit-route` arguments. No runner command carrying caller-supplied or model-authored argument values SHALL be pre-approved. A CLI that cannot express this safely SHALL keep its normal interactive approval prompt rather than broaden permissions. Process-local native commands and hooks SHALL be injected for the spawned process without requiring global user installation or project-file changes. Failure to prepare a required integration SHALL fail before spawn.

#### Scenario: Unrelated commands remain supervised
- **WHEN** an interactive agent runs a command other than the exact pre-approved runner commands
- **THEN** the CLI's normal approval behavior is unchanged

#### Scenario: Native command is process-local
- **WHEN** Agent Runner adds an adapter-native completion command or hook
- **THEN** it is available to the spawned CLI without global installation or project mutation

#### Scenario: Route submission command is pre-approved only in its exact form
- **WHEN** an adapter pre-approves the route submission command for a route-eligible step
- **THEN** only the exact absolute-path command with fixed `step submit-route` arguments is granted
- **AND** a variant carrying additional arguments is not covered by the grant

From `specs/builtin-workflows/spec.md`:

### Requirement: Intake workflow embedded under core

The builtin set SHALL include an intake workflow in the `core` namespace, resolvable as `core:intake`. It SHALL declare `hidden: true`, because the new tab already surfaces it through a dedicated "Plan with an agent" entry and listing it a second time as an ordinary workflow row would duplicate the same action. Per existing hidden-workflow behavior, it SHALL remain runnable from the CLI and SHALL appear in the browser when the show-hidden toggle is on.

#### Scenario: Intake workflow runnable by namespace
- **WHEN** the user runs `agent-runner core:intake` in an interactive terminal
- **THEN** the intake workflow loads from the embedded `core` namespace and executes

#### Scenario: Intake workflow omitted from the browser by default
- **WHEN** the new tab renders with the show-hidden toggle in its default "off" state
- **THEN** no ordinary workflow row for `core:intake` appears in the Core group
- **AND** the dedicated "Plan with an agent" entry is still present

#### Scenario: Intake workflow visible under the hidden toggle
- **WHEN** the user presses `h` to show hidden workflows
- **THEN** a row for `core:intake` appears in the Core group alongside the other core workflows

> The "Plan with an agent" entry referenced in the second scenario is delivered elsewhere in this
> change. Your portion is the embedded, hidden, `core:intake`-resolvable workflow.

From `specs/workflow-intake/spec.md`:

### Requirement: Intake conversation responsibilities

The intake workflow SHALL present the user with an interactive agent session that is able to inspect the repository, research the problem, and recommend a course of action, and that is enabled to submit a route. The agent SHALL recommend; the human SHALL decide, and the agent SHALL NOT commit the user to an implementation path without the user's agreement in the conversation. Agent Runner SHALL supply a run-owned path at which the agent writes the handoff, so that its location is known to the runner whether or not a route is ever submitted.

#### Scenario: Intake presents a capable conversational agent
- **WHEN** an intake run reaches its agent step
- **THEN** the user is placed in an interactive agent session that can inspect the repository and is enabled to submit a route

#### Scenario: Agent recommends and the user decides
- **WHEN** the intake agent identifies a suitable workflow
- **THEN** it presents the recommendation to the user and submits a route only after the user agrees in the conversation

#### Scenario: Handoff location is runner-owned
- **WHEN** the intake agent writes a handoff
- **THEN** it writes to the run-owned path Agent Runner supplied
- **AND** Agent Runner can report that path even when no route was ever submitted

From `specs/audit-log-entries/spec.md` — the portion of **Requirement: Event types** and
**Requirement: Route event data** this task delivers. The full modified event list is:

> The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `error`, `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, `child_continued`, `route_submitted`, `route_accepted`, `route_rejected`, `route_frozen`, `route_launch_attempted`, and `route_launch_failed`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Route events are intermediate
- **WHEN** the audit logger receives route lifecycle events during the intake agent step
- **THEN** it writes them as intermediate events distinct from the step's final `step_end`

And from **Requirement: Route event data**, the in-run portion:

#### Scenario: Acceptance records the sealed route
- **WHEN** a route submission is accepted
- **THEN** the `route_accepted` entry records the selected workflow, the exact resolved workflow source reference, the supplied parameters, and the sealed handoff location

#### Scenario: Rejection records the reason
- **WHEN** a route submission is rejected
- **THEN** the `route_rejected` entry records the reason for the rejection

> The `route_launch_attempted` and `route_launch_failed` types and their post-finalization append path
> are delivered elsewhere in this change. Your portion is the four in-run route event types, emitted
> with the data above, through the run's own logger.

## Test Plan

You MUST read `test-plan.md` for the full text of the obligations below.

**INT-001: Authenticated route submission stages a sealed route.** Boundary: real
`control.ControlServer` over a real Unix socket, `internal/intakeroute`, real filesystem. Setup: a
control server with a route-eligible active attempt; a run directory containing a valid
`route-request.json` and a non-empty handoff; a workflow catalog resolving the named workflow. Action:
send a payload-free `submit_route` message carrying the active attempt's credential. Assert: the
response is a success acknowledgement; `<run-dir>/intake-route.json` exists with state `staged`;
`Sealed.SourceRef` is the exact resolved definition reference rather than the canonical name;
`Sealed.HandoffPath` addresses a snapshot whose bytes match the source at submission time; mutating the
agent-written handoff afterwards does not change the snapshot's contents; a submission from a
non-route-eligible attempt is rejected and audited with workflow state unchanged. Execution:
`internal/control` and `internal/intakeroute` package tests, `go test ./...`.

**INT-002: Freeze ordering and completion interaction.** Boundary: real control server with concurrent
per-connection goroutines, `internal/intakeroute`, real filesystem. Setup: an active route-eligible
attempt, exercised **with and without** a staged route; a store variant whose `Freeze` fails. Action:
send `complete_step`; separately send `submit_route` after completion acceptance. Assert: with a staged
route, the sidecar reads `frozen` **before** the completion acknowledgement is written; a `submit_route`
arriving after acceptance is rejected and audited; **with no staged route the completion is accepted
normally**, proving ordinary workflows are unaffected; when `Freeze` fails the completion request is
rejected rather than acknowledged, and the client may retry. Execution: `internal/control` package
tests, `go test ./...`.

**INT-007: Replacement, retry, and staging idempotency at the control boundary.** Boundary: real control
server over a real socket, `internal/intakeroute`, real filesystem. Setup: an active route-eligible
attempt with a valid request. Action: submit; resubmit the same request ID after discarding the
response; submit a second, different valid request; submit an invalid request afterwards; drive a
submission and a completion concurrently **under a barrier in both orderings**. Assert: the retry
returns the original acknowledgement and exactly one route remains staged; the second valid submission
replaces the first and is what freezes; the invalid submission leaves the previously staged route
unchanged; each concurrent ordering resolves to either staged-then-frozen or
rejected-because-accepted, and never to a submission staged after a freeze. Execution:
`internal/control` package tests, `go test ./...`.

**INT-005: Handoff copy and provenance round-trip** — the staged-route resume portion. Boundary:
`internal/runner` run preparation and resume, `internal/stateio`, `internal/intakeroute`, real
filesystem. Setup: an intake parent run with a staged sidecar. Action: interrupt the run after a route
is staged but before its step completes, then `PrepareResume`. Assert: the resumed intake parent
restores its staged route, and the resumed attempt can **replace** it with a different valid submission.
This is the only obligation covering the full stage → interrupt → resume → replace flow; store-level
persistence and active-attempt replacement are each proven elsewhere, and both can pass while the resume
wiring fails to let a restored attempt operate against the existing staged route. Execution:
`internal/runner` and `internal/control` package tests, `go test ./...`.

**INT-010: Route eligibility requires intake identity.** Boundary: `internal/model` validation,
`internal/exec` eligibility resolution, real control server. Setup: a user-authored workflow declaring
the route-submission tool; the intake workflow referenced as a sub-workflow of another workflow; the
genuine top-level intake workflow. Action: load each and attempt a submission where loading succeeds.
Assert: the user-authored workflow is rejected at load and statically by pre-validation; the nested
intake step is not route-eligible and its submission is rejected; only the genuine top-level intake step
is eligible. Execution: `internal/model`, `internal/prevalidate`, and `internal/exec` tests,
`go test ./...`.

The test plan deliberately leaves per-adapter `RunnerCommand` grant-string construction across all five
adapters to unit tests, as pure argument building. The design asks for the exact grant string each of
the five adapters emits for a route-eligible step, and its absence otherwise (`internal/cli`), plus
`submit_route` grant presence only for a route-eligible step (`internal/exec`). Cover the no-staged-route
completion case explicitly — that is what proves ordinary workflows are unaffected by the
`handleCompletion` edit.

## Done When

- `model.RunnerToolSubmitRoute` exists, `Step.validateTools` accepts it, and declaring it on a step that
  cannot satisfy intake identity is rejected at load and reported statically by `internal/prevalidate`.
- `internal/control` handles a fourth `submit_route` message type with attempt-scoped authentication
  identical to completion, its own acceptance state distinct from completion acceptance, and rejection
  plus audit for malformed, stale, unknown, or ineligible messages.
- Route eligibility is computed as tool declaration **and** top-level workflow **and** built-in intake
  identity by reference, and is passed into `control.AttemptOptions` from `ExecuteAgentStep`.
- A route-eligible attempt's child environment carries `AGENT_RUNNER_ROUTE_REQUEST` and
  `AGENT_RUNNER_INTAKE_HANDOFF` pointing at run-owned paths.
- The `submit_route` control message carries no route content; the runner reads the request only from
  the path it advertised for that attempt.
- Validation, handoff reading, and handoff copying run outside the server mutex via the sidecar's
  validate/prepare phase; the final eligibility recheck and the publication of the prepared route run
  inside the same mutex that guards completion acceptance. A prepared route that is not published is
  discarded, leaving no temporary snapshot and no change to a previously staged route.
- `handleCompletion` freezes a staged route before marking the completion accepted, and rejects the
  completion when the freeze fails. Completion with no staged route is accepted exactly as before.
- Retry of an accepted request ID returns the original acknowledgement without staging twice; a later
  valid submission replaces the staged route; a later invalid submission leaves it unchanged; a
  submission after accepted completion is rejected.
- An intake run interrupted with a route staged but its step incomplete resumes with that staged route
  still present, and the resumed attempt can replace it with a different valid submission.
- `cmd/agent-runner` exposes `step submit-route`: argument-free, exits nonzero on any argument, reads
  endpoint and credential only from the inherited control environment, exits nonzero with guidance when
  that environment is absent, and prints the runner's error to stderr and exits nonzero on rejection so
  the agent sees it in its own tool output. It is dispatched alongside the existing `step` handler at
  `cmd/agent-runner/main.go:307`.
- `cli.RunnerCommand` with `Kind`/`Executable` and kind-derived `Args()` replaces `CompletionCommand`;
  `BuildArgsInput` carries `RunnerCommands []RunnerCommand`; all five adapters iterate and grant each
  exactly. The `turn_committed` hook stays attached to the completion grant. The route grant appears
  only for a route-eligible step, and never for OpenCode.
- `workflows/core/intake-v1.0.yaml` exists, resolves as `core:intake`, declares `hidden: true`, reuses
  the `lead` profile, and has a single interactive agent step with `tools: [submit_route]` whose prompt
  establishes recommend-not-decide and writes the handoff to the runner-supplied path.
- `internal/audit` defines the route event types and the runner emits `route_submitted`,
  `route_accepted`, `route_rejected`, and `route_frozen` as intermediate events, with `route_accepted`
  carrying workflow, exact source reference, parameters, and sealed handoff location, `route_rejected`
  carrying a reason, and no route event carrying a launched-run ID.
- Nothing launches: no post-run launch path, no `route_launch_*` emission.
- INT-001, INT-002, INT-007, INT-010, and the staged-route resume portion of INT-005 pass.
- `make fmt`, `make lint`, and `make test` pass.
