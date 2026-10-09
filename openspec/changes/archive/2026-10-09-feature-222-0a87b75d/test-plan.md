## Coverage Strategy

Specifications remain the source of unit-test requirements. This plan records only additional
integration and end-to-end obligations, the acceptance testing envelope, and exceptional human-only
obligations.

The design places all decision logic in two pure-ish functions: `Step.validateFieldConstraints` and
`resolveInvocationContext`, with the `isStdinTerminal` seam. Unit tables there cover the full matrix of
mode, step field, user setting, CLI, TTY, external user, and capture.

The automated obligations below cover the wiring those unit tests can't see:
- the YAML field reaching `model.Step`;
- the user settings file reaching `ExecutionContext`;
- a resolution failure stopping the step before any CLI process starts;
- ordinary step-failure handling (`continue_on_failure`) applying to that failure;
- `-validate` and `--external-user` surfacing the errors at the public CLI.

INT-001, E2E-001, and E2E-002 run in the existing testscript suite (`cmd/agent-runner/testdata/scripts/*.txtar`), which runs the
real binary without a TTY and uses a `bin/claude` stub on `PATH` that logs its arguments. That is exactly
the unattended environment the new failure rule targets. No new suites or CI jobs.

The successful path for a step that requires the interactive backend (TTY present) is covered by
INT-002. It extends the existing PTY workflow test in
`cmd/agent-runner/smoke_interactive_integration_test.go`, which launches the built binary directly under
a real PTY with an isolated `HOME` and a fake CLI that speaks the completion protocol. It doesn't use the
browser TUI, so the synthetic-PTY hotkey limitation doesn't apply.

## Integration Tests

### INT-001: Step-required interactive backend in external-user mode fails before launch
- Covers: `external-user-mode` "Other step types in external-user mode": step-level interactive
  rejected; step-level headless and inherited setting still run headless.
- Boundary: `--external-user` CLI flag → runner `ExecutionContext.ExternalUser` → agent executor →
  process spawn.
- Setup: testscript with an isolated `HOME`, a writable exchange directory, a `bin/claude` stub that
  appends its arguments to a log, and a profile with `default_mode: autonomous, cli: claude`. The
  workflow has three autonomous agent steps:
  1. `autonomous_backend: interactive` with `continue_on_failure: true`;
  2. `autonomous_backend: headless`;
  3. no field, with `~/.agent-runner/settings.yaml` setting `autonomous_backend: interactive`.
- Action: run the workflow with `--external-user $WORK/exchange`.
- Assertions:
  - stderr names step 1 and says the autonomous-interactive backend is unsupported in external-user mode;
  - the stub log has exactly two invocations, both with headless (print-mode) arguments, for steps 2
    and 3;
  - the run completes, because step 1 continues on failure.
- Execution: `cmd/agent-runner/testdata/scripts/autonomous_backend_external_user.txtar`, run by
  `go test ./cmd/agent-runner` in the existing `test` job.

### INT-002: Step-level interactive backend launches autonomous-interactive under a real PTY
- Covers: `cli-adapter` "Effective autonomous backend resolution", scenario "Step-required interactive
  with TTY" (autonomy instructions and autonomous permission flags); `step-model` "Per-step autonomous
  backend" (step overrides a `headless` user setting).
- Boundary: workflow YAML field → loader → runner settings → agent executor → Claude adapter args →
  direct terminal handoff under a PTY → control-channel completion → step success.
- Setup: reuse the helpers from `TestInteractiveDirectHandoffWorkflowIntegration`:
  - `buildAgentRunner`, `writeSmokeProfileConfig`, `writeInteractiveAgentFixtures` (fake `claude`),
    `runInteractiveWorkflowInPTY`;
  - an isolated `HOME` whose `~/.agent-runner/settings.yaml` sets `autonomous_backend: headless`;
  - a workflow with one agent step: `mode: autonomous`, `autonomous_backend: interactive`, a prompt,
    `session: new`, and an agent profile whose CLI is Claude.
- Action: run the workflow in a PTY; the fake CLI completes through the existing
  `completeInteractiveFixture` control-channel path.
- Assertions:
  - the fixture log records exactly one Claude invocation, and its args are the autonomous-interactive
    form (no headless print-mode flag);
  - its system-prompt argument contains the autonomy and completion-signal instructions;
  - its args include the autonomous permission flags for the configured `autonomous_permission_mode`;
  - the run state is `Completed` and the audit log records the step as successful.
- Execution: a new test function in `cmd/agent-runner/smoke_interactive_integration_test.go`, run by
  `go test ./cmd/agent-runner` in the existing `test` job. It is skipped on Windows like its siblings.

## End-to-End Tests

### E2E-001: Step-level backend without a TTY fails the step, leaves others alone
- Covers: `cli-adapter` "Step-required interactive backend fails without a TTY", "TTY fallback for
  autonomous-interactive" (narrowed), and "Effective autonomous backend resolution" (step `headless`
  overrides setting `interactive`); `step-model` "Per-step autonomous backend" (per-step override).
- Surface: `agent-runner <workflow>` CLI.
- Setup: same stub and profile pattern as `multi_profile_active.txtar`, with isolated `HOME` and
  `settings.yaml` setting `autonomous_backend: interactive`. The workflow has:
  - step A: `autonomous_backend: interactive`, `continue_on_failure: true`;
  - step B: no field;
  - step C: `autonomous_backend: headless`, `capture: out`;
  - a final shell step that echoes `{{out}}`.
- Journey: run the workflow with no TTY (the testscript default).
- Assertions:
  - stderr reports step A failed, naming the step and saying a TTY is required;
  - stderr carries the existing headless fallback warning for step B only;
  - the stub log has two headless invocations (B and C) and none for A;
  - the final step prints the captured stub output;
  - the exit status is success.
  - A second scenario in the same script, without `continue_on_failure` on step A, ends with non-zero
    exit, and the stub log has no invocations.
- Execution: `cmd/agent-runner/testdata/scripts/autonomous_backend_step_override.txtar`, run by
  `go test ./cmd/agent-runner`.

### E2E-002: Workflow validation rejects invalid step backends
- Covers: `step-model` "Per-step autonomous backend" (invalid value), "`autonomous_backend` field limited
  to autonomous agent steps" (shell step, `mode: interactive`), and "Interactive `autonomous_backend`
  rejected with capture".
- Surface: `agent-runner -validate <workflow file>`, plus a plain run of one invalid workflow to show
  nothing executes.
- Setup: several small workflow files in one testscript archive, each with one defect:
  - `autonomous_backend: tty`;
  - the field on a `command:` step;
  - the field with `mode: interactive`;
  - `capture` with `autonomous_backend: interactive`;
  - one valid file: `capture` with `autonomous_backend: headless`.
- Journey: run `-validate` on each file. Then run the capture-plus-interactive workflow normally.
- Assertions:
  - each invalid file fails validation with a message containing `autonomous_backend` and its specific
    reason (allowed values listed for the invalid-value case);
  - the valid file passes;
  - the normal run exits non-zero before any step runs, and the stub log is absent.
- Execution: `cmd/agent-runner/testdata/scripts/autonomous_backend_validation.txtar`, run by
  `go test ./cmd/agent-runner`.

## Acceptance Testing Envelope

- **Environments and sandboxes:** the local checkout run through `./dev.sh`, using throwaway temp project
  directories and an isolated `HOME` for settings. A real terminal can be simulated with Python
  `pty.fork` and screen reconstruction with `pyte`, per the repository `CLAUDE.md` notes. Wait for a
  sentinel string, because the first `./dev.sh` paint compiles first.
- **Credentials and secrets:** whatever local Claude/Codex CLI logins already exist on the machine. Don't
  create, copy, or print credentials.
- **Authorized effects:**
  - Running real agent CLIs is allowed for a few trivial prompts (for example "reply done and signal
    completion") inside temp directories, at negligible cost.
  - Writing temp workflows, settings files under an isolated `HOME`, and run state under those temp
    directories.
  - Delete the temp directories afterward.
- **Off limits:**
  - the user's real `~/.agent-runner/settings.yaml` and profiles;
  - built-in workflows under `workflows/` (do not add the field to them);
  - pushing, opening pull requests, or touching GitHub issues;
  - the live Agent Factory.
- **Permitted substitutes:** a stub `claude`/`codex` on `PATH` in place of a real CLI, wherever the point
  is which backend was selected rather than real agent behavior. A PTY harness in place of a human
  terminal. If a real CLI under a PTY can't be driven to completion, record that limitation rather than
  treating it as a defect.
- **Known risk areas:**
  - The external-user check moved inside the mode branches of `resolveInvocationContext`; check for
    regressions in existing external-user and capture behavior.
  - The profile `default_mode` resolving to interactive while the step sets the field fails only at run
    time.
  - `ResolveAgentInvocationContext` must not hand the terminal over (`PrepareStepHook`) for a step that
    will fail.
  - Real Claude/Codex CLIs under a PTY with a step-level interactive backend; INT-002 uses a fake CLI.
    If the acceptance pass can't drive a real CLI to completion, record it as an unexercised path, not
    a pass.
  - Accepted limitation: unattended runs without a TTY can't complete steps that require the
    interactive backend.

## Human-Only Testing

None.

## Coverage Map

| Requirement or journey | INT | E2E | HT |
| --- | --- | --- | --- |
| step-model: Per-step autonomous backend | INT-002 | E2E-001, E2E-002 | — |
| step-model: `autonomous_backend` field limited to autonomous agent steps | — | E2E-002 | — |
| step-model: Interactive `autonomous_backend` rejected with capture | — | E2E-002 | — |
| cli-adapter: Effective autonomous backend resolution | INT-002 | E2E-001 | — |
| cli-adapter: Step-required interactive backend fails without a TTY | — | E2E-001 | — |
| cli-adapter: Capture forces autonomous-headless (modified) | — | E2E-001 | — |
| cli-adapter: TTY fallback for autonomous-interactive (modified) | — | E2E-001 | — |
| external-user-mode: Other step types in external-user mode (modified) | INT-001 | — | — |
