## Context

Autonomous agent steps get their backend only from the user setting `autonomous_backend`
(`usersettings.AutonomousBackend`: `headless`, `interactive`, `interactive-claude`). The runner copies the
setting into `model.ExecutionContext.AutonomousBackend` (`internal/runner/runner.go`), and child contexts
inherit it. The backend is chosen in one place, `resolveInvocationContext` in `internal/exec/agent.go`,
which checks in this order:

1. External-user mode: autonomous steps run headless, all other steps use the external-user context.
2. A mode other than autonomous gives the interactive context.
3. `capture` forces headless.
4. The backend setting is checked. `interactive-claude` counts as interactive only when the CLI is
   `claude`.
5. If stdin is a TTY (`isStdinTerminal`, a test seam), the step runs autonomous-interactive. Otherwise
   it logs a warning and runs headless.

Two callers use it:
- `ExecuteAgentStep` (`agent.go:194`), followed by `validateAgentInvocationContext`, which fails the step
  through `emitAgentFailure`.
- `ResolveAgentInvocationContext` (`agent.go:536`), used by `dispatch.go:81` only to tell
  `PrepareStepHook` whether the step will take over the terminal.

Steps are validated at load time by `Step.validateFieldConstraints` in `internal/model/step.go`. Agent
steps are those with a prompt or an agent (`isAgentContext`). `validateAgentAdapterFields` already rejects
`cli`/`model` on UI and script steps, and `validateAgentOnlyField` rejects agent-only fields elsewhere.
A step's mode comes from `resolveModeFromProfile`: the step's `mode`, then the profile's `default_mode`,
then interactive.

The specs (`specs/step-model`, `specs/cli-adapter`, `specs/external-user-mode`) define the new behavior:
- a per-step `autonomous_backend` field that only autonomous agent steps may use;
- a step-required interactive backend that fails instead of quietly running headless when there is no
  TTY or the run is in external-user mode;
- load-time rejection of `capture` combined with an interactive step backend.

## Goals / Non-Goals

**Goals:**
- Let a workflow step choose its own autonomous backend, overriding the user setting for that step only.
- Make a step-required interactive backend a guarantee: the step runs autonomous-interactive or fails
  before the agent process starts.
- Keep current behavior for every step that doesn't declare the field.

**Non-Goals:**
- Changing the user settings file, the settings editor, or the run-state and audit schemas.
- Inheriting a backend default through workflows, groups, loops, or sub-workflows.
- Capture support for autonomous-interactive steps.
- Detecting native sub-agents aborted under the headless backend.
- Editing the orchestrated implement-change workflow, which is still in open PR #209.

## Approach

### 1. Model: field and load-time validation (`internal/model/step.go`)

Add the field to `Step`:

```go
AutonomousBackend string `yaml:"autonomous_backend,omitempty" json:"autonomous_backend,omitempty"`
```

Add the allowed values to the model package. It must not import `usersettings`, so the values are
declared here:

```go
// AutonomousBackendValues lists the values accepted by the step-level
// autonomous_backend field. They mirror usersettings.AutonomousBackend.
var AutonomousBackendValues = []string{"headless", "interactive", "interactive-claude"}
```

Add `validateAutonomousBackendField(isAgent, isScript, isUI bool) error`. Call it from
`validateFieldConstraints` next to `validateAgentAdapterFields`. When `s.AutonomousBackend == ""` it does
nothing. Otherwise it checks these cases in order:

| Condition | Error |
|-----------|-------|
| `isUI` / `isScript` / `!isAgent` (shell, sub-workflow, group, loop steps have no prompt/agent) | `"autonomous_backend" is only allowed on agent steps` |
| value not in `AutonomousBackendValues` | `invalid autonomous_backend %q (valid values: headless, interactive, interactive-claude)` |
| `s.Mode == ModeInteractive` | `"autonomous_backend" requires autonomous mode` |
| `s.Capture != ""` and value is `interactive` or `interactive-claude` | `"capture" cannot be combined with an interactive "autonomous_backend"` |

A shell step with `command:` and no prompt is not an agent context, so the `!isAgent` check covers it.
The same check rejects sub-workflow, group, and loop steps. Their own child steps are still validated
recursively, as now. An explicit `mode: ui` is caught by `isUI` before the agent check. The
wrong-step-type errors all use one message ("only allowed on agent steps"). That matches
`validateAgentOnlyField`, and the step-model scenarios only need the error to say the field is valid
only on agent steps.

### 2. Exec: backend resolution (`internal/exec/agent.go`)

Replace `resolveInvocationContext(mode, ctx, cliName, hasCapture, log)` with:

```go
func resolveInvocationContext(
	step *model.Step, mode model.StepMode, ctx *model.ExecutionContext, cliName string, log Logger,
) (cli.InvocationContext, error)
```

Steps run in this order. The changes from today are the step-backend error in step 1 and the
step-required branch in step 5:

1. `mode != ModeAutonomous`:
   - If `step.AutonomousBackend != ""`, return an error: ``"autonomous_backend" requires autonomous mode
     (step resolved to <mode>)``. Load-time validation cannot catch this case, because the mode can come
     from the profile's `default_mode` at run time.
   - Otherwise keep today's result: external-user context or interactive.
2. Effective backend: `step.AutonomousBackend` if set, otherwise `ctx.AutonomousBackend`. The request is
   `stepRequired := step.AutonomousBackend != ""`.
3. `wantsInteractive`: same switch as today on the effective backend. `interactive-claude` counts as
   interactive only when `cliName == "claude"`.
4. `!wantsInteractive`: return `ContextAutonomousHeadless`, with no error.
5. `stepRequired`:
   - If `ctx.ExternalUser != nil`, return an error: `autonomous_backend "<v>" requires the
     autonomous-interactive backend, which is not supported in external-user mode`.
   - If `step.Capture != ""`, return an error. Load-time validation makes this case unreachable; the
     check stays as a defensive guard.
   - If `!isStdinTerminal()`, return an error: `autonomous_backend "<v>" requires the
     autonomous-interactive backend, which needs a TTY (stdin is not a terminal)`.
   - Otherwise return `ContextAutonomousInteractive`.
6. Inherited request: keep today's chain unchanged. External-user mode runs headless, capture runs
   headless, and no TTY logs a warning and runs headless. Otherwise the step runs interactive.

Today the external-user check comes first. It moves inside the mode branches (step 1 and steps 5/6),
which gives the same results for every input that has no step field.

**Caller `ExecuteAgentStep`.** On a resolution error, call `emitAgentFailure(ctx, prefix, startTime,
string(mode), step, err.Error(), log)` and return `OutcomeFailed, nil`. This is the same path
`validateAgentInvocationContext` failures take. The step fails before `buildStepInvocation`, before
`emitAgentStart`, and before any process starts. `emitAgentFailure` prints the reason as `agent-runner:
step "<id>": …` and records a failed step-end audit event with usage `not invoked`. That satisfies "names
the step and the requested backend". Since the result is an ordinary `OutcomeFailed`,
`continue_on_failure` and the rest of the runner's step-failure handling apply unchanged.

**Caller `ResolveAgentInvocationContext`.** Keep its exported signature. When resolution fails, return
`ContextAutonomousHeadless`, matching how it already handles profile-resolution errors. The step will fail
without starting a process, so `PrepareStepHook` must not hand the terminal over.

Because every later piece reads the returned context, nothing else changes:
- the autonomy system-prompt preamble
- the disallowed-tools list (`AskUserQuestion`)
- the autonomous permission flags
- terminal handoff
- control-channel setup

### 3. Docs

- `docs/sessions-and-modes.md`, "Autonomous Agent Steps": document `autonomous_backend`, its values, the
  order in which step field, user setting, and default win, the fail-instead-of-fallback rule, and the
  `capture` restriction.
- `docs/agent-profiles.md`, settings table row for `autonomous_backend`: note that a step can override it.
- `docs/external-user-mode.md`: note that steps requiring the interactive backend fail in this mode.

### Data flow

```
YAML step ── Step.Validate (field/value/mode/capture checks) ── load error?
   │
   ▼ run time
ExecuteAgentStep
   profile → mode → cliName
   resolveInvocationContext(step, mode, ctx, cliName)
        step.AutonomousBackend ─┐ (step-required: fail if unmet)
        ctx.AutonomousBackend ──┴→ effective backend → context | error
   error → emitAgentFailure → OutcomeFailed (no process)
   context → buildStepInvocation (prompt preamble, flags) → spawn
```

## Decisions

- **The field is a plain `string` on `Step`, not a type shared with `usersettings`.** The architecture
  notes keep `model` free of service packages. A parity test in `internal/usersettings` asserts that
  `model.AutonomousBackendValues` matches the `usersettings.Backend*` constants, so the two lists cannot
  drift. Alternative: move the type into `model` and alias it from `usersettings`. That touches the
  settings code and editor for no behavioral gain.
- **Resolution returns `(context, error)`.** It doesn't use a separate preflight function. Keeping all
  backend logic in one function avoids two code paths that could disagree about when a request is
  required. The function is unexported and has one exported wrapper, so the signature change is
  contained.
- **Run-time mode mismatch fails for every field value.** A step with `autonomous_backend: headless`
  whose profile makes it interactive also fails. The declaration contradicts the resolved mode, and
  failing is consistent with the spec ("requires autonomous mode").
- **No new audit fields.** The failure reason is in the existing step-end `error` field, and the chosen
  context is already reported through the existing step-start data and `IsHeadless`. Adding a
  "backend source" field would change the audit schema, which is out of scope.

## Risks / Trade-offs

- **Unattended runners can't complete steps that adopt the field.** This applies to the Agent Factory and
  to CI runs without a TTY. That is intended: a clear failure is better than a step that silently didn't
  finish. Workflow authors must weigh it before adding the field to built-in workflows. Doing so is out
  of scope here.
- **The order of the external-user check changes.** Any regression would show up in existing
  `resolveInvocationContext` tests. The new tests cover every combination of mode, external user,
  capture, backend source, CLI, and TTY, using the `isStdinTerminal` seam.
- **Two lists of allowed values.** The parity test covers this.

## Testing

Write the tests first, next to each package:
- `internal/model/step_test.go`: table tests, one per load-time scenario in `specs/step-model`. Cover
  valid values on autonomous agent steps, an invalid value, the field on shell, script, UI, sub-workflow,
  group, and loop steps, `mode: interactive`, `capture` with each value, and the field on a step that
  omits `mode`, which passes validation.
- `internal/usersettings/settings_test.go`: parity between `model.AutonomousBackendValues` and the
  `Backend*` constants.
- `internal/exec/agent_test.go`: a table test of `resolveInvocationContext` over mode, step field, user
  setting, CLI, TTY, external user, and capture, including every existing case unchanged. Add
  `ExecuteAgentStep` tests showing that a step-required interactive request without a TTY, and one in
  external-user mode, fail with `OutcomeFailed`. The process runner must never be invoked, and the
  logged error must name the step and the backend. Add a test where the profile's `default_mode` is
  interactive. Add a positive `ExecuteAgentStep` test with a TTY (seam), user setting `headless`, and step
  `autonomous_backend: interactive`. It asserts that the adapter receives `ContextAutonomousInteractive`
  and that the built args carry the autonomy preamble and autonomous permission flags. Also test that `continue_on_failure` lets the run continue.
- `cmd/agent-runner/smoke_interactive_integration_test.go`: one PTY workflow test (test-plan INT-002).
  A step-level `autonomous_backend: interactive` overrides a `headless` setting and completes through
  the fake CLI's control-channel completion.
- `internal/exec/dispatch_test.go`: `ResolveAgentInvocationContext` returns headless for a request that
  cannot be met, so `PrepareStepHook` gets `false`.

## Migration Plan

The field is additive and optional, so there is no migration. Workflows and settings files without the
field behave exactly as before. To roll back, remove the field. Workflows that declare it would then fail
to load only if the loader rejected unknown keys; it doesn't today, so it would silently ignore the
field.
