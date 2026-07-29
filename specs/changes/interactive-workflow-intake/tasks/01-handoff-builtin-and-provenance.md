# Task: `intake_handoff` built-in, reserved name, and intake run provenance

## Goal

Make `{{intake_handoff}}` a first-class built-in template variable that is defined on **every** run,
reserve its name against workflow-supplied data, teach pre-validation about it, and plumb intake
provenance (parent run ID and handoff path) through run preparation, state persistence, and resume.

This is the foundation the rest of the change stands on. The runner cannot ship a prompt referencing
`{{intake_handoff}}` until the built-in exists and pre-validation recognizes it, because interpolation
treats an unresolved reference as a hard error rather than an empty substitution. Delivering this first
means every later piece can assume the variable resolves everywhere.

## Background

### Why the variable rather than a parameter

Carrying the intake handoff as a declared workflow parameter would leak it into every target workflow's
public interface and would require editing each workflow before intake could reach it. A built-in keeps
the interface clean. But built-ins have the **lowest** interpolation precedence, so a workflow declaring
a parameter or capture of the same name would silently shadow the sealed path and defeat the provenance
guarantee the whole feature rests on. Hence the name is reserved as well as defined.

### The deliberate departure from existing built-in convention

`internal/model/context.go` currently implements built-ins like this:

```go
func (c *ExecutionContext) BuiltinVarsForStep(stepID string) map[string]string {
	m := make(map[string]string)
	if c.SessionDir != "" {
		m["session_dir"] = c.SessionDir
	}
	if stepID != "" {
		m["step_id"] = stepID
	}
	if len(m) == 0 {
		return nil
	}
	return m
}
```

Every existing built-in **omits itself when empty**, and the function returns `nil` for an empty map.
`intake_handoff` must break that convention: it is set unconditionally, **including to the empty
string**. If it were omitted on direct runs, every direct invocation of a workflow referencing it would
fail reference checking and interpolation. Add a comment at the site saying exactly this, because the
surrounding lines model the opposite convention. This also means the function can no longer return
`nil` for the "nothing set" case.

### Paths and current shape

- `internal/model/context.go` — `ExecutionContext` and `BuiltinVarsForStep`, plus the three context
  constructors: `NewRootContext(opts *RootContextOptions)` at line 151, `NewLoopIterationContext` at
  line 241, and `NewSubWorkflowContext` at line 322. All three copy run-scoped fields by **explicit
  assignment**, so the handoff path and the intake parent run ID must be added deliberately to each;
  nothing is inherited automatically. `NewRootContext` is where run preparation seeds them; the other
  two are what make nested and looped steps in a launched workflow able to reference
  `{{intake_handoff}}`.
- `internal/model/step.go` — despite the file name, this is where `Workflow` (line 658) and
  `Workflow.Validate` (line 707) live, alongside `Step` validation. There is no `workflow.go`.
  Reserved-name rejection belongs in both `Workflow.Validate` and step validation. `Step.validateTools`
  at line 438 shows the established shape for this kind of check.
- `internal/prevalidate/pipeline.go:669` — the known built-in set is currently the hardcoded literal
  `map[string]string{"session_dir": "", "step_id": ""}`. It must gain `intake_handoff`.
- `internal/runner/runner.go:38` — `Options`. Add fields carrying the intake handoff **source** path and
  the intake parent run ID.
- `internal/runner/runner.go` — `PrepareRun` creates the session directory; `PrepareResume` restores
  state by copying an **explicit field list**, so restoring provenance is a deliberate addition.
- `internal/model/state.go:73` — `RunState`. Gains persisted intake provenance.
- `internal/runner/runner.go:824` — `writeStepState` rebuilds a fresh `model.RunState` from the
  execution context after every step and writes it wholesale, without read-modify-write. Anything
  **derived from the execution context** reconstructs correctly on each write and is therefore safe to
  keep in `RunState`. Intake provenance qualifies: it is seeded onto the execution context at
  `PrepareRun`. (Route submission state does not qualify and belongs elsewhere; it is not part of this
  task.)

### Handoff copy ordering

The launched run's directory does not exist until `PrepareRun` creates it, so a handoff cannot be
copied into place before run preparation. `Options` carries the **source** snapshot path in;
`PrepareRun` copies it into the new session directory and sets the execution context's handoff field to
the **destination**. The launched run is then self-contained: deleting the originating intake run later
must not break it.

### Reservation must cover every capture sink

Steps write captured variables through **both** `step.Capture` and a UI step's `OutcomeCapture`.
`internal/prevalidate`'s walk currently records only `step.Capture`. Enforcing the reserved name against
`capture:` alone would leave `outcome_capture: intake_handoff` as a working way to shadow the sealed
path. Both sinks must reject the name, at model validation and statically in pre-validation.

## Spec

From `specs/builtin-vars/spec.md`:

### Requirement: intake_handoff built-in variable

The runner SHALL expose `{{intake_handoff}}` as a built-in template variable in every run, without exception. Its value SHALL be the absolute path of the sealed handoff when the run was launched from intake, and the empty string otherwise. Unlike other built-ins, it SHALL be present even when its value is empty, so that a workflow referencing it never fails interpolation on a directly invoked run. Its value SHALL be preserved across resume, so a resumed run sees the same value its original invocation had.

#### Scenario: Intake-launched run resolves the sealed path
- **WHEN** a run launched from intake interpolates `{{intake_handoff}}`
- **THEN** the runner replaces it with the absolute path of that run's sealed handoff

#### Scenario: Direct run resolves to empty
- **WHEN** a run invoked directly from the CLI or the workflow browser interpolates `{{intake_handoff}}`
- **THEN** the runner replaces it with the empty string and interpolation succeeds

#### Scenario: Reference does not fail on a direct run
- **WHEN** a workflow prompt references `{{intake_handoff}}` and the workflow is invoked directly
- **THEN** the step runs without an unresolved-variable failure

#### Scenario: Resumed intake-launched run keeps its handoff
- **WHEN** a run launched from intake is interrupted and later resumed, and a step interpolates `{{intake_handoff}}`
- **THEN** it resolves to the same sealed handoff path it had before the interruption

#### Scenario: Resumed direct run stays empty
- **WHEN** a directly invoked run is resumed and a step interpolates `{{intake_handoff}}`
- **THEN** it resolves to the empty string

### Requirement: Built-in precedence

Built-in variables have the **lowest** interpolation precedence. A workflow `params` entry or a captured variable with the same name as a built-in SHALL shadow the built-in.

`intake_handoff` is exempt: it is a reserved name. A workflow SHALL NOT declare a parameter named `intake_handoff`, and a step SHALL NOT capture into that name through **any** capture sink, including both ordinary output capture and UI-step outcome capture. Such a workflow SHALL be rejected, so the sealed handoff path can never be shadowed by workflow-supplied data.

#### Scenario: Param shadows built-in
- **WHEN** a workflow declares `params: [step_id]` and a caller passes `step_id: custom-value`
- **THEN** `{{step_id}}` in that step resolves to `custom-value`, not the actual step ID

#### Scenario: Captured variable shadows built-in
- **WHEN** a prior step captures output into a variable named `session_dir`
- **THEN** `{{session_dir}}` in subsequent steps resolves to the captured value, not the session directory path

#### Scenario: Param named intake_handoff is rejected
- **WHEN** a workflow declares a parameter named `intake_handoff`
- **THEN** the workflow is rejected with an error stating the name is reserved

#### Scenario: Capture named intake_handoff is rejected
- **WHEN** a step captures its output into a variable named `intake_handoff`
- **THEN** the workflow is rejected with an error stating the name is reserved

#### Scenario: UI outcome capture named intake_handoff is rejected
- **WHEN** a UI step captures its outcome into a variable named `intake_handoff`
- **THEN** the workflow is rejected with an error stating the name is reserved

From `specs/workflow-pre-validation/spec.md`:

### Requirement: Pre-validation pipeline scope

The requirement is a nine-item list describing the full static graph analysis, most of which is
unchanged. Two items change. Item 2 gains a reserved-name clause and item 4 gains an explicit statement
about empty-valued built-ins:

> 2. Per-file constraint validation: `skip_if` not on the first step in scope, `break_if` only inside a loop body, sessions block well-formed, named-session references resolve in scope, and no workflow parameter or capture uses a reserved built-in name.
>
> 4. `{{var}}` reference checks: every interpolated reference in step prompts, commands, sub-workflow paths, and parameter values MUST refer to a workflow parameter visible at that scope, a known built-in variable, or a capture variable produced by an earlier step on a control-flow path that reaches the reference. The known built-in set SHALL include every variable the runner exposes at runtime, including those whose value may be empty.

#### Scenario: Reference to the intake handoff built-in passes
- **WHEN** a workflow prompt references `{{intake_handoff}}` and the workflow declares no parameter of that name
- **THEN** pre-validation accepts the reference as a known built-in rather than reporting an undefined variable

#### Scenario: Reserved built-in name used as a parameter fails
- **WHEN** a workflow declares a parameter named `intake_handoff`
- **THEN** pre-validation fails with an error stating the name is reserved

From `specs/resume-by-session-id/spec.md`:

### Requirement: Intake provenance survives resume

Resuming a run SHALL restore the intake provenance the run was created with. A run launched from intake SHALL resume with its intake parent run ID and its sealed handoff path intact. A run invoked directly SHALL resume with no intake provenance. Resume SHALL NOT infer, re-derive, or discard provenance based on how the resume itself was invoked.

#### Scenario: Resumed intake-launched run restores its handoff
- **WHEN** a run launched from intake is interrupted and resumed with `--resume <id>`
- **THEN** the resumed run's steps see the same sealed handoff path they saw before the interruption

#### Scenario: Resumed intake-launched run restores its parent
- **WHEN** a run launched from intake is resumed
- **THEN** its recorded intake parent run ID is unchanged

#### Scenario: Resumed direct run has no intake provenance
- **WHEN** a directly invoked run is resumed
- **THEN** it carries no intake parent and its handoff value remains empty

#### Scenario: Resumed intake run restores its staged route
- **WHEN** an intake run that staged a route without completing its step is resumed
- **THEN** the staged route is still present and can be replaced by the resumed attempt

> Note on the last scenario: staged-route persistence is provided by run-owned storage independent of
> `RunState`, which is delivered elsewhere in this change. Your responsibility here is that `RunState`
> and `PrepareResume` do **not** own, overwrite, or discard route state, and that the provenance fields
> you add round-trip correctly. Do not put route state into `RunState`.

## Test Plan

You MUST read `test-plan.md` for the full text of the obligations below.

**INT-004: Handoff built-in resolves through validation and runtime.** Boundary: `internal/prevalidate`,
`internal/loader`, `internal/model` built-ins, runtime interpolation. Add fixture workflows under
`testdata/` — one whose prompt references `{{intake_handoff}}`, one declaring a parameter of that name,
one capturing into that name. Pre-validate and run each fixture, both as a direct run and as a run
prepared with intake provenance. Assert: the referencing workflow pre-validates and interpolates to the
sealed path when prepared with a handoff and to the empty string when invoked directly, with no
unresolved-variable failure; the parameter and capture fixtures are rejected at load and reported by
pre-validation as a reserved name. Cover the UI-step `outcome_capture` sink too. Execution:
`internal/prevalidate` and `internal/runner` package tests, `go test ./...`.

**INT-005: Handoff copy and provenance round-trip** — the handoff-copy and parent-provenance portions.
Boundary: `internal/runner` run preparation and resume, `internal/stateio`, real filesystem. Setup:
`Options` carrying an intake handoff source and an intake parent run ID. Action: `PrepareRun`, then
interrupt and `PrepareResume`. Assert: the handoff is copied into the new session directory and
`{{intake_handoff}}` addresses **the copy rather than the source**; the copy survives deletion of the
source; the resumed run restores the same handoff path and parent run ID; the launched run's persisted
state records its own run ID and the workflow version it executed; a directly prepared run resumes with
no parent and an empty handoff. Execution: `internal/runner` package tests, `go test ./...`.

Unit-level coverage stays a TDD decision, but the design calls out these cases specifically:
`intake_handoff` present and empty on a direct run, and reserved-name rejection for params, `capture`,
and `outcome_capture` (`internal/model`).

## Done When

- `BuiltinVarsForStep` returns `intake_handoff` on every call, empty string included, with a comment
  explaining why it departs from the surrounding omit-when-empty convention.
- `ExecutionContext` carries the intake handoff path and intake parent run ID, and both propagate
  through `NewLoopIterationContext` and the sub-workflow context constructor by explicit assignment, so
  nested and looped steps in a launched workflow resolve `{{intake_handoff}}`.
- `Workflow.Validate` rejects a parameter named `intake_handoff`; step validation rejects a capture into
  that name through both `capture` and UI `outcome_capture`. Errors state that the name is reserved.
- `internal/prevalidate` recognizes `intake_handoff` as a known built-in and reports the same
  reserved-name violations statically, walking both capture sinks.
- `runner.Options` accepts an intake handoff source path and an intake parent run ID; `PrepareRun`
  copies the handoff into the new session directory and seeds the execution context with the
  destination path; `RunState` persists both; `PrepareResume` restores both from its explicit field list.
- Route state is **not** added to `RunState`.
- INT-004 passes; the handoff-copy and parent-provenance assertions of INT-005 pass.
- No workflow YAML in `workflows/` references `{{intake_handoff}}` yet — this task only makes it safe to
  do so.
- Every existing run path is unchanged in behavior: direct, headless, and browser-launched runs — and
  resumes of those direct-origin runs — all see an empty `{{intake_handoff}}` and no intake parent. A
  run prepared **with** intake provenance keeps its handoff path and parent run ID across resume; resume
  never erases, re-derives, or infers provenance from how the resume itself was invoked.
- `make fmt`, `make lint`, and `make test` pass.
