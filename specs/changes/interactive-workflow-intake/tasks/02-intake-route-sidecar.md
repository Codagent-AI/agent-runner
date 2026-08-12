# Task: Intake route sidecar package with validation and sealing

## Goal

Create `internal/intakeroute`: a run-owned sidecar store that validates an agent-written route request,
seals a snapshot of the chosen workflow and its handoff, and manages a `staged → frozen` lifecycle with
atomic writes.

This is the transport-independent core of intake routing. Delivering it as its own package with its own
ownership rules is what keeps a durable, acknowledged route from being silently destroyed by ordinary
step-state writes, and what lets both the control channel and the launcher share one set of invariant
checks.

## Background

### Why route state is not a field on `RunState`

This is the single most important constraint in the task, and it is worth stating precisely for anyone
tempted to simplify it later.

`runner.writeStepState` (`internal/runner/runner.go:824`) constructs a **fresh** `model.RunState` from
the execution context after every step and writes it wholesale. It does not read-modify-write. Anything
derived from the execution context reconstructs correctly on each write and is safe to keep in
`RunState`. The staged route is **not** context-derived: it is written out-of-band by a control-server
goroutine while the step is still running. A route field written into `state.json` would be silently
obliterated by the next step-state write, destroying a route the runner had already acknowledged to the
agent.

So the route gets its own run-owned file with its own atomic writes and a single owner. It also avoids
forcing route fields through every state reconstruction and resume path.

### The artifacts

The agent writes a request; the runner seals a record. The design specifies both shapes:

```go
type State string // "staged" | "frozen"

// Request is the agent-written route-request.json. Strict-decoded, unknown
// fields rejected. Only these three fields exist.
type Request struct {
    Workflow string            `json:"workflow"`          // required, canonical name
    Params   map[string]string `json:"params,omitempty"`  // optional
    Handoff  string            `json:"handoff"`           // required, absolute or run-relative
}

// Sealed is the sidecar at <run-dir>/intake-route.json.
type Sealed struct {
    State        State             `json:"state"`
    ParentRunID  string            `json:"parent_run_id"`  // the intake run
    Workflow     string            `json:"workflow"`       // canonical name; display and audit only
    SourceRef    string            `json:"source_ref"`     // exact resolved reference; this is what launches
    Params       map[string]string `json:"params"`         // exactly as supplied; see normalization below
    HandoffPath  string            `json:"handoff_path"`   // sealed snapshot, never the agent's source
    StagedAt     string            `json:"staged_at"`
    FrozenAt     string            `json:"frozen_at,omitempty"`
}

type Store struct{ path string }

func (s *Store) Load() (*Sealed, error)
func (s *Store) Stage(*Sealed) error   // atomic replace
func (s *Store) Freeze() error         // atomic staged -> frozen
```

The sidecar lives at `<run-dir>/intake-route.json`.

### The two-phase staging contract — read this before designing the API

The design sketches `Stage(*Sealed)` and separately says the handoff copy happens *during validation*
while the staging write happens *under the caller's ordering lock*. Those two statements only reconcile
one way, and the design states it explicitly: *"the handoff copy performed during validation is written
to a temporary path and only published into `HandoffPath` by the staging write, so a submission that
loses the race leaves nothing behind."*

Concretely, the caller of this package needs **two separately callable phases**, because it will run
them on opposite sides of a mutex:

- **Phase 1 — validate and prepare (slow, no lock held).** Decode, resolve the workflow, check
  parameters, open and check the handoff, and copy the handoff bytes **from that same open handle** to a
  **temporary path** inside the run directory. Returns a prepared value carrying the sealed metadata,
  the temporary snapshot location, and a way to discard the temporary artifact.
- **Phase 2 — publish (fast, lock held).** Atomically publish the prepared value as the staged sidecar,
  moving or renaming the temporary snapshot into its final `HandoffPath` and writing
  `intake-route.json`. This phase performs no filesystem reads of agent-writable input and no catalog
  resolution.

Design the exported API so those phases are genuinely separate — for example `Validate` (or a
`Prepare`) returning a prepared route, and `Stage(prepared)` doing only the atomic publication — rather
than a single `Stage` that does both. The caller cannot hold the server mutex across phase 1's
filesystem work, and it cannot let phase 1 publish anything, so a combined call is unusable.

**Cleanup is part of the contract.** The temporary snapshot must be discardable without side effects,
and must be discarded when validation fails, when the caller abandons a prepared route (for instance
because a final eligibility recheck loses a race), and when publication itself fails. A prepared route
that is never published must leave the run directory exactly as it found it: no stray temporary file, no
change to any previously staged route.

`Freeze()` remains a standalone atomic `staged → frozen` transition on the published sidecar.

### Validation

`intakeroute.Validate` must be **transport-independent** so the launcher can reuse its invariant checks
without dragging in the control channel. The sequence:

1. Strict JSON decode with `DisallowUnknownFields`, under a size bound.
2. Workflow resolves through the normal catalog; capture the resolved source reference.
3. Target is not the intake workflow itself.
4. Every declared-required parameter present; no undeclared parameter supplied.
5. Handoff path is canonicalized, contained within the run directory, opens as a regular file, and is
   non-empty, under a size bound.

**Bounds:** the route request is read under a 64 KiB limit and the handoff under 1 MiB. Both are
generous for their purpose and small enough that a runaway write fails fast rather than filling the run
directory.

**Resolution reuses existing machinery.** Workflow resolution, version selection, and parameter
validation go through the existing catalog and discovery paths rather than being reimplemented against a
prompt-supplied name. `discovery.WorkflowEntry.SourcePath` (`internal/discovery/discovery.go:35`,
"exact selected workflow path") is where the exact resolved path comes from. Consequence: any workflow
the user could launch manually is routable, and workflows that never reference the handoff simply ignore
it.

**Parameter normalization.** `Params` records exactly the key/value pairs the agent supplied, after
validation confirms every required parameter is present and no undeclared parameter appears. Defaults
are **not** materialized and omitted optional parameters are **not** added, because the launch path runs
the same parameter-binding and default-filling code as a manual launch; pre-materializing them here
would diverge from that behavior the first time a default changes. Note that `Param.Required` is a
`*bool` in this codebase and `nil` means required by default.

**`SourceRef` is a reference, not a content snapshot.** For a project or user workflow it is the exact
versioned file path; for a builtin it is the `builtin:` reference. It pins *which definition is
selected*, so a newer sibling version is never chosen at launch. It does **not** pin the definition's
bytes: editing that same path between acceptance and launch is not detected, since content hashing is
out of scope for this change.

### Sealing

The handoff bytes are copied **immediately on successful validation**, from the same handle that was
validated, into a run-owned snapshot. `Sealed.HandoffPath` references that snapshot and never the
agent-written source. Editing the original afterward cannot change what launches, and there is no window
between validation and copying in which the validated bytes and the launched bytes could differ.

Per the two-phase contract above, that copy lands at a **temporary path** during validation and is
published into `HandoffPath` only by the staging write, atomically. A prepared-but-never-published
submission therefore leaves nothing behind.

### Threat model

These rules are ordinary robustness, not an adversarial threat model. The intake agent is not treated as
an attacker. Sealing addresses malformed and oversized input — strict decoding, bounded sizes, regular
file checks, containment within the run directory, bytes copied from the same opened handle, atomic
publication. It does not address symlink races or fsync-level durability guarantees, which are
explicitly out of scope.

### Scope boundary

This task delivers the package and its file-level behavior only. Wiring it to the control channel, the
`submit_route` message, eligibility, freeze-on-completion ordering, audit events, and the launcher are
delivered elsewhere in this change. `Freeze()` is exercised here as a store operation; the concurrency
rules governing *when* it is called are not this task's concern. Do not add route state to
`model.RunState`.

## Spec

From `specs/intake-route-submission/spec.md`:

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

> The "client receives an error" and "same session" phrasing describes the end-to-end experience. Your
> obligation is that validation returns a structured, actionable error naming the specific violation —
> the unresolved workflow, the missing or unexpected parameter, the decoding failure, the containment
> violation, the bound — and that a failed validation performs no write that disturbs a previously
> staged route.

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

## Test Plan

You MUST read `test-plan.md` for the full text of the obligation below.

**INT-003: Step-state writes do not clobber a staged route.** Boundary: `internal/runner` step-state
persistence, `internal/stateio`, `internal/intakeroute`, shared run directory. Setup: a run whose
control attempt has staged a route into the sidecar. Action: drive the runner through a step boundary so
`writeStepState` rebuilds and rewrites `RunState`. Assert: the sidecar still reads `staged` with
identical contents after the state write, and the rewritten `state.json` is well-formed and unaffected.
Execution: `internal/runner` package tests, `go test ./...`.

The test plan notes explicitly: *this is the specific regression the sidecar design exists to prevent.
If route state is ever moved into `RunState`, this test is what should fail.* Name and comment the test
so that intent survives.

The test plan deliberately leaves every `intakeroute.Validate` edge case to unit tests rather than
recording a separate integration obligation — unknown workflow, missing and undeclared parameters,
malformed JSON, unknown fields, uncontained or non-regular or empty handoff, oversized input,
self-routing. Cover them as table tests in the package. The design additionally calls for staging
idempotency, replacement, and a test proving `Stage` snapshots bytes such that mutating the source
afterward does not change `HandoffPath` contents.

## Done When

- `internal/intakeroute` exists with the `Request`, `Sealed`, `State`, and `Store` types described above,
  and `Load`, `Stage`, and `Freeze` operations using atomic replace.
- The API exposes the two staging phases as **separately callable** operations: a validate/prepare phase
  that does all filesystem and catalog work and returns a prepared route, and a publication phase that
  takes that prepared route and does nothing but the atomic staged-sidecar write. A caller can run the
  first without holding a lock and the second while holding one.
- A prepared route can be discarded without side effects, and discarding it — on validation failure, on
  caller abandonment, or on publication failure — leaves no temporary artifact and no change to any
  previously staged route. Tests cover the abandoned-prepared-route case explicitly.
- `Validate` is transport-independent — it takes the run directory, request bytes or path, and catalog
  access, and returns a structured error; it does not import the control channel or the CLI layer.
- Validation performs all five checks in order, under the 64 KiB request bound and 1 MiB handoff bound,
  and returns an error that names the specific violation.
- Workflow resolution goes through the existing catalog and discovery paths; the sealed record captures
  the exact resolved source reference, and a builtin target seals its `builtin:` reference.
- Parameters are recorded exactly as supplied; no defaults are materialized and no omitted optional
  parameters are added.
- The handoff bytes are copied from the same handle validation opened, to a temporary path, and are
  published into `HandoffPath` atomically as part of the staging write. Mutating the agent-written
  original afterwards does not change the snapshot.
- A failed validation leaves any previously staged route byte-identical, and leaves no temporary
  artifact behind.
- `Freeze` performs an atomic `staged → frozen` transition and records the freeze time; a frozen record
  is not re-staged by the store.
- Route state is stored only in `<run-dir>/intake-route.json` and never in `model.RunState`.
- INT-003 passes, with a test name and comment identifying it as the anti-clobbering regression guard.
- Table tests cover every validation edge case listed above, plus staging idempotency, replacement, and
  handoff-snapshot immutability.
- `make fmt`, `make lint`, and `make test` pass.
