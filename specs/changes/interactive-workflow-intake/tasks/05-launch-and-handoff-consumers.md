# Task: Launch across the process boundary, shared fresh-run preparation, and handoff consumers

## Goal

Close the loop: when an intake run finishes successfully with a frozen route, launch the selected
workflow as a fresh top-level run carrying the sealed handoff — prepared exactly as a manual launch
would be, recording child-owned provenance, with launch evidence appended after the run's audit log has
closed. Make exploration-only a clean success path. Teach the two most commonly routed-to shipped
workflows to read the handoff.

## Background

### Why the launch happens at the process boundary

The workflow executes in a goroutine while the Bubble Tea program owns the terminal, and TUI-originated
launches happen only after that program exits, in the top-level switcher (`cmd/agent-runner/main.go`,
`execSelfWithEnv` at line 958 and `execStartRun` around line 1063). Explicit CLI runs bypass the
switcher entirely and dispatch straight into the run path.

Launching from inside the control handler would bypass `runner.finalizeRun`, which closes the control
server, releases the run lock, marks state completed, emits `run_end`, and closes the audit log. So route
submission only validates and stages, the run finalizes normally, and the launch happens after the run
returns.

### The check belongs to the run, not the entry point

The intake workflow is an ordinary embedded workflow, so `agent-runner core:intake` starts it too. If
the frozen-route check lived only in the `-i` path, starting intake by name would hold the whole
conversation, freeze a route, and then silently discard it — the agent would tell the user it was
launching something and nothing would happen. So the check sits on the shared path every top-level run
returns through:

```
after any top-level run completes:
    if result != ResultSuccess:      return result   // durability failure, step failure, ...
    sealed := intakeroute.Load(<run-dir>)            // single stat; absent for every non-intake run
    if sealed == nil || sealed.State != frozen:      return result
    if !stateMarkedCompleted(<run-dir>):             return result
    appendLaunchAttempted(<run-dir>, sealed)
    exec agent-runner internal launch-intake-route <run-dir>/intake-route.json
```

The cost on ordinary runs is one comparison plus one stat of a file that does not exist.

### Frozen does not mean successful

This is the subtlest rule in the change. Completion acceptance is explicitly an **intermediate** state.
After the completion is delivered, `finishDirectCompletion` waits up to 30 seconds for semantic
turn-durability evidence, and on failure the step fails and the run fails. So a run can end **failed**
while its route is **frozen**.

Launching therefore requires all three of: the run returned success, its persisted state is marked
completed, and the route is frozen. Checking `State == frozen` alone would launch a workflow off an
intake turn that was never durably recorded.

When a route freezes and durability then fails, the frozen route is retained and nothing launches. The
run is resumable, and because a frozen route is immutable, the resumed attempt retries completion against
that same route; success then launches it. This keeps a decision the user actually made without acting on
an unrecorded turn.

### The transition must not wait on the user

The live TUI only sends itself an exit message when `quitOnDone` is set, which an ordinary by-name run
does not set (`cmd/agent-runner/main.go:774`, `:831`, `:907`). Left alone, `agent-runner core:intake`
would strand the user on a completed-run view until they dismissed it, while `-i` transitioned
immediately. A successful intake run with a frozen route therefore signals the TUI to exit regardless of
how it was started.

### `prepareFreshRun` is a shared service, not a shortcut

`runner.PrepareRun` takes a parsed `*model.Workflow`, not a path. The real launch path
(`handleRunWithRunOptions`, `cmd/agent-runner/main.go:1650`) does considerably more before calling it:

```go
workflow, err := loader.LoadWorkflow(workflowFile, loader.Options{})
positional, keyed, err := parseParams(args[1:])
params, err := matchParams(&workflow, positional, keyed)
if !builtinworkflows.IsRef(workflowFile) {
    prevalidate.Pipeline(workflowFile, params, prevalidate.Strict, prevalidate.Options{})
}
if workflow.Engine != nil { eng, err = engine.Create(engConfig) }
runner.PrepareRun(&workflow, params, &runner.Options{...})
```

A launcher that called `PrepareRun` directly would skip **strict pre-validation for project workflows**
and start **engine-backed workflows without their engine** — an intake-launched run would behave
differently from the same workflow launched by hand. So extract that sequence into one internal service
used by **both** callers:

```
prepareFreshRun(req) :=
    workflow := LoadWorkflow(req.SourceRef)
    params   := bindAndDefault(workflow, req.Params)
    if not builtin(req.SourceRef): prevalidate.Pipeline(..., Strict)
    engine   := engine.Create(workflow.Engine)      // when declared
    runner.PrepareRun(&workflow, params, Options{ engine, intake provenance, ... })
```

The difference between the two callers is only the front end: the CLI resolves a logical name into a
reference first, while the launcher already has one. Do **not** reuse `discovery.WorkflowEntry` for
launch validation — discovery models the browser's view of a catalog and does not resolve exactly as the
CLI launch path does.

A failure anywhere in `prepareFreshRun` must not leave a partially created run directory behind, and
must surface the cause to its caller.

**Error reporting is the caller's job, not the shared service's.** The design's goal that every existing
invocation path stay unchanged in behavior applies here: an ordinary CLI launch has no sealed handoff,
so it must keep reporting preparation failures exactly as `handleRunWithRunOptions` does today. Only the
intake launcher adds the sealed handoff path to the message before exiting nonzero. So the shared
service returns errors and guarantees cleanup; it does not print, does not exit, and does not know about
intake-only state.

### The launcher takes only a path, and never re-resolves the name

```
handleLaunchIntakeRoute(path):
    sealed := strict-decode(path)
    verify invariants: state == frozen, SourceRef resolves,
                       HandoffPath readable and non-empty
    prepareFreshRun(FreshRunRequest{
        SourceRef:           sealed.SourceRef,   // already resolved; never re-resolved
        Params:              sealed.Params,
        IntakeParentRunID:   sealed.ParentRunID,
        IntakeHandoffSource: sealed.HandoffPath,
    })
```

The subcommand takes **only** the absolute sidecar path. The complete launch plan lives in that one
artifact, so nothing is inherited through the environment and nothing is re-derived.
`resolveWorkflowArg` (`cmd/agent-runner/main.go:1449`) deliberately rejects versioned names and selects
the latest version, so routing the launch back through it would reintroduce exactly the drift sealing
exists to prevent.

It is dispatched in `run()` before flag parsing, alongside the existing `step` and `internal` handlers
(`cmd/agent-runner/main.go:307-312`), and is excluded from help output and completions. It is hidden and
unsupported, **not a security boundary** — anything that can write that file already runs as the user.
Mitigation is invariant validation at launch so a malformed or partially written artifact fails loudly
rather than launching something unexpected.

The launcher must **not** read or populate the intake run's `--cli`/`--model` override, so the launched
workflow resolves every agent through the ordinary step and profile rules.

### Handoff copy ordering and provenance

The launched run's directory does not exist until `PrepareRun` creates it, so the copy cannot precede the
launch. `Options.IntakeHandoffSource` carries the sealed snapshot path in; `PrepareRun` copies it into
the new session directory and sets the context's handoff field to the destination. The launched run is
then self-contained: deleting the intake run later does not break it. That copy behavior already exists;
your job is to feed it.

Provenance is **child-owned**. The child's run ID is generated inside `runner.Start` after the parent
process has been replaced and its audit log closed, so the parent structurally **cannot** record it. The
child records its parent instead.

### Launch-attempt evidence after finalization

`finalizeRun` closes the audit logger before returning, and `Logger.Emit` silently discards events once
closed. Since the launch is deliberately attempted after that return, `route_launch_attempted` cannot be
emitted through the run's logger — it would vanish.

So launch evidence is appended through a **standalone append against the run's audit file**, opened and
closed for that write alone, immediately before `exec`. If `exec` returns — meaning it failed, since a
successful `exec` never returns — a `route_launch_failed` record is appended the same way. These entries
follow `run_end` in the log, which the audit specification explicitly permits.

Recording the attempt *before* `exec` rather than after is deliberate: after a successful `exec` this
process no longer exists, so there is no later moment at which to write anything. Use the real
`audit.EventLogger` types; do not replace the interface.

### Handoff consumers

Two shipped workflows gain a handoff reference in their first agent step:

- `workflows/core/define-change-v1.0.yaml` — the `proposal` step, whose prompt currently opens *"First
  ask what the change is about; do not guess."* This sub-workflow is **shared by both the spec-driven
  and OpenSpec change flows**, so editing it once reaches both.
- `workflows/spec-driven/simple-change-v1.0.yaml` — edited separately.

Each first agent step must be instructed to read `{{intake_handoff}}` **before** asking the user for
context when that value is non-empty, and must behave exactly as today when it is empty. Exact prompt
wording is an implementation detail. These are workflow YAML edits — configuration, not behavior — so
focused unit tests are not expected for the YAML itself; the behavior is covered end to end.

The lifecycle's prompts gain a handoff reference; its structure does not change.

### Exploration-only

If the intake agent completes its step with no accepted route, the intake run finishes cleanly, launches
nothing, and preserves any handoff that was written. Because the handoff path is **runner-owned** rather
than agent-chosen, the runner can report that location whenever the file exists and is non-empty, even
though no route request was ever submitted to name it.

### What already exists that you depend on

`{{intake_handoff}}` is a built-in on every run, reserved and pre-validation-aware;
`runner.Options` already accepts an intake handoff source and parent run ID, `PrepareRun` already copies
the handoff into the new session directory, and `RunState`/`PrepareResume` already round-trip provenance.
`internal/intakeroute` provides `Load`/`Stage`/`Freeze` and transport-independent invariant validation.
Route submission, eligibility, and freeze-on-completion work over the control channel, emitting
`route_submitted`/`route_accepted`/`route_rejected`/`route_frozen`. `workflows/core/intake-v1.0.yaml`
exists as a hidden, route-eligible `core:intake`. Entry points `-i`, `--cli`, `--model`, the intake TTY
gate, and the "Plan with an agent" New tab entry all work.

## Spec

From `specs/workflow-intake/spec.md`:

### Requirement: Exploration-only completion

An intake run whose agent step completes without an accepted route SHALL finish successfully, SHALL NOT launch any workflow, and SHALL preserve any handoff the agent wrote. Because the handoff path is runner-owned rather than supplied by the agent, Agent Runner SHALL be able to report that location whenever the file exists and is non-empty, even though no route request was ever submitted.

#### Scenario: Completing with no route launches nothing
- **WHEN** the intake agent completes its step and no route was ever accepted
- **THEN** the intake run finishes with a success outcome
- **AND** no new run is created

#### Scenario: Handoff written without a route is preserved
- **WHEN** the intake agent wrote a handoff but completed the step without submitting a route
- **THEN** the handoff file remains readable after the run finishes
- **AND** Agent Runner reports its path

### Requirement: Launching the selected workflow

Agent Runner SHALL launch the selected workflow only when all three conditions hold: the intake run finished successfully, its persisted state is marked completed, and its route is frozen. A frozen route alone SHALL NOT authorize a launch, because a route freezes when completion is accepted, which is an intermediate state that a later durability failure can still turn into a failed step.

When those conditions hold, Agent Runner SHALL start exactly one new top-level run of the selected workflow using the sealed exact workflow source reference recorded at acceptance, without re-resolving the logical workflow name. The launched run SHALL be prepared through the same loading, parameter binding, pre-validation, and engine-creation path as a manually launched run, so an intake-launched workflow is validated and configured identically to the same workflow launched by hand.

Launching SHALL be a property of the intake run rather than of the entry point used to start it, so an intake run started by workflow name launches identically to one started through `-i` or the New tab entry, and the transition SHALL occur without requiring the user to dismiss a completed-run view first.

The launched run's first agent session SHALL be fresh and SHALL NOT inherit the intake conversation. The sealed handoff SHALL be copied into the launched run's own directory so the launched run does not depend on the intake run's directory surviving.

#### Scenario: Exactly one run is launched
- **WHEN** an intake run finishes successfully with a frozen route and completed state
- **THEN** exactly one new top-level run of the selected workflow is created

#### Scenario: A frozen route on a failed run does not launch
- **WHEN** an intake run's route is frozen but the run finishes with a failed outcome
- **THEN** no workflow is launched

#### Scenario: Launched run is prepared like a manual launch
- **WHEN** a workflow that requires pre-validation or an engine is launched from intake
- **THEN** it is pre-validated and its engine created exactly as when the same workflow is launched manually
- **AND** a pre-validation failure prevents the launch and reports the failure rather than starting a partially prepared run

#### Scenario: Intake started by name launches identically
- **WHEN** the user starts intake by naming the intake workflow directly rather than using `-i` or the New tab entry, and the run finishes successfully with a frozen route
- **THEN** the selected workflow is launched exactly as it would have been from either entry point
- **AND** the transition happens without the user having to dismiss a completed-run view

#### Scenario: Sealed path wins over later version selection
- **WHEN** a newer version of the selected workflow becomes available between route acceptance and launch
- **THEN** the launched run executes the workflow definition recorded in the sealed route, not the newer version

#### Scenario: Launched workflow starts a fresh session
- **WHEN** the launched workflow reaches its first agent step
- **THEN** that step runs in a new agent session containing none of the intake conversation

#### Scenario: Handoff is copied into the launched run
- **WHEN** a workflow is launched from intake
- **THEN** the sealed handoff is present in the launched run's own directory
- **AND** deleting the intake run afterward leaves the launched run's handoff readable

### Requirement: Child-owned launch provenance

The launched run SHALL record the intake run's ID, its own run ID, and the workflow version it actually executed. The intake run SHALL record the frozen route and that a launch was attempted, and SHALL NOT record the launched run's ID, because that ID does not exist until after the intake process has been replaced and its audit log closed.

#### Scenario: Launched run names its intake parent
- **WHEN** a run is launched from intake
- **THEN** its persisted state identifies the intake run it came from, its own run ID, and the workflow version it executed

#### Scenario: Intake run records the route and the attempt
- **WHEN** an intake run finalizes with a frozen route
- **THEN** its audit evidence records the selected workflow, the sealed source path, the parameters, the handoff location, and that a launch was attempted

#### Scenario: Intake run does not claim a child run ID
- **WHEN** an intake run's evidence is inspected after a successful launch
- **THEN** it contains no launched-run ID, and the linkage is discoverable from the launched run instead

### Requirement: Launch failure and interruption recovery

If starting the launched run fails, Agent Runner SHALL exit nonzero, report the failure and the sealed handoff path, and SHALL NOT leave a partially created run behind.

A frozen route SHALL be immutable: it survives interruption and failure, and no later submission may replace it. When an intake run freezes a route but then fails — most commonly because turn durability could not be confirmed after completion was accepted — Agent Runner SHALL retain the frozen route, launch nothing, and allow the run to be resumed. A resumed attempt SHALL retry completion against that same frozen route, and a successful resumed attempt SHALL launch it. This preserves a decision the user actually made without acting on a turn that was never durably recorded.

If the process is terminated after the route freezes and after the run completed successfully, but before the launch, the intake run SHALL remain complete and inspectable with its sealed handoff intact, and the user SHALL restart intake rather than resuming it.

#### Scenario: Launch failure leaves no partial run
- **WHEN** starting the launched run fails
- **THEN** Agent Runner exits nonzero, reports the cause and the sealed handoff path
- **AND** no new run directory is left behind

#### Scenario: Durability failure after freeze does not launch
- **WHEN** a route is frozen at completion acceptance and turn durability is subsequently not confirmed, failing the step
- **THEN** no workflow is launched
- **AND** the frozen route is retained unchanged

#### Scenario: Resume after a durability failure launches the original route
- **WHEN** an intake run that failed after freezing a route is resumed and its completion succeeds
- **THEN** the workflow recorded in the original frozen route is launched
- **AND** no submission during the resumed attempt could have replaced it

#### Scenario: Termination between freeze and launch leaves an inspectable run
- **WHEN** the process is killed after a successful intake run froze its route but before the launch, and the user later passes that run to `--resume`
- **THEN** the run opens in inspect mode because it is already complete
- **AND** the sealed handoff remains readable

### Requirement: Shipped workflows consume the handoff

The built-in workflows that intake most commonly routes to SHALL read the handoff when one is present. Specifically, the first agent step of the shared change-definition workflow and of the simple-change workflow SHALL instruct the agent to read `{{intake_handoff}}` before asking the user for context when that value is non-empty, and SHALL behave exactly as they do today when it is empty. Exact prompt wording is an implementation detail.

#### Scenario: Change definition reads an intake handoff
- **WHEN** the shared change-definition workflow is launched from intake
- **THEN** its first agent step is instructed to read the handoff before asking the user what the change is about

#### Scenario: Simple change reads an intake handoff
- **WHEN** the simple-change workflow is launched from intake
- **THEN** its first agent step is instructed to read the handoff before asking the user what the change is about

#### Scenario: Direct invocation is unchanged
- **WHEN** either workflow is invoked directly, so the handoff value is empty
- **THEN** its first agent step behaves exactly as it does today, asking the user for context

From `specs/audit-log-entries/spec.md`:

### Requirement: Route event data

Route lifecycle events SHALL carry enough structured data to answer, from the intake run's evidence alone, which workflow was selected, which exact workflow definition it resolved to, which parameters were supplied, where the sealed handoff lives, and whether a launch was attempted and with what outcome. A `route_rejected` event SHALL record the rejection reason. Events SHALL NOT record a launched run ID, because the launched run does not exist while the intake run's evidence is being written.

Launch-attempt evidence is written **after** normal run finalization has already closed the run's audit log, so it SHALL be appended to the run's audit log through a mechanism that does not depend on the closed run logger, and it SHALL be valid for these entries to follow `run_end`. A launch that fails SHALL be recorded as such, so the evidence distinguishes an attempted launch from a successful one.

#### Scenario: Acceptance records the sealed route
- **WHEN** a route submission is accepted
- **THEN** the `route_accepted` entry records the selected workflow, the exact resolved workflow source reference, the supplied parameters, and the sealed handoff location

#### Scenario: Rejection records the reason
- **WHEN** a route submission is rejected
- **THEN** the `route_rejected` entry records the reason for the rejection

#### Scenario: Launch attempt is recorded after finalization
- **WHEN** an intake run finishes successfully with a frozen route and a launch is attempted
- **THEN** a `route_launch_attempted` entry naming the workflow being launched is present in the intake run's audit log
- **AND** it appears after that run's `run_end` entry
- **AND** it contains no launched-run ID

#### Scenario: Launch failure is recorded
- **WHEN** the launch attempt fails
- **THEN** a `route_launch_failed` entry recording the cause is appended to the intake run's audit log

### Requirement: Event types

The audit log SHALL support these event types: `run_start`, `run_end`, `step_start`, `step_end`, `iteration_start`, `iteration_end`, `sub_workflow_start`, `sub_workflow_end`, `error`, `completion_requested`, `completion_acknowledged`, `turn_committed`, `durability_failure`, `control_rejected`, `child_stopped`, `child_continued`, `route_submitted`, `route_accepted`, `route_rejected`, `route_frozen`, `route_launch_attempted`, and `route_launch_failed`.

#### Scenario: All event types recognized
- **WHEN** the audit logger receives any of the defined event types
- **THEN** it writes the entry without error

#### Scenario: Completion events are intermediate
- **WHEN** the audit logger receives control or durability events during an interactive agent step
- **THEN** it writes them as intermediate events distinct from the step's final `step_end`

#### Scenario: Route events are intermediate
- **WHEN** the audit logger receives route lifecycle events during the intake agent step
- **THEN** it writes them as intermediate events distinct from the step's final `step_end`

> The in-run route event types and their data are already emitted through the run's logger. Your portion
> of these two requirements is `route_launch_attempted` and `route_launch_failed`, appended through a
> mechanism independent of the closed run logger, positioned after `run_end`, and carrying no
> launched-run ID.

## Test Plan

You MUST read `test-plan.md` for the full text of the obligations below.

**INT-006: Launch gating and post-freeze durability failure.** Boundary: real control server,
`internal/interactive` durability wait, `internal/runner` outcome handling, `internal/intakeroute`.
Setup: an intake-shaped run whose route is staged, driven with a durability probe that can be made to
fail after completion acceptance. Action: accept completion, freeze, then fail durability; separately
resume the failed run and let completion succeed. Assert: the run finishes failed with the route still
`frozen` and unchanged; the launch gate does not fire, because it requires success **and** completed
state **and** frozen; a route submission during the resumed attempt is rejected, proving frozen is
immutable; after the resumed completion succeeds, the gate fires for the original route. Execution:
`internal/runner` and `internal/control` package tests, `go test ./...`.

**INT-008: Launched runs are prepared like manual launches.** Boundary: the shared fresh-run preparation
service, `internal/loader`, `internal/prevalidate`, `internal/engine`, `internal/runner`. Setup: three
sealed routes — one naming a non-builtin workflow that fails strict pre-validation, one naming a
workflow declaring an `engine:` block, and one naming a valid builtin. Action: prepare each through the
launcher path **and** through the ordinary CLI path. Assert: the failing non-builtin is rejected with the
**same** pre-validation error on both paths and leaves no run directory; the engine-backed workflow
receives a created engine on both paths; the valid builtin prepares identically; parameter defaults and
binding match between the two paths. Execution: `internal/runner` package tests, `go test ./...`.

**INT-009: Route lifecycle audit evidence.** Boundary: `internal/audit` real log file, `internal/control`,
the post-finalization append path. Setup: an intake-shaped run driven through accept, reject, freeze, and
launch-attempt. Action: read back the run's audit log after finalization. Assert: `route_accepted`
records the selected workflow, exact source reference, parameters, and sealed handoff location;
`route_rejected` records a reason; `route_launch_attempted` is present **after** `run_end`, proving the
standalone append works once the run logger is closed; a failed launch appends `route_launch_failed`; no
route entry contains a launched-run ID. Execution: `cmd/agent-runner` and `internal/audit` tests,
`go test ./...`.

**E2E-001: Intake stages, freezes, and launches the selected workflow.** Surface: the `agent-runner`
binary. Setup: isolated `HOME` and working directory following the existing smoke-test pattern; a fake
CLI shell stub that writes a route request and a handoff, runs `agent-runner step submit-route`, then
`agent-runner step complete`; a target workflow in the catalog whose prompt references
`{{intake_handoff}}`. Assert: exactly one new run directory beyond the intake run; the launched run
executed the definition at the sealed `SourceRef`; its `{{intake_handoff}}` resolved to a file inside its
own directory whose contents match the sealed snapshot; its first agent session is fresh, containing no
turn from the intake conversation; its state records the intake run as parent plus its own run ID and
executed version; the intake run records the route and a launch attempt but no child run ID; deleting the
intake run afterwards leaves the launched run's handoff readable; starting intake **by workflow name**
launches identically to starting it with `-i`, without requiring the completed-run view to be dismissed
first. Execution: `cmd/agent-runner` integration test alongside the existing smoke tests,
`go test ./...`.

**E2E-002: Exploration-only intake launches nothing.** As E2E-001, with a stub that writes a handoff and
calls `agent-runner step complete` without ever submitting a route. Assert: the intake run finishes with
a success outcome; no additional run directory is created; the handoff file remains readable and its path
appears in the run's output, proving the runner can report a handoff location even though no route
request was ever submitted to name it.

**E2E-003: The sealed definition wins over a newer version.** Setup: a project workflow catalog containing
`example-v1.0.yaml`; a stub that routes to the logical name `example`; the test adds `example-v1.1.yaml`,
distinguishable by its recorded output, **after** the route is accepted but before the launch. Assert:
the launched run executed `example-v1.0.yaml`, the definition sealed at acceptance, not
`example-v1.1.yaml`; the recorded workflow file in the launched run's state matches the sealed
`SourceRef`.

**E2E-004: A real CLI runs the submission command without prompting.** Surface: the `agent-runner` binary
driving a live **Cursor** agent through a PTY. Reuses the existing real-agent harness —
`prepareRealAgentE2E`, `writeRealAgentCatalogWorkflow`, `runRealAgentWorkflowInPTY`,
`assertRealAgentRunCompleted` (`cmd/agent-runner/real_agent_e2e_test.go`) — driving the built-in intake
workflow. The prompt instructs the agent to write a route request naming a trivial target workflow, run
the submission command, then complete the step. Assert: `commandApprovals == 0`, proving the exact-string
grant covers the new command without a permission prompt; the sidecar reaches `frozen`; a child run is
created and executed the sealed workflow. Execution: `cmd/agent-runner`, build tag `e2e_agents`, run by
`make test-e2e-agents`. **Constraint: Cursor specifically, not the default CLI.** The harness comment is
explicit that `commandApprovals` is "answered for Copilot, and only counted for Cursor", so asserting
`== 0` on Claude or Codex would prove nothing. Do not parameterize across the other adapters. Requires
live agent credentials and consumes API budget; excluded from `go test ./...` by its build tag.

**E2E-006: The shipped intake workflow and its handoff consumers.** Surface: the `agent-runner` binary and
the embedded workflow set. Setup: the binary's own embedded workflows, with a fake CLI stub standing in
for the agent. Journey: run the embedded intake workflow; separately launch each of the two shipped
handoff-consuming workflows both from intake and directly. Assert: the embedded intake workflow exists,
is hidden, and its agent step is route-eligible, so a stub agent can actually submit a route through it;
each shipped consumer's first agent step receives a prompt referencing the handoff when launched from
intake, and one that does not when invoked directly. **Exact prompt wording is not asserted.** Execution:
`cmd/agent-runner` integration test, `go test ./...`. The test plan notes: *without this, an
implementation could satisfy every specification delta while shipping an intake workflow that cannot
route and consumers that ignore the handoff.*

The design additionally asks for: a frozen route on a failed run does not launch; a durability failure
after freeze retains the route and launches nothing; a resumed attempt that completes successfully
launches the original frozen route; the launcher rejects a non-frozen or invariant-violating artifact;
and launch-attempt and launch-failure records are appended after `run_end` (`cmd/agent-runner`).

## Done When

- A shared fresh-run preparation service performs load → parameter bind and default → strict
  pre-validation for non-builtins → engine creation → `PrepareRun`, and **both** the ordinary CLI launch
  in `handleRunWithRunOptions` and the intake launcher go through it. Neither path duplicates the
  sequence.
- Any failure inside that service leaves no partially created run directory and surfaces the cause to
  its caller. The ordinary CLI launch reports preparation failures exactly as it does today; the intake
  launcher reports the cause **and the sealed handoff path** and exits nonzero. The shared service
  itself neither prints nor exits, and carries no intake-only state.
- A launch gate sits on the shared path every top-level run returns through, firing only when the run
  returned success **and** its persisted state is marked completed **and** the sidecar reads `frozen`.
  Ordinary non-intake runs pay one comparison and one stat of an absent file.
- `agent-runner internal launch-intake-route <path>` accepts only the absolute sidecar path, is
  dispatched before flag parsing alongside the existing `step`/`internal` handlers, is excluded from help
  and completions, strict-decodes the artifact, verifies `state == frozen`, that `SourceRef` resolves,
  and that `HandoffPath` is readable and non-empty, and launches `SourceRef` directly without
  re-resolving the logical name.
- The launcher does not read or propagate the intake run's `--cli`/`--model` override; the launched run
  resolves agents through ordinary step and profile rules.
- The launched run's state records its intake parent run ID, its own run ID, and the executed workflow
  version; the intake run records the frozen route and the launch attempt and never a child run ID.
- The sealed handoff is copied into the launched run's own directory and survives deletion of the intake
  run; the launched run's first agent session is fresh.
- A successful intake run with a frozen route signals the TUI to exit regardless of how intake was
  started, so `agent-runner core:intake` transitions without the user dismissing a completed-run view.
- Exploration-only intake finishes successfully, launches nothing, preserves the handoff, and reports the
  runner-owned handoff path when the file exists and is non-empty.
- A durability failure after freeze retains the frozen route unchanged, launches nothing, and leaves the
  run resumable; a successful resumed completion launches the original frozen route.
- `route_launch_attempted` is appended immediately **before** `exec` through a standalone append against
  the run's audit file, opened and closed for that write alone; `route_launch_failed` is appended the
  same way if `exec` returns. Both follow `run_end` and carry no launched-run ID. The real
  `audit.EventLogger` types are used.
- `workflows/core/define-change-v1.0.yaml`'s `proposal` step and
  `workflows/spec-driven/simple-change-v1.0.yaml`'s first agent step instruct the agent to read
  `{{intake_handoff}}` before asking the user for context when non-empty, and are unchanged in behavior
  when empty. Both still pre-validate and interpolate on a direct invocation.
- INT-006, INT-008, INT-009, E2E-001, E2E-002, E2E-003, and E2E-006 pass under `go test ./...`; E2E-004
  passes under the `e2e_agents` build tag with `make test-e2e-agents`.
- Direct CLI, headless, and manual New-tab launches are unchanged, with `{{intake_handoff}}` empty and
  no intake parent, and resuming one of those direct-origin runs stays empty. An intake-launched run
  resumes with its sealed handoff path and intake parent run ID intact.
- `make fmt`, `make lint`, and `make test` pass.
