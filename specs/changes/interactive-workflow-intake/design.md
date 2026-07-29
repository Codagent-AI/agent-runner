## Context

Agent Runner's default entry point requires the user to pick a workflow before any agent can help them pick one. This change adds an intake experience: an agent explores the problem with the user, they agree on a route, and Agent Runner launches the selected workflow as a fresh top-level run carrying a durable handoff.

The relevant existing machinery, all of which this design reuses rather than replaces:

- **Control channel** (`internal/control`). One Unix socket per run, credential rotated per step attempt. Three message types today: `complete_step`, `turn_committed`, `agent_call`. Requests are handled in per-connection goroutines under a server mutex.
- **Runner tools** (`model.RunnerTool`, `step.HasTool`). Steps opt into runner-owned integrations with `tools: [call_agent]`. Validation lives in `Step.validateTools`.
- **Adapter command grants** (`internal/cli`). `CompletionCommand.Valid()` requires argv to equal exactly `["step", "complete"]`; adapters turn that into an exact-string permission grant. The type comment states this "prevents adapters from accepting shell fragments supplied as a single opaque string."
- **Launch boundary** (`cmd/agent-runner/main.go`). The workflow runs in a goroutine while Bubble Tea owns the terminal. TUI-originated launches happen only after `p.Run()` returns, via `execSelfWithEnv`, which replaces the process. Explicit CLI runs bypass the switcher entirely.
- **Run finalization** (`runner.finalizeRun`). Closes the control server, releases the run lock, marks state completed, emits `run_end`, closes the audit log.
- **State persistence** (`runner.writeStepState`). Rebuilds a fresh `model.RunState` from the execution context after each step and writes it wholesale. It does not read-modify-write.
- **Interpolation** (`internal/textfmt`). `{{name}}` placeholders only; an unresolved reference is a hard error, not an empty substitution. Built-ins have the lowest precedence.
- **Hidden subcommands** (`main.go:307-312`). `step` and `internal` are dispatched before flag parsing.

## Goals / Non-Goals

**Goals:**

- Let an agent and user explore before committing to a workflow, from the New tab and from `agent-runner -i`.
- Give the agent a structured, authenticated, runner-validated way to name the workflow it and the user agreed on, with validation errors returned while the conversation is still live.
- Guarantee that the workflow definition validated at acceptance is the definition that runs.
- Carry a durable handoff into the launched run without changing any workflow's public parameter interface.
- Keep every existing invocation path byte-for-byte unchanged in behavior.

**Non-Goals:**

- Crash-safe launch retry. No preallocated child run ID, launch ledger, or claim protocol.
- Content hashing or drift rejection on the sealed workflow file.
- An adversarial filesystem threat model. The intake agent is not treated as an attacker.
- A runner-owned confirmation gate. Agreement in the conversation is sufficient.
- General-purpose `--cli`/`--model` overrides for arbitrary runs.
- Making step-level `cli`/`model` interpolable.

## Approach

### Component map

| Component | Change |
|---|---|
| `internal/intakeroute` (new) | Sidecar store, sealed-route record, transport-independent validation |
| `internal/control` | `submit_route` message type, route eligibility, freeze-on-completion |
| `internal/exec` | `submit_route` tool wiring, route handler, adapter command grants, run-scoped agent override |
| `internal/model` | `RunnerToolSubmitRoute`, `intake_handoff` built-in and reserved name, ctx fields, `RunState` provenance |
| `internal/cli` | `RunnerCommand` replacing `CompletionCommand`; adapters grant a list |
| `internal/runner` | Options for provenance and override; handoff copy in `PrepareRun`; restore on resume; shared `prepareFreshRun` service |
| `internal/prevalidate` | Recognize `intake_handoff`; reject reserved-name params and captures |
| `internal/listview` | "Plan with an agent" entry |
| `cmd/agent-runner` | `-i`/`--cli`/`--model`, `step submit-route`, `internal launch-intake-route` |
| `workflows/core/intake-v1.0.yaml` (new) | The intake workflow |

### The sidecar, and why route state is not in `RunState`

Route state lives in `<run-dir>/intake-route.json`, owned by a new `internal/intakeroute` package:

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

**Bounds:** the route request is read under a 64 KiB limit and the handoff under 1 MiB. Both are
generous for their purpose and small enough that a runaway write fails fast rather than filling the
run directory.

**Parameter normalization:** `Params` records exactly the key/value pairs the agent supplied, after
validation confirms every required parameter is present and no undeclared parameter appears. Defaults
are **not** materialized and omitted optional parameters are **not** added, because the launch path
runs the same parameter-binding and default-filling code as a manual launch, and pre-materializing
them here would diverge from that behavior the first time a default changes.

**`SourceRef` is a reference, not a content snapshot.** For a project or user workflow it is the exact
versioned file path; for a builtin it is the `builtin:` reference. It pins *which definition is
selected*, so a newer sibling version is never chosen at launch. It does **not** pin the definition's
bytes: editing that same path between acceptance and launch is not detected, since content hashing is
out of scope. The specifications state the guarantee in those terms.

The reason this is not a field on `RunState` is specific and worth stating for anyone tempted to simplify it later. `writeStepState` constructs a fresh `model.RunState` from the execution context after every step and writes it wholesale. Anything **derived from the execution context** reconstructs correctly on each write and is safe to keep in `RunState`. The staged route is **not** ctx-derived: it is written out-of-band by a control-server goroutine while the step is still running. Putting it in `RunState` would mean the next step-state write silently destroys an acknowledged, durable route.

This is also why intake *provenance* (parent run ID, handoff path) **is** kept in `RunState`: those values are seeded onto the execution context at `PrepareRun` and are therefore reconstructed on every write.

### Route submission path

The intake step opts in through the existing tools mechanism:

```yaml
- id: plan
  session: intake-planner
  mode: interactive
  tools: [submit_route]
```

**The tool declaration is necessary but not sufficient.** `tools:` is a public workflow field validated generically, so any project or user workflow could write `tools: [submit_route]`. If that alone conferred eligibility, and the post-run check launches any frozen route, the change would have quietly shipped general dynamic workflow chaining from arbitrary agents — an explicit non-goal. Eligibility therefore requires **runner-established intake identity** as well:

```go
routeEligible := step.HasTool(model.RunnerToolSubmitRoute) &&
    ctx.IsTopLevelWorkflow() &&                 // not reached as a sub-workflow
    isBuiltinIntakeWorkflow(ctx.WorkflowFile)   // the embedded core:intake, by reference
```

Static validation rejects the declaration outright on any step that cannot satisfy those conditions, so a user workflow fails at load rather than silently never working.

`ExecuteAgentStep` passes the result into `control.AttemptOptions` alongside a route handler, the same shape `RunnerToolCallAgent` uses today. When route-eligible, the child's environment gains `AGENT_RUNNER_ROUTE_REQUEST=<run-dir>/route-request.json` and `AGENT_RUNNER_INTAKE_HANDOFF=<run-dir>/intake-handoff.md`.

**The handoff path is runner-owned.** The agent is told where to write rather than choosing. Besides removing a class of validation, this is what makes exploration-only reportable: when no route request is ever submitted, the runner still knows where the handoff would be and can report it if the file exists and is non-empty. The route request still carries a `handoff` field, which validation requires to resolve to that same runner-owned path.

**The runner reads the request; the client never transmits it.** `agent-runner step submit-route` sends a payload-free `submit_route` control message. The runner then reads the request from the path it created and advertised. This is deliberate: the path cannot be redirected by the agent, size bounds are enforced at read time, and the control message stays payload-free so no new framing concerns arise.

Validation, in `intakeroute.Validate`, is transport-independent so the launcher can reuse its invariant checks:

1. Strict JSON decode with `DisallowUnknownFields`, under a size bound.
2. Workflow resolves through the normal catalog; capture `WorkflowEntry.SourcePath`.
3. Target is not the intake workflow itself.
4. Every declared-required parameter present; no undeclared parameter supplied.
5. Handoff path is canonicalized, contained within the run directory, opens as a regular file, and is non-empty, under a size bound.

On success, **`Stage` copies the handoff bytes immediately**, from the same handle that was validated, into a run-owned snapshot path. `Sealed.HandoffPath` references that snapshot and never the agent-written source. Editing the original afterward cannot change what launches, and there is no window between validation and copying.

Failures return an actionable message through `controlResponse.Error`, which `step submit-route` prints to stderr and exits nonzero on. The agent sees it in its own tool output and can correct the file and retry inside the same conversation. A failed submission leaves any previously staged route untouched.

### Freeze and completion ordering

`handleCompletion` gains one step, under the mutex it already holds, before it marks the completion accepted:

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

- **Completion with no staged route succeeds normally.** That is the exploration-only path: intake finishes, nothing launches.
- **A failed freeze rejects completion.** The runner must never acknowledge completion while the route is in an indeterminate state. The agent receives the error and can retry `step complete`.

**The linearization point must be stated explicitly, because "freeze is under the mutex" is not sufficient on its own.** Control requests are handled in per-connection goroutines, and the existing completion path deliberately releases the mutex to capture the durability checkpoint. If route submission validated *and staged* outside the mutex, a validated replacement could be written after a freeze had already run. So:

- Validation — decoding, catalog resolution, parameter checks, handoff reading and copying — happens **outside** the mutex, since it does filesystem work and must not block the server.
- The **final eligibility recheck and the `Stage` write** happen **inside** the same mutex that guards completion acceptance.

That yields exactly two possible outcomes for any interleaving: the submission stages and is later frozen, or it observes an accepted completion and is rejected. A submission can never be staged after a freeze. Note that the handoff copy performed during validation is written to a temporary path and only published into `HandoffPath` by the staging write, so a submission that loses the race leaves nothing behind.

### Frozen does not mean successful

Completion acceptance is explicitly an intermediate state. After the completion is delivered, `finishDirectCompletion` waits up to 30 seconds for semantic turn-durability evidence, and on failure the step fails and the run fails. So a run can end **failed** while its route is **frozen**.

Launching therefore requires all three of: the run returned success, its persisted state is marked completed, and the route is frozen. Checking `State == frozen` alone would launch a workflow off an intake turn that was never durably recorded.

When a route freezes and durability then fails, the frozen route is retained and nothing launches. The run is resumable, and because a frozen route is immutable, the resumed attempt retries completion against that same route; success then launches it. This keeps a decision the user actually made without acting on an unrecorded turn.

### Launch across the process boundary

Launching from inside the control handler would bypass `finalizeRun`, which closes the control server, releases the run lock, and closes the audit log. So the launch happens after the run returns.

**The check belongs to the run, not to the entry point.** The intake workflow is an ordinary embedded workflow, so `agent-runner core:intake` starts it too, and `builtin-workflows` requires that invocation to work. If the frozen-route check lived only in the `-i` path, starting intake by name would hold the whole conversation, freeze a route, and then silently discard it — the agent would tell the user it was launching something and nothing would happen. So the check sits on the shared path every top-level run returns through:

```
after any top-level run completes:
    if result != ResultSuccess:      return result   // durability failure, step failure, ...
    sealed := intakeroute.Load(<run-dir>)            // single stat; absent for every non-intake run
    if sealed == nil || sealed.State != frozen:      return result
    if !stateMarkedCompleted(<run-dir>):             return result
    appendLaunchAttempted(<run-dir>, sealed)
    exec agent-runner internal launch-intake-route <run-dir>/intake-route.json
```

The cost on ordinary runs is one comparison plus one stat of a file that does not exist. `-i` therefore stays a pure entry-point convenience: it decides how intake *starts*, never what intake *does*.

**Headless intake is rejected wherever it appears.** Intake is a conversation and needs a real terminal, so the check attaches to the workflow rather than to the flag: `--headless -i` and `--headless core:intake` are both refused, and so is any invocation of intake without a real stdin and stdout. This keeps the design consistent with the stated non-goal instead of accepting headless intake through the by-name path.

**The transition must not wait on the user.** The live TUI only sends itself an exit message when `quitOnDone` is set, which an ordinary by-name run does not set. Left alone, `agent-runner core:intake` would strand the user on a completed-run view until they dismissed it, while `-i` transitioned immediately. A successful intake run with a frozen route therefore signals the TUI to exit regardless of how it was started.

### Launch-attempt evidence after finalization

`finalizeRun` closes the audit logger before returning, and `Logger.Emit` silently discards events once closed. Since the launch is deliberately attempted after that return, `route_launch_attempted` cannot be emitted through the run's logger — it would vanish.

So launch evidence is appended through a standalone append against the run's audit file, opened and closed for that write alone, immediately before `exec`. If `exec` returns — meaning it failed, since a successful `exec` never returns — a `route_launch_failed` record is appended the same way. These entries follow `run_end` in the log, which the audit specification now explicitly permits.

Recording the attempt *before* `exec` rather than after is deliberate: after a successful `exec` this process no longer exists, so there is no later moment at which to write anything.

The subcommand takes **only** the absolute sidecar path. The complete launch plan lives in that one artifact, so nothing is inherited through the environment and nothing is re-derived.

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

**The launcher does not re-resolve the logical workflow name.** It validates the sealed artifact's invariants and then launches `Sealed.SourceRef` directly. That is the point of sealing the reference: `resolveWorkflowArg` deliberately rejects versioned names and selects the latest version, so routing the launch back through it would reintroduce exactly the drift this exists to prevent.

**`prepareFreshRun` is a shared service, not a shortcut.** `runner.PrepareRun` takes a parsed `*model.Workflow`, not a path, and the real launch path does considerably more before calling it: `LoadWorkflow`, `parseParams`/`matchParams`, **strict pre-validation for non-builtin workflows**, and **engine creation** for workflows declaring an `engine:` block. A launcher that called `PrepareRun` directly would skip pre-validation for project workflows and start engine-backed workflows without their engine — an intake-launched run would behave differently from the same workflow launched by hand.

So that sequence is extracted into one internal service used by **both** the ordinary CLI launch and the intake launcher:

```
prepareFreshRun(req) :=
    workflow := LoadWorkflow(req.SourceRef)
    params   := bindAndDefault(workflow, req.Params)
    if not builtin(req.SourceRef): prevalidate.Pipeline(..., Strict)
    engine   := engine.Create(workflow.Engine)      // when declared
    runner.PrepareRun(&workflow, params, Options{ engine, intake provenance, ... })
```

The difference between the two callers is only the front end: the CLI resolves a logical name into a reference first, while the launcher already has one. Extracting the service rather than reusing `discovery.WorkflowEntry` for launch validation also avoids a behavioral mismatch — discovery models the browser's view of a catalog and does not resolve exactly as the CLI launch path does.

A failure anywhere in `prepareFreshRun` exits nonzero reporting the cause and the sealed handoff path, and must not leave a partially created run directory behind.

The subcommand is dispatched in `run()` before flag parsing, alongside the existing `step` and `internal` handlers, and is excluded from help output and completions. It is hidden and unsupported, not a security boundary.

**Handoff copy ordering.** The launched run's directory does not exist until `PrepareRun` creates it, so the copy cannot precede the launch. `Options.IntakeHandoffSource` carries the sealed snapshot path in; `PrepareRun` copies it into the new session directory and sets `ctx.IntakeHandoff` to the destination. The launched run is then self-contained: deleting the intake run later does not break it.

### Built-in variable and provenance propagation

`{{intake_handoff}}` is added to `BuiltinVarsForStep`, with one deliberate departure from the surrounding code. That function currently omits empty values and returns `nil` for an empty map. `intake_handoff` must be set **unconditionally, including to the empty string**, because interpolation treats an unresolved reference as a hard error. If it were omitted on direct runs, every direct invocation of a workflow referencing it would fail. The implementation needs a comment saying so, because the surrounding lines model the opposite convention.

Since built-ins have the lowest precedence and would be shadowed by a same-named param or capture, `intake_handoff` is a **reserved name**: `Workflow.Validate` rejects a param of that name, step validation rejects a capture into it, and `internal/prevalidate` reports the same violation statically. Prevalidate's built-in set is currently the hardcoded pair `session_dir`/`step_id` and must gain the new name, or every prompt referencing it fails reference checking.

**Reservation must cover every capture sink, not just `capture:`.** Steps write captured variables through both `step.Capture` and a UI step's `OutcomeCapture`, but prevalidate's walk currently records only `step.Capture`. Enforcing the reserved name against `capture:` alone would leave `outcome_capture: intake_handoff` as a working way to shadow the sealed path.

Both `ctx.IntakeHandoff` and the parent run ID propagate through `NewLoopIterationContext` and the sub-workflow context constructor. Both constructors copy run-scoped fields by explicit assignment, so these are deliberate additions rather than something inherited automatically. They are persisted in `RunState` and restored by `PrepareResume`, which likewise copies an explicit field list.

### Run-scoped agent override

`--cli`/`--model` become `ctx.AgentOverride`, consulted in `resolveStepProfile` immediately after the existing step-level overrides:

```go
// existing
if step.Model != "" { resolved.Model = step.Model }
if step.CLI   != "" { resolved.CLI   = step.CLI }

// new, run-scoped, highest precedence
if ov := ctx.AgentOverride; ov != nil {
    if ov.Model != "" { resolved.Model = ov.Model }
    if ov.CLI   != "" { resolved.CLI   = ov.CLI }
}
```

Making step-level `cli`/`model` interpolable was rejected: `ParseWorkflow` calls `w.Validate(cli.KnownCLIs())` at load time, before any interpolation runs, so a placeholder would be rejected outright. Supporting it would mean changing loader validation, prevalidation, and probing.

The override is validated at flag-parse time, before any run is created, on two counts: the CLI must be in `cli.KnownCLIs()`, and it must support interactive steps. The second check matters because OpenCode is a *known* adapter that returns `InteractiveModeError`, so membership alone would accept `-i --cli opencode` and then fail once intake tried to run.

It is persisted in intake run state and restored by `PrepareResume`, so a resumed intake continues under the same CLI and model rather than silently reverting to the profile. And it is **confined to the intake run**: `handleLaunchIntakeRoute` neither reads it nor populates `Options.AgentOverride`, so the launched workflow resolves every agent through the ordinary step and profile rules.

### Adapter command grants

`CompletionCommand` becomes a general descriptor whose argv is derived from its kind, so kind and argv can never disagree:

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

`BuildArgsInput` carries `RunnerCommands []RunnerCommand`. Adapters iterate and grant each exactly, rather than special-casing a single field. `RunnerCommandCompleteStep` is supplied wherever `CompletionCommand` is supplied today; `RunnerCommandSubmitRoute` is supplied **only** for a route-eligible step. Adding a third runner command later touches no adapter.

The `turn_committed` hook command stays attached to the completion grant, since it is completion-specific.

### Entry points and flag handling

`-i` starts intake directly. `--cli` and `--model` require `-i`. `-i` is rejected with `--headless`, `--list`, `--resume`, `--inspect`, `--validate`, `--onboarding-from`, or a workflow positional argument, joining the existing mutual-exclusivity checks in `run()`.

The TTY check cannot go through `requireTTY`, which returns nil when `AGENT_RUNNER_NO_TUI=1` — precisely the variable `--headless` sets. Intake checks `isatty` on stdin and stdout directly.

In the TUI, the "Plan with an agent" entry renders above every group, is the initial cursor position, and is exempt from both the search filter and the `h` toggle. Selecting it emits a message that the switcher turns into `execSelfWithEnv([]string{liveRunImmediateAltScreenEnv + "=1"}, "-i")`, mirroring how `execStartRun` already works.

### Failure behavior

| Situation | Behavior |
|---|---|
| Request malformed, workflow unknown, params wrong, handoff bad, target is intake | Inline error to the agent; nothing staged; previous staged route intact |
| Submission from a non-route-eligible step | Rejected and audited; workflow state unchanged |
| Submission after accepted completion | Rejected and audited |
| Freeze fails during completion | Completion rejected; agent may retry `step complete` |
| Completion with no staged route | Success; nothing launches; runner-owned handoff path reported if the file is non-empty |
| Interrupted before completion, route staged | Staged route persists; resumed attempt may replace it |
| Durability fails after freeze | Step and run fail; frozen route retained; nothing launches; resume retries completion against the same immutable route, and success launches it |
| Killed after a successful run froze its route, before exec | Intake run is complete, so `--resume` opens inspect mode; sealed handoff preserved; user restarts intake |
| Sealed artifact fails invariant checks at launch | Launcher exits nonzero naming the violation; no run created |
| Load, parameter binding, pre-validation, or engine creation fails at launch | Launcher exits nonzero reporting the cause and the sealed handoff path; no partial run directory left behind |
| `exec` fails | Parent still alive; appends `route_launch_failed`; exits nonzero reporting the cause and the sealed handoff path |
| Intake invoked without a real terminal, by flag or by name | Rejected before any run is created |

## Decisions

**Runner reads the request rather than the client sending it.** Single source of truth for the path, no spoofing, bounds enforced at read time, and the control message stays payload-free.

**Stage copies the handoff immediately.** Validating and copying from the same open handle removes any window in which the validated bytes and the launched bytes could differ, and makes `Sealed.HandoffPath` unambiguously a snapshot.

**Freeze under the completion mutex, and reject completion if it fails.** Ordering the freeze before acceptance is what makes "the frozen route is what launches" true under concurrency. Rejecting on failure is what keeps the run from ever acknowledging completion with an indeterminate route.

**The launcher takes only a path.** The whole plan is one artifact. Nothing is inherited through the environment and the logical name is never re-resolved.

**Provenance is child-owned.** The child's run ID is generated inside `runner.Start` after the parent process has been replaced and its audit log closed, so the parent structurally cannot record it. The child records its parent instead.

**`intake_handoff` is reserved rather than merely defined.** Built-ins have the lowest precedence, so without reservation a workflow could declare a param of that name and silently receive its own value in place of the sealed path, defeating the feature's provenance guarantee.

## Risks / Trade-offs

**The hidden launch subcommand is a supported-looking surface.** `internal launch-intake-route` accepts a path to a JSON file that dictates what runs. It is not a security boundary and is not meant to be one: anything that can write that file already runs as the user. Mitigation is invariant validation at launch (state must be frozen, paths must resolve) so a malformed or partially written artifact fails loudly rather than launching something unexpected.

**Sealing the source path pins a definition that could be deleted.** If a project workflow file is removed between acceptance and launch, the launcher fails rather than falling back to a newer version. This is the intended trade: a loud failure beats silently running a different definition. The window is seconds.

**Modifying the completion path carries the most regression risk in the change.** `handleCompletion` is load-bearing for every interactive step in every workflow. The freeze is additive and no-ops when no route is staged, but the new rejection branch is a genuinely new failure mode for a path that previously always accepted. Tests must cover the no-route case explicitly to prove ordinary workflows are unaffected.

**The `RunnerCommand` rename touches the whole adapter registry.** `cli.KnownCLIs()` holds five adapters — claude, codex, copilot, cursor, and opencode — so the rename is mechanical but broad. The alternative, a parallel `RouteCommand` field, was rejected because each adapter would grow a second near-identical branch and a third runner command would repeat the duplication. OpenCode needs no route grant, since it declares `InteractiveModeError` and cannot run interactive steps at all, which is also why `-i --cli opencode` is rejected at flag-parse time rather than failing after a run has been created.

**A crash in the freeze-to-exec window loses the launch.** Accepted deliberately. The sealed handoff survives and the user restarts intake. Closing it would need a preallocated child run ID and a claim protocol, which is machinery this change deliberately excludes.

## Migration Plan

No data migration and no config migration. The intake agent reuses the existing `lead` profile, so custom profile sets keep working untouched.

Ordering matters in one place: `{{intake_handoff}}` must exist as a built-in, and prevalidate must recognize it, **before** any workflow prompt references it. Otherwise every run of the edited workflows fails reference checking. So the built-in and prevalidate changes land before the `core:define-change` and `spec-driven:simple-change` prompt edits.

Rollback is removing the entry point: without `-i` and the New tab entry, no intake run is ever created, no sidecar is ever written, and `{{intake_handoff}}` is empty everywhere. Every other path is unchanged by construction.

## Testing

- **`internal/intakeroute`**: table tests over validation — unknown workflow, missing and undeclared params, malformed JSON, unknown fields, handoff outside the run directory, non-regular file, empty file, oversized input, self-routing. Plus staging idempotency, replacement, and that `Stage` snapshots bytes such that mutating the source afterward does not change `HandoffPath` contents.
- **`internal/control`**: freeze-before-acknowledge ordering; submission after accepted completion rejected; completion with no staged route accepted; completion rejected when freeze fails; stale credential rejected; ineligible step rejected; both concurrent orderings of submission versus completion driven under a barrier; retry of an accepted request ID returns the original acknowledgement without staging twice.
- **`internal/runner`**: a step-state write after staging leaves the sidecar intact — the regression this design exists to prevent; provenance and agent override round-trip through `PrepareResume`; `PrepareRun` copies the handoff into the new session directory; `prepareFreshRun` refuses a non-builtin target that fails strict pre-validation and creates the engine for an engine-backed target; a launch-preparation failure leaves no run directory behind.
- **Launch gating**: a frozen route on a failed run does not launch; a durability failure after freeze retains the route and launches nothing; a resumed attempt that completes successfully launches the original frozen route.
- **`internal/model`**: `intake_handoff` present and empty on a direct run; reserved-name rejection for params, `capture`, and `outcome_capture`.
- **`internal/exec`**: override precedence command > step > profile; `submit_route` grant present only for a route-eligible step; a user workflow declaring the tool is rejected, and intake reached as a sub-workflow is not eligible.
- **`internal/cli`**: the exact grant string each of the five adapters emits for a route-eligible step, and its absence otherwise.
- **`cmd/agent-runner`**: flag rejection matrix for `-i`, including `--cli` naming an adapter that rejects interactive steps; TTY check not bypassed by `AGENT_RUNNER_NO_TUI`, for both `-i` and by-name invocation; launcher rejects a non-frozen or invariant-violating artifact; launch-attempt and launch-failure records are appended after `run_end`.
- **End to end**: an intake run that stages, freezes, and launches, asserting the launched run uses the sealed `SourceRef` even when a newer version of the same logical workflow is present, that its first agent session is fresh, and that its handoff survives deletion of the intake run.
