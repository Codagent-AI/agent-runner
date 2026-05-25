# Tasks

Tasks 2 and 3 both assume the runner-side helpers from task 1 (the `agent-runner debug` subcommand, `internal/audit/summary.go`, `internal/audit/redact.go`, `internal/resumehandoff/`, and the post-workflow marker check in `main.go`) already exist. Implement tasks in order.

## 1. Runtime helpers for the debug agent

- [ ] 1.1 Implement `agent-runner debug --state/--audit-summary/--show-workflow`, `internal/audit/summary.go`, `internal/audit/redact.go`, `internal/resumehandoff/`, and the post-workflow marker check in `main.go` (`tasks/1-runtime-helpers.md`)

## 2. Debug workflow + run-view trigger

- [ ] 2.1 Add `workflows/core/debug.yaml` (three interactive steps with `session: resume`), bundle the playbook at `workflows/core/debug/bundled/docs/playbook.md`, wire the run-view `d` keybinding, and handle `LaunchDebugMsg` in `main.go` (`tasks/2-debug-workflow-and-run-view-trigger.md`)

## 3. Onboarding-failure modal + --onboarding-from routing

- [ ] 3.1 Add the onboarding-failure modal to `internal/listview/` (mirrors splash-modal pattern), persist `Onboarding.Dismissed` on Skip via `usersettings.Save()`, launch `core:debug` with `failed_session_dir` on Debug-now, and route terminal-failure onboarding outcomes through `WithOnboardingFailure(...)` in `main.go` (`tasks/3-onboarding-failure-modal.md`)
