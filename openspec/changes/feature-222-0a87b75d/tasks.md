- [ ] Implement the per-step `autonomous_backend` field described by these files using TDD, as `CLAUDE.md` requires. Satisfy every spec scenario and every `INT-*` / `E2E-*` obligation in the test plan.

  **Model (`internal/model/step.go`)**
  - Add `AutonomousBackend string` with tags `yaml:"autonomous_backend,omitempty" json:"autonomous_backend,omitempty"` to `Step`.
  - Add the exported `AutonomousBackendValues` slice (`headless`, `interactive`, `interactive-claude`). Do not import `usersettings`.
  - Add `validateAutonomousBackendField(isAgent, isScript, isUI bool)` and call it from `validateFieldConstraints` beside `validateAgentAdapterFields`. It checks these cases, in order:
    1. Not an agent step (UI, script, shell, sub-workflow, group, loop): reject with `"autonomous_backend" is only allowed on agent steps`.
    2. Invalid value: reject with an error naming the value and listing the allowed values.
    3. `mode: interactive`: reject with `"autonomous_backend" requires autonomous mode`.
    4. `capture` together with `interactive` or `interactive-claude`: reject with `"capture" cannot be combined with an interactive "autonomous_backend"`.

  **Resolution (`internal/exec/agent.go`)**
  - Change `resolveInvocationContext` to `(step, mode, ctx, cliName, log) (cli.InvocationContext, error)`, using the order in `design.md`:
    1. If the resolved mode isn't autonomous and the step sets the field, return an error ("requires autonomous mode"). Otherwise keep today's external-user and interactive results.
    2. The effective backend is the step's value, then the user setting, then headless. Requests from the step's field are step-required.
    3. `interactive-claude` counts as interactive only for the `claude` CLI. When it doesn't, the step runs headless without an error.
    4. A step-required interactive request returns an error, never a headless fallback, when the run is in external-user mode, the step has `capture` (defensive), or there's no TTY (`isStdinTerminal`). Each error message names the requested backend and the reason.
    5. Inherited requests keep today's chain exactly: external-user headless, capture headless, and a warning plus headless with no TTY.
  - In `ExecuteAgentStep`, on a resolution error, call `emitAgentFailure` and return `OutcomeFailed, nil` before `buildStepInvocation` and `emitAgentStart`. No process may start.
  - `ResolveAgentInvocationContext` keeps its signature and returns `ContextAutonomousHeadless` when resolution fails, so `PrepareStepHook` doesn't hand the terminal over.

  **Docs**
  - `docs/sessions-and-modes.md` ("Autonomous Agent Steps"): the field, its values, the precedence order, failing instead of falling back for step-level interactive requests, and the `capture` restriction.
  - `docs/agent-profiles.md`: in the settings table's `autonomous_backend` row, note that a step can override it.
  - `docs/external-user-mode.md`: steps that require the interactive backend fail in this mode.

  **Tests**
  - Add the unit tests listed under "Testing" in `design.md`:
    - `internal/model/step_test.go`: validation tables.
    - `internal/usersettings/settings_test.go`: parity between `model.AutonomousBackendValues` and the `Backend*` constants.
    - `internal/exec/agent_test.go`: a full resolution matrix (existing cases unchanged), failure-before-launch tests including profile `default_mode: interactive`, and a positive `ExecuteAgentStep` override test.
    - `internal/exec/dispatch_test.go`: the `PrepareStepHook` handoff value.
  - Add INT-001, E2E-001, and E2E-002 as testscript archives under `cmd/agent-runner/testdata/scripts/`, using a stub `bin/claude` and an isolated `HOME`.
  - Add INT-002 as a new PTY test in `cmd/agent-runner/smoke_interactive_integration_test.go`, reusing the existing fixture helpers.

  Do not change the user settings file format, the settings editor, the run-state and audit schemas, or any built-in workflow YAML under `workflows/`. Finish with `make fmt`, `make test`, and `make lint` passing.

  Source files:
  - [proposal.md](proposal.md)
  - [specs/step-model/spec.md](specs/step-model/spec.md)
  - [specs/cli-adapter/spec.md](specs/cli-adapter/spec.md)
  - [specs/external-user-mode/spec.md](specs/external-user-mode/spec.md)
  - [design.md](design.md)
  - [test-plan.md](test-plan.md)
  - [decisions.md](decisions.md)
