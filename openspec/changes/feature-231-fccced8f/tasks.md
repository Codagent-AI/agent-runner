- [x] Implement the change described by these files using TDD, as `CLAUDE.md` requires. Satisfy every spec scenario and every `INT-*` obligation in the test plan (there are no `E2E-*` obligations). HT-001 is human-only and is not part of this task.

  **Adapter**
  - In `internal/cli/claude.go`, add the pure helper `claudePermissionMode(context InvocationContext, mode usersettings.AutonomousPermissionMode) string` and use it from `BuildArgsWithError` in place of the inline selection. The helper returns:
    - for interactive context: `""`, and no `--permission-mode` is emitted;
    - for every other context, when the setting is conservative or empty: `acceptEdits`;
    - for yolo in `ContextAutonomousInteractive`: `auto`;
    - for yolo in autonomous-headless and external-user: `bypassPermissions`.
  - Keep the flag at the same position in the argument order. Apply it to fresh and resumed sessions alike.
  - Do not add availability probing, version detection, or any fallback to another mode.
  - Update the "Patterns" doc comment on `BuildArgs` to describe the per-backend yolo mode.

  **Copy and docs**
  - Update the YOLO description in `internal/settingseditor/editor.go`. It must still say that YOLO pre-approves shell, file and network actions and recommend an external sandbox, and it must add that Claude steps on an interactive backend use Claude's auto mode (classifier-reviewed, and may occasionally prompt). Update any test that pins this text.
  - Leave the native onboarding YOLO label in `internal/onboarding/native/native.go` as it is.
  - Add an "Autonomous permission mode" subsection under User Settings in `docs/agent-profiles.md`. It contains the conservative/yolo × headless (including capture steps)/interactive table from `design.md` and notes on:
    - auto-mode availability and the Manual fallback (Agent Runner never substitutes another mode);
    - best-effort autonomy, with clarifying questions still blocked;
    - cross-session messaging classes and the `crossSessionInbound: accept` guidance.

  **Tests**
  - Add the unit tests in `internal/cli/adapter_test.go` listed under "Verification" in `design.md`:
    - replace "yolo autonomous-interactive uses bypass permission mode" with an `auto` assertion;
    - add a table-driven matrix of {interactive, autonomous-headless, autonomous-interactive, external-user} × {"", conservative, yolo};
    - add the resume case;
    - add the autonomous-interactive yolo case where `--disallowedTools AskUserQuestion` and `--permission-mode auto` appear together.
  - Add INT-001 to `internal/exec/agent_test.go` through the `interactiveRunnerFn` / `isStdinTerminal` seams. Use separate fresh and resume assertions as given in `test-plan.md`. On resume the preamble is absent and there is no `--append-system-prompt`; the existing resume behavior must not change.
  - Add INT-002 to `internal/exec/agent_test.go` through `mockRunner`, for both the capture-forced headless case and the plain headless case.

  Do not change the runner (`internal/exec/agent.go`), `BuildArgsInput`, the settings schema, other adapters, the external-user or conservative mappings, or any Claude messaging settings. Finish with `make fmt`, `make test`, and `make lint` passing.

  Source files:
  - [proposal.md](proposal.md)
  - [specs/cli-adapter/spec.md](specs/cli-adapter/spec.md)
  - [design.md](design.md)
  - [test-plan.md](test-plan.md)
  - [decisions.md](decisions.md)
