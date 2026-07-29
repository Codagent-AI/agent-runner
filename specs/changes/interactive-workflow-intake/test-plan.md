## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration, end-to-end, agent-acceptance, and exceptional human-only obligations.

The change's risk is concentrated in three places, and the layer choices below follow from that:

1. **State ownership.** A staged route is written out-of-band by a control-server goroutine while
   `writeStepState` rebuilds `RunState` from the execution context after every step. Proving the two
   do not collide requires real components together, so it is an integration obligation (INT-003).
2. **Ordering under concurrency.** Freeze must precede completion acknowledgement, and a failed
   freeze must reject completion — a new failure branch on a path every interactive step in every
   workflow depends on. Covered at the control-channel boundary (INT-002).
3. **Cross-process launch fidelity.** That the sealed workflow definition is what actually runs is
   only observable end to end (E2E-001, E2E-003).

One property cannot be proven by any deterministic layer: that a real CLI honours the exact-string
permission grant for the new argument-free command and runs it without stopping to ask the user.
The suite already asserts precisely this for `step complete`
(`cmd/agent-runner/real_agent_e2e_test.go`, the cursor `commandApprovals` check), so the new command
gets the same treatment in the same opt-in suite (E2E-004).

Deliberately left to unit tests, and not repeated here: the `-i` flag rejection matrix, including
`--cli` naming an adapter that rejects interactive steps (`cmd/agent-runner/main_test.go` already
covers argument parsing); per-adapter `RunnerCommand` grant-string construction across all five
adapters, which is pure argument building; and every `intakeroute.Validate` edge case — unknown
workflow, missing and undeclared parameters, malformed JSON, unknown fields, uncontained or
non-regular or empty handoff, oversized input, self-routing.

No new obligation is recorded for backward compatibility. Direct and headless invocation are already
exercised throughout the existing suite, which would fail if intake provenance leaked or if the
`intake_handoff` built-in were absent on a direct run.

## Scope Decisions (resolve-assumptions)

The obligations below were reviewed after implementation. Where an obligation's
property turned out to be covered by a cheaper existing test, it is narrowed here with
its rationale rather than silently dropped.

**Built as specified:**

- **E2E-003** — implemented as `TestInternalLaunchIntakeRouteIgnoresNewerVersionAtLaunch`
  (`cmd/agent-runner/intake_launch_test.go`). This was the only genuinely uncovered
  guarantee: a newer sibling version appearing between acceptance and launch must not be
  selected. The test writes `target-v1.1.yaml` after sealing `target-v1.0.yaml` and asserts
  the sealed reference ran.
- **INT-002 (freeze ordering), extended** — `TestControlServerRejectsCompletionWhenFreezeFails`
  now covers the failed-freeze branch, enabled by the `control.RouteStore` seam.

**Narrowed, with rationale:**

- **INT-006** — its launch-gating half is covered by
  `TestLaunchFrozenIntakeRouteGatesOnSuccessfulCompletedRunAndAppendsEvidence` and
  `TestLaunchFrozenIntakeRouteDoesNotLaunchFailedOrIncompleteRuns`, which assert that a
  frozen route on a failed or incomplete run does not launch. The durability-failure
  trigger itself lives in `internal/interactive` and is exercised by the existing
  durability suite; reproducing a real 30-second durability timeout in a launch test
  would add runtime without adding a distinct assertion.
- **INT-008** — `TestPrepareFreshRunPrevalidationFailureLeavesNoRunDirectory` covers the
  consequential half (a non-builtin target failing strict pre-validation is rejected and
  leaves no run directory). Engine-creation parity is structurally guaranteed because both
  launch paths call the same `prepareFreshRun`, so a divergence is not expressible.
- **INT-009** — route lifecycle audit events are asserted in
  `TestLaunchFrozenIntakeRouteGatesOnSuccessfulCompletedRunAndAppendsEvidence`, including
  that launch evidence is appended after `run_end` once the run logger is closed.
- **E2E-001, E2E-002, E2E-006** — the substance is covered by
  `TestInternalLaunchIntakeRoutePreparesSealedSourceWithCopiedHandoff` (sealed source,
  copied handoff, child provenance), `TestExplorationHandoffPathReturnsNonEmptyIntakeHandoff`
  (exploration-only), and the embedded-workflow tests. A fake-CLI PTY harness would
  re-assert these through a slower surface. **Live coverage exists instead:** acceptance
  flows AT-001 through AT-005 exercised all of these journeys against real agents, with evidence in
  the run's `acceptance-flow-evidence.md`.

**Left open, tracked as an assumption:**

- **E2E-004** — a real Cursor session asserting `commandApprovals == 0`. The underlying
  defect it guards is now fixed (Cursor emits the exact `step submit-route` grant, unit-tested
  in `internal/cli/cursor_route_test.go`), but the end-to-end proof that a live Cursor session
  runs the command unprompted still requires the `e2e_agents` suite and live credentials.
  This remains the one obligation without equivalent coverage.

## Integration Tests

### INT-001: Authenticated route submission stages a sealed route
- Covers: `intake-route-submission` — request location and submission client, route eligibility,
  request validation, sealing on acceptance
- Boundary: real `control.ControlServer` over a real Unix socket, `internal/intakeroute`, real filesystem
- Setup: a control server with a route-eligible active attempt; a run directory containing a valid
  `route-request.json` and a non-empty handoff; a workflow catalog resolving the named workflow
- Action: send a payload-free `submit_route` message carrying the active attempt's credential
- Assertions: the response is a success acknowledgement; `<run-dir>/intake-route.json` exists with
  state `staged`; `Sealed.SourceRef` is the exact resolved definition reference rather than the canonical
  name; `Sealed.HandoffPath` addresses a snapshot whose bytes match the source at submission time;
  mutating the agent-written handoff afterwards does not change the snapshot's contents; a submission
  from a non-route-eligible attempt is rejected and audited with workflow state unchanged
- Execution: `internal/control` and `internal/intakeroute` package tests, `go test ./...`

### INT-002: Freeze ordering and completion interaction
- Covers: `step-control-channel` — acknowledgement precedes termination;
  `intake-route-submission` — freeze on completion
- Boundary: real control server with concurrent per-connection goroutines, `internal/intakeroute`,
  real filesystem
- Setup: an active route-eligible attempt, exercised with and without a staged route; a store variant
  whose `Freeze` fails
- Action: send `complete_step`; separately send `submit_route` after completion acceptance
- Assertions: with a staged route, the sidecar reads `frozen` before the completion acknowledgement is
  written; a `submit_route` arriving after acceptance is rejected and audited; **with no staged route
  the completion is accepted normally**, proving ordinary workflows are unaffected; when `Freeze`
  fails the completion request is rejected rather than acknowledged, and the client may retry
- Execution: `internal/control` package tests, `go test ./...`

### INT-003: Step-state writes do not clobber a staged route
- Covers: `intake-route-submission` — route state durability
- Boundary: `internal/runner` step-state persistence, `internal/stateio`, `internal/intakeroute`,
  shared run directory
- Setup: a run whose control attempt has staged a route into the sidecar
- Action: drive the runner through a step boundary so `writeStepState` rebuilds and rewrites `RunState`
- Assertions: the sidecar still reads `staged` with identical contents after the state write; the
  rewritten `state.json` is well-formed and unaffected
- Execution: `internal/runner` package tests, `go test ./...`
- Note: this is the specific regression the sidecar design exists to prevent. If route state is ever
  moved into `RunState`, this test is what should fail.

### INT-004: Handoff built-in resolves through validation and runtime
- Covers: `builtin-vars` — `intake_handoff` built-in and built-in precedence;
  `workflow-pre-validation` — pipeline scope
- Boundary: `internal/prevalidate`, `internal/loader`, `internal/model` built-ins, runtime interpolation
- Setup: fixture workflows under `testdata/` — one whose prompt references `{{intake_handoff}}`, one
  declaring a parameter of that name, one capturing into that name
- Action: pre-validate and run each fixture, both as a direct run and as an intake-launched run
- Assertions: the referencing workflow pre-validates and interpolates to the sealed path when launched
  from intake and to the empty string when invoked directly, without an unresolved-variable failure;
  the parameter and capture fixtures are rejected at load and reported by pre-validation as a reserved
  name
- Execution: `internal/prevalidate` and `internal/runner` package tests, `go test ./...`

### INT-005: Handoff copy and provenance round-trip
- Covers: `workflow-intake` — launching the selected workflow, child-owned launch provenance;
  `resume-by-session-id` — intake provenance survives resume
- Boundary: `internal/runner` run preparation and resume, `internal/stateio`, real filesystem
- Setup: `Options` carrying `IntakeHandoffSource`, an intake parent run ID, and an agent override; plus
  an intake-parent run with a staged sidecar
- Action: `PrepareRun`, then interrupt and `PrepareResume`, for both a launched run and an intake parent
- Assertions: the handoff is copied into the new session directory and `{{intake_handoff}}` addresses
  the copy rather than the source; the copy survives deletion of the source; the resumed run restores
  the same handoff path and parent run ID; the launched run's persisted state records its own run ID
  and the workflow version it executed; a resumed intake parent restores its staged route and can
  replace it; a resumed intake run restores its `--cli` and `--model` override rather than reverting to
  the profile; a directly prepared run resumes with no parent and an empty handoff
- Execution: `internal/runner` package tests, `go test ./...`

### INT-006: Launch gating and post-freeze durability failure
- Covers: `workflow-intake` — launching the selected workflow (gating), launch failure and
  interruption recovery; `intake-route-submission` — freeze on completion (immutability)
- Boundary: real control server, `internal/interactive` durability wait, `internal/runner` outcome
  handling, `internal/intakeroute`
- Setup: an intake-shaped run whose route is staged, driven with a durability probe that can be made to
  fail after completion acceptance
- Action: accept completion, freeze, then fail durability; separately resume the failed run and let
  completion succeed
- Assertions: the run finishes failed with the route still `frozen` and unchanged; the launch gate does
  not fire, because it requires success **and** completed state **and** frozen; a route submission
  during the resumed attempt is rejected, proving frozen is immutable; after the resumed completion
  succeeds, the gate fires for the original route
- Execution: `internal/runner` and `internal/control` package tests, `go test ./...`
- Note: this is the finding that "frozen does not mean successful". Freeze happens at completion
  acceptance, which is an intermediate state up to 30 seconds before the step's real outcome is known.

### INT-007: Replacement, retry, and staging idempotency at the control boundary
- Covers: `intake-route-submission` — replacement and retry idempotency
- Boundary: real control server over a real socket, `internal/intakeroute`, real filesystem
- Setup: an active route-eligible attempt with a valid request
- Action: submit; resubmit the same request ID after discarding the response; submit a second, different
  valid request; submit an invalid request afterwards; drive a submission and a completion concurrently
  under a barrier in both orderings
- Assertions: the retry returns the original acknowledgement and exactly one route remains staged; the
  second valid submission replaces the first and is what freezes; the invalid submission leaves the
  previously staged route unchanged; each concurrent ordering resolves to either staged-then-frozen or
  rejected-because-accepted, and never to a submission staged after a freeze
- Execution: `internal/control` package tests, `go test ./...`

### INT-008: Launched runs are prepared like manual launches
- Covers: `workflow-intake` — launching the selected workflow (preparation parity)
- Boundary: the shared fresh-run preparation service, `internal/loader`, `internal/prevalidate`,
  `internal/engine`, `internal/runner`
- Setup: three sealed routes — one naming a non-builtin workflow that fails strict pre-validation, one
  naming a workflow declaring an `engine:` block, and one naming a valid builtin
- Action: prepare each through the launcher path and through the ordinary CLI path
- Assertions: the failing non-builtin is rejected with the same pre-validation error on both paths and
  leaves no run directory; the engine-backed workflow receives a created engine on both paths; the
  valid builtin prepares identically. Parameter defaults and binding match between the two paths
- Execution: `internal/runner` package tests, `go test ./...`

### INT-009: Route lifecycle audit evidence
- Covers: `audit-log-entries` — event types and route event data
- Boundary: `internal/audit` real log file, `internal/control`, the post-finalization append path
- Setup: an intake-shaped run driven through accept, reject, freeze, and launch-attempt
- Action: read back the run's audit log after finalization
- Assertions: `route_accepted` records the selected workflow, exact source reference, parameters, and
  sealed handoff location; `route_rejected` records a reason; `route_launch_attempted` is present
  **after** `run_end`, proving the standalone append works once the run logger is closed; a failed
  launch appends `route_launch_failed`; no route entry contains a launched-run ID
- Execution: `cmd/agent-runner` and `internal/audit` tests, `go test ./...`

### INT-010: Route eligibility requires intake identity
- Covers: `intake-route-submission` — route eligibility
- Boundary: `internal/model` validation, `internal/exec` eligibility resolution, real control server
- Setup: a user-authored workflow declaring the route-submission tool; the intake workflow referenced
  as a sub-workflow of another workflow; the genuine top-level intake workflow
- Action: load each and attempt a submission where loading succeeds
- Assertions: the user-authored workflow is rejected at load and statically by pre-validation; the
  nested intake step is not route-eligible and its submission is rejected; only the genuine top-level
  intake step is eligible
- Execution: `internal/model`, `internal/prevalidate`, and `internal/exec` tests, `go test ./...`
- Note: without identity gating, any workflow could declare the tool and chain into another workflow,
  which is the dynamic-orchestration non-goal.

## End-to-End Tests

### E2E-001: Intake stages, freezes, and launches the selected workflow
- Covers: `workflow-intake` — intake entry points, launching the selected workflow, child-owned
  provenance; `intake-route-submission` end to end
- Surface: the `agent-runner` binary
- Setup: isolated `HOME` and working directory following the existing smoke-test pattern; a fake CLI
  shell stub that writes a route request and a handoff, runs `agent-runner step submit-route`, then
  `agent-runner step complete`; a target workflow in the catalog whose prompt references
  `{{intake_handoff}}`
- Journey: start intake; the stubbed agent selects the target workflow with valid parameters; intake
  finalizes and the selected workflow launches
- Assertions: exactly one new run directory beyond the intake run; the launched run executed the
  definition at the sealed `SourceRef`; its `{{intake_handoff}}` resolved to a file inside its own
  directory whose contents match the sealed snapshot; its first agent session is fresh, containing no
  turn from the intake conversation; its state records the intake run as parent plus its own run ID and
  executed version; the intake run records the route and a launch attempt but no child run ID; deleting
  the intake run afterwards leaves the launched run's handoff readable; starting intake **by workflow
  name** launches identically to starting it with `-i`, without requiring the completed-run view to be
  dismissed first
- Execution: `cmd/agent-runner` integration test alongside the existing smoke tests, `go test ./...`

### E2E-002: Exploration-only intake launches nothing
- Covers: `workflow-intake` — exploration-only completion
- Surface: the `agent-runner` binary
- Setup: as E2E-001, with a stub that writes a handoff and calls `agent-runner step complete` without
  ever submitting a route
- Journey: start intake; the stubbed agent explores, writes a handoff to the runner-provided path, and
  finishes without selecting a workflow
- Assertions: the intake run finishes with a success outcome; no additional run directory is created;
  the handoff file remains readable and its path appears in the run's output, proving the runner can
  report a handoff location even though no route request was ever submitted to name it
- Execution: `cmd/agent-runner` integration test, `go test ./...`

### E2E-003: The sealed definition wins over a newer version
- Covers: `workflow-intake` — launching the selected workflow (sealed path);
  `intake-route-submission` — sealing on acceptance
- Surface: the `agent-runner` binary
- Setup: a project workflow catalog containing `example-v1.0.yaml`; a stub that routes to the logical
  name `example`; the test adds `example-v1.1.yaml`, distinguishable by its recorded output, after the
  route is accepted but before the launch
- Journey: intake accepts and freezes a route naming `example`; a newer version appears; intake
  finalizes and launches
- Assertions: the launched run executed `example-v1.0.yaml`, the definition sealed at acceptance, not
  `example-v1.1.yaml`; the recorded workflow file in the launched run's state matches the sealed
  `SourceRef`
- Execution: `cmd/agent-runner` integration test, `go test ./...`

### E2E-004: A real CLI runs the submission command without prompting
- Covers: `step-control-channel` — completion integration preserves supervision (route submission
  granted only in its exact form); `intake-route-submission` — submission client
- Surface: the `agent-runner` binary driving a live **Cursor** agent through a PTY
- Setup: reuses the existing real-agent harness — `prepareRealAgentE2E`,
  `writeRealAgentCatalogWorkflow`, `runRealAgentWorkflowInPTY`, `assertRealAgentRunCompleted` — driving
  the built-in intake workflow. The prompt instructs the agent to write a route request naming a
  trivial target workflow, run the submission command, then complete the step
- Journey: the live agent writes the request, runs `agent-runner step submit-route`, then completes
- Assertions: `commandApprovals == 0`, proving the exact-string grant covers the new command without a
  permission prompt; the sidecar reaches `frozen`; a child run is created and executed the sealed
  workflow
- Execution: `cmd/agent-runner`, build tag `e2e_agents`, run by `make test-e2e-agents`
- Constraints: **Cursor specifically, not the default CLI.** The harness comment is explicit that
  `commandApprovals` is "answered for Copilot, and only counted for Cursor", so asserting `== 0` on
  Claude or Codex would prove nothing. Cursor is where the narrow pre-approval means any prompt at all
  is a regression, which is exactly the property under test. Not parameterized across the other
  adapters, whose grant-string construction is unit-tested. Requires live agent credentials and
  consumes API budget; excluded from `go test ./...` by its build tag

### E2E-005: Intake requires a real terminal however it is started
- Covers: `workflow-intake` — intake invocation constraints; invocations naming another workflow
  bypass intake
- Surface: the `agent-runner` binary
- Setup: isolated `HOME` and working directory; no PTY attached
- Journey: invoke `-i` under `--headless`; invoke the intake workflow by name under `--headless`;
  invoke `-i` with `AGENT_RUNNER_NO_TUI=1` and stdout redirected; invoke a non-intake workflow normally
- Assertions: the first three exit nonzero stating an interactive terminal is required and create no
  run directory; the fourth runs normally, proving the check attaches to intake rather than to headless
  operation generally
- Execution: `cmd/agent-runner` integration test, `go test ./...`

### E2E-006: The shipped intake workflow and its handoff consumers
- Covers: `workflow-intake` — intake conversation responsibilities, shipped workflows consume the
  handoff; `builtin-workflows` — intake workflow embedded under core
- Surface: the `agent-runner` binary and the embedded workflow set
- Setup: the binary's own embedded workflows, with a fake CLI stub standing in for the agent
- Journey: run the embedded intake workflow; separately launch each of the two shipped
  handoff-consuming workflows both from intake and directly
- Assertions: the embedded intake workflow exists, is hidden, and its agent step is route-eligible, so
  a stub agent can actually submit a route through it; each shipped consumer's first agent step
  receives a prompt referencing the handoff when launched from intake, and one that does not when
  invoked directly. Exact prompt wording is not asserted
- Execution: `cmd/agent-runner` integration test, `go test ./...`
- Note: without this, an implementation could satisfy every specification delta while shipping an
  intake workflow that cannot route and consumers that ignore the handoff

## Agent Acceptance Tests

### AT-001: Explore, choose a workflow, and see it launch
- Classification: Required
- Covers: `workflow-intake` — intake entry points, launching the selected workflow, child-owned
  provenance; the change's primary journey
- Actor and surface: a developer at an interactive terminal, running the `agent-runner` CLI
- Setup: a git repository with a configured `lead` agent profile and working agent credentials
- Steps: run `agent-runner -i`; describe a small real change in conversation; let the agent inspect the
  repository and recommend a workflow; agree on one and supply the parameters it asks for
- Expected: intake ends and the chosen workflow starts in the same terminal; its first agent step
  demonstrably has the intake context, referring to conclusions from the intake conversation rather
  than asking from scratch
- Evidence: captured terminal output spanning both runs; the two run directories; the launched run's
  state showing the intake run as parent; the copied handoff inside the launched run's directory
- Effects and cleanup: creates two runs and consumes agent API budget; delete the created run
  directories afterwards. No repository mutation is required — stop the launched workflow at its first
  step
- Permitted substitutes: None

### AT-002: The New tab entry is present and behaves correctly
- Classification: Required
- Covers: `new-tab-layout` — the "Plan with an agent" entry and the modified navigation scenarios
- Actor and surface: a developer at an interactive terminal, running the `agent-runner` TUI
- Setup: a project with at least one project-scope workflow so more than one group renders
- Steps: run `agent-runner` with no arguments; observe the entry's position and the initial cursor;
  type a search filter matching a workflow name; press `h`; press `up` from the entry; press `down`
  from the entry; finally select the entry
- Expected: the entry renders above every group and holds the initial cursor; it remains visible under
  the search filter and is unaffected by `h`; `up` moves focus to the search box; `down` moves to the
  first workflow row, skipping the group header; selecting it starts intake
- Evidence: captured terminal renderings for the initial state, the filtered state, and the post-`h`
  state, which are the TUI equivalent of screenshots
- Effects and cleanup: starting intake creates one run; delete it afterwards
- Permitted substitutes: None

### AT-003: A bad route is corrected inside the conversation
- Classification: Required
- Covers: `intake-route-submission` — request validation and inline error reporting; the central
  usability claim behind submitting during the session rather than after it
- Actor and surface: a developer at an interactive terminal, running the `agent-runner` CLI
- Setup: as AT-001
- Steps: run `agent-runner -i`; instruct the agent to submit a route naming a workflow that does not
  exist, and separately one omitting a required parameter; observe what the agent sees; then let it
  correct the request and submit successfully
- Expected: each rejection returns an actionable message naming the unresolved workflow or the missing
  parameter, visible to the agent in its own tool output while the session is still live; the agent
  corrects and succeeds without the conversation ending or restarting
- Evidence: terminal transcript showing the rejection text and the subsequent successful submission
- Effects and cleanup: creates one intake run plus the eventually launched run; delete both. Consumes
  agent API budget
- Permitted substitutes: None. A fake CLI cannot demonstrate that a real agent can read and act on the
  error, which is the property under test

### AT-004: Exploration-only intake ends cleanly
- Classification: Required
- Covers: `workflow-intake` — exploration-only completion
- Actor and surface: a developer at an interactive terminal, running the `agent-runner` CLI
- Setup: as AT-001
- Steps: run `agent-runner -i`; explore a question with the agent; conclude that no implementation is
  warranted and end intake without selecting a workflow
- Expected: intake finishes reporting success; no workflow starts; the terminal returns to the shell;
  any handoff the agent wrote is reported by path and is readable
- Evidence: captured terminal output; confirmation that exactly one run directory was created
- Effects and cleanup: creates one run; delete it afterwards. Consumes agent API budget
- Permitted substitutes: None

### AT-005: Per-invocation CLI and model override
- Classification: Conditional — run when a second agent CLI is installed and credentialed on the host
- Covers: `workflow-intake` — intake agent overrides
- Actor and surface: a developer at an interactive terminal, running the `agent-runner` CLI
- Setup: two configured agent CLIs; a `lead` profile pointing at the first
- Steps: run `agent-runner -i --cli <second-cli>`; confirm which CLI is driving intake; select a
  workflow and let it launch; confirm which CLI drives the launched workflow's first agent step. Then
  separately run `agent-runner --cli <second-cli> <workflow>` without `-i`
- Expected: intake runs under the overridden CLI; the launched workflow resolves its agent through the
  ordinary profile rules and does **not** inherit the override; the invocation without `-i` is rejected
  with an error stating the flags require `-i`
- Evidence: captured terminal output and the recorded CLI in each run's state or metrics
- Effects and cleanup: creates two runs and consumes budget on both CLIs; delete the runs afterwards
- Permitted substitutes: None, but the flow is skippable when its activation condition is unmet. Record
  the reason if skipped

## Human-Only Testing

None.

## Coverage Map

Rows use each requirement's exact `### Requirement:` heading so the map can be checked mechanically
against the specification files. Every requirement in the change appears; a row of all dashes means the
requirement is deliberately covered by unit tests alone, with the reason given in Coverage Strategy.

| Requirement or journey | INT | E2E | AT | HT |
| --- | --- | --- | --- | --- |
| workflow-intake: Intake conversation responsibilities | — | E2E-006 | AT-001, AT-004 | — |
| workflow-intake: Shipped workflows consume the handoff | — | E2E-006 | AT-001 | — |
| workflow-intake: Intake entry points | — | E2E-001 | AT-001, AT-002 | — |
| workflow-intake: Intake invocation constraints | — | E2E-005 | — | — |
| workflow-intake: Intake agent overrides | INT-005 | — | AT-005 | — |
| workflow-intake: Exploration-only completion | — | E2E-002 | AT-004 | — |
| workflow-intake: Launching the selected workflow | INT-005, INT-006, INT-008 | E2E-001, E2E-003 | AT-001 | — |
| workflow-intake: Child-owned launch provenance | INT-005 | E2E-001 | AT-001 | — |
| workflow-intake: Launch failure and interruption recovery | INT-006, INT-008 | — | — | — |
| workflow-intake: Invocations naming another workflow bypass intake | — | E2E-005 | — | — |
| intake-route-submission: Route request location and submission client | INT-001 | E2E-004 | — | — |
| intake-route-submission: Route eligibility | INT-001, INT-010 | — | — | — |
| intake-route-submission: Route request validation | INT-001 | — | AT-003 | — |
| intake-route-submission: Sealing on acceptance | INT-001 | E2E-003 | — | — |
| intake-route-submission: Replacement and retry idempotency | INT-007 | — | — | — |
| intake-route-submission: Freeze on completion | INT-002, INT-006, INT-007 | — | — | — |
| intake-route-submission: Route state durability | INT-003 | — | — | — |
| step-control-channel: Route submission message type | INT-001, INT-010 | — | — | — |
| step-control-channel: Acknowledgement precedes termination | INT-002 | — | — | — |
| step-control-channel: Completion integration preserves supervision | — | E2E-004 | — | — |
| builtin-vars: intake_handoff built-in variable | INT-004 | E2E-001 | — | — |
| builtin-vars: Built-in precedence | INT-004 | — | — | — |
| new-tab-layout: Plan with an agent entry | — | — | AT-002 | — |
| new-tab-layout: Workflow groups render with header and description | — | — | AT-002 | — |
| builtin-workflows: Intake workflow embedded under core | — | E2E-006 | AT-002 | — |
| resume-by-session-id: Intake provenance survives resume | INT-005 | — | — | — |
| audit-log-entries: Event types | INT-009 | — | — | — |
| audit-log-entries: Route event data | INT-009 | — | — | — |
| workflow-pre-validation: Pre-validation pipeline scope | INT-004, INT-010 | — | — | — |
