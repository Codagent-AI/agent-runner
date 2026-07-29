## ADDED Requirements

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

### Requirement: Route request validation

On submission, Agent Runner SHALL validate that: the request decodes as strict JSON with unknown fields rejected; the request and referenced handoff are within bounded sizes; the named workflow resolves through the normal workflow catalog; every parameter the workflow declares as required is supplied; no parameter the workflow does not declare is supplied; the referenced handoff resolves to a readable, non-empty regular file contained within the run directory; and the named workflow is not the intake workflow itself. Any failure SHALL return an actionable error to the submitting agent while its session is still active, and SHALL leave any previously staged route unchanged.

#### Scenario: Unknown workflow is rejected inline
- **WHEN** the route request names a workflow that does not resolve through the catalog
- **THEN** the client receives an error naming the unresolved workflow
- **AND** no route is staged

#### Scenario: Missing required parameter is rejected inline
- **WHEN** the route request omits a parameter the selected workflow declares as required
- **THEN** the client receives an error naming the missing parameter
- **AND** no route is staged

#### Scenario: Undeclared parameter is rejected
- **WHEN** the route request supplies a parameter the selected workflow does not declare
- **THEN** the client receives an error naming the unexpected parameter
- **AND** no route is staged

#### Scenario: Malformed request is rejected
- **WHEN** the route request is not valid JSON, or contains a field the schema does not define
- **THEN** the client receives an error describing the decoding failure
- **AND** no route is staged

#### Scenario: Handoff outside the run directory is rejected
- **WHEN** the referenced handoff path resolves outside the run's own directory
- **THEN** the client receives an error stating the handoff must live inside the run directory
- **AND** no route is staged

#### Scenario: Unreadable or empty handoff is rejected
- **WHEN** the referenced handoff does not exist, is not a regular file, or is empty
- **THEN** the client receives an error describing the problem
- **AND** no route is staged

#### Scenario: Oversized input is rejected
- **WHEN** the route request or the referenced handoff exceeds its size bound
- **THEN** the client receives an error stating the bound
- **AND** no route is staged

#### Scenario: Routing to intake itself is rejected
- **WHEN** the route request names the intake workflow as its target
- **THEN** the client receives an error stating intake cannot route to itself
- **AND** no route is staged

#### Scenario: Agent corrects and retries within the session
- **WHEN** a submission is rejected and the agent writes a corrected request and submits again in the same session
- **THEN** the corrected submission is validated and accepted

### Requirement: Sealing on acceptance

On acceptance, Agent Runner SHALL seal a snapshot of the route: it SHALL copy the handoff bytes from the same opened handle it validated, record the exact resolved workflow source reference and the normalized parameters, and publish the snapshot atomically. Later modification of the agent-writable handoff original SHALL NOT change what is launched.

The workflow guarantee is scoped to **reference** rather than **content**: the exact source reference selected at acceptance is used at launch without re-resolving the logical name, so a newer version appearing in the meantime is not selected. Editing the bytes at that same path between acceptance and launch is **not** detected, because no content snapshot or hash of the workflow definition is taken.

#### Scenario: Handoff edited after acceptance does not change the launch
- **WHEN** the agent modifies the original handoff file after its route was accepted
- **THEN** the launched run receives the bytes sealed at acceptance, not the modified content

#### Scenario: Sealed record names the exact workflow definition
- **WHEN** a route is accepted
- **THEN** the sealed record identifies the exact resolved workflow source reference, not only the canonical workflow name

#### Scenario: A newer version is not selected at launch
- **WHEN** a newer version of the selected logical workflow becomes available between acceptance and launch
- **THEN** the launch uses the sealed source reference and does not select the newer version

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

### Requirement: Route state durability

Staged route state SHALL be persisted in run-owned storage that survives interruption, and SHALL be owned independently of the workflow's step-state record so that ordinary step-state writes cannot overwrite it. A resumed intake attempt SHALL be able to replace a route staged by an earlier attempt.

#### Scenario: Staged route survives interruption
- **WHEN** an intake run is interrupted after a route is staged but before its step completes, and the run is resumed
- **THEN** the previously staged route is still present

#### Scenario: Step-state writes do not clobber the route
- **WHEN** the workflow writes its step state after a route has been staged
- **THEN** the staged route remains intact

#### Scenario: Resumed attempt replaces the staged route
- **WHEN** a resumed intake attempt submits a different valid route
- **THEN** the staged route becomes the newly submitted one
